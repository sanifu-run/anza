// Package process runs explicitly specified local child processes without a shell.
package process

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	DefaultTimeout   = 30 * time.Second
	MaximumTimeout   = 24 * time.Hour
	DefaultOutputCap = 64 << 10
	MaximumOutputCap = 1 << 20
	MaximumSecrets   = 128
	MaximumSecretLen = 32 << 10
)

var (
	ErrInvalidSpec = errors.New("invalid process specification")
	ErrTimeout     = errors.New("process deadline exceeded")
	ErrStart       = errors.New("process could not be started")
	ErrExitCode    = errors.New("process exited unsuccessfully")
)

var envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Spec contains non-secret execution inputs. Its JSON representation omits
// executable paths, argument values, working directories, and environment values.
type Spec struct {
	Program      string            `json:"-"`
	Args         []string          `json:"-"`
	Dir          string            `json:"-"`
	Env          map[string]string `json:"-"`
	Timeout      time.Duration     `json:"-"`
	OutputLimit  int               `json:"-"`
	SensitiveEnv SensitiveEnv      `json:"-"`
}

// SensitiveEnv holds values that must be passed to the child but excluded from
// serialized specs and redacted from captured output.
type SensitiveEnv struct {
	values map[string]string
}

func (s SensitiveEnv) String() string {
	return fmt.Sprintf("SensitiveEnv{value_count:%d}", len(s.values))
}
func (s SensitiveEnv) GoString() string { return s.String() }

// NewSensitiveEnv validates and copies secret environment variables.
func NewSensitiveEnv(values map[string]string) (SensitiveEnv, error) {
	if len(values) > MaximumSecrets {
		return SensitiveEnv{}, fmt.Errorf("%w: too many sensitive environment values", ErrInvalidSpec)
	}
	copyValues := make(map[string]string, len(values))
	for key, value := range values {
		if !envNamePattern.MatchString(key) || strings.ContainsRune(value, '\x00') || len(value) > MaximumSecretLen {
			return SensitiveEnv{}, fmt.Errorf("%w: invalid sensitive environment value", ErrInvalidSpec)
		}
		copyValues[key] = value
	}
	return SensitiveEnv{values: copyValues}, nil
}

// Result contains bounded, redacted output only; command and environment inputs
// are deliberately not retained.
type Result struct {
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	ExitCode        int    `json:"exit_code"`
	StdoutTruncated bool   `json:"stdout_truncated"`
	StderrTruncated bool   `json:"stderr_truncated"`
	StdoutRedacted  bool   `json:"stdout_redacted"`
	StderrRedacted  bool   `json:"stderr_redacted"`
}

// ExitError reports only the numeric status. It never includes command output.
type ExitError struct{ Code int }

func (e *ExitError) Error() string { return fmt.Sprintf("%s: exit code %d", ErrExitCode, e.Code) }
func (e *ExitError) Unwrap() error { return ErrExitCode }

// MarshalJSON deliberately exposes only bounded non-sensitive execution
// metadata. Arguments, paths and environment values are not serialized.
func (s Spec) MarshalJSON() ([]byte, error) {
	keys := make([]string, 0, len(s.Env))
	for key := range s.Env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return json.Marshal(struct {
		ArgumentCount int           `json:"argument_count"`
		Environment   []string      `json:"environment_keys,omitempty"`
		Timeout       time.Duration `json:"timeout"`
		OutputLimit   int           `json:"output_limit"`
	}{ArgumentCount: len(s.Args), Environment: keys, Timeout: s.Timeout, OutputLimit: s.OutputLimit})
}

// String keeps routine formatting safe for diagnostics.
func (s Spec) String() string {
	return fmt.Sprintf("Spec{argument_count:%d environment_key_count:%d timeout:%s output_limit:%d}", len(s.Args), len(s.Env), s.Timeout, s.OutputLimit)
}

// Run executes Program directly with Args, a fixed allowlisted environment,
// bounded output, and cancellation of the process tree owned by this call.
func Run(ctx context.Context, spec Spec) (Result, error) {
	result := Result{ExitCode: -1}
	if ctx == nil {
		return result, fmt.Errorf("%w: context is nil", ErrInvalidSpec)
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := validateSpec(spec); err != nil {
		return result, err
	}

	timeout := spec.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	outputLimit := spec.OutputLimit
	if outputLimit == 0 {
		outputLimit = DefaultOutputCap
	}
	childEnv, secrets, err := buildEnvironment(spec)
	if err != nil {
		return result, err
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, spec.Program, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Env = childEnv
	cmd.WaitDelay = time.Second
	configureCommand(cmd)

	stdout := newCapture(outputLimit, secrets)
	stderr := newCapture(outputLimit, secrets)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	runErr := cmd.Run()
	stdout.finish()
	stderr.finish()
	result.Stdout, result.StdoutTruncated, result.StdoutRedacted = stdout.result()
	result.Stderr, result.StderrTruncated, result.StderrRedacted = stderr.result()

	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return result, ErrTimeout
	}
	if runErr == nil {
		result.ExitCode = 0
		return result, nil
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result, &ExitError{Code: result.ExitCode}
	}
	return result, ErrStart
}

func validateSpec(spec Spec) error {
	if spec.Program == "" || !filepath.IsAbs(spec.Program) || strings.ContainsRune(spec.Program, '\x00') {
		return fmt.Errorf("%w: program must be an absolute path", ErrInvalidSpec)
	}
	if spec.Dir == "" || !filepath.IsAbs(spec.Dir) || strings.ContainsRune(spec.Dir, '\x00') {
		return fmt.Errorf("%w: working directory must be an absolute path", ErrInvalidSpec)
	}
	if len(spec.Args) > 256 {
		return fmt.Errorf("%w: too many arguments", ErrInvalidSpec)
	}
	for _, arg := range spec.Args {
		if strings.ContainsRune(arg, '\x00') {
			return fmt.Errorf("%w: argument contains NUL", ErrInvalidSpec)
		}
	}
	if spec.Timeout < 0 || spec.Timeout > MaximumTimeout {
		return fmt.Errorf("%w: timeout is outside the supported range", ErrInvalidSpec)
	}
	if spec.OutputLimit < 0 || spec.OutputLimit > MaximumOutputCap {
		return fmt.Errorf("%w: output limit is outside the supported range", ErrInvalidSpec)
	}
	if len(spec.Env) > 128 || len(spec.SensitiveEnv.values) > MaximumSecrets {
		return fmt.Errorf("%w: too many environment values", ErrInvalidSpec)
	}
	for key, value := range spec.Env {
		if !allowedEnvironment[key] || strings.ContainsRune(value, '\x00') {
			return fmt.Errorf("%w: environment key is not allowlisted", ErrInvalidSpec)
		}
	}
	for key, value := range spec.SensitiveEnv.values {
		if !envNamePattern.MatchString(key) || strings.ContainsRune(value, '\x00') || len(value) > MaximumSecretLen {
			return fmt.Errorf("%w: invalid sensitive environment value", ErrInvalidSpec)
		}
		if _, exists := spec.Env[key]; exists {
			return fmt.Errorf("%w: environment key appears in both public and sensitive inputs", ErrInvalidSpec)
		}
	}
	return nil
}

var allowedEnvironment = map[string]bool{
	"PATH": true, "HOME": true, "TMPDIR": true, "TMP": true, "TEMP": true,
	"SYSTEMROOT": true, "WINDIR": true, "USERPROFILE": true, "HOMEDRIVE": true,
	"HOMEPATH": true, "LANG": true, "LC_ALL": true, "TERM": true, "NO_COLOR": true,
}

func buildEnvironment(spec Spec) ([]string, []string, error) {
	values := make(map[string]string, len(allowedEnvironment)+len(spec.Env)+len(spec.SensitiveEnv.values))
	for key := range allowedEnvironment {
		if value, ok := os.LookupEnv(key); ok {
			values[key] = value
		}
	}
	for key, value := range spec.Env {
		values[key] = value
	}
	secrets := make([]string, 0, len(spec.SensitiveEnv.values))
	for key, value := range spec.SensitiveEnv.values {
		values[key] = value
		if value != "" {
			secrets = append(secrets, value)
		}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	env := make([]string, 0, len(keys))
	for _, key := range keys {
		if strings.ContainsRune(values[key], '\x00') {
			return nil, nil, fmt.Errorf("%w: environment value contains NUL", ErrInvalidSpec)
		}
		env = append(env, key+"="+values[key])
	}
	// Longest-first replacement prevents a short value hiding a longer secret.
	sort.Slice(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
	return env, secrets, nil
}

type capture struct {
	limit     int
	secrets   [][]byte
	pending   []byte
	output    bytes.Buffer
	truncated bool
	redacted  bool
	discard   bool
	finished  bool
}

func newCapture(limit int, secrets []string) *capture {
	w := &capture{limit: limit}
	for _, secret := range secrets {
		if secret != "" {
			w.secrets = append(w.secrets, []byte(secret))
		}
	}
	return w
}

func (w *capture) Write(data []byte) (int, error) {
	original := len(data)
	if w.finished || w.discard {
		return original, nil
	}
	w.pending = append(w.pending, data...)
	w.process(false)
	return original, nil
}

func (w *capture) process(final bool) {
	maxSecret := 0
	for _, secret := range w.secrets {
		if len(secret) > maxSecret {
			maxSecret = len(secret)
		}
	}
	safeLimit := len(w.pending)
	if !final && maxSecret > 0 {
		safeLimit -= maxSecret - 1
		if safeLimit < 0 {
			safeLimit = 0
		}
	}
	var clean []byte
	consumed := 0
	for consumed < safeLimit {
		matched := 0
		for _, secret := range w.secrets {
			if len(w.pending)-consumed >= len(secret) && bytes.Equal(w.pending[consumed:consumed+len(secret)], secret) {
				matched = len(secret)
				break
			}
		}
		if matched > 0 {
			w.redacted = true
			w.discard = true
			w.output.Reset()
			w.pending = nil
			return
		} else {
			clean = append(clean, w.pending[consumed])
			consumed++
		}
	}
	if w.discard {
		return
	}
	if consumed > 0 {
		copy(w.pending, w.pending[consumed:])
		w.pending = w.pending[:len(w.pending)-consumed]
	}
	remaining := w.limit - w.output.Len()
	if remaining <= 0 {
		if len(clean) > 0 {
			w.truncated = true
		}
		return
	}
	if len(clean) > remaining {
		_, _ = w.output.Write(clean[:remaining])
		w.truncated = true
		return
	}
	_, _ = w.output.Write(clean)
}

func (w *capture) finish() {
	if w.finished {
		return
	}
	w.finished = true
	w.process(true)
}

func (w *capture) result() (string, bool, bool) {
	return w.output.String(), w.truncated, w.redacted
}

var _ io.Writer = (*capture)(nil)
