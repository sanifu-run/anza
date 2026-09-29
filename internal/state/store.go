// Package state provides private, crash-safe local state and process locks.
package state

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/sanifu-run/anza/internal/domain"
)

const schemaVersion = 1

var (
	ErrLocked            = errors.New("state lock is held by another process")
	ErrLockOrder         = errors.New("state locks must be acquired user-tools before workspace")
	ErrFutureSchema      = errors.New("state uses a newer schema and was preserved")
	ErrUnsupportedSchema = errors.New("state schema is not supported and was preserved")
	ErrMalformedState    = errors.New("state is malformed and was preserved")
	ErrInsecureState     = errors.New("state path is not a private regular file or directory")
)

var processStartedAt = time.Now().UTC().Format(time.RFC3339Nano)

type envelope struct {
	SchemaVersion int             `json:"schema_version"`
	Payload       json.RawMessage `json:"payload"`
}

// Store is rooted in Anza's per-user state directory. Pass an explicit root
// only for an application-selected state location or a temporary test root.
type Store struct {
	root string
}

// LockScope names the shared effect locks. AcquireLocks enforces the global
// order: user tool installation first, then workspace changes.
type LockScope string

const (
	LockUserTools LockScope = "user-tools"
	LockWorkspace LockScope = "workspace"
)

// NewStore creates a private state root. It creates no path outside root.
func NewStore(root string) (*Store, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("state root is empty")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve state root: %w", err)
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, fmt.Errorf("create state root: %w", err)
	}
	if err := securePath(abs, true); err != nil {
		return nil, fmt.Errorf("secure state root: %w", err)
	}
	locks := filepath.Join(abs, "locks")
	if err := os.MkdirAll(locks, 0o700); err != nil {
		return nil, fmt.Errorf("create state lock directory: %w", err)
	}
	if err := securePath(locks, true); err != nil {
		return nil, fmt.Errorf("secure state lock directory: %w", err)
	}
	return &Store{root: abs}, nil
}

// OpenUserStore resolves and opens the operating-system per-user state root.
func OpenUserStore() (*Store, error) {
	root, err := UserStateRoot()
	if err != nil {
		return nil, err
	}
	return NewStore(root)
}

// UserStateRoot returns Anza's platform-specific state directory. Linux honors
// an absolute XDG_STATE_HOME; macOS uses Application Support; Windows uses
// LocalAppData. Tests should use NewStore with t.TempDir instead.
func UserStateRoot() (string, error) {
	if runtime.GOOS == "windows" {
		root := os.Getenv("LOCALAPPDATA")
		if root == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("resolve user home for Windows state: %w", err)
			}
			root = filepath.Join(home, "AppData", "Local")
		}
		if !filepath.IsAbs(root) {
			return "", errors.New("LOCALAPPDATA must be absolute")
		}
		return filepath.Join(root, "Sanifu", "Anza", "State"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home for state: %w", err)
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support", "Anza", "State"), nil
	}
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		if !filepath.IsAbs(xdg) {
			return "", errors.New("XDG_STATE_HOME must be absolute")
		}
		return filepath.Join(xdg, "anza"), nil
	}
	return filepath.Join(home, ".local", "state", "anza"), nil
}

// Root reports the canonical absolute state root selected for this Store.
func (s *Store) Root() string { return s.root }

// Save writes a schema-versioned JSON document atomically. Malformed or newer
// existing state is never overwritten. Writes to each state name are serialized.
func (s *Store) Save(name string, value any) error {
	return s.saveWithHook(name, value, nil)
}

func (s *Store) saveWithHook(name string, value any, beforeReplace func() error) (returnErr error) {
	if err := validateName(name); err != nil {
		return err
	}
	lock, err := s.acquireFileLock("state-" + name)
	if err != nil {
		return fmt.Errorf("lock state %q: %w", name, err)
	}
	defer func() {
		if err := lock.release(); err != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("release state lock: %w", err))
		}
	}()

	path := filepath.Join(s.root, name+".json")
	if _, err := s.readEnvelope(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode state %q payload: %w", name, err)
	}
	if bytes.Equal(bytes.TrimSpace(payload), []byte("null")) {
		return errors.New("state payload must not be null")
	}
	data, err := json.Marshal(envelope{SchemaVersion: schemaVersion, Payload: payload})
	if err != nil {
		return fmt.Errorf("encode state %q envelope: %w", name, err)
	}
	if err := atomicWrite(path, data, beforeReplace); err != nil {
		return fmt.Errorf("persist state %q: %w", name, err)
	}
	return nil
}

// Load decodes one schema-versioned JSON document into dst. Malformed and
// newer-schema documents return an error without modifying the source file.
func (s *Store) Load(name string, dst any) (returnErr error) {
	if err := validateName(name); err != nil {
		return err
	}
	lock, err := s.acquireFileLock("state-" + name)
	if err != nil {
		return fmt.Errorf("lock state %q: %w", name, err)
	}
	defer func() {
		if err := lock.release(); err != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("release state lock: %w", err))
		}
	}()
	path := filepath.Join(s.root, name+".json")
	state, err := s.readEnvelope(path)
	if err != nil {
		return err
	}
	if dst == nil {
		return errors.New("state destination is nil")
	}
	dec := json.NewDecoder(bytes.NewReader(state.Payload))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("%w: decode payload: %v", ErrMalformedState, err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("%w: payload has trailing JSON", ErrMalformedState)
	}
	return nil
}

func (s *Store) readEnvelope(path string) (envelope, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return envelope{}, os.ErrNotExist
		}
		return envelope{}, fmt.Errorf("inspect state file: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return envelope{}, fmt.Errorf("%w: %s", ErrInsecureState, filepath.Base(path))
	}
	if err := securePath(path, false); err != nil {
		return envelope{}, fmt.Errorf("secure state file: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return envelope{}, fmt.Errorf("read state file: %w", err)
	}
	canonical, err := domain.CanonicalJSON(data)
	if err != nil {
		return envelope{}, fmt.Errorf("%w: %v", ErrMalformedState, err)
	}
	var state envelope
	dec := json.NewDecoder(bytes.NewReader(canonical))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&state); err != nil {
		return envelope{}, fmt.Errorf("%w: decode envelope: %v", ErrMalformedState, err)
	}
	if state.SchemaVersion == 0 || len(state.Payload) == 0 || bytes.Equal(bytes.TrimSpace(state.Payload), []byte("null")) {
		return envelope{}, fmt.Errorf("%w: schema_version and payload are required", ErrMalformedState)
	}
	if state.SchemaVersion > schemaVersion {
		return envelope{}, fmt.Errorf("%w: found %d, support through %d", ErrFutureSchema, state.SchemaVersion, schemaVersion)
	}
	if state.SchemaVersion < schemaVersion {
		return envelope{}, fmt.Errorf("%w: found %d, support %d", ErrUnsupportedSchema, state.SchemaVersion, schemaVersion)
	}
	return state, nil
}

func validateName(name string) error {
	if name == "" || len(name) > 64 || name == "." || name == ".." {
		return errors.New("state name must be 1 to 64 safe characters")
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._-", r)) {
			return errors.New("state name contains an unsafe character")
		}
	}
	return nil
}

// WorkspaceID hashes a canonical workspace path and its filesystem identity.
// Paths are only inputs to this local digest and are never transmitted.
func WorkspaceID(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("workspace path is empty")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve workspace path: %w", err)
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("canonicalize workspace path: %w", err)
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", fmt.Errorf("inspect workspace: %w", err)
	}
	if !info.IsDir() {
		return "", errors.New("workspace path is not a directory")
	}
	identity, err := filesystemIdentity(canonical, info)
	if err != nil {
		return "", fmt.Errorf("identify workspace filesystem object: %w", err)
	}
	if runtime.GOOS == "windows" {
		canonical = strings.ToLower(filepath.Clean(canonical))
	}
	sum := sha256.Sum256([]byte("anza-workspace-v1\x00" + canonical + "\x00" + identity))
	return hex.EncodeToString(sum[:]), nil
}

// LockSet releases acquired locks in reverse order. OS file locks, rather than
// timestamps, determine liveness; after process death another process can
// acquire the persistent lock file and replace its owner record.
type LockSet struct {
	locks []*fileLock
}

// AcquireLocks obtains shared effect locks in contract order. Either scope may
// be acquired alone; when both are needed, user-tools precedes workspace.
func (s *Store) AcquireLocks(scopes ...LockScope) (*LockSet, error) {
	if len(scopes) == 0 {
		return nil, errors.New("at least one lock scope is required")
	}
	last := 0
	for _, scope := range scopes {
		rank := 0
		switch scope {
		case LockUserTools:
			rank = 1
		case LockWorkspace:
			rank = 2
		default:
			return nil, fmt.Errorf("unknown lock scope %q", scope)
		}
		if rank <= last {
			return nil, ErrLockOrder
		}
		last = rank
	}
	set := &LockSet{}
	for _, scope := range scopes {
		lock, err := s.acquireFileLock(string(scope))
		if err != nil {
			_ = set.Release()
			return nil, fmt.Errorf("acquire %s lock: %w", scope, err)
		}
		set.locks = append(set.locks, lock)
	}
	return set, nil
}

// Release unlocks in reverse acquisition order and returns the first error.
func (s *LockSet) Release() error {
	if s == nil {
		return nil
	}
	var first error
	for i := len(s.locks) - 1; i >= 0; i-- {
		if err := s.locks[i].release(); err != nil && first == nil {
			first = err
		}
	}
	s.locks = nil
	return first
}

func (s *Store) acquireFileLock(name string) (*fileLock, error) {
	path := filepath.Join(s.root, "locks", name+".lock")
	if info, err := os.Lstat(path); err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular()) {
		return nil, fmt.Errorf("%w: %s", ErrInsecureState, filepath.Base(path))
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	if err := securePath(path, false); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("secure lock file: %w", err)
	}
	if err := lockFile(file); err != nil {
		_ = file.Close()
		return nil, err
	}
	owner, err := json.Marshal(struct {
		PID        int    `json:"pid"`
		StartedAt  string `json:"process_started_at"`
		AcquiredAt string `json:"acquired_at"`
	}{os.Getpid(), processStartedAt, time.Now().UTC().Format(time.RFC3339Nano)})
	if err == nil {
		err = file.Truncate(0)
	}
	if err == nil {
		_, err = file.Seek(0, io.SeekStart)
	}
	if err == nil {
		_, err = file.Write(owner)
	}
	if err == nil {
		err = file.Sync()
	}
	if err != nil {
		_ = unlockFile(file)
		_ = file.Close()
		return nil, fmt.Errorf("write lock owner record: %w", err)
	}
	return &fileLock{file: file}, nil
}

type fileLock struct{ file *os.File }

func (l *fileLock) release() error {
	if l == nil || l.file == nil {
		return nil
	}
	unlockErr := unlockFile(l.file)
	closeErr := l.file.Close()
	l.file = nil
	if unlockErr != nil {
		return fmt.Errorf("unlock state file: %w", unlockErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close state lock: %w", closeErr)
	}
	return nil
}
