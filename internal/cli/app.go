// Package cli wires the participant facing Anza command graph.
package cli

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	assets "github.com/sanifu-run/anza/catalog"
	"github.com/sanifu-run/anza/internal/adapters/claude"
	"github.com/sanifu-run/anza/internal/catalog"
	"github.com/sanifu-run/anza/internal/configedit"
	"github.com/sanifu-run/anza/internal/credentials"
	"github.com/sanifu-run/anza/internal/diagnostics"
	"github.com/sanifu-run/anza/internal/doctor"
	"github.com/sanifu-run/anza/internal/domain"
	"github.com/sanifu-run/anza/internal/download"
	"github.com/sanifu-run/anza/internal/executor"
	"github.com/sanifu-run/anza/internal/exercise"
	"github.com/sanifu-run/anza/internal/interviewclient"
	"github.com/sanifu-run/anza/internal/launch"
	"github.com/sanifu-run/anza/internal/lifecycle"
	"github.com/sanifu-run/anza/internal/planner"
	"github.com/sanifu-run/anza/internal/platform"
	"github.com/sanifu-run/anza/internal/state"
	"golang.org/x/term"
)

// Exit codes are stable across the executable and embedders.
const (
	ExitOK          = 0
	ExitUsage       = 2
	ExitInputClosed = 3
	ExitFailure     = 4
	ExitCanceled    = 130
)

// Command receives the active context and the three injected streams. Handlers
// must not read process globals; this keeps command execution scriptable.
type Command func(context.Context, []string, io.Reader, io.Writer, io.Writer) error

// App is a command graph with replaceable leaves for deterministic fixtures.
type App struct {
	In              io.Reader
	Out             io.Writer
	Err             io.Writer
	Version         string
	Commit          string
	BuildTime       string
	Interactive     bool
	Commands        map[string]Command
	Effects         executor.Effects
	RepairActions   lifecycle.RepairActions
	RemovalFS       lifecycle.FileSystem
	UpdateChecker   lifecycle.Checker
	UpdateSetupErr  error
	DownloadClient  *http.Client
	Chat            WizardChat
	CredentialStore credentials.Store
	SecretPrompt    func(io.Reader, io.Writer) (credentials.Secret, error)
	FindExecutable  func(string) (string, error)
	LaunchRunner    launch.Runner
	GuidedApproval  func(context.Context, string) (string, error)
	lastSetupName   string
}

// NewApp returns the public v1 command registry. Integrators can replace a
// leaf in Commands to supply state, catalog, network clients, or local fakes.
func NewApp(in io.Reader, out, errOut io.Writer, version string) *App {
	a := &App{In: in, Out: out, Err: errOut, Version: version, Commit: "unknown", BuildTime: "unknown", Commands: map[string]Command{}}
	a.DownloadClient = &http.Client{Timeout: 2 * time.Minute}
	a.Commands = map[string]Command{
		"bootstrap": a.bootstrapCommand, "inspect": inspectCommand,
		"setup": a.setupCommand, "interview": a.setupCommand,
		"plan": planCommand, "review": a.reviewCommand, "apply": a.applyCommand,
		"run": a.runAgentCommand, "repair": a.repairCommand,
		"doctor": doctorCommand, "update": a.updateCommand,
		"uninstall": a.uninstallCommand, "diagnostics": diagnosticsCommand,
		"exercise": exerciseCommand,
	}
	return a
}

// Register replaces a command leaf. It is used by the executable's service
// composition and by subprocess fixtures with local fake services.
func (a *App) Register(name string, command Command) error {
	if a == nil || a.Commands == nil {
		return errors.New("command registry is unavailable")
	}
	if _, ok := a.Commands[name]; !ok {
		return fmt.Errorf("unknown command %q", name)
	}
	if command == nil {
		return fmt.Errorf("command %q requires a handler", name)
	}
	a.Commands[name] = command
	return nil
}

func (a *App) bootstrapCommand(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer) error {
	if len(args) != 0 {
		return errors.New("bootstrap accepts no arguments in this version")
	}
	if !a.Interactive {
		return errors.New("an interactive terminal is required; run `anza setup` from a terminal")
	}
	return a.setupCommand(ctx, nil, in, out, errOut)
}

func (a *App) setupCommand(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer) error {
	if len(args) != 0 {
		return errors.New("setup accepts no arguments; import a reviewed brief with the setup brief workflow")
	}
	if !a.Interactive {
		return errors.New("setup requires interactive consent; rerun from a terminal. No credentials were read.")
	}
	store, err := state.OpenUserStore()
	if err != nil {
		return fmt.Errorf("opening private setup state: %w", err)
	}
	cat, err := catalog.LoadBundled()
	if err != nil {
		return err
	}
	snapshot, err := catalogSnapshot(cat)
	if err != nil {
		return err
	}
	var chat WizardChat = a.Chat
	if chat == nil {
		client, err := interviewclient.NewClient(&http.Client{Timeout: 45 * time.Second}, store, &snapshot)
		if err != nil {
			return err
		}
		chat = liveWizardChat{client: client}
	}
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	facts, err := platform.Inspect(ctx, root)
	if err != nil {
		return err
	}
	captured := &sessionNameCapture{reader: in}
	wizard, err := NewWizardWithChat(captured, out, chat, store, &facts)
	if err != nil {
		return err
	}
	if err := wizard.Run(ctx); err != nil {
		return err
	}
	name := strings.TrimSpace(captured.name)
	if !validWizardName(name) {
		return errors.New("interview finished without a valid local session name")
	}
	a.lastSetupName = name
	if wizard.recommendation == nil {
		return errors.New("interview finished without a typed recommendation")
	}
	var draft wizardDraft
	if err := store.Load("wizard-"+name, &draft); err != nil {
		return fmt.Errorf("load reviewed interview brief: %w", err)
	}
	brief := draft.Brief
	if brief == nil {
		brief = &domain.ProjectBrief{SchemaVersion: 1, ProjectSummary: draft.ProjectSummary, DesiredSlice: draft.DesiredSlice, Experience: draft.Experience, Constraints: []string{}, KnownStack: []string{}, ProjectKind: "general", ExistingProject: true}
	}
	if brief.Constraints == nil {
		brief.Constraints = []string{}
	}
	if brief.KnownStack == nil {
		brief.KnownStack = []string{}
	}
	if err := store.Save("setup-"+name, savedSetup{Brief: *brief, Recommendation: wizard.recommendation.Recommendation, Facts: facts}); err != nil {
		return fmt.Errorf("save private setup recommendation: %w", err)
	}
	fmt.Fprintf(out, "Interview and recommendation saved privately. Next: `anza plan --session %s`\n", name)
	return nil
}

type savedSetup struct {
	Brief          domain.ProjectBrief   `json:"brief"`
	Recommendation domain.Recommendation `json:"recommendation"`
	Facts          domain.MachineFacts   `json:"facts"`
}

// workspaceStateKey keeps the workspace hash within state.Store's 64-character
// name limit while retaining 224 bits of collision resistance.
func workspaceStateKey(prefix, workspaceID string) string {
	maxID := 64 - len(prefix) - 1
	if len(workspaceID) > maxID {
		workspaceID = workspaceID[:maxID]
	}
	return prefix + "-" + workspaceID
}

type sessionNameCapture struct {
	reader io.Reader
	name   string
	done   bool
}

func (r *sessionNameCapture) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if !r.done && n > 0 {
		line := string(p[:n])
		if at := strings.IndexByte(line, '\n'); at >= 0 {
			r.name = strings.TrimSpace(line[:at])
			r.done = true
		}
	}
	return n, err
}

func planCommand(ctx context.Context, args []string, _ io.Reader, out, _ io.Writer) error {
	args, jsonOutput := removeOption(args, "--json")
	flags, err := parseValueFlags(args, map[string]string{"--brief": "", "--recommendation": "", "--session": "", "--profile": "beginner"})
	if err != nil {
		return err
	}
	var brief domain.ProjectBrief
	var recommendation domain.Recommendation
	var store *state.Store
	if flags["--session"] != "" {
		if flags["--brief"] != "" || flags["--recommendation"] != "" {
			return errors.New("--session cannot be combined with --brief or --recommendation")
		}
		store, err = state.OpenUserStore()
		if err != nil {
			return err
		}
		var setup savedSetup
		if err := store.Load("setup-"+flags["--session"], &setup); err != nil {
			return fmt.Errorf("no saved setup session %q; run `anza setup` first", flags["--session"])
		}
		brief, recommendation = setup.Brief, setup.Recommendation
	} else {
		if flags["--brief"] == "" || flags["--recommendation"] == "" {
			return errors.New("usage: anza plan --session NAME | --brief FILE --recommendation FILE [--profile beginner|developer]")
		}
		if err := readStrictJSON(flags["--brief"], &brief); err != nil {
			return fmt.Errorf("reading brief: %w", err)
		}
		if err := readStrictJSON(flags["--recommendation"], &recommendation); err != nil {
			return fmt.Errorf("reading recommendation: %w", err)
		}
	}
	cat, err := catalog.LoadBundled()
	if err != nil {
		return err
	}
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	facts, err := platform.Inspect(ctx, root)
	if err != nil {
		return err
	}
	workspaceID, err := state.WorkspaceID(root)
	if err != nil {
		return err
	}
	var rawID [16]byte
	if _, err := rand.Read(rawID[:]); err != nil {
		return err
	}
	plan, err := planner.Build(planner.Input{ID: hex.EncodeToString(rawID[:]), WorkspaceID: workspaceID, Profile: flags["--profile"], Now: time.Now(), Facts: facts, Brief: brief, Recommendation: recommendation, Catalog: cat, Existing: planner.ExistingState{}})
	if err != nil {
		return err
	}
	if store == nil {
		store, err = state.OpenUserStore()
		if err != nil {
			return err
		}
	}
	if err := store.Save(workspaceStateKey("plan", workspaceID), plan); err != nil {
		return err
	}
	if jsonOutput {
		return json.NewEncoder(out).Encode(plan)
	}
	fmt.Fprintf(out, "Plan %s\nDigest: %s\nOperations: %d\nExpires: %s\n", plan.ID, plan.Digest, len(plan.Operations), plan.ExpiresAt)
	fmt.Fprintln(out, "Review the plan with `anza review` before applying it.")
	return nil
}

func (a *App) reviewCommand(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer) error {
	args, jsonOutput := removeOption(args, "--json")
	flags, err := parseValueFlags(args, map[string]string{"--approve-digest": ""})
	if err != nil {
		return err
	}
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	workspaceID, err := state.WorkspaceID(root)
	if err != nil {
		return err
	}
	store, err := state.OpenUserStore()
	if err != nil {
		return err
	}
	var plan domain.Plan
	if err := store.Load(workspaceStateKey("plan", workspaceID), &plan); err != nil {
		return fmt.Errorf("no current plan is available; run `anza plan` first: %w", err)
	}
	cat, err := catalog.LoadBundled()
	if err != nil {
		return err
	}
	recipes := map[string]domain.Recipe{}
	for _, operation := range plan.Operations {
		if operation.RecipeID == "" {
			continue
		}
		if recipe, ok := cat.Recipe(operation.RecipeID); ok {
			recipes[operation.RecipeID] = recipe
		}
	}
	approval, err := Review(ctx, plan, ReviewOptions{
		Input: in, Output: out, Diagnostics: errOut, Recipes: recipes,
		NonInteractive: !a.Interactive, ApproveDigest: flags["--approve-digest"],
		SaveApproval: func(approval domain.Approval) error {
			return store.Save(workspaceStateKey("approval", workspaceID), approval)
		},
	})
	if err != nil {
		return err
	}
	if jsonOutput {
		return json.NewEncoder(out).Encode(approval)
	}
	fmt.Fprintf(out, "Approval saved for plan digest %s.\n", approval.PlanDigest)
	return nil
}

func removeOption(args []string, option string) ([]string, bool) {
	filtered := make([]string, 0, len(args))
	found := false
	for _, arg := range args {
		if arg == option {
			found = true
			continue
		}
		filtered = append(filtered, arg)
	}
	return filtered, found
}

func parseValueFlags(args []string, defaults map[string]string) (map[string]string, error) {
	values := make(map[string]string, len(defaults))
	for name, value := range defaults {
		values[name] = value
	}
	for i := 0; i < len(args); i++ {
		name := args[i]
		if _, ok := values[name]; !ok {
			return nil, fmt.Errorf("unknown option %q", name)
		}
		if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
			return nil, fmt.Errorf("option %s requires a value", name)
		}
		i++
		values[name] = args[i]
	}
	return values, nil
}

func readStrictJSON(path string, dst any) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("input must contain exactly one JSON object")
	}
	return nil
}

func (a *App) runAgentCommand(ctx context.Context, args []string, in io.Reader, out, _ io.Writer) error {
	if len(args) < 5 || (args[0] != "codex" && args[0] != "claude") || args[1] != "--provider" || args[3] != "--version" {
		return errors.New("usage: anza run codex|claude --provider subscription|openrouter --version VERSION [--model PROVIDER/MODEL]")
	}
	agent := launch.Agent(args[0])
	provider := launch.Provider(args[2])
	version := args[4]
	model := ""
	for i := 5; i < len(args); i++ {
		if args[i] != "--model" || i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") || model != "" {
			return errors.New("usage: anza run codex|claude --provider subscription|openrouter --version VERSION [--model PROVIDER/MODEL]")
		}
		model = args[i+1]
		i++
	}
	if provider == launch.ProviderOpenRouter && model == "" {
		return errors.New("OpenRouter launch requires an explicit --model PROVIDER/MODEL")
	}
	if provider != launch.ProviderOpenRouter && provider != launch.ProviderSubscription {
		return errors.New("provider must be subscription or openrouter")
	}
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	choices := map[launch.ChoiceKey]launch.Provider{{Workspace: root, Agent: agent}: provider}
	var store credentials.Store
	var credentialID credentials.CredentialID
	credentialMissing := false
	if provider == launch.ProviderOpenRouter {
		store = a.CredentialStore
		if store == nil {
			var storeErr error
			store, storeErr = credentials.NewPlatformStore()
			if storeErr != nil {
				return errors.New("native credential storage is unavailable; enable the system keychain or use the provider's native subscription login")
			}
		}
		credentialID, err = credentials.NewCredentialID("openrouter", root)
		if err != nil {
			return errors.New("cannot scope the OpenRouter credential to this workspace")
		}
		stored, getErr := store.Get(credentialID)
		if getErr == nil {
			stored.Clear()
		} else if errors.Is(getErr, credentials.ErrNotFound) {
			credentialMissing = true
		} else {
			return errors.New("cannot access the workspace-scoped native credential")
		}
	}
	if credentialMissing && !a.Interactive {
		return errors.New("OpenRouter credential is missing; rerun interactively to approve and enter it through Anza's masked prompt")
	}
	findExecutable := a.FindExecutable
	if findExecutable == nil {
		findExecutable = exec.LookPath
	}
	program, err := findExecutable(string(agent))
	if err != nil {
		return fmt.Errorf("%s is not installed; install it through its official channel, then retry", agent)
	}
	if credentialMissing {
		fmt.Fprint(out, "Store an OpenRouter credential in the system keychain for this workspace? [yes/no]: ")
		answer, readErr := bufio.NewReader(in).ReadString('\n')
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return errors.New("could not read credential consent")
		}
		if !strings.EqualFold(strings.TrimSpace(answer), "yes") {
			return errors.New("credential was not stored; no launch occurred")
		}
		prompt := a.SecretPrompt
		if prompt == nil {
			prompt = maskedSecretPrompt
		}
		secret, promptErr := prompt(in, out)
		if promptErr != nil {
			return fmt.Errorf("read masked OpenRouter credential: %w", promptErr)
		}
		if len(secret) == 0 {
			secret.Clear()
			return errors.New("empty OpenRouter credential was not stored")
		}
		if err := store.Put(credentialID, secret); err != nil {
			secret.Clear()
			return errors.New("system credential storage failed; no launch occurred")
		}
		secret.Clear()
	}
	request := launch.Request{Workspace: root, Choices: choices, Agent: agent, Program: program, Dir: root, Model: model, Store: store, SessionEnv: func(string) (string, bool) { return "", false }, Runner: a.LaunchRunner}
	if agent == launch.AgentClaude {
		request.ClaudeVersion = version
		request.ClaudeAuth = claude.AuthNone
	} else {
		request.CodexVersion = version
	}
	result, err := launch.Launch(ctx, request)
	if err != nil {
		return err
	}
	if result.Instructions != "" {
		fmt.Fprintln(out, result.Instructions)
	}
	return nil
}

func maskedSecretPrompt(in io.Reader, out io.Writer) (credentials.Secret, error) {
	file, ok := in.(*os.File)
	if !ok || !term.IsTerminal(int(file.Fd())) {
		return nil, errors.New("masked credential input requires a real terminal")
	}
	if _, err := fmt.Fprint(out, "OpenRouter credential (input hidden): "); err != nil {
		return nil, err
	}
	value, err := term.ReadPassword(int(file.Fd()))
	_, writeErr := fmt.Fprintln(out)
	if err != nil {
		clear(value)
		return nil, err
	}
	if writeErr != nil {
		clear(value)
		return nil, writeErr
	}
	return credentials.NewSecret(value), nil
}

func diagnosticsCommand(ctx context.Context, args []string, _ io.Reader, out, _ io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(args) == 1 && args[0] == "--preview" {
		preview, err := diagnostics.PreviewRequest(diagnostics.Request{Destination: "private local file", Metadata: diagnostics.Metadata{Version: "1.0.0", Platform: runtimePlatform(), Status: "unknown", Operation: "readiness"}})
		if err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(preview)
	}
	if len(args) == 2 && args[0] == "--delete-local-session" {
		root, err := state.UserStateRoot()
		if err != nil {
			return err
		}
		if err := diagnostics.DeleteLocalSession(root, args[1]); err != nil {
			return err
		}
		fmt.Fprintln(out, "Local interview session deleted.")
		return nil
	}
	if len(args) == 2 && args[0] == "--export" {
		request := diagnostics.Request{Destination: args[1], Metadata: diagnostics.Metadata{Version: "1.0.0", Platform: runtimePlatform(), Status: "unknown", Operation: "readiness"}}
		if _, err := diagnostics.PreviewRequest(request); err != nil {
			return err
		}
		if err := diagnostics.Export(request); err != nil {
			return err
		}
		fmt.Fprintln(out, "Redacted local diagnostics exported.")
		return nil
	}
	return errors.New("usage: anza diagnostics --preview | --export FILE | --delete-local-session NAME")
}

func runtimePlatform() string {
	return runtime.GOOS + "-" + runtime.GOARCH
}

func (a *App) loadWorkspacePlan() (*state.Store, string, domain.Plan, error) {
	root, err := os.Getwd()
	if err != nil {
		return nil, "", domain.Plan{}, err
	}
	workspaceID, err := state.WorkspaceID(root)
	if err != nil {
		return nil, "", domain.Plan{}, err
	}
	store, err := state.OpenUserStore()
	if err != nil {
		return nil, "", domain.Plan{}, err
	}
	var plan domain.Plan
	if err := store.Load(workspaceStateKey("plan", workspaceID), &plan); err != nil {
		return nil, "", domain.Plan{}, fmt.Errorf("no current plan is available; run `anza plan` first: %w", err)
	}
	return store, workspaceID, plan, nil
}

func (a *App) applyCommand(ctx context.Context, args []string, _ io.Reader, out, _ io.Writer) error {
	args, jsonOutput := removeOption(args, "--json")
	if len(args) != 0 {
		return errors.New("apply accepts only --json")
	}
	store, workspaceID, plan, err := a.loadWorkspacePlan()
	if err != nil {
		return err
	}
	var approval domain.Approval
	if err := store.Load(workspaceStateKey("approval", workspaceID), &approval); err != nil {
		return errors.New("apply requires a saved approval for the current plan; run `anza review` first")
	}
	canonical, err := domain.CanonicalPlanDigest(plan)
	approvedAt, approvedErr := time.Parse(time.RFC3339Nano, approval.ApprovedAt)
	expiresAt, expiresErr := time.Parse(time.RFC3339Nano, plan.ExpiresAt)
	if err != nil || canonical == "" || approval.PlanDigest != canonical || approvedErr != nil || expiresErr != nil || approval.DisclosureVersion == "" || time.Now().After(expiresAt) || approvedAt.After(time.Now()) {
		return errors.New("apply requires a current digest-bound approval; run `anza review` again")
	}
	cat, err := catalog.LoadBundled()
	if err != nil {
		return err
	}
	payloads, err := a.downloadPayloads(ctx, plan, cat)
	if err != nil {
		return err
	}
	storeRoot, err := state.UserStateRoot()
	if err != nil {
		return err
	}
	if a.Effects == nil {
		root, err := os.Getwd()
		if err != nil {
			return err
		}
		a.Effects = &localEffects{workspace: root, stateRoot: storeRoot}
	}
	receipt, err := executor.Apply(ctx, plan, approval, executor.Options{Store: store, Effects: a.Effects, Payloads: payloads})
	if err != nil {
		return err
	}
	if err := store.Save(workspaceStateKey("receipt-current", workspaceID), receipt); err != nil {
		return err
	}
	if jsonOutput {
		return json.NewEncoder(out).Encode(receipt)
	}
	fmt.Fprintf(out, "Apply finished with %d operation records.\n", len(receipt.Operations))
	return nil
}

func (a *App) repairCommand(ctx context.Context, _ []string, _ io.Reader, out, _ io.Writer) error {
	store, workspaceID, plan, err := a.loadWorkspacePlan()
	if err != nil {
		return err
	}
	var approval domain.Approval
	if err := store.Load(workspaceStateKey("approval", workspaceID), &approval); err != nil {
		return errors.New("repair requires a current digest-bound approval; run `anza review` first")
	}
	actions := a.RepairActions
	if actions == nil {
		root, err := os.Getwd()
		if err != nil {
			return err
		}
		stateRoot, err := state.UserStateRoot()
		if err != nil {
			return err
		}
		actions = localRepair{files: &localEffects{workspace: root, stateRoot: stateRoot}}
	}
	result, err := lifecycle.Repair(ctx, plan, approval, actions)
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(result)
}

func (a *App) uninstallCommand(ctx context.Context, args []string, _ io.Reader, out, _ io.Writer) error {
	return a.uninstallWithArgs(ctx, args, out)
}

func (a *App) uninstallWithArgs(ctx context.Context, args []string, out io.Writer) error {
	previewOnly := len(args) == 1 && args[0] == "--preview"
	confirmed := len(args) == 2 && args[0] == "--confirm-digest"
	if !previewOnly && !confirmed {
		return errors.New("usage: anza uninstall --preview | --confirm-digest PLAN_DIGEST")
	}
	store, workspaceID, plan, err := a.loadWorkspacePlan()
	if err != nil {
		return err
	}
	var receipt domain.Receipt
	if err := store.Load(workspaceStateKey("receipt-current", workspaceID), &receipt); err != nil {
		return errors.New("no Anza-owned applied receipt is available; no files were changed")
	}
	if confirmed && args[1] != plan.Digest {
		return errors.New("confirmation digest does not match the current plan; no files were changed")
	}
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	stateRoot, err := state.UserStateRoot()
	if err != nil {
		return err
	}
	fs := a.RemovalFS
	if fs == nil {
		fs = &localEffects{workspace: root, stateRoot: stateRoot}
	}
	preview, err := lifecycle.PreviewRemove(ctx, receipt, plan, fs)
	if err != nil {
		return err
	}
	if previewOnly {
		for _, item := range preview.Items {
			fmt.Fprintf(out, "%s: remove=%t restore=%t conflict=%s\n", item.Path, item.Remove, item.Restore, item.Conflict)
		}
		fmt.Fprintf(out, "Preview only; no files changed. Use --confirm-digest %s to apply these removals.\n", plan.Digest)
		return nil
	}
	if err := lifecycle.ApplyRemove(ctx, preview, fs, true); err != nil {
		return err
	}
	fmt.Fprintf(out, "Removed %d Anza-owned items; unowned and conflicting items were preserved.\n", len(preview.Items))
	return nil
}

func (a *App) updateCommand(ctx context.Context, _ []string, _ io.Reader, out, _ io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if a.UpdateSetupErr != nil {
		return fmt.Errorf("configure release update verification: %w", a.UpdateSetupErr)
	}
	release, err := a.UpdateChecker.Check(ctx)
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(release)
}

// guidedCommand composes the default end-to-end flow. Approval remains a
// separate explicit typed action; stopping at any boundary reports readiness
// without implying that later effects or login were completed.
func (a *App) guidedCommand(ctx context.Context, in io.Reader, out, errOut io.Writer) error {
	if err := a.setupCommand(ctx, nil, in, out, errOut); err != nil {
		if errors.Is(err, ErrWizardEOF) {
			fmt.Fprintln(out, "Readiness: partial. Interview progress was saved; no plan was approved or applied.")
			return nil
		}
		return err
	}
	if !validWizardName(a.lastSetupName) {
		return errors.New("guided setup completed without a local session name")
	}
	var planOutput bytes.Buffer
	if err := planCommand(ctx, []string{"--session", a.lastSetupName, "--json"}, in, &planOutput, errOut); err != nil {
		return fmt.Errorf("plan reviewed setup: %w", err)
	}
	var plan domain.Plan
	if err := json.Unmarshal(planOutput.Bytes(), &plan); err != nil {
		return fmt.Errorf("decode generated plan: %w", err)
	}
	fmt.Fprintf(out, "Plan ready for review: %s (%d operations).\n", plan.Digest, len(plan.Operations))
	reviewInput := in
	if a.GuidedApproval != nil {
		approval, err := a.GuidedApproval(ctx, plan.Digest)
		if err != nil {
			return fmt.Errorf("collect synthetic guided review input: %w", err)
		}
		reviewInput = strings.NewReader(approval + "\n")
	}
	if err := a.reviewCommand(ctx, nil, reviewInput, out, errOut); err != nil {
		if errors.Is(err, ErrDeclined) || errors.Is(err, ErrWizardEOF) {
			fmt.Fprintln(out, "Readiness: partial. Plan remains unapplied because review approval was skipped or declined.")
			return nil
		}
		return err
	}
	var applyOutput bytes.Buffer
	if err := a.applyCommand(ctx, []string{"--json"}, in, &applyOutput, errOut); err != nil {
		fmt.Fprintf(out, "Readiness: partial. The plan was approved, but apply did not complete: %s\n", sanitizeError(err))
		return fmt.Errorf("apply reviewed plan: %w", err)
	}
	fmt.Fprint(out, "Approved plan applied. ")
	fmt.Fprintln(out, "Login guidance: Anza does not infer authentication. Use the agent's native subscription login, or select OpenRouter explicitly and store its key with Anza's masked prompt.")
	if err := doctorCommand(ctx, nil, in, out, errOut); err != nil {
		return fmt.Errorf("run readiness checks: %w", err)
	}
	store, err := state.OpenUserStore()
	if err != nil {
		return err
	}
	var setup savedSetup
	if err := store.Load("setup-"+a.lastSetupName, &setup); err != nil {
		return fmt.Errorf("load guided exercise selection: %w", err)
	}
	exerciseID := setup.Recommendation.SelectedExerciseID
	projectKind := setup.Brief.ProjectKind
	cat, err := catalog.LoadBundled()
	if err != nil {
		return err
	}
	exercise, ok := cat.Exercise(exerciseID)
	if !ok {
		return fmt.Errorf("selected exercise %q is not installed", exerciseID)
	}
	if !hasExerciseScenario(exercise, projectKind, setup.Facts.OS+"-"+setup.Facts.Arch) {
		projectKind = "unknown"
	}
	if err := exerciseCommand(ctx, []string{"--id", exerciseID, "--project-kind", projectKind}, in, out, errOut); err != nil {
		return fmt.Errorf("run guided local exercise: %w", err)
	}
	return nil
}

func hasExerciseScenario(descriptor domain.Exercise, projectKind, platform string) bool {
	for _, scenario := range descriptor.Scenarios {
		if scenario.ProjectKind != projectKind {
			continue
		}
		for _, supported := range scenario.SupportedPlatforms {
			if supported == platform {
				return true
			}
		}
	}
	return false
}

func (a *App) downloadPayloads(ctx context.Context, plan domain.Plan, cat *catalog.Catalog) (map[string]executor.Payload, error) {
	payloads := map[string]executor.Payload{}
	for _, op := range plan.Operations {
		if op.Kind != "install_artifact" {
			continue
		}
		recipe, ok := cat.Recipe(op.RecipeID)
		if !ok || recipe.Artifact == nil {
			return nil, fmt.Errorf("approved artifact metadata is missing for %s", op.ID)
		}
		parsed, err := url.Parse(recipe.Artifact.Origin)
		if err != nil || parsed.Hostname() == "" {
			return nil, fmt.Errorf("artifact origin for %s is invalid", op.ID)
		}
		stateRoot, err := state.UserStateRoot()
		if err != nil {
			return nil, err
		}
		cache := filepath.Join(stateRoot, "downloads")
		client := a.DownloadClient
		if client == nil {
			client = &http.Client{Timeout: 2 * time.Minute}
		}
		name, err := download.Fetch(ctx, client, download.Artifact{URL: recipe.Artifact.Origin, SHA256: recipe.Artifact.Digest, Size: recipe.Artifact.Size}, []string{parsed.Hostname()}, recipe.Artifact.Size, cache)
		if err != nil {
			return nil, fmt.Errorf("fetching verified artifact for %s: %w", op.ID, err)
		}
		data, err := os.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("reading verified artifact for %s: %w", op.ID, err)
		}
		payloads[op.ID] = executor.ArtifactPayload{Bytes: data}
	}
	return payloads, nil
}

// localEffects maps the planner's closed logical roots into a private state
// directory or the current workspace, rejects symlinked targets, and publishes
// writes by atomic rename after rechecking the reviewed preimage.
type localEffects struct{ workspace, stateRoot string }

func (e *localEffects) base(logical string) (string, string, error) {
	rootName, relative, found := strings.Cut(logical, "/")
	if !found {
		return "", "", errors.New("logical path has no relative file")
	}
	base := ""
	switch rootName {
	case "launch-profile":
		base = filepath.Join(e.stateRoot, "launch-profile")
	case "project-agents":
		base = filepath.Join(e.workspace, ".agents", "skills")
	case "project-claude":
		base = filepath.Join(e.workspace, ".claude", "skills")
	case "tool-cache":
		base = filepath.Join(e.stateRoot, "tool-cache")
	default:
		return "", "", fmt.Errorf("unsupported logical root %q", rootName)
	}
	if !safeLocalPath(relative) {
		return "", "", errors.New("unsafe logical relative path")
	}
	return base, filepath.FromSlash(relative), nil
}

func (e *localEffects) inspectPath(logical string, createParents bool) (string, error) {
	base, relative, err := e.base(logical)
	if err != nil {
		return "", err
	}
	if createParents {
		if err := os.MkdirAll(base, 0o700); err != nil {
			return "", err
		}
		if err := rejectSymlink(base); err != nil {
			return "", err
		}
		parent := filepath.Dir(filepath.Join(base, relative))
		if err := os.MkdirAll(parent, 0o700); err != nil {
			return "", err
		}
	}
	if _, err := os.Lstat(base); err != nil {
		if errors.Is(err, os.ErrNotExist) && !createParents {
			return filepath.Join(base, relative), nil
		}
		return "", err
	}
	if err := rejectSymlink(base); err != nil {
		return "", err
	}
	full := filepath.Join(base, relative)
	if err := rejectSymlinkAncestors(base, full); err != nil {
		return "", err
	}
	return full, nil
}

func (e *localEffects) Read(ctx context.Context, op domain.Operation) (executor.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return executor.Snapshot{}, err
	}
	full, err := e.inspectPath(filepath.ToSlash(filepath.Join(op.TargetRoot, op.RelativePath)), false)
	if err != nil {
		return executor.Snapshot{}, err
	}
	info, err := os.Lstat(full)
	if errors.Is(err, os.ErrNotExist) {
		return executor.Snapshot{}, nil
	}
	if err != nil {
		return executor.Snapshot{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return executor.Snapshot{Exists: true, Symlink: true, Mode: info.Mode()}, nil
	}
	if !info.Mode().IsRegular() {
		return executor.Snapshot{}, errors.New("target is not a regular file")
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return executor.Snapshot{}, err
	}
	return executor.Snapshot{Exists: true, Regular: true, Mode: info.Mode(), Bytes: data}, nil
}

func (e *localEffects) ApplyConfig(ctx context.Context, op domain.Operation, expected string, edit configedit.Edit) error {
	if hashBytes(edit.Bytes) != expected || edit.PostimageHash != expected {
		return errors.New("approved config output digest changed")
	}
	logical := filepath.ToSlash(filepath.Join(op.TargetRoot, op.RelativePath))
	full, err := e.inspectPath(logical, true)
	if err != nil {
		return err
	}
	current, err := readRegular(full)
	if err != nil {
		return err
	}
	if current.hash != edit.PreimageHash {
		return errors.New("config preimage changed since review")
	}
	return atomicReplace(full, edit.Bytes, edit.PreimageHash)
}

func (e *localEffects) PublishArtifact(ctx context.Context, op domain.Operation, expected string, artifact executor.ArtifactPayload) error {
	if hashBytes(artifact.Bytes) != expected {
		return errors.New("approved artifact digest changed")
	}
	logical := filepath.ToSlash(filepath.Join(op.TargetRoot, op.RelativePath))
	full, err := e.inspectPath(logical, true)
	if err != nil {
		return err
	}
	current, err := readRegular(full)
	if err != nil {
		return err
	}
	if current.hash != op.PreimageHash {
		return errors.New("artifact target changed since review")
	}
	return atomicReplace(full, artifact.Bytes, op.PreimageHash)
}

func (e *localEffects) ReadFile(ctx context.Context, logical string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	full, err := e.inspectPath(logical, false)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(full)
}
func (e *localEffects) Remove(ctx context.Context, logical string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	full, err := e.inspectPath(logical, false)
	if err != nil {
		return err
	}
	return os.Remove(full)
}
func (e *localEffects) WriteFile(ctx context.Context, logical string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	full, err := e.inspectPath(logical, true)
	if err != nil {
		return err
	}
	current, err := readRegular(full)
	if err != nil {
		return err
	}
	return atomicReplace(full, data, current.hash)
}
func (e *localEffects) Apply(context.Context, domain.Operation) error {
	return errors.New("repair has no resumable artifact payload; create a fresh plan and review")
}

type localRepair struct{ files *localEffects }

func (r localRepair) Read(ctx context.Context, op domain.Operation) ([]byte, error) {
	snapshot, err := r.files.Read(ctx, op)
	if err != nil {
		return nil, err
	}
	if !snapshot.Exists {
		return nil, nil
	}
	if snapshot.Symlink || !snapshot.Regular {
		return nil, errors.New("repair target is not a regular file")
	}
	return snapshot.Bytes, nil
}
func (r localRepair) Apply(ctx context.Context, op domain.Operation) error {
	return r.files.Apply(ctx, op)
}

type fileSnapshot struct{ hash string }

func readRegular(name string) (fileSnapshot, error) {
	info, err := os.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return fileSnapshot{hash: ""}, nil
	}
	if err != nil {
		return fileSnapshot{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fileSnapshot{}, errors.New("target is not a regular file")
	}
	data, err := os.ReadFile(name)
	if err != nil {
		return fileSnapshot{}, err
	}
	return fileSnapshot{hash: hashBytes(data)}, nil
}

func atomicReplace(name string, data []byte, expected string) error {
	current, err := readRegular(name)
	if err != nil {
		return err
	}
	if current.hash != expected {
		return errors.New("target changed at the write boundary")
	}
	dir := filepath.Dir(name)
	temp, err := os.CreateTemp(dir, ".anza-write-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	current, err = readRegular(name)
	if err != nil {
		return err
	}
	if current.hash != expected {
		return errors.New("target changed at the write boundary")
	}
	return os.Rename(tempName, name)
}

func rejectSymlink(name string) error {
	info, err := os.Lstat(name)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("logical root is not a plain directory")
	}
	return nil
}
func rejectSymlinkAncestors(base, full string) error {
	relative, err := filepath.Rel(base, full)
	if err != nil || !safeLocalPath(relative) {
		return errors.New("target escapes its logical root")
	}
	current := base
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("symlinked target paths are not allowed")
		}
	}
	return nil
}
func safeLocalPath(value string) bool {
	if value == "" || filepath.IsAbs(value) || filepath.VolumeName(value) != "" {
		return false
	}
	clean := filepath.Clean(value)
	return clean != "." && clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator))
}
func hashBytes(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func catalogSnapshot(cat *catalog.Catalog) (interviewclient.CatalogSnapshot, error) {
	snapshot := interviewclient.CatalogSnapshot{Version: cat.Version(), Digest: cat.Digest(), RecipeIDs: map[string]bool{}, PackIDs: map[string]bool{}, ExerciseIDs: map[string]bool{}}
	return snapshot, fs.WalkDir(assets.Assets, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || path.Ext(name) != ".json" {
			return nil
		}
		var item struct {
			ID string `json:"id"`
		}
		data, err := fs.ReadFile(assets.Assets, name)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, &item); err != nil {
			return fmt.Errorf("reading bundled catalog entry %s: %w", name, err)
		}
		if item.ID == "" {
			return nil
		}
		switch {
		case strings.HasPrefix(name, "recipes/"):
			snapshot.RecipeIDs[item.ID] = true
		case strings.HasPrefix(name, "packs/"):
			snapshot.PackIDs[item.ID] = true
		case strings.HasPrefix(name, "exercises/"):
			snapshot.ExerciseIDs[item.ID] = true
		}
		return nil
	})
}

func inspectCommand(ctx context.Context, args []string, _ io.Reader, out, _ io.Writer) error {
	jsonOutput, err := onlyJSONFlag(args)
	if err != nil {
		return err
	}
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	facts, err := platform.Inspect(ctx, root)
	if err != nil {
		return err
	}
	if jsonOutput {
		return json.NewEncoder(out).Encode(facts)
	}
	fmt.Fprintf(out, "OS: %s\nArchitecture: %s\nShell: %s\n", facts.OS, facts.Arch, facts.ShellKind)
	fmt.Fprintln(out, "Capability probes are local observations and do not verify provider login or live requests.")
	return nil
}

func doctorCommand(ctx context.Context, args []string, _ io.Reader, out, _ io.Writer) error {
	jsonOutput, err := onlyJSONFlag(args)
	if err != nil {
		return err
	}
	if len(args) > 0 && args[0] == "--live" {
		return errors.New("live checks require an explicit interactive confirmation and are not enabled in this command path")
	}
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	localChecks := []doctor.CheckSpec{
		{ID: "workspace", Required: true, Run: func(ctx context.Context, path string) (domain.CheckResult, error) {
			info, err := os.Stat(path)
			if err != nil || !info.IsDir() {
				return domain.CheckResult{ID: "workspace", Status: "action_required", Summary: "The selected workspace is not an accessible directory.", NextAction: "Choose an accessible project directory."}, nil
			}
			return domain.CheckResult{ID: "workspace", Status: "ready", Summary: "The selected workspace is accessible."}, nil
		}},
		{ID: "platform", Required: true, Run: func(ctx context.Context, path string) (domain.CheckResult, error) {
			facts, err := platform.Inspect(ctx, path)
			if err != nil {
				return domain.CheckResult{}, err
			}
			status := "ready"
			if facts.OS == "" || facts.Arch == "" {
				status = "unknown"
			}
			return domain.CheckResult{ID: "platform", Status: status, Summary: fmt.Sprintf("Detected %s/%s; local capability probes are recorded independently.", facts.OS, facts.Arch)}, nil
		}},
		{ID: "catalog", Required: true, Run: func(context.Context, string) (domain.CheckResult, error) {
			cat, err := catalog.LoadBundled()
			if err != nil {
				return domain.CheckResult{}, err
			}
			return domain.CheckResult{ID: "catalog", Status: "ready", Summary: fmt.Sprintf("Bundled catalog %s is valid.", cat.Version())}, nil
		}},
		{ID: "provider_login", Required: true, Run: func(context.Context, string) (domain.CheckResult, error) {
			return domain.CheckResult{ID: "provider_login", Status: "manual", Summary: "Provider login status is not inferred from local fixtures.", NextAction: "Use the selected provider's native login flow and run a real request when ready."}, nil
		}},
	}
	results, err := doctor.Check(ctx, root, doctor.Options{LocalChecks: localChecks})
	if err != nil {
		return err
	}
	if jsonOutput {
		return json.NewEncoder(out).Encode(map[string]any{"status": doctor.Aggregate(results), "checks": results})
	}
	for _, result := range results {
		fmt.Fprintf(out, "%s: %s — %s\n", result.ID, result.Status, result.Summary)
		if result.NextAction != "" {
			fmt.Fprintf(out, "  Next: %s\n", result.NextAction)
		}
	}
	fmt.Fprintf(out, "Overall status: %s\n", doctor.Aggregate(results))
	return nil
}

func exerciseCommand(ctx context.Context, args []string, _ io.Reader, out, _ io.Writer) error {
	if len(args) < 3 || args[0] != "--id" || args[2] != "--project-kind" || len(args) < 4 {
		return errors.New("usage: anza exercise --id ID --project-kind KIND [--json]")
	}
	id, projectKind := args[1], args[3]
	jsonOutput := false
	if len(args) > 4 {
		if len(args) != 5 || args[4] != "--json" {
			return errors.New("exercise accepts only --json after --project-kind")
		}
		jsonOutput = true
	}
	cat, err := catalog.LoadBundled()
	if err != nil {
		return err
	}
	descriptor, ok := cat.Exercise(id)
	if !ok {
		return fmt.Errorf("exercise %q is not in the installed catalog", id)
	}
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	facts, err := platform.Inspect(ctx, root)
	if err != nil {
		return err
	}
	result, err := exercise.Run(ctx, descriptor, projectKind, facts.OS)
	if err != nil {
		return err
	}
	if jsonOutput {
		return json.NewEncoder(out).Encode(result)
	}
	fmt.Fprintf(out, "%s: %s\n", result.ExerciseID, result.Status)
	if result.Summary != "" {
		fmt.Fprintln(out, result.Summary)
	}
	for _, step := range result.ManualSteps {
		fmt.Fprintf(out, "- %s\n", step)
	}
	return nil
}

func onlyJSONFlag(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	if len(args) == 1 && args[0] == "--json" {
		return true, nil
	}
	return false, errors.New("command accepts only the --json option")
}

// Run dispatches one command. An empty argument list and help are side-effect
// free; unknown commands and malformed command arguments return ExitUsage.
func (a *App) Run(ctx context.Context, args []string) int {
	if a == nil || a.In == nil || a.Out == nil || a.Err == nil {
		return ExitFailure
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		fmt.Fprintf(a.Err, "anza: canceled: %v\n", err)
		return ExitCanceled
	}
	if len(args) == 0 {
		if a.Interactive {
			err := a.guidedCommand(ctx, a.In, a.Out, a.Err)
			return a.commandStatus("setup", err)
		}
		a.usage(a.Out)
		return ExitOK
	}
	if helpFlag(args[0]) {
		if len(args) != 1 {
			return a.usageError("help accepts no arguments")
		}
		a.usage(a.Out)
		return ExitOK
	}
	if args[0] == "version" || args[0] == "--version" {
		if len(args) != 1 {
			return a.usageError("version accepts no arguments")
		}
		fmt.Fprintf(a.Out, "Anza %s\nCommit: %s\nBuilt: %s\n", a.Version, a.Commit, a.BuildTime)
		return ExitOK
	}
	command := args[0]
	if command == "help" {
		if len(args) != 1 {
			return a.usageError("help accepts no arguments")
		}
		a.usage(a.Out)
		return ExitOK
	}
	return a.dispatch(ctx, command, args[1:])
}

func (a *App) dispatch(ctx context.Context, command string, args []string) int {
	h, ok := a.Commands[command]
	if !ok {
		return a.usageError("unknown command " + command)
	}
	if h == nil {
		return a.usageError("command has no handler: " + command)
	}
	err := h(ctx, args, a.In, a.Out, a.Err)
	return a.commandStatus(command, err)
}

func (a *App) commandStatus(command string, err error) int {
	if err == nil {
		return ExitOK
	}
	if errors.Is(err, context.Canceled) {
		return ExitCanceled
	}
	if errors.Is(err, ErrWizardEOF) {
		return ExitInputClosed
	}
	if errors.Is(err, flag.ErrHelp) {
		return ExitOK
	}
	fmt.Fprintf(a.Err, "anza %s: %s\n", command, sanitizeError(err))
	return ExitFailure
}

func (a *App) usageError(message string) int {
	fmt.Fprintf(a.Err, "anza: %s\n", message)
	a.usage(a.Err)
	return ExitUsage
}
func (a *App) usage(w io.Writer) {
	fmt.Fprintln(w, "Usage: anza <command> [options]")
	fmt.Fprintln(w, "Commands:")
	for _, item := range []string{"bootstrap  verify and install the Anza runtime", "inspect    inspect the current workspace", "setup      interview, plan, review, apply, then guide login and checks", "interview  continue a saved setup interview", "plan       create a local setup plan", "review     disclose effects and approve the exact plan digest", "apply      apply an approved plan", "run        launch codex or claude", "repair     recover an interrupted apply", "doctor     check workspace readiness", "update     plan or apply a safe update", "uninstall  remove Anza-owned changes", "diagnostics export or delete local diagnostics", "exercise   run a local project exercise", "help       show this usage", "version    print version information"} {
		fmt.Fprintf(w, "  %s\n", item)
	}
}
func helpFlag(s string) bool { return s == "-h" || s == "--help" }
func sanitizeError(err error) string {
	return strings.ReplaceAll(strings.TrimSpace(err.Error()), "\n", " ")
}
