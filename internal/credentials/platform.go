package credentials

import (
	"errors"
	"runtime"

	"github.com/byteness/keyring"
)

// NewPlatformStore opens only the native credential backend for the current
// operating system. If it cannot be opened, it returns a session-only store
// together with ErrUnavailable so the caller can warn that values will not be
// persisted.
func NewPlatformStore() (Store, error) {
	return openPlatformStore(openNativeBackend)
}

func openPlatformStore(open func() (Backend, error)) (Store, error) {
	backend, err := open()
	if err != nil {
		return NewSessionStore(), ErrUnavailable
	}
	store, err := NewNativeStore(backend)
	if err != nil {
		return NewSessionStore(), ErrUnavailable
	}
	return store, nil
}

func openNativeBackend() (Backend, error) {
	return openNativeBackendForOS(runtime.GOOS, keyring.Open)
}

func openNativeBackendForOS(goos string, open func(keyring.Config) (keyring.Keyring, error)) (Backend, error) {
	backendType, ok := nativeBackendForOS(goos)
	if !ok {
		return nil, ErrUnavailable
	}
	keyringStore, err := open(keyring.Config{
		ServiceName:     serviceName,
		AllowedBackends: []keyring.BackendType{backendType},
	})
	if err != nil {
		return nil, ErrUnavailable
	}
	if keyringStore == nil {
		return nil, ErrUnavailable
	}
	return &keyringBackend{store: keyringStore}, nil
}

func nativeBackendForOS(goos string) (keyring.BackendType, bool) {
	switch goos {
	case "darwin":
		return keyring.KeychainBackend, true
	case "windows":
		return keyring.WinCredBackend, true
	case "linux":
		return keyring.SecretServiceBackend, true
	default:
		return keyring.InvalidBackend, false
	}
}

type keyringBackend struct {
	store keyring.Keyring
}

func (b *keyringBackend) Put(key string, secret []byte) error {
	value := append([]byte(nil), secret...)
	defer clear(value)
	return b.store.Set(keyring.Item{Key: key, Data: value})
}

func (b *keyringBackend) Get(key string) ([]byte, error) {
	item, err := b.store.Get(key)
	if err != nil {
		if errors.Is(err, keyring.ErrKeyNotFound) {
			return nil, ErrNotFound
		}
		return nil, ErrUnavailable
	}
	value := append([]byte(nil), item.Data...)
	clear(item.Data)
	return value, nil
}

func (b *keyringBackend) Delete(key string) error {
	if err := b.store.Remove(key); err != nil {
		if errors.Is(err, keyring.ErrKeyNotFound) {
			return ErrNotFound
		}
		return ErrUnavailable
	}
	return nil
}
