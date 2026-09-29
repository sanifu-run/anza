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
	case "exercise":
		value, err = DecodeExercise(data)
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

func TestManualRecipeMayOmitArtifact(t *testing.T) {
	data := []byte(`{"id":"git","version":"2.56","description":"Git manual guidance","purpose":"Use the system Git installation","supported_platforms":["linux-amd64"],"prerequisites":[],"detection":"Check the current Git version without changing it.","install_strategy":"manual","privileges":[],"license_notes":"Use the vendor or OS package license notices.","estimated_download_bytes":0,"side_effects":[],"verification":"Verify the selected Git version.","reversal_class":"manual"}`)
	got, err := DecodeRecipe(data)
	if err != nil {
		t.Fatalf("DecodeRecipe(manual without artifact): %v", err)
	}
	if got.Artifact != nil || got.EstimatedDownloadBytes != 0 {
		t.Fatalf("artifact-less manual recipe retained artifact metadata: %+v", got)
	}
}

func TestManualRecipeArtifactPolicy(t *testing.T) {
	base := `{"id":"tool","version":"1","description":"Tool","purpose":"Use tool","supported_platforms":["linux-amd64"],"prerequisites":[],"detection":"Check version","install_strategy":"manual","artifact":{"digest":"not-a-digest","size":1,"origin":"https://example.invalid/tool"},"privileges":[],"license_notes":"License","estimated_download_bytes":0,"side_effects":[],"verification":"Check version","reversal_class":"manual"}`
	if _, err := DecodeRecipe([]byte(base)); err != nil {
		t.Fatalf("manual artifact metadata remains allowed for descriptive legacy recipes: %v", err)
	}
	noArtifactAutomated := strings.Replace(base, `,"artifact":{"digest":"not-a-digest","size":1,"origin":"https://example.invalid/tool"}`, "", 1)
	noArtifactAutomated = strings.Replace(noArtifactAutomated, `"install_strategy":"manual"`, `"install_strategy":"verified_archive"`, 1)
	if _, err := DecodeRecipe([]byte(noArtifactAutomated)); err == nil {
		t.Fatal("automated recipe without artifact was accepted")
	}
	noEstimate := strings.Replace(base, `"estimated_download_bytes":0`, `"estimated_download_bytes":1`, 1)
	noEstimate = strings.Replace(noEstimate, `,"artifact":{"digest":"not-a-digest","size":1,"origin":"https://example.invalid/tool"}`, "", 1)
	if _, err := DecodeRecipe([]byte(noEstimate)); err == nil {
		t.Fatal("artifact-less manual recipe with nonzero download estimate was accepted")
	}
}

func TestExerciseRejectsOverlappingScenarios(t *testing.T) {
	data := []byte(`{"schema_version":1,"id":"mobile-desktop","version":"1","description":"Manual guidance","scenarios":[{"id":"ios-one","project_kind":"ios","supported_platforms":["linux-amd64"],"status":"unsupported","summary":"Requires macOS.","manual_steps":["Use a macOS host."],"missing_capability_ids":["macos-build-host"],"readiness_constraints":[],"verification":"Confirm the host."},{"id":"ios-two","project_kind":"ios","supported_platforms":["linux-amd64"],"status":"manual","summary":"Another result.","manual_steps":["Review requirements."],"missing_capability_ids":[],"readiness_constraints":[],"verification":"Confirm requirements."}]}`)
	if _, err := DecodeExercise(data); err == nil {
		t.Fatal("overlapping project-kind/platform scenarios were accepted")
	}
}
