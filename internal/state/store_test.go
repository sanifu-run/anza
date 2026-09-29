package state

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore(temp root): %v", err)
	}
	return store
}

func TestAtomicCrashRecovery(t *testing.T) {
	store := newTestStore(t)
	wantOld := testDocument{Value: "old"}
	if err := store.Save("journal", wantOld); err != nil {
		t.Fatalf("Save(old): %v", err)
	}

	crash := errors.New("injected process interruption before replace")
	err := store.saveWithHook("journal", testDocument{Value: "new"}, func() error { return crash })
	if !errors.Is(err, crash) {
		t.Fatalf("saveWithHook error = %v, want injected interruption", err)
	}
	var got testDocument
	if err := store.Load("journal", &got); err != nil {
		t.Fatalf("Load after interrupted replace: %v", err)
	}
	if got.Value != wantOld.Value {
		t.Fatalf("interrupted replacement exposed %q, want old value %q", got.Value, wantOld.Value)
	}

	if err := store.Save("journal", testDocument{Value: "new"}); err != nil {
		t.Fatalf("Save after recovery: %v", err)
	}
	if err := store.Load("journal", &got); err != nil {
		t.Fatalf("Load after recovery: %v", err)
	}
	if got.Value != "new" {
		t.Fatalf("recovered value = %q, want new", got.Value)
	}
}

func TestLockExclusion(t *testing.T) {
	store := newTestStore(t)
	first, err := store.AcquireLocks(LockWorkspace)
	if err != nil {
		t.Fatalf("first workspace lock: %v", err)
	}
	defer first.Release()
	lockPath := filepath.Join(store.root, "locks", "workspace.lock")
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatalf("age live lock file: %v", err)
	}
	ownerData, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read private lock owner record: %v", err)
	}
	var owner struct {
		PID       int    `json:"pid"`
		StartedAt string `json:"process_started_at"`
	}
	if err := json.Unmarshal(ownerData, &owner); err != nil {
		t.Fatalf("decode lock owner record: %v", err)
	}
	if owner.PID != os.Getpid() || owner.StartedAt == "" {
		t.Fatalf("lock owner = pid %d, started_at %q; want current PID and process start identity", owner.PID, owner.StartedAt)
	}

	second, err := store.AcquireLocks(LockWorkspace)
	if !errors.Is(err, ErrLocked) {
		if second != nil {
			_ = second.Release()
		}
		t.Fatalf("second workspace lock error = %v, want ErrLocked", err)
	}

	if err := first.Release(); err != nil {
		t.Fatalf("release first lock: %v", err)
	}
	first = nil
	third, err := store.AcquireLocks(LockWorkspace)
	if err != nil {
		t.Fatalf("reacquire after release: %v", err)
	}
	if err := third.Release(); err != nil {
		t.Fatalf("release recovered lock: %v", err)
	}
}

func TestLockRecoveryAfterProcessExit(t *testing.T) {
	if root := os.Getenv("ANZA_STATE_LOCK_CHILD_ROOT"); root != "" {
		store, err := NewStore(root)
		if err != nil {
			os.Exit(2)
		}
		if _, err := store.AcquireLocks(LockWorkspace); err != nil {
			os.Exit(3)
		}
		// Process exit deliberately skips Release; the kernel must drop the lock.
		os.Exit(0)
	}

	store := newTestStore(t)
	cmd := exec.Command(os.Args[0], "-test.run=^TestLockRecoveryAfterProcessExit$")
	cmd.Env = append(os.Environ(), "ANZA_STATE_LOCK_CHILD_ROOT="+store.root)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("lock-owning child exit failed: %v: %s", err, output)
	}
	lock, err := store.AcquireLocks(LockWorkspace)
	if err != nil {
		t.Fatalf("acquire persistent lock after child process exit: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("release recovered lock: %v", err)
	}
}

func TestPrivatePermissions(t *testing.T) {
	store := newTestStore(t)
	if err := store.Save("private", testDocument{Value: "local"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if runtime.GOOS == "windows" {
		t.Skip("ACL creation is enforced by the Windows implementation; ACL inspection needs a native Windows runner")
	}
	for _, path := range []string{store.root, filepath.Join(store.root, "locks"), filepath.Join(store.root, "private.json"), filepath.Join(store.root, "locks", "state-private.lock")} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat(%q): %v", filepath.Base(path), err)
		}
		if info.Mode().Perm()&0o077 != 0 {
			t.Errorf("%s permissions = %04o, want no group/other access", filepath.Base(path), info.Mode().Perm())
		}
	}
}

func TestFutureSchemaPreserved(t *testing.T) {
	store := newTestStore(t)
	path := filepath.Join(store.root, "future.json")
	future := []byte(`{"schema_version":999,"payload":{"value":"future"}}`)
	if err := os.WriteFile(path, future, 0o600); err != nil {
		t.Fatalf("write future state fixture: %v", err)
	}

	var got testDocument
	if err := store.Load("future", &got); !errors.Is(err, ErrFutureSchema) {
		t.Fatalf("Load future schema error = %v, want ErrFutureSchema", err)
	}
	if err := store.Save("future", testDocument{Value: "replacement"}); !errors.Is(err, ErrFutureSchema) {
		t.Fatalf("Save over future schema error = %v, want ErrFutureSchema", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read preserved future state: %v", err)
	}
	if string(after) != string(future) {
		t.Fatalf("future state changed: %s", strings.TrimSpace(string(after)))
	}
}

func TestMalformedStatePreserved(t *testing.T) {
	store := newTestStore(t)
	path := filepath.Join(store.root, "malformed.json")
	malformed := []byte(`{"schema_version":1,"payload":`)
	if err := os.WriteFile(path, malformed, 0o600); err != nil {
		t.Fatalf("write malformed state fixture: %v", err)
	}
	if err := store.Save("malformed", testDocument{Value: "replacement"}); !errors.Is(err, ErrMalformedState) {
		t.Fatalf("Save over malformed state error = %v, want ErrMalformedState", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read preserved malformed state: %v", err)
	}
	if string(after) != string(malformed) {
		t.Fatalf("malformed state changed: %s", strings.TrimSpace(string(after)))
	}
}

func TestLockOrder(t *testing.T) {
	store := newTestStore(t)
	locks, err := store.AcquireLocks(LockWorkspace, LockUserTools)
	if locks != nil {
		_ = locks.Release()
	}
	if !errors.Is(err, ErrLockOrder) {
		t.Fatalf("reversed lock order error = %v, want ErrLockOrder", err)
	}
	locks, err = store.AcquireLocks(LockUserTools, LockWorkspace)
	if err != nil {
		t.Fatalf("documented lock order: %v", err)
	}
	if err := locks.Release(); err != nil {
		t.Fatalf("release ordered locks: %v", err)
	}
}

func TestWorkspaceIDCanonicalPath(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "project")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatalf("Mkdir workspace: %v", err)
	}
	link := filepath.Join(root, "alias")
	if err := os.Symlink(workspace, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	first, err := WorkspaceID(workspace)
	if err != nil {
		t.Fatalf("WorkspaceID(canonical): %v", err)
	}
	second, err := WorkspaceID(link)
	if err != nil {
		t.Fatalf("WorkspaceID(symlink): %v", err)
	}
	if first != second {
		t.Fatalf("canonical and symlink IDs differ: %q != %q", first, second)
	}
}

type testDocument struct {
	Value string `json:"value"`
}
