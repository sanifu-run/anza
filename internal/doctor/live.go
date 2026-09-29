package doctor

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sanifu-run/anza/internal/domain"
)

// LiveCheckFunc may perform a small provider/agent round trip. It must be
// supplied by the caller; doctor never discovers credentials or starts an agent.
type LiveCheckFunc func(context.Context, string) (domain.CheckResult, error)

func runLiveCheck(ctx context.Context, workspace string, options Options, checkedAt func() string) (domain.CheckResult, error) {
	unverified := domain.CheckResult{
		ID: "live_agent", Required: false, Status: "unknown",
		Summary:      "No live provider or agent response has been verified.",
		NextAction:   "A live check can be run only after separate explicit consent.",
		EvidenceKind: "live", CheckedAt: checkedAt(),
	}
	if !options.Live {
		return unverified, nil
	}
	if options.ConfirmLive == nil {
		return domain.CheckResult{}, errors.New("live check requires an explicit consent callback")
	}
	approved, err := options.ConfirmLive(ctx)
	if err != nil {
		return domain.CheckResult{}, fmt.Errorf("requesting live-check consent: %w", err)
	}
	if !approved {
		unverified.NextAction = "Live check skipped because consent was not granted."
		return unverified, nil
	}
	if options.LiveCheck == nil {
		return domain.CheckResult{}, errors.New("live check consent was granted but no live check callback is configured")
	}
	result, err := options.LiveCheck(ctx, workspace)
	if err != nil {
		return domain.CheckResult{}, fmt.Errorf("live provider/agent check: %w", err)
	}
	if err := validateResult("live_agent", result); err != nil {
		return domain.CheckResult{}, fmt.Errorf("live provider/agent check: %w", err)
	}
	result.Required = false
	result.EvidenceKind = "live"
	if result.CheckedAt == "" {
		result.CheckedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	return result, nil
}
