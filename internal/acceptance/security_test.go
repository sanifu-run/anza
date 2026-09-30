package acceptance

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"testing/fstest"

	"github.com/sanifu-run/anza/internal/brief"
	"github.com/sanifu-run/anza/internal/catalog"
	"github.com/sanifu-run/anza/internal/download"
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
