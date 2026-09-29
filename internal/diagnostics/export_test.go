package diagnostics

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiagnosticsAllowlist(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "diagnostics.json")
	req := Request{Destination: destination, Metadata: Metadata{
		Version: "1.2.3", Platform: "darwin-arm64", Status: "ready", Operation: "readiness",
	}}
	preview, err := PreviewRequest(req)
	if err != nil {
		t.Fatal("PreviewRequest returned an error")
	}
	if preview.Network || len(preview.Fields) != 4 || strings.Contains(preview.Destination, destination) {
		t.Fatalf("preview exposed extra information: %#v", preview)
	}
	if _, err := os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("preview wrote an export")
	}
	if err := Export(req); err != nil {
		t.Fatal("Export returned an error")
	}
	info, err := os.Stat(destination)
	if err != nil {
		t.Fatal("export file was not created")
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("permissions = %o, want 600", info.Mode().Perm())
	}
	data, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal("could not read synthetic export")
	}
	if err := Export(req); !errors.Is(err, ErrWrite) {
		t.Fatal("export overwrote an existing file")
	}
	after, err := os.ReadFile(destination)
	if err != nil || !bytes.Equal(data, after) {
		t.Fatal("existing destination changed after a second export request")
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal("export is not valid JSON")
	}
	if len(got) != 2 {
		t.Fatalf("unexpected top-level fields: %#v", got)
	}
	metadata, ok := got["metadata"].(map[string]any)
	if !ok || len(metadata) != 4 {
		t.Fatalf("unexpected metadata fields: %#v", got["metadata"])
	}
}

func TestDiagnosticsSecretSeeds(t *testing.T) {
	seed := "sk-live-synthetic-secret-123"
	req := Request{Destination: filepath.Join(t.TempDir(), "diagnostics.json"), Metadata: Metadata{
		Version: "1.2.3", Platform: "darwin-arm64", Status: seed, Operation: "setup",
	}}
	err := Export(req)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatal("secret-bearing metadata should be rejected")
	}
	if strings.Contains(err.Error(), seed) {
		t.Fatal("export error exposed the synthetic secret")
	}
	root := t.TempDir()
	secretPath := filepath.Join(root, seed)
	if err := os.WriteFile(secretPath, []byte("synthetic existing file"), 0o600); err != nil {
		t.Fatal("could not create synthetic existing-file fixture")
	}
	req = Request{Destination: secretPath, Metadata: Metadata{
		Version: "1.2.3", Platform: "darwin-arm64", Status: "failed", Operation: "setup",
	}}
	err = Export(req)
	if !errors.Is(err, ErrWrite) || strings.Contains(err.Error(), seed) {
		t.Fatal("filesystem error exposed the synthetic path secret")
	}
	if err := Export(Request{Destination: filepath.Join(t.TempDir(), "out.json"), Metadata: Metadata{
		Version: "1.2.3", Platform: "darwin-arm64", Status: "failed", Operation: "setup",
	}}); err != nil {
		t.Fatal("safe synthetic metadata should export")
	}
}

func TestDeleteSessionOnly(t *testing.T) {
	root := t.TempDir()
	session := filepath.Join(root, "interview-demo.json")
	project := filepath.Join(root, "project-source.txt")
	credential := filepath.Join(root, "vendor-auth.json")
	for path, value := range map[string]string{
		session:    "synthetic session token fixture",
		project:    "synthetic project content",
		credential: "synthetic vendor credential",
	} {
		if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
			t.Fatal("could not create synthetic fixture")
		}
	}
	if err := DeleteLocalSession(root, "demo"); err != nil {
		t.Fatal("could not delete synthetic session")
	}
	if _, err := os.Stat(session); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("session state still exists")
	}
	for _, path := range []string{project, credential} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("unrelated fixture was removed: %s", filepath.Base(path))
		}
	}
}

func TestDeleteSessionCallsRemoteBeforeLocal(t *testing.T) {
	order := []string{}
	ctx := context.Background()
	err := DeleteSession(ctx, "synthetic-recovery-token", DeleteCallbacks{
		Remote: func(_ context.Context, token string) error {
			if token != "synthetic-recovery-token" {
				t.Fatal("remote callback did not receive the recovery token")
			}
			order = append(order, "remote")
			return nil
		},
		Local: func() error { order = append(order, "local"); return nil },
	})
	if err != nil || strings.Join(order, ",") != "remote,local" {
		t.Fatal("delete callbacks did not complete in the required order")
	}
	order = nil
	remoteErr := errors.New("synthetic remote failure containing a secret")
	err = DeleteSession(ctx, "synthetic-recovery-token", DeleteCallbacks{
		Remote: func(context.Context, string) error { order = append(order, "remote"); return remoteErr },
		Local:  func() error { order = append(order, "local"); return nil },
	})
	if !errors.Is(err, ErrDelete) || strings.Contains(err.Error(), "secret") || strings.Join(order, ",") != "remote" {
		t.Fatal("remote error should stay redacted and preserve local recovery state")
	}
}
