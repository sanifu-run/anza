// Package exercise resolves catalog-authored exercise guidance for a project.
// Exercise descriptors are declarative and never provide executable commands.
package exercise

import (
	"context"
	"errors"
	"fmt"

	"github.com/sanifu-run/anza/internal/domain"
)

type Status string

const (
	StatusUnmet     Status = "unmet"
	StatusCancelled Status = "cancelled"
)

// Result is a sanitized, serializable account of the selected exercise outcome.
// The exercise schema has no commands, so manual and unsupported scenarios are
// recorded with their unmet prerequisites and guidance without running scripts.
type Result struct {
	ExerciseID           string   `json:"exercise_id"`
	ExerciseVersion      string   `json:"exercise_version"`
	ScenarioID           string   `json:"scenario_id,omitempty"`
	ProjectKind          string   `json:"project_kind"`
	Platform             string   `json:"platform"`
	Status               Status   `json:"status"`
	Summary              string   `json:"summary,omitempty"`
	ManualSteps          []string `json:"manual_steps"`
	MissingCapabilityIDs []string `json:"missing_capability_ids"`
	ReadinessConstraints []string `json:"readiness_constraints"`
	Verification         string   `json:"verification,omitempty"`
	Diagnostic           string   `json:"diagnostic,omitempty"`
}

// Run selects the unique scenario for projectKind and platform and records its
// outcome. It does not read or modify a project directory or execute commands.
func Run(ctx context.Context, descriptor domain.Exercise, projectKind, platform string) (Result, error) {
	result := Result{
		ExerciseID:           descriptor.ID,
		ExerciseVersion:      descriptor.Version,
		ProjectKind:          projectKind,
		Platform:             platform,
		Status:               StatusUnmet,
		ManualSteps:          []string{},
		MissingCapabilityIDs: []string{},
		ReadinessConstraints: []string{},
	}
	if ctx == nil {
		result.Diagnostic = "exercise context is nil"
		return result, errors.New(result.Diagnostic)
	}
	if descriptor.ID == "" || projectKind == "" || platform == "" {
		result.Diagnostic = "exercise ID, project kind, and platform are required"
		return result, errors.New(result.Diagnostic)
	}

	scenario, ok := SelectScenario(descriptor, projectKind, platform)
	if !ok {
		result.Diagnostic = fmt.Sprintf("no catalog scenario matches project kind %q on %q", projectKind, platform)
		if err := ctx.Err(); err != nil {
			result.Status = StatusCancelled
			return result, err
		}
		return result, nil
	}
	result.ScenarioID = scenario.ID
	result.Summary = scenario.Summary
	result.ManualSteps = append([]string(nil), scenario.ManualSteps...)
	result.MissingCapabilityIDs = append([]string(nil), scenario.MissingCapabilityIDs...)
	result.ReadinessConstraints = append([]string(nil), scenario.ReadinessConstraints...)
	result.Verification = scenario.Verification
	result.Diagnostic = scenario.Summary

	if err := ctx.Err(); err != nil {
		result.Status = StatusCancelled
		result.Diagnostic = "exercise cancelled after scenario selection: " + err.Error()
		return result, err
	}
	return result, nil
}

// SelectScenario returns the exact project-kind/platform match, if present.
func SelectScenario(descriptor domain.Exercise, projectKind, platform string) (domain.ExerciseScenario, bool) {
	for _, scenario := range descriptor.Scenarios {
		if scenario.ProjectKind != projectKind {
			continue
		}
		for _, supportedPlatform := range scenario.SupportedPlatforms {
			if supportedPlatform == platform {
				return scenario, true
			}
		}
	}
	return domain.ExerciseScenario{}, false
}
