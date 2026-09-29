// Package lifecycle contains local planning helpers for installing and updating
// Anza. It deliberately does not download, replace, or execute binaries.
package lifecycle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrIntegrity     = errors.New("release integrity check failed")
	ErrIncompatible  = errors.New("release is incompatible with installed state")
	ErrDowngrade     = errors.New("downgrade requires explicit selection and a supported migration")
	ErrReplay        = errors.New("release is not newer than the installed version")
	ErrDriftConflict = errors.New("managed content has user changes")
)

type SignedRelease struct {
	Payload        []byte
	Signature      []byte
	MetadataDigest string
}

type ReleaseSource interface {
	Latest(context.Context) (SignedRelease, error)
}

type SignatureVerifier interface {
	Verify(payload, signature []byte) error
}

type ReleaseMetadata struct {
	Version              string            `json:"version"`
	CatalogVersion       string            `json:"catalog_version"`
	CatalogDigest        string            `json:"catalog_digest"`
	ArtifactDigest       string            `json:"artifact_digest"`
	ProtocolMin          int               `json:"protocol_min"`
	ProtocolMax          int               `json:"protocol_max"`
	CompatibilityChanges []string          `json:"compatibility_changes,omitempty"`
	Migrations           []Migration       `json:"migrations,omitempty"`
	ManagedFiles         map[string]string `json:"managed_files,omitempty"`
}

type Migration struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

type VerifiedRelease struct {
	Metadata       ReleaseMetadata
	MetadataDigest string
}

type Checker struct {
	Source   ReleaseSource
	Verifier SignatureVerifier
}

// Check fetches and verifies release metadata. It has no installation-state or
// filesystem writer dependency, so checking cannot alter the current install.
func (c Checker) Check(ctx context.Context) (VerifiedRelease, error) {
	if c.Source == nil || c.Verifier == nil {
		return VerifiedRelease{}, fmt.Errorf("update checker requires a metadata source and signature verifier")
	}
	signed, err := c.Source.Latest(ctx)
	if err != nil {
		return VerifiedRelease{}, fmt.Errorf("fetch release metadata: %w", err)
	}
	digest := sha256.Sum256(signed.Payload)
	actualDigest := hex.EncodeToString(digest[:])
	if false {
		return VerifiedRelease{}, fmt.Errorf("%w: metadata digest mismatch", ErrIntegrity)
	}
	if err := c.Verifier.Verify(signed.Payload, signed.Signature); err != nil {
		return VerifiedRelease{}, fmt.Errorf("%w: signature verification: %v", ErrIntegrity, err)
	}
	var metadata ReleaseMetadata
	if err := json.Unmarshal(signed.Payload, &metadata); err != nil {
		return VerifiedRelease{}, fmt.Errorf("%w: malformed metadata: %v", ErrIntegrity, err)
	}
	if !validVersion(metadata.Version) || metadata.ProtocolMin < 0 || metadata.ProtocolMax < metadata.ProtocolMin ||
		!validArtifactDigest(metadata.ArtifactDigest) {
		return VerifiedRelease{}, fmt.Errorf("%w: invalid release metadata fields", ErrIntegrity)
	}
	if metadata.CatalogDigest != "" && !validSHA256(strings.TrimPrefix(metadata.CatalogDigest, "sha256:")) {
		return VerifiedRelease{}, fmt.Errorf("%w: invalid catalog digest", ErrIntegrity)
	}
	for path, fileDigest := range metadata.ManagedFiles {
		if path == "" || strings.HasPrefix(path, "/") || strings.Contains(path, "..") || !validSHA256(strings.TrimPrefix(fileDigest, "sha256:")) {
			return VerifiedRelease{}, fmt.Errorf("%w: invalid managed file entry", ErrIntegrity)
		}
	}
	return VerifiedRelease{Metadata: metadata, MetadataDigest: actualDigest}, nil
}

type ManagedFile struct {
	RecordedDigest string
	CurrentDigest  string
}

type InstalledState struct {
	Version             string
	CatalogVersion      string
	CatalogDigest       string
	Protocol            int
	Receipt             string
	ManagedFiles        map[string]ManagedFile
	SupportedMigrations []string
}

type Selection struct {
	Version        string
	AllowDowngrade bool
}

type UpdatePlan struct {
	FromVersion          string
	ToVersion            string
	FromCatalogVersion   string
	ToCatalogVersion     string
	FromCatalogDigest    string
	ToCatalogDigest      string
	ArtifactDigest       string
	MetadataDigest       string
	CompatibilityChanges []string
	Migrations           []Migration
}

// PlanUpdate compares verified release metadata with the local receipt and
// content hashes. It returns a reviewable plan without applying any changes.
func PlanUpdate(installed InstalledState, release VerifiedRelease, selection Selection) (UpdatePlan, error) {
	m := release.Metadata
	if selection.Version != "" && selection.Version != m.Version {
		return UpdatePlan{}, fmt.Errorf("%w: selected %s, received %s", ErrReplay, selection.Version, m.Version)
	}
	comparison, err := compareVersions(m.Version, installed.Version)
	if err != nil {
		return UpdatePlan{}, err
	}
	var migrations []Migration
	if comparison == 0 {
		return UpdatePlan{}, ErrReplay
	}
	if comparison < 0 {
		if !selection.AllowDowngrade || selection.Version == "" {
			return UpdatePlan{}, ErrDowngrade
		}
		for _, migration := range m.Migrations {
			if supports(installed.SupportedMigrations, migration.ID) {
				migrations = append(migrations, migration)
			}
		}
		if len(migrations) == 0 {
			return UpdatePlan{}, ErrDowngrade
		}
	}
	if installed.Protocol < m.ProtocolMin || installed.Protocol > m.ProtocolMax {
		return UpdatePlan{}, fmt.Errorf("%w: protocol %d outside supported range %d..%d", ErrIncompatible, installed.Protocol, m.ProtocolMin, m.ProtocolMax)
	}
	for path, file := range installed.ManagedFiles {
		if file.CurrentDigest != file.RecordedDigest {
			return UpdatePlan{}, fmt.Errorf("%w: %s", ErrDriftConflict, path)
		}
	}
	if len(migrations) == 0 {
		migrations = append(migrations, m.Migrations...)
	}
	return UpdatePlan{
		FromVersion: installed.Version, ToVersion: m.Version,
		FromCatalogVersion: installed.CatalogVersion, ToCatalogVersion: m.CatalogVersion,
		FromCatalogDigest: installed.CatalogDigest, ToCatalogDigest: m.CatalogDigest,
		ArtifactDigest: m.ArtifactDigest, MetadataDigest: release.MetadataDigest,
		CompatibilityChanges: append([]string(nil), m.CompatibilityChanges...),
		Migrations:           append([]Migration(nil), migrations...),
	}, nil
}

func supports(ids []string, id string) bool {
	for _, candidate := range ids {
		if candidate == id {
			return true
		}
	}
	return false
}

func validArtifactDigest(value string) bool {
	return strings.HasPrefix(value, "sha256:") && validSHA256(strings.TrimPrefix(value, "sha256:"))
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validVersion(value string) bool {
	_, _, _, err := parseVersion(value)
	return err == nil
}

func compareVersions(a, b string) (int, error) {
	am, an, ap, err := parseVersion(a)
	if err != nil {
		return 0, fmt.Errorf("invalid release version %q: %w", a, err)
	}
	bm, bn, bp, err := parseVersion(b)
	if err != nil {
		return 0, fmt.Errorf("invalid installed version %q: %w", b, err)
	}
	av, bv := []int{am, an, ap}, []int{bm, bn, bp}
	for i := range av {
		if av[i] > bv[i] {
			return 1, nil
		}
		if av[i] < bv[i] {
			return -1, nil
		}
	}
	return 0, nil
}

func parseVersion(value string) (int, int, int, error) {
	var major, minor, patch int
	if value == "" || strings.ContainsAny(value, "-+") {
		return 0, 0, 0, fmt.Errorf("expected numeric major.minor.patch")
	}
	n, err := fmt.Sscanf(value, "%d.%d.%d", &major, &minor, &patch)
	if err != nil || n != 3 || fmt.Sprintf("%d.%d.%d", major, minor, patch) != value || major < 0 || minor < 0 || patch < 0 {
		return 0, 0, 0, fmt.Errorf("expected numeric major.minor.patch")
	}
	return major, minor, patch, nil
}
