package lifecycle

import (
	"context"
	"errors"
	"io/fs"
	"testing"

	"github.com/sanifu-run/anza/internal/configedit"
	"github.com/sanifu-run/anza/internal/domain"
)

type fixtureFS map[string][]byte

func (f fixtureFS) ReadFile(_ context.Context, p string) ([]byte, error) {
	b, ok := f[p]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return append([]byte(nil), b...), nil
}
func (f fixtureFS) Remove(_ context.Context, p string) error {
	if _, ok := f[p]; !ok {
		return fs.ErrNotExist
	}
	delete(f, p)
	return nil
}
func (f fixtureFS) WriteFile(_ context.Context, p string, b []byte) error {
	f[p] = append([]byte(nil), b...)
	return nil
}

func fixture() (domain.Receipt, domain.Plan, fixtureFS) {
	content := []byte("installed")
	path := "fixture/profile.json"
	return domain.Receipt{OwnedPaths: []string{path}, Operations: []domain.ReceiptOperation{{ID: "op1", State: "succeeded"}}}, domain.Plan{Operations: []domain.Operation{{ID: "op1", TargetRoot: "fixture", RelativePath: "profile.json", ExpectedPostimageHash: digest(content), Reversible: true}}}, fixtureFS{path: content, "fixture/unowned": []byte("keep"), "fixture/native-login": []byte("keep")}
}

func TestRemoveOwnership(t *testing.T) {
	r, p, f := fixture()
	preview, err := PreviewRemove(context.Background(), r, p, f)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Items) != 1 || preview.Items[0].Path != "fixture/profile.json" {
		t.Fatalf("preview included unowned paths: %#v", preview)
	}
	if err := ApplyRemove(context.Background(), preview, f, true); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"fixture/unowned", "fixture/native-login"} {
		if string(f[path]) != "keep" {
			t.Fatalf("removed unowned/credential fixture %s", path)
		}
	}
}

func TestRemoveModifiedFiles(t *testing.T) {
	r, p, f := fixture()
	f["fixture/profile.json"] = []byte("participant edit")
	preview, err := PreviewRemove(context.Background(), r, p, f)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Items) != 1 || preview.Items[0].Remove || preview.Items[0].Conflict == "" {
		t.Fatalf("modified file not retained as conflict: %#v", preview)
	}
	if err := ApplyRemove(context.Background(), preview, f, true); !errors.Is(err, ErrDrift) {
		t.Fatalf("expected drift error, got %v", err)
	}
	if string(f["fixture/profile.json"]) != "participant edit" {
		t.Fatal("modified file was removed")
	}
}

func TestRemoveIdempotent(t *testing.T) {
	r, p, f := fixture()
	ctx := context.Background()
	preview, err := PreviewRemove(ctx, r, p, f)
	if err != nil {
		t.Fatal(err)
	}
	if err = ApplyRemove(ctx, preview, f, true); err != nil {
		t.Fatal(err)
	}
	if err = ApplyRemove(ctx, preview, f, true); err != nil {
		t.Fatalf("second removal: %v", err)
	}
}

func TestRemoveInversePatchPreservesOtherKeys(t *testing.T) {
	before := []byte(`{"model":"old","participant":"original"}`)
	edit, err := configedit.Plan(before, configedit.Patch{Format: configedit.JSON, Set: map[string]any{"model": "anza"}, Expected: map[string]any{"model": "old"}})
	if err != nil {
		t.Fatal(err)
	}
	path := "fixture/settings.json"
	receipt := domain.Receipt{OwnedPaths: []string{path}, Operations: []domain.ReceiptOperation{{ID: "settings", State: "succeeded"}}}
	plan := domain.Plan{Operations: []domain.Operation{{ID: "settings", TargetRoot: "fixture", RelativePath: "settings.json", ExpectedPostimageHash: edit.PostimageHash, Reversible: true}}}
	// An unrelated key changed after install. The inverse must preserve it.
	current := []byte(`{"model":"anza","participant":"edited"}`)
	f := fixtureFS{path: current}
	preview, err := PreviewRemoveWithEdits(context.Background(), receipt, plan, f, map[string]configedit.Edit{"settings": edit})
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Items) != 1 || !preview.Items[0].Restore {
		t.Fatalf("inverse was not planned: %#v", preview)
	}
	if err := ApplyRemove(context.Background(), preview, f, true); err != nil {
		t.Fatal(err)
	}
	if got := string(f[path]); got != `{"model":"old","participant":"edited"}` {
		t.Fatalf("inverse changed unrelated content: %s", got)
	}
}

type repairFixture struct {
	current []byte
	applied int
	reads   int
	output  []byte
	fail    error
}

func (r *repairFixture) Read(context.Context, domain.Operation) ([]byte, error) {
	r.reads++
	return append([]byte(nil), r.current...), nil
}
func (r *repairFixture) Apply(_ context.Context, _ domain.Operation) error {
	r.applied++
	if r.fail != nil {
		return r.fail
	}
	r.current = append([]byte(nil), r.output...)
	return nil
}

func TestRepairNewApproval(t *testing.T) {
	before, after := []byte("before"), []byte("after")
	op := domain.Operation{ID: "repair", RelativePath: "config.json", PreimageHash: digest(before), ExpectedPostimageHash: digest(after)}
	plan := domain.Plan{Operations: []domain.Operation{op}}
	setPlanDigest(t, &plan)
	actions := &repairFixture{current: before, output: after}
	if _, err := Repair(context.Background(), plan, domain.Approval{PlanDigest: "old-digest", ApprovedAt: "now", DisclosureVersion: "v1"}, actions); !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("old approval accepted: %v", err)
	}
	if actions.applied != 0 {
		t.Fatal("action ran with stale approval")
	}
	result, err := Repair(context.Background(), plan, domain.Approval{PlanDigest: plan.Digest, ApprovedAt: "now", DisclosureVersion: "v1"}, actions)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Succeeded) != 1 || result.Succeeded[0] != "repair" || len(result.Failed) != 0 {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestRepairPreservesDriftAndFailedAction(t *testing.T) {
	before, after := []byte("before"), []byte("after")
	plan := domain.Plan{Operations: []domain.Operation{{ID: "repair", RelativePath: "config", PreimageHash: digest(before), ExpectedPostimageHash: digest(after)}}}
	setPlanDigest(t, &plan)
	approval := domain.Approval{PlanDigest: plan.Digest, ApprovedAt: "now", DisclosureVersion: "v1"}
	drift := &repairFixture{current: []byte("changed"), output: after}
	if _, err := Repair(context.Background(), plan, approval, drift); !errors.Is(err, ErrDrift) {
		t.Fatalf("expected drift refusal: %v", err)
	}
	if drift.applied != 0 || string(drift.current) != "changed" {
		t.Fatal("repair overwrote drift")
	}
	failure := errors.New("synthetic apply failure")
	failed := &repairFixture{current: before, fail: failure}
	result, err := Repair(context.Background(), plan, approval, failed)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Succeeded) != 0 || result.Failed["repair"] == "" {
		t.Fatalf("failed action claimed success: %#v", result)
	}
}

func TestRepairRejectsPlanMutationAfterApproval(t *testing.T) {
	before, after := []byte("before"), []byte("after")
	plan := domain.Plan{Operations: []domain.Operation{{ID: "repair", RelativePath: "config", PreimageHash: digest(before), ExpectedPostimageHash: digest(after)}}}
	setPlanDigest(t, &plan)
	approval := domain.Approval{PlanDigest: plan.Digest, ApprovedAt: "now", DisclosureVersion: "v1"}
	plan.Operations[0].Description = "changed after approval"
	actions := &repairFixture{current: before, output: after}
	if _, err := Repair(context.Background(), plan, approval, actions); !errors.Is(err, ErrPlanDigestMismatch) {
		t.Fatalf("mutated plan was not rejected as a digest mismatch: %v", err)
	}
	if actions.reads != 0 || actions.applied != 0 {
		t.Fatalf("tampered plan caused effects: reads=%d applies=%d", actions.reads, actions.applied)
	}
}

func setPlanDigest(t *testing.T, plan *domain.Plan) {
	t.Helper()
	digest, err := domain.CanonicalPlanDigest(*plan)
	if err != nil {
		t.Fatal(err)
	}
	plan.Digest = digest
}
