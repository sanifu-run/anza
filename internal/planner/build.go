// Package planner turns reviewed local facts and catalog selections into a
// deterministic, digest-bound plan. It performs no filesystem or process I/O.
package planner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/sanifu-run/anza/internal/adapters/claude"
	"github.com/sanifu-run/anza/internal/adapters/codex"
	"github.com/sanifu-run/anza/internal/configedit"
	"github.com/sanifu-run/anza/internal/domain"
	"github.com/sanifu-run/anza/internal/platform"
)

var (
	ErrInvalidInput     = errors.New("invalid planner input")
	ErrUnsupported      = errors.New("selected item is unsupported")
	ErrConflict         = errors.New("planned change conflicts with existing state")
	ErrUnsafeText       = errors.New("recommendation contains command-like text")
	pathSegmentPattern  = regexp.MustCompile(`(^|/)\.\.?(?:/|$)`)
	drivePathPattern    = regexp.MustCompile(`^[A-Za-z]:`)
	catalogIDPattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,99}$`)
	commandTextPattern  = regexp.MustCompile(`(?i)(` + "`" + `|\$\(|\|\||&&|;|\b(?:sudo|curl|wget|rm\s+-|chmod\s|chown\s|powershell\s+-|bash\s+-c|sh\s+-c)\b)`)
	allowedLogicalRoots = map[string]bool{"launch-profile": true, "project-agents": true, "project-claude": true, "tool-cache": true}
)

// Registry is the immutable catalog surface required by the planner.
type Registry interface {
	Version() string
	Digest() string
	Recipe(string) (domain.Recipe, bool)
	Pack(string) (domain.Pack, bool)
	Exercise(string) (domain.Exercise, bool)
	HasSkill(string) bool
}

// Target names an executor-owned logical root and a safe relative path.
type Target struct {
	Root         string
	RelativePath string
}

// ObservedFile is supplied by the caller's read-only inspection layer.
type ObservedFile struct {
	Exists bool
	Bytes  []byte
}

// ExistingState contains sanitized tool facts and already-read config bytes.
type ExistingState struct {
	Capabilities  map[string]domain.Capability
	ConfigTargets map[string]Target
	Files         map[Target]ObservedFile
}

// Input contains explicit planning values. ID and Now are injected so
// identical inputs can produce identical plans without hidden clock/random IO.
// Claude launch environment/arguments remain runtime-only until T7.1.
type Input struct {
	ID             string
	WorkspaceID    string
	Profile        string
	Now            time.Time
	TTL            time.Duration
	Facts          domain.MachineFacts
	Brief          domain.ProjectBrief
	Project        platform.ProjectFacts
	Recommendation domain.Recommendation
	Catalog        Registry
	Existing       ExistingState
	Codex          codex.Result
	Claude         claude.Result
	Providers      map[string]string
}

// Build resolves the selected catalog graph, turns only validated catalog or
// adapter data into fixed operation kinds, and binds the result to all inputs.
func Build(in Input) (domain.Plan, error) {
	if in.Catalog == nil || strings.TrimSpace(in.ID) == "" || strings.TrimSpace(in.WorkspaceID) == "" || in.Now.IsZero() {
		return domain.Plan{}, fmt.Errorf("%w: catalog, id, workspace and injected time are required", ErrInvalidInput)
	}
	if in.Profile != "beginner" && in.Profile != "developer" {
		return domain.Plan{}, fmt.Errorf("%w: profile must be beginner or developer", ErrInvalidInput)
	}
	if in.TTL == 0 {
		in.TTL = 24 * time.Hour
	}
	if in.TTL < time.Minute || in.TTL > 7*24*time.Hour {
		return domain.Plan{}, fmt.Errorf("%w: expiry must be between one minute and seven days", ErrInvalidInput)
	}
	if in.Recommendation.CatalogVersion != in.Catalog.Version() {
		return domain.Plan{}, fmt.Errorf("%w: recommendation catalog version does not match loaded catalog", ErrConflict)
	}
	if err := validateInputValues(in); err != nil {
		return domain.Plan{}, err
	}

	ordered, selectedPacks, err := resolveSelection(in.Recommendation, in.Catalog)
	if err != nil {
		return domain.Plan{}, err
	}
	plan := domain.Plan{
		SchemaVersion: 1, ID: in.ID, WorkspaceID: in.WorkspaceID,
		CatalogDigest: in.Catalog.Digest(), Profile: in.Profile,
		CreatedAt: in.Now.UTC().Format(time.RFC3339Nano),
		ExpiresAt: in.Now.UTC().Add(in.TTL).Format(time.RFC3339Nano),
		Providers: cloneStringMap(in.Providers), Operations: []domain.Operation{},
		Warnings: []string{}, ManualSteps: []string{},
	}
	factsDigest, err := digestValue(struct {
		Facts   domain.MachineFacts   `json:"facts"`
		Brief   domain.ProjectBrief   `json:"brief"`
		Project platform.ProjectFacts `json:"project_facts"`
	}{in.Facts, in.Brief, platform.ProjectFacts{KnownStack: sortedUnique(in.Project.KnownStack), Manifests: sortedUnique(in.Project.Manifests)}})
	if err != nil {
		return domain.Plan{}, err
	}
	plan.FactsDigest = factsDigest
	plan.RecommendationDigest, err = digestValue(in.Recommendation)
	if err != nil {
		return domain.Plan{}, err
	}

	selectedRecipeOps := make(map[string]string)
	recipeReady := make(map[string]bool, len(ordered))
	for _, recipe := range ordered {
		blockedBy := ""
		for _, prerequisite := range recipe.Prerequisites {
			if prerequisiteRecipe, ok := in.Catalog.Recipe(prerequisite); ok {
				if !recipeReady[prerequisiteRecipe.ID] {
					blockedBy = prerequisiteRecipe.ID
					break
				}
				continue
			}
			if prerequisitePack, ok := in.Catalog.Pack(prerequisite); ok && len(prerequisitePack.SkillIDs) > 0 {
				blockedBy = prerequisitePack.ID
				break
			}
		}
		if blockedBy != "" {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("%s depends on %s, which is not available through an executable operation; keep %s manual and unmet", recipe.ID, blockedBy, recipe.ID))
			plan.ManualSteps = append(plan.ManualSteps, fmt.Sprintf("Review the prerequisite %s before planning %s.", blockedBy, recipe.ID))
			recipeReady[recipe.ID] = false
			continue
		}
		cell := platformCell(in.Facts)
		if !contains(recipe.SupportedPlatforms, cell) {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("%s is not qualified for the observed platform; leave it manual and unmet", recipe.ID))
			plan.ManualSteps = append(plan.ManualSteps, fmt.Sprintf("Review the supported-platform guidance for %s before choosing an installation method.", recipe.ID))
			recipeReady[recipe.ID] = false
			continue
		}
		capability, observed := in.Existing.Capabilities[recipe.ID]
		if !observed {
			capability, observed = in.Facts.Capabilities[recipe.ID]
		}
		if observed && (capability.Status == "unsupported" || capability.Status == "unknown") {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("Capability %s is %s; it cannot enter executable operations.", recipe.ID, capability.Status))
			plan.ManualSteps = append(plan.ManualSteps, fmt.Sprintf("Review availability and compatibility for %s manually before continuing.", recipe.ID))
			recipeReady[recipe.ID] = false
			continue
		}
		if observed && capability.Status == "present" && compatibleVersion(recipe, capability.Version) {
			recipeReady[recipe.ID] = true
			continue
		}
		if observed && capability.Status == "present" {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("The installed %s version does not match the reviewed target; preserve it and review compatibility before any change.", recipe.ID))
			plan.ManualSteps = append(plan.ManualSteps, fmt.Sprintf("Review whether the existing %s version meets this project's needs; Anza will not replace or upgrade it.", recipe.ID))
			recipeReady[recipe.ID] = false
			continue
		}
		if !observed || capability.Status != "missing" {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("Capability %s was not explicitly observed missing; no executable operation was planned.", recipe.ID))
			plan.ManualSteps = append(plan.ManualSteps, fmt.Sprintf("Inspect availability of %s before planning an installation.", recipe.ID))
			recipeReady[recipe.ID] = false
			continue
		}
		if recipe.InstallStrategy == "manual" {
			plan.ManualSteps = append(plan.ManualSteps, fmt.Sprintf("Review the vendor or distribution guidance for %s and complete any installation yourself; Anza will not install or upgrade it.", recipe.ID))
			recipeReady[recipe.ID] = false
			continue
		}
		if recipe.InstallStrategy != "verified_archive" {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("%s uses an installation strategy not represented by a planner operation; keep it manual and unmet", recipe.ID))
			plan.ManualSteps = append(plan.ManualSteps, fmt.Sprintf("Review the installation method for %s with the participant; Anza will not invoke an installer or package manager.", recipe.ID))
			recipeReady[recipe.ID] = false
			continue
		}
		if recipe.Artifact == nil || !validDigest(recipe.Artifact.Digest) || recipe.Artifact.Size <= 0 || len(recipe.Privileges) != 0 {
			return domain.Plan{}, fmt.Errorf("%w: recipe %q lacks a planner-qualified artifact or uses an unknown privilege", ErrUnsupported, recipe.ID)
		}
		path := "artifacts/" + recipe.ID + "/" + recipe.Version
		if !safeRelative(path) {
			return domain.Plan{}, fmt.Errorf("%w: recipe %q has an unsafe target", ErrInvalidInput, recipe.ID)
		}
		op := domain.Operation{
			ID: "install-" + recipe.ID, Kind: "install_artifact", RecipeID: recipe.ID,
			Version: recipe.Version, Dependencies: []string{}, TargetRoot: "tool-cache",
			RelativePath: path, ExpectedPostimageHash: recipe.Artifact.Digest,
			Reversible: false, Privilege: "none",
			Description: fmt.Sprintf("Acquire the verified %s artifact for %s; installation remains subject to explicit review.", recipe.ID, recipe.Version),
		}
		for _, prerequisite := range recipe.Prerequisites {
			if prerequisiteOp := selectedRecipeOps[prerequisite]; prerequisiteOp != "" {
				op.Dependencies = append(op.Dependencies, prerequisiteOp)
			}
		}
		op.Dependencies = sortedUnique(op.Dependencies)
		plan.Operations = append(plan.Operations, op)
		selectedRecipeOps[recipe.ID] = op.ID
		recipeReady[recipe.ID] = true
	}

	for _, pack := range selectedPacks {
		for _, id := range pack.SkillIDs {
			if !in.Catalog.HasSkill(id) {
				return domain.Plan{}, fmt.Errorf("%w: pack references unknown skill %q", ErrInvalidInput, id)
			}
		}
		if len(pack.SkillIDs) > 0 {
			plan.ManualSteps = append(plan.ManualSteps, fmt.Sprintf("Review the selected %s instruction pack before choosing whether to copy its managed skills; this plan does not write skill files.", pack.ID))
		}
	}
	if in.Recommendation.SelectedExerciseID != "" {
		exercise, ok := in.Catalog.Exercise(in.Recommendation.SelectedExerciseID)
		if !ok {
			return domain.Plan{}, fmt.Errorf("%w: unknown exercise %q", ErrInvalidInput, in.Recommendation.SelectedExerciseID)
		}
		found := false
		for _, scenario := range exercise.Scenarios {
			if scenario.ProjectKind != in.Brief.ProjectKind || !contains(scenario.SupportedPlatforms, platformCell(in.Facts)) {
				continue
			}
			found = true
			plan.ManualSteps = append(plan.ManualSteps, scenario.ManualSteps...)
			plan.Warnings = append(plan.Warnings, scenario.ReadinessConstraints...)
			if scenario.Status == "unsupported" {
				plan.Warnings = append(plan.Warnings, scenario.Summary)
			}
		}
		if !found {
			plan.Warnings = append(plan.Warnings, "No catalog exercise scenario matches the reviewed project kind and exact platform; readiness remains manual.")
		}
	}
	plan.ManualSteps = append(plan.ManualSteps, in.Recommendation.ManualSteps...)
	if len(in.Recommendation.UnresolvedQuestions) > 0 {
		plan.Warnings = append(plan.Warnings, fmt.Sprintf("The recommendation has %d unresolved question(s); review them before approval.", len(in.Recommendation.UnresolvedQuestions)))
	}
	if len(in.Recommendation.ReadinessConstraints) > 0 {
		plan.Warnings = append(plan.Warnings, in.Recommendation.ReadinessConstraints...)
	}

	if err := addCodexConfigOperation(&plan, in); err != nil {
		return domain.Plan{}, err
	}
	addAdapterManualStatus(&plan, "Codex", string(in.Codex.Status), in.Codex.Reason)
	addAdapterManualStatus(&plan, "Claude Code", string(in.Claude.Status), in.Claude.Reason)
	if len(in.Codex.Skills) > 0 || len(in.Claude.SkillCopies) > 0 {
		plan.ManualSteps = append(plan.ManualSteps, "Review managed agent skill copies manually; this planner does not emit writes without a verified skill-content digest.")
	}
	plan.ManualSteps = sortedUnique(plan.ManualSteps)
	plan.Warnings = sortedUnique(plan.Warnings)
	plan.Operations = append([]domain.Operation{}, plan.Operations...)
	plan.Digest, err = domain.CanonicalPlanDigest(plan)
	if err != nil {
		return domain.Plan{}, fmt.Errorf("digesting plan: %w", err)
	}
	encoded, err := json.Marshal(plan)
	if err != nil {
		return domain.Plan{}, err
	}
	if _, err := domain.DecodePlan(encoded); err != nil {
		return domain.Plan{}, fmt.Errorf("validating planned output: %w", err)
	}
	return plan, nil
}

func validateInputValues(in Input) error {
	if len(in.Project.KnownStack) > 20 || len(in.Project.Manifests) > 4 {
		return fmt.Errorf("%w: sanitized project facts exceed limits", ErrInvalidInput)
	}
	if len(in.Existing.Capabilities) > 200 {
		return fmt.Errorf("%w: existing capabilities exceed limits", ErrInvalidInput)
	}
	for id, capability := range in.Existing.Capabilities {
		if !catalogIDPattern.MatchString(id) || !oneOf(capability.Status, "present", "missing", "unknown", "unsupported") || len(capability.Version) > 100 {
			return fmt.Errorf("%w: malformed existing capability", ErrInvalidInput)
		}
	}
	if in.Codex.Status != "" && in.Codex.Status != codex.StatusPlanned && in.Codex.Status != codex.StatusUnsupported && in.Codex.Status != codex.StatusConflict {
		return fmt.Errorf("%w: invalid Codex adapter status", ErrInvalidInput)
	}
	if in.Claude.Status != "" && in.Claude.Status != claude.StatusPlanned && in.Claude.Status != claude.StatusManual && in.Claude.Status != claude.StatusConflict && in.Claude.Status != claude.StatusUnsupported {
		return fmt.Errorf("%w: invalid Claude adapter status", ErrInvalidInput)
	}
	for _, id := range in.Project.KnownStack {
		if !catalogIDPattern.MatchString(id) {
			return fmt.Errorf("%w: invalid sanitized project stack ID", ErrInvalidInput)
		}
	}
	for _, manifest := range in.Project.Manifests {
		if manifest != "package.json" && manifest != "go.mod" && manifest != "Cargo.toml" && manifest != "pyproject.toml" {
			return fmt.Errorf("%w: unsupported sanitized manifest name", ErrInvalidInput)
		}
	}
	brief, err := json.Marshal(in.Brief)
	if err != nil {
		return err
	}
	if _, err := domain.DecodeProjectBrief(brief); err != nil {
		return fmt.Errorf("%w: brief: %v", ErrInvalidInput, err)
	}
	facts, err := json.Marshal(in.Facts)
	if err != nil {
		return err
	}
	if _, err := domain.DecodeMachineFacts(facts); err != nil {
		return fmt.Errorf("%w: machine facts: %v", ErrInvalidInput, err)
	}
	recommendation, err := json.Marshal(in.Recommendation)
	if err != nil {
		return err
	}
	if _, err := domain.DecodeRecommendation(recommendation); err != nil {
		return fmt.Errorf("%w: recommendation: %v", ErrInvalidInput, err)
	}
	for agent, provider := range in.Providers {
		if (agent != "codex" && agent != "claude") || (provider != "subscription" && provider != "openrouter") {
			return fmt.Errorf("%w: unsupported provider choice for %q", ErrInvalidInput, agent)
		}
	}
	for _, text := range recommendationText(in.Recommendation) {
		if commandTextPattern.MatchString(text) {
			return fmt.Errorf("%w: executable syntax is not accepted", ErrUnsafeText)
		}
	}
	return nil
}

func recommendationText(r domain.Recommendation) []string {
	out := []string{r.Summary}
	for _, values := range [][]string{r.ManualSteps, r.UnresolvedQuestions, r.ReadinessConstraints} {
		out = append(out, values...)
	}
	keys := make([]string, 0, len(r.Reasons))
	for key := range r.Reasons {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		out = append(out, r.Reasons[key])
	}
	return out
}

func addCodexConfigOperation(plan *domain.Plan, in Input) error {
	edit := in.Codex.Config
	if edit == nil {
		return nil
	}
	if in.Codex.Status != codex.StatusPlanned || in.Codex.ConfigScope != codex.ScopeLaunchProfile {
		return fmt.Errorf("%w: Codex config edits require a planned launch-profile result", ErrConflict)
	}
	target, ok := in.Existing.ConfigTargets["codex"]
	if !ok || target.Root != "launch-profile" || !allowedLogicalRoots[target.Root] || !safeRelative(target.RelativePath) {
		return fmt.Errorf("%w: missing or unsafe Codex logical destination", ErrInvalidInput)
	}
	observed, hasObservation := in.Existing.Files[target]
	if !hasObservation {
		return fmt.Errorf("%w: current Codex config preimage was not supplied", ErrConflict)
	}
	if err := checkEdit(*edit, observed); err != nil {
		return err
	}
	if observed.Exists && digestBytes(observed.Bytes) == edit.PostimageHash {
		return nil
	}
	if !observed.Exists && edit.InputMissing && len(edit.Bytes) == 0 {
		return nil
	}
	op := domain.Operation{
		ID: "write-codex-profile", Kind: "write_config", Dependencies: []string{},
		TargetRoot: target.Root, RelativePath: target.RelativePath,
		ExpectedPostimageHash: edit.PostimageHash, Reversible: true, Privilege: "none",
		Description: "Update only the Anza-owned Codex launch profile keys and preserve unrelated settings.",
	}
	if observed.Exists {
		op.PreimageHash = digestBytes(observed.Bytes)
	}
	plan.Operations = append(plan.Operations, op)
	return nil
}

func checkEdit(edit configedit.Edit, observed ObservedFile) error {
	return checkConfigEdit(edit, observed)
}

func addAdapterManualStatus(plan *domain.Plan, name, status, reason string) {
	if status == "" || status == "planned" {
		return
	}
	plan.Warnings = append(plan.Warnings, fmt.Sprintf("%s setup status is %s; no automatic configuration operation was added.", name, status))
	if strings.TrimSpace(reason) != "" {
		plan.ManualSteps = append(plan.ManualSteps, fmt.Sprintf("Review the %s setup guidance: %s", name, reason))
	}
}

func digestValue(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return digestBytes(b), nil
}
func digestBytes(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
func validDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil && strings.ToLower(s) == s
}
func cloneStringMap(in map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		out[k] = v
	}
	return out
}
func sortedUnique(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, value := range in {
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}
func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
func compatibleVersion(recipe domain.Recipe, observed string) bool {
	return observed != "" && (recipe.Version == "platform-managed" || recipe.Version == observed)
}
func safeRelative(path string) bool {
	return path != "" && strings.TrimSpace(path) == path && !strings.HasPrefix(path, "/") && !strings.Contains(path, `\`) && !strings.Contains(path, ":") && !strings.Contains(path, "//") && !strings.HasSuffix(path, "/") && !drivePathPattern.MatchString(path) && !pathSegmentPattern.MatchString(path)
}

func platformCell(f domain.MachineFacts) string {
	if f.OS != "linux" {
		return f.OS + "-" + f.Arch
	}
	distro := strings.ToLower(strings.TrimSpace(f.DistroID))
	version := strings.TrimSpace(f.DistroVersion)
	if distro == "" || distro == "unknown" || version == "" || version == "unknown" {
		return ""
	}
	return "linux-" + distro + "-" + strings.ReplaceAll(version, ".", "-") + "-" + f.Arch
}
