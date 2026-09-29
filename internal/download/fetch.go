package download

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	maxRedirects        = 10
	defaultFetchTimeout = 5 * time.Minute
	maxSupportedBytes   = int64(8 << 30)
)

var sha256Pattern = regexp.MustCompile(`^[a-fA-F0-9]{64}$`)

// Artifact is trusted catalog metadata. Callers must not populate it from
// participant or model input.
type Artifact struct {
	URL    string
	SHA256 string
	Size   int64
}

// Fetch downloads an HTTPS artifact into a digest-addressed private cache.
// The response is never made visible at its final cache path until its exact
// size and SHA-256 have been verified.
func Fetch(ctx context.Context, client *http.Client, artifact Artifact, allowedHosts []string, maxBytes int64, cacheDir string) (string, error) {
	if ctx == nil {
		return "", errors.New("download context is nil")
	}
	if client == nil {
		return "", errors.New("download HTTP client is nil")
	}
	if maxBytes <= 0 || maxBytes > maxSupportedBytes || artifact.Size <= 0 || artifact.Size > maxBytes {
		return "", errors.New("artifact size is outside the configured limit")
	}
	if !sha256Pattern.MatchString(artifact.SHA256) {
		return "", errors.New("artifact SHA-256 is invalid")
	}
	parsed, err := url.Parse(artifact.URL)
	if err != nil || parsed == nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return "", errors.New("artifact URL must be an absolute HTTPS URL without user info or fragment")
	}
	if !approvedHost(parsed, allowedHosts) {
		return "", errors.New("artifact URL host is not approved")
	}
	if cacheDir == "" {
		return "", errors.New("artifact cache directory is required")
	}
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		return "", fmt.Errorf("create artifact cache: %w", err)
	}
	cacheInfo, err := os.Lstat(cacheDir)
	if err != nil {
		return "", fmt.Errorf("inspect artifact cache: %w", err)
	}
	if !cacheInfo.IsDir() || cacheInfo.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("artifact cache must be a real directory")
	}
	if err := os.Chmod(cacheDir, 0700); err != nil {
		return "", fmt.Errorf("make artifact cache private: %w", err)
	}
	sha := strings.ToLower(artifact.SHA256)
	finalPath := filepath.Join(cacheDir, sha)
	if err := verifyCached(finalPath, sha, artifact.Size); err == nil {
		return finalPath, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("verify cached artifact: %w", err)
	}

	fetchCtx, cancel := context.WithTimeout(ctx, defaultFetchTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return "", fmt.Errorf("create artifact request: %w", err)
	}
	guarded := *client
	originalRedirect := client.CheckRedirect
	guarded.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return errors.New("artifact exceeded redirect limit")
		}
		if req.URL.Scheme != "https" || !approvedHost(req.URL, allowedHosts) || req.URL.User != nil || req.URL.Fragment != "" {
			return errors.New("artifact redirect target is not approved")
		}
		if originalRedirect != nil {
			return originalRedirect(req, via)
		}
		return nil
	}
	response, err := guarded.Do(request)
	if err != nil {
		return "", fmt.Errorf("fetch artifact: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("fetch artifact: unexpected HTTP status %d", response.StatusCode)
	}
	if response.ContentLength > maxBytes {
		return "", errors.New("artifact response exceeds configured size limit")
	}

	temp, err := os.CreateTemp(cacheDir, ".anza-download-*")
	if err != nil {
		return "", fmt.Errorf("create temporary artifact: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0600); err != nil {
		_ = temp.Close()
		return "", fmt.Errorf("set temporary artifact permissions: %w", err)
	}
	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temp, hasher), io.LimitReader(response.Body, maxBytes+1))
	if copyErr != nil {
		_ = temp.Close()
		return "", fmt.Errorf("read artifact response: %w", copyErr)
	}
	if written > maxBytes {
		_ = temp.Close()
		return "", errors.New("artifact response exceeds configured size limit")
	}
	if written != artifact.Size {
		_ = temp.Close()
		return "", fmt.Errorf("artifact size mismatch: received %d bytes, expected %d", written, artifact.Size)
	}
	gotDigest := hex.EncodeToString(hasher.Sum(nil))
	if gotDigest != sha {
		_ = temp.Close()
		return "", errors.New("artifact SHA-256 mismatch")
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return "", fmt.Errorf("flush verified artifact: %w", err)
	}
	if err := temp.Close(); err != nil {
		return "", fmt.Errorf("close verified artifact: %w", err)
	}
	if err := os.Link(tempPath, finalPath); err != nil {
		if errors.Is(err, os.ErrExist) {
			if verifyErr := verifyCached(finalPath, sha, artifact.Size); verifyErr == nil {
				return finalPath, nil
			}
		}
		return "", fmt.Errorf("publish verified artifact to cache: %w", err)
	}
	if err := os.Remove(tempPath); err != nil {
		return "", fmt.Errorf("remove temporary artifact: %w", err)
	}
	return finalPath, nil
}

func verifyCached(path, expectedDigest string, expectedSize int64) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != expectedSize {
		return errors.New("cached artifact is not a regular file of the expected size")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	hasher := sha256.New()
	_, copyErr := io.Copy(hasher, io.LimitReader(file, expectedSize+1))
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if hex.EncodeToString(hasher.Sum(nil)) != expectedDigest {
		return errors.New("cached artifact digest mismatch")
	}
	return nil
}

func approvedHost(target *url.URL, allowedHosts []string) bool {
	if target == nil || target.Hostname() == "" {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(target.Host, "."))
	hostname := strings.ToLower(strings.TrimSuffix(target.Hostname(), "."))
	port := target.Port()
	for _, allowed := range allowedHosts {
		allowed = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(allowed), "."))
		if allowed == "" {
			continue
		}
		if allowed == host {
			return true
		}
		if allowed == hostname && (port == "" || port == "443") {
			return true
		}
	}
	return false
}
