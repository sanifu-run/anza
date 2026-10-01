package executor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sanifu-run/anza/internal/configedit"
	"github.com/sanifu-run/anza/internal/domain"
)

// Recover classifies journaled running operations by their exact postcondition.
// A proven postimage is never applied a second time; an unchanged preimage is
// resumed only while the same approval remains current. Ambiguous targets are
// preserved and surfaced for manual reconciliation.
func Recover(ctx context.Context, plan domain.Plan, approval domain.Approval, options Options) (result domain.Receipt, returnErr error) {
	if ctx == nil || ctx.Err() != nil {
		return domain.Receipt{}, errors.New("recovery context is nil or canceled")
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
	name := receiptName(plan.Digest)
	var receipt domain.Receipt
	if err := options.Store.Load(name, &receipt); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return domain.Receipt{}, errors.New("no receipt exists for this plan")
		}
		return domain.Receipt{}, fmt.Errorf("load receipt for recovery: %w", err)
	}
	if receipt.PlanDigest != plan.Digest {
		return receipt, ErrStaleApproval
	}
	options = hydrateConfigPayloads(options, plan)
	if err := validateOperations(plan, options.Payloads); err != nil {
		return receipt, err
	}
	return reconcileReceipt(ctx, plan, receipt, approval, options)
}

func reconcileReceipt(ctx context.Context, plan domain.Plan, receipt domain.Receipt, approval domain.Approval, options Options) (domain.Receipt, error) {
	name := receiptName(plan.Digest)
	changed := false
	for _, op := range plan.Operations {
		idx := operationIndex(receipt, op.ID)
		if idx < 0 {
			return receipt, fmt.Errorf("receipt omits operation %s", op.ID)
		}
		stateName := receipt.Operations[idx].State
		if stateName != "running" && stateName != "unknown" {
			continue
		}
		snap, err := options.Effects.Read(ctx, op)
		if err != nil {
			return receipt, fmt.Errorf("reconcile %s: %w", op.ID, err)
		}
		if snap.Exists && !snap.Symlink && snap.Regular && digest(snap.Bytes) == op.ExpectedPostimageHash {
			setOperation(&receipt, op.ID, "succeeded", options.Now().UTC().Format(time.RFC3339Nano))
			recordOwnership(&receipt, op, options.Payloads[op.ID])
			changed = true
			continue
		}
		if err := matchPreimage(op, snap); err == nil {
			setOperation(&receipt, op.ID, "planned", "")
			receipt.Operations[idx].StartedAt = ""
			receipt.Operations[idx].FinishedAt = ""
			changed = true
			continue
		}
		setOperation(&receipt, op.ID, "unknown", options.Now().UTC().Format(time.RFC3339Nano))
		receipt.PendingManualActions = appendUnique(receipt.PendingManualActions, "Reconcile operation "+op.ID+" manually; the target changed after Anza began it.")
		changed = true
	}
	if changed {
		if err := saveReceipt(options.Store, name, receipt); err != nil {
			return receipt, err
		}
	}
	for _, item := range receipt.Operations {
		if item.State == "unknown" {
			return receipt, ErrRecoveryManual
		}
	}
	return runPending(ctx, plan, receipt, approval, options)
}

func resume(ctx context.Context, plan domain.Plan, receipt domain.Receipt, approval domain.Approval, options Options) (domain.Receipt, error) {
	if options.Now == nil {
		options.Now = time.Now
	}
	return reconcileReceipt(ctx, plan, receipt, approval, options)
}

func runPending(ctx context.Context, plan domain.Plan, receipt domain.Receipt, approval domain.Approval, options Options) (domain.Receipt, error) {
	name := receiptName(plan.Digest)
	for _, op := range plan.Operations {
		idx := operationIndex(receipt, op.ID)
		if idx < 0 {
			return receipt, fmt.Errorf("receipt omits operation %s", op.ID)
		}
		if receipt.Operations[idx].State == "succeeded" || receipt.Operations[idx].State == "skipped" {
			continue
		}
		if receipt.Operations[idx].State == "unknown" {
			return receipt, ErrRecoveryManual
		}
		if err := ctx.Err(); err != nil {
			return receipt, err
		}
		if err := checkDependencies(plan, receipt, op); err != nil {
			return rollback(ctx, plan, receipt, options, err)
		}
		snap, err := options.Effects.Read(ctx, op)
		if err != nil {
			return receipt, fmt.Errorf("recheck %s before resume: %w", op.ID, err)
		}
		if snap.Exists && !snap.Symlink && snap.Regular && digest(snap.Bytes) == op.ExpectedPostimageHash {
			setOperation(&receipt, op.ID, "succeeded", options.Now().UTC().Format(time.RFC3339Nano))
			if err := saveReceipt(options.Store, name, receipt); err != nil {
				return receipt, err
			}
			continue
		}
		if err := matchPreimage(op, snap); err != nil {
			return rollback(ctx, plan, receipt, options, err)
		}
		if err := saveBackup(options.Store, plan, op, snap, options.Payloads[op.ID], &receipt); err != nil {
			return receipt, err
		}
		setOperation(&receipt, op.ID, "running", options.Now().UTC().Format(time.RFC3339Nano))
		if err := saveReceipt(options.Store, name, receipt); err != nil {
			return receipt, err
		}
		if err := validateApproval(plan, approval, options.Now); err != nil {
			setOperation(&receipt, op.ID, "planned", "")
			receipt.Operations[idx].StartedAt = ""
			receipt.Operations[idx].FinishedAt = ""
			if saveErr := saveReceipt(options.Store, name, receipt); saveErr != nil {
				return receipt, errors.Join(err, saveErr)
			}
			return receipt, err
		}
		if err := applyEffect(ctx, options.Effects, op, op.PreimageHash, options.Payloads[op.ID]); err != nil {
			if errors.Is(err, ErrInterrupted) {
				return receipt, ErrInterrupted
			}
			setOperation(&receipt, op.ID, "failed", options.Now().UTC().Format(time.RFC3339Nano))
			if saveErr := saveReceipt(options.Store, name, receipt); saveErr != nil {
				err = errors.Join(err, saveErr)
			}
			return rollback(ctx, plan, receipt, options, err)
		}
		setOperation(&receipt, op.ID, "succeeded", options.Now().UTC().Format(time.RFC3339Nano))
		recordOwnership(&receipt, op, options.Payloads[op.ID])
		if err := saveReceipt(options.Store, name, receipt); err != nil {
			return receipt, err
		}
	}
	_, postDigest, err := inspectPostimages(ctx, plan, options.Effects)
	if err != nil {
		return receipt, err
	}
	receipt.PostDigest = postDigest
	receipt.FinishedAt = options.Now().UTC().Format(time.RFC3339Nano)
	if err := saveReceipt(options.Store, name, receipt); err != nil {
		return receipt, err
	}
	return receipt, nil
}

func rollback(ctx context.Context, plan domain.Plan, receipt domain.Receipt, options Options, cause error) (domain.Receipt, error) {
	name := receiptName(plan.Digest)
	for i := len(plan.Operations) - 1; i >= 0; i-- {
		op := plan.Operations[i]
		idx := operationIndex(receipt, op.ID)
		if idx < 0 || receipt.Operations[idx].State != "succeeded" || !op.Reversible || op.Kind != "write_config" {
			continue
		}
		payload, ok := options.Payloads[op.ID].(ConfigPayload)
		if !ok {
			receipt.Operations[idx].RollbackState = "conflict"
			continue
		}
		snap, err := options.Effects.Read(ctx, op)
		if err != nil || !snap.Exists || snap.Symlink || !snap.Regular || digest(snap.Bytes) != op.ExpectedPostimageHash {
			receipt.Operations[idx].RollbackState = "conflict"
			continue
		}
		inverse, err := configedit.Inverse(snap.Bytes, payload.Edit)
		if err != nil || len(inverse.SkippedKeys) != 0 {
			receipt.Operations[idx].RollbackState = "conflict"
			continue
		}
		receipt.Operations[idx].RollbackState = "rolling_back"
		if err := saveReceipt(options.Store, name, receipt); err != nil {
			return receipt, errors.Join(cause, err)
		}
		if err := options.Effects.ApplyConfig(ctx, op, op.ExpectedPostimageHash, inverse); err != nil {
			receipt.Operations[idx].RollbackState = "conflict"
			continue
		}
		receipt.Operations[idx].RollbackState = "rolled_back"
		ownedPath := op.TargetRoot + "/" + op.RelativePath
		receipt.OwnedPaths = removeString(receipt.OwnedPaths, ownedPath)
		receipt.OwnedKeys = removePrefix(receipt.OwnedKeys, ownedPath+"#")
		if err := saveReceipt(options.Store, name, receipt); err != nil {
			return receipt, errors.Join(cause, err)
		}
	}
	if err := saveReceipt(options.Store, name, receipt); err != nil {
		return receipt, errors.Join(cause, fmt.Errorf("persist rollback receipt: %w", err))
	}
	return receipt, cause
}

func recordOwnership(r *domain.Receipt, op domain.Operation, payload Payload) {
	if op.Kind != "write_config" && op.Kind != "install_artifact" {
		return
	}
	r.OwnedPaths = appendUnique(r.OwnedPaths, op.TargetRoot+"/"+op.RelativePath)
	if op.Kind == "write_config" {
		edit := payload.(ConfigPayload).Edit
		for _, key := range edit.OwnedKeys {
			r.OwnedKeys = appendUnique(r.OwnedKeys, op.TargetRoot+"/"+op.RelativePath+"#"+key.Path)
		}
	}
}
func removeString(items []string, value string) []string {
	out := items[:0]
	for _, item := range items {
		if item != value {
			out = append(out, item)
		}
	}
	return out
}
func removePrefix(items []string, prefix string) []string {
	out := items[:0]
	for _, item := range items {
		if !strings.HasPrefix(item, prefix) {
			out = append(out, item)
		}
	}
	return out
}

func operationIndex(r domain.Receipt, id string) int {
	for i, op := range r.Operations {
		if op.ID == id {
			return i
		}
	}
	return -1
}
