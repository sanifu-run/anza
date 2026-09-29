// Package codex plans preserving Codex configuration and skill operations.
package codex

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/sanifu-run/anza/internal/configedit"
)

type Mode string
type Status string
type Scope string
type AuthShell string

const (
	ModeSubscription   Mode      = "subscription"
	ModeOpenRouter     Mode      = "openrouter"
	StatusPlanned      Status    = "planned"
	StatusUnsupported  Status    = "unsupported"
	StatusConflict     Status    = "conflict"
	ScopeLaunchProfile Scope     = "launch_profile_only"
	ShellPOSIX         AuthShell = "sh"
	ShellPowerShell    AuthShell = "powershell"
	DocumentedVersion            = "0.157.1"
)

type Request struct {
	Version       string
	Mode          Mode
	Model         string
	AuthShell     AuthShell
	ProfileConfig []byte
	SkillIDs      []string
}

type SkillCopy struct {
	SkillID        string
	SourceAssetID  string
	RelativePath   string
	ConflictPolicy string
}

type Result struct {
	Status                Status
	Reason                string
	Config                *configedit.Edit
	ConfigScope           Scope
	CredentialEnvironment string
	Skills                []SkillCopy
	EvidenceClass         string
}

var (
	ErrInvalidMode  = errors.New("unsupported Codex auth mode")
	ErrInvalidModel = errors.New("invalid OpenRouter model slug")
	ErrInvalidSkill = errors.New("invalid managed skill ID")
	modelSlug       = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*/[a-z0-9][a-z0-9._-]*$`)
	skillID         = regexp.MustCompile(`^anza-[a-z0-9][a-z0-9._-]{0,94}$`)
)

// Plan returns a pure config edit and managed skill copy intents. OpenRouter
// config edits are scoped to an Anza launch profile and must never be applied
// to Codex's global config. The current compatibility record documents syntax
// for 0.157.1 but does not qualify a native runtime or provider connection.
func Plan(request Request) (Result, error) {
	if request.Version != DocumentedVersion {
		return Result{
			Status:        StatusUnsupported,
			Reason:        fmt.Sprintf("Codex %q is outside the reviewed configuration snapshot %s; re-review compatibility before planning changes", request.Version, DocumentedVersion),
			EvidenceClass: "documented_only",
		}, nil
	}
	skills, err := planSkillCopies(request.SkillIDs)
	if err != nil {
		return Result{}, err
	}
	result := Result{
		Status:        StatusPlanned,
		Skills:        skills,
		EvidenceClass: "documented_only",
	}
	switch request.Mode {
	case ModeSubscription:
		// Preserve Codex's native sign-in and all global settings. No token or
		// provider selection is written or exported by the adapter.
		return result, nil
	case ModeOpenRouter:
		if !modelSlug.MatchString(request.Model) || strings.Contains(strings.ToLower(request.Model), "sk-") {
			return Result{}, ErrInvalidModel
		}
		command, args, ok := authCommand(request.AuthShell)
		if !ok {
			return Result{
				Status:        StatusUnsupported,
				Reason:        fmt.Sprintf("OpenRouter auth syntax has no reviewed mapping for shell %q; select a documented shell or keep the setup manual", request.AuthShell),
				EvidenceClass: "documented_only",
			}, nil
		}
		providerPatch := configedit.Patch{
			Format: configedit.TOML,
			Set: map[string]any{
				"model":                               request.Model,
				"model_provider":                      "openrouter",
				"model_providers.openrouter.name":     "OpenRouter",
				"model_providers.openrouter.base_url": "https://openrouter.ai/api/v1",
				"model_providers.openrouter.wire_api": "responses",
			},
		}
		providerEdit, err := configedit.Plan(request.ProfileConfig, providerPatch)
		if err != nil {
			if errors.Is(err, configedit.ErrConflict) {
				return Result{Status: StatusConflict, Reason: err.Error(), EvidenceClass: "documented_only"}, nil
			}
			return Result{}, fmt.Errorf("planning Codex OpenRouter launch profile: %w", err)
		}
		authEdit, err := configedit.Plan(providerEdit.Bytes, configedit.Patch{
			Format: configedit.TOML,
			Set: map[string]any{
				"model_providers.openrouter.auth.command": command,
				"model_providers.openrouter.auth.args":    args,
			},
		})
		if err != nil {
			if errors.Is(err, configedit.ErrConflict) {
				return Result{Status: StatusConflict, Reason: err.Error(), EvidenceClass: "documented_only"}, nil
			}
			return Result{}, fmt.Errorf("planning Codex OpenRouter auth profile: %w", err)
		}
		edit := authEdit
		edit.InputMissing = providerEdit.InputMissing
		edit.PreimageHash = providerEdit.PreimageHash
		edit.OwnedKeys = append(providerEdit.OwnedKeys, authEdit.OwnedKeys...)
		result.Config = &edit
		result.ConfigScope = ScopeLaunchProfile
		result.CredentialEnvironment = "OPENROUTER_API_KEY"
		return result, nil
	default:
		return Result{}, fmt.Errorf("%w: %q", ErrInvalidMode, request.Mode)
	}
}

func authCommand(shell AuthShell) (string, []string, bool) {
	switch shell {
	case ShellPOSIX:
		return "sh", []string{"-c", "echo $OPENROUTER_API_KEY"}, true
	case ShellPowerShell:
		return "powershell", []string{"-NoProfile", "-Command", "Write-Output $env:OPENROUTER_API_KEY"}, true
	default:
		return "", nil, false
	}
}

func planSkillCopies(ids []string) ([]SkillCopy, error) {
	seen := make(map[string]struct{}, len(ids))
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	copies := make([]SkillCopy, 0, len(sorted))
	for _, id := range sorted {
		if !skillID.MatchString(id) {
			return nil, fmt.Errorf("%w: %q", ErrInvalidSkill, id)
		}
		if _, exists := seen[id]; exists {
			return nil, fmt.Errorf("duplicate managed skill ID %q", id)
		}
		seen[id] = struct{}{}
		copies = append(copies, SkillCopy{
			SkillID:        id,
			SourceAssetID:  id,
			RelativePath:   strings.Join([]string{".agents", "skills", id, "SKILL.md"}, "/"),
			ConflictPolicy: "preserve_existing",
		})
	}
	return copies, nil
}
