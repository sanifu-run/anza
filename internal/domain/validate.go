package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,99}$`)

func DecodeProjectBrief(data []byte) (ProjectBrief, error) {
	var value ProjectBrief
	if err := decodeStrict(data, &value, "schema_version", "project_summary", "desired_slice", "experience", "constraints", "known_stack", "project_kind", "existing_project"); err != nil {
		return value, err
	}
	return value, validateProjectBrief(value)
}

func DecodeMachineFacts(data []byte) (MachineFacts, error) {
	var value MachineFacts
	if err := decodeStrict(data, &value, "os", "arch", "os_version", "shell_kind", "capabilities"); err != nil {
		return value, err
	}
	return value, validateMachineFacts(value)
}

func DecodeSetupContextEnvelope(data []byte) (SetupContextEnvelope, error) {
	var value SetupContextEnvelope
	if err := decodeStrict(data, &value, "requestId", "expectedVersion", "machineFacts"); err != nil {
		return value, err
	}
	if len(value.RequestID) < 16 || len(value.RequestID) > 64 {
		return value, fieldError("requestId", "must contain 16..64 ASCII alphanumeric, underscore or hyphen characters")
	}
	if value.ExpectedVersion == 0 {
		return value, fieldError("expectedVersion", "must be positive")
	}
	for _, c := range value.RequestID {
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return value, fieldError("requestId", "must contain 16..64 ASCII alphanumeric, underscore or hyphen characters")
		}
	}
	if err := validateMachineFacts(value.MachineFacts); err != nil {
		return value, err
	}
	return value, nil
}

func DecodeRecommendation(data []byte) (Recommendation, error) {
	var value Recommendation
	if err := decodeStrict(data, &value, "schema_version", "catalog_version", "summary", "selected_recipe_ids", "selected_pack_ids", "selected_exercise_id", "reasons", "unresolved_questions", "manual_steps", "readiness_constraints"); err != nil {
		return value, err
	}
	return value, validateRecommendation(value)
}

func DecodeRecipe(data []byte) (Recipe, error) {
	var value Recipe
	if err := decodeStrict(data, &value, "id", "version", "description", "purpose", "supported_platforms", "prerequisites", "detection", "install_strategy", "artifact", "privileges", "license_notes", "estimated_download_bytes", "side_effects", "verification", "reversal_class"); err != nil {
		return value, err
	}
	return value, validateRecipe(value)
}

func DecodePack(data []byte) (Pack, error) {
	var value Pack
	if err := decodeStrict(data, &value, "id", "version", "skill_ids", "prerequisites", "license_ids", "provenance_ids", "compatible_agent_versions", "file_digests"); err != nil {
		return value, err
	}
	return value, validatePack(value)
}

func DecodePlan(data []byte) (Plan, error) {
	var value Plan
	if err := decodeStrict(data, &value, "schema_version", "id", "workspace_id", "catalog_digest", "facts_digest", "recommendation_digest", "created_at", "expires_at", "profile", "providers", "operations", "warnings", "manual_steps", "digest"); err != nil {
		return value, err
	}
	return value, validatePlan(value)
}

func DecodeReceipt(data []byte) (Receipt, error) {
	var value Receipt
	if err := decodeStrict(data, &value, "schema_version", "plan_digest", "operations", "owned_paths", "owned_keys", "pre_digest", "post_digest", "backup_references", "started_at", "finished_at", "pending_manual_actions"); err != nil {
		return value, err
	}
	return value, validateReceipt(value)
}

func DecodeApproval(data []byte) (Approval, error) {
	var value Approval
	if err := decodeStrict(data, &value, "plan_digest", "approved_at", "disclosure_version"); err != nil {
		return value, err
	}
	if !hexDigest(value.PlanDigest) {
		return value, fieldError("plan_digest", "must be a 64-character lowercase SHA-256 digest")
	}
	if _, err := parseUTC("approved_at", value.ApprovedAt); err != nil {
		return value, err
	}
	if !boundedNonEmpty(value.DisclosureVersion, 100) {
		return value, fieldError("disclosure_version", "must contain 1..100 characters")
	}
	return value, nil
}

func DecodeCheckResult(data []byte) (CheckResult, error) {
	var value CheckResult
	if err := decodeStrict(data, &value, "id", "required", "status", "summary", "next_action", "evidence_kind", "checked_at"); err != nil {
		return value, err
	}
	if !validID(value.ID) {
		return value, fieldError("id", "invalid check ID")
	}
	if !oneOf(value.Status, "ready", "action_required", "manual", "unsupported", "blocked", "failed", "unknown") {
		return value, fieldError("status", "unknown check status")
	}
	if !oneOf(value.EvidenceKind, "local", "live", "manual") {
		return value, fieldError("evidence_kind", "unknown evidence kind")
	}
	if !bounded(value.Summary, 2000) || !bounded(value.NextAction, 2000) {
		return value, fieldError("check_result", "summary or next_action exceeds 2000 characters")
	}
	if _, err := parseUTC("checked_at", value.CheckedAt); err != nil {
		return value, err
	}
	return value, nil
}

// DecodeFixture decodes one fixture using the public contract decoder for its type.
func DecodeFixture(kind string, data []byte) error {
	switch kind {
	case "project_brief":
		_, err := DecodeProjectBrief(data)
		return err
	case "machine_facts":
		_, err := DecodeMachineFacts(data)
		return err
	case "chat_setup_context_envelope":
		_, err := DecodeSetupContextEnvelope(data)
		return err
	case "recommendation":
		_, err := DecodeRecommendation(data)
		return err
	case "recipe":
		_, err := DecodeRecipe(data)
		return err
	case "pack":
		_, err := DecodePack(data)
		return err
	case "plan":
		_, err := DecodePlan(data)
		return err
	case "receipt":
		_, err := DecodeReceipt(data)
		return err
	case "approval":
		_, err := DecodeApproval(data)
		return err
	case "check_result":
		_, err := DecodeCheckResult(data)
		return err
	default:
		return fmt.Errorf("unknown fixture type %q", kind)
	}
}

func decodeStrict(data []byte, dst any, required ...string) error {
	if !utf8.Valid(data) {
		return fmt.Errorf("JSON: input is not valid UTF-8")
	}
	if err := rejectDuplicateKeys(data); err != nil {
		return err
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return fmt.Errorf("JSON: expected one object")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		return fmt.Errorf("JSON: %w", err)
	}
	for _, name := range required {
		value, ok := fields[name]
		if !ok {
			return fmt.Errorf("%s: required field is missing", name)
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fieldError(name, "must not be null")
		}
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("JSON: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return fmt.Errorf("JSON: expected exactly one object")
	}
	return nil
}

func rejectDuplicateKeys(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := scanValue(dec, "$ "); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		if err == nil {
			return fmt.Errorf("JSON: trailing value")
		}
		return fmt.Errorf("JSON: %w", err)
	}
	return nil
}

func scanValue(dec *json.Decoder, path string) error {
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("JSON: %w", err)
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for dec.More() {
			keyToken, err := dec.Token()
			if err != nil {
				return fmt.Errorf("JSON: %w", err)
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("JSON: invalid object key")
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("%s%s: duplicate key", path, key)
			}
			seen[key] = struct{}{}
			if err := scanValue(dec, path+key+"."); err != nil {
				return err
			}
		}
		if _, err := dec.Token(); err != nil {
			return fmt.Errorf("JSON: %w", err)
		}
	case '[':
		for i := 0; dec.More(); i++ {
			if err := scanValue(dec, fmt.Sprintf("%s[%d].", path, i)); err != nil {
				return err
			}
		}
		if _, err := dec.Token(); err != nil {
			return fmt.Errorf("JSON: %w", err)
		}
	default:
		return fmt.Errorf("JSON: unexpected delimiter %q", delim)
	}
	return nil
}

func validateProjectBrief(v ProjectBrief) error {
	if v.SchemaVersion != 1 {
		return fieldError("schema_version", "must be 1")
	}
	if n := utf8.RuneCountInString(v.ProjectSummary); n < 1 || n > 4000 {
		return fieldError("project_summary", "must contain 1..4000 characters")
	}
	if !bounded(v.DesiredSlice, 2000) {
		return fieldError("desired_slice", "must contain at most 2000 characters")
	}
	if v.Experience != "beginner" && v.Experience != "developer" {
		return fieldError("experience", "must be beginner or developer")
	}
	if len(v.Constraints) > 20 {
		return fieldError("constraints", "must contain at most 20 items")
	}
	for i, s := range v.Constraints {
		if !bounded(s, 500) {
			return fieldError(fmt.Sprintf("constraints[%d]", i), "must contain at most 500 characters")
		}
	}
	if len(v.KnownStack) > 20 {
		return fieldError("known_stack", "must contain at most 20 IDs")
	}
	for i, id := range v.KnownStack {
		if !validID(id) {
			return fieldError(fmt.Sprintf("known_stack[%d]", i), "invalid ID")
		}
	}
	if !bounded(v.ProjectKind, 200) {
		return fieldError("project_kind", "must contain at most 200 characters")
	}
	if v.Source != nil && (v.Source.Kind != "learner_export" || !v.Source.Reviewed) {
		return fieldError("source", "must be a reviewed learner_export")
	}
	return nil
}

func validateMachineFacts(v MachineFacts) error {
	if v.OS != "darwin" && v.OS != "windows" && v.OS != "linux" {
		return fieldError("os", "unknown operating system")
	}
	if v.Arch != "amd64" && v.Arch != "arm64" {
		return fieldError("arch", "must be amd64 or arm64")
	}
	if !bounded(v.OSVersion, 100) {
		return fieldError("os_version", "must contain at most 100 characters")
	}
	if !bounded(v.DistroID, 100) || !bounded(v.DistroVersion, 100) || !bounded(v.ShellKind, 100) {
		return fieldError("machine_facts", "string field exceeds 100 characters")
	}
	if len(v.Capabilities) > 200 {
		return fieldError("capabilities", "must contain at most 200 entries")
	}
	for id, c := range v.Capabilities {
		if !validID(id) {
			return fieldError("capabilities."+id, "invalid catalog ID")
		}
		if !oneOf(c.Status, "present", "missing", "unknown", "unsupported") {
			return fieldError("capabilities."+id+".status", "unknown status")
		}
		if !bounded(c.Version, 100) {
			return fieldError("capabilities."+id+".version", "must contain at most 100 characters")
		}
	}
	return nil
}

func validateRecommendation(v Recommendation) error {
	if v.SchemaVersion != 1 {
		return fieldError("schema_version", "must be 1")
	}
	if !boundedNonEmpty(v.CatalogVersion, 100) || !bounded(v.Summary, 4000) {
		return fieldError("recommendation", "catalog_version or summary is invalid")
	}
	if len(v.SelectedRecipeIDs) > 30 {
		return fieldError("selected_recipe_ids", "must contain at most 30 IDs")
	}
	if len(v.SelectedPackIDs) > 10 {
		return fieldError("selected_pack_ids", "must contain at most 10 IDs")
	}
	for i, id := range v.SelectedRecipeIDs {
		if !validID(id) {
			return fieldError(fmt.Sprintf("selected_recipe_ids[%d]", i), "invalid ID")
		}
	}
	for i, id := range v.SelectedPackIDs {
		if !validID(id) {
			return fieldError(fmt.Sprintf("selected_pack_ids[%d]", i), "invalid ID")
		}
	}
	if !validID(v.SelectedExerciseID) {
		return fieldError("selected_exercise_id", "invalid ID")
	}
	if err := boundedStrings("unresolved_questions", v.UnresolvedQuestions, 100, 2000); err != nil {
		return err
	}
	if err := boundedStrings("manual_steps", v.ManualSteps, 100, 2000); err != nil {
		return err
	}
	if err := boundedStrings("readiness_constraints", v.ReadinessConstraints, 100, 2000); err != nil {
		return err
	}
	for id, reason := range v.Reasons {
		if !validID(id) {
			return fieldError("reasons."+id, "invalid ID")
		}
		if !boundedNonEmpty(reason, 2000) {
			return fieldError("reasons."+id, "must contain 1..2000 characters")
		}
	}
	return nil
}

func validateRecipe(v Recipe) error {
	if !validID(v.ID) {
		return fieldError("id", "invalid recipe ID")
	}
	if !boundedNonEmpty(v.Version, 100) || !boundedNonEmpty(v.Description, 4000) || !boundedNonEmpty(v.Purpose, 1000) {
		return fieldError("recipe", "version, description and purpose are required and bounded")
	}
	if !oneOf(v.InstallStrategy, "verified_archive", "vendor_installer", "package_manager", "manual") {
		return fieldError("install_strategy", "unknown strategy")
	}
	if v.EstimatedDownloadBytes < 0 || v.Artifact.Size < 0 {
		return fieldError("estimated_download_bytes", "must not be negative")
	}
	if !boundedNonEmpty(v.Artifact.Digest, 128) || !boundedNonEmpty(v.Artifact.Origin, 500) {
		return fieldError("artifact", "digest and origin are required")
	}
	if err := boundedStrings("supported_platforms", v.SupportedPlatforms, 20, 100); err != nil {
		return err
	}
	if err := boundedStrings("prerequisites", v.Prerequisites, 100, 200); err != nil {
		return err
	}
	if err := boundedStrings("privileges", v.Privileges, 20, 100); err != nil {
		return err
	}
	if err := boundedStrings("side_effects", v.SideEffects, 100, 1000); err != nil {
		return err
	}
	return nil
}

func validatePack(v Pack) error {
	if !validID(v.ID) || !boundedNonEmpty(v.Version, 100) {
		return fieldError("id", "invalid pack ID or version")
	}
	if len(v.SkillIDs) > 100 {
		return fieldError("skill_ids", "must contain at most 100 IDs")
	}
	for i, id := range v.SkillIDs {
		if !strings.HasPrefix(id, "anza-") || !validID(id) {
			return fieldError(fmt.Sprintf("skill_ids[%d]", i), "must be an anza- skill ID")
		}
	}
	if len(v.Prerequisites) > 100 || len(v.LicenseIDs) > 100 || len(v.ProvenanceIDs) > 100 {
		return fieldError("pack", "list exceeds 100 items")
	}
	for _, group := range [][]string{v.Prerequisites, v.LicenseIDs, v.ProvenanceIDs} {
		for _, id := range group {
			if !validID(id) {
				return fieldError("pack", "invalid prerequisite, license or provenance ID")
			}
		}
	}
	if len(v.CompatibleAgentVersions) > 20 || len(v.FileDigests) > 500 {
		return fieldError("pack", "map exceeds contract limit")
	}
	for k, value := range v.CompatibleAgentVersions {
		if !validID(k) || !boundedNonEmpty(value, 100) {
			return fieldError("compatible_agent_versions."+k, "invalid agent or version")
		}
	}
	for path, digest := range v.FileDigests {
		if !boundedNonEmpty(path, 500) || !boundedNonEmpty(digest, 128) {
			return fieldError("file_digests."+path, "invalid path or digest")
		}
	}
	return nil
}

func validatePlan(v Plan) error {
	if v.SchemaVersion != 1 {
		return fieldError("schema_version", "must be 1")
	}
	if !boundedNonEmpty(v.ID, 100) || !boundedNonEmpty(v.WorkspaceID, 200) {
		return fieldError("plan", "id and workspace_id are required")
	}
	for name, value := range map[string]string{"catalog_digest": v.CatalogDigest, "facts_digest": v.FactsDigest, "recommendation_digest": v.RecommendationDigest, "digest": v.Digest} {
		if !hexDigest(value) {
			return fieldError(name, "must be a 64-character lowercase SHA-256 digest")
		}
	}
	created, err := parseUTC("created_at", v.CreatedAt)
	if err != nil {
		return err
	}
	expires, err := parseUTC("expires_at", v.ExpiresAt)
	if err != nil {
		return err
	}
	if !expires.After(created) {
		return fieldError("expires_at", "must be after created_at")
	}
	if v.Profile != "beginner" && v.Profile != "developer" {
		return fieldError("profile", "must be beginner or developer")
	}
	if len(v.Providers) > 20 || len(v.Operations) > 500 {
		return fieldError("plan", "providers or operations exceed limit")
	}
	for key, provider := range v.Providers {
		if !validID(key) || !boundedNonEmpty(provider, 100) {
			return fieldError("providers."+key, "invalid provider choice")
		}
	}
	for i, op := range v.Operations {
		if !validID(op.ID) || !boundedNonEmpty(op.Kind, 100) || !boundedNonEmpty(op.TargetRoot, 100) || !safeRelative(op.RelativePath) || !hexDigest(op.ExpectedPostimageHash) || !boundedNonEmpty(op.Privilege, 100) || !boundedNonEmpty(op.Description, 1000) {
			return fieldError(fmt.Sprintf("operations[%d]", i), "invalid required operation field")
		}
		if op.PreimageHash != "" && !hexDigest(op.PreimageHash) {
			return fieldError(fmt.Sprintf("operations[%d].preimage_hash", i), "must be a 64-character lowercase SHA-256 digest")
		}
		if op.RecipeID != "" && !validID(op.RecipeID) {
			return fieldError(fmt.Sprintf("operations[%d].recipe_id", i), "invalid recipe ID")
		}
		if op.Dependencies == nil {
			return fieldError(fmt.Sprintf("operations[%d].dependencies", i), "must be an array")
		}
		if len(op.Dependencies) > 500 {
			return fieldError(fmt.Sprintf("operations[%d].dependencies", i), "too many dependencies")
		}
	}
	if err := boundedStrings("warnings", v.Warnings, 200, 2000); err != nil {
		return err
	}
	return boundedStrings("manual_steps", v.ManualSteps, 200, 2000)
}

func validateReceipt(v Receipt) error {
	if v.SchemaVersion != 1 || !hexDigest(v.PlanDigest) || !hexDigest(v.PreDigest) || !hexDigest(v.PostDigest) {
		return fieldError("receipt", "schema_version must be 1 and digests must be SHA-256")
	}
	if _, err := parseUTC("started_at", v.StartedAt); err != nil {
		return err
	}
	if _, err := parseUTC("finished_at", v.FinishedAt); err != nil {
		return err
	}
	if len(v.Operations) > 500 {
		return fieldError("operations", "must contain at most 500 items")
	}
	for i, op := range v.Operations {
		if !validID(op.ID) || !oneOf(op.State, "planned", "running", "succeeded", "failed", "unknown", "skipped") {
			return fieldError(fmt.Sprintf("operations[%d]", i), "invalid ID or state")
		}
		if op.RollbackState != "" && !oneOf(op.RollbackState, "rolling_back", "rolled_back", "conflict") {
			return fieldError(fmt.Sprintf("operations[%d].rollback_state", i), "unknown rollback state")
		}
	}
	return nil
}

func fieldError(field, message string) error { return fmt.Errorf("%s: %s", field, message) }
func bounded(s string, max int) bool         { return utf8.ValidString(s) && utf8.RuneCountInString(s) <= max }
func boundedNonEmpty(s string, max int) bool { return bounded(s, max) && utf8.RuneCountInString(s) > 0 }
func validID(s string) bool                  { return idPattern.MatchString(s) }
func oneOf(s string, values ...string) bool {
	for _, v := range values {
		if s == v {
			return true
		}
	}
	return false
}
func boundedStrings(field string, values []string, maxCount, maxLength int) error {
	if len(values) > maxCount {
		return fieldError(field, fmt.Sprintf("must contain at most %d items", maxCount))
	}
	for i, s := range values {
		if !bounded(s, maxLength) {
			return fieldError(fmt.Sprintf("%s[%d]", field, i), fmt.Sprintf("must contain at most %d characters", maxLength))
		}
	}
	return nil
}
func hexDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func safeRelative(s string) bool {
	if s == "" || strings.HasPrefix(s, "/") || strings.Contains(s, `\`) {
		return false
	}
	if len(s) >= 2 && ((s[0] >= 'A' && s[0] <= 'Z') || (s[0] >= 'a' && s[0] <= 'z')) && s[1] == ':' {
		return false
	}
	for _, segment := range strings.Split(s, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	return true
}
func parseUTC(field, value string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fieldError(field, "must be RFC3339")
	}
	_, offset := t.Zone()
	if offset != 0 {
		return time.Time{}, fieldError(field, "must use UTC")
	}
	return t, nil
}
