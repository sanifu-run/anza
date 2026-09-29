package configedit

import (
	"bytes"
	"strings"
	"testing"
)

func TestPreserveUnknownConfig(t *testing.T) {
	tests := []struct {
		name   string
		format Format
		before []byte
		patch  Patch
		want   []string
	}{
		{
			name:   "json preserving unrelated keys and layout",
			format: JSON,
			before: []byte("{\n  \"user\": {\"theme\": \"dark\"},\n  \"model\": \"existing\"\n}\n"),
			patch:  Patch{Format: JSON, Set: map[string]any{"model_reasoning_effort": "high"}},
			want:   []string{"\"user\": {\"theme\": \"dark\"}", "\"model\": \"existing\"", "\"model_reasoning_effort\": \"high\""},
		},
		{
			name:   "json nested supported key",
			format: JSON,
			before: []byte("{\n  \"settings\": {\n  }\n}\n"),
			patch:  Patch{Format: JSON, Set: map[string]any{"settings.anza_mode": "safe"}},
			want:   []string{"\"settings\": {", "\"anza_mode\": \"safe\""},
		},
		{
			name:   "toml preserving comments and unrelated keys",
			format: TOML,
			before: []byte("# keep this comment\nmodel = \"existing\"\n\n[features] # retain table comment\nother = true # keep inline comment\n"),
			patch:  Patch{Format: TOML, Set: map[string]any{"model_reasoning_effort": "high"}},
			want:   []string{"# keep this comment", "model = \"existing\"", "[features] # retain table comment", "other = true # keep inline comment", "model_reasoning_effort = \"high\""},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			edit, err := Plan(tc.before, tc.patch)
			if err != nil {
				t.Fatal(err)
			}
			for _, fragment := range tc.want {
				if !bytes.Contains(edit.Bytes, []byte(fragment)) {
					t.Errorf("output did not preserve/add %q:\n%s", fragment, edit.Bytes)
				}
			}
			if edit.PreimageHash == "" || edit.PostimageHash == "" || len(edit.OwnedKeys) != 1 {
				t.Fatalf("missing edit metadata: %#v", edit)
			}
			if bytes.Equal(tc.before, edit.Bytes) {
				t.Fatal("requested setting was not added")
			}
		})
	}
}

func TestConfigConflict(t *testing.T) {
	before := []byte("{\n  \"model\": \"user-choice\"\n}\n")
	_, err := Plan(before, Patch{Format: JSON, Set: map[string]any{"model": "anza-default"}})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "conflict") {
		t.Fatalf("expected user-key conflict, got %v", err)
	}
	_, err = Plan(before, Patch{Format: JSON, Remove: []string{"model"}})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "conflict") {
		t.Fatalf("expected conflict when removing unowned user key, got %v", err)
	}
	if string(before) != "{\n  \"model\": \"user-choice\"\n}\n" {
		t.Fatal("Plan modified input bytes")
	}
}

func TestDriftAwareInverse(t *testing.T) {
	before := []byte("{\n  \"unrelated\": 1\n}\n")
	planned, err := Plan(before, Patch{Format: JSON, Set: map[string]any{"model_reasoning_effort": "high"}})
	if err != nil {
		t.Fatal(err)
	}
	current := bytes.Replace(planned.Bytes, []byte(`"model_reasoning_effort": "high"`), []byte(`"model_reasoning_effort": "user-edit"`), 1)
	current = bytes.Replace(current, []byte(`"unrelated": 1`), []byte(`"unrelated": 2`), 1)
	undo, err := Inverse(current, planned)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(undo.Bytes, []byte(`"model_reasoning_effort": "user-edit"`)) {
		t.Fatalf("inverse overwrote participant edit:\n%s", undo.Bytes)
	}
	if !bytes.Contains(undo.Bytes, []byte(`"unrelated": 2`)) {
		t.Fatalf("inverse restored stale unrelated data:\n%s", undo.Bytes)
	}
	if len(undo.SkippedKeys) != 1 || undo.SkippedKeys[0] != "model_reasoning_effort" {
		t.Fatalf("expected drift to be reported, got %#v", undo.SkippedKeys)
	}
}

func TestInverseRestoresOnlyOwnedSetting(t *testing.T) {
	for _, tc := range []struct {
		name   string
		format Format
		before []byte
	}{
		{name: "json", format: JSON, before: []byte("{\n  \"user\": 1\n}\n")},
		{name: "toml", format: TOML, before: []byte("# preserved\nmodel = \"current\"\n[other]\nkeep = true\n")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			planned, err := Plan(tc.before, Patch{Format: tc.format, Set: map[string]any{"model_reasoning_effort": "high"}})
			if err != nil {
				t.Fatal(err)
			}
			undo, err := Inverse(planned.Bytes, planned)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(undo.Bytes, tc.before) {
				t.Fatalf("inverse did not restore exact original bytes\n got: %q\nwant: %q", undo.Bytes, tc.before)
			}
		})
	}
}

func TestPreviouslyOwnedKeyCanBeUpdated(t *testing.T) {
	first, err := Plan([]byte("{}"), Patch{Format: JSON, Set: map[string]any{"model_reasoning_effort": "high"}})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := Plan(first.Bytes, Patch{Format: JSON, Set: map[string]any{"model_reasoning_effort": "low"}, Expected: map[string]any{"model_reasoning_effort": "high"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.OwnedKeys) != 1 || !bytes.Contains(updated.Bytes, []byte(`"model_reasoning_effort":"low"`)) {
		t.Fatalf("managed value did not update: %#v\n%s", updated.OwnedKeys, updated.Bytes)
	}
	undo, err := Inverse(updated.Bytes, updated)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(undo.Bytes, first.Bytes) {
		t.Fatalf("inverse did not restore prior owned value\n got: %q\nwant: %q", undo.Bytes, first.Bytes)
	}
}

func TestMissingJSONConfigDiffersFromMalformedExisting(t *testing.T) {
	missing, err := Plan(nil, Patch{Format: JSON, Set: map[string]any{"model": "first"}})
	if err != nil {
		t.Fatal(err)
	}
	if !missing.InputMissing {
		t.Fatal("nil input was not recorded as a missing file")
	}
	if _, err := Plan([]byte{}, Patch{Format: JSON, Set: map[string]any{"model": "first"}}); err == nil {
		t.Fatal("empty existing JSON file was treated as missing")
	}
}

func TestMalformedConfig(t *testing.T) {
	for _, tc := range []struct {
		name   string
		format Format
		data   []byte
	}{
		{name: "malformed json", format: JSON, data: []byte(`{"x":`)},
		{name: "duplicate json", format: JSON, data: []byte(`{"x":1,"x":2}`)},
		{name: "malformed toml", format: TOML, data: []byte("[broken\nx = true\n")},
		{name: "duplicate toml", format: TOML, data: []byte("x = 1\nx = 2\n")},
		{name: "unsupported multiline toml", format: TOML, data: []byte("prompt = \"\"\"multi\nline\"\"\"\n")},
		{name: "toml leading zero integer", format: TOML, data: []byte("count = 012\n")},
		{name: "toml malformed integer separator", format: TOML, data: []byte("count = 1__2\n")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Plan(tc.data, Patch{Format: tc.format, Set: map[string]any{"anza.setting": true}})
			if err == nil {
				t.Fatal("expected malformed or duplicate syntax to be refused")
			}
		})
	}
}
