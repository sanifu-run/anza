package planner

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/sanifu-run/anza/internal/adapters/claude"
	"github.com/sanifu-run/anza/internal/adapters/codex"
	"github.com/sanifu-run/anza/internal/catalog"
	"github.com/sanifu-run/anza/internal/configedit"
	"github.com/sanifu-run/anza/internal/domain"
)

func testInput(t *testing.T) Input {
	t.Helper()
	cat, err := catalog.LoadBundled()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	return Input{
		ID: "plan-test-1", WorkspaceID: "workspace-test", Profile: "developer",
		Now: now, TTL: 24 * time.Hour, Catalog: cat,
		Facts: domain.MachineFacts{OS: "darwin", Arch: "arm64", OSVersion: "15.0", ShellKind: "zsh", Capabilities: map[string]domain.Capability{
			"codex-cli": {Status: "missing"}, "claude-code": {Status: "missing"}, "git": {Status: "missing"},
		}},
		Brief: domain.ProjectBrief{SchemaVersion: 1, ProjectSummary: "Synthetic local workspace", DesiredSlice: "", Experience: "developer", ProjectKind: "web", ExistingProject: true, Constraints: []string{}, KnownStack: []string{}},
		Recommendation: domain.Recommendation{
			SchemaVersion: 1, CatalogVersion: cat.Version(), Summary: "Set up this workspace",
			SelectedRecipeIDs: []string{"codex-cli", "claude-code", "git"}, SelectedPackIDs: []string{"base"},
			SelectedExerciseID: "mobile-desktop-exercise", Reasons: map[string]string{},
			UnresolvedQuestions: []string{}, ManualSteps: []string{}, ReadinessConstraints: []string{},
		},
		Existing:  ExistingState{Capabilities: map[string]domain.Capability{}},
		Codex:     codex.Result{Status: codex.StatusPlanned, Skills: []codex.SkillCopy{}},
		Claude:    claude.Result{Status: claude.StatusPlanned, SkillCopies: []claude.SkillCopy{}},
		Providers: map[string]string{"codex": "subscription", "claude": "subscription"},
	}
}

func TestPlanDeterministic(t *testing.T) {
	in := testInput(t)
	first, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != second.Digest || !bytes.Equal(mustPlanJSON(t, first), mustPlanJSON(t, second)) {
		t.Fatal("same inputs produced different plans")
	}
	changedFacts := in
	changedFacts.Facts = cloneFacts(in.Facts)
	changedFacts.Facts.OSVersion = "15.1"
	changed, err := Build(changedFacts)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Digest == first.Digest {
		t.Fatal("machine facts change did not change approval digest")
	}
	changedCatalog := in
	changedCatalog.Catalog = modifiedCatalog(t, in.Catalog)
	changed, err = Build(changedCatalog)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Digest == first.Digest {
		t.Fatal("catalog change did not change approval digest")
	}
}

func TestPlanUnsupported(t *testing.T) {
	in := testInput(t)
	in.Facts.OS, in.Facts.Arch, in.Facts.DistroID, in.Facts.DistroVersion = "linux", "arm64", "fedora", "42"
	plan, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 0 {
		t.Fatalf("unsupported platform produced executable operations: %#v", plan.Operations)
	}
	if len(plan.ManualSteps) == 0 || !strings.Contains(strings.Join(plan.Warnings, "\n"), "not qualified") {
		t.Fatalf("unsupported platform lacks explicit manual constraint: %#v", plan)
	}
	in = testInput(t)
	in.Recommendation.SelectedRecipeIDs = []string{"git"}
	in.Recommendation.SelectedPackIDs = []string{}
	registry := recipeRegistry{Registry: in.Catalog, overrides: map[string]domain.Recipe{}}
	recipe, _ := in.Catalog.Recipe("git")
	recipe.InstallStrategy = "verified_archive"
	recipe.Artifact = &domain.Artifact{Digest: strings.Repeat("a", 64), Size: 100, Origin: "https://vendor.invalid/artifact"}
	recipe.Privileges = []string{}
	registry.overrides["git"] = recipe
	in.Catalog = registry
	in.Facts.Capabilities["git"] = domain.Capability{Status: "unsupported"}
	plan, err = Build(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 0 || !strings.Contains(strings.Join(plan.Warnings, "\n"), "cannot enter executable operations") {
		t.Fatalf("unsupported capability entered executable plan or lacked constraint: %#v", plan)
	}
	delete(in.Facts.Capabilities, "git")
	plan, err = Build(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 0 || !strings.Contains(strings.Join(plan.Warnings, "\n"), "not explicitly observed missing") {
		t.Fatalf("unobserved capability entered executable plan or lacked constraint: %#v", plan)
	}
}

func TestPlanOrdersExecutablePrerequisites(t *testing.T) {
	in := testInput(t)
	registry := recipeRegistry{Registry: in.Catalog, overrides: map[string]domain.Recipe{}}
	for _, id := range []string{"git", "codex-cli"} {
		recipe, ok := in.Catalog.Recipe(id)
		if !ok {
			t.Fatalf("missing bundled recipe %s", id)
		}
		recipe.InstallStrategy = "verified_archive"
		recipe.Artifact = &domain.Artifact{Digest: strings.Repeat("a", 64), Size: 100, Origin: "https://vendor.invalid/artifact"}
		recipe.Privileges = []string{}
		registry.overrides[id] = recipe
	}
	codexRecipe := registry.overrides["codex-cli"]
	codexRecipe.Prerequisites = []string{"git"}
	registry.overrides["codex-cli"] = codexRecipe
	in.Catalog = registry
	plan, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 2 {
		t.Fatalf("expected two selected artifact operations, got %#v", plan.Operations)
	}
	if plan.Operations[0].RecipeID != "git" || plan.Operations[1].RecipeID != "codex-cli" {
		t.Fatalf("operations are not dependency ordered: %#v", plan.Operations)
	}
	if len(plan.Operations[1].Dependencies) != 1 || plan.Operations[1].Dependencies[0] != plan.Operations[0].ID {
		t.Fatalf("dependent operation lacks prerequisite edge: %#v", plan.Operations)
	}
}

func TestPlanRejectsDependencyCycle(t *testing.T) {
	in := testInput(t)
	recipe, ok := in.Catalog.Recipe("codex-cli")
	if !ok {
		t.Fatal("missing Codex recipe")
	}
	recipe.Prerequisites = []string{"base"}
	in.Catalog = recipeRegistry{Registry: in.Catalog, overrides: map[string]domain.Recipe{"codex-cli": recipe}}
	if _, err := Build(in); err == nil {
		t.Fatal("cyclic catalog prerequisite graph was accepted")
	}
}

func TestPlanRejectsUnknownPrivilege(t *testing.T) {
	in := testInput(t)
	in.Recommendation.SelectedRecipeIDs = []string{"git"}
	in.Recommendation.SelectedPackIDs = []string{}
	recipe, ok := in.Catalog.Recipe("git")
	if !ok {
		t.Fatal("missing Git recipe")
	}
	recipe.InstallStrategy = "verified_archive"
	recipe.Artifact = &domain.Artifact{Digest: strings.Repeat("a", 64), Size: 100, Origin: "https://vendor.invalid/artifact"}
	recipe.Privileges = []string{"administrator"}
	in.Catalog = recipeRegistry{Registry: in.Catalog, overrides: map[string]domain.Recipe{"git": recipe}}
	if _, err := Build(in); err == nil {
		t.Fatal("unmapped privilege entered executable plan")
	}
}

func TestPlanPreservesExisting(t *testing.T) {
	in := testInput(t)
	in.Facts.Capabilities["git"] = domain.Capability{Status: "present", Version: "2.56.0"}
	plan, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range plan.Operations {
		if op.RecipeID == "git" {
			t.Fatal("planner scheduled a duplicate write for an installed compatible tool")
		}
	}
	edit, err := configedit.Plan([]byte("# participant profile\n"), configedit.Patch{Format: configedit.TOML, Set: map[string]any{"model": "openai/gpt-6"}})
	if err != nil {
		t.Fatal(err)
	}
	in.Codex = codex.Result{Status: codex.StatusPlanned, Config: &edit, ConfigScope: codex.ScopeLaunchProfile, Skills: []codex.SkillCopy{}}
	in.Existing.ConfigTargets = map[string]Target{"codex": {Root: "launch-profile", RelativePath: "codex/config.toml"}}
	in.Existing.Files = map[Target]ObservedFile{{Root: "launch-profile", RelativePath: "codex/config.toml"}: {Exists: true, Bytes: []byte("# participant profile\n")}}
	first, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Operations) != 1 {
		t.Fatalf("expected one config operation, got %d", len(first.Operations))
	}
	drifted := in
	drifted.Existing.Files = map[Target]ObservedFile{{Root: "launch-profile", RelativePath: "codex/config.toml"}: {Exists: true, Bytes: []byte("# participant changed the file\n")}}
	if _, err := Build(drifted); err == nil {
		t.Fatal("config drift after adapter planning was accepted")
	}
	otherEdit, err := configedit.Plan([]byte("# another participant profile\n"), configedit.Patch{Format: configedit.TOML, Set: map[string]any{"model": "openai/gpt-6"}})
	if err != nil {
		t.Fatal(err)
	}
	changedPreimage := in
	changedPreimage.Codex = codex.Result{Status: codex.StatusPlanned, Config: &otherEdit, ConfigScope: codex.ScopeLaunchProfile, Skills: []codex.SkillCopy{}}
	changedPreimage.Existing.Files = map[Target]ObservedFile{{Root: "launch-profile", RelativePath: "codex/config.toml"}: {Exists: true, Bytes: []byte("# another participant profile\n")}}
	withOtherPreimage, err := Build(changedPreimage)
	if err != nil {
		t.Fatal(err)
	}
	if withOtherPreimage.Digest == first.Digest {
		t.Fatal("config preimage change did not change approval digest")
	}
	in.Existing.Files[Target{Root: "launch-profile", RelativePath: "codex/config.toml"}] = ObservedFile{Exists: true, Bytes: append([]byte(nil), edit.Bytes...)}
	replannedEdit, err := configedit.Plan(edit.Bytes, configedit.Patch{Format: configedit.TOML, Set: map[string]any{"model": "openai/gpt-6"}})
	if err != nil {
		t.Fatal(err)
	}
	in.Codex.Config = &replannedEdit
	second, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Operations) != 0 {
		t.Fatalf("replanning wrote existing config again: %#v", second.Operations)
	}
}

func TestPlanRejectsProviderSecrets(t *testing.T) {
	in := testInput(t)
	in.Providers = map[string]string{"codex": "sk-live-secret"}
	if _, err := Build(in); err == nil {
		t.Fatal("unrecognized provider choice was accepted")
	}
}

func TestPlanRejectsEscapingDestination(t *testing.T) {
	in := testInput(t)
	edit, err := configedit.Plan([]byte("# participant profile\n"), configedit.Patch{Format: configedit.TOML, Set: map[string]any{"model": "openai/gpt-6"}})
	if err != nil {
		t.Fatal(err)
	}
	in.Codex = codex.Result{Status: codex.StatusPlanned, Config: &edit, ConfigScope: codex.ScopeLaunchProfile, Skills: []codex.SkillCopy{}}
	in.Existing.ConfigTargets = map[string]Target{"codex": {Root: "launch-profile", RelativePath: "C:/outside/config.toml"}}
	in.Existing.Files = map[Target]ObservedFile{{Root: "launch-profile", RelativePath: "C:/outside/config.toml"}: {Exists: true, Bytes: []byte("# participant profile\n")}}
	if _, err := Build(in); err == nil {
		t.Fatal("drive-prefixed destination was accepted")
	}
}

func TestSafeRelativeRejectsEscapingSegments(t *testing.T) {
	for _, path := range []string{"C:/foo", "a/..", "a/../config.toml", "../config.toml", "/etc/config", `a\\config.toml`} {
		if safeRelative(path) {
			t.Errorf("unsafe relative path %q was accepted", path)
		}
	}
	for _, path := range []string{"codex/config.toml", "artifacts/git/2.56.0"} {
		if !safeRelative(path) {
			t.Errorf("safe relative path %q was rejected", path)
		}
	}
}

func TestPlanCodexConfigOnMissingFile(t *testing.T) {
	in := testInput(t)
	edit, err := configedit.Plan(nil, configedit.Patch{Format: configedit.TOML, Set: map[string]any{"model": "openai/gpt-6"}})
	if err != nil {
		t.Fatal(err)
	}
	in.Codex = codex.Result{Status: codex.StatusPlanned, Config: &edit, ConfigScope: codex.ScopeLaunchProfile, Skills: []codex.SkillCopy{}}
	in.Existing.ConfigTargets = map[string]Target{"codex": {Root: "launch-profile", RelativePath: "codex/config.toml"}}
	in.Existing.Files = map[Target]ObservedFile{{Root: "launch-profile", RelativePath: "codex/config.toml"}: {Exists: false}}
	plan, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 1 || plan.Operations[0].PreimageHash != "" {
		t.Fatalf("missing config should create one guarded write with no preimage hash: %#v", plan.Operations)
	}
}

func TestPlanRejectsModelCommands(t *testing.T) {
	in := testInput(t)
	in.Recommendation.ManualSteps = []string{"Run `rm -rf ~/project` to clear old files"}
	if _, err := Build(in); err == nil {
		t.Fatal("command-like model text was accepted")
	}
}

func mustPlanJSON(t *testing.T, p domain.Plan) []byte {
	t.Helper()
	b, err := domain.CanonicalPlanJSON(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func cloneFacts(f domain.MachineFacts) domain.MachineFacts {
	out := f
	out.Capabilities = make(map[string]domain.Capability, len(f.Capabilities))
	for k, v := range f.Capabilities {
		out.Capabilities[k] = v
	}
	return out
}

func modifiedCatalog(t *testing.T, original Registry) Registry {
	t.Helper()
	sum := sha256.Sum256([]byte("different-catalog-snapshot"))
	return catalogWithDigest{Registry: original, digest: hex.EncodeToString(sum[:])}
}

type catalogWithDigest struct {
	Registry
	digest string
}

func (c catalogWithDigest) Digest() string { return c.digest }

type recipeRegistry struct {
	Registry
	overrides map[string]domain.Recipe
}

func (r recipeRegistry) Recipe(id string) (domain.Recipe, bool) {
	if recipe, ok := r.overrides[id]; ok {
		return recipe, true
	}
	return r.Registry.Recipe(id)
}
