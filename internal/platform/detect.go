package platform

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

var (
	errExecutableMissing = exec.ErrNotFound
	versionPattern       = regexp.MustCompile(`(?:^|[^0-9])v?([0-9]+\.[0-9]+\.[0-9]+)(?:[^0-9]|$)`)
)

func compatibilityFor(goos, arch string) string {
	switch goos {
	case "darwin":
		if arch == "amd64" || arch == "arm64" {
			return StatusSupported
		}
	case "windows":
		if arch == "amd64" || arch == "arm64" {
			return StatusSupported
		}
	case "linux":
		if arch == "amd64" || arch == "arm64" {
			return StatusSupported
		}
	}
	return StatusUnsupported
}

func detectRuntime() runtimeDetails {
	info := runtimeDetails{OS: runtime.GOOS, Arch: runtime.GOARCH, OSVersion: "unknown", Shell: os.Getenv("SHELL")}
	if info.Shell == "" {
		info.Shell = os.Getenv("ComSpec")
	}
	if runtime.GOOS == "linux" {
		id, version := linuxRelease()
		info.DistroID, info.DistroVersion, info.OSVersion = id, version, version
	}
	return info
}

func linuxRelease() (id, version string) {
	file, err := os.Open("/etc/os-release")
	if err != nil {
		return "unknown", "unknown"
	}
	defer file.Close()
	scanner := bufio.NewScanner(io.LimitReader(file, 64*1024))
	for scanner.Scan() {
		line := scanner.Text()
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
			if value[0] == '"' {
				if unquoted, err := strconv.Unquote(value); err == nil {
					value = unquoted
				}
			} else {
				value = value[1 : len(value)-1]
			}
		}
		switch key {
		case "ID":
			id = strings.ToLower(value)
		case "VERSION_ID":
			version = value
		}
	}
	if id == "" {
		id = "unknown"
	}
	if version == "" {
		version = "unknown"
	}
	return id, version
}

func inspectRoot(root string) (string, error) {
	if root == "" {
		return "", fmt.Errorf("workspace root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolving workspace root")
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return "", fmt.Errorf("workspace root is unavailable")
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", ErrSymlinkedRoot
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workspace root is not a directory")
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolving workspace root")
	}
	if filepath.Clean(resolved) != filepath.Clean(abs) {
		return "", ErrSymlinkedRoot
	}
	return resolved, nil
}

func filesystemStatus(fsType string) string {
	fsType = strings.ToLower(strings.TrimSpace(fsType))
	if fsType == "" || fsType == "unknown" {
		return StatusUnknown
	}
	switch fsType {
	case "apfs", "hfs", "hfsplus", "ext2", "ext3", "ext4", "xfs", "btrfs", "ntfs", "refs", "tmpfs":
		return StatusSupported
	}
	if strings.HasPrefix(fsType, "nfs") || strings.HasPrefix(fsType, "cifs") || strings.HasPrefix(fsType, "smb") || strings.HasPrefix(fsType, "fuse") || fsType == "overlay" || fsType == "9p" || fsType == "virtiofs" {
		return StatusUnsupported
	}
	return StatusUnknown
}

func defaultFilesystemType(root string) (string, error) {
	if runtime.GOOS != "linux" {
		return "unknown", nil
	}
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return "unknown", nil
	}
	return mountTypeForPath(data, root), nil
}

func mountTypeForPath(mountInfo []byte, root string) string {
	bestMount, bestType := "", "unknown"
	for _, line := range bytes.Split(mountInfo, []byte{'\n'}) {
		fields := strings.Fields(string(line))
		separator := -1
		for i, field := range fields {
			if field == "-" {
				separator = i
				break
			}
		}
		if separator < 0 || separator+1 >= len(fields) || len(fields) < 5 {
			continue
		}
		mount := unescapeMountField(fields[4])
		if root == mount || strings.HasPrefix(root, strings.TrimRight(mount, string(filepath.Separator))+string(filepath.Separator)) {
			if len(mount) > len(bestMount) {
				bestMount, bestType = mount, fields[separator+1]
			}
		}
	}
	return bestType
}

func unescapeMountField(field string) string {
	for _, pair := range [][2]string{{`\040`, " "}, {`\011`, "\t"}, {`\012`, "\n"}, {`\134`, `\`}} {
		field = strings.ReplaceAll(field, pair[0], pair[1])
	}
	return filepath.Clean(field)
}

func osExecutableLookup(program string) (string, error) {
	if _, allowed := allowedProbePrograms[program]; !allowed {
		return "", fmt.Errorf("probe program is not in catalog")
	}
	path, err := exec.LookPath(program)
	if err != nil {
		return "", err
	}
	return path, nil
}

func runExecutable(ctx context.Context, spec probeSpec, path string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, path, spec.Args...)
	stdout, stderr := &limitedBuffer{max: spec.MaxOutput}, &limitedBuffer{max: spec.MaxOutput}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err := cmd.Run()
	if errors.Is(stdoutError(stdout, stderr), errOutputLimit) {
		return nil, errOutputLimit
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	output := append(stdout.Bytes(), stderr.Bytes()...)
	if len(output) > spec.MaxOutput {
		return nil, errOutputLimit
	}
	return output, nil
}

func stdoutError(buffers ...*limitedBuffer) error {
	for _, buffer := range buffers {
		if buffer.overflow {
			return errOutputLimit
		}
	}
	return nil
}

func parseVersion(output []byte) (string, bool) {
	match := versionPattern.FindSubmatch(output)
	if len(match) != 2 {
		return "", false
	}
	return string(match[1]), true
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
