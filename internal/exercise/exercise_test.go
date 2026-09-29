package exercise

import (
	"context"
	"errors"
	"testing"

	"github.com/sanifu-run/anza/internal/catalog"
	"github.com/sanifu-run/anza/internal/domain"
)

func testExercise() domain.Exercise {
	return domain.Exercise{
		SchemaVersion: 1,
		ID:            "fixture-exercise",
		Version:       "1.0.0",
		Description:   "Fixture exercise",
		Scenarios: []domain.ExerciseScenario{
			{
				ID:                   "scratch-linux",
				ProjectKind:          "go",
				SupportedPlatforms:   []string{"linux-amd64"},
				Status:               "manual",
				Summary:              "A Go toolchain is required.",
				ManualSteps:          []string{"Install Go after reviewing the project requirements."},
				MissingCapabilityIDs: []string{"go.toolchain"},
				ReadinessConstraints: []string{"The Go toolchain has not been qualified."},
				Verification:         "Review the selected Go version against the project requirements.",
			},
			{
				ID:                   "scratch-windows",
				ProjectKind:          "go",
				SupportedPlatforms:   []string{"windows-amd64"},
				Status:               "unsupported",
				Summary:              "This platform is unsupported.",
				ManualSteps:          []string{"Use a supported host."},
				MissingCapabilityIDs: []string{"go.supported-host"},
				ReadinessConstraints: []string{"No supported host was observed."},
				Verification:         "Confirm a supported host before continuing.",
			},
		},
	}
}

func TestScratchExercise(t *testing.T) {
	result, err := Run(context.Background(), testExercise(), "go", "linux-amd64")
	if err != nil {
		t.Fatalf("Run(scratch fixture): %v", err)
	}
	if result.Status != StatusUnmet || result.ScenarioID != "scratch-linux" {
		t.Fatalf("unexpected exercise result: %+v", result)
	}
	if len(result.MissingCapabilityIDs) != 1 || result.MissingCapabilityIDs[0] != "go.toolchain" {
		t.Fatalf("missing prerequisite was not recorded: %+v", result.MissingCapabilityIDs)
	}
}

func TestBundledExerciseRecordsUnmetPrerequisites(t *testing.T) {
	loaded, err := catalog.LoadBundled()
	if err != nil {
		t.Fatalf("LoadBundled(): %v", err)
	}
	descriptor, ok := loaded.Exercise("mobile-desktop-exercise")
	if !ok {
		t.Fatal("reviewed mobile and desktop exercise missing from bundled catalog")
	}
	result, err := Run(context.Background(), descriptor, "ios", "darwin-arm64")
	if err != nil {
		t.Fatalf("Run(bundled iOS scenario): %v", err)
	}
	if result.Status != StatusUnmet || result.ScenarioID != "ios-macos-manual" {
		t.Fatalf("unexpected bundled exercise result: %+v", result)
	}
	if len(result.MissingCapabilityIDs) == 0 || result.Verification == "" {
		t.Fatalf("bundled unmet prerequisites were not recorded: %+v", result)
	}
}

func TestExistingProjectReadOnly(t *testing.T) {
	before := testExercise()
	result, err := Run(context.Background(), before, "go", "linux-amd64")
	if err != nil {
		t.Fatalf("Run(existing project decision): %v", err)
	}
	if result.Status != StatusUnmet || result.Summary == "" || result.Verification == "" {
		t.Fatalf("expected useful read-only outcome: %+v", result)
	}
	if got := testExercise(); got.Scenarios[0].Summary != before.Scenarios[0].Summary {
		t.Fatal("exercise input was mutated")
	}
}

func TestExerciseFailureAndCancel(t *testing.T) {
	result, err := Run(context.Background(), testExercise(), "rust", "linux-amd64")
	if err != nil {
		t.Fatalf("Run(unknown project kind): %v", err)
	}
	if result.Status != StatusUnmet || result.ScenarioID != "" || result.Diagnostic == "" {
		t.Fatalf("unknown exercise did not preserve a diagnostic: %+v", result)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err = Run(ctx, testExercise(), "go", "linux-amd64")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run(canceled): got %v, want context.Canceled", err)
	}
	if result.ScenarioID != "scratch-linux" || result.Diagnostic == "" {
		t.Fatalf("cancellation lost the selected scenario diagnostics: %+v", result)
	}
}
