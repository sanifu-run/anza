package download

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDigestBeforeInstall(t *testing.T) {
	payload := []byte("verified payload")
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer server.Close()
	client := server.Client()
	cache := t.TempDir()
	wrong := strings.Repeat("0", 64)
	_, err := Fetch(context.Background(), client, Artifact{URL: server.URL, SHA256: wrong, Size: int64(len(payload))}, []string{hostPort(server)}, 1024, cache)
	if err == nil {
		t.Fatal("wrong digest was accepted")
	}
	entries, err := os.ReadDir(cache)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed verification left cache files: %v", entries)
	}

	artifact := Artifact{URL: server.URL, SHA256: digest(payload), Size: int64(len(payload))}
	path, err := Fetch(context.Background(), client, artifact, []string{hostPort(server)}, 1024, cache)
	if err != nil {
		t.Fatalf("Fetch valid artifact: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("cached payload = %q, err = %v", got, err)
	}
}

func TestRedirectPolicy(t *testing.T) {
	var unapprovedHits atomic.Int64
	payload := []byte("should not be fetched")
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host == "trusted.invalid" {
			return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{"https://untrusted.invalid/payload"}}, Body: io.NopCloser(strings.NewReader("")), Request: request}, nil
		}
		unapprovedHits.Add(1)
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(payload)), ContentLength: int64(len(payload)), Request: request}, nil
	})}
	cache := t.TempDir()
	_, err := Fetch(context.Background(), client, Artifact{URL: "https://trusted.invalid/start", SHA256: digest(payload), Size: int64(len(payload))}, []string{"trusted.invalid"}, 1024, cache)
	if err == nil {
		t.Fatal("redirect to unapproved host was accepted")
	}
	if hits := unapprovedHits.Load(); hits != 0 {
		t.Fatalf("unapproved redirect destination received %d requests", hits)
	}
	entries, readErr := os.ReadDir(cache)
	if readErr != nil || len(entries) != 0 {
		t.Fatalf("redirect rejection left cache entries: entries=%v err=%v", entries, readErr)
	}
}

func TestFetchBoundsAndCache(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), 128)
	var requests atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = w.Write(payload)
	}))
	defer server.Close()
	client := server.Client()
	cache := t.TempDir()
	artifact := Artifact{URL: server.URL, SHA256: digest(payload), Size: int64(len(payload))}
	oversized := artifact
	oversized.Size = 64
	if _, err := Fetch(context.Background(), client, oversized, []string{hostPort(server)}, 64, cache); err == nil {
		t.Fatal("oversized body was accepted")
	}
	entries, err := os.ReadDir(cache)
	if err != nil || len(entries) != 0 {
		t.Fatalf("oversized body left cache entry: entries=%v err=%v", entries, err)
	}

	if _, err := Fetch(context.Background(), client, artifact, []string{hostPort(server)}, 256, cache); err != nil {
		t.Fatalf("Fetch within limit: %v", err)
	}
	broken := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network must not be used for a verified cache hit")
	})}
	if _, err := Fetch(context.Background(), broken, artifact, []string{hostPort(server)}, 256, cache); err != nil {
		t.Fatalf("verified cache did not work offline: %v", err)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("network request count = %d, want 2 (oversize and initial valid fetch)", got)
	}
}

func TestArchiveTraversalMatrix(t *testing.T) {
	tests := []struct {
		name   string
		format Format
		data   []byte
	}{
		{name: "zip parent traversal", format: FormatZip, data: makeZip(t, zipEntry{name: "../escape", data: []byte("x")})},
		{name: "zip absolute", format: FormatZip, data: makeZip(t, zipEntry{name: "/absolute", data: []byte("x")})},
		{name: "zip windows drive", format: FormatZip, data: makeZip(t, zipEntry{name: `C:\outside`, data: []byte("x")})},
		{name: "zip UNC", format: FormatZip, data: makeZip(t, zipEntry{name: `\\server\share\x`, data: []byte("x")})},
		{name: "zip Windows device name", format: FormatZip, data: makeZip(t, zipEntry{name: "CON.txt", data: []byte("x")})},
		{name: "zip COM superscript one", format: FormatZip, data: makeZip(t, zipEntry{name: "COM¹.txt", data: []byte("x")})},
		{name: "zip COM superscript two", format: FormatZip, data: makeZip(t, zipEntry{name: "COM².txt", data: []byte("x")})},
		{name: "zip COM superscript three", format: FormatZip, data: makeZip(t, zipEntry{name: "COM³.txt", data: []byte("x")})},
		{name: "zip LPT superscript one", format: FormatZip, data: makeZip(t, zipEntry{name: "LPT¹.txt", data: []byte("x")})},
		{name: "zip LPT superscript two", format: FormatZip, data: makeZip(t, zipEntry{name: "LPT².txt", data: []byte("x")})},
		{name: "zip LPT superscript three", format: FormatZip, data: makeZip(t, zipEntry{name: "LPT³.txt", data: []byte("x")})},
		{name: "zip case-fold collision", format: FormatZip, data: makeZip(t, zipEntry{name: "Bin/tool", data: []byte("a")}, zipEntry{name: "bin/TOOL", data: []byte("b")})},
		{name: "zip conflicting parent", format: FormatZip, data: makeZip(t, zipEntry{name: "node", data: []byte("file")}, zipEntry{name: "node/child", data: []byte("x")})},
		{name: "zip escaping symlink", format: FormatZip, data: makeZip(t, zipEntry{name: "link", mode: os.ModeSymlink | 0777, data: []byte("../../outside")})},
		{name: "tar hardlink escape", format: FormatTarGzip, data: makeTarGzip(t, tarEntry{name: "link", typeflag: tar.TypeLink, link: "../../outside"})},
		{name: "tar symlink escape", format: FormatTarGzip, data: makeTarGzip(t, tarEntry{name: "link", typeflag: tar.TypeSymlink, link: "../../outside"})},
		{name: "tar device", format: FormatTarGzip, data: makeTarGzip(t, tarEntry{name: "device", typeflag: tar.TypeChar, mode: 0600})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			archive := filepath.Join(root, "fixture.archive")
			if err := os.WriteFile(archive, tt.data, 0600); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(root, "install")
			err := Extract(context.Background(), archive, target, tt.format, Limits{MaxEntries: 20, MaxBytes: 1024, MaxFileBytes: 512})
			if err == nil {
				t.Fatal("unsafe archive was accepted")
			}
			if _, statErr := os.Lstat(target); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("target changed before archive validation completed: stat err=%v", statErr)
			}
		})
	}
}

func TestBoundedExtraction(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "valid.zip")
	data := makeZip(t, zipEntry{name: "bin/tool", data: []byte("executable"), mode: 0755}, zipEntry{name: "share/readme", data: []byte("docs")})
	if err := os.WriteFile(archive, data, 0600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "installed")
	if err := Extract(context.Background(), archive, target, FormatZip, Limits{MaxEntries: 4, MaxBytes: 32, MaxFileBytes: 16}); err != nil {
		t.Fatalf("Extract valid archive: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(target, "bin", "tool"))
	if err != nil || string(got) != "executable" {
		t.Fatalf("extracted tool = %q, err = %v", got, err)
	}
	info, err := os.Stat(filepath.Join(target, "bin", "tool"))
	if err != nil || info.Mode().Perm() != 0755 {
		t.Fatalf("executable mode = %v, err = %v", info.Mode(), err)
	}

	tooLarge := filepath.Join(root, "large.zip")
	if err := os.WriteFile(tooLarge, makeZip(t, zipEntry{name: "huge", data: bytes.Repeat([]byte("z"), 256)}), 0600); err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(root, "blocked")
	err = Extract(context.Background(), tooLarge, blocked, FormatZip, Limits{MaxEntries: 5, MaxBytes: 100, MaxFileBytes: 80})
	if err == nil {
		t.Fatal("decompression limit was not enforced")
	}
	if _, statErr := os.Lstat(blocked); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("limit failure created target: %v", statErr)
	}

	largeTar := filepath.Join(root, "large.tar.gz")
	if err := os.WriteFile(largeTar, makeTarGzip(t, tarEntry{name: "huge", data: bytes.Repeat([]byte("z"), 256)}), 0600); err != nil {
		t.Fatal(err)
	}
	blockedTar := filepath.Join(root, "blocked-tar")
	err = Extract(context.Background(), largeTar, blockedTar, FormatTarGzip, Limits{MaxEntries: 5, MaxBytes: 100, MaxFileBytes: 80})
	if err == nil {
		t.Fatal("tar decompression limit was not enforced")
	}
	if _, statErr := os.Lstat(blockedTar); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("tar limit failure created target: %v", statErr)
	}
}

func TestFetchDeadlineAndInitialHostPolicy(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
			_, _ = io.WriteString(w, "late")
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := Fetch(ctx, server.Client(), Artifact{URL: server.URL, SHA256: digest([]byte("late")), Size: 4}, []string{hostPort(server)}, 16, t.TempDir())
	if err == nil {
		t.Fatal("expired fetch deadline was ignored")
	}
	_, err = Fetch(context.Background(), server.Client(), Artifact{URL: server.URL, SHA256: digest([]byte("late")), Size: 4}, []string{"untrusted.invalid"}, 16, t.TempDir())
	if err == nil {
		t.Fatal("unapproved initial host was accepted")
	}
	var plainRequests atomic.Int64
	plainClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		plainRequests.Add(1)
		return nil, errors.New("plain HTTP must not be requested")
	})}
	_, err = Fetch(context.Background(), plainClient, Artifact{URL: "http://untrusted.invalid/file", SHA256: digest([]byte("late")), Size: 4}, []string{"untrusted.invalid"}, 16, t.TempDir())
	if err == nil || plainRequests.Load() != 0 {
		t.Fatal("plain HTTP URL was accepted or requested")
	}
}

type zipEntry struct {
	name string
	data []byte
	mode os.FileMode
}

func makeZip(t *testing.T, entries ...zipEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name}
		mode := entry.mode
		if mode == 0 {
			mode = 0644
		}
		header.SetMode(mode)
		file, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(entry.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type tarEntry struct {
	name     string
	data     []byte
	typeflag byte
	link     string
	mode     int64
}

func makeTarGzip(t *testing.T, entries ...tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	writer := tar.NewWriter(gz)
	for _, entry := range entries {
		mode := entry.mode
		if mode == 0 {
			mode = 0644
		}
		typeflag := entry.typeflag
		if typeflag == 0 {
			typeflag = tar.TypeReg
		}
		header := &tar.Header{Name: entry.name, Linkname: entry.link, Typeflag: typeflag, Mode: mode, Size: int64(len(entry.data))}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(entry.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func hostPort(server *httptest.Server) string {
	return strings.TrimPrefix(server.URL, "https://")
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }
