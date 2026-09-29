package credentials

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/byteness/keyring"
)

func TestCredentialLifecycle(t *testing.T) {
	backend := &fakeBackend{items: make(map[string][]byte)}
	store, err := NewNativeStore(backend)
	if err != nil {
		t.Fatal("NewNativeStore returned an error")
	}

	id, err := NewCredentialID("openrouter", "workspace-1")
	if err != nil {
		t.Fatal("NewCredentialID returned an error")
	}

	input := []byte("synthetic-secret")
	if err := store.Put(id, NewSecret(input)); err != nil {
		t.Fatal("Put returned an error")
	}
	input[0] = 'X'

	got, err := store.Get(id)
	if err != nil {
		t.Fatal("Get returned an error")
	}
	if string(got.Bytes()) != "synthetic-secret" {
		t.Fatal("Get did not return the stored value")
	}
	gotBytes := got.Bytes()
	gotBytes[0] = 'Y'
	again, err := store.Get(id)
	if err != nil || string(again.Bytes()) != "synthetic-secret" {
		t.Fatal("Get exposed the store's mutable value")
	}

	if err := store.Delete(id); err != nil {
		t.Fatal("Delete returned an error")
	}
	if _, err := store.Get(id); !errors.Is(err, ErrNotFound) {
		t.Fatal("Get after Delete did not return ErrNotFound")
	}
}

func TestSessionStoreLifecycle(t *testing.T) {
	store := NewSessionStore()
	defer store.Close()
	id, err := NewCredentialID("openrouter", "workspace-1")
	if err != nil {
		t.Fatal("NewCredentialID returned an error")
	}
	if err := store.Put(id, NewSecret([]byte("session-only-secret"))); err != nil {
		t.Fatal("session-only Put returned an error")
	}
	got, err := store.Get(id)
	if err != nil || string(got.Bytes()) != "session-only-secret" {
		t.Fatal("session-only Get did not return the stored value")
	}
	if err := store.Delete(id); err != nil {
		t.Fatal("session-only Delete returned an error")
	}
}

func TestUnavailableStore(t *testing.T) {
	store := NewUnavailableStore()
	id, err := NewCredentialID("openrouter", "workspace-1")
	if err != nil {
		t.Fatal("NewCredentialID returned an error")
	}

	if err := store.Put(id, NewSecret([]byte("synthetic-secret"))); !errors.Is(err, ErrUnavailable) {
		t.Fatal("Put did not report the platform store as unavailable")
	}
	if _, err := store.Get(id); !errors.Is(err, ErrUnavailable) {
		t.Fatal("Get did not report the platform store as unavailable")
	}
	if err := store.Delete(id); !errors.Is(err, ErrUnavailable) {
		t.Fatal("Delete did not report the platform store as unavailable")
	}
}

func TestNativeStoreSanitizesBackendErrors(t *testing.T) {
	secretValue := "backend-error-secret"
	partialValue := []byte("partial-secret")
	id, err := NewCredentialID("openrouter", "private-workspace")
	if err != nil {
		t.Fatal("NewCredentialID returned an error")
	}
	backend, err := NewNativeStore(&fakeBackend{
		items:   make(map[string][]byte),
		failure: fmt.Errorf("backend failed with %s", secretValue),
		partial: partialValue,
	})
	if err != nil {
		t.Fatal("NewNativeStore returned an error")
	}
	if err := backend.Put(id, NewSecret([]byte(secretValue))); !errors.Is(err, ErrUnavailable) {
		t.Fatal("Put did not sanitize backend failure")
	} else if strings.Contains(err.Error(), secretValue) || strings.Contains(err.Error(), "private-workspace") {
		t.Fatal("Put error exposed a secret or credential scope")
	}
	if _, err := backend.Get(id); !errors.Is(err, ErrUnavailable) {
		t.Fatal("Get did not sanitize backend failure")
	} else if strings.Contains(err.Error(), secretValue) || strings.Contains(err.Error(), "private-workspace") {
		t.Fatal("Get error exposed a secret or credential scope")
	}
	for _, b := range partialValue {
		if b != 0 {
			t.Fatal("Get did not clear bytes returned alongside an error")
		}
	}
	if err := backend.Delete(id); !errors.Is(err, ErrUnavailable) {
		t.Fatal("Delete did not sanitize backend failure")
	} else if strings.Contains(err.Error(), secretValue) || strings.Contains(err.Error(), "private-workspace") {
		t.Fatal("Delete error exposed a secret or credential scope")
	}
}

func TestPlatformStoreFallsBackToSessionOnly(t *testing.T) {
	secretValue := "synthetic-secret"
	store, err := openPlatformStore(func() (Backend, error) {
		return nil, fmt.Errorf("backend failure included %s", secretValue)
	})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatal("unavailable native backend did not return ErrUnavailable")
	}
	if strings.Contains(err.Error(), secretValue) {
		t.Fatal("native backend error leaked through the unavailable result")
	}
	session, ok := store.(*SessionStore)
	if !ok {
		t.Fatal("unavailable native backend did not return a session-only store")
	}
	defer session.Close()

	id, err := NewCredentialID("openrouter", "workspace-1")
	if err != nil {
		t.Fatal("NewCredentialID returned an error")
	}
	if err := store.Put(id, NewSecret([]byte(secretValue))); err != nil {
		t.Fatal("session fallback did not accept a credential")
	}
	got, err := store.Get(id)
	if err != nil || string(got.Bytes()) != secretValue {
		t.Fatal("session fallback did not retain a credential for this process")
	}
}

func TestNativeBackendSelectionIsNarrow(t *testing.T) {
	for goos, want := range map[string]string{
		"darwin":  "keychain",
		"windows": "wincred",
		"linux":   "secret-service",
	} {
		got, ok := nativeBackendForOS(goos)
		if !ok || string(got) != want {
			t.Fatalf("nativeBackendForOS(%q) = %q, %v; want %q, true", goos, got, ok, want)
		}
	}
	for _, goos := range []string{"freebsd", "plan9", "js"} {
		if backend, ok := nativeBackendForOS(goos); ok || backend != "" {
			t.Fatalf("nativeBackendForOS(%q) selected non-approved backend %q", goos, backend)
		}
	}
}

func TestPlatformStartupUsesOnlyNativeBackend(t *testing.T) {
	for goos, want := range map[string]keyring.BackendType{
		"darwin":  keyring.KeychainBackend,
		"windows": keyring.WinCredBackend,
		"linux":   keyring.SecretServiceBackend,
	} {
		var gotConfig keyring.Config
		backend, err := openNativeBackendForOS(goos, func(config keyring.Config) (keyring.Keyring, error) {
			gotConfig = config
			return &fakeKeyring{items: make(map[string][]byte)}, nil
		})
		if err != nil {
			t.Fatal("openNativeBackendForOS returned an error")
		}
		if gotConfig.ServiceName != "anza" {
			t.Fatal("platform startup did not use the Anza service namespace")
		}
		if len(gotConfig.AllowedBackends) != 1 || gotConfig.AllowedBackends[0] != want {
			t.Fatal("platform startup did not restrict selection to one native backend")
		}
		if backend == nil {
			t.Fatal("platform startup returned a nil adapter")
		}
	}
}

func TestCredentialIDSupportsOpaqueWorkspacePaths(t *testing.T) {
	workspace := "/private/workspaces/example"
	id, err := NewCredentialID("openrouter", workspace)
	if err != nil {
		t.Fatal("NewCredentialID rejected a workspace path")
	}
	if strings.Contains(id.backendKey(), workspace) {
		t.Fatal("native credential target exposed the workspace path")
	}
}

func TestNoCredentialSerialization(t *testing.T) {
	secretValue := "serialization-must-not-leak"
	secret := NewSecret([]byte(secretValue))
	formatted := fmt.Sprintf("%v %s %+v %#v %q %x", secret, secret, secret, secret, secret, secret)
	if strings.Contains(formatted, secretValue) {
		t.Fatal("secret formatting exposed the value")
	}

	encoded, err := json.Marshal(secret)
	if err != nil {
		t.Fatal("json.Marshal returned an error")
	}
	if strings.Contains(string(encoded), secretValue) {
		t.Fatal("secret JSON exposed the value")
	}

	id, err := NewCredentialID("provider-name", "private-workspace-name")
	if err != nil {
		t.Fatal("NewCredentialID returned an error")
	}
	idOutput := fmt.Sprintf("%v %+v %#v", id, id, id)
	idJSON, err := json.Marshal(id)
	if err != nil {
		t.Fatal("json.Marshal returned an error")
	}
	for _, output := range []string{idOutput, string(idJSON)} {
		if strings.Contains(output, "private-workspace-name") || strings.Contains(output, "provider-name") {
			t.Fatal("credential ID formatting exposed its scope")
		}
	}

	storeSecret := "store-state-secret"
	store, err := NewNativeStore(&fakeBackend{items: map[string][]byte{"native-test-target": []byte(storeSecret)}})
	if err != nil {
		t.Fatal("NewNativeStore returned an error")
	}
	session := NewSessionStore()
	defer session.Close()
	if err := session.Put(id, NewSecret([]byte(storeSecret))); err != nil {
		t.Fatal("session Put returned an error")
	}
	for _, value := range []any{store, session} {
		formatted := fmt.Sprintf("%v %+v %#v", value, value, value)
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal("store json.Marshal returned an error")
		}
		if strings.Contains(formatted, storeSecret) || strings.Contains(string(encoded), storeSecret) {
			t.Fatal("store formatting or JSON exposed an internal credential")
		}
	}
}

func TestOwnershipLimitedDelete(t *testing.T) {
	id, err := NewCredentialID("openrouter", "workspace-1")
	if err != nil {
		t.Fatal("NewCredentialID returned an error")
	}
	foreignKeys := []string{"codex:native-login", "claude:native-login"}
	backend := &fakeBackend{items: map[string][]byte{
		foreignKeys[0]: []byte("codex-secret"),
		foreignKeys[1]: []byte("claude-secret"),
	}}
	store, err := NewNativeStore(backend)
	if err != nil {
		t.Fatal("NewNativeStore returned an error")
	}

	if err := store.Put(id, NewSecret([]byte("synthetic-secret"))); err != nil {
		t.Fatal("Put returned an error")
	}
	if err := store.Delete(id); err != nil {
		t.Fatal("Delete returned an error")
	}
	for _, foreignKey := range foreignKeys {
		if _, ok := backend.items[foreignKey]; !ok {
			t.Fatal("Delete removed a credential outside Anza's namespace")
		}
	}
	if len(backend.deleted) != 1 || backend.deleted[0] != id.backendKey() {
		t.Fatal("Delete did not target only the derived Anza credential key")
	}

	if err := store.Delete(CredentialID{}); !errors.Is(err, ErrInvalidID) {
		t.Fatal("Delete accepted an invalid, unowned credential ID")
	}
	if len(backend.deleted) != 1 {
		t.Fatal("invalid ID reached the backend")
	}
}

type fakeBackend struct {
	items   map[string][]byte
	deleted []string
	failure error
	partial []byte
}

type fakeKeyring struct {
	items map[string][]byte
}

func (f *fakeKeyring) Get(key string) (keyring.Item, error) {
	value, ok := f.items[key]
	if !ok {
		return keyring.Item{}, keyring.ErrKeyNotFound
	}
	return keyring.Item{Key: key, Data: append([]byte(nil), value...)}, nil
}

func (f *fakeKeyring) GetMetadata(string) (keyring.Metadata, error) {
	return keyring.Metadata{}, nil
}

func (f *fakeKeyring) Keys() ([]string, error) {
	keys := make([]string, 0, len(f.items))
	for key := range f.items {
		keys = append(keys, key)
	}
	return keys, nil
}

func (f *fakeKeyring) Remove(key string) error {
	if _, ok := f.items[key]; !ok {
		return keyring.ErrKeyNotFound
	}
	delete(f.items, key)
	return nil
}

func (f *fakeKeyring) Set(item keyring.Item) error {
	f.items[item.Key] = append([]byte(nil), item.Data...)
	return nil
}

func (f *fakeBackend) Put(key string, value []byte) error {
	if f.failure != nil {
		return f.failure
	}
	f.items[key] = append([]byte(nil), value...)
	return nil
}

func (f *fakeBackend) Get(key string) ([]byte, error) {
	if f.failure != nil {
		return f.partial, f.failure
	}
	value, ok := f.items[key]
	if !ok {
		return nil, ErrNotFound
	}
	return append([]byte(nil), value...), nil
}

func (f *fakeBackend) Delete(key string) error {
	if f.failure != nil {
		return f.failure
	}
	f.deleted = append(f.deleted, key)
	delete(f.items, key)
	return nil
}
