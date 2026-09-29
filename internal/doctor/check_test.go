package doctor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sanifu-run/anza/internal/domain"
)

func TestReadinessEvidenceLevels(t *testing.T) {
	checks := []CheckSpec{{ID: "codex", Required: true, Run: func(context.Context, string) (domain.CheckResult, error) {
		return domain.CheckResult{ID: "codex", Required: true, Status: "action_required", Summary: "Codex is installed but not authenticated", EvidenceKind: "local"}, nil
	}}}
	results, err := Check(context.Background(), "workspace-a", Options{LocalChecks: checks, Now: func() time.Time { return time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	if got := findResult(t, results, "codex"); got.Status != "action_required" || got.EvidenceKind != "local" {
		t.Fatalf("installed unauthenticated result = %+v", got)
	}
	live := findResult(t, results, "live_agent")
	if live.Status != "unknown" || live.EvidenceKind != "live" {
		t.Fatalf("skipped live result = %+v", live)
	}
	if got := Aggregate(results); got != "action_required" {
		t.Fatalf("aggregate = %q, want action_required", got)
	}
}

func TestLiveCheckConsent(t *testing.T) {
	confirmCalls, liveCalls := 0, 0
	results, err := Check(context.Background(), "workspace-a", Options{
		Live:        true,
		ConfirmLive: func(context.Context) (bool, error) { confirmCalls++; return false, nil },
		LiveCheck: func(context.Context, string) (domain.CheckResult, error) {
			liveCalls++
			return domain.CheckResult{ID: "live_agent", Status: "ready", Summary: "synthetic live success"}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if confirmCalls != 1 || liveCalls != 0 {
		t.Fatalf("confirmation calls=%d live calls=%d", confirmCalls, liveCalls)
	}
	live := findResult(t, results, "live_agent")
	if live.Status != "unknown" || live.EvidenceKind != "live" {
		t.Fatalf("denied live result = %+v", live)
	}
	if _, err := Check(context.Background(), "workspace-a", Options{Live: true, LiveCheck: func(context.Context, string) (domain.CheckResult, error) {
		liveCalls++
		return domain.CheckResult{}, nil
	}}); err == nil {
		t.Fatal("live check without a consent callback was accepted")
	}
	if liveCalls != 0 {
		t.Fatalf("missing-consent path made %d live calls", liveCalls)
	}
	if _, err := Check(context.Background(), "workspace-a", Options{Live: true, ConfirmLive: func(context.Context) (bool, error) { return false, errors.New("prompt failed") }}); err == nil {
		t.Fatal("consent prompt error was ignored")
	}
	if liveCalls != 0 {
		t.Fatalf("prompt-error path made %d live calls", liveCalls)
	}
}

func TestMandatoryFailureAggregation(t *testing.T) {
	results := []domain.CheckResult{{ID: "agent", Required: true, Status: "ready"}, {ID: "platform", Required: true, Status: "unsupported"}, {ID: "live_agent", Required: false, Status: "unknown"}}
	if got := Aggregate(results); got != "unsupported" {
		t.Fatalf("aggregate = %q, want unsupported", got)
	}
	results[1].Required = false
	if got := Aggregate(results); got != "ready" {
		t.Fatalf("optional failure aggregate = %q, want ready", got)
	}
}

func findResult(t *testing.T, results []domain.CheckResult, id string) domain.CheckResult {
	t.Helper()
	for _, result := range results {
		if result.ID == id {
			return result
		}
	}
	t.Fatalf("missing result %q in %#v", id, results)
	return domain.CheckResult{}
}
