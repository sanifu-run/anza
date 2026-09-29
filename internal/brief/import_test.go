package brief

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validBriefJSON = `{"schema_version":1,"project_summary":"A small inventory app","desired_slice":"Track a few items","experience":"beginner","constraints":["Works offline"],"known_stack":["go"],"project_kind":"inventory","existing_project":false}`

func TestBriefImport(t *testing.T) {
	preview, err := ParseJSON([]byte(validBriefJSON))
	if err != nil {
		t.Fatal(err)
	}
	if preview.Kind() != KindProjectBrief {
		t.Fatalf("kind=%q, want %q", preview.Kind(), KindProjectBrief)
	}
	optionalFields := strings.TrimSuffix(validBriefJSON, "}") + `,"client_project":true,"source":{"kind":"learner_export","reviewed":true}}`
	optionalPreview, err := ParseJSON([]byte(optionalFields))
	if err != nil {
		t.Fatalf("schema-declared optional fields rejected: %v", err)
	}
	optionalApproved, err := optionalPreview.Approve(true)
	if err != nil {
		t.Fatal(err)
	}
	if optionalApproved.Brief == nil || !optionalApproved.Brief.ClientProject {
		t.Fatalf("optional client_project field was lost: %#v", optionalApproved.Brief)
	}
	rendered, err := preview.Display()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rendered, "A small inventory app") {
		t.Fatalf("preview omitted project context: %q", rendered)
	}
	if _, err := preview.Approve(false); !errors.Is(err, ErrReviewRequired) {
		t.Fatalf("unreviewed typed brief error=%v, want review required", err)
	}
	approved, err := preview.Approve(true)
	if err != nil {
		t.Fatal(err)
	}
	if approved.Brief == nil || approved.ApprovedBriefText != "" {
		t.Fatalf("typed upload context=%#v, want only typed brief", approved)
	}
	if approved.Brief.ProjectSummary != "A small inventory app" || approved.Brief.DesiredSlice != "Track a few items" || approved.Brief.Source == nil || approved.Brief.Source.Kind != "learner_export" || !approved.Brief.Source.Reviewed {
		t.Fatalf("typed context lost fields or review provenance: %#v", approved.Brief)
	}

	text := "A little garden planner. I want to track seedlings.\n\nExisting text stays untrusted."
	textPreview, err := ParseText([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	textRendered, err := textPreview.Display()
	if err != nil {
		t.Fatal(err)
	}
	if textPreview.Kind() != KindLearnerText || !strings.Contains(textRendered, "untrusted") || !strings.Contains(textRendered, text) {
		t.Fatalf("text preview lost its label or content: %q", textRendered)
	}
	textContext, err := textPreview.Approve(true)
	if err != nil {
		t.Fatal(err)
	}
	if textContext.Brief != nil || !strings.Contains(textContext.ApprovedBriefText, "untrusted context") || !strings.HasSuffix(textContext.ApprovedBriefText, text) {
		t.Fatalf("text context was not labeled or was converted to typed facts: %#v", textContext)
	}
}

func TestRejectSensitiveFields(t *testing.T) {
	secretValue := "contact-secret-value@example.test"
	for _, field := range []string{"api_key", "contact_email", "conversation_token", "transcript_token"} {
		t.Run(field, func(t *testing.T) {
			input := strings.TrimSuffix(validBriefJSON, "}") + `,"` + field + `":"` + secretValue + `"}`
			if _, err := ParseJSON([]byte(input)); err == nil {
				t.Fatal("sensitive field was accepted")
			} else if strings.Contains(err.Error(), secretValue) {
				t.Fatalf("error exposed sensitive value: %q", err.Error())
			}
		})
	}
}

func TestImportNoSideEffects(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "learner-brief.json")
	original := []byte(validBriefJSON)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := preview.Approve(false); !errors.Is(err, ErrReviewRequired) {
		t.Fatalf("unreviewed file error=%v, want review required", err)
	}
	afterBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(afterBytes) != string(original) || before.Mode() != after.Mode() {
		t.Fatal("import modified the source file")
	}
	rendered, err := preview.Display()
	if err != nil {
		t.Fatal(err)
	}
	if len(rendered) == 0 {
		t.Fatal("import did not produce a review preview")
	}

	if _, err := ReadFile(filepath.Join(dir, "unsupported.csv")); err == nil {
		t.Fatal("unsupported source file type was accepted")
	}
	oversized := make([]byte, MaxImportBytes+1)
	if _, err := ParseText(oversized); err == nil {
		t.Fatal("oversized text import was accepted")
	}
}
