// Package claude plans preserving Claude settings, launch environments, and
// managed project skill copies.
package claude

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
)

type Mode string
type Status string
type AuthState string

const (
	ModeSubscription  Mode      = "subscription"
	ModeOpenRouter    Mode      = "openrouter"
	StatusPlanned     Status    = "planned"
	StatusManual      Status    = "manual"
	StatusConflict    Status    = "conflict"
	StatusUnsupported Status    = "unsupported"
	AuthUnknown       AuthState = "unknown"
	AuthNone          AuthState = "none"
	AuthSubscription  AuthState = "subscription"
	DocumentedVersion           = "2.1.276"
	ProjectAgentsRoot           = "project-agents"
	ProjectClaudeRoot           = "project-claude"
)

type SkillRoot struct {
	// ID is a sanitized project-scope label, never an absolute filesystem path.
	ID string
	// PhysicalID is an opaque, caller-computed identity shared by symlink aliases.
	PhysicalID string
}

type Request struct {
	Version    string
	Mode       Mode
	Model      string
	Settings   []byte
	AuthState  AuthState
	SkillIDs   []string
	SkillRoots []SkillRoot
}

type EnvironmentValue struct {
	Name          string
	Value         string
	CredentialRef string
}

type SkillCopy struct {
	SkillID        string
	SourceAssetID  string
	RootID         string
	RelativePath   string
	ConflictPolicy string
}

type Result struct {
	Status        Status
	Reason        string
	Environment   []EnvironmentValue
	LaunchArgs    []string
	SkillCopies   []SkillCopy
	EvidenceClass string
}

var ErrMalformedSettings = errors.New("malformed Claude settings JSON")

var (
	ErrInvalidMode        = errors.New("unsupported Claude auth mode")
	ErrInvalidModel       = errors.New("invalid OpenRouter model slug")
	ErrInvalidSkill       = errors.New("invalid managed skill ID")
	ErrInvalidSkillRoot   = errors.New("invalid or unknown physical skill root")
	claudeVersionPattern  = regexp.MustCompile(`^2\.1\.276$`)
	openRouterModelSlug   = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*/[a-z0-9][a-z0-9._-]*$`)
	managedSkillIDPattern = regexp.MustCompile(`^anza-[a-z0-9][a-z0-9._-]{0,94}$`)
	physicalIDPattern     = regexp.MustCompile(`^[a-zA-Z0-9._:-]{1,128}$`)
)

const maxClaudeSettingsBytes = 1 << 20

// Plan returns provider environment and managed skill-copy intents without
// reading or changing files. Settings bytes are validated but otherwise
// retained by the caller. Skill roots must carry an opaque physical identity
// supplied by a read-only discovery layer so symlinked aliases can be folded.
func Plan(request Request) (Result, error) {
	if err := validateSettings(request.Settings); err != nil {
		return Result{}, err
	}
	if !claudeVersionPattern.MatchString(request.Version) {
		return Result{
			Status:        StatusUnsupported,
			Reason:        fmt.Sprintf("Claude Code %q is outside the reviewed configuration snapshot %s; re-review compatibility before planning changes", request.Version, DocumentedVersion),
			EvidenceClass: "documented_only",
		}, nil
	}
	copies, err := planSkillCopies(request.SkillIDs, request.SkillRoots)
	if err != nil {
		return Result{}, err
	}
	result := Result{Status: StatusPlanned, SkillCopies: copies, EvidenceClass: "documented_only"}
	switch request.Mode {
	case ModeSubscription:
		// Keep Claude's native login and settings untouched.
		return result, nil
	case ModeOpenRouter:
		if !openRouterModelSlug.MatchString(request.Model) || strings.Contains(strings.ToLower(request.Model), "sk-") {
			return Result{}, ErrInvalidModel
		}
		switch request.AuthState {
		case AuthUnknown:
			result.Status = StatusManual
			result.Reason = "Check Claude Code /status and ask the participant to confirm the current auth source before selecting OpenRouter; do not log out automatically."
			return result, nil
		case AuthSubscription:
			result.Status = StatusConflict
			result.Reason = "A cached Claude subscription session conflicts with OpenRouter auth. Preserve the session; ask the participant to choose a mode and, only if they choose to switch, perform Claude's documented /logout action themselves."
			return result, nil
		case AuthNone:
			conflictingKeys, err := settingsAuthConflicts(request.Settings)
			if err != nil {
				return Result{}, err
			}
			if len(conflictingKeys) != 0 {
				result.Status = StatusConflict
				result.Reason = fmt.Sprintf("Claude settings already define provider environment key(s) %s; preserve them and ask the participant to review before selecting OpenRouter.", strings.Join(conflictingKeys, ", "))
				return result, nil
			}
			// Explicitly clear the competing API-key variable in the child
			// process environment; never inspect or persist its prior value.
			result.Environment = []EnvironmentValue{
				{Name: "ANTHROPIC_BASE_URL", Value: "https://openrouter.ai/api"},
				{Name: "ANTHROPIC_AUTH_TOKEN", CredentialRef: "OPENROUTER_API_KEY"},
				{Name: "ANTHROPIC_API_KEY", Value: ""},
			}
			result.LaunchArgs = []string{"--model", request.Model}
			return result, nil
		default:
			return Result{}, fmt.Errorf("unsupported Claude auth state %q", request.AuthState)
		}
	default:
		return Result{}, fmt.Errorf("%w: %q", ErrInvalidMode, request.Mode)
	}
}

func settingsAuthConflicts(settings []byte) ([]string, error) {
	if settings == nil {
		return nil, nil
	}
	var topLevel map[string]json.RawMessage
	if err := json.Unmarshal(settings, &topLevel); err != nil {
		return nil, fmt.Errorf("%w: unable to inspect provider settings", ErrMalformedSettings)
	}
	envJSON, exists := topLevel["env"]
	if !exists || bytes.Equal(bytes.TrimSpace(envJSON), []byte("null")) {
		return nil, nil
	}
	var env map[string]json.RawMessage
	if err := json.Unmarshal(envJSON, &env); err != nil || env == nil {
		return []string{"env"}, nil
	}
	var conflicts []string
	for _, key := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL"} {
		if _, exists := env[key]; exists {
			conflicts = append(conflicts, key)
		}
	}
	return conflicts, nil
}

func planSkillCopies(ids []string, roots []SkillRoot) ([]SkillCopy, error) {
	sortedRoots := append([]SkillRoot(nil), roots...)
	sort.Slice(sortedRoots, func(i, j int) bool { return sortedRoots[i].ID < sortedRoots[j].ID })
	rootIDs := make(map[string]string, len(sortedRoots))
	seenPhysical := make(map[string]struct{}, len(sortedRoots))
	uniqueRoots := make([]SkillRoot, 0, len(sortedRoots))
	for _, root := range sortedRoots {
		if (root.ID != ProjectAgentsRoot && root.ID != ProjectClaudeRoot) || !physicalIDPattern.MatchString(root.PhysicalID) {
			return nil, fmt.Errorf("%w: root identity must be an opaque token", ErrInvalidSkillRoot)
		}
		if physical, exists := rootIDs[root.ID]; exists && physical != root.PhysicalID {
			return nil, fmt.Errorf("%w: logical root ID maps to multiple targets", ErrInvalidSkillRoot)
		}
		rootIDs[root.ID] = root.PhysicalID
		if _, duplicate := seenPhysical[root.PhysicalID]; duplicate {
			continue
		}
		seenPhysical[root.PhysicalID] = struct{}{}
		uniqueRoots = append(uniqueRoots, root)
	}

	uniqueSkills := make(map[string]struct{}, len(ids))
	sortedSkills := append([]string(nil), ids...)
	sort.Strings(sortedSkills)
	for _, id := range sortedSkills {
		if !managedSkillIDPattern.MatchString(id) {
			return nil, fmt.Errorf("%w: %q", ErrInvalidSkill, id)
		}
		uniqueSkills[id] = struct{}{}
	}
	canonicalSkills := make([]string, 0, len(uniqueSkills))
	for id := range uniqueSkills {
		canonicalSkills = append(canonicalSkills, id)
	}
	sort.Strings(canonicalSkills)

	copies := make([]SkillCopy, 0, len(uniqueRoots)*len(canonicalSkills))
	for _, root := range uniqueRoots {
		for _, id := range canonicalSkills {
			copies = append(copies, SkillCopy{
				SkillID:        id,
				SourceAssetID:  id,
				RootID:         root.ID,
				RelativePath:   id + "/SKILL.md",
				ConflictPolicy: "preserve_existing",
			})
		}
	}
	return copies, nil
}

func validateSettings(settings []byte) error {
	if settings == nil {
		return nil
	}
	if len(settings) == 0 || len(settings) > maxClaudeSettingsBytes {
		return fmt.Errorf("%w: settings size is empty or exceeds the 1 MiB limit", ErrMalformedSettings)
	}
	decoder := json.NewDecoder(bytes.NewReader(settings))
	decoder.UseNumber()
	first, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("%w: invalid JSON", ErrMalformedSettings)
	}
	opening, ok := first.(json.Delim)
	if !ok || opening != '{' {
		return fmt.Errorf("%w: root value must be an object", ErrMalformedSettings)
	}
	if err := consumeJSONObject(decoder, 0); err != nil {
		return fmt.Errorf("%w: invalid JSON object", ErrMalformedSettings)
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("%w: trailing JSON value", ErrMalformedSettings)
	}
	return nil
}

func consumeJSONValue(decoder *json.Decoder, depth int) error {
	if depth > 128 {
		return ErrMalformedSettings
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return nil
	}
	switch delimiter {
	case '{':
		return consumeJSONObject(decoder, depth+1)
	case '[':
		for decoder.More() {
			if err := consumeJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
		close, err := decoder.Token()
		if err != nil || close != json.Delim(']') {
			return ErrMalformedSettings
		}
		return nil
	default:
		return ErrMalformedSettings
	}
}

func consumeJSONObject(decoder *json.Decoder, depth int) error {
	if depth > 128 {
		return ErrMalformedSettings
	}
	seen := make(map[string]struct{})
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		key, ok := token.(string)
		if !ok {
			return ErrMalformedSettings
		}
		if _, exists := seen[key]; exists {
			return ErrMalformedSettings
		}
		seen[key] = struct{}{}
		if err := consumeJSONValue(decoder, depth+1); err != nil {
			return err
		}
	}
	close, err := decoder.Token()
	if err != nil || close != json.Delim('}') {
		return ErrMalformedSettings
	}
	return nil
}
