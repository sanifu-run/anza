package configedit

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

type Format string

const (
	JSON Format = "json"
	TOML Format = "toml"
)

// Patch describes exact keys an Anza operation wants to set or remove.
// Expected supplies the previously observed value for any existing key that
// Anza already owns; an unowned conflicting value is never replaced.
type Patch struct {
	Format   Format
	Set      map[string]any
	Remove   []string
	Expected map[string]any
}

// OwnedKey records only the value transition made by this patch. Raw values
// retain each format's exact representation for a drift-aware inverse.
type OwnedKey struct {
	Path          string
	BeforePresent bool
	BeforeValue   []byte
	AfterPresent  bool
	AfterValue    []byte
}

// Edit is a pure planned edit. Bytes are returned to the caller; no file I/O
// occurs in this package.
type Edit struct {
	Format        Format
	Bytes         []byte
	InputMissing  bool
	PreimageHash  string
	PostimageHash string
	OwnedKeys     []OwnedKey
	SkippedKeys   []string
}

var ErrConflict = errors.New("configuration conflict")

// Plan parses existing configuration and returns a preserving patch. A nil
// input means the file is missing; a non-nil empty JSON document is malformed.
func Plan(existing []byte, requested Patch) (Edit, error) {
	if requested.Format != JSON && requested.Format != TOML {
		return Edit{}, fmt.Errorf("unsupported config format %q", requested.Format)
	}
	if err := validatePatch(requested); err != nil {
		return Edit{}, err
	}
	preimage := cloneBytes(existing)
	working := cloneBytes(existing)
	if existing == nil {
		if requested.Format == JSON {
			working = []byte("{}")
		} else {
			working = []byte{}
		}
	}
	if err := validateDocument(requested.Format, working); err != nil {
		return Edit{}, fmt.Errorf("parsing existing %s config: %w", requested.Format, err)
	}
	edit := Edit{Format: requested.Format, Bytes: working, InputMissing: existing == nil, PreimageHash: digest(preimage)}
	setKeys := make([]string, 0, len(requested.Set))
	for key := range requested.Set {
		setKeys = append(setKeys, key)
	}
	sort.Strings(setKeys)
	for _, key := range setKeys {
		want, err := encodeValue(requested.Format, requested.Set[key])
		if err != nil {
			return Edit{}, fmt.Errorf("encoding %s: %w", key, err)
		}
		before, present, err := lookup(requested.Format, edit.Bytes, key)
		if err != nil {
			return Edit{}, fmt.Errorf("reading %s: %w", key, err)
		}
		if present && equalValue(requested.Format, before, want) {
			continue
		}
		if present {
			expected, owned := requested.Expected[key]
			if !owned {
				return Edit{}, conflict(key, "existing key is not owned by Anza")
			}
			expectedValue, err := encodeValue(requested.Format, expected)
			if err != nil {
				return Edit{}, fmt.Errorf("encoding expected value for %s: %w", key, err)
			}
			if !equalValue(requested.Format, before, expectedValue) {
				return Edit{}, conflict(key, "current value differs from recorded Anza value")
			}
		} else if _, owned := requested.Expected[key]; owned {
			return Edit{}, conflict(key, "recorded Anza key is missing")
		}
		updated, err := modify(requested.Format, edit.Bytes, key, want, false)
		if err != nil {
			return Edit{}, fmt.Errorf("planning %s: %w", key, err)
		}
		edit.OwnedKeys = append(edit.OwnedKeys, OwnedKey{Path: key, BeforePresent: present, BeforeValue: cloneBytes(before), AfterPresent: true, AfterValue: cloneBytes(want)})
		edit.Bytes = updated
	}
	removeKeys := append([]string(nil), requested.Remove...)
	sort.Strings(removeKeys)
	for _, key := range removeKeys {
		before, present, err := lookup(requested.Format, edit.Bytes, key)
		if err != nil {
			return Edit{}, fmt.Errorf("reading %s: %w", key, err)
		}
		if !present {
			continue
		}
		expected, owned := requested.Expected[key]
		if !owned {
			return Edit{}, conflict(key, "existing key is not owned by Anza")
		}
		expectedValue, err := encodeValue(requested.Format, expected)
		if err != nil {
			return Edit{}, fmt.Errorf("encoding expected value for %s: %w", key, err)
		}
		if !equalValue(requested.Format, before, expectedValue) {
			return Edit{}, conflict(key, "current value differs from recorded Anza value")
		}
		updated, err := modify(requested.Format, edit.Bytes, key, nil, true)
		if err != nil {
			return Edit{}, fmt.Errorf("planning removal of %s: %w", key, err)
		}
		edit.OwnedKeys = append(edit.OwnedKeys, OwnedKey{Path: key, BeforePresent: true, BeforeValue: cloneBytes(before), AfterPresent: false})
		edit.Bytes = updated
	}
	if err := validateDocument(requested.Format, edit.Bytes); err != nil {
		return Edit{}, fmt.Errorf("validating planned %s config: %w", requested.Format, err)
	}
	edit.PostimageHash = digest(edit.Bytes)
	return edit, nil
}

// Inverse restores only values recorded in applied that still equal the
// applied postimage. Changed or newly replaced values are preserved and listed
// in SkippedKeys; unrelated bytes are never replaced from an old backup.
func Inverse(current []byte, applied Edit) (Edit, error) {
	if applied.Format != JSON && applied.Format != TOML {
		return Edit{}, fmt.Errorf("unsupported config format %q", applied.Format)
	}
	working := cloneBytes(current)
	if err := validateDocument(applied.Format, working); err != nil {
		return Edit{}, fmt.Errorf("parsing current %s config: %w", applied.Format, err)
	}
	result := Edit{Format: applied.Format, Bytes: working, PreimageHash: digest(current)}
	for i := len(applied.OwnedKeys) - 1; i >= 0; i-- {
		owned := applied.OwnedKeys[i]
		actual, present, err := lookup(applied.Format, result.Bytes, owned.Path)
		if err != nil {
			return Edit{}, fmt.Errorf("reading %s: %w", owned.Path, err)
		}
		if present != owned.AfterPresent || present && !equalValue(applied.Format, actual, owned.AfterValue) {
			result.SkippedKeys = append(result.SkippedKeys, owned.Path)
			continue
		}
		var updated []byte
		if owned.BeforePresent {
			updated, err = modify(applied.Format, result.Bytes, owned.Path, owned.BeforeValue, false)
		} else {
			updated, err = modify(applied.Format, result.Bytes, owned.Path, nil, true)
		}
		if err != nil {
			return Edit{}, fmt.Errorf("reversing %s: %w", owned.Path, err)
		}
		result.OwnedKeys = append(result.OwnedKeys, OwnedKey{Path: owned.Path, BeforePresent: present, BeforeValue: cloneBytes(actual), AfterPresent: owned.BeforePresent, AfterValue: cloneBytes(owned.BeforeValue)})
		result.Bytes = updated
	}
	if err := validateDocument(applied.Format, result.Bytes); err != nil {
		return Edit{}, fmt.Errorf("validating inverse %s config: %w", applied.Format, err)
	}
	result.PostimageHash = digest(result.Bytes)
	return result, nil
}

func validatePatch(p Patch) error {
	if len(p.Set) == 0 && len(p.Remove) == 0 {
		return errors.New("patch has no changes")
	}
	for key := range p.Set {
		if err := validatePath(key); err != nil {
			return err
		}
	}
	seen := make(map[string]struct{}, len(p.Remove))
	for _, key := range p.Remove {
		if err := validatePath(key); err != nil {
			return err
		}
		if _, ok := p.Set[key]; ok {
			return fmt.Errorf("key %s cannot be both set and removed", key)
		}
		if _, ok := seen[key]; ok {
			return fmt.Errorf("duplicate removal for %s", key)
		}
		seen[key] = struct{}{}
	}
	for key := range p.Expected {
		if _, set := p.Set[key]; !set {
			if _, remove := seen[key]; !remove {
				return fmt.Errorf("expected ownership value supplied for unchanged key %s", key)
			}
		}
		if err := validatePath(key); err != nil {
			return err
		}
	}
	return nil
}

func validatePath(path string) error {
	if strings.TrimSpace(path) != path || path == "" {
		return fmt.Errorf("invalid empty or whitespace-padded config key")
	}
	for _, segment := range strings.Split(path, ".") {
		if segment == "" {
			return fmt.Errorf("invalid config key path %q", path)
		}
		for i, r := range segment {
			valid := r == '_' || r == '-' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || i > 0 && r >= '0' && r <= '9'
			if !valid {
				return fmt.Errorf("unsupported config key syntax %q", path)
			}
		}
	}
	return nil
}

func encodeValue(format Format, value any) ([]byte, error) {
	if format == JSON {
		return json.Marshal(value)
	}
	return encodeTOMLValue(value)
}

func equalValue(format Format, left, right []byte) bool {
	if format == JSON {
		var a, b any
		decA := json.NewDecoder(bytes.NewReader(left))
		decA.UseNumber()
		decB := json.NewDecoder(bytes.NewReader(right))
		decB.UseNumber()
		if decA.Decode(&a) != nil || decB.Decode(&b) != nil {
			return false
		}
		return reflect.DeepEqual(a, b)
	}
	a, errA := parseTOMLValue(left)
	b, errB := parseTOMLValue(right)
	return errA == nil && errB == nil && reflect.DeepEqual(a, b)
}

func lookup(format Format, data []byte, key string) ([]byte, bool, error) {
	if format == JSON {
		return jsonLookup(data, key)
	}
	return tomlLookup(data, key)
}

func modify(format Format, data []byte, key string, value []byte, remove bool) ([]byte, error) {
	if format == JSON {
		return jsonModify(data, key, value, remove)
	}
	return tomlModify(data, key, value, remove)
}

func validateDocument(format Format, data []byte) error {
	if format == JSON {
		_, err := parseJSONConfig(data)
		return err
	}
	_, err := parseTOMLConfig(data)
	return err
}

func conflict(key, detail string) error { return fmt.Errorf("%w: %s: %s", ErrConflict, key, detail) }
func digest(data []byte) string         { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func cloneBytes(data []byte) []byte     { return append([]byte(nil), data...) }
