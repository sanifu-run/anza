package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sanifu-run/anza/internal/catalog"
	"github.com/sanifu-run/anza/internal/credentials"
	"github.com/sanifu-run/anza/internal/domain"
	"github.com/sanifu-run/anza/internal/interviewclient"
	"github.com/sanifu-run/anza/internal/lifecycle"
	"github.com/sanifu-run/anza/internal/process"
)

func TestCLICommandRegistry(t *testing.T) {
	app := NewApp(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, "test")
	for _, name := range []string{"bootstrap", "inspect", "setup", "interview", "plan", "review", "apply", "run", "repair", "doctor", "update", "uninstall", "diagnostics", "exercise"} {
		if handler, ok := app.Commands[name]; !ok || handler == nil {
			t.Errorf("command %q has no reachable handler", name)
		}
	}
}

func TestAppInjectedCommandAndStreams(t *testing.T) {
	var out, diagnostic bytes.Buffer
	input := strings.NewReader("fixture input")
	app := NewApp(input, &out, &diagnostic, "test")
	called := false
	app.Commands["inspect"] = func(ctx context.Context, args []string, in io.Reader, stdout, stderr io.Writer) error {
		called = true
		if string(args[0]) != "--json" {
			t.Fatalf("args = %v", args)
		}
		if in != input || stdout != &out || stderr != &diagnostic {
			t.Fatal("handler did not receive injected streams")
		}
		_, err := io.WriteString(stdout, "{}\n")
		return err
	}
	if got := app.Run(context.Background(), []string{"inspect", "--json"}); got != ExitOK {
		t.Fatalf("exit = %d", got)
	}
	if !called || out.String() != "{}\n" {
		t.Fatalf("called=%v stdout=%q", called, out.String())
	}
}

func TestAppCancellationAndUnknownCommand(t *testing.T) {
	var out, diagnostic bytes.Buffer
	app := NewApp(strings.NewReader(""), &out, &diagnostic, "test")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := app.Run(ctx, []string{"setup"}); got != ExitCanceled {
		t.Fatalf("canceled exit = %d", got)
	}
	diagnostic.Reset()
	if got := app.Run(context.Background(), []string{"does-not-exist"}); got != ExitUsage {
		t.Fatalf("unknown exit = %d", got)
	}
	if !strings.Contains(diagnostic.String(), "unknown command") {
		t.Fatalf("diagnostics = %q", diagnostic.String())
	}
}

func TestCLIExitCodes(t *testing.T) {
	tests := []struct {
		args []string
		want int
	}{
		{nil, ExitOK},
		{[]string{"help"}, ExitOK},
		{[]string{"version"}, ExitOK},
		{[]string{"unknown"}, ExitUsage},
		{[]string{"setup"}, ExitFailure},
	}
	for _, test := range tests {
		app := NewApp(strings.NewReader("synthetic"), &bytes.Buffer{}, &bytes.Buffer{}, "test")
		if got := app.Run(context.Background(), test.args); got != test.want {
			t.Errorf("Run(%v) = %d, want %d", test.args, got, test.want)
		}
	}
}

func TestCLINoninteractive(t *testing.T) {
	const syntheticCredential = "test-only-credential-marker"
	t.Setenv("HOME", t.TempDir())
	t.Setenv("OPENROUTER_API_KEY", syntheticCredential)
	var out, diagnostic bytes.Buffer
	app := NewApp(strings.NewReader("synthetic input"), &out, &diagnostic, "test")
	if got := app.Run(context.Background(), []string{"setup", "--api-key", syntheticCredential}); got != ExitFailure {
		t.Fatalf("exit = %d", got)
	}
	if !strings.Contains(diagnostic.String(), "setup accepts no arguments") {
		t.Fatalf("diagnostic = %q", diagnostic.String())
	}
	if strings.Contains(out.String()+diagnostic.String(), syntheticCredential) || strings.Contains(out.String()+diagnostic.String(), "synthetic input") {
		t.Fatal("credential or synthetic input leaked to CLI output")
	}
	if got := app.Run(context.Background(), []string{"setup"}); got != ExitFailure {
		t.Fatalf("noninteractive setup exit = %d", got)
	}
	if !strings.Contains(diagnostic.String(), "interactive consent") {
		t.Fatalf("diagnostic = %q", diagnostic.String())
	}
	entries, err := os.ReadDir(os.Getenv("HOME"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("noninteractive setup changed HOME: entries=%v err=%v", entries, err)
	}
}

func TestCLIDeniedSetupConsentStartsNoProviderSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	chat := &countingWizardChat{}
	var out, diagnostic bytes.Buffer
	app := NewApp(strings.NewReader("demo\nnew\nbeginner\nBuild a demo app\nCreate first slice\n\nno\n"), &out, &diagnostic, "test")
	app.Interactive, app.Chat = true, chat
	if got := app.Run(context.Background(), []string{"setup"}); got != ExitFailure {
		t.Fatalf("denied setup exit = %d, diagnostic=%s", got, diagnostic.String())
	}
	if chat.calls != 0 || !strings.Contains(diagnostic.String(), "consent was not recorded") {
		t.Fatalf("denied setup calls=%d diagnostic=%q", chat.calls, diagnostic.String())
	}
}

func TestCLIGuidedDefaultStopsPartiallyWhenReviewIsSkipped(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var out, diagnostic bytes.Buffer
	app := NewApp(strings.NewReader("guided\nnew\nbeginner\nBuild a demo\nCreate a slice\n\nyes\n:recommend\n"), &out, &diagnostic, "test")
	app.Interactive, app.Chat = true, fixtureChat{}
	app.GuidedApproval = func(context.Context, string) (string, error) { return "no", nil }
	if got := app.Run(context.Background(), nil); got != ExitOK {
		t.Fatalf("default guided flow exit=%d stderr=%s", got, diagnostic.String())
	}
	if !strings.Contains(out.String(), "Readiness: partial. Plan remains unapplied") {
		t.Fatalf("skipped review did not report partial readiness: %s", out.String())
	}
	diagnostic.Reset()
	if got := app.Run(context.Background(), []string{"apply", "--json"}); got != ExitFailure || !strings.Contains(diagnostic.String(), "saved approval") {
		t.Fatalf("skipped review left an applicable plan: exit=%d stderr=%s", got, diagnostic.String())
	}
}

func TestRunOpenRouterPromptsAndIgnoresEnvironment(t *testing.T) {
	const envSecret = "environment-secret-marker"
	const enteredSecret = "synthetic-masked-secret"
	t.Setenv("OPENROUTER_API_KEY", envSecret)
	t.Setenv("HOME", t.TempDir())
	store, err := credentials.NewNativeStore(&memoryCredentialBackend{items: map[string][]byte{}})
	if err != nil {
		t.Fatal(err)
	}
	var out, diagnostic bytes.Buffer
	app := NewApp(strings.NewReader("yes\n"), &out, &diagnostic, "test")
	app.Interactive, app.CredentialStore = true, store
	app.FindExecutable = func(string) (string, error) { return "/fixture/claude", nil }
	promptCalls, runnerCalls := 0, 0
	app.SecretPrompt = func(io.Reader, io.Writer) (credentials.Secret, error) {
		promptCalls++
		return credentials.NewSecret([]byte(enteredSecret)), nil
	}
	app.LaunchRunner = func(_ context.Context, spec process.Spec) (process.Result, error) {
		runnerCalls++
		if got := spec.SensitiveEnv.String(); got != "SensitiveEnv{value_count:3}" {
			t.Fatalf("child environment is not redacted or scoped: %s", got)
		}
		if strings.Contains(strings.Join(spec.Args, " "), enteredSecret) || strings.Contains(strings.Join(spec.Args, " "), envSecret) {
			t.Fatal("credential appeared in child argv")
		}
		return process.Result{}, nil
	}
	if got := app.Run(context.Background(), []string{"run", "claude", "--provider", "openrouter", "--version", "2.1.276", "--model", "anthropic/claude-sonnet-4"}); got != ExitOK {
		t.Fatalf("run exit=%d stderr=%s", got, diagnostic.String())
	}
	if promptCalls != 1 || runnerCalls != 1 {
		t.Fatalf("prompt calls=%d runner calls=%d", promptCalls, runnerCalls)
	}
	id, err := credentials.NewCredentialID("openrouter", mustTestWorkspace(t))
	if err != nil {
		t.Fatal(err)
	}
	stored, err := store.Get(id)
	if err != nil {
		t.Fatal("masked credential was not retained in the injected session store")
	}
	if string(stored.Bytes()) != enteredSecret {
		t.Fatal("credential store did not receive the masked synthetic value")
	}
	stored.Clear()
	if strings.Contains(out.String()+diagnostic.String(), enteredSecret) || strings.Contains(out.String()+diagnostic.String(), envSecret) {
		t.Fatal("credential value leaked to CLI output")
	}
	entries, err := os.ReadDir(os.Getenv("HOME"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("credential was written outside native store: entries=%v err=%v", entries, err)
	}
}

func TestRunOpenRouterNoninteractiveIgnoresEnvironment(t *testing.T) {
	const envSecret = "environment-secret-marker"
	t.Setenv("OPENROUTER_API_KEY", envSecret)
	store, err := credentials.NewNativeStore(&memoryCredentialBackend{items: map[string][]byte{}})
	if err != nil {
		t.Fatal(err)
	}
	var out, diagnostic bytes.Buffer
	app := NewApp(strings.NewReader("synthetic input"), &out, &diagnostic, "test")
	app.CredentialStore = store
	app.FindExecutable = func(string) (string, error) { return "/fixture/claude", nil }
	promptCalls := 0
	app.SecretPrompt = func(io.Reader, io.Writer) (credentials.Secret, error) {
		promptCalls++
		return credentials.NewSecret([]byte(envSecret)), nil
	}
	if got := app.Run(context.Background(), []string{"run", "claude", "--provider", "openrouter", "--version", "2.1.276", "--model", "anthropic/claude-sonnet-4", "--api-key", envSecret}); got != ExitFailure {
		t.Fatalf("argv credential was accepted; exit=%d", got)
	}
	if strings.Contains(out.String()+diagnostic.String(), envSecret) || promptCalls != 0 {
		t.Fatal("argv credential was displayed or prompted through")
	}
	diagnostic.Reset()
	if got := app.Run(context.Background(), []string{"run", "claude", "--provider", "openrouter", "--version", "2.1.276", "--model", "anthropic/claude-sonnet-4"}); got != ExitFailure {
		t.Fatalf("noninteractive credential exit=%d", got)
	}
	if promptCalls != 0 || !strings.Contains(diagnostic.String(), "rerun interactively") {
		t.Fatalf("prompt calls=%d diagnostic=%q", promptCalls, diagnostic.String())
	}
	if strings.Contains(out.String()+diagnostic.String(), envSecret) {
		t.Fatal("environment credential leaked to CLI output")
	}
}

func mustTestWorkspace(t *testing.T) string {
	t.Helper()
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return root
}

type memoryCredentialBackend struct{ items map[string][]byte }

func (b *memoryCredentialBackend) Put(key string, value []byte) error {
	b.items[key] = append([]byte(nil), value...)
	return nil
}
func (b *memoryCredentialBackend) Get(key string) ([]byte, error) {
	value, ok := b.items[key]
	if !ok {
		return nil, credentials.ErrNotFound
	}
	return append([]byte(nil), value...), nil
}
func (b *memoryCredentialBackend) Delete(key string) error {
	if _, ok := b.items[key]; !ok {
		return credentials.ErrNotFound
	}
	delete(b.items, key)
	return nil
}

func TestAppMissingConsentStyleErrorsStayActionable(t *testing.T) {
	var out, diagnostic bytes.Buffer
	app := NewApp(strings.NewReader(""), &out, &diagnostic, "test")
	app.Commands["setup"] = func(context.Context, []string, io.Reader, io.Writer, io.Writer) error {
		return errors.New("consent is required; rerun interactively")
	}
	if got := app.Run(context.Background(), []string{"setup", "--non-interactive"}); got != ExitFailure {
		t.Fatalf("exit = %d", got)
	}
	if !strings.Contains(diagnostic.String(), "consent is required") {
		t.Fatalf("diagnostics = %q", diagnostic.String())
	}
}

func TestCLIEndToEndFixture(t *testing.T) {
	var providerRequests, downloadRequests atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerRequests.Add(1)
		_, _ = io.WriteString(w, `{"version":"1.2.3","artifact_digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","protocol_min":1,"protocol_max":1}`)
	}))
	defer provider.Close()
	download := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloadRequests.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "synthetic artifact")
	}))
	defer download.Close()
	tmpHome := t.TempDir()
	workspace := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestCLIHelperProcess$")
	cmd.Dir = workspace
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + tmpHome, "ANZA_CLI_HELPER=1", "ANZA_FAKE_PROVIDER=" + provider.URL, "ANZA_FAKE_DOWNLOAD=" + download.URL}
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI fixture subprocess failed: %v\n%s", err, output)
	}
	if got := providerRequests.Load(); got != 1 {
		t.Fatalf("expected only the local update metadata request, got %d provider requests", got)
	}
	if got := downloadRequests.Load(); got != 0 {
		t.Fatalf("unexpected download requests for artifact-free reviewed plan: %d", got)
	}
	entries, err := os.ReadDir(workspace)
	if err != nil || len(entries) != 0 {
		t.Fatalf("CLI fixture wrote outside HOME: workspace entries=%v err=%v", entries, err)
	}
	if _, err := os.Stat(filepath.Join(tmpHome, "diagnostics.json")); err != nil {
		t.Fatalf("diagnostics export was not confined to temp HOME: %v", err)
	}
}

// TestCLIHelperProcess runs only inside the child test binary created by the
// integration fixture. It injects synthetic input and loopback service fakes.
func TestCLIHelperProcess(t *testing.T) {
	if os.Getenv("ANZA_CLI_HELPER") != "1" {
		return
	}
	providerURL, downloadURL := os.Getenv("ANZA_FAKE_PROVIDER"), os.Getenv("ANZA_FAKE_DOWNLOAD")
	if !strings.HasPrefix(providerURL, "http://127.0.0.1:") || !strings.HasPrefix(downloadURL, "http://127.0.0.1:") {
		os.Exit(8)
	}
	var stdout, stderr bytes.Buffer
	input := strings.NewReader("demo\nnew\nbeginner\nBuild a demo app\nCreate first slice\n\n yes\n:recommend\n")
	app := NewApp(input, &stdout, &stderr, "fixture")
	app.Interactive = true
	app.Chat = fixtureChat{}
	app.UpdateChecker = lifecycle.Checker{Source: fixtureReleaseSource{url: providerURL}, Verifier: fixtureVerifier{}}
	app.DownloadClient = &http.Client{Timeout: 2 * time.Second, Transport: fixtureDownloadTransport{url: downloadURL}}
	fixtureRun := func(args ...string) string {
		stdout.Reset()
		stderr.Reset()
		if code := app.Run(context.Background(), args); code != ExitOK {
			fmt.Fprintf(os.Stderr, "%v exited %d: %s", args, code, stderr.String())
			os.Exit(11)
		}
		return stdout.String()
	}
	if got := fixtureRun("setup"); !strings.Contains(got, "Interview and recommendation saved privately") {
		fmt.Fprintln(os.Stderr, "real setup handler did not save recommendation")
		os.Exit(12)
	}
	planJSON := fixtureRun("plan", "--session", "demo", "--json")
	var plan domain.Plan
	if err := json.Unmarshal([]byte(planJSON), &plan); err != nil || plan.Digest == "" {
		fmt.Fprintf(os.Stderr, "real plan handler produced invalid plan: %v %s", err, planJSON)
		os.Exit(13)
	}
	app.In = strings.NewReader("approve " + plan.Digest + "\n")
	if got := fixtureRun("review", "--approve-digest", plan.Digest, "--json"); !strings.Contains(got, plan.Digest) {
		fmt.Fprintln(os.Stderr, "real review handler did not persist plan approval")
		os.Exit(14)
	}
	if got := fixtureRun("apply", "--json"); !strings.Contains(got, plan.Digest) || !strings.Contains(got, `"operations":[]`) {
		fmt.Fprintf(os.Stderr, "apply did not persist an empty effect receipt for the approved plan: %s", got)
		os.Exit(19)
	}
	if got := fixtureRun("repair"); !strings.HasPrefix(strings.TrimSpace(got), "{") {
		fmt.Fprintf(os.Stderr, "repair handler returned no lifecycle result: %s", got)
		os.Exit(20)
	}
	if got := fixtureRun("doctor", "--json"); !strings.Contains(got, "checks") {
		fmt.Fprintln(os.Stderr, "doctor handler produced no checks")
		os.Exit(15)
	}
	if got := fixtureRun("exercise", "--id", "mobile-desktop-exercise", "--project-kind", "unknown", "--json"); !strings.Contains(got, "manual_steps") {
		fmt.Fprintln(os.Stderr, "exercise handler produced no manual exercise result")
		os.Exit(16)
	}
	if got := fixtureRun("update"); !strings.Contains(got, `"version":"1.2.3"`) {
		fmt.Fprintln(os.Stderr, "update handler did not verify local release metadata")
		os.Exit(17)
	}
	if got := fixtureRun("diagnostics", "--preview"); !strings.HasPrefix(strings.TrimSpace(got), "{") {
		fmt.Fprintf(os.Stderr, "diagnostics preview returned no report: %s", got)
		os.Exit(21)
	}
	fixtureRun("diagnostics", "--export", filepath.Join(os.Getenv("HOME"), "diagnostics.json"))
	if got := fixtureRun("uninstall", "--preview"); !strings.Contains(got, "Preview only") {
		fmt.Fprintf(os.Stderr, "uninstall preview did not describe its guarded state: %s", got)
		os.Exit(22)
	}
	if got := fixtureRun("uninstall", "--confirm-digest", plan.Digest); !strings.Contains(got, "Removed 0") {
		fmt.Fprintf(os.Stderr, "uninstall did not report its confirmed receipt result: %s", got)
		os.Exit(23)
	}
	if got := fixtureRun("inspect", "--json"); !strings.Contains(got, `"os":`) {
		fmt.Fprintf(os.Stderr, "inspect handler produced no platform facts: %s", got)
		os.Exit(18)
	}
	app.In = strings.NewReader("guided\nnew\nbeginner\nBuild a local demo\nCreate the first slice\n\n yes\n:recommend\n")
	app.GuidedApproval = func(_ context.Context, digest string) (string, error) { return "approve " + digest, nil }
	stdout.Reset()
	if code := app.Run(context.Background(), nil); code != ExitOK {
		fmt.Fprintf(os.Stderr, "guided default command exited %d: %s", code, stderr.String())
		os.Exit(24)
	}
	if !strings.Contains(stdout.String(), "Login guidance:") || !strings.Contains(stdout.String(), "Overall status:") || !strings.Contains(stdout.String(), "mobile-desktop-exercise") {
		fmt.Fprintf(os.Stderr, "guided default path omitted a readiness phase: %s", stdout.String())
		os.Exit(25)
	}
	if home, _ := os.UserHomeDir(); home != os.Getenv("HOME") || !filepath.IsAbs(home) {
		os.Exit(10)
	}
	os.Exit(0)
}

type fixtureChat struct{}

func (fixtureChat) Capabilities(context.Context) (interviewclient.Capabilities, error) {
	return interviewclient.Capabilities{ProtocolVersion: 1, Enabled: true}, nil
}
func (fixtureChat) NewSession(_ context.Context, name string, start interviewclient.SetupStart) (WizardSession, error) {
	if start.ConsentVersion != SetupConsentVersion {
		return nil, errors.New("explicit setup consent was not attached")
	}
	return fixtureSession{name: name}, nil
}
func (fixtureChat) ResumeSession(name string) (WizardSession, error) {
	return fixtureSession{name: name}, nil
}

type fixtureSession struct{ name string }

func (fixtureSession) Ask(context.Context, string) (interviewclient.AskResponse, error) {
	return interviewclient.AskResponse{Mode: "answer", Answer: "Synthetic local interview response"}, nil
}
func (fixtureSession) UpdateContext(context.Context, *domain.ProjectBrief, *string, *domain.MachineFacts) (interviewclient.Conversation, error) {
	return interviewclient.Conversation{}, nil
}
func (fixtureSession) Recommend(context.Context) (interviewclient.RecommendationResponse, error) {
	cat, err := catalog.LoadBundled()
	if err != nil {
		return interviewclient.RecommendationResponse{}, err
	}
	return interviewclient.RecommendationResponse{Recommendation: domain.Recommendation{SchemaVersion: 1, CatalogVersion: cat.Version(), Summary: "Synthetic reviewed recommendation", SelectedRecipeIDs: []string{}, SelectedPackIDs: []string{}, SelectedExerciseID: "mobile-desktop-exercise", Reasons: map[string]string{}, UnresolvedQuestions: []string{}, ManualSteps: []string{}, ReadinessConstraints: []string{}}}, nil
}

type fixtureReleaseSource struct{ url string }

func (s fixtureReleaseSource) Latest(ctx context.Context) (lifecycle.SignedRelease, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return lifecycle.SignedRelease{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return lifecycle.SignedRelease{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return lifecycle.SignedRelease{}, fmt.Errorf("local release server returned %s", resp.Status)
	}
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return lifecycle.SignedRelease{}, err
	}
	digest := sha256.Sum256(payload)
	return lifecycle.SignedRelease{Payload: payload, Signature: []byte("fixture"), MetadataDigest: hex.EncodeToString(digest[:])}, nil
}

type fixtureVerifier struct{}

func (fixtureVerifier) Verify(_, signature []byte) error {
	if string(signature) != "fixture" {
		return errors.New("invalid fixture signature")
	}
	return nil
}

type fixtureDownloadTransport struct{ url string }

func (t fixtureDownloadTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	local, err := http.NewRequestWithContext(req.Context(), req.Method, t.url, req.Body)
	if err != nil {
		return nil, err
	}
	return http.DefaultTransport.RoundTrip(local)
}

type countingWizardChat struct{ calls int }

func (c *countingWizardChat) Capabilities(context.Context) (interviewclient.Capabilities, error) {
	c.calls++
	return interviewclient.Capabilities{}, nil
}
func (c *countingWizardChat) NewSession(context.Context, string, interviewclient.SetupStart) (WizardSession, error) {
	c.calls++
	return nil, nil
}
func (c *countingWizardChat) ResumeSession(string) (WizardSession, error) { c.calls++; return nil, nil }
