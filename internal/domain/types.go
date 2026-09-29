package domain

// ProjectBrief is the reviewed, minimized project context shared with setup.
type ProjectBrief struct {
	SchemaVersion   int          `json:"schema_version"`
	ProjectSummary  string       `json:"project_summary"`
	DesiredSlice    string       `json:"desired_slice"`
	Experience      string       `json:"experience"`
	Constraints     []string     `json:"constraints"`
	KnownStack      []string     `json:"known_stack"`
	ProjectKind     string       `json:"project_kind"`
	ExistingProject bool         `json:"existing_project"`
	ClientProject   bool         `json:"client_project,omitempty"`
	Source          *BriefSource `json:"source,omitempty"`
}

type BriefSource struct {
	Kind     string `json:"kind"`
	Reviewed bool   `json:"reviewed"`
}

type MachineFacts struct {
	OS            string                `json:"os"`
	Arch          string                `json:"arch"`
	OSVersion     string                `json:"os_version"`
	DistroID      string                `json:"distro_id,omitempty"`
	DistroVersion string                `json:"distro_version,omitempty"`
	ShellKind     string                `json:"shell_kind"`
	Capabilities  map[string]Capability `json:"capabilities"`
}

// SetupContextEnvelope preserves the established chat HTTP camelCase wrapper
// while keeping the nested Anza domain payload in snake_case.
type SetupContextEnvelope struct {
	RequestID       string       `json:"requestId"`
	ExpectedVersion uint64       `json:"expectedVersion"`
	CatalogVersion  string       `json:"catalogVersion,omitempty"`
	MachineFacts    MachineFacts `json:"machineFacts"`
}

type Capability struct {
	Status  string `json:"status"`
	Version string `json:"version,omitempty"`
}

type Recommendation struct {
	SchemaVersion        int               `json:"schema_version"`
	CatalogVersion       string            `json:"catalog_version"`
	Summary              string            `json:"summary"`
	SelectedRecipeIDs    []string          `json:"selected_recipe_ids"`
	SelectedPackIDs      []string          `json:"selected_pack_ids"`
	SelectedExerciseID   string            `json:"selected_exercise_id"`
	Reasons              map[string]string `json:"reasons"`
	UnresolvedQuestions  []string          `json:"unresolved_questions"`
	ManualSteps          []string          `json:"manual_steps"`
	ReadinessConstraints []string          `json:"readiness_constraints"`
}

type Recipe struct {
	ID                     string    `json:"id"`
	Version                string    `json:"version"`
	Description            string    `json:"description"`
	Purpose                string    `json:"purpose"`
	SupportedPlatforms     []string  `json:"supported_platforms"`
	Prerequisites          []string  `json:"prerequisites"`
	Detection              string    `json:"detection"`
	InstallStrategy        string    `json:"install_strategy"`
	Artifact               *Artifact `json:"artifact,omitempty"`
	Privileges             []string  `json:"privileges"`
	LicenseNotes           string    `json:"license_notes"`
	EstimatedDownloadBytes int64     `json:"estimated_download_bytes"`
	SideEffects            []string  `json:"side_effects"`
	Verification           string    `json:"verification"`
	ReversalClass          string    `json:"reversal_class"`
}

type Artifact struct {
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
	Origin string `json:"origin"`
}

// Exercise is a catalog-authored, non-executable manual guidance descriptor.
// Scenarios can only report manual or unsupported readiness.
type Exercise struct {
	SchemaVersion int                `json:"schema_version"`
	ID            string             `json:"id"`
	Version       string             `json:"version"`
	Description   string             `json:"description"`
	Scenarios     []ExerciseScenario `json:"scenarios"`
}

type ExerciseScenario struct {
	ID                   string   `json:"id"`
	ProjectKind          string   `json:"project_kind"`
	SupportedPlatforms   []string `json:"supported_platforms"`
	Status               string   `json:"status"`
	Summary              string   `json:"summary"`
	ManualSteps          []string `json:"manual_steps"`
	MissingCapabilityIDs []string `json:"missing_capability_ids"`
	ReadinessConstraints []string `json:"readiness_constraints"`
	Verification         string   `json:"verification"`
}

type Pack struct {
	ID                      string            `json:"id"`
	Version                 string            `json:"version"`
	SkillIDs                []string          `json:"skill_ids"`
	Prerequisites           []string          `json:"prerequisites"`
	LicenseIDs              []string          `json:"license_ids"`
	ProvenanceIDs           []string          `json:"provenance_ids"`
	CompatibleAgentVersions map[string]string `json:"compatible_agent_versions"`
	FileDigests             map[string]string `json:"file_digests"`
}

type Plan struct {
	SchemaVersion        int               `json:"schema_version"`
	ID                   string            `json:"id"`
	WorkspaceID          string            `json:"workspace_id"`
	CatalogDigest        string            `json:"catalog_digest"`
	FactsDigest          string            `json:"facts_digest"`
	RecommendationDigest string            `json:"recommendation_digest"`
	CreatedAt            string            `json:"created_at"`
	ExpiresAt            string            `json:"expires_at"`
	Profile              string            `json:"profile"`
	Providers            map[string]string `json:"providers"`
	Operations           []Operation       `json:"operations"`
	Warnings             []string          `json:"warnings"`
	ManualSteps          []string          `json:"manual_steps"`
	Digest               string            `json:"digest"`
}

type Operation struct {
	ID                    string   `json:"id"`
	Kind                  string   `json:"kind"`
	RecipeID              string   `json:"recipe_id,omitempty"`
	Version               string   `json:"version,omitempty"`
	Dependencies          []string `json:"dependencies"`
	TargetRoot            string   `json:"target_root"`
	RelativePath          string   `json:"relative_path"`
	PreimageHash          string   `json:"preimage_hash,omitempty"`
	ExpectedPostimageHash string   `json:"expected_postimage_hash"`
	Reversible            bool     `json:"reversible"`
	Privilege             string   `json:"privilege"`
	Description           string   `json:"description"`
}

type Approval struct {
	PlanDigest        string `json:"plan_digest"`
	ApprovedAt        string `json:"approved_at"`
	DisclosureVersion string `json:"disclosure_version"`
}

type Receipt struct {
	SchemaVersion        int                `json:"schema_version"`
	PlanDigest           string             `json:"plan_digest"`
	Operations           []ReceiptOperation `json:"operations"`
	OwnedPaths           []string           `json:"owned_paths"`
	OwnedKeys            []string           `json:"owned_keys"`
	PreDigest            string             `json:"pre_digest"`
	PostDigest           string             `json:"post_digest"`
	BackupReferences     []string           `json:"backup_references"`
	StartedAt            string             `json:"started_at"`
	FinishedAt           string             `json:"finished_at"`
	PendingManualActions []string           `json:"pending_manual_actions"`
}

type ReceiptOperation struct {
	ID            string `json:"id"`
	State         string `json:"state"`
	RollbackState string `json:"rollback_state,omitempty"`
	StartedAt     string `json:"started_at,omitempty"`
	FinishedAt    string `json:"finished_at,omitempty"`
}

type CheckResult struct {
	ID           string `json:"id"`
	Required     bool   `json:"required"`
	Status       string `json:"status"`
	Summary      string `json:"summary"`
	NextAction   string `json:"next_action"`
	EvidenceKind string `json:"evidence_kind"`
	CheckedAt    string `json:"checked_at"`
}
