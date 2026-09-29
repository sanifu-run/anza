package launch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sanifu-run/anza/internal/adapters/claude"
	"github.com/sanifu-run/anza/internal/credentials"
	"github.com/sanifu-run/anza/internal/process"
)

func TestMixedAgentProviders(t *testing.T) {
	choices := map[ChoiceKey]Provider{
		{Workspace: "workspace-a", Agent: AgentCodex}:  ProviderOpenRouter,
		{Workspace: "workspace-a", Agent: AgentClaude}: ProviderSubscription,
		{Workspace: "workspace-b", Agent: AgentCodex}:  ProviderSubscription,
		{Workspace: "workspace-b", Agent: AgentClaude}: ProviderOpenRouter,
	}
	for key, want := range map[ChoiceKey]Provider{
		{Workspace: "workspace-a", Agent: AgentCodex}:  ProviderOpenRouter,
		{Workspace: "workspace-a", Agent: AgentClaude}: ProviderSubscription,
		{Workspace: "workspace-b", Agent: AgentCodex}:  ProviderSubscription,
		{Workspace: "workspace-b", Agent: AgentClaude}: ProviderOpenRouter,
	} {
		got, err := ResolveProvider(choices, key.Workspace, key.Agent)
		if err != nil || got != want {
			t.Fatalf("ResolveProvider(%+v) = %q, %v; want %q", key, got, err, want)
		}
	}
	for key, provider := range choices {
		t.Run(string(key.Agent)+"/"+key.Workspace, func(t *testing.T) {
			called := false
			req := Request{
				Workspace: key.Workspace, Choices: choices, Agent: key.Agent,
				Program: "synthetic-agent", Store: credentials.NewUnavailableStore(),
				CodexVersion: "0.157.1", CodexHome: t.TempDir(), ClaudeVersion: "2.1.276",
				ClaudeAuth: claude.AuthNone, Model: "anthropic/claude-sonnet-4",
				SessionEnv: func(string) (string, bool) { return "synthetic-session-key", true },
				Runner: func(_ context.Context, spec process.Spec) (process.Result, error) {
					called = true
					wantSecrets := 0
					if provider == ProviderOpenRouter {
						wantSecrets = 2
						if key.Agent == AgentClaude {
							wantSecrets = 3
						}
					}
					if got := spec.SensitiveEnv.String(); got != fmt.Sprintf("SensitiveEnv{value_count:%d}", wantSecrets) {
						t.Fatalf("%s/%s sensitive child environment = %s", key.Agent, provider, got)
					}
					return process.Result{}, nil
				},
			}
			result, err := Launch(context.Background(), req)
			if err != nil || !called || result.Provider != provider || result.Agent != key.Agent || result.Authenticated {
				t.Fatalf("launch(%+v) = result %+v, called %v, err %v", key, result, called, err)
			}
		})
	}
}

func TestSecretChildEnvironment(t *testing.T) {
	const secret = "synthetic-openrouter-key"
	parentEnv := snapshotLaunchEnvironment()
	defer assertParentEnvironmentUnchanged(t, parentEnv)
	store, _ := fixtureStore(t, "workspace-a", secret)
	var childArgs []string
	fixtureRunner := func(ctx context.Context, spec process.Spec) (process.Result, error) {
		if len(spec.Args) < 2 || !reflect.DeepEqual(spec.Args[:2], []string{"--model", "anthropic/claude-sonnet-4"}) {
			t.Fatalf("Claude model arguments = %#v", spec.Args)
		}
		childArgs = append([]string(nil), spec.Args...)
		spec.Args = append([]string(nil), spec.Args[2:]...)
		spec.Timeout = 5 * time.Second
		return process.Run(ctx, spec)
	}
	forwarded := []string{"-test.run=TestLaunchChildFixture", "-test.v", "--", "anza-claude-fixture", "literal;$(touch nope)", "--api-key", "synthetic-user-token"}
	result, err := Launch(context.Background(), Request{
		Workspace: "workspace-a", Agent: AgentClaude, Choices: choices(AgentClaude, ProviderOpenRouter),
		Program: os.Args[0], Dir: t.TempDir(), Args: forwarded,
		Model: "anthropic/claude-sonnet-4", ClaudeVersion: "2.1.276", ClaudeAuth: claude.AuthNone,
		Store: store, Runner: fixtureRunner,
	})
	if err != nil {
		t.Fatalf("%v; child stderr: %s", err, result.Execution.Stderr)
	}
	if !strings.Contains(result.Execution.Stdout, "AUTH_MATCH=1") || !strings.Contains(result.Execution.Stdout, "BASE_MATCH=1") {
		t.Fatalf("child did not receive Claude OpenRouter environment: execution=%+v args=%q", result.Execution, childArgs)
	}
	if !strings.Contains(result.Execution.Stdout, "OPENROUTER_KEY_ABSENT=1") {
		t.Fatal("Claude child received an unrelated OpenRouter environment key")
	}
	if !strings.Contains(result.Execution.Stdout, "API_EMPTY=1") || !strings.Contains(result.Execution.Stdout, "ARG=literal;$(touch nope)") || !strings.Contains(result.Execution.Stdout, "ARG_TOKEN_MATCH=1") {
		t.Fatalf("child environment or argument forwarding mismatch: %q", result.Execution.Stdout)
	}
	if !reflect.DeepEqual(childArgs[2:], forwarded) || result.ArgumentCount != len(childArgs) {
		t.Fatal("child argument vector was not forwarded intact")
	}
	if strings.Contains(strings.Join(childArgs, " "), secret) {
		t.Fatal("provider key appeared in argv")
	}
	encoded, err := json.Marshal(result)
	if err != nil || strings.Contains(string(encoded), secret) || strings.Contains(string(encoded), "synthetic-user-token") {
		t.Fatal("provider key or caller argument appeared in launch receipt")
	}
}

func TestCodexOpenRouterChildEnvironment(t *testing.T) {
	parentEnv := snapshotLaunchEnvironment()
	defer assertParentEnvironmentUnchanged(t, parentEnv)
	store, _ := fixtureStore(t, "workspace-codex", "synthetic-codex-key")
	home := t.TempDir()
	var childArgs []string
	result, err := Launch(context.Background(), Request{
		Workspace: "workspace-codex", Agent: AgentCodex, Choices: choicesFor("workspace-codex", AgentCodex, ProviderOpenRouter),
		Program: os.Args[0], Dir: t.TempDir(), Args: []string{"-test.run=TestCodexLaunchChildFixture", "-test.v", "--", "anza-codex-fixture", home},
		Model: "openai/gpt-5", CodexVersion: "0.157.1", CodexHome: home,
		Store: store, Runner: func(ctx context.Context, spec process.Spec) (process.Result, error) {
			childArgs = append([]string(nil), spec.Args...)
			spec.Timeout = 5 * time.Second
			return process.Run(ctx, spec)
		},
	})
	if err != nil {
		t.Fatalf("%v; child stderr: %s", err, result.Execution.Stderr)
	}
	if !strings.Contains(result.Execution.Stdout, "CODEX_MATCH=1") {
		t.Fatalf("Codex child did not receive its workspace OpenRouter environment: %+v", result.Execution)
	}
	if strings.Contains(strings.Join(childArgs, " "), "synthetic-codex-key") {
		t.Fatal("OpenRouter key appeared in Codex arguments")
	}
	encoded, err := json.Marshal(result)
	if err != nil || strings.Contains(string(encoded), "synthetic-codex-key") {
		t.Fatal("OpenRouter key appeared in Codex launch receipt")
	}
}

func TestCodexLaunchChildFixture(t *testing.T) {
	var fixtureMode, expectedHome string
	for i, arg := range os.Args {
		if arg == "--" && i+2 < len(os.Args) {
			fixtureMode, expectedHome = os.Args[i+1], os.Args[i+2]
			break
		}
	}
	if fixtureMode != "anza-codex-fixture" {
		return
	}
	key := os.Getenv("OPENROUTER_API_KEY")
	if key != "synthetic-codex-key" {
		t.Fatal("unexpected Codex fixture credential")
	}
	if expectedHome == "" || os.Getenv("CODEX_HOME") != expectedHome {
		t.Fatal("Codex did not receive its selected workspace launch profile")
	}
	_, _ = os.Stdout.WriteString("CODEX_MATCH=1\n")
}

func TestLaunchChildFixture(t *testing.T) {
	fixtureMode := false
	for i, arg := range os.Args {
		if arg == "--" && i+1 < len(os.Args) && os.Args[i+1] == "anza-claude-fixture" {
			fixtureMode = true
			break
		}
	}
	if !fixtureMode {
		return
	}
	if os.Getenv("ANTHROPIC_AUTH_TOKEN") != "synthetic-openrouter-key" {
		t.Fatal("unexpected synthetic auth token")
	}
	if os.Getenv("ANTHROPIC_BASE_URL") != "https://openrouter.ai/api" || os.Getenv("ANTHROPIC_API_KEY") != "" {
		t.Fatal("unexpected synthetic provider environment")
	}
	_, _ = os.Stdout.WriteString("AUTH_MATCH=1\n")
	_, _ = os.Stdout.WriteString("BASE_MATCH=1\n")
	_, _ = os.Stdout.WriteString("API_EMPTY=1\n")
	if _, exists := os.LookupEnv("OPENROUTER_API_KEY"); !exists {
		_, _ = os.Stdout.WriteString("OPENROUTER_KEY_ABSENT=1\n")
	}
	for i, arg := range os.Args {
		if arg == "literal;$(touch nope)" {
			_, _ = os.Stdout.WriteString("ARG=" + arg + "\n")
		}
		if arg == "--api-key" && i+1 < len(os.Args) && os.Args[i+1] == "synthetic-user-token" {
			_, _ = os.Stdout.WriteString("ARG_TOKEN_MATCH=1\n")
		}
	}
}

func TestNativeLoginCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	called := false
	_, err := Launch(ctx, Request{
		Workspace: "workspace-a", Agent: AgentClaude, Choices: choices(AgentClaude, ProviderSubscription),
		Program: "fixture", ClaudeVersion: "2.1.276", Runner: func(context.Context, process.Spec) (process.Result, error) {
			called = true
			cancel()
			return process.Result{}, context.Canceled
		},
	})
	if !called || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled native login launch = called %v, err %v", called, err)
	}
}

func TestAgentArgumentForwarding(t *testing.T) {
	want := []string{"-p", "literal;$(echo safe)", "--", "value"}
	var got process.Spec
	launchResult, err := Launch(context.Background(), Request{
		Workspace: "workspace-a", Agent: AgentClaude, Choices: choices(AgentClaude, ProviderSubscription),
		Program: "fixture", ClaudeVersion: "2.1.276", Args: want,
		Runner: func(_ context.Context, spec process.Spec) (process.Result, error) {
			got = spec
			return process.Result{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Args, want) {
		t.Fatalf("args = %#v; want %#v", got.Args, want)
	}
	if launchResult.ArgumentCount != len(want) {
		t.Fatalf("argument count = %d; want %d", launchResult.ArgumentCount, len(want))
	}
}

func TestOpenRouterMissingCredentialAndClaudeConflict(t *testing.T) {
	_, err := Launch(context.Background(), Request{
		Workspace: "workspace-a", Agent: AgentCodex, Choices: choices(AgentCodex, ProviderOpenRouter),
		Program: "fixture", CodexVersion: "0.157.1", CodexHome: t.TempDir(),
		CodexProfileConfig: []byte(""), Store: credentials.NewUnavailableStore(),
		Runner: func(context.Context, process.Spec) (process.Result, error) {
			t.Fatal("runner called without key")
			return process.Result{}, nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "OpenRouter") {
		t.Fatalf("missing credential error = %v", err)
	}
	_, err = Launch(context.Background(), Request{
		Workspace: "workspace-a", Agent: AgentClaude, Choices: choices(AgentClaude, ProviderOpenRouter),
		Program: "fixture", Model: "anthropic/claude-sonnet-4", ClaudeVersion: "2.1.276",
		ClaudeAuth: claude.AuthSubscription, SessionEnv: func(string) (string, bool) { return "fake-key", true },
		Runner: func(context.Context, process.Spec) (process.Result, error) {
			t.Fatal("runner called during subscription conflict")
			return process.Result{}, nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("Claude auth conflict error = %v", err)
	}
}

func choices(agent Agent, provider Provider) map[ChoiceKey]Provider {
	return map[ChoiceKey]Provider{{Workspace: "workspace-a", Agent: agent}: provider}
}

func choicesFor(workspace string, agent Agent, provider Provider) map[ChoiceKey]Provider {
	return map[ChoiceKey]Provider{{Workspace: workspace, Agent: agent}: provider}
}

func snapshotLaunchEnvironment() map[string]string {
	values := make(map[string]string)
	for _, key := range []string{"OPENROUTER_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_API_KEY", "ANTHROPIC_BASE_URL", "CODEX_HOME"} {
		if value, exists := os.LookupEnv(key); exists {
			values[key] = value
		}
	}
	return values
}

func assertParentEnvironmentUnchanged(t *testing.T, before map[string]string) {
	t.Helper()
	if !reflect.DeepEqual(snapshotLaunchEnvironment(), before) {
		t.Error("parent environment changed during launch")
	}
}

type fixtureCredentialStore struct {
	values map[credentials.CredentialID]credentials.Secret
}

func (s *fixtureCredentialStore) Put(id credentials.CredentialID, secret credentials.Secret) error {
	s.values[id] = credentials.NewSecret(secret.Bytes())
	return nil
}
func (s *fixtureCredentialStore) Get(id credentials.CredentialID) (credentials.Secret, error) {
	value, ok := s.values[id]
	if !ok {
		return nil, credentials.ErrNotFound
	}
	return credentials.NewSecret(value.Bytes()), nil
}
func (s *fixtureCredentialStore) Delete(id credentials.CredentialID) error {
	delete(s.values, id)
	return nil
}

func fixtureStore(t *testing.T, workspace, secret string) (*fixtureCredentialStore, credentials.CredentialID) {
	t.Helper()
	id, err := credentials.NewCredentialID("openrouter", workspace)
	if err != nil {
		t.Fatal(err)
	}
	return &fixtureCredentialStore{values: map[credentials.CredentialID]credentials.Secret{
		id: credentials.NewSecret([]byte(secret)),
	}}, id
}
