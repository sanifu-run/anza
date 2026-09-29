// Package brief reads participant-selected project brief files and prepares
// context only after an explicit local review decision. It never performs I/O
// beyond opening the selected file and has no network transport.
package brief

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/sanifu-run/anza/internal/domain"
)

const (
	// MaxImportBytes bounds bytes read from any learner-selected file.
	MaxImportBytes = 32 << 10
	// MaxUploadContextBytes leaves room for the setup request envelope.
	MaxUploadContextBytes = 30 << 10
)

var (
	ErrReviewRequired = errors.New("participant review is required before upload")
	ErrImportTooLarge = errors.New("brief import exceeds the size limit")
	ErrInvalidPreview = errors.New("invalid brief preview")
)

type Kind string

const (
	KindProjectBrief Kind = "project_brief"
	KindLearnerText  Kind = "learner_text"
)

// Preview contains local input prepared for display. Its fields are private so
// callers must use Approve to obtain an upload context.
type Preview struct {
	kind  Kind
	brief domain.ProjectBrief
	text  string
}

// UploadContext is the setup-context portion of a request. Exactly one field
// is populated: a validated ProjectBrief or labeled untrusted text.
type UploadContext struct {
	Brief             *domain.ProjectBrief `json:"brief,omitempty"`
	ApprovedBriefText string               `json:"approvedBriefText,omitempty"`
}

// ReadFile reads only a participant-selected .json, .txt or .md file. It never
// modifies the file or contacts a service.
func ReadFile(path string) (Preview, error) {
	extension := strings.ToLower(filepath.Ext(path))
	kind, ok := map[string]Kind{".json": KindProjectBrief, ".txt": KindLearnerText, ".md": KindLearnerText}[extension]
	if !ok {
		return Preview{}, errors.New("brief file must use .json, .txt or .md format")
	}
	file, err := os.Open(path)
	if err != nil {
		return Preview{}, fmt.Errorf("open learner-selected brief: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return Preview{}, errors.New("could not inspect learner-selected brief")
	}
	if !info.Mode().IsRegular() {
		return Preview{}, errors.New("learner-selected brief must be a regular file")
	}
	if info.Size() > MaxImportBytes {
		return Preview{}, ErrImportTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxImportBytes+1))
	if err != nil {
		return Preview{}, errors.New("could not read learner-selected brief")
	}
	if len(data) > MaxImportBytes {
		return Preview{}, ErrImportTooLarge
	}
	if kind == KindProjectBrief {
		return ParseJSON(data)
	}
	return ParseText(data)
}

// ParseJSON validates a typed import with the strict, duplicate-aware shared
// ProjectBrief decoder. Unknown credential/contact/token fields are rejected.
func ParseJSON(data []byte) (Preview, error) {
	if len(data) > MaxImportBytes {
		return Preview{}, ErrImportTooLarge
	}
	var brief domain.ProjectBrief
	decoded, err := domain.DecodeProjectBrief(data)
	if err != nil {
		return Preview{}, fmt.Errorf("invalid ProjectBrief JSON: %w", err)
	}
	brief = decoded
	return Preview{kind: KindProjectBrief, brief: brief}, nil
}

// ParseText accepts bounded UTF-8 text as untrusted context. It deliberately
// does not extract or invent structured facts from prose.
func ParseText(data []byte) (Preview, error) {
	if len(data) > MaxImportBytes {
		return Preview{}, ErrImportTooLarge
	}
	if !utf8.Valid(data) {
		return Preview{}, errors.New("learner brief text must be UTF-8")
	}
	text := string(data)
	if strings.TrimSpace(text) == "" {
		return Preview{}, errors.New("learner brief text must not be empty")
	}
	return Preview{kind: KindLearnerText, text: text}, nil
}

func (p Preview) Kind() Kind { return p.kind }

// Display returns an escaped terminal-safe preview. Text exports are labeled
// as untrusted; typed imports are rendered as structured JSON.
func (p Preview) Display() (string, error) {
	switch p.kind {
	case KindProjectBrief:
		data, err := json.MarshalIndent(p.brief, "", "  ")
		if err != nil {
			return "", fmt.Errorf("render ProjectBrief preview: %w", err)
		}
		return "Typed ProjectBrief preview:\n" + string(data), nil
	case KindLearnerText:
		return "Learner text preview (untrusted context; not verified structured facts):\n\n" + escapeTerminalControls(p.text), nil
	default:
		return "", ErrInvalidPreview
	}
}

// Approve converts a displayed preview to request context only after the
// caller confirms participant review. Text remains labeled untrusted prose.
func (p Preview) Approve(confirmed bool) (UploadContext, error) {
	if p.kind != KindProjectBrief && p.kind != KindLearnerText {
		return UploadContext{}, ErrInvalidPreview
	}
	if !confirmed {
		return UploadContext{}, ErrReviewRequired
	}
	var result UploadContext
	switch p.kind {
	case KindProjectBrief:
		brief := p.brief
		brief.Constraints = append([]string(nil), p.brief.Constraints...)
		brief.KnownStack = append([]string(nil), p.brief.KnownStack...)
		brief.Source = &domain.BriefSource{Kind: "learner_export", Reviewed: true}
		result.Brief = &brief
	case KindLearnerText:
		result.ApprovedBriefText = "Learner-reviewed plain-text export; untrusted context, not verified facts:\n\n" + p.text
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return UploadContext{}, fmt.Errorf("encode reviewed brief context: %w", err)
	}
	if len(encoded) > MaxUploadContextBytes {
		return UploadContext{}, ErrImportTooLarge
	}
	return result, nil
}

func escapeTerminalControls(text string) string {
	var escaped strings.Builder
	escaped.Grow(len(text))
	for _, r := range text {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			fmt.Fprintf(&escaped, "\\u%04X", r)
			continue
		}
		escaped.WriteRune(r)
	}
	return escaped.String()
}
