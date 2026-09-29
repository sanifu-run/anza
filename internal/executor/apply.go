package executor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/sanifu-run/anza/internal/configedit"
	"github.com/sanifu-run/anza/internal/domain"
	"github.com/sanifu-run/anza/internal/state"
)

const receiptPrefix = "exec-"
const backupPrefix = "backup-"

type Options struct {
	Store    *state.Store
	Effects  Effects
	Payloads map[string]Payload
	Now      func() time.Time
}
type backupRecord struct {
	PlanDigest  string           `json:"plan_digest"`
	OperationID string           `json:"operation_id"`
	Exists      bool             `json:"exists"`
	Mode        uint32           `json:"mode"`
	Bytes       []byte           `json:"bytes"`
	ConfigEdit  *configedit.Edit `json:"config_edit,omitempty"`
}

// Apply validates the exact plan approval, preflights every operation, then
// durably journals each typed effect before invoking its injected boundary.
func Apply(ctx context.Context, plan domain.Plan, approval domain.Approval, options Options) (result domain.Receipt, returnErr error) {
	if ctx == nil || ctx.Err() != nil {
		return domain.Receipt{}, errors.New("apply context is nil or canceled")
	}
	if err := validateApproval(plan, approval, options.Now); err != nil {
		return domain.Receipt{}, err
	}
	if options.Store == nil || options.Effects == nil {
		return domain.Receipt{}, errors.New("executor state store and effects are required")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	locks, err := acquireEffectLocks(options.Store, plan)
	if err != nil {
		return domain.Receipt{}, err
	}
	defer func() {
		if err := locks.Release(); err != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("release executor locks: %w", err))
		}
	}()
	rname := receiptName(plan.Digest)
	var receipt domain.Receipt
	if err := options.Store.Load(rname, &receipt); err == nil {
		if receipt.PlanDigest != plan.Digest {
			return receipt, ErrStaleApproval
		}
		options = hydrateConfigPayloads(options, plan)
		if err := validateOperations(plan, options.Payloads); err != nil {
			return receipt, err
		}
		return resume(ctx, plan, receipt, options)
	} else if !errors.Is(err, os.ErrNotExist) {
		return domain.Receipt{}, fmt.Errorf("load executor receipt: %w", err)
	}
	if err := validateOperations(plan, options.Payloads); err != nil {
		return domain.Receipt{}, err
	}
	preimages, preDigest, err := inspectPlan(ctx, plan, options.Effects)
	if err != nil {
		return domain.Receipt{}, err
	}
	now := options.Now().UTC().Format(time.RFC3339Nano)
	receipt = domain.Receipt{SchemaVersion: 1, PlanDigest: plan.Digest, Operations: make([]domain.ReceiptOperation, 0, len(plan.Operations)), OwnedPaths: []string{}, OwnedKeys: []string{}, PreDigest: preDigest, PostDigest: preDigest, BackupReferences: []string{}, StartedAt: now, FinishedAt: now, PendingManualActions: []string{}}
	for _, op := range plan.Operations {
		receipt.Operations = append(receipt.Operations, domain.ReceiptOperation{ID: op.ID, State: "planned"})
	}
	if err := saveReceipt(options.Store, rname, receipt); err != nil {
		return domain.Receipt{}, err
	}
	for _, op := range plan.Operations {
		if err := ctx.Err(); err != nil {
			return receipt, err
		}
		if err := checkDependencies(plan, receipt, op); err != nil {
			return rollback(ctx, plan, receipt, options, err)
		}
		current, err := options.Effects.Read(ctx, op)
		if err != nil {
			return receipt, fmt.Errorf("recheck preimage for %s: %w", op.ID, err)
		}
		if err := matchPreimage(op, current); err != nil {
			return rollback(ctx, plan, receipt, options, err)
		}
		if current.Exists && digest(current.Bytes) == op.ExpectedPostimageHash {
			setOperation(&receipt, op.ID, "succeeded", now)
			if err := saveReceipt(options.Store, rname, receipt); err != nil {
				return receipt, err
			}
			continue
		}
		if err := saveBackup(options.Store, plan, op, preimages[op.ID], options.Payloads[op.ID], &receipt); err != nil {
			return receipt, err
		}
		setOperation(&receipt, op.ID, "running", options.Now().UTC().Format(time.RFC3339Nano))
		if err := saveReceipt(options.Store, rname, receipt); err != nil {
			return receipt, err
		}
		if err := applyEffect(ctx, options.Effects, op, op.PreimageHash, options.Payloads[op.ID]); err != nil {
			if errors.Is(err, ErrInterrupted) {
				return receipt, ErrInterrupted
			}
			setOperation(&receipt, op.ID, "failed", options.Now().UTC().Format(time.RFC3339Nano))
			if saveErr := saveReceipt(options.Store, rname, receipt); saveErr != nil {
				err = errors.Join(err, saveErr)
			}
			return rollback(ctx, plan, receipt, options, err)
		}
		setOperation(&receipt, op.ID, "succeeded", options.Now().UTC().Format(time.RFC3339Nano))
		recordOwnership(&receipt, op, options.Payloads[op.ID])
		if err := saveReceipt(options.Store, rname, receipt); err != nil {
			return receipt, err
		}
	}
	_, postDigest, err := inspectPostimages(ctx, plan, options.Effects)
	if err != nil {
		return receipt, err
	}
	receipt.PostDigest = postDigest
	receipt.FinishedAt = options.Now().UTC().Format(time.RFC3339Nano)
	if err := saveReceipt(options.Store, rname, receipt); err != nil {
		return receipt, err
	}
	return receipt, nil
}

func validateApproval(plan domain.Plan, approval domain.Approval, clock func() time.Time) error {
	data, err := json.Marshal(plan)
	if err != nil {
		return fmt.Errorf("encode plan: %w", err)
	}
	valid, err := domain.DecodePlan(data)
	if err != nil {
		return fmt.Errorf("validate plan: %w", err)
	}
	canonical, err := domain.CanonicalPlanDigest(valid)
	if err != nil {
		return err
	}
	if canonical != plan.Digest || approval.PlanDigest != canonical {
		return ErrStaleApproval
	}
	encoded, err := json.Marshal(approval)
	if err != nil {
		return err
	}
	if _, err := domain.DecodeApproval(encoded); err != nil {
		return fmt.Errorf("validate approval: %w", err)
	}
	if clock == nil {
		clock = time.Now
	}
	now := clock().UTC()
	created, _ := time.Parse(time.RFC3339, plan.CreatedAt)
	expires, _ := time.Parse(time.RFC3339, plan.ExpiresAt)
	approved, _ := time.Parse(time.RFC3339, approval.ApprovedAt)
	if approval.DisclosureVersion != "plan-review-v1" || !now.Before(expires) || approved.After(now) || approved.Before(created) || !approved.Before(expires) {
		return ErrStaleApproval
	}
	return nil
}
func validateOperations(plan domain.Plan, payloads map[string]Payload) error {
	seen := map[string]int{}
	for i, op := range plan.Operations {
		if _, ok := seen[op.ID]; ok {
			return fmt.Errorf("duplicate operation ID %q", op.ID)
		}
		seen[op.ID] = i
		if !safeRelative(op.RelativePath) {
			return fmt.Errorf("%w: unsafe target path", ErrUnsupportedOperation)
		}
		if op.Kind == "write_config" && op.TargetRoot != "launch-profile" && op.TargetRoot != "project-agents" && op.TargetRoot != "project-claude" {
			return fmt.Errorf("%w: config root %q", ErrUnsupportedOperation, op.TargetRoot)
		}
		if op.Kind == "install_artifact" && op.TargetRoot != "tool-cache" {
			return fmt.Errorf("%w: artifact root %q", ErrUnsupportedOperation, op.TargetRoot)
		}
		if op.Kind != "write_config" && op.Kind != "install_artifact" {
			return fmt.Errorf("%w: %q", ErrUnsupportedOperation, op.Kind)
		}
		p, ok := payloads[op.ID]
		if !ok {
			return fmt.Errorf("operation %s has no typed payload", op.ID)
		}
		if err := validatePayload(p, op); err != nil {
			return fmt.Errorf("operation %s: %w", op.ID, err)
		}
		for _, dep := range op.Dependencies {
			n, ok := seen[dep]
			if !ok || n >= i {
				return fmt.Errorf("operation %s has absent or forward dependency %s", op.ID, dep)
			}
		}
	}
	if len(payloads) != len(plan.Operations) {
		return errors.New("payload set contains an operation not present in approved plan")
	}
	return nil
}
func inspectPlan(ctx context.Context, plan domain.Plan, effects Effects) (map[string]Snapshot, string, error) {
	m := make(map[string]Snapshot, len(plan.Operations))
	parts := make([]string, 0, len(plan.Operations))
	for _, op := range plan.Operations {
		s, err := effects.Read(ctx, op)
		if err != nil {
			return nil, "", fmt.Errorf("inspect %s: %w", op.ID, err)
		}
		if err := matchPreimage(op, s); err != nil {
			return nil, "", err
		}
		m[op.ID] = s
		parts = append(parts, op.ID+"="+snapshotHash(s))
	}
	sort.Strings(parts)
	return m, digest([]byte(strings.Join(parts, "\n"))), nil
}
func inspectPostimages(ctx context.Context, plan domain.Plan, effects Effects) (map[string]Snapshot, string, error) {
	m := map[string]Snapshot{}
	parts := make([]string, 0, len(plan.Operations))
	for _, op := range plan.Operations {
		s, err := effects.Read(ctx, op)
		if err != nil {
			return nil, "", err
		}
		if !s.Exists || s.Symlink || !s.Regular || digest(s.Bytes) != op.ExpectedPostimageHash {
			return nil, "", fmt.Errorf("%w: postimage for %s does not match approved output", ErrPreimageConflict, op.ID)
		}
		m[op.ID] = s
		parts = append(parts, op.ID+"="+snapshotHash(s))
	}
	sort.Strings(parts)
	return m, digest([]byte(strings.Join(parts, "\n"))), nil
}
func matchPreimage(op domain.Operation, s Snapshot) error {
	if s.Symlink || s.Exists && !s.Regular {
		return fmt.Errorf("%w: %s is symlink or non-regular", ErrPreimageConflict, op.ID)
	}
	if op.PreimageHash == "" {
		if s.Exists {
			return fmt.Errorf("%w: target %s appeared after planning", ErrPreimageConflict, op.ID)
		}
		return nil
	}
	if !s.Exists || digest(s.Bytes) != op.PreimageHash {
		return fmt.Errorf("%w: target %s differs from approved preimage", ErrPreimageConflict, op.ID)
	}
	return nil
}
func saveBackup(store *state.Store, plan domain.Plan, op domain.Operation, s Snapshot, payload Payload, r *domain.Receipt) error {
	if op.Kind != "write_config" {
		return nil
	}
	edit, ok := payload.(ConfigPayload)
	if !ok {
		return fmt.Errorf("operation %s has no config payload for backup", op.ID)
	}
	name := backupName(plan.Digest, op.ID)
	rec := backupRecord{PlanDigest: plan.Digest, OperationID: op.ID, Exists: s.Exists, Mode: uint32(s.Mode.Perm()), Bytes: append([]byte(nil), s.Bytes...), ConfigEdit: &edit.Edit}
	if err := store.Save(name, rec); err != nil {
		return fmt.Errorf("persist preimage backup for %s: %w", op.ID, err)
	}
	r.BackupReferences = appendUnique(r.BackupReferences, name)
	return nil
}
func hydrateConfigPayloads(options Options, plan domain.Plan) Options {
	if options.Payloads == nil {
		options.Payloads = make(map[string]Payload, len(plan.Operations))
	}
	for _, op := range plan.Operations {
		if op.Kind != "write_config" {
			continue
		}
		if _, ok := options.Payloads[op.ID].(ConfigPayload); ok {
			continue
		}
		var backup backupRecord
		if err := options.Store.Load(backupName(plan.Digest, op.ID), &backup); err != nil {
			continue
		}
		if backup.PlanDigest == plan.Digest && backup.OperationID == op.ID && backup.ConfigEdit != nil {
			options.Payloads[op.ID] = ConfigPayload{Edit: *backup.ConfigEdit}
		}
	}
	return options
}

func saveReceipt(store *state.Store, name string, r domain.Receipt) error {
	if err := store.Save(name, r); err != nil {
		return fmt.Errorf("persist executor receipt: %w", err)
	}
	return nil
}
func receiptName(d string) string { return receiptPrefix + d[:59] }
func backupName(d, id string) string {
	sum := sha256.Sum256([]byte(id))
	return backupPrefix + d[:16] + "-" + hex.EncodeToString(sum[:8])
}
func appendUnique(xs []string, v string) []string {
	for _, x := range xs {
		if x == v {
			return xs
		}
	}
	return append(xs, v)
}
func snapshotHash(s Snapshot) string {
	if !s.Exists {
		return "missing"
	}
	return digest(s.Bytes)
}
func digest(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
func safeRelative(p string) bool {
	if p == "" || len(p) > 1024 || strings.Contains(p, "\\") || strings.HasPrefix(p, "/") {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return !(len(p) > 1 && p[1] == ':')
}
func acquireEffectLocks(store *state.Store, plan domain.Plan) (*state.LockSet, error) {
	tools, workspace := false, false
	for _, op := range plan.Operations {
		if op.TargetRoot == "tool-cache" {
			tools = true
		} else {
			workspace = true
		}
	}
	scopes := []state.LockScope{}
	if tools {
		scopes = append(scopes, state.LockUserTools)
	}
	if workspace {
		scopes = append(scopes, state.LockWorkspace)
	}
	if len(scopes) == 0 {
		scopes = append(scopes, state.LockWorkspace)
	}
	return store.AcquireLocks(scopes...)
}
func checkDependencies(_ domain.Plan, r domain.Receipt, op domain.Operation) error {
	states := map[string]string{}
	for _, v := range r.Operations {
		states[v.ID] = v.State
	}
	for _, dep := range op.Dependencies {
		if states[dep] != "succeeded" {
			return fmt.Errorf("operation %s dependency %s is incomplete", op.ID, dep)
		}
	}
	return nil
}
func setOperation(r *domain.Receipt, id, stateName, at string) {
	for i := range r.Operations {
		if r.Operations[i].ID == id {
			r.Operations[i].State = stateName
			if stateName == "running" {
				r.Operations[i].StartedAt = at
			} else if stateName == "succeeded" || stateName == "failed" || stateName == "unknown" {
				r.Operations[i].FinishedAt = at
			}
			return
		}
	}
}
