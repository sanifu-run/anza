package lifecycle

import (
	"context"
	"errors"
	"fmt"

	"github.com/sanifu-run/anza/internal/domain"
)

var ErrApprovalRequired = errors.New("fresh approval for this repair plan required")
var ErrPlanDigestMismatch = errors.New("repair plan digest does not match its contents")

// RepairActions contains the effects needed for a newly reviewed repair plan.
// Implementations should use the same locked, preimage-checked effect path as
// initial installation.
type RepairActions interface {
	Read(context.Context, domain.Operation) ([]byte, error)
	Apply(context.Context, domain.Operation) error
}

type RepairResult struct {
	Succeeded []string
	Failed    map[string]string
}

// Repair applies only the supplied current plan after validating its fresh,
// digest-bound approval and every operation preimage. The entire plan is
// inspected before the first effect so drift cannot be overwritten.
func Repair(ctx context.Context, plan domain.Plan, approval domain.Approval, actions RepairActions) (RepairResult, error) {
	result := RepairResult{Succeeded: []string{}, Failed: map[string]string{}}
	currentDigest, err := domain.CanonicalPlanDigest(plan)
	if err != nil {
		return result, fmt.Errorf("digesting repair plan: %w", err)
	}
	if plan.Digest == "" || plan.Digest != currentDigest {
		return result, ErrPlanDigestMismatch
	}
	if approval.PlanDigest != currentDigest || approval.ApprovedAt == "" || approval.DisclosureVersion == "" {
		return result, ErrApprovalRequired
	}
	for _, op := range plan.Operations {
		if !safePath(op.RelativePath) {
			return result, fmt.Errorf("unsafe operation path %q", op.RelativePath)
		}
		current, err := actions.Read(ctx, op)
		if err != nil {
			return result, fmt.Errorf("inspect %s: %w", op.ID, err)
		}
		if err := matchPreimage(op, current); err != nil {
			return result, fmt.Errorf("%s: %w", op.ID, err)
		}
	}
	for _, op := range plan.Operations {
		if err := actions.Apply(ctx, op); err != nil {
			result.Failed[op.ID] = err.Error()
			continue
		}
		post, err := actions.Read(ctx, op)
		if err != nil {
			result.Failed[op.ID] = fmt.Sprintf("verify result: %v", err)
			continue
		}
		if digest(post) != op.ExpectedPostimageHash {
			result.Failed[op.ID] = "result does not match approved postimage"
			continue
		}
		result.Succeeded = append(result.Succeeded, op.ID)
	}
	return result, nil
}

func matchPreimage(op domain.Operation, current []byte) error {
	if op.PreimageHash == "" {
		if current != nil {
			return fmt.Errorf("%w: target appeared after planning", ErrDrift)
		}
		return nil
	}
	if current == nil || digest(current) != op.PreimageHash {
		return fmt.Errorf("%w: target differs from reviewed preimage", ErrDrift)
	}
	return nil
}
