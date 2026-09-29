// Package doctor reports local setup readiness separately from verified live access.
package doctor

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sanifu-run/anza/internal/domain"
)

// CheckFunc performs one local, read-only readiness probe.
type CheckFunc func(context.Context, string) (domain.CheckResult, error)

// CheckSpec describes a single local readiness probe.
type CheckSpec struct {
	ID       string
	Required bool
	Run      CheckFunc
}

// Options controls local probes and the separately consented optional live check.
type Options struct {
	LocalChecks []CheckSpec
	Live        bool
	LiveTimeout time.Duration
	ConfirmLive func(context.Context) (bool, error)
	LiveCheck   LiveCheckFunc
	Now         func() time.Time
}

var validStatuses = map[string]bool{
	"ready": true, "action_required": true, "manual": true,
	"unsupported": true, "blocked": true, "failed": true, "unknown": true,
}

// Check runs local probes and reports live access as unverified unless the caller
// requests it and a distinct consent callback explicitly approves it.
func Check(ctx context.Context, workspace string, options Options) ([]domain.CheckResult, error) {
	if ctx == nil {
		return nil, errors.New("doctor context is required")
	}
	if workspace == "" {
		return nil, errors.New("doctor workspace is required")
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	checkedAt := func() string { return now().UTC().Format(time.RFC3339Nano) }

	results := make([]domain.CheckResult, 0, len(options.LocalChecks)+1)
	if len(options.LocalChecks) == 0 {
		results = append(results, domain.CheckResult{
			ID: "local_probes", Required: true, Status: "unknown",
			Summary:      "No local readiness probes were configured.",
			NextAction:   "Configure local checks for installed tools, settings, account availability, and platform requirements.",
			EvidenceKind: "local", CheckedAt: checkedAt(),
		})
	}
	for _, spec := range options.LocalChecks {
		if spec.ID == "" || spec.Run == nil {
			return nil, errors.New("local check requires an id and probe")
		}
		result, err := spec.Run(ctx, workspace)
		if err != nil {
			return nil, fmt.Errorf("local check %q: %w", spec.ID, err)
		}
		if err := validateResult(spec.ID, result); err != nil {
			return nil, fmt.Errorf("local check %q: %w", spec.ID, err)
		}
		result.Required = spec.Required
		result.EvidenceKind = "local"
		if result.CheckedAt == "" {
			result.CheckedAt = checkedAt()
		}
		results = append(results, result)
	}

	live, err := runLiveCheck(ctx, workspace, options, checkedAt)
	if err != nil {
		return nil, err
	}
	results = append(results, live)
	return results, nil
}

func validateResult(expectedID string, result domain.CheckResult) error {
	if result.ID != expectedID {
		return fmt.Errorf("probe returned id %q, expected %q", result.ID, expectedID)
	}
	if !validStatuses[result.Status] {
		return fmt.Errorf("probe returned invalid status %q", result.Status)
	}
	if result.Summary == "" {
		return errors.New("probe returned an empty summary")
	}
	return nil
}

// Aggregate returns the most restrictive status among mandatory checks.
// Optional live evidence never becomes an implied requirement.
func Aggregate(results []domain.CheckResult) string {
	if len(results) == 0 {
		return "unknown"
	}
	priority := map[string]int{
		"ready": 0, "manual": 1, "unknown": 2, "action_required": 3,
		"failed": 4, "blocked": 5, "unsupported": 6,
	}
	status, rank, found := "ready", 0, false
	for _, result := range results {
		if !result.Required {
			continue
		}
		found = true
		candidateRank, ok := priority[result.Status]
		if !ok {
			return "unknown"
		}
		if candidateRank > rank {
			status, rank = result.Status, candidateRank
		}
	}
	if !found {
		return "unknown"
	}
	return status
}
