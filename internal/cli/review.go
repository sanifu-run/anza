// Package cli contains local, participant-facing command flows.
package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/sanifu-run/anza/internal/domain"
)

const disclosureVersion = "plan-review-v1"

var (
	ErrDeclined         = errors.New("plan approval declined")
	ErrPlanChanged      = errors.New("plan changed since it was created")
	ErrPlanExpired      = errors.New("plan expired")
	ErrApprovalRequired = errors.New("exact plan digest approval is required")
)

// ReviewOptions supplies all I/O and time-dependent behavior to Review. The
// callback is invoked only after explicit approval of the current digest.
type ReviewOptions struct {
	Input          io.Reader
	Output         io.Writer
	Diagnostics    io.Writer
	Recipes        map[string]domain.Recipe
	JSON           bool
	NonInteractive bool
	ApproveDigest  string
	Now            func() time.Time
	SaveApproval   func(domain.Approval) error
}

type reviewOperation struct {
	ID                    string   `json:"id"`
	Kind                  string   `json:"kind"`
	ToolID                string   `json:"tool_id,omitempty"`
	ToolVersion           string   `json:"tool_version,omitempty"`
	Purpose               string   `json:"purpose,omitempty"`
	Description           string   `json:"description"`
	DownloadBytes         int64    `json:"download_bytes"`
	DownloadOrigin        string   `json:"download_origin,omitempty"`
	Privileges            []string `json:"privileges"`
	Cost                  string   `json:"cost"`
	AccountOwner          string   `json:"account_owner"`
	Target                string   `json:"target"`
	PreimageSHA256        string   `json:"preimage_sha256,omitempty"`
	ExpectedPostimageHash string   `json:"expected_postimage_sha256"`
	Reversible            bool     `json:"reversible"`
	ReversalLimit         string   `json:"reversal_limit"`
	Dependencies          []string `json:"dependencies"`
}

type reviewDocument struct {
	SchemaVersion int               `json:"schema_version"`
	Status        string            `json:"status"`
	PlanID        string            `json:"plan_id"`
	WorkspaceID   string            `json:"workspace_id"`
	Profile       string            `json:"profile"`
	PlanDigest    string            `json:"plan_digest"`
	ExpiresAt     string            `json:"expires_at"`
	Providers     map[string]string `json:"providers"`
	Operations    []reviewOperation `json:"operations"`
	Warnings      []string          `json:"warnings"`
	ManualSteps   []string          `json:"manual_steps"`
}

// Review validates and displays one exact plan, then accepts either a typed
// digest confirmation or, in noninteractive mode, the matching digest flag.
// It never applies operations; SaveApproval is the only side effect.
func Review(ctx context.Context, plan domain.Plan, options ReviewOptions) (domain.Approval, error) {
	if ctx == nil {
		return domain.Approval{}, errors.New("review context is nil")
	}
	if err := ctx.Err(); err != nil {
		return domain.Approval{}, fmt.Errorf("review canceled: %w", err)
	}
	if err := validateReviewPlan(plan); err != nil {
		if diagnosticErr := writeDiagnostic(options.Diagnostics, "Plan cannot be approved: %v. Create a fresh plan and review it again.\n", err); diagnosticErr != nil {
			return domain.Approval{}, errors.Join(err, fmt.Errorf("write review diagnostic: %w", diagnosticErr))
		}
		return domain.Approval{}, err
	}
	if err := validateReviewRecipes(plan, options.Recipes); err != nil {
		if diagnosticErr := writeDiagnostic(options.Diagnostics, "Plan catalog details are unavailable or changed; create a fresh plan and review it again.\n"); diagnosticErr != nil {
			return domain.Approval{}, errors.Join(err, fmt.Errorf("write catalog diagnostic: %w", diagnosticErr))
		}
		return domain.Approval{}, err
	}
	now := time.Now
	if options.Now != nil {
		now = options.Now
	}
	current := now().UTC()
	expires, err := time.Parse(time.RFC3339, plan.ExpiresAt)
	if err != nil {
		return domain.Approval{}, fmt.Errorf("parse plan expiry: %w", err)
	}
	status := "awaiting_approval"
	if !current.Before(expires) {
		status = "expired"
	}
	doc := makeReviewDocument(plan, options.Recipes, status)
	if err := writeReview(options.Output, options.JSON, doc, plan, options.Recipes); err != nil {
		return domain.Approval{}, err
	}
	if status == "expired" {
		if err := writeDiagnostic(options.Diagnostics, "Plan expired at %s; create a new plan before approving.\n", plan.ExpiresAt); err != nil {
			return domain.Approval{}, fmt.Errorf("write expiry diagnostic: %w", err)
		}
		return domain.Approval{}, ErrPlanExpired
	}

	if options.NonInteractive {
		if options.ApproveDigest == "" {
			if err := writeDiagnostic(options.Diagnostics, "Noninteractive review requires --approve-digest with this exact plan digest.\n"); err != nil {
				return domain.Approval{}, fmt.Errorf("write approval diagnostic: %w", err)
			}
			return domain.Approval{}, ErrApprovalRequired
		}
		if options.ApproveDigest != plan.Digest {
			if err := writeDiagnostic(options.Diagnostics, "Approval digest does not match the current plan; review the current plan again.\n"); err != nil {
				return domain.Approval{}, fmt.Errorf("write approval diagnostic: %w", err)
			}
			return domain.Approval{}, ErrPlanChanged
		}
	} else {
		if options.ApproveDigest != "" && options.ApproveDigest != plan.Digest {
			if err := writeDiagnostic(options.Diagnostics, "Approval digest does not match the current plan; review the current plan again.\n"); err != nil {
				return domain.Approval{}, fmt.Errorf("write approval diagnostic: %w", err)
			}
			return domain.Approval{}, ErrPlanChanged
		}
		if err := promptForApproval(ctx, options.Input, options.Diagnostics, plan.Digest); err != nil {
			return domain.Approval{}, err
		}
	}
	if err := ctx.Err(); err != nil {
		return domain.Approval{}, fmt.Errorf("review canceled before saving approval: %w", err)
	}
	current = now().UTC()
	if !current.Before(expires) {
		if err := writeDiagnostic(options.Diagnostics, "Plan expired before approval could be saved; create a new plan.\n"); err != nil {
			return domain.Approval{}, fmt.Errorf("write expiry diagnostic: %w", err)
		}
		return domain.Approval{}, ErrPlanExpired
	}
	if options.SaveApproval == nil {
		return domain.Approval{}, errors.New("approval persistence is not configured")
	}
	approval := domain.Approval{
		PlanDigest:        plan.Digest,
		ApprovedAt:        current.Format(time.RFC3339Nano),
		DisclosureVersion: disclosureVersion,
	}
	encoded, err := json.Marshal(approval)
	if err != nil {
		return domain.Approval{}, fmt.Errorf("encode plan approval: %w", err)
	}
	if _, err := domain.DecodeApproval(encoded); err != nil {
		return domain.Approval{}, fmt.Errorf("validate plan approval: %w", err)
	}
	if err := options.SaveApproval(approval); err != nil {
		return domain.Approval{}, fmt.Errorf("store plan approval: %w", err)
	}
	return approval, nil
}

// ParseApprovalArgs recognizes only the explicit digest approval flag. In
// particular, --yes and profile names are never translated into approval.
func ParseApprovalArgs(args []string) (string, error) {
	if len(args) == 0 {
		return "", nil
	}
	if len(args) != 2 || args[0] != "--approve-digest" || !isLowerDigest(args[1]) {
		return "", errors.New("only --approve-digest HEX is accepted as noninteractive approval")
	}
	return args[1], nil
}

// RenderReviewText returns a human-readable disclosure for a plan and its
// catalog recipes. It makes absent cost/account-owner data explicit.
func RenderReviewText(plan domain.Plan, recipes map[string]domain.Recipe) (string, error) {
	if err := validateReviewRecipes(plan, recipes); err != nil {
		return "", err
	}
	doc := makeReviewDocument(plan, recipes, "awaiting_approval")
	var out strings.Builder
	fmt.Fprintf(&out, "Plan: %s\nWorkspace: %s\nProfile: %s\nPlan digest: %s\nExpires: %s\n", displayText(doc.PlanID), displayText(doc.WorkspaceID), displayText(doc.Profile), doc.PlanDigest, doc.ExpiresAt)
	fmt.Fprintf(&out, "Providers/account choices: %s\n", formatProviders(doc.Providers))
	fmt.Fprintf(&out, "Cost: not specified in plan\nAccount owner: not specified in plan\n\nEffects (%d):\n", len(doc.Operations))
	for _, op := range doc.Operations {
		fmt.Fprintf(&out, "- Operation %s (%s): %s\n", displayText(op.ID), displayText(op.Kind), displayText(op.Description))
		if op.ToolID != "" {
			fmt.Fprintf(&out, "  Tool: %s %s\n  Purpose: %s\n", displayText(op.ToolID), displayText(op.ToolVersion), displayText(op.Purpose))
		}
		fmt.Fprintf(&out, "  Estimated download: %d bytes", op.DownloadBytes)
		if op.DownloadOrigin != "" {
			fmt.Fprintf(&out, " from %s", displayText(op.DownloadOrigin))
		}
		fmt.Fprintln(&out)
		fmt.Fprintf(&out, "  Privilege: %s\n", formatStrings(op.Privileges))
		if strings.Contains(strings.ToLower(op.Kind), "config") {
			fmt.Fprintf(&out, "  Config change: %s\n", displayText(op.Target))
		} else {
			fmt.Fprintf(&out, "  Target: %s\n", displayText(op.Target))
		}
		if op.PreimageSHA256 != "" {
			fmt.Fprintf(&out, "  Before SHA-256: %s\n", op.PreimageSHA256)
		}
		reversible := "no"
		if op.Reversible {
			reversible = "yes"
		}
		fmt.Fprintf(&out, "  After SHA-256: %s\n  Reversible: %s", op.ExpectedPostimageHash, reversible)
		if op.ReversalLimit != "" {
			fmt.Fprintf(&out, " (%s)", displayText(op.ReversalLimit))
		}
		fmt.Fprintln(&out)
		if len(op.Dependencies) != 0 {
			fmt.Fprintf(&out, "  Depends on: %s\n", formatStrings(op.Dependencies))
		}
	}
	fmt.Fprintln(&out, "\nManual steps:")
	if len(doc.ManualSteps) == 0 {
		fmt.Fprintln(&out, "- None listed")
	} else {
		for _, step := range doc.ManualSteps {
			fmt.Fprintf(&out, "- Manual step: %s\n", displayText(step))
		}
	}
	if len(doc.Warnings) != 0 {
		fmt.Fprintln(&out, "Warnings:")
		for _, warning := range doc.Warnings {
			fmt.Fprintf(&out, "- Warning: %s\n", displayText(warning))
		}
	}
	fmt.Fprintf(&out, "\nNo operation will run during review. Approve only this digest: %s\n", plan.Digest)
	return out.String(), nil
}

func validateReviewPlan(plan domain.Plan) error {
	encoded, err := json.Marshal(plan)
	if err != nil {
		return fmt.Errorf("encode plan: %w", err)
	}
	validated, err := domain.DecodePlan(encoded)
	if err != nil {
		return fmt.Errorf("validate plan: %w", err)
	}
	digest, err := domain.CanonicalPlanDigest(validated)
	if err != nil {
		return fmt.Errorf("compute canonical plan digest: %w", err)
	}
	if digest != plan.Digest {
		return ErrPlanChanged
	}
	return nil
}

func validateReviewRecipes(plan domain.Plan, recipes map[string]domain.Recipe) error {
	for _, operation := range plan.Operations {
		if operation.RecipeID == "" {
			continue
		}
		recipe, ok := recipes[operation.RecipeID]
		if !ok || recipe.ID != operation.RecipeID || strings.TrimSpace(recipe.Purpose) == "" {
			return fmt.Errorf("recipe review details are missing for %q", operation.RecipeID)
		}
		if operation.Version != "" && recipe.Version != operation.Version {
			return ErrPlanChanged
		}
	}
	return nil
}

func makeReviewDocument(plan domain.Plan, recipes map[string]domain.Recipe, status string) reviewDocument {
	doc := reviewDocument{
		SchemaVersion: 1, Status: status, PlanID: plan.ID, WorkspaceID: plan.WorkspaceID,
		Profile: plan.Profile, PlanDigest: plan.Digest, ExpiresAt: plan.ExpiresAt,
		Providers: cloneStringsMap(plan.Providers), Warnings: append([]string{}, plan.Warnings...),
		ManualSteps: append([]string{}, plan.ManualSteps...), Operations: make([]reviewOperation, 0, len(plan.Operations)),
	}
	for _, op := range plan.Operations {
		item := reviewOperation{
			ID: op.ID, Kind: op.Kind, Description: op.Description, Cost: "not specified in plan",
			AccountOwner: "not specified in plan", Target: op.TargetRoot + "/" + op.RelativePath,
			PreimageSHA256: op.PreimageHash, ExpectedPostimageHash: op.ExpectedPostimageHash,
			Reversible: op.Reversible, Dependencies: append([]string{}, op.Dependencies...),
		}
		if !op.Reversible {
			item.ReversalLimit = "not automatically reversible"
		}
		if recipe, ok := recipes[op.RecipeID]; op.RecipeID != "" && ok {
			item.ToolID = recipe.ID
			item.ToolVersion = op.Version
			if item.ToolVersion == "" {
				item.ToolVersion = recipe.Version
			}
			item.Purpose = recipe.Purpose
			item.DownloadBytes = recipe.EstimatedDownloadBytes
			item.Privileges = append([]string{}, recipe.Privileges...)
			if recipe.Artifact != nil {
				item.DownloadOrigin = recipe.Artifact.Origin
				if recipe.Artifact.Size > item.DownloadBytes {
					item.DownloadBytes = recipe.Artifact.Size
				}
			}
			if recipe.ReversalClass != "" {
				item.ReversalLimit = recipe.ReversalClass
			}
		} else {
			item.DownloadBytes = 0
			item.Privileges = []string{op.Privilege}
		}
		doc.Operations = append(doc.Operations, item)
	}
	return doc
}

func writeReview(w io.Writer, asJSON bool, doc reviewDocument, plan domain.Plan, recipes map[string]domain.Recipe) error {
	if w == nil {
		return errors.New("review output writer is not configured")
	}
	if asJSON {
		encoded, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return fmt.Errorf("encode review JSON: %w", err)
		}
		if _, err := fmt.Fprintln(w, string(encoded)); err != nil {
			return fmt.Errorf("write review JSON: %w", err)
		}
		return nil
	}
	text, err := RenderReviewText(plan, recipes)
	if err != nil {
		return err
	}
	if doc.Status == "expired" {
		text += "Approval unavailable: this plan has expired.\n"
	}
	if _, err := io.WriteString(w, text); err != nil {
		return fmt.Errorf("write plan review: %w", err)
	}
	return nil
}

func promptForApproval(ctx context.Context, input io.Reader, diagnostics io.Writer, digest string) error {
	if input == nil {
		if err := writeDiagnostic(diagnostics, "No approval input is available; no changes were made.\n"); err != nil {
			return fmt.Errorf("write approval diagnostic: %w", err)
		}
		return ErrApprovalRequired
	}
	if err := writeDiagnostic(diagnostics, "To approve only this plan, type: approve %s\nAny other response declines; no changes are made.\n", digest); err != nil {
		return fmt.Errorf("write approval prompt: %w", err)
	}
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 128), 256)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("read approval response: %w", err)
		}
		return ErrDeclined
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("review canceled during approval: %w", err)
	}
	if strings.TrimSpace(scanner.Text()) != "approve "+digest {
		if err := writeDiagnostic(diagnostics, "Approval declined; no changes were made.\n"); err != nil {
			return errors.Join(ErrDeclined, fmt.Errorf("write decline diagnostic: %w", err))
		}
		return ErrDeclined
	}
	return nil
}

func formatProviders(values map[string]string) string {
	if len(values) == 0 {
		return "none specified"
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, displayText(key)+"="+displayText(values[key]))
	}
	return strings.Join(parts, ", ")
}

func formatStrings(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	parts := make([]string, len(values))
	for index, value := range values {
		parts[index] = displayText(value)
	}
	return strings.Join(parts, ", ")
}

func displayText(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
}

func cloneStringsMap(values map[string]string) map[string]string {
	copy := make(map[string]string, len(values))
	for key, value := range values {
		copy[key] = value
	}
	return copy
}

func isLowerDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		isDigit := char >= '0' && char <= '9'
		isLowerHex := char >= 'a' && char <= 'f'
		if !isDigit && !isLowerHex {
			return false
		}
	}
	return true
}

func writeDiagnostic(w io.Writer, format string, values ...any) error {
	if w == nil {
		return nil
	}
	_, err := fmt.Fprintf(w, format, values...)
	return err
}
