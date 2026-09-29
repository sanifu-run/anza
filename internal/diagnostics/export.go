// Package diagnostics creates an explicitly requested, local diagnostic export.
// It accepts only small, allowlisted metadata fields; it never collects
// configuration, paths, environment, conversation content, or error strings.
package diagnostics

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	ErrInvalidMetadata = errors.New("diagnostic metadata is not allowlisted")
	ErrInvalidRequest  = errors.New("diagnostic export request is invalid")
	ErrWrite           = errors.New("diagnostic export could not be written")
	ErrDelete          = errors.New("session state could not be deleted")
)

// Metadata contains the complete set of information permitted in an export.
// Values are validated against fixed, low-entropy formats before serialization.
type Metadata struct {
	Version   string `json:"version"`
	Platform  string `json:"platform"`
	Status    string `json:"status"`
	Operation string `json:"operation"`
}

// Request is an explicit user request to write one private local file.
type Request struct {
	Destination string
	Metadata    Metadata
}

// Preview describes the export without revealing the destination path or
// writing any data. The summary intentionally contains only the field names.
type Preview struct {
	Destination string   `json:"destination"`
	Fields      []string `json:"fields"`
	Network     bool     `json:"network"`
}

type document struct {
	SchemaVersion int      `json:"schema_version"`
	Metadata      Metadata `json:"metadata"`
}

var (
	versionPattern  = regexp.MustCompile(`^v?[0-9]+(\.[0-9A-Za-z-]+){0,3}$`)
	platformPattern = regexp.MustCompile(`^(darwin|windows|linux|wsl[12])-(amd64|arm64)$`)
)

var allowedStatuses = map[string]bool{
	"ready": true, "action_required": true, "manual": true, "unsupported": true,
	"blocked": true, "failed": true, "unknown": true,
}
var allowedOperations = map[string]bool{
	"setup": true, "resume": true, "delete": true, "readiness": true, "install": true,
}

// PreviewRequest validates a request and returns a privacy-safe preview.
func PreviewRequest(r Request) (Preview, error) {
	if strings.TrimSpace(r.Destination) == "" {
		return Preview{}, ErrInvalidRequest
	}
	if !validMetadata(r.Metadata) {
		return Preview{}, ErrInvalidMetadata
	}
	return Preview{
		Destination: "private local file (path hidden)",
		Fields:      []string{"version", "platform", "status", "operation"},
		Network:     false,
	}, nil
}

// Export writes an explicitly requested JSON export with mode 0600. It never
// creates parent directories, overwrites a file, or contacts a network service.
// Errors are deliberately static so OS paths and unexpected values stay private.
func Export(r Request) error {
	if _, err := PreviewRequest(r); err != nil {
		return ErrInvalidRequest
	}
	data, err := json.MarshalIndent(document{SchemaVersion: 1, Metadata: r.Metadata}, "", "  ")
	if err != nil {
		return ErrWrite
	}
	f, err := os.OpenFile(r.Destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return ErrWrite
	}
	path := f.Name()
	if _, err := io.WriteString(f, string(data)+"\n"); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return ErrWrite
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return ErrWrite
	}
	return nil
}

func validMetadata(m Metadata) bool {
	return versionPattern.MatchString(m.Version) &&
		platformPattern.MatchString(m.Platform) &&
		allowedStatuses[m.Status] && allowedOperations[m.Operation]
}

// DeleteLocalSession removes only the named local interview session file.
// State names are restricted to the format used by interviewclient; callers
// cannot supply a path or select credentials, project files, or other state.
func DeleteLocalSession(stateRoot, name string) error {
	if !validSessionName(name) || strings.TrimSpace(stateRoot) == "" {
		return ErrDelete
	}
	root, err := filepath.Abs(stateRoot)
	if err != nil {
		return ErrDelete
	}
	rootInfo, err := os.Lstat(root)
	if err != nil || rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return ErrDelete
	}
	dir, err := os.OpenRoot(root)
	if err != nil {
		return ErrDelete
	}
	defer dir.Close()
	entry := "interview-" + name + ".json"
	info, err := dir.Lstat(entry)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return ErrDelete
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return ErrDelete
	}
	if err := dir.Remove(entry); err != nil {
		return ErrDelete
	}
	return nil
}

// DeleteCallbacks isolates deletion orchestration from the storage and chat
// implementations. The remote callback receives the existing recovery token;
// the local callback should remove only that session's state file.
type DeleteCallbacks struct {
	Remote func(context.Context, string) error
	Local  func() error
}

// DeleteSession requests server deletion before removing local state, keeping
// the recovery token available if the remote request fails.
func DeleteSession(ctx context.Context, recoveryToken string, callbacks DeleteCallbacks) error {
	if strings.TrimSpace(recoveryToken) == "" || callbacks.Remote == nil || callbacks.Local == nil {
		return ErrDelete
	}
	if err := callbacks.Remote(ctx, recoveryToken); err != nil {
		return ErrDelete
	}
	if err := callbacks.Local(); err != nil {
		return ErrDelete
	}
	return nil
}

func validSessionName(name string) bool {
	if len(name) < 1 || len(name) > 48 {
		return false
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
