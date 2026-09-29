package process

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestProcessChild(t *testing.T) {
	switch os.Getenv("NO_COLOR") {
	case "anza-child-literal":
		fmt.Fprint(os.Stdout, os.Args[len(os.Args)-1])
		os.Exit(0)
	case "anza-child-output":
		fmt.Fprint(os.Stdout, strings.Repeat("x", 4096))
		fmt.Fprint(os.Stderr, strings.Repeat("y", 4096))
		os.Exit(0)
	case "anza-child-secret":
		secret := os.Getenv("ANZA_TEST_SECRET")
		fmt.Fprint(os.Stdout, secret)
		fmt.Fprint(os.Stderr, secret)
		os.Exit(0)
	case "anza-child-env":
		fmt.Fprint(os.Stdout, os.Getenv("ANZA_SHOULD_NOT_INHERIT"))
		os.Exit(0)
	case "anza-child-grandchild":
		marker := filepath.Join(os.Getenv("TMPDIR"), "grandchild.marker")
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for n := 0; ; n++ {
			if err := os.WriteFile(marker, []byte(strconv.Itoa(n)), 0600); err != nil {
				os.Exit(2)
			}
			if n == 0 {
				fmt.Fprintln(os.Stdout, "ready")
			}
			<-ticker.C
		}
	case "anza-child-tree":
		exe, err := os.Executable()
		if err != nil {
			os.Exit(2)
		}
		cmd := exec.Command(exe, "-test.run=^TestProcessChild$")
		cmd.Env = []string{"NO_COLOR=anza-child-grandchild", "TMPDIR=" + os.Getenv("TMPDIR")}
		pipe, err := cmd.StdoutPipe()
		if err != nil {
			os.Exit(2)
		}
		if err := cmd.Start(); err != nil {
			os.Exit(2)
		}
		if _, err := bufio.NewReader(pipe).ReadString('\n'); err != nil {
			os.Exit(2)
		}
		fmt.Fprintln(os.Stdout, cmd.Process.Pid)
		marker := filepath.Join(os.Getenv("TMPDIR"), "parent.marker")
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for n := 0; ; n++ {
			if err := os.WriteFile(marker, []byte(strconv.Itoa(n)), 0600); err != nil {
				os.Exit(2)
			}
			<-ticker.C
		}
	}
}

func TestLiteralArguments(t *testing.T) {
	literal := `$(touch /tmp/anza-must-not-run); "$HOME"; a | b > c`
	spec := childSpec(t)
	spec.Args = []string{"-test.run=^TestProcessChild$", "--", literal}
	spec.Env["NO_COLOR"] = "anza-child-literal"
	result, err := Run(context.Background(), spec)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Stdout != literal {
		t.Fatalf("argument changed: got %q, want %q", result.Stdout, literal)
	}
}

func TestOutputBoundsSecretRedactionAndEnvironmentAllowlist(t *testing.T) {
	t.Setenv("ANZA_SHOULD_NOT_INHERIT", "ambient-value")
	spec := childSpec(t)
	spec.Args = []string{"-test.run=^TestProcessChild$"}
	spec.Env["NO_COLOR"] = "anza-child-output"
	spec.OutputLimit = 128
	secrets, err := NewSensitiveEnv(map[string]string{"ANZA_TEST_SECRET": "synthetic-top-secret"})
	if err != nil {
		t.Fatal(err)
	}
	spec.SensitiveEnv = secrets
	result, err := Run(context.Background(), spec)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(result.Stdout) > spec.OutputLimit || len(result.Stderr) > spec.OutputLimit {
		t.Fatal("captured output exceeds limit")
	}
	if !result.StdoutTruncated || !result.StderrTruncated {
		t.Fatalf("output was not bounded: %+v", result)
	}

	spec.Env["NO_COLOR"] = "anza-child-secret"
	result, err = Run(context.Background(), spec)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.StdoutRedacted || !result.StderrRedacted || result.Stdout != "" || result.Stderr != "" {
		t.Fatalf("secret-bearing stream was not dropped (stdout bytes=%d, stderr bytes=%d, redacted flags=%v/%v)", len(result.Stdout), len(result.Stderr), result.StdoutRedacted, result.StderrRedacted)
	}
	if strings.Contains(result.Stdout, "synthetic-top-secret") || strings.Contains(result.Stderr, "synthetic-top-secret") {
		t.Fatal("secret leaked into result")
	}

	spec.Args = []string{"-test.run=^TestProcessChild$"}
	spec.Env["NO_COLOR"] = "anza-child-env"
	spec.OutputLimit = 0
	result, err = Run(context.Background(), spec)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Stdout != "" {
		t.Fatalf("non-allowlisted ambient environment leaked: %q", result.Stdout)
	}
}

func TestTimeoutCancelsOwnedProcessTree(t *testing.T) {
	dir := t.TempDir()
	unrelatedDir := t.TempDir()
	processBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	unrelated := exec.Command(processBinary, "-test.run=^TestProcessChild$")
	unrelated.Env = []string{"NO_COLOR=anza-child-grandchild", "TMPDIR=" + unrelatedDir}
	unrelated.Stdout = io.Discard
	unrelated.Stderr = io.Discard
	if err := unrelated.Start(); err != nil {
		t.Fatalf("start unrelated fixture: %v", err)
	}
	t.Cleanup(func() {
		_ = unrelated.Process.Kill()
		_ = unrelated.Wait()
	})
	waitForFile(t, filepath.Join(unrelatedDir, "grandchild.marker"))

	spec := childSpec(t)
	spec.Dir = dir
	spec.Env["NO_COLOR"] = "anza-child-tree"
	spec.Env["TMPDIR"] = dir
	spec.Timeout = 300 * time.Millisecond
	result, err := Run(context.Background(), spec)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("Run error = %v, want ErrTimeout", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(result.Stdout))
	if err != nil || pid <= 0 {
		t.Fatalf("missing grandchild pid in output %q: %v", result.Stdout, err)
	}
	cleanupFixtureProcess(t, pid)
	assertMarkersStopped(t, dir)
	assertMarkerAdvances(t, filepath.Join(unrelatedDir, "grandchild.marker"))
}

func TestContextCancellationAndSafeValidation(t *testing.T) {
	dir := t.TempDir()
	spec := childSpec(t)
	spec.Dir = dir
	spec.Env["NO_COLOR"] = "anza-child-tree"
	spec.Env["TMPDIR"] = dir
	ctx, cancel := context.WithCancel(context.Background())
	type runResult struct {
		result Result
		err    error
	}
	done := make(chan runResult, 1)
	go func() { result, err := Run(ctx, spec); done <- runResult{result: result, err: err} }()
	waitForFile(t, filepath.Join(dir, "parent.marker"))
	cancel()
	var outcome runResult
	select {
	case outcome = <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
	if !errors.Is(outcome.err, context.Canceled) {
		t.Fatalf("Run error = %v, want context.Canceled", outcome.err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(outcome.result.Stdout))
	if err != nil || pid <= 0 {
		t.Fatalf("missing grandchild pid in output %q: %v", outcome.result.Stdout, err)
	}
	cleanupFixtureProcess(t, pid)
	assertMarkersStopped(t, dir)

	spec.Program = "./relative-program"
	if _, err := Run(context.Background(), spec); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("relative executable error = %v, want ErrInvalidSpec", err)
	}
	spec = childSpec(t)
	spec.Env["OPENROUTER_API_KEY"] = "must-not-be-public"
	if _, err := Run(context.Background(), spec); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("unsafe environment key error = %v, want ErrInvalidSpec", err)
	}
}

func TestSpecSerializationOmitsArgumentsAndSensitiveValues(t *testing.T) {
	spec := childSpec(t)
	spec.Args = []string{"literal-argument-secret"}
	secrets, err := NewSensitiveEnv(map[string]string{"OPENROUTER_API_KEY": "synthetic-secret-value"})
	if err != nil {
		t.Fatal(err)
	}
	spec.SensitiveEnv = secrets
	encoded, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"literal-argument-secret", "synthetic-secret-value", "OPENROUTER_API_KEY"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatal("Spec JSON contains sensitive input")
		}
	}
	if strings.Contains(fmt.Sprint(spec), "synthetic-secret-value") {
		t.Fatal("Spec formatting leaks sensitive environment value")
	}
	if strings.Contains(fmt.Sprint(secrets), "synthetic-secret-value") {
		t.Fatal("SensitiveEnv formatting leaks sensitive environment value")
	}
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatalf("timed out waiting for fixture file %s", filepath.Base(path))
		}
	}
}

func assertMarkersStopped(t *testing.T, dir string) {
	t.Helper()
	for _, name := range []string{"parent.marker", "grandchild.marker"} {
		path := filepath.Join(dir, name)
		before, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("child marker %s missing: %v", name, err)
		}
		deadline := time.NewTimer(250 * time.Millisecond)
		ticker := time.NewTicker(10 * time.Millisecond)
		stopped := true
	loop:
		for {
			select {
			case <-deadline.C:
				break loop
			case <-ticker.C:
				after, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("read child marker: %v", err)
				}
				if string(after) != string(before) {
					stopped = false
					break loop
				}
			}
		}
		ticker.Stop()
		deadline.Stop()
		if !stopped {
			t.Fatalf("owned process tree still running: %s changed", name)
		}
	}
}

func assertMarkerAdvances(t *testing.T, path string) {
	t.Helper()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read unrelated marker: %v", err)
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(500 * time.Millisecond)
	defer deadline.Stop()
	for {
		select {
		case <-deadline.C:
			t.Fatal("cancelling owned process tree stopped unrelated process")
		case <-ticker.C:
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read unrelated marker: %v", err)
			}
			if string(after) != string(before) {
				return
			}
		}
	}
}

func cleanupFixtureProcess(t *testing.T, pid int) {
	t.Helper()
	t.Cleanup(func() {
		proc, err := os.FindProcess(pid)
		if err == nil {
			_ = proc.Kill()
		}
	})
}

func childSpec(t *testing.T) Spec {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Spec{Program: exe, Args: []string{"-test.run=^TestProcessChild$"}, Dir: t.TempDir(), Timeout: 3 * time.Second, OutputLimit: 1024, Env: map[string]string{"NO_COLOR": ""}}
}
