package domain

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestContractFixtures(t *testing.T) {
	valid, err := os.ReadFile("../../testdata/contracts/valid.json")
	if err != nil {
		t.Fatal(err)
	}
	invalid, err := os.ReadFile("../../testdata/contracts/invalid.json")
	if err != nil {
		t.Fatal(err)
	}
	var good struct {
		Cases []struct {
			Type string          `json:"type"`
			JSON json.RawMessage `json:"json"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(valid, &good); err != nil {
		t.Fatal(err)
	}
	if len(good.Cases) == 0 {
		t.Fatal("valid fixture has no cases")
	}
	for _, tc := range good.Cases {
		t.Run("valid/"+tc.Type, func(t *testing.T) {
			if err := DecodeFixture(tc.Type, tc.JSON); err != nil {
				t.Fatalf("DecodeFixture(%s): %v", tc.Type, err)
			}
			encoded, err := reencodeFixture(tc.Type, tc.JSON)
			if err != nil {
				t.Fatal(err)
			}
			want, err := CanonicalJSON(tc.JSON)
			if err != nil {
				t.Fatal(err)
			}
			got, err := CanonicalJSON(encoded)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(want) {
				t.Fatalf("canonical round trip differs\n got: %s\nwant: %s", got, want)
			}
		})
	}
	var bad struct {
		Cases []struct {
			Name string          `json:"name"`
			Type string          `json:"type"`
			JSON json.RawMessage `json:"json"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(invalid, &bad); err != nil {
		t.Fatal(err)
	}
	if len(bad.Cases) == 0 {
		t.Fatal("invalid fixture has no cases")
	}
	for _, tc := range bad.Cases {
		t.Run("invalid/"+tc.Name, func(t *testing.T) {
			if err := DecodeFixture(tc.Type, tc.JSON); err == nil {
				t.Fatalf("DecodeFixture(%s) accepted invalid fixture %s", tc.Type, tc.Name)
			}
		})
	}
}

func TestCanonicalDigest(t *testing.T) {
	first := Plan{SchemaVersion: 1, ID: "p-1", Operations: []Operation{{ID: "op-1", Kind: "write", TargetRoot: "project", RelativePath: "README.md"}}, Warnings: []string{"one"}}
	second := first
	second.Providers = map[string]string{"codex": "openrouter", "claude": "native"}
	first.Providers = map[string]string{"claude": "native", "codex": "openrouter"}
	left, err := CanonicalPlanDigest(first)
	if err != nil {
		t.Fatal(err)
	}
	right, err := CanonicalPlanDigest(second)
	if err != nil {
		t.Fatal(err)
	}
	if left != right {
		t.Fatalf("map order changed digest: %s != %s", left, right)
	}
	second.Digest = strings.Repeat("f", 64)
	if withDigest, err := CanonicalPlanDigest(second); err != nil || withDigest != left {
		t.Fatalf("digest field changed canonical digest: %s, %v", withDigest, err)
	}
	second.Operations[0].RelativePath = "AGENTS.md"
	changed, err := CanonicalPlanDigest(second)
	if err != nil {
		t.Fatal(err)
	}
	if left == changed {
		t.Fatal("changed operation did not change digest")
	}
}

func TestCanonicalJSONPreservesArrayOrder(t *testing.T) {
	a, err := CanonicalJSON([]byte(`{"steps":["first","second"]}`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := CanonicalJSON([]byte(`{"steps":["second","first"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(a) == string(b) {
		t.Fatal("canonical JSON reordered an array")
	}
}

func TestRejectDuplicateKeys(t *testing.T) {
	_, err := DecodeProjectBrief([]byte(`{"schema_version":1,"schema_version":1,"project_summary":"x","desired_slice":"","experience":"beginner","constraints":[],"known_stack":[],"project_kind":"demo","existing_project":false}`))
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "duplicate") {
		t.Fatalf("expected duplicate-key error, got %v", err)
	}
}

func reencodeFixture(kind string, data []byte) ([]byte, error) {
	var value any
	var err error
	switch kind {
	case "project_brief":
		value, err = DecodeProjectBrief(data)
	case "machine_facts":
		value, err = DecodeMachineFacts(data)
	case "chat_setup_context_envelope":
		value, err = DecodeSetupContextEnvelope(data)
	case "recommendation":
		value, err = DecodeRecommendation(data)
	case "recipe":
		value, err = DecodeRecipe(data)
	case "pack":
		value, err = DecodePack(data)
	case "plan":
		value, err = DecodePlan(data)
	case "receipt":
		value, err = DecodeReceipt(data)
	case "approval":
		value, err = DecodeApproval(data)
	case "check_result":
		value, err = DecodeCheckResult(data)
	default:
		return nil, fmt.Errorf("unknown fixture type %q", kind)
	}
	if err != nil {
		return nil, err
	}
	return json.Marshal(value)
}
