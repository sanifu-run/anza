package platform

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func syntheticSupportedFilesystem(string) (string, error) { return "apfs", nil }

func TestInspectReadOnly(t *testing.T) {
	root := canonicalTempDir(t)
	before := snapshotTree(t, root)
	info := runtimeDetails{OS: "darwin", Arch: "arm64", OSVersion: "15.0", Shell: "/bin/zsh"}
	inspection, err := inspectWith(context.Background(), root, info, syntheticSupportedFilesystem, func(string) (string, error) { return "", errExecutableMissing }, func(context.Context, probeSpec, string) ([]byte, error) {
		t.Fatal("missing executable was executed")
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := snapshotTree(t, root); !reflect.DeepEqual(got, before) {
		t.Fatalf("inspection changed project root: before=%v after=%v", before, got)
	}
	if strings.Contains(inspection.Facts.ShellKind, "/") {
		t.Fatalf("shell path leaked: %q", inspection.Facts.ShellKind)
	}
	if inspection.Facts.OS != "darwin" || inspection.Facts.Arch != "arm64" || inspection.Facts.ShellKind != "zsh" {
		t.Fatalf("unexpected facts: %#v", inspection.Facts)
	}
}

func TestManifestNeverExecuted(t *testing.T) {
	root := canonicalTempDir(t)
	marker := filepath.Join(root, "script-ran")
	manifest := `{"name":"sample","scripts":{"postinstall":"touch ` + marker + `"},"dependencies":{"react":"18.0.0"}}`
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	denied, err := InspectProject(context.Background(), root, []string{"package.json"}, func(context.Context, []string) (bool, error) { return false, nil })
	if !errors.Is(err, ErrConsentDenied) {
		t.Fatalf("expected permission denial before manifest read, got %#v / %v", denied, err)
	}
	approved, err := InspectProject(context.Background(), root, []string{"package.json"}, func(_ context.Context, selected []string) (bool, error) {
		if !reflect.DeepEqual(selected, []string{"package.json"}) {
			t.Fatalf("consent did not name selected manifest: %v", selected)
		}
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(approved.KnownStack, []string{"node", "react"}) {
		t.Fatalf("unexpected minimized stack facts: %#v", approved)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("project script ran or marker unexpectedly exists: %v", err)
	}
}

func TestManifestConsentPrecedesRootAccess(t *testing.T) {
	called := false
	_, err := InspectProject(context.Background(), filepath.Join(t.TempDir(), "missing-root"), []string{"package.json"}, func(context.Context, []string) (bool, error) {
		called = true
		return false, nil
	})
	if !called || !errors.Is(err, ErrConsentDenied) {
		t.Fatalf("manifest permission must be obtained before touching root: called=%v err=%v", called, err)
	}
}

func TestMalformedPackageManifestIsRejected(t *testing.T) {
	for _, contents := range []string{"null", "{broken"} {
		t.Run(contents, func(t *testing.T) {
			root := canonicalTempDir(t)
			if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := InspectProject(context.Background(), root, []string{"package.json"}, func(context.Context, []string) (bool, error) { return true, nil })
			if err == nil {
				t.Fatal("malformed package manifest was accepted")
			}
		})
	}
}

func TestPlatformMatrix(t *testing.T) {
	cases := []struct {
		goos, arch string
		want       string
	}{
		{"darwin", "arm64", StatusSupported}, {"darwin", "amd64", StatusSupported},
		{"windows", "amd64", StatusSupported}, {"windows", "arm64", StatusSupported}, {"linux", "amd64", StatusSupported}, {"linux", "arm64", StatusSupported},
		{"freebsd", "amd64", StatusUnsupported}, {"linux", "386", StatusUnsupported},
	}
	for _, tc := range cases {
		t.Run(tc.goos+"/"+tc.arch, func(t *testing.T) {
			got := compatibilityFor(tc.goos, tc.arch)
			if got != tc.want {
				t.Fatalf("compatibilityFor(%s,%s)=%s, want %s", tc.goos, tc.arch, got, tc.want)
			}
		})
	}
	facts, err := machineFacts(runtimeDetails{OS: "windows", Arch: "arm64", Shell: `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`}, nil)
	if err != nil || facts.OS != "windows" || facts.Arch != "arm64" {
		t.Fatalf("Windows/arm64 machine facts should be reported independently of vendor qualification: %#v, %v", facts, err)
	}
	if _, err := machineFacts(runtimeDetails{OS: "freebsd", Arch: "amd64", Shell: "/bin/sh"}, nil); err == nil || !errors.Is(err, ErrUnsupportedTarget) {
		t.Fatalf("unsupported target was not explicit: %v", err)
	}
}

func TestProbeTimeout(t *testing.T) {
	ctx := context.Background()
	probe := probeSpec{ID: "codex-cli", Program: "codex", Args: []string{"--version"}, Timeout: 5 * time.Millisecond, MaxOutput: 32}
	results := runProbes(ctx, []probeSpec{probe}, func(name string) (string, error) { return "/synthetic/bin/" + name, nil }, func(ctx context.Context, _ probeSpec, _ string) ([]byte, error) { <-ctx.Done(); return nil, ctx.Err() })
	if len(results) != 1 || results[0].Status != StatusUnknown || results[0].Reason != ProbeTimedOut {
		t.Fatalf("timeout outcome not distinguished: %#v", results)
	}
}

func TestProbeOutcomes(t *testing.T) {
	spec := probeSpec{ID: "codex-cli", Program: "codex", Args: []string{"--version"}, Timeout: time.Second, MaxOutput: 64}
	cases := []struct {
		name                    string
		lookupErr               error
		output                  []byte
		runErr                  error
		status, reason, version string
	}{
		{name: "missing", lookupErr: errExecutableMissing, status: StatusMissing, reason: ProbeExecutableMissing},
		{name: "malformed version", output: []byte("codex soon"), status: StatusUnknown, reason: ProbeVersionMalformed},
		{name: "present", output: []byte("codex-cli 0.157.1\n"), status: StatusPresent, version: "0.157.1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			results := runProbes(context.Background(), []probeSpec{spec}, func(string) (string, error) { return "/synthetic/codex", tc.lookupErr }, func(context.Context, probeSpec, string) ([]byte, error) { return tc.output, tc.runErr })
			if len(results) != 1 || results[0].Status != tc.status || results[0].Reason != tc.reason || results[0].Version != tc.version {
				t.Fatalf("unexpected probe outcome: %#v", results)
			}
		})
	}
}

func TestProbeUnexpectedLookupFailure(t *testing.T) {
	spec := probeSpec{ID: "codex-cli", Program: "codex", Args: []string{"--version"}, Timeout: time.Second, MaxOutput: 64}
	results := runProbes(context.Background(), []probeSpec{spec}, func(string) (string, error) { return "", errors.New("permission denied") }, func(context.Context, probeSpec, string) ([]byte, error) {
		t.Fatal("runner called after executable lookup failure")
		return nil, nil
	})
	if len(results) != 1 || results[0].Status != StatusUnknown || results[0].Reason != ProbeFailed {
		t.Fatalf("unexpected lookup errors should remain unknown, got %#v", results)
	}
}

func TestProbeOutputLimit(t *testing.T) {
	spec := probeSpec{ID: "codex-cli", Program: "codex", Args: []string{"--version"}, Timeout: time.Second, MaxOutput: 4}
	results := runProbes(context.Background(), []probeSpec{spec}, func(string) (string, error) { return "/synthetic/codex", nil }, func(context.Context, probeSpec, string) ([]byte, error) { return []byte("12345"), nil })
	if len(results) != 1 || results[0].Status != StatusUnknown || results[0].Reason != ProbeOutputLimited {
		t.Fatalf("oversized probe output was not bounded: %#v", results)
	}
}

func TestRootSafety(t *testing.T) {
	root := canonicalTempDir(t)
	link := filepath.Join(t.TempDir(), "workspace-link")
	if err := os.Symlink(root, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := inspectWith(context.Background(), link, runtimeDetails{OS: "linux", Arch: "amd64", Shell: "/bin/sh"}, syntheticSupportedFilesystem, func(string) (string, error) { return "", errExecutableMissing }, func(context.Context, probeSpec, string) ([]byte, error) { return nil, nil }); !errors.Is(err, ErrSymlinkedRoot) {
		t.Fatalf("symlinked root was not rejected: %v", err)
	}
	parentLink := filepath.Join(t.TempDir(), "parent-link")
	if err := os.Symlink(filepath.Dir(root), parentLink); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := inspectRoot(filepath.Join(parentLink, filepath.Base(root))); !errors.Is(err, ErrSymlinkedRoot) {
		t.Fatalf("root reached through a symlinked parent was not rejected: %v", err)
	}
	if state := filesystemStatus("nfs4"); state != StatusUnsupported {
		t.Fatalf("NFS was not marked unsupported: %s", state)
	}
	if state := filesystemStatus(""); state != StatusUnknown {
		t.Fatalf("unknown filesystem not preserved: %s", state)
	}
	if _, err := inspectWith(context.Background(), root, runtimeDetails{OS: "linux", Arch: "amd64", Shell: "/bin/sh"}, func(string) (string, error) { return "nfs4", nil }, func(string) (string, error) { return "", errExecutableMissing }, func(context.Context, probeSpec, string) ([]byte, error) { return nil, nil }); !errors.Is(err, ErrUnsupportedFilesystem) {
		t.Fatalf("unsupported filesystem was not explicit: %v", err)
	}
}

func canonicalTempDir(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func snapshotTree(t *testing.T, root string) []string {
	t.Helper()
	var paths []string
	if err := filepath.WalkDir(root, func(path string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		paths = append(paths, rel)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return paths
}
