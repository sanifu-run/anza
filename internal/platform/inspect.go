package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/sanifu-run/anza/internal/domain"
)

const (
	StatusSupported        = "supported"
	StatusUnsupported      = "unsupported"
	StatusUnknown          = "unknown"
	StatusPresent          = "present"
	StatusMissing          = "missing"
	ProbeTimedOut          = "timeout"
	ProbeVersionMalformed  = "malformed_version"
	ProbeExecutableMissing = "missing_executable"
	ProbeOutputLimited     = "output_limit"
	ProbeFailed            = "probe_failed"
	ProbeCanceled          = "canceled"
	maxManifestBytes       = 1 << 20
)

var (
	ErrUnsupportedTarget     = errors.New("unsupported operating system or architecture")
	ErrUnsupportedFilesystem = errors.New("unsupported workspace filesystem")
	ErrSymlinkedRoot         = errors.New("workspace root resolves through a symlink")
	ErrConsentRequired       = errors.New("explicit project manifest permission is required")
	ErrConsentDenied         = errors.New("project manifest inspection was not approved")
)

// ProbeResult exposes only a catalog ID, a parsed version and an actionable
// status reason. It never includes executable paths or raw command output.
type ProbeResult struct {
	ID      string
	Status  string
	Version string
	Reason  string
}

type Inspection struct {
	Facts                domain.MachineFacts
	Probes               []ProbeResult
	RootFilesystemStatus string
}

type runtimeDetails struct {
	OS            string
	Arch          string
	OSVersion     string
	DistroID      string
	DistroVersion string
	Shell         string
}

type probeSpec struct {
	ID        string
	Program   string
	Args      []string
	Timeout   time.Duration
	MaxOutput int
}

type executableLookup func(string) (string, error)
type probeRunner func(context.Context, probeSpec, string) ([]byte, error)
type filesystemLookup func(string) (string, error)

var defaultProbes = []probeSpec{
	{ID: "codex-cli", Program: "codex", Args: []string{"--version"}, Timeout: 2 * time.Second, MaxOutput: 2048},
	{ID: "claude-code", Program: "claude", Args: []string{"--version"}, Timeout: 2 * time.Second, MaxOutput: 2048},
}

var allowedProbePrograms = map[string][]string{
	"codex":  {"--version"},
	"claude": {"--version"},
}

// Inspect gathers minimized machine facts and catalog-constrained version
// probes. It checks only root metadata; project manifests require a separate
// explicit InspectProject permission callback.
func Inspect(ctx context.Context, root string) (domain.MachineFacts, error) {
	report, err := inspectWith(ctx, root, detectRuntime(), defaultFilesystemType, osExecutableLookup, runExecutable)
	if err != nil {
		return domain.MachineFacts{}, err
	}
	return report.Facts, nil
}

func inspectWith(ctx context.Context, root string, runtime runtimeDetails, filesystem filesystemLookup, lookup executableLookup, runner probeRunner) (Inspection, error) {
	if err := ctx.Err(); err != nil {
		return Inspection{}, err
	}
	facts, err := machineFacts(runtime, nil)
	if err != nil {
		return Inspection{}, err
	}
	rootStatus := StatusUnknown
	filesystemID := ""
	if root != "" {
		canonical, err := inspectRoot(root)
		if err != nil {
			return Inspection{}, err
		}
		fsType, err := filesystem(canonical)
		if err != nil {
			return Inspection{}, fmt.Errorf("checking workspace filesystem: %w", err)
		}
		rootStatus = filesystemStatus(fsType)
		filesystemID = fsType
		if rootStatus == StatusUnsupported {
			return Inspection{}, fmt.Errorf("%w: %s", ErrUnsupportedFilesystem, fsType)
		}
	}
	results := runProbes(ctx, defaultProbes, lookup, runner)
	capabilities := make(map[string]domain.Capability, len(results)+1)
	for _, result := range results {
		status := result.Status
		if status != StatusPresent && status != StatusMissing {
			status = "unknown"
		}
		capabilities[result.ID] = domain.Capability{Status: status, Version: result.Version}
	}
	if root != "" {
		status := "unknown"
		if rootStatus == StatusSupported {
			status = "present"
		}
		capabilities["workspace-filesystem"] = domain.Capability{Status: status, Version: filesystemID}
	}
	facts.Capabilities = capabilities
	return Inspection{Facts: facts, Probes: results, RootFilesystemStatus: rootStatus}, nil
}

func machineFacts(runtime runtimeDetails, capabilities map[string]domain.Capability) (domain.MachineFacts, error) {
	status := compatibilityFor(runtime.OS, runtime.Arch)
	if status != StatusSupported {
		return domain.MachineFacts{}, fmt.Errorf("%w: %s/%s", ErrUnsupportedTarget, runtime.OS, runtime.Arch)
	}
	version := runtime.OSVersion
	if version == "" {
		version = "unknown"
	}
	distroID, distroVersion := runtime.DistroID, runtime.DistroVersion
	if runtime.OS != "linux" {
		distroID, distroVersion = "", ""
	}
	if distroID == "" && runtime.OS == "linux" {
		distroID = "unknown"
	}
	if distroVersion == "" && runtime.OS == "linux" {
		distroVersion = "unknown"
	}
	shell := filepath.Base(runtime.Shell)
	if shell == "." || shell == string(filepath.Separator) || shell == "" {
		shell = "unknown"
	}
	return domain.MachineFacts{OS: runtime.OS, Arch: runtime.Arch, OSVersion: version, DistroID: distroID, DistroVersion: distroVersion, ShellKind: shell, Capabilities: capabilities}, nil
}

func runProbes(ctx context.Context, catalog []probeSpec, lookup executableLookup, runner probeRunner) []ProbeResult {
	results := make([]ProbeResult, 0, len(catalog))
	for _, spec := range catalog {
		result := ProbeResult{ID: spec.ID, Status: StatusUnknown}
		args, allowed := allowedProbePrograms[spec.Program]
		if !allowed || !equalStrings(spec.Args, args) || spec.ID == "" || spec.Timeout <= 0 || spec.MaxOutput <= 0 {
			result.Reason = ProbeFailed
			results = append(results, result)
			continue
		}
		if err := ctx.Err(); err != nil {
			result.Reason = ProbeCanceled
			results = append(results, result)
			continue
		}
		path, err := lookup(spec.Program)
		if err != nil {
			if errors.Is(err, errExecutableMissing) {
				result.Status = StatusMissing
				result.Reason = ProbeExecutableMissing
			} else {
				result.Reason = ProbeFailed
			}
			results = append(results, result)
			continue
		}
		probeCtx, cancel := context.WithTimeout(ctx, spec.Timeout)
		output, err := runner(probeCtx, spec, path)
		cancel()
		if err != nil {
			switch {
			case errors.Is(err, context.DeadlineExceeded):
				result.Reason = ProbeTimedOut
			case errors.Is(err, errOutputLimit):
				result.Reason = ProbeOutputLimited
			case errors.Is(err, context.Canceled):
				result.Reason = ProbeCanceled
			default:
				result.Reason = ProbeFailed
			}
			results = append(results, result)
			continue
		}
		if len(output) > spec.MaxOutput {
			result.Reason = ProbeOutputLimited
			results = append(results, result)
			continue
		}
		version, ok := parseVersion(output)
		if !ok {
			result.Reason = ProbeVersionMalformed
			results = append(results, result)
			continue
		}
		result.Status, result.Version = StatusPresent, version
		results = append(results, result)
	}
	return results
}

// ProjectFacts contains only reviewed technology IDs and selected manifest
// names; it never returns source text, package scripts, module paths or names.
type ProjectFacts struct {
	KnownStack []string
	Manifests  []string
}

// ManifestConsent is called before any selected manifest is opened. The
// callback should obtain explicit participant approval for the named files.
type ManifestConsent func(context.Context, []string) (bool, error)

var supportedManifests = map[string]string{
	"package.json":   "node",
	"go.mod":         "go",
	"Cargo.toml":     "rust",
	"pyproject.toml": "python",
}

// InspectProject reads only explicitly selected known manifests after consent.
// It parses a small allowlisted subset and never executes package scripts.
func InspectProject(ctx context.Context, root string, selected []string, consent ManifestConsent) (ProjectFacts, error) {
	if consent == nil {
		return ProjectFacts{}, ErrConsentRequired
	}
	if len(selected) == 0 {
		return ProjectFacts{}, fmt.Errorf("no project manifests selected")
	}
	seen := make(map[string]struct{}, len(selected))
	files := append([]string(nil), selected...)
	for _, name := range files {
		if _, supported := supportedManifests[name]; !supported || filepath.Base(name) != name {
			return ProjectFacts{}, fmt.Errorf("unsupported project manifest selection")
		}
		if _, duplicate := seen[name]; duplicate {
			return ProjectFacts{}, fmt.Errorf("duplicate project manifest selection")
		}
		seen[name] = struct{}{}
	}
	approved, err := consent(ctx, append([]string(nil), files...))
	if err != nil {
		return ProjectFacts{}, fmt.Errorf("requesting project manifest permission: %w", err)
	}
	if !approved {
		return ProjectFacts{}, ErrConsentDenied
	}
	if err := ctx.Err(); err != nil {
		return ProjectFacts{}, err
	}
	canonicalRoot, err := inspectRoot(root)
	if err != nil {
		return ProjectFacts{}, err
	}
	rootDir, err := os.OpenRoot(canonicalRoot)
	if err != nil {
		return ProjectFacts{}, fmt.Errorf("opening selected project root: %w", err)
	}
	defer rootDir.Close()
	facts := ProjectFacts{}
	stack := map[string]struct{}{}
	for _, name := range files {
		info, err := os.Lstat(filepath.Join(canonicalRoot, name))
		if err != nil {
			return ProjectFacts{}, fmt.Errorf("reading selected project manifest: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return ProjectFacts{}, fmt.Errorf("selected manifest is not a regular file")
		}
		file, err := rootDir.Open(name)
		if err != nil {
			return ProjectFacts{}, fmt.Errorf("opening selected project manifest: %w", err)
		}
		content, readErr := io.ReadAll(io.LimitReader(file, maxManifestBytes+1))
		closeErr := file.Close()
		if readErr != nil {
			return ProjectFacts{}, fmt.Errorf("reading selected project manifest: %w", readErr)
		}
		if closeErr != nil {
			return ProjectFacts{}, fmt.Errorf("closing selected project manifest: %w", closeErr)
		}
		if len(content) > maxManifestBytes {
			return ProjectFacts{}, fmt.Errorf("selected project manifest exceeds size limit")
		}
		stack[supportedManifests[name]] = struct{}{}
		if name == "package.json" {
			known, err := inspectPackageDependencies(content)
			if err != nil {
				return ProjectFacts{}, fmt.Errorf("parsing selected package manifest: %w", err)
			}
			for _, id := range known {
				stack[id] = struct{}{}
			}
		}
		facts.Manifests = append(facts.Manifests, name)
	}
	for id := range stack {
		facts.KnownStack = append(facts.KnownStack, id)
	}
	sort.Strings(facts.KnownStack)
	sort.Strings(facts.Manifests)
	return facts, nil
}

func inspectPackageDependencies(data []byte) ([]string, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, fmt.Errorf("expected a JSON object")
	}
	var packageInfo struct {
		Dependencies    map[string]json.RawMessage `json:"dependencies"`
		DevDependencies map[string]json.RawMessage `json:"devDependencies"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&packageInfo); err != nil {
		return nil, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("expected one JSON manifest")
	}
	known := map[string]struct{}{}
	for _, deps := range []map[string]json.RawMessage{packageInfo.Dependencies, packageInfo.DevDependencies} {
		for name := range deps {
			if _, ok := knownStackPackages[name]; ok {
				known[name] = struct{}{}
			}
		}
	}
	ids := make([]string, 0, len(known))
	for id := range known {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

var knownStackPackages = map[string]string{"react": "react", "next": "next", "vite": "vite", "typescript": "typescript"}

var errOutputLimit = errors.New("probe output limit exceeded")

type limitedBuffer struct {
	bytes.Buffer
	max      int
	overflow bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.max {
		b.overflow = true
		remain := b.max - b.Len()
		if remain > 0 {
			_, _ = b.Buffer.Write(p[:remain])
		}
		return 0, errOutputLimit
	}
	return b.Buffer.Write(p)
}
