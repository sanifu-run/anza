package executor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sanifu-run/anza/internal/configedit"
	"github.com/sanifu-run/anza/internal/domain"
	"github.com/sanifu-run/anza/internal/state"
)

func TestApplyExactApproval(t *testing.T) {
	plan, edit := executorFixture(t, "write_config", []byte("{\n}\n"), []byte("{\n  \"mode\": \"safe\"\n}\n"))
	fs := newMemoryEffects("launch-profile", "settings.json", []byte("{\n}\n"))
	store, err := state.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	opts := Options{Store: store, Effects: fs, Payloads: map[string]Payload{"write-config": ConfigPayload{Edit: edit}}, Now: fixedNow}
	approval := domain.Approval{PlanDigest: strings.Repeat("0", 64), ApprovedAt: fixedNow().Add(-time.Minute).Format(time.RFC3339Nano), DisclosureVersion: "plan-review-v1"}
	if _, err := Apply(context.Background(), plan, approval, opts); !errors.Is(err, ErrStaleApproval) {
		t.Fatalf("stale digest error = %v", err)
	}
	if fs.writes != 0 {
		t.Fatalf("stale approval wrote %d times", fs.writes)
	}
	approval = approved(plan)
	approval.DisclosureVersion = "old-disclosure"
	if _, err := Apply(context.Background(), plan, approval, opts); !errors.Is(err, ErrStaleApproval) {
		t.Fatalf("old disclosure error = %v", err)
	}
	if fs.writes != 0 {
		t.Fatalf("old disclosure wrote %d times", fs.writes)
	}
	approval.PlanDigest = plan.Digest
	approval.DisclosureVersion = "plan-review-v1"
	receipt, err := Apply(context.Background(), plan, approval, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(receipt.Operations) != 1 || receipt.Operations[0].State != "succeeded" || fs.writes != 1 {
		t.Fatalf("receipt=%+v writes=%d", receipt, fs.writes)
	}
	if string(fs.files["launch-profile/settings.json"]) != string(edit.Bytes) {
		t.Fatal("approved config bytes were not applied")
	}
}

func TestCrashAtEveryBoundary(t *testing.T) {
	for _, boundary := range []string{"before-effect", "after-effect"} {
		t.Run(boundary, func(t *testing.T) {
			plan, edit := executorFixture(t, "write_config", []byte("{}"), []byte("{\"mode\":\"safe\"}"))
			fs := newMemoryEffects("launch-profile", "settings.json", []byte("{}"))
			fs.crashAt = boundary
			store, err := state.NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			opts := Options{Store: store, Effects: fs, Payloads: map[string]Payload{"write-config": ConfigPayload{Edit: edit}}, Now: fixedNow}
			if _, err := Apply(context.Background(), plan, approved(plan), opts); !errors.Is(err, ErrInterrupted) {
				t.Fatalf("apply err=%v", err)
			}
			fs.crashAt = ""
			recoveryOptions := opts
			recoveryOptions.Payloads = nil // exact config edit is recovered from its private durable backup
			receipt, err := Recover(context.Background(), plan, approved(plan), recoveryOptions)
			if err != nil {
				t.Fatal(err)
			}
			if receipt.Operations[0].State != "succeeded" {
				t.Fatalf("recovered state=%s", receipt.Operations[0].State)
			}
			wantWrites := 1
			if fs.writes != wantWrites {
				t.Fatalf("writes=%d want %d", fs.writes, wantWrites)
			}
		})
	}
}

func TestTOCTOUConflict(t *testing.T) {
	before := []byte("{}")
	plan, edit := executorFixture(t, "write_config", before, []byte("{\"mode\":\"safe\"}"))
	fs := newMemoryEffects("launch-profile", "settings.json", []byte("{\"user\":true}"))
	store, err := state.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	opts := Options{Store: store, Effects: fs, Payloads: map[string]Payload{"write-config": ConfigPayload{Edit: edit}}, Now: fixedNow}
	if _, err := Apply(context.Background(), plan, approved(plan), opts); !errors.Is(err, ErrPreimageConflict) {
		t.Fatalf("conflict error=%v", err)
	}
	if fs.writes != 0 || string(fs.files["launch-profile/settings.json"]) != "{\"user\":true}" {
		t.Fatal("conflicting user file was modified")
	}
}

func TestUnownedTargetAppearsAfterPlanning(t *testing.T) {
	for _, tc := range []struct {
		name    string
		file    []byte
		symlink bool
	}{
		{name: "unowned file", file: []byte("participant data")},
		{name: "symlink", symlink: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan, edit := executorFixture(t, "write_config", nil, nil)
			fs := newMemoryEffects("launch-profile", "settings.json", tc.file)
			if tc.symlink {
				fs.symlinks["launch-profile/settings.json"] = true
			}
			store, err := state.NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			opts := Options{Store: store, Effects: fs, Payloads: map[string]Payload{"write-config": ConfigPayload{Edit: edit}}, Now: fixedNow}
			if _, err := Apply(context.Background(), plan, approved(plan), opts); !errors.Is(err, ErrPreimageConflict) {
				t.Fatalf("appeared target error=%v", err)
			}
			if fs.writes != 0 {
				t.Fatalf("executor wrote %d times", fs.writes)
			}
			if tc.symlink && !fs.symlinks["launch-profile/settings.json"] {
				t.Fatal("executor changed appeared symlink")
			}
			if !tc.symlink && string(fs.files["launch-profile/settings.json"]) != string(tc.file) {
				t.Fatal("executor changed appeared unowned file")
			}
		})
	}
}

func TestRollbackOnlyUnchangedOwnedConfig(t *testing.T) {
	for _, tc := range []struct {
		name            string
		participantEdit bool
		wantRollback    string
	}{
		{name: "unchanged owned edit rolls back", wantRollback: "rolled_back"},
		{name: "participant edit is preserved", participantEdit: true, wantRollback: "conflict"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			beforeA := []byte("{\n  \"keep\": 1\n}\n")
			beforeB := []byte("{\n  \"keep\": 2\n}\n")
			editA, err := configedit.Plan(beforeA, configedit.Patch{Format: configedit.JSON, Set: map[string]any{"mode": "safe"}})
			if err != nil {
				t.Fatal(err)
			}
			editB, err := configedit.Plan(beforeB, configedit.Patch{Format: configedit.JSON, Set: map[string]any{"mode": "safe"}})
			if err != nil {
				t.Fatal(err)
			}
			plan, _ := executorFixture(t, "write_config", beforeA, editA.Bytes)
			plan.Operations[0].ID = "config-a"
			plan.Operations[0].RelativePath = "a.json"
			plan.Operations = append(plan.Operations, domain.Operation{ID: "config-b", Kind: "write_config", Dependencies: []string{"config-a"}, TargetRoot: "launch-profile", RelativePath: "b.json", PreimageHash: hash(beforeB), ExpectedPostimageHash: editB.PostimageHash, Reversible: true, Privilege: "none", Description: "second synthetic config effect"})
			plan.Digest, err = domain.CanonicalPlanDigest(plan)
			if err != nil {
				t.Fatal(err)
			}
			fs := newMemoryEffects("launch-profile", "a.json", beforeA)
			fs.files["launch-profile/b.json"] = beforeB
			fs.mutateAfterApply = func(op domain.Operation) {
				if op.ID != "config-a" {
					return
				}
				if tc.participantEdit {
					fs.files["launch-profile/a.json"] = []byte("{\n  \"mode\": \"participant\",\n  \"keep\": 1\n}\n")
				}
				fs.files["launch-profile/b.json"] = []byte("{\n  \"keep\": 99\n}\n")
			}
			store, err := state.NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			opts := Options{Store: store, Effects: fs, Payloads: map[string]Payload{"config-a": ConfigPayload{Edit: editA}, "config-b": ConfigPayload{Edit: editB}}, Now: fixedNow}
			receipt, err := Apply(context.Background(), plan, approved(plan), opts)
			if !errors.Is(err, ErrPreimageConflict) {
				t.Fatalf("apply error=%v", err)
			}
			if receipt.Operations[0].RollbackState != tc.wantRollback {
				t.Fatalf("rollback state=%q want %q", receipt.Operations[0].RollbackState, tc.wantRollback)
			}
			got := fs.files["launch-profile/a.json"]
			if tc.participantEdit {
				if !strings.Contains(string(got), "participant") {
					t.Fatalf("participant edit was overwritten: %s", got)
				}
			} else if string(got) != string(beforeA) {
				t.Fatalf("owned config was not rolled back: %s", got)
			}
		})
	}
}

func TestInstallReconcile(t *testing.T) {
	artifact := []byte("verified synthetic archive")
	plan, _ := executorFixture(t, "install_artifact", nil, artifact)
	fs := newMemoryEffects("tool-cache", "artifacts/example/1.0", nil)
	store, err := state.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	opts := Options{Store: store, Effects: fs, Payloads: map[string]Payload{"write-config": ArtifactPayload{Bytes: artifact}}, Now: fixedNow}
	fs.crashAt = "after-effect"
	if _, err := Apply(context.Background(), plan, approved(plan), opts); !errors.Is(err, ErrInterrupted) {
		t.Fatalf("apply err=%v", err)
	}
	fs.crashAt = ""
	receipt, err := Recover(context.Background(), plan, approved(plan), opts)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Operations[0].State != "succeeded" || fs.writes != 1 {
		t.Fatalf("state=%s writes=%d", receipt.Operations[0].State, fs.writes)
	}
	if len(receipt.OwnedPaths) != 1 || receipt.OwnedPaths[0] != "tool-cache/artifacts/example/1.0" {
		t.Fatalf("artifact ownership missing: %#v", receipt.OwnedPaths)
	}
}

func TestUnknownOperationIsRejected(t *testing.T) {
	plan, _ := executorFixture(t, "package_install", nil, []byte("payload"))
	fs := newMemoryEffects("tool-cache", "artifacts/example/1.0", nil)
	store, err := state.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = Apply(context.Background(), plan, approved(plan), Options{Store: store, Effects: fs, Payloads: map[string]Payload{"write-config": ArtifactPayload{Bytes: []byte("payload")}}, Now: fixedNow})
	if !errors.Is(err, ErrUnsupportedOperation) || fs.writes != 0 {
		t.Fatalf("err=%v writes=%d", err, fs.writes)
	}
}

type memoryEffects struct {
	files            map[string][]byte
	symlinks         map[string]bool
	writes           int
	crashAt          string
	mutateAfterApply func(domain.Operation)
	root, path       string
}

func newMemoryEffects(root, path string, initial []byte) *memoryEffects {
	fs := &memoryEffects{files: map[string][]byte{}, symlinks: map[string]bool{}, root: root, path: path}
	if initial != nil {
		fs.files[root+"/"+path] = append([]byte(nil), initial...)
	}
	return fs
}
func (m *memoryEffects) Read(_ context.Context, op domain.Operation) (Snapshot, error) {
	key := op.TargetRoot + "/" + op.RelativePath
	b, file := m.files[key]
	symlink := m.symlinks[key]
	return Snapshot{Exists: file || symlink, Symlink: symlink, Regular: file && !symlink, Mode: 0600, Bytes: append([]byte(nil), b...)}, nil
}
func (m *memoryEffects) ApplyConfig(ctx context.Context, op domain.Operation, expected string, edit configedit.Edit) error {
	if m.crashAt == "before-effect" {
		m.crashAt = ""
		return ErrInterrupted
	}
	if err := m.compare(op, expected); err != nil {
		return err
	}
	m.files[op.TargetRoot+"/"+op.RelativePath] = append([]byte(nil), edit.Bytes...)
	m.writes++
	if m.mutateAfterApply != nil {
		m.mutateAfterApply(op)
	}
	if m.crashAt == "after-effect" {
		m.crashAt = ""
		return ErrInterrupted
	}
	return nil
}
func (m *memoryEffects) PublishArtifact(ctx context.Context, op domain.Operation, expected string, artifact ArtifactPayload) error {
	if m.crashAt == "before-effect" {
		m.crashAt = ""
		return ErrInterrupted
	}
	if err := m.compare(op, expected); err != nil {
		return err
	}
	m.files[op.TargetRoot+"/"+op.RelativePath] = append([]byte(nil), artifact.Bytes...)
	m.writes++
	if m.mutateAfterApply != nil {
		m.mutateAfterApply(op)
	}
	if m.crashAt == "after-effect" {
		m.crashAt = ""
		return ErrInterrupted
	}
	return nil
}
func (m *memoryEffects) compare(op domain.Operation, expected string) error {
	key := op.TargetRoot + "/" + op.RelativePath
	old, ok := m.files[key]
	if expected == "" {
		if ok || m.symlinks[key] {
			return ErrPreimageConflict
		}
	} else if !ok || hash(old) != expected {
		return ErrPreimageConflict
	}
	return nil
}

func executorFixture(t *testing.T, kind string, before, after []byte) (domain.Plan, configedit.Edit) {
	t.Helper()
	edit, err := configedit.Plan(before, configedit.Patch{Format: configedit.JSON, Set: map[string]any{"mode": "safe"}})
	if err != nil {
		t.Fatal(err)
	}
	pre := ""
	if before != nil {
		pre = hash(before)
	}
	post := after
	if kind == "write_config" {
		post = edit.Bytes
	}
	plan := domain.Plan{SchemaVersion: 1, ID: "plan-fixture", WorkspaceID: "workspace-fixture", CatalogDigest: strings.Repeat("1", 64), FactsDigest: strings.Repeat("2", 64), RecommendationDigest: strings.Repeat("3", 64), CreatedAt: "2026-09-28T21:00:00Z", ExpiresAt: "2026-09-29T23:00:00Z", Profile: "developer", Providers: map[string]string{}, Operations: []domain.Operation{{ID: "write-config", Kind: kind, Dependencies: []string{}, TargetRoot: "launch-profile", RelativePath: "settings.json", PreimageHash: pre, ExpectedPostimageHash: hash(post), Reversible: true, Privilege: "none", Description: "synthetic config effect"}}, Warnings: []string{}, ManualSteps: []string{}}
	if kind == "install_artifact" {
		plan.Operations[0].TargetRoot = "tool-cache"
		plan.Operations[0].RelativePath = "artifacts/example/1.0"
		plan.Operations[0].Reversible = false
	}
	plan.Digest, err = domain.CanonicalPlanDigest(plan)
	if err != nil {
		t.Fatal(err)
	}
	return plan, edit
}
func approved(plan domain.Plan) domain.Approval {
	return domain.Approval{PlanDigest: plan.Digest, ApprovedAt: fixedNow().Add(-time.Minute).Format(time.RFC3339Nano), DisclosureVersion: "plan-review-v1"}
}
func fixedNow() time.Time  { return time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC) }
func hash(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
