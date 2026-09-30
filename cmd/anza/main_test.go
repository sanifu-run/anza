package main

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionNoSideEffects(t *testing.T) {
	home := t.TempDir()
	workdir := t.TempDir()
	t.Setenv("HOME", home)
	previousDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workdir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousDir); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})

	homeBefore := directoryEntries(t, home)
	workdirBefore := directoryEntries(t, workdir)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"version"}, &stdout, &stderr); code != 0 {
		t.Fatalf("run(version) exit code = %d, want 0; stderr: %s", code, stderr.String())
	}
	if got, want := stdout.String(), "Anza dev\nCommit: unknown\nBuilt: unknown\n"; got != want {
		t.Errorf("version output = %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Errorf("version wrote to stderr: %q", stderr.String())
	}
	if got := directoryEntries(t, home); !equalStrings(got, homeBefore) {
		t.Errorf("version changed HOME contents: before %v, after %v", homeBefore, got)
	}
	if got := directoryEntries(t, workdir); !equalStrings(got, workdirBefore) {
		t.Errorf("version changed working-directory contents: before %v, after %v", workdirBefore, got)
	}
}

func TestHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("run(help) exit code = %d, want 0; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Usage: anza") {
		t.Errorf("help output %q does not contain usage", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("help wrote to stderr: %q", stderr.String())
	}
}

func TestUnknownFlagExitCode(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--not-a-real-option"}, &stdout, &stderr); code != 2 {
		t.Fatalf("run(unknown flag) exit code = %d, want 2; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "not-a-real-option") {
		t.Errorf("unknown-flag error %q does not identify the flag", stderr.String())
	}
}

func TestUpdateFailsClosedWithoutBuildTimeKey(t *testing.T) {
	t.Setenv("ANZA_RELEASE_METADATA_URL", "https://updates.example/anza/1.2.3/release-metadata.json")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"update"}, &stdout, &stderr); code == 0 {
		t.Fatal("update succeeded without a build-time public key")
	}
	if !strings.Contains(stderr.String(), "release Ed25519 public key is not configured") {
		t.Fatalf("missing-key error = %q", stderr.String())
	}
}

func directoryEntries(t *testing.T, root string) []string {
	t.Helper()
	var entries []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path != root {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			entries = append(entries, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read directory tree %q: %v", root, err)
	}
	return entries
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
