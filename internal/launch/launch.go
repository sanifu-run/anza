// Package launch resolves each workspace's agent provider choice and builds a
// child-only environment for an Anza-managed OpenRouter launch.
package launch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/sanifu-run/anza/internal/adapters/claude"
	"github.com/sanifu-run/anza/internal/adapters/codex"
	"github.com/sanifu-run/anza/internal/credentials"
	"github.com/sanifu-run/anza/internal/process"
)

type Agent string
type Provider string

const (
	AgentCodex  Agent = "codex"
	AgentClaude Agent = "claude"

	ProviderSubscription Provider = "subscription"
	ProviderOpenRouter   Provider = "openrouter"
)

// ChoiceKey scopes a provider choice to both a project workspace and agent.
type ChoiceKey struct {
	Workspace string
	Agent     Agent
}

var (
	ErrMissingChoice     = errors.New("provider choice is missing")
	ErrMissingCredential = errors.New("OpenRouter credential is unavailable")
)

type Runner func(context.Context, process.Spec) (process.Result, error)
type EnvLookup func(string) (string, bool)

type Request struct {
	Workspace string
	Choices   map[ChoiceKey]Provider
	Agent     Agent
	Program   string
	Args      []string
	Dir       string
	Model     string

	// CodexHome points at the workspace's already prepared Anza launch profile.
	// It is used only for OpenRouter, keeping native subscription state intact.
	CodexHome          string
	CodexVersion       string
	CodexProfileConfig []byte

	ClaudeVersion  string
	ClaudeAuth     claude.AuthState
	ClaudeSettings []byte

	Store      credentials.Store
	SessionEnv EnvLookup
	Runner     Runner
}

// Result reports only nonsensitive launch selection and bounded process output.
// Authentication is never inferred from exit status or launcher completion.
type Result struct {
	Agent         Agent          `json:"agent"`
	Provider      Provider       `json:"provider"`
	Args          []string       `json:"args"`
	Execution     process.Result `json:"execution"`
	Authenticated bool           `json:"authenticated"`
	Instructions  string         `json:"instructions,omitempty"`
}

// ResolveProvider resolves an explicit agent/workspace entry; it never applies
// another agent's or workspace's choice as a default.
func ResolveProvider(choices map[ChoiceKey]Provider, workspace string, agent Agent) (Provider, error) {
	if strings.TrimSpace(workspace) == "" || (agent != AgentClaude && agent != AgentCodex) {
		return "", fmt.Errorf("%w: invalid workspace or agent", ErrMissingChoice)
	}
	provider, ok := choices[ChoiceKey{Workspace: workspace, Agent: agent}]
	if !ok || (provider != ProviderSubscription && provider != ProviderOpenRouter) {
		return "", fmt.Errorf("%w for %s in this workspace", ErrMissingChoice, agent)
	}
	return provider, nil
}

// Launch invokes the selected vendor executable directly, without a shell.
// OpenRouter secrets are resolved at call time and attached as sensitive child
// environment values. Native vendor credentials are never read or forwarded.
func Launch(ctx context.Context, request Request) (Result, error) {
	result := Result{Agent: request.Agent, Authenticated: false}
	provider, err := ResolveProvider(request.Choices, request.Workspace, request.Agent)
	if err != nil {
		return result, err
	}
	result.Provider = provider
	if request.Program == "" {
		return result, fmt.Errorf("agent executable is required")
	}
	runner := request.Runner
	if runner == nil {
		runner = process.Run
	}
	spec := process.Spec{Program: request.Program, Args: append([]string(nil), request.Args...), Dir: request.Dir}

	switch request.Agent {
	case AgentClaude:
		settings := request.ClaudeSettings
		if len(settings) == 0 {
			settings = []byte("{}")
		}
		plan, planErr := claude.Plan(claude.Request{
			Version: request.ClaudeVersion, Mode: claude.Mode(provider), Model: request.Model,
			Settings: settings, AuthState: request.ClaudeAuth,
		})
		if planErr != nil {
			return result, fmt.Errorf("planning Claude launch: %w", planErr)
		}
		if plan.Status != claude.StatusPlanned {
			return result, fmt.Errorf("Claude launch requires review: %s", plan.Reason)
		}
		if provider == ProviderSubscription {
			result.Instructions = "Use Claude Code's interactive /login flow if sign-in is needed; Anza does not read or export its native session."
		} else {
			key, keyErr := lookupOpenRouterKey(request, AgentClaude)
			if keyErr != nil {
				return result, keyErr
			}
			defer clear(key)
			sensitive, envErr := process.NewSensitiveEnv(map[string]string{
				"ANTHROPIC_AUTH_TOKEN": string(key),
				"ANTHROPIC_BASE_URL":   "https://openrouter.ai/api",
				"ANTHROPIC_API_KEY":    "",
			})
			if envErr != nil {
				return result, fmt.Errorf("preparing Claude child environment: %w", envErr)
			}
			spec.SensitiveEnv = sensitive
			spec.Args = append(append([]string(nil), plan.LaunchArgs...), spec.Args...)
		}
	case AgentCodex:
		plan, planErr := codex.Plan(codex.Request{
			Version: request.CodexVersion, Mode: codex.Mode(provider), Model: request.Model,
			AuthShell: codexShell(), ProfileConfig: request.CodexProfileConfig,
		})
		if planErr != nil {
			return result, fmt.Errorf("planning Codex launch: %w", planErr)
		}
		if plan.Status != codex.StatusPlanned {
			return result, fmt.Errorf("Codex launch requires review: %s", plan.Reason)
		}
		if provider == ProviderSubscription {
			result.Instructions = "Use Codex's interactive ChatGPT sign-in or `codex login` if sign-in is needed; Anza does not read or export its native session."
		} else {
			if strings.TrimSpace(request.CodexHome) == "" {
				return result, fmt.Errorf("Codex OpenRouter launch requires the prepared workspace launch profile")
			}
			key, keyErr := lookupOpenRouterKey(request, AgentCodex)
			if keyErr != nil {
				return result, keyErr
			}
			defer clear(key)
			sensitive, envErr := process.NewSensitiveEnv(map[string]string{
				"OPENROUTER_API_KEY": string(key),
				"CODEX_HOME":         request.CodexHome,
			})
			if envErr != nil {
				return result, fmt.Errorf("preparing Codex child environment: %w", envErr)
			}
			spec.SensitiveEnv = sensitive
		}
	default:
		return result, fmt.Errorf("unsupported agent %q", request.Agent)
	}

	result.Args = append([]string(nil), spec.Args...)
	result.Execution, err = runner(ctx, spec)
	if err != nil {
		if provider == ProviderSubscription && errors.Is(err, context.Canceled) {
			return result, fmt.Errorf("native login or agent launch was canceled; no authentication status was recorded: %w", err)
		}
		return result, err
	}
	return result, nil
}

func lookupOpenRouterKey(request Request, agent Agent) ([]byte, error) {
	id, err := credentials.NewCredentialID("openrouter", request.Workspace)
	if err != nil {
		return nil, fmt.Errorf("invalid workspace credential scope")
	}
	if request.Store != nil {
		secret, getErr := request.Store.Get(id)
		if getErr == nil {
			value := secret.Bytes()
			secret.Clear()
			if len(value) != 0 {
				return value, nil
			}
			clear(value)
		} else if !errors.Is(getErr, credentials.ErrNotFound) && !errors.Is(getErr, credentials.ErrUnavailable) {
			return nil, fmt.Errorf("reading OpenRouter credential failed")
		}
	}
	lookup := request.SessionEnv
	if lookup == nil {
		lookup = os.LookupEnv
	}
	value, ok := lookup("OPENROUTER_API_KEY")
	if ok && strings.TrimSpace(value) != "" {
		return []byte(value), nil
	}
	return nil, fmt.Errorf("%w; store it for this workspace or provide OPENROUTER_API_KEY for this session", ErrMissingCredential)
}

func codexShell() codex.AuthShell {
	if os.PathSeparator == '\\' {
		return codex.ShellPowerShell
	}
	return codex.ShellPOSIX
}
