package codex

import (
	"bytes"
	"strings"
	"testing"
)

func TestCodexSubscriptionConfig(t *testing.T) {
	t.Parallel()
	got, err := Plan(Request{
		Version:  DocumentedVersion,
		Mode:     ModeSubscription,
		SkillIDs: []string{"anza-build"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusPlanned || got.Config != nil {
		t.Fatalf("subscription must retain native login and avoid a config patch: %#v", got)
	}
	if got.CredentialEnvironment != "" {
		t.Fatalf("subscription mode must not export or reference a provider credential: %q", got.CredentialEnvironment)
	}
	if len(got.Skills) != 1 || got.Skills[0].RelativePath != ".agents/skills/anza-build/SKILL.md" {
		t.Fatalf("subscription mode did not produce a project-scoped managed skill copy: %#v", got.Skills)
	}
}

func TestCodexOpenRouterConfig(t *testing.T) {
	t.Parallel()
	got, err := Plan(Request{
		Version:       DocumentedVersion,
		Mode:          ModeOpenRouter,
		Model:         "openai/gpt-5.6-sol",
		AuthShell:     ShellPOSIX,
		ProfileConfig: []byte("# launch profile only\nmodel_reasoning_effort = \"high\"\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusPlanned || got.Config == nil || got.ConfigScope != ScopeLaunchProfile {
		t.Fatalf("OpenRouter selection must produce a launch-profile config patch: %#v", got)
	}
	for _, want := range []string{
		"model = \"openai/gpt-5.6-sol\"",
		"model_provider = \"openrouter\"",
		"name = \"OpenRouter\"",
		"base_url = \"https://openrouter.ai/api/v1\"",
		"wire_api = \"responses\"",
		"OPENROUTER_API_KEY",
	} {
		if !strings.Contains(string(got.Config.Bytes), want) {
			t.Errorf("OpenRouter config missing %q:\n%s", want, got.Config.Bytes)
		}
	}
	if got.CredentialEnvironment != "OPENROUTER_API_KEY" {
		t.Fatalf("credential must be a symbolic reference, got %q", got.CredentialEnvironment)
	}
	for _, forbidden := range []string{"sk-or-", "OPENAI_API_KEY", "approval_policy =", "sandbox_mode ="} {
		if strings.Contains(string(got.Config.Bytes), forbidden) {
			t.Errorf("OpenRouter config contains forbidden value %q", forbidden)
		}
	}
	if got.EvidenceClass != "documented_only" {
		t.Fatalf("config syntax must not imply native qualification: %q", got.EvidenceClass)
	}
	powershell, err := Plan(Request{
		Version:       DocumentedVersion,
		Mode:          ModeOpenRouter,
		Model:         "openai/gpt-5.6-sol",
		AuthShell:     ShellPowerShell,
		ProfileConfig: []byte(""),
	})
	if err != nil {
		t.Fatal(err)
	}
	if powershell.Config == nil || !strings.Contains(string(powershell.Config.Bytes), `command = "powershell"`) || !strings.Contains(string(powershell.Config.Bytes), `Write-Output $env:OPENROUTER_API_KEY`) {
		t.Fatalf("PowerShell auth syntax does not match the reviewed OpenRouter example: %#v", powershell.Config)
	}
	for _, model := range []string{"", "gpt-5.6-sol", "openai/gpt 5", "openai/sk-or-secret"} {
		if _, err := Plan(Request{Version: DocumentedVersion, Mode: ModeOpenRouter, Model: model}); err == nil {
			t.Errorf("accepted invalid or secret-bearing model slug %q", model)
		}
	}
	unspecifiedShell, err := Plan(Request{Version: DocumentedVersion, Mode: ModeOpenRouter, Model: "openai/gpt-5.6-sol"})
	if err != nil {
		t.Fatal(err)
	}
	if unspecifiedShell.Status != StatusUnsupported || unspecifiedShell.Config != nil {
		t.Fatalf("missing shell was guessed instead of held manual: %#v", unspecifiedShell)
	}
}

func TestCodexPreserveUserConfig(t *testing.T) {
	t.Parallel()
	profile := []byte("# existing comment\nmodel_reasoning_effort = \"high\" # preserve inline comment\napproval_policy = \"on-request\"\nsandbox_mode = \"workspace-write\"\n\n[hooks.notify]\ncommand = \"keep-this-hook\"\n\n[model_providers.custom]\nname = \"Keep Me\"\n")
	profileBefore := append([]byte(nil), profile...)
	got, err := Plan(Request{
		Version:       DocumentedVersion,
		Mode:          ModeOpenRouter,
		Model:         "openai/gpt-5.6-sol",
		AuthShell:     ShellPOSIX,
		ProfileConfig: profile,
		SkillIDs:      []string{"anza-build", "anza-check"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Config == nil {
		t.Fatal("OpenRouter mode did not return a profile config edit")
	}
	for _, want := range []string{"# existing comment", "# preserve inline comment", "approval_policy = \"on-request\"", "sandbox_mode = \"workspace-write\"", "command = \"keep-this-hook\"", "name = \"Keep Me\""} {
		if !strings.Contains(string(got.Config.Bytes), want) {
			t.Errorf("config patch did not preserve %q:\n%s", want, got.Config.Bytes)
		}
	}
	if len(got.Skills) != 2 {
		t.Fatalf("got %d skill copies, want 2", len(got.Skills))
	}
	if !bytes.Equal(profile, profileBefore) {
		t.Fatal("planning mutated the caller's config bytes")
	}
	for _, skill := range got.Skills {
		if !strings.HasPrefix(skill.RelativePath, ".agents/skills/") || !strings.HasSuffix(skill.RelativePath, "/SKILL.md") || strings.Contains(skill.RelativePath, "AGENTS.md") || skill.ConflictPolicy != "preserve_existing" {
			t.Errorf("unsafe or overwriting skill copy intent: %#v", skill)
		}
	}
	conflict, err := Plan(Request{
		Version:       DocumentedVersion,
		Mode:          ModeOpenRouter,
		Model:         "openai/gpt-5.6-sol",
		AuthShell:     ShellPOSIX,
		ProfileConfig: []byte("model = \"existing/model\"\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if conflict.Status != StatusConflict || conflict.Config != nil || conflict.Reason == "" {
		t.Fatalf("unowned model selection was not preserved as an explicit conflict: %#v", conflict)
	}
}

func TestCodexVersionGate(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"0.157.2", "0.157.1-beta.1", "codex 0.157.1", ""} {
		got, err := Plan(Request{Version: version, Mode: ModeOpenRouter, Model: "openai/gpt-5.6-sol"})
		if err != nil {
			t.Errorf("version %q returned unexpected error: %v", version, err)
			continue
		}
		if got.Status != StatusUnsupported || got.Config != nil || len(got.Skills) != 0 || got.Reason == "" {
			t.Errorf("version %q did not return explicit unsupported result: %#v", version, got)
		}
	}
}
