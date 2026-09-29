package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sanifu-run/anza/internal/domain"
	"github.com/sanifu-run/anza/internal/state"
)

func TestReviewAllEffects(t *testing.T) {
	plan := reviewFixture(t)
	recipe := domain.Recipe{
		ID: "codex-cli", Version: "0.157.1", Purpose: "Run the selected coding agent",
		Description: "Install the reviewed command line tool", EstimatedDownloadBytes: 123456,
		Artifact:   &domain.Artifact{Origin: "https://vendor.example/download", Digest: strings.Repeat("a", 64), Size: 123456},
		Privileges: []string{"user-home"}, ReversalClass: "manual",
	}
	got, err := RenderReviewText(plan, map[string]domain.Recipe{"codex-cli": recipe})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Plan digest: " + plan.Digest,
		"Tool: codex-cli 0.157.1",
		"Purpose: Run the selected coding agent",
		"Estimated download: 123456 bytes",
		"Privilege: user-home",
		"Cost: not specified in plan",
		"Account owner: not specified in plan",
		"Config change: user-config/settings.json",
		"Before SHA-256: " + strings.Repeat("b", 64),
		"After SHA-256: " + strings.Repeat("c", 64),
		"Reversible: no (manual)",
		"Manual step: Review the provider login in your own account.",
		"Warning: Existing settings are preserved.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("review text missing %q:\n%s", want, got)
		}
	}
}

func TestExactDigestApproval(t *testing.T) {
	plan := reviewFixture(t)
	now := time.Date(2026, 9, 28, 22, 0, 0, 0, time.UTC)
	store := newApprovalFileStore(t)
	var stdout, stderr bytes.Buffer
	approval, err := Review(context.Background(), plan, ReviewOptions{
		NonInteractive: true, ApproveDigest: plan.Digest, Now: func() time.Time { return now },
		Output: &stdout, Diagnostics: &stderr, SaveApproval: store.Save, Recipes: reviewRecipes(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if approval.PlanDigest != plan.Digest || approval.ApprovedAt != now.Format(time.RFC3339Nano) || approval.DisclosureVersion == "" {
		t.Fatalf("approval not bound to exact plan and time: %+v", approval)
	}
	if got := store.path; got == "" {
		t.Fatal("approval was not stored")
	}
	if _, err := os.Stat(store.path); err != nil {
		t.Fatalf("stored approval missing: %v", err)
	}
	if _, err := ParseApprovalArgs([]string{"--approve-digest", plan.Digest}); err != nil {
		t.Fatalf("exact approval flag rejected: %v", err)
	}
	if _, err := ParseApprovalArgs([]string{"--yes"}); err == nil {
		t.Fatal("--yes was accepted as approval")
	}
	if _, err := ParseApprovalArgs([]string{"--profile", "developer"}); err == nil {
		t.Fatal("profile selection was accepted as approval")
	}
	if strings.Contains(stderr.String(), "approved") {
		t.Fatalf("unexpected approval diagnostic on stderr: %s", stderr.String())
	}
}

func TestReviewInteractiveApproval(t *testing.T) {
	plan := reviewFixture(t)
	store := newApprovalFileStore(t)
	var stdout, stderr bytes.Buffer
	approval, err := Review(context.Background(), plan, ReviewOptions{
		Input: strings.NewReader("approve " + plan.Digest + "\n"), Output: &stdout, Diagnostics: &stderr,
		Now: func() time.Time { return time.Date(2026, 9, 28, 22, 0, 0, 0, time.UTC) }, SaveApproval: store.Save, Recipes: reviewRecipes(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if approval.PlanDigest != plan.Digest || store.path == "" {
		t.Fatalf("interactive exact approval was not stored: %+v path=%q", approval, store.path)
	}
	if !strings.Contains(stderr.String(), "approve "+plan.Digest) {
		t.Fatalf("explicit digest prompt missing from stderr: %s", stderr.String())
	}
}

func TestReviewDecline(t *testing.T) {
	plan := reviewFixture(t)
	store := newApprovalFileStore(t)
	var stdout, stderr bytes.Buffer
	_, err := Review(context.Background(), plan, ReviewOptions{
		Input: strings.NewReader("no\n"), Output: &stdout, Diagnostics: &stderr,
		Now:          func() time.Time { return time.Date(2026, 9, 28, 22, 0, 0, 0, time.UTC) },
		SaveApproval: store.Save, Recipes: reviewRecipes(),
	})
	if !errors.Is(err, ErrDeclined) {
		t.Fatalf("decline error = %v, want ErrDeclined", err)
	}
	if store.path != "" {
		t.Fatal("declining created or persisted approval state")
	}
	if strings.Contains(stdout.String(), "approved") {
		t.Fatalf("decline output claims approval: %s", stdout.String())
	}
}

func TestReviewJSON(t *testing.T) {
	plan := reviewFixture(t)
	store := newApprovalFileStore(t)
	var stdout, stderr bytes.Buffer
	_, err := Review(context.Background(), plan, ReviewOptions{
		JSON: true, NonInteractive: true, ApproveDigest: plan.Digest,
		Now:    func() time.Time { return time.Date(2026, 9, 28, 22, 0, 0, 0, time.UTC) },
		Output: &stdout, Diagnostics: &stderr, SaveApproval: store.Save, Recipes: reviewRecipes(),
	})
	if err != nil {
		t.Fatal(err)
	}
	var output map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("stdout is not one JSON value: %v\n%s", err, stdout.String())
	}
	if output["plan_digest"] != plan.Digest || output["status"] != "awaiting_approval" {
		t.Fatalf("unexpected review JSON: %v", output)
	}
	if strings.Contains(stderr.String(), "{") {
		t.Fatalf("JSON leaked to diagnostic stream: %s", stderr.String())
	}
}

func TestReviewRejectsEditedAndExpiredPlans(t *testing.T) {
	t.Run("edited", func(t *testing.T) {
		plan := reviewFixture(t)
		plan.Warnings = append(plan.Warnings, "edited after digest")
		store := newApprovalFileStore(t)
		var stdout bytes.Buffer
		_, err := Review(context.Background(), plan, ReviewOptions{
			NonInteractive: true, ApproveDigest: plan.Digest, Output: &stdout, SaveApproval: store.Save,
			Now: func() time.Time { return time.Date(2026, 9, 28, 22, 0, 0, 0, time.UTC) }, Recipes: reviewRecipes(),
		})
		if !errors.Is(err, ErrPlanChanged) || store.path != "" {
			t.Fatalf("edited plan result err=%v stored=%q", err, store.path)
		}
	})
	t.Run("expired", func(t *testing.T) {
		plan := reviewFixture(t)
		store := newApprovalFileStore(t)
		var stdout, stderr bytes.Buffer
		_, err := Review(context.Background(), plan, ReviewOptions{
			NonInteractive: true, ApproveDigest: plan.Digest, SaveApproval: store.Save,
			Output: &stdout, Diagnostics: &stderr, Now: func() time.Time { return time.Date(2026, 9, 29, 1, 0, 0, 0, time.UTC) }, Recipes: reviewRecipes(),
		})
		if !errors.Is(err, ErrPlanExpired) || store.path != "" {
			t.Fatalf("expired plan result err=%v stored=%q", err, store.path)
		}
		if !strings.Contains(stderr.String(), "expired") {
			t.Fatalf("expiry path was not explained: %s", stderr.String())
		}
	})
	t.Run("catalog-version-changed", func(t *testing.T) {
		plan := reviewFixture(t)
		store := newApprovalFileStore(t)
		var stdout, stderr bytes.Buffer
		recipes := reviewRecipes()
		recipe := recipes["codex-cli"]
		recipe.Version = "0.157.2"
		recipes["codex-cli"] = recipe
		_, err := Review(context.Background(), plan, ReviewOptions{
			NonInteractive: true, ApproveDigest: plan.Digest, Output: &stdout, Diagnostics: &stderr,
			SaveApproval: store.Save, Recipes: recipes,
			Now: func() time.Time { return time.Date(2026, 9, 28, 22, 0, 0, 0, time.UTC) },
		})
		if !errors.Is(err, ErrPlanChanged) || store.path != "" {
			t.Fatalf("changed catalog version result err=%v stored=%q", err, store.path)
		}
	})
}

func reviewRecipes() map[string]domain.Recipe {
	return map[string]domain.Recipe{
		"codex-cli": {
			ID: "codex-cli", Version: "0.157.1", Purpose: "Run the selected coding agent",
			Description: "Install the reviewed command line tool", EstimatedDownloadBytes: 123456,
			Artifact:   &domain.Artifact{Origin: "https://vendor.example/download", Digest: strings.Repeat("a", 64), Size: 123456},
			Privileges: []string{"user-home"}, ReversalClass: "manual",
		},
	}
}

func reviewFixture(t *testing.T) domain.Plan {
	t.Helper()
	plan := domain.Plan{
		SchemaVersion: 1, ID: "plan-fixture", WorkspaceID: "workspace-fixture",
		CatalogDigest: strings.Repeat("1", 64), FactsDigest: strings.Repeat("2", 64), RecommendationDigest: strings.Repeat("3", 64),
		CreatedAt: "2026-09-28T21:00:00Z", ExpiresAt: "2026-09-28T23:00:00Z", Profile: "developer",
		Providers: map[string]string{"codex": "subscription"}, Warnings: []string{"Existing settings are preserved."},
		ManualSteps: []string{"Review the provider login in your own account."},
		Operations: []domain.Operation{{
			ID: "op-codex", Kind: "config_edit", RecipeID: "codex-cli", Version: "0.157.1",
			Dependencies: []string{}, TargetRoot: "user-config", RelativePath: "settings.json",
			PreimageHash: strings.Repeat("b", 64), ExpectedPostimageHash: strings.Repeat("c", 64),
			Reversible: false, Privilege: "user-home", Description: "Select Codex provider settings",
		}},
	}
	digest, err := domain.CanonicalPlanDigest(plan)
	if err != nil {
		t.Fatal(err)
	}
	plan.Digest = digest
	return plan
}

type approvalFileStore struct {
	path  string
	store *state.Store
}

func newApprovalFileStore(t *testing.T) *approvalFileStore {
	t.Helper()
	root := t.TempDir()
	store, err := state.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	return &approvalFileStore{path: "", store: store}
}

func (s *approvalFileStore) Save(approval domain.Approval) error {
	if err := s.store.Save("approval", approval); err != nil {
		return err
	}
	s.path = filepath.Join(s.store.Root(), "approval.json")
	return nil
}
