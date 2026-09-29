// Package credentials stores provider credentials without exposing their values
// through formatting, JSON, or error messages.
package credentials

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"
	"unicode"
)

var (
	ErrUnavailable = errors.New("credential store unavailable")
	ErrNotFound    = errors.New("credential not found")
	ErrInvalidID   = errors.New("invalid credential ID")
	ErrClosed      = errors.New("credential session store closed")
)

const serviceName = "anza"

// CredentialID scopes a credential to one provider and one workspace. Its
// internal representation is deliberately private so callers cannot address
// arbitrary native credential targets.
type CredentialID struct {
	provider  string
	workspace string
}

// NewCredentialID creates a provider/workspace-scoped Anza credential ID.
func NewCredentialID(provider, workspace string) (CredentialID, error) {
	if !validScope(provider) || !validScope(workspace) {
		return CredentialID{}, ErrInvalidID
	}
	return CredentialID{provider: provider, workspace: workspace}, nil
}

func validScope(value string) bool {
	if value == "" || len(value) > 4096 || strings.TrimSpace(value) != value {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func (id CredentialID) valid() bool {
	return validScope(id.provider) && validScope(id.workspace)
}

// backendKey is a deterministic, opaque target restricted to Anza's service
// namespace. Native adapters must pair this key only with serviceName.
func (id CredentialID) backendKey() string {
	if !id.valid() {
		return ""
	}
	h := sha256.Sum256([]byte(id.provider + "\x00" + id.workspace))
	return "credential:v1:" + base64.RawURLEncoding.EncodeToString(h[:])
}

func (CredentialID) String() string   { return "[credential-id]" }
func (CredentialID) GoString() string { return "[credential-id]" }
func (CredentialID) Format(state fmt.State, _ rune) {
	_, _ = io.WriteString(state, "[credential-id]")
}
func (CredentialID) MarshalJSON() ([]byte, error) { return []byte(`"[credential-id]"`), nil }

// Secret is a credential value with redacted formatting and serialization.
// Use NewSecret to take an owned copy and Bytes to obtain a separate copy.
type Secret []byte

func NewSecret(value []byte) Secret {
	return append(Secret(nil), value...)
}

// Bytes returns a copy so callers cannot mutate the store's retained value.
func (s Secret) Bytes() []byte { return append([]byte(nil), s...) }

// Clear overwrites this secret buffer and releases it. Go does not guarantee
// that the compiler or runtime removes all copies from memory.
func (s *Secret) Clear() {
	if s == nil {
		return
	}
	for i := range *s {
		(*s)[i] = 0
	}
	*s = nil
}

func (Secret) String() string   { return "[REDACTED]" }
func (Secret) GoString() string { return "[REDACTED]" }
func (Secret) Error() string    { return "[REDACTED]" }
func (Secret) Format(state fmt.State, _ rune) {
	_, _ = io.WriteString(state, "[REDACTED]")
}
func (Secret) MarshalJSON() ([]byte, error) { return []byte(`"[REDACTED]"`), nil }
func (Secret) MarshalText() ([]byte, error) { return []byte("[REDACTED]"), nil }

// Store contains credential operations addressed only by scoped IDs.
type Store interface {
	Put(id CredentialID, secret Secret) error
	Get(id CredentialID) (Secret, error)
	Delete(id CredentialID) error
}

// Backend is the narrow native-keyring seam. Implementations must use only
// Anza's service namespace and must never select a plaintext file backend.
type Backend interface {
	Put(key string, secret []byte) error
	Get(key string) ([]byte, error)
	Delete(key string) error
}

// NativeStore adapts a native keyring backend to the credential Store API.
type NativeStore struct {
	backend Backend
}

func NewNativeStore(backend Backend) (*NativeStore, error) {
	if isNilBackend(backend) {
		return nil, ErrUnavailable
	}
	return &NativeStore{backend: backend}, nil
}

func (s *NativeStore) Put(id CredentialID, secret Secret) error {
	key, err := id.nativeKey()
	if err != nil {
		return err
	}
	value := secret.Bytes()
	defer clear(value)
	if err := s.backend.Put(key, value); err != nil {
		return ErrUnavailable
	}
	return nil
}

func (s *NativeStore) Get(id CredentialID) (Secret, error) {
	key, err := id.nativeKey()
	if err != nil {
		return nil, err
	}
	value, err := s.backend.Get(key)
	defer clear(value)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, ErrUnavailable
	}
	secret := NewSecret(value)
	return secret, nil
}

func (s *NativeStore) Delete(id CredentialID) error {
	key, err := id.nativeKey()
	if err != nil {
		return err
	}
	if err := s.backend.Delete(key); err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrNotFound
		}
		return ErrUnavailable
	}
	return nil
}

func (*NativeStore) String() string   { return "[credential-store]" }
func (*NativeStore) GoString() string { return "[credential-store]" }
func (*NativeStore) Format(state fmt.State, _ rune) {
	_, _ = io.WriteString(state, "[credential-store]")
}

func (id CredentialID) nativeKey() (string, error) {
	key := id.backendKey()
	if key == "" {
		return "", ErrInvalidID
	}
	return key, nil
}

// UnavailableStore explicitly reports that no native store is usable.
type UnavailableStore struct{}

func NewUnavailableStore() UnavailableStore               { return UnavailableStore{} }
func (UnavailableStore) Put(CredentialID, Secret) error   { return ErrUnavailable }
func (UnavailableStore) Get(CredentialID) (Secret, error) { return nil, ErrUnavailable }
func (UnavailableStore) Delete(CredentialID) error        { return ErrUnavailable }
func (UnavailableStore) String() string                   { return "[credential-store unavailable]" }
func (UnavailableStore) GoString() string                 { return "[credential-store unavailable]" }
func (UnavailableStore) Format(state fmt.State, _ rune) {
	_, _ = io.WriteString(state, "[credential-store unavailable]")
}

// SessionStore keeps credentials in memory only. Close clears values owned by
// the store; callers should also clear their own copies when they are done.
type SessionStore struct {
	mu     sync.Mutex
	items  map[CredentialID]Secret
	closed bool
}

func NewSessionStore() *SessionStore {
	return &SessionStore{items: make(map[CredentialID]Secret)}
}

func (*SessionStore) String() string   { return "[credential-store session-only]" }
func (*SessionStore) GoString() string { return "[credential-store session-only]" }
func (*SessionStore) Format(state fmt.State, _ rune) {
	_, _ = io.WriteString(state, "[credential-store session-only]")
}

func (s *SessionStore) Put(id CredentialID, secret Secret) error {
	if !id.valid() {
		return ErrInvalidID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	if old, ok := s.items[id]; ok {
		old.Clear()
	}
	s.items[id] = NewSecret(secret)
	return nil
}

func (s *SessionStore) Get(id CredentialID) (Secret, error) {
	if !id.valid() {
		return nil, ErrInvalidID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrClosed
	}
	secret, ok := s.items[id]
	if !ok {
		return nil, ErrNotFound
	}
	return NewSecret(secret), nil
}

func (s *SessionStore) Delete(id CredentialID) error {
	if !id.valid() {
		return ErrInvalidID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	secret, ok := s.items[id]
	if !ok {
		return ErrNotFound
	}
	secret.Clear()
	delete(s.items, id)
	return nil
}

func (s *SessionStore) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	for id, secret := range s.items {
		secret.Clear()
		delete(s.items, id)
	}
	s.closed = true
}

func isNilBackend(backend Backend) bool {
	if backend == nil {
		return true
	}
	value := reflect.ValueOf(backend)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
