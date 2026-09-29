package claude

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestClaudeSubscriptionConfig(t *testing.T) {
	t.Parallel()
	settings := []byte(`{"model":"claude-sonnet","hooks":{"Notification":[{"command":"keep-hook"}]},"permissions":{"defaultMode":"default"}}`)
	before := append([]byte(nil), settings...)
	got, err := Plan(Request{
		Version:  DocumentedVersion,
		Mode:     ModeSubscription,
		Settings: settings,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusPlanned || len(got.Environment) != 0 || len(got.LaunchArgs) != 0 {
		t.Fatalf("subscription mode should preserve native auth and produce no override: %#v", got)
	}
	if !bytes.Equal(settings, before) {
		t.Fatal("planning mutated Claude settings")
	}
}

func TestClaudeOpenRouterConfig(t *testing.T) {
	t.Parallel()
	got, err := Plan(Request{
		Version:   DocumentedVersion,
		Mode:      ModeOpenRouter,
		Model:     "anthropic/claude-sonnet-4.5",
		Settings:  []byte(`{"env":{"KEEP":"untouched"}}`),
		AuthState: AuthNone,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusPlanned || len(got.Environment) != 3 {
		t.Fatalf("OpenRouter mode should plan exactly three child-process variables: %#v", got)
	}
	want := map[string]EnvironmentValue{
		"ANTHROPIC_BASE_URL":   {Name: "ANTHROPIC_BASE_URL", Value: "https://openrouter.ai/api"},
		"ANTHROPIC_AUTH_TOKEN": {Name: "ANTHROPIC_AUTH_TOKEN", CredentialRef: "OPENROUTER_API_KEY"},
		"ANTHROPIC_API_KEY":    {Name: "ANTHROPIC_API_KEY", Value: ""},
	}
	for _, value := range got.Environment {
		if expected, ok := want[value.Name]; !ok || expected != value {
			t.Errorf("unexpected or unsafe env assignment: %#v", value)
		}
	}
	if strings.Contains(strings.Join(got.LaunchArgs, " "), "sk-") || len(got.LaunchArgs) != 2 || got.LaunchArgs[0] != "--model" || got.LaunchArgs[1] != "anthropic/claude-sonnet-4.5" {
		t.Fatalf("model selection must be a validated launch argument, got %#v", got.LaunchArgs)
	}
	if got.EvidenceClass != "documented_only" {
		t.Fatalf("adapter must not imply runtime qualification: %q", got.EvidenceClass)
	}
	if len(got.SkillCopies) != 0 {
		t.Fatalf("no skill copy intents were requested, got %#v", got.SkillCopies)
	}
}

func TestClaudeExistingAuthConflict(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		auth     AuthState
		want     Status
		contains string
	}{
		{name: "unknown auth state stays manual", auth: AuthUnknown, want: StatusManual, contains: "/status"},
		{name: "cached subscription requires participant choice", auth: AuthSubscription, want: StatusConflict, contains: "logout"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Plan(Request{Version: DocumentedVersion, Mode: ModeOpenRouter, Model: "anthropic/claude-sonnet-4.5", AuthState: tc.auth})
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != tc.want || len(got.Environment) != 0 || !strings.Contains(strings.ToLower(got.Reason), strings.ToLower(tc.contains)) {
				t.Fatalf("auth conflict did not stop automatic provider switching: %#v", got)
			}
		})
	}
	configuredKey, err := Plan(Request{
		Version:   DocumentedVersion,
		Mode:      ModeOpenRouter,
		Model:     "anthropic/claude-sonnet-4.5",
		AuthState: AuthNone,
		Settings:  []byte(`{"env":{"ANTHROPIC_AUTH_TOKEN":"synthetic-secret"}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if configuredKey.Status != StatusConflict || len(configuredKey.Environment) != 0 || !strings.Contains(configuredKey.Reason, "ANTHROPIC_AUTH_TOKEN") || strings.Contains(configuredKey.Reason, "synthetic-secret") {
		t.Fatalf("existing settings auth key was not reported without exposing its value: %#v", configuredKey)
	}
}

func TestClaudeSharedSkillRoots(t *testing.T) {
	t.Parallel()
	got, err := Plan(Request{
		Version:  DocumentedVersion,
		Mode:     ModeSubscription,
		SkillIDs: []string{"anza-build", "anza-check"},
		SkillRoots: []SkillRoot{
			{ID: ProjectAgentsRoot, PhysicalID: "physical-a"},
			{ID: ProjectClaudeRoot, PhysicalID: "physical-b"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.SkillCopies) != 4 {
		t.Fatalf("two distinct physical project roots times two skills should yield four intents, got %#v", got.SkillCopies)
	}
	seen := make(map[string]struct{})
	for _, copy := range got.SkillCopies {
		key := copy.RootID + "\x00" + copy.SkillID
		if _, exists := seen[key]; exists {
			t.Errorf("duplicate copy intent for the same physical root: %#v", copy)
		}
		seen[key] = struct{}{}
		if (copy.RootID != ProjectAgentsRoot && copy.RootID != ProjectClaudeRoot) || copy.RelativePath != copy.SkillID+"/SKILL.md" || copy.ConflictPolicy != "preserve_existing" {
			t.Errorf("unsafe or non-project skill intent: %#v", copy)
		}
	}
	shared, err := Plan(Request{
		Version:  DocumentedVersion,
		Mode:     ModeSubscription,
		SkillIDs: []string{"anza-build", "anza-check"},
		SkillRoots: []SkillRoot{
			{ID: ProjectAgentsRoot, PhysicalID: "same-physical-root"},
			{ID: ProjectClaudeRoot, PhysicalID: "same-physical-root"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(shared.SkillCopies) != 2 {
		t.Fatalf("aliases of one physical root should produce one intent per skill, got %#v", shared.SkillCopies)
	}
	for _, copy := range shared.SkillCopies {
		if copy.RootID != ProjectAgentsRoot {
			t.Errorf("shared physical root did not choose the stable canonical project label: %#v", copy)
		}
	}
	if _, err := Plan(Request{
		Version:    DocumentedVersion,
		Mode:       ModeSubscription,
		SkillIDs:   []string{"anza-build"},
		SkillRoots: []SkillRoot{{ID: "project-home-dndungu", PhysicalID: "physical-private"}},
	}); !errors.Is(err, ErrInvalidSkillRoot) {
		t.Errorf("private/nonstandard root label was not rejected: %v", err)
	}
}

func TestClaudeMalformedSettings(t *testing.T) {
	t.Parallel()
	for _, fixture := range []string{
		`{"env":{"A":"one","A":"two"}}`,
		`[]`,
		`{"env":`,
		`{} {}`,
	} {
		if _, err := Plan(Request{Version: DocumentedVersion, Mode: ModeSubscription, Settings: []byte(fixture)}); !errors.Is(err, ErrMalformedSettings) {
			t.Errorf("malformed settings %q returned %v, want ErrMalformedSettings", fixture, err)
		}
	}
}

func TestClaudeVersionGate(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"2.1.275", "2.1.277", "2.1.276-beta", "claude 2.1.276", ""} {
		got, err := Plan(Request{Version: version, Mode: ModeSubscription})
		if err != nil {
			t.Errorf("version %q returned unexpected error: %v", version, err)
			continue
		}
		if got.Status != StatusUnsupported || len(got.Environment) != 0 || len(got.SkillCopies) != 0 || got.Reason == "" {
			t.Errorf("version %q did not produce an explicit unsupported result: %#v", version, got)
		}
	}
}
