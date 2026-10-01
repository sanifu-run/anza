package acceptance

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"testing/fstest"
	"time"

	"github.com/sanifu-run/anza/internal/brief"
	"github.com/sanifu-run/anza/internal/catalog"
	"github.com/sanifu-run/anza/internal/configedit"
	"github.com/sanifu-run/anza/internal/credentials"
	"github.com/sanifu-run/anza/internal/domain"
	"github.com/sanifu-run/anza/internal/download"
	"github.com/sanifu-run/anza/internal/executor"
	"github.com/sanifu-run/anza/internal/planner"
	"github.com/sanifu-run/anza/internal/process"
	"github.com/sanifu-run/anza/internal/state"
)

func TestMaliciousInputs(t *testing.T) {
	t.Run("poisoned brief field", func(t *testing.T) {
		input := []byte(`{"schema_version":1,"project_summary":"reviewed","desired_slice":"one item","experience":"beginner","constraints":[],"known_stack":[],"project_kind":"web","existing_project":false,"access_token":"canary-secret"}`)
		if _, err := brief.ParseJSON(input); err == nil {
			t.Fatal("credential-bearing brief was accepted")
		}
	})

	for _, tc := range []struct {
		name    string
		entries []archiveFixtureEntry
	}{
		{name: "parent traversal", entries: []archiveFixtureEntry{{name: "../outside.txt", body: "escape"}}},
		{name: "case collision", entries: []archiveFixtureEntry{{name: "Config/settings.json", body: "one"}, {name: "config/SETTINGS.json", body: "two"}}},
		{name: "symlink", entries: []archiveFixtureEntry{{name: "link", body: "../../outside.txt", symlink: true}}},
	} {
		t.Run("archive "+tc.name, func(t *testing.T) {
			archivePath := filepath.Join(t.TempDir(), "hostile.zip")
			writeSecurityZip(t, archivePath, tc.entries...)
			target := filepath.Join(t.TempDir(), "extract")
			err := download.Extract(context.Background(), archivePath, target, download.FormatZip, download.Limits{
				MaxEntries: 8, MaxBytes: 4096, MaxFileBytes: 2048, MaxArchiveBytes: 4096,
			})
			if err == nil {
				t.Fatalf("hostile archive %q was accepted", tc.name)
			}
		})
	}

	t.Run("unknown manifest field", func(t *testing.T) {
		fsys := fstest.MapFS{"manifest.json": {Data: []byte(`{"schema_version":1,"version":"test","entries":[],"provenance":[],"fetch_url":"https://attacker.invalid"}`)}}
		if _, err := catalog.Load(fsys); err == nil {
			t.Fatal("manifest with an uncontracted fetch field was accepted")
		}
	})
}

func TestQuotaReplayIsolation(t *testing.T) {
	t.Skip("quota replay isolation is owned and exercised by Chat T3.6; the shared Chat fixture cannot bind sockets in this runtime")
}

func TestCredentialLeakCanaries(t *testing.T) {
	const canary = "ANZA_CREDENTIAL_CANARY_7c1b"
	input := []byte(`{"schema_version":1,"project_summary":"reviewed","desired_slice":"one item","experience":"beginner","constraints":[],"known_stack":[],"project_kind":"web","existing_project":false,"api_key":"` + canary + `"}`)
	_, err := brief.ParseJSON(input)
	if err == nil {
		t.Fatal("brief containing an API key was accepted")
	}
	if strings.Contains(err.Error(), canary) {
		t.Fatal("credential canary leaked into the validation error")
	}
}

func TestApprovalTOCTOU(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reviewed.json")
	const reviewed = `{"schema_version":1,"project_summary":"reviewed content","desired_slice":"one item","experience":"beginner","constraints":[],"known_stack":[],"project_kind":"web","existing_project":false}`
	if err := os.WriteFile(path, []byte(reviewed), 0600); err != nil {
		t.Fatal(err)
	}
	preview, err := brief.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const replacement = `{"schema_version":1,"project_summary":"changed after preview","desired_slice":"different item","experience":"beginner","constraints":[],"known_stack":[],"project_kind":"web","existing_project":false}`
	if err := os.WriteFile(path, []byte(replacement), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := preview.Approve(false); !errors.Is(err, brief.ErrReviewRequired) {
		t.Fatalf("unconfirmed preview was approved: %v", err)
	}
	approved, err := preview.Approve(true)
	if err != nil {
		t.Fatal(err)
	}
	if approved.Brief == nil || approved.Brief.ProjectSummary != "reviewed content" {
		t.Fatalf("approval silently switched to bytes changed after review: %#v", approved.Brief)
	}
}

func TestRedirectPolicy(t *testing.T) {
	body := []byte("synthetic artifact")
	digest := sha256.Sum256(body)
	transport := &redirectFixture{location: "https://attacker.invalid/payload"}
	client := &http.Client{Transport: transport}
	_, err := download.Fetch(context.Background(), client, download.Artifact{
		URL: "https://downloads.example.test/payload", SHA256: hex.EncodeToString(digest[:]), Size: int64(len(body)),
	}, []string{"downloads.example.test"}, 1024, filepath.Join(t.TempDir(), "cache"))
	if err == nil || !strings.Contains(err.Error(), "redirect target is not approved") {
		t.Fatalf("unapproved redirect was not denied, err=%v", err)
	}
	if transport.requests != 1 {
		t.Fatalf("redirect followed to another host: round trips=%d", transport.requests)
	}
}

func TestStalePlanApplyRace(t *testing.T) {
	start := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	expires := start.Add(time.Minute)
	now := start
	before := []byte(`{"mode":"old"}`)
	edit, err := configedit.Plan(before, configedit.Patch{Format: configedit.JSON, Set: map[string]any{"mode": "new"}, Expected: map[string]any{"mode": "old"}})
	if err != nil {
		t.Fatal(err)
	}
	plan := domain.Plan{
		SchemaVersion: 1, ID: "stale-race", WorkspaceID: "workspace-test",
		CatalogDigest: strings.Repeat("1", 64), FactsDigest: strings.Repeat("2", 64), RecommendationDigest: strings.Repeat("3", 64),
		CreatedAt: start.Format(time.RFC3339Nano), ExpiresAt: expires.Format(time.RFC3339Nano), Profile: "developer",
		Providers: map[string]string{}, Operations: []domain.Operation{{ID: "config", Kind: "write_config", Dependencies: []string{}, TargetRoot: "launch-profile", RelativePath: "settings.json", PreimageHash: edit.PreimageHash, ExpectedPostimageHash: edit.PostimageHash, Reversible: true, Privilege: "none", Description: "synthetic config update"}},
		Warnings: []string{}, ManualSteps: []string{},
	}
	plan.Digest, err = domain.CanonicalPlanDigest(plan)
	if err != nil {
		t.Fatal(err)
	}
	store, err := state.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	effects := &staleRaceEffects{file: append([]byte(nil), before...), onFirstRead: func() { now = expires.Add(time.Nanosecond) }}
	approval := domain.Approval{PlanDigest: plan.Digest, ApprovedAt: start.Format(time.RFC3339Nano), DisclosureVersion: "plan-review-v1"}
	_, err = executor.Apply(context.Background(), plan, approval, executor.Options{
		Store: store, Effects: effects, Payloads: map[string]executor.Payload{"config": executor.ConfigPayload{Edit: edit}}, Now: func() time.Time { return now },
	})
	if !errors.Is(err, executor.ErrStaleApproval) {
		t.Fatalf("expired apply error=%v, want ErrStaleApproval", err)
	}
	if effects.writes != 0 || string(effects.file) != string(before) {
		t.Fatalf("stale plan produced an effect: writes=%d file=%s", effects.writes, effects.file)
	}
}

func TestStalePlanResumeRace(t *testing.T) {
	start := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	expires := start.Add(time.Minute)
	now := start
	before := []byte(`{"mode":"old"}`)
	edit, err := configedit.Plan(before, configedit.Patch{Format: configedit.JSON, Set: map[string]any{"mode": "new"}, Expected: map[string]any{"mode": "old"}})
	if err != nil {
		t.Fatal(err)
	}
	plan := staleSecurityPlan(t, start, expires, edit)
	approval := domain.Approval{PlanDigest: plan.Digest, ApprovedAt: start.Format(time.RFC3339Nano), DisclosureVersion: "plan-review-v1"}
	store, err := state.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	effects := &staleRaceEffects{file: append([]byte(nil), before...), interruptNext: true}
	opts := executor.Options{Store: store, Effects: effects, Payloads: map[string]executor.Payload{"config": executor.ConfigPayload{Edit: edit}}, Now: func() time.Time { return now }}
	if _, err := executor.Apply(context.Background(), plan, approval, opts); !errors.Is(err, executor.ErrInterrupted) {
		t.Fatalf("seed interrupted apply error=%v", err)
	}
	effects.readCount = 0
	effects.onFirstRead = func() { now = expires.Add(time.Nanosecond) }
	_, err = executor.Recover(context.Background(), plan, approval, opts)
	if !errors.Is(err, executor.ErrStaleApproval) {
		t.Fatalf("expired recovery error=%v, want ErrStaleApproval", err)
	}
	if effects.writes != 0 || string(effects.file) != string(before) {
		t.Fatalf("stale recovery produced an effect: writes=%d file=%s", effects.writes, effects.file)
	}
}

type staleRaceEffects struct {
	file          []byte
	writes        int
	readCount     int
	onFirstRead   func()
	interruptNext bool
}

func (e *staleRaceEffects) Read(_ context.Context, _ domain.Operation) (executor.Snapshot, error) {
	e.readCount++
	if e.readCount == 1 && e.onFirstRead != nil {
		e.onFirstRead()
	}
	return executor.Snapshot{Exists: true, Regular: true, Mode: 0600, Bytes: append([]byte(nil), e.file...)}, nil
}

func (e *staleRaceEffects) ApplyConfig(_ context.Context, _ domain.Operation, expected string, edit configedit.Edit) error {
	if e.interruptNext {
		e.interruptNext = false
		return executor.ErrInterrupted
	}
	if hashSecurity(e.file) != expected {
		return executor.ErrPreimageConflict
	}
	e.file = append([]byte(nil), edit.Bytes...)
	e.writes++
	return nil
}

func staleSecurityPlan(t *testing.T, start, expires time.Time, edit configedit.Edit) domain.Plan {
	t.Helper()
	plan := domain.Plan{
		SchemaVersion: 1, ID: "stale-race", WorkspaceID: "workspace-test",
		CatalogDigest: strings.Repeat("1", 64), FactsDigest: strings.Repeat("2", 64), RecommendationDigest: strings.Repeat("3", 64),
		CreatedAt: start.Format(time.RFC3339Nano), ExpiresAt: expires.Format(time.RFC3339Nano), Profile: "developer",
		Providers: map[string]string{}, Operations: []domain.Operation{{ID: "config", Kind: "write_config", Dependencies: []string{}, TargetRoot: "launch-profile", RelativePath: "settings.json", PreimageHash: edit.PreimageHash, ExpectedPostimageHash: edit.PostimageHash, Reversible: true, Privilege: "none", Description: "synthetic config update"}},
		Warnings: []string{}, ManualSteps: []string{},
	}
	var err error
	plan.Digest, err = domain.CanonicalPlanDigest(plan)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func (*staleRaceEffects) PublishArtifact(context.Context, domain.Operation, string, executor.ArtifactPayload) error {
	return errors.New("unexpected artifact effect")
}

func hashSecurity(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func TestConfigInverseAndCorruptionRecovery(t *testing.T) {
	before := []byte(`{"owned":"old","participant":"before"}`)
	applied, err := configedit.Plan(before, configedit.Patch{Format: configedit.JSON, Set: map[string]any{"owned": "anza"}, Expected: map[string]any{"owned": "old"}})
	if err != nil {
		t.Fatal(err)
	}
	current := []byte(`{"owned":"anza","participant":"after"}`)
	inverse, err := configedit.Inverse(current, applied)
	if err != nil {
		t.Fatal(err)
	}
	var restored map[string]string
	if err := json.Unmarshal(inverse.Bytes, &restored); err != nil {
		t.Fatal(err)
	}
	if restored["owned"] != "old" || restored["participant"] != "after" || len(inverse.SkippedKeys) != 0 {
		t.Fatalf("inverse failed to restore owned key while preserving participant change: %#v skipped=%v", restored, inverse.SkippedKeys)
	}
	participantChangedOwned := []byte(`{"owned":"participant","participant":"after"}`)
	preserved, err := configedit.Inverse(participantChangedOwned, applied)
	if err != nil {
		t.Fatal(err)
	}
	var preservedValues map[string]string
	if err := json.Unmarshal(preserved.Bytes, &preservedValues); err != nil {
		t.Fatal(err)
	}
	if preservedValues["owned"] != "participant" || len(preserved.SkippedKeys) != 1 || preserved.SkippedKeys[0] != "owned" {
		t.Fatalf("inverse overwrote a participant replacement: %#v skipped=%v", preservedValues, preserved.SkippedKeys)
	}
	if _, err := configedit.Inverse([]byte(`{"owned":`), applied); err == nil {
		t.Fatal("corrupt current configuration was treated as recoverable")
	}

	store, err := state.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.Root(), "journal.json")
	corrupt := []byte(`{"schema_version":1,"payload":`)
	if err := os.WriteFile(path, corrupt, 0600); err != nil {
		t.Fatal(err)
	}
	var destination map[string]any
	if err := store.Load("journal", &destination); !errors.Is(err, state.ErrMalformedState) {
		t.Fatalf("Load corrupt journal error=%v", err)
	}
	if err := store.Save("journal", map[string]string{"state": "replacement"}); !errors.Is(err, state.ErrMalformedState) {
		t.Fatalf("Save over corrupt journal error=%v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(corrupt) {
		t.Fatal("corrupt journal was overwritten during recovery")
	}
}

func TestCredentialLifetimeAndLeakage(t *testing.T) {
	const canary = "ANZA_SESSION_CANARY_4f9a"
	id, err := credentials.NewCredentialID("synthetic-provider", "workspace-test")
	if err != nil {
		t.Fatal(err)
	}
	store := credentials.NewSessionStore()
	input := []byte(canary)
	secret := credentials.NewSecret(input)
	clear(input)
	if err := store.Put(id, secret); err != nil {
		t.Fatal(err)
	}
	secret.Clear()
	got, err := store.Get(id)
	gotBytes := got.Bytes()
	if err != nil || !bytes.Equal(gotBytes, []byte(canary)) {
		clear(gotBytes)
		t.Fatalf("session store did not retain its owned copy: err=%v", err)
	}
	clear(gotBytes)
	serialized, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got.String()+got.Error()+string(serialized), canary) {
		t.Fatal("credential canary leaked through formatting or JSON")
	}
	store.Close()
	if _, err := store.Get(id); !errors.Is(err, credentials.ErrClosed) {
		t.Fatalf("closed session store returned a credential: %v", err)
	}
	got.Clear()
}

func TestSecurityLeaderExitDescendantCancellation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix process-group cancellation semantics")
	}
	if role := os.Getenv("ANZA_SECURITY_PIPE_ROLE"); role != "" {
		securityPipeFixture(t, role)
		return
	}
	childBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "descendant-counter")
	leaderPID := filepath.Join(dir, "leader-pid")
	descendantPID := filepath.Join(dir, "descendant-pid")
	t.Cleanup(func() { killSecurityProcess(descendantPID) })
	env, err := process.NewSensitiveEnv(map[string]string{
		"ANZA_SECURITY_PIPE_ROLE":           "leader",
		"ANZA_SECURITY_PIPE_MARKER":         marker,
		"ANZA_SECURITY_PIPE_LEADER_PID":     leaderPID,
		"ANZA_SECURITY_PIPE_DESCENDANT_PID": descendantPID,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, runErr := process.Run(ctx, process.Spec{Program: childBinary, Args: []string{"-test.run=^TestSecurityLeaderExitDescendantCancellation$"}, Dir: dir, Timeout: 10 * time.Second, SensitiveEnv: env})
		result <- runErr
	}()
	waitSecurityCounter(t, marker, -1)
	waitForSecurityLeaderReaped(t, leaderPID)
	cancel()
	select {
	case runErr := <-result:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run error=%v, want context.Canceled", runErr)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("Run did not join after process-group cancellation")
	}
	before := readSecurityCounter(t, marker)
	time.Sleep(50 * time.Millisecond)
	if after := readSecurityCounter(t, marker); after != before {
		t.Fatalf("same-group descendant continued after Run returned: %d -> %d", before, after)
	}
}

func TestSecurityWaitDelayClassifiedAndCleansDescendant(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix process-group cleanup semantics")
	}
	if role := os.Getenv("ANZA_SECURITY_PIPE_ROLE"); role != "" {
		securityPipeFixture(t, role)
		return
	}
	childBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "descendant-counter")
	leaderPID := filepath.Join(dir, "leader-pid")
	descendantPID := filepath.Join(dir, "descendant-pid")
	t.Cleanup(func() { killSecurityProcess(descendantPID) })
	env, err := process.NewSensitiveEnv(map[string]string{
		"ANZA_SECURITY_PIPE_ROLE":           "leader",
		"ANZA_SECURITY_PIPE_MARKER":         marker,
		"ANZA_SECURITY_PIPE_LEADER_PID":     leaderPID,
		"ANZA_SECURITY_PIPE_DESCENDANT_PID": descendantPID,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, runErr := process.Run(context.Background(), process.Spec{Program: childBinary, Args: []string{"-test.run=^TestSecurityWaitDelayClassifiedAndCleansDescendant$"}, Dir: dir, Timeout: 10 * time.Second, SensitiveEnv: env})
	if !errors.Is(runErr, process.ErrWaitDelay) {
		t.Fatalf("Run error=%v, want process.ErrWaitDelay", runErr)
	}
	before := readSecurityCounter(t, marker)
	time.Sleep(50 * time.Millisecond)
	if after := readSecurityCounter(t, marker); after != before {
		t.Fatalf("same-group descendant continued after WaitDelay: %d -> %d", before, after)
	}
}

func securityPipeFixture(t *testing.T, role string) {
	t.Helper()
	marker := os.Getenv("ANZA_SECURITY_PIPE_MARKER")
	if role == "descendant" {
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for i := 1; ; i++ {
			if err := writeSecurityCounter(marker, i); err != nil {
				t.Fatal(err)
			}
			<-ticker.C
		}
	}
	if role != "leader" {
		t.Fatalf("unexpected fixture role %q", role)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestSecurityLeaderExitDescendantCancellation$")
	descendantPID := os.Getenv("ANZA_SECURITY_PIPE_DESCENDANT_PID")
	child.Env = []string{"ANZA_SECURITY_PIPE_ROLE=descendant", "ANZA_SECURITY_PIPE_MARKER=" + marker, "ANZA_SECURITY_PIPE_DESCENDANT_PID=" + descendantPID}
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(descendantPID, []byte(strconv.Itoa(child.Process.Pid)), 0600); err != nil {
		t.Fatal(err)
	}
	waitSecurityCounter(t, marker, -1)
	if err := os.WriteFile(os.Getenv("ANZA_SECURITY_PIPE_LEADER_PID"), []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
		t.Fatal(err)
	}
}

func waitForSecurityLeaderReaped(t *testing.T, pidPath string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		pidBytes, err := os.ReadFile(pidPath)
		if err == nil {
			pid, parseErr := strconv.Atoi(string(pidBytes))
			if parseErr == nil {
				leader, findErr := os.FindProcess(pid)
				if findErr == nil {
					signalErr := leader.Signal(syscall.Signal(0))
					if errors.Is(signalErr, os.ErrProcessDone) || errors.Is(signalErr, syscall.ESRCH) {
						return
					}
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("process.Run leader remained alive")
}

func TestSecurityProcessCancellationOwnership(t *testing.T) {
	if marker := os.Getenv("ANZA_SECURITY_CHILD_MARKER"); marker != "" {
		securityMarkerChild(t, marker)
		return
	}
	childBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	unrelatedMarker := filepath.Join(t.TempDir(), "unrelated-counter")
	unrelated := exec.Command(childBinary, "-test.run=^TestSecurityProcessCancellationOwnership$")
	unrelated.Env = []string{"ANZA_SECURITY_CHILD_MARKER=" + unrelatedMarker}
	if err := unrelated.Start(); err != nil {
		t.Fatalf("start unrelated child: %v", err)
	}
	t.Cleanup(func() {
		_ = unrelated.Process.Kill()
		_ = unrelated.Wait()
	})
	waitSecurityCounter(t, unrelatedMarker, -1)

	ownedDir := t.TempDir()
	ownedMarker := filepath.Join(ownedDir, "owned-counter")
	ownedPID := filepath.Join(ownedDir, "owned-pid")
	ownedGrandchildMarker := filepath.Join(ownedDir, "owned-grandchild-counter")
	ownedGrandchildPID := filepath.Join(ownedDir, "owned-grandchild-pid")
	sensitiveMarker, err := process.NewSensitiveEnv(map[string]string{
		"ANZA_SECURITY_CHILD_MARKER":      ownedMarker,
		"ANZA_SECURITY_CHILD_PID":         ownedPID,
		"ANZA_SECURITY_SPAWN_GRANDCHILD":  "1",
		"ANZA_SECURITY_GRANDCHILD_MARKER": ownedGrandchildMarker,
		"ANZA_SECURITY_GRANDCHILD_PID":    ownedGrandchildPID,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		killSecurityProcess(ownedPID)
		killSecurityProcess(ownedGrandchildPID)
	})
	result := make(chan error, 1)
	go func() {
		_, err := process.Run(ctx, process.Spec{Program: childBinary, Args: []string{"-test.run=^TestSecurityProcessCancellationOwnership$"}, Dir: t.TempDir(), Timeout: 5 * time.Second, SensitiveEnv: sensitiveMarker})
		result <- err
	}()
	waitSecurityCounter(t, ownedMarker, -1)
	waitSecurityCounter(t, ownedGrandchildMarker, -1)
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run error=%v, want context.Canceled", err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
	ownedValue := readSecurityCounter(t, ownedMarker)
	ownedGrandchildValue := readSecurityCounter(t, ownedGrandchildMarker)
	unrelatedValue := readSecurityCounter(t, unrelatedMarker)
	waitSecurityCounter(t, unrelatedMarker, unrelatedValue)
	if got := readSecurityCounter(t, ownedMarker); got != ownedValue {
		t.Fatalf("owned child continued after Run returned: counter %d -> %d", ownedValue, got)
	}
	if got := readSecurityCounter(t, unrelatedMarker); got <= unrelatedValue {
		t.Fatalf("cancellation stopped unrelated child: counter %d -> %d", unrelatedValue, got)
	}
	if got := readSecurityCounter(t, ownedGrandchildMarker); got != ownedGrandchildValue {
		t.Fatalf("owned descendant continued after Run returned: counter %d -> %d", ownedGrandchildValue, got)
	}
}

func securityMarkerChild(t *testing.T, path string) {
	t.Helper()
	if pidPath := os.Getenv("ANZA_SECURITY_CHILD_PID"); pidPath != "" {
		if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if os.Getenv("ANZA_SECURITY_SPAWN_GRANDCHILD") == "1" {
		child := exec.Command(os.Args[0], "-test.run=^TestSecurityProcessCancellationOwnership$")
		child.Env = []string{"ANZA_SECURITY_CHILD_MARKER=" + os.Getenv("ANZA_SECURITY_GRANDCHILD_MARKER"), "ANZA_SECURITY_CHILD_PID=" + os.Getenv("ANZA_SECURITY_GRANDCHILD_PID")}
		child.Stdout = io.Discard
		child.Stderr = io.Discard
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for counter := 1; ; counter++ {
		if err := writeSecurityCounter(path, counter); err != nil {
			t.Fatal(err)
		}
		<-ticker.C
	}
}

func writeSecurityCounter(path string, value int) error {
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, []byte(strconv.Itoa(value)), 0600); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func killSecurityProcess(pidPath string) {
	pidBytes, err := os.ReadFile(pidPath)
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(string(pidBytes))
	if err != nil {
		return
	}
	child, err := os.FindProcess(pid)
	if err == nil {
		_ = child.Kill()
	}
}

func readSecurityCounter(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read process marker: %v", err)
	}
	value, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatalf("decode process marker: %v", err)
	}
	return value
}

func waitSecurityCounter(t *testing.T, path string, after int) {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if data, err := os.ReadFile(path); err == nil {
				if counter, err := strconv.Atoi(string(data)); err == nil && counter > after {
					return
				}
			}
		case <-deadline.C:
			t.Fatalf("process marker %q did not advance beyond %d", filepath.Base(path), after)
		}
	}
}

func TestMaliciousRecommendationIsNonExecutable(t *testing.T) {
	maliciousModelOutput := []byte(`{"schema_version":1,"catalog_version":"2026.09","summary":"synthetic","selected_recipe_ids":[],"selected_pack_ids":[],"selected_exercise_id":"starter","reasons":{},"unresolved_questions":[],"manual_steps":[],"readiness_constraints":[],"command":"rm -rf ~/project"}`)
	if _, err := domain.DecodeRecommendation(maliciousModelOutput); err == nil {
		t.Fatal("model recommendation with an executable command field was accepted")
	}
	in, err := plannerSecurityInput()
	if err != nil {
		t.Fatal(err)
	}
	in.Recommendation.ManualSteps = []string{"Run `rm -rf ~/project` to repair the setup"}
	if _, err := planner.Build(in); !errors.Is(err, planner.ErrUnsafeText) {
		t.Fatalf("command-bearing recommendation was not rejected: %v", err)
	}
}

func plannerSecurityInput() (planner.Input, error) {
	cat, err := catalog.LoadBundled()
	if err != nil {
		return planner.Input{}, err
	}
	now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	return planner.Input{
		ID: "security-plan", WorkspaceID: "workspace-test", Profile: "developer", Now: now, TTL: time.Hour, Catalog: cat,
		Facts:          domain.MachineFacts{OS: "darwin", Arch: "arm64", OSVersion: "15.0", ShellKind: "zsh", Capabilities: map[string]domain.Capability{"codex-cli": {Status: "missing"}, "claude-code": {Status: "missing"}, "git": {Status: "missing"}}},
		Brief:          domain.ProjectBrief{SchemaVersion: 1, ProjectSummary: "synthetic", Experience: "developer", ProjectKind: "web", ExistingProject: true, Constraints: []string{}, KnownStack: []string{}},
		Recommendation: domain.Recommendation{SchemaVersion: 1, CatalogVersion: cat.Version(), Summary: "Synthetic recommendation", SelectedRecipeIDs: []string{}, SelectedPackIDs: []string{}, SelectedExerciseID: "mobile-desktop-exercise", Reasons: map[string]string{}, UnresolvedQuestions: []string{}, ManualSteps: []string{}, ReadinessConstraints: []string{}},
		Existing:       planner.ExistingState{Capabilities: map[string]domain.Capability{}},
	}, nil
}

type archiveFixtureEntry struct {
	name    string
	body    string
	symlink bool
}

func writeSecurityZip(t *testing.T, path string, entries ...archiveFixtureEntry) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name}
		if entry.symlink {
			header.SetMode(os.ModeSymlink | 0777)
		}
		part, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(part, entry.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

type redirectFixture struct {
	location string
	requests int
}

func (r *redirectFixture) RoundTrip(request *http.Request) (*http.Response, error) {
	r.requests++
	if r.requests != 1 {
		return nil, errors.New("unexpected second request")
	}
	return &http.Response{
		StatusCode: http.StatusFound,
		Header:     http.Header{"Location": []string{r.location}},
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    request,
	}, nil
}
