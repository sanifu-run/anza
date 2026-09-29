// Package catalog loads a reviewed, versioned catalog from a caller-supplied
// read-only filesystem. It performs no network access and owns no filesystem.
package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/sanifu-run/anza/internal/domain"
)

const manifestName = "manifest.json"

// Entry identifies one checksummed catalog asset. Recipe and pack entries are
// decoded against the v1 contract; skill entries are opaque reviewed text.
type Entry struct {
	Kind          string   `json:"kind"`
	ID            string   `json:"id"`
	Path          string   `json:"path"`
	SHA256        string   `json:"sha256"`
	ProvenanceIDs []string `json:"provenance_ids"`
	Dependencies  []string `json:"dependencies"`
}

// Provenance records the source and license evidence required by an asset.
type Provenance struct {
	ID      string `json:"id"`
	Source  string `json:"source"`
	License string `json:"license"`
}

type manifest struct {
	SchemaVersion int          `json:"schema_version"`
	Version       string       `json:"version"`
	Entries       []Entry      `json:"entries"`
	Provenance    []Provenance `json:"provenance"`
}

// Catalog is immutable after Load. Accessors return copies so callers cannot
// mutate the validated snapshot.
type Catalog struct {
	version   string
	digest    string
	recipes   map[string]domain.Recipe
	packs     map[string]domain.Pack
	exercises map[string]domain.Exercise
	skills    map[string]struct{}
}

// Load reads and validates a catalog rooted at fsys. All reads are relative to
// that fs.FS; it never writes or performs runtime fetches.
func Load(fsys fs.FS) (*Catalog, error) {
	if fsys == nil {
		return nil, errors.New("catalog filesystem is nil")
	}
	manifestBytes, err := fs.ReadFile(fsys, manifestName)
	if err != nil {
		return nil, fmt.Errorf("reading catalog manifest: %w", err)
	}
	canonicalManifest, err := domain.CanonicalJSON(manifestBytes)
	if err != nil {
		return nil, fmt.Errorf("canonicalizing catalog manifest: %w", err)
	}
	var m manifest
	dec := json.NewDecoder(strings.NewReader(string(canonicalManifest)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("decoding catalog manifest: %w", err)
	}
	if err := ensureEOF(dec); err != nil {
		return nil, fmt.Errorf("decoding catalog manifest: %w", err)
	}
	if m.SchemaVersion != 1 || strings.TrimSpace(m.Version) == "" || len(m.Version) > 100 {
		return nil, errors.New("catalog manifest requires schema_version 1 and a version")
	}

	provenance := make(map[string]Provenance, len(m.Provenance))
	for _, p := range m.Provenance {
		if !validID(p.ID) || strings.TrimSpace(p.Source) == "" || strings.TrimSpace(p.License) == "" {
			return nil, fmt.Errorf("provenance %q requires a valid id, source, and license", p.ID)
		}
		if _, exists := provenance[p.ID]; exists {
			return nil, fmt.Errorf("duplicate provenance id %q", p.ID)
		}
		provenance[p.ID] = p
	}

	cat := &Catalog{
		version:   m.Version,
		recipes:   make(map[string]domain.Recipe),
		packs:     make(map[string]domain.Pack),
		exercises: make(map[string]domain.Exercise),
		skills:    make(map[string]struct{}),
	}
	entries := make(map[string]Entry, len(m.Entries))
	paths := make(map[string]struct{}, len(m.Entries))
	for _, entry := range m.Entries {
		if !validID(entry.ID) {
			return nil, fmt.Errorf("entry has invalid id %q", entry.ID)
		}
		if entry.Kind != "recipe" && entry.Kind != "pack" && entry.Kind != "skill" && entry.Kind != "exercise" {
			return nil, fmt.Errorf("entry %q has unsupported kind %q", entry.ID, entry.Kind)
		}
		if _, exists := entries[entry.ID]; exists {
			return nil, fmt.Errorf("duplicate catalog id %q", entry.ID)
		}
		if !fs.ValidPath(entry.Path) || entry.Path == manifestName || path.IsAbs(entry.Path) {
			return nil, fmt.Errorf("entry %q has unsafe path %q", entry.ID, entry.Path)
		}
		if _, exists := paths[entry.Path]; exists {
			return nil, fmt.Errorf("duplicate catalog path %q", entry.Path)
		}
		paths[entry.Path] = struct{}{}
		if !isDigest(entry.SHA256) {
			return nil, fmt.Errorf("entry %q has invalid sha256", entry.ID)
		}
		if len(entry.ProvenanceIDs) == 0 {
			return nil, fmt.Errorf("entry %q has no provenance", entry.ID)
		}
		for _, id := range entry.ProvenanceIDs {
			if _, ok := provenance[id]; !ok {
				return nil, fmt.Errorf("entry %q references missing provenance %q", entry.ID, id)
			}
		}
		contents, err := fs.ReadFile(fsys, entry.Path)
		if err != nil {
			return nil, fmt.Errorf("reading catalog entry %q: %w", entry.ID, err)
		}
		if got := sum(contents); got != entry.SHA256 {
			return nil, fmt.Errorf("entry %q digest mismatch: got %s", entry.ID, got)
		}
		entries[entry.ID] = entry
		switch entry.Kind {
		case "recipe":
			value, err := domain.DecodeRecipe(contents)
			if err != nil {
				return nil, fmt.Errorf("validating recipe %q: %w", entry.ID, err)
			}
			if value.ID != entry.ID {
				return nil, fmt.Errorf("recipe entry id %q does not match payload id %q", entry.ID, value.ID)
			}
			if strings.TrimSpace(value.LicenseNotes) == "" {
				return nil, fmt.Errorf("recipe %q requires license notes", entry.ID)
			}
			if value.Artifact != nil && strings.TrimSpace(value.Artifact.Origin) == "" {
				return nil, fmt.Errorf("recipe %q has an artifact with no source", entry.ID)
			}
			if err := validatePlatforms(value.SupportedPlatforms); err != nil {
				return nil, fmt.Errorf("recipe %q: %w", entry.ID, err)
			}
			cat.recipes[entry.ID] = value
		case "pack":
			value, err := domain.DecodePack(contents)
			if err != nil {
				return nil, fmt.Errorf("validating pack %q: %w", entry.ID, err)
			}
			if value.ID != entry.ID {
				return nil, fmt.Errorf("pack entry id %q does not match payload id %q", entry.ID, value.ID)
			}
			if len(value.LicenseIDs) == 0 || len(value.ProvenanceIDs) == 0 {
				return nil, fmt.Errorf("pack %q requires license and provenance references", entry.ID)
			}
			for _, id := range value.LicenseIDs {
				p, ok := provenance[id]
				if !ok || strings.TrimSpace(p.License) == "" {
					return nil, fmt.Errorf("pack %q references missing license %q", entry.ID, id)
				}
			}
			for _, id := range value.ProvenanceIDs {
				if _, ok := provenance[id]; !ok {
					return nil, fmt.Errorf("pack %q references missing provenance %q", entry.ID, id)
				}
			}
			for relative, expected := range value.FileDigests {
				if !fs.ValidPath(relative) || relative == "." || !isDigest(expected) {
					return nil, fmt.Errorf("pack %q has invalid file digest entry %q", entry.ID, relative)
				}
				assetPath := path.Join(path.Dir(entry.Path), relative)
				asset, err := fs.ReadFile(fsys, assetPath)
				if err != nil {
					return nil, fmt.Errorf("reading pack asset %q: %w", relative, err)
				}
				if got := sum(asset); got != expected {
					return nil, fmt.Errorf("pack %q asset %q digest mismatch", entry.ID, relative)
				}
			}
			cat.packs[entry.ID] = value
		case "exercise":
			value, err := domain.DecodeExercise(contents)
			if err != nil {
				return nil, fmt.Errorf("validating exercise %q: %w", entry.ID, err)
			}
			if value.ID != entry.ID {
				return nil, fmt.Errorf("exercise entry id %q does not match payload id %q", entry.ID, value.ID)
			}
			for _, scenario := range value.Scenarios {
				if err := validatePlatforms(scenario.SupportedPlatforms); err != nil {
					return nil, fmt.Errorf("exercise %q scenario %q: %w", entry.ID, scenario.ID, err)
				}
			}
			cat.exercises[entry.ID] = value
		case "skill":
			if !strings.HasPrefix(entry.ID, "anza-") || len(contents) == 0 {
				return nil, fmt.Errorf("skill %q must use anza- ID and contain content", entry.ID)
			}
			cat.skills[entry.ID] = struct{}{}
		}
	}

	for _, entry := range m.Entries {
		refs := append([]string(nil), entry.Dependencies...)
		if recipe, ok := cat.recipes[entry.ID]; ok {
			refs = append(refs, recipe.Prerequisites...)
		}
		if pack, ok := cat.packs[entry.ID]; ok {
			refs = append(refs, pack.Prerequisites...)
			for _, skillID := range pack.SkillIDs {
				if _, ok := cat.skills[skillID]; !ok {
					return nil, fmt.Errorf("pack %q references missing skill %q", entry.ID, skillID)
				}
			}
		}
		for _, ref := range refs {
			if _, ok := entries[ref]; !ok {
				return nil, fmt.Errorf("entry %q references unknown dependency %q", entry.ID, ref)
			}
		}
		entries[entry.ID] = Entry{ID: entry.ID, Dependencies: uniqueSorted(refs)}
	}
	if err := validateAcyclic(entries); err != nil {
		return nil, err
	}

	cat.digest, err = manifestDigest(m)
	if err != nil {
		return nil, fmt.Errorf("digesting catalog manifest: %w", err)
	}
	return cat, nil
}

func (c *Catalog) Version() string { return c.version }
func (c *Catalog) Digest() string  { return c.digest }

func (c *Catalog) Recipe(id string) (domain.Recipe, bool) {
	v, ok := c.recipes[id]
	if !ok {
		return domain.Recipe{}, false
	}
	v.SupportedPlatforms = cloneStrings(v.SupportedPlatforms)
	if v.Artifact != nil {
		artifact := *v.Artifact
		v.Artifact = &artifact
	}
	v.Prerequisites = cloneStrings(v.Prerequisites)
	v.Privileges = cloneStrings(v.Privileges)
	v.SideEffects = cloneStrings(v.SideEffects)
	return v, true
}

func (c *Catalog) Exercise(id string) (domain.Exercise, bool) {
	v, ok := c.exercises[id]
	if !ok {
		return domain.Exercise{}, false
	}
	v.Scenarios = append([]domain.ExerciseScenario(nil), v.Scenarios...)
	for i := range v.Scenarios {
		v.Scenarios[i].SupportedPlatforms = cloneStrings(v.Scenarios[i].SupportedPlatforms)
		v.Scenarios[i].ManualSteps = cloneStrings(v.Scenarios[i].ManualSteps)
		v.Scenarios[i].ReadinessConstraints = cloneStrings(v.Scenarios[i].ReadinessConstraints)
		v.Scenarios[i].MissingCapabilityIDs = cloneStrings(v.Scenarios[i].MissingCapabilityIDs)
	}
	return v, true
}

func (c *Catalog) Pack(id string) (domain.Pack, bool) {
	v, ok := c.packs[id]
	if !ok {
		return domain.Pack{}, false
	}
	v.SkillIDs = cloneStrings(v.SkillIDs)
	v.Prerequisites = cloneStrings(v.Prerequisites)
	v.LicenseIDs = cloneStrings(v.LicenseIDs)
	v.ProvenanceIDs = cloneStrings(v.ProvenanceIDs)
	v.CompatibleAgentVersions = cloneMap(v.CompatibleAgentVersions)
	v.FileDigests = cloneMap(v.FileDigests)
	return v, true
}

func (c *Catalog) HasSkill(id string) bool { _, ok := c.skills[id]; return ok }

func manifestDigest(m manifest) (string, error) {
	m.Entries = append([]Entry(nil), m.Entries...)
	m.Provenance = append([]Provenance(nil), m.Provenance...)
	for i := range m.Entries {
		m.Entries[i].ProvenanceIDs = uniqueSorted(m.Entries[i].ProvenanceIDs)
		m.Entries[i].Dependencies = uniqueSorted(m.Entries[i].Dependencies)
	}
	sort.Slice(m.Entries, func(i, j int) bool {
		if m.Entries[i].Kind == m.Entries[j].Kind {
			return m.Entries[i].ID < m.Entries[j].ID
		}
		return m.Entries[i].Kind < m.Entries[j].Kind
	})
	sort.Slice(m.Provenance, func(i, j int) bool { return m.Provenance[i].ID < m.Provenance[j].ID })
	data, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return sum(data), nil
}

func validateAcyclic(entries map[string]Entry) error {
	const (
		unseen = iota
		visiting
		done
	)
	state := make(map[string]int, len(entries))
	var visit func(string) error
	visit = func(id string) error {
		if state[id] == visiting {
			return fmt.Errorf("catalog dependency cycle includes %q", id)
		}
		if state[id] == done {
			return nil
		}
		state[id] = visiting
		for _, dependency := range entries[id].Dependencies {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		state[id] = done
		return nil
	}
	ids := make([]string, 0, len(entries))
	for id := range entries {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

func ensureEOF(dec *json.Decoder) error {
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("trailing JSON value")
		}
		return err
	}
	return nil
}
func isDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil && strings.ToLower(s) == s
}
func sum(data []byte) string { value := sha256.Sum256(data); return hex.EncodeToString(value[:]) }
func validID(s string) bool {
	if len(s) == 0 || len(s) > 100 {
		return false
	}
	for i, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || i > 0 && strings.ContainsRune("._-", r)) {
			return false
		}
	}
	return true
}
func uniqueSorted(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, v := range values {
		set[v] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
func cloneStrings(in []string) []string {
	if in == nil {
		return nil
	}
	return append([]string(nil), in...)
}
func cloneMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

var platformPattern = regexp.MustCompile(`^(darwin|windows|linux|wsl1|wsl2)-(amd64|arm64)$`)
var linuxDistroPattern = regexp.MustCompile(`^linux-(ubuntu|debian|alpine|other)(-[0-9]+(\.[0-9]+)*)?-(amd64|arm64)$`)

func validatePlatforms(platforms []string) error {
	seen := make(map[string]struct{}, len(platforms))
	for _, platform := range platforms {
		if _, ok := seen[platform]; ok {
			return fmt.Errorf("duplicate supported platform %q", platform)
		}
		seen[platform] = struct{}{}
		if !platformPattern.MatchString(platform) && !linuxDistroPattern.MatchString(platform) {
			return fmt.Errorf("unsupported platform predicate %q", platform)
		}
	}
	return nil
}
