package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/sanifu-run/anza/internal/brief"
	"github.com/sanifu-run/anza/internal/domain"
	"github.com/sanifu-run/anza/internal/interviewclient"
)

const SetupConsentVersion = "setup-privacy-v1"

var (
	ErrWizardEOF       = errors.New("wizard paused at end of input; private progress was saved")
	ErrWizardCancelled = errors.New("wizard interrupted")
)

// WizardChat is the small part of the shared chat client used by setup.
type WizardChat interface {
	Capabilities(context.Context) (interviewclient.Capabilities, error)
	NewSession(context.Context, string, interviewclient.SetupStart) (WizardSession, error)
	ResumeSession(string) (WizardSession, error)
}

// WizardSession represents one private, recovery-capable shared chat conversation.
type WizardSession interface {
	Ask(context.Context, string) (interviewclient.AskResponse, error)
	UpdateContext(context.Context, *domain.ProjectBrief, *string, *domain.MachineFacts) (interviewclient.Conversation, error)
	Recommend(context.Context) (interviewclient.RecommendationResponse, error)
}

type wizardStateStore interface {
	Save(string, any) error
	Load(string, any) error
}

type Wizard struct {
	in             *lineInput
	chat           WizardChat
	store          wizardStateStore
	facts          *domain.MachineFacts
	privacy        string
	recommendation *interviewclient.RecommendationResponse
}

type wizardDraft struct {
	SchemaVersion   int                  `json:"schema_version"`
	Name            string               `json:"name"`
	Experience      string               `json:"experience,omitempty"`
	Brief           *domain.ProjectBrief `json:"brief,omitempty"`
	BriefText       *string              `json:"brief_text,omitempty"`
	ProjectSummary  string               `json:"project_summary,omitempty"`
	DesiredSlice    string               `json:"desired_slice,omitempty"`
	ProjectCaptured bool                 `json:"project_captured,omitempty"`
	BriefSelected   bool                 `json:"brief_selected,omitempty"`
}

// NewWizard constructs an injectable text wizard. The store must be the same
// private state store supplied to the shared interview client.
func NewWizard(reader io.Reader, writer io.Writer, chat *interviewclient.Client, store wizardStateStore, facts *domain.MachineFacts) (*Wizard, error) {
	if reader == nil || writer == nil || chat == nil || store == nil {
		return nil, errors.New("wizard requires input, output, shared chat client, and private state store")
	}
	return &Wizard{in: newLineInput(reader, writer), chat: liveWizardChat{chat}, store: store, facts: facts, privacy: defaultSetupDisclosure}, nil
}

// NewWizardWithChat supports deterministic local scripted sessions.
func NewWizardWithChat(reader io.Reader, writer io.Writer, chat WizardChat, store wizardStateStore, facts *domain.MachineFacts) (*Wizard, error) {
	if reader == nil || writer == nil || chat == nil || store == nil {
		return nil, errors.New("wizard requires input, output, shared chat client, and private state store")
	}
	return &Wizard{in: newLineInput(reader, writer), chat: chat, store: store, facts: facts, privacy: defaultSetupDisclosure}, nil
}

const defaultSetupDisclosure = "Your reviewed project details and interview answers will be sent to the existing Sanifu chat service. Its AWS conversation store expires after 30 days without updates. External model providers may retain copies under their own policies, and deleting the chat cannot remove those copies. Setup conversations are excluded from owner summary email."

type liveWizardChat struct{ client *interviewclient.Client }

type liveWizardSession struct{ session *interviewclient.Session }

func (s liveWizardSession) Ask(ctx context.Context, message string) (interviewclient.AskResponse, error) {
	return s.session.Ask(ctx, message)
}
func (s liveWizardSession) UpdateContext(ctx context.Context, brief *domain.ProjectBrief, text *string, facts *domain.MachineFacts) (interviewclient.Conversation, error) {
	return s.session.UpdateContext(ctx, brief, text, facts)
}
func (s liveWizardSession) Recommend(ctx context.Context) (interviewclient.RecommendationResponse, error) {
	return s.session.Recommend(ctx)
}

func (c liveWizardChat) Capabilities(ctx context.Context) (interviewclient.Capabilities, error) {
	return c.client.Capabilities(ctx)
}
func (c liveWizardChat) NewSession(ctx context.Context, name string, start interviewclient.SetupStart) (WizardSession, error) {
	s, err := c.client.NewSession(ctx, name, start)
	return liveWizardSession{s}, err
}
func (c liveWizardChat) ResumeSession(name string) (WizardSession, error) {
	s, err := interviewclient.ResumeSession(c.client, name)
	if err != nil {
		return nil, err
	}
	return liveWizardSession{s}, nil
}

func (w *Wizard) Run(ctx context.Context) error {
	name, err := w.prompt(ctx, "Private session name (1-48 letters, numbers, dash or underscore): ")
	if err != nil {
		return w.handleStop(err)
	}
	name = strings.TrimSpace(name)
	if !validWizardName(name) {
		return errors.New("session name must contain 1..48 letters, numbers, dash or underscore")
	}
	choice, err := w.prompt(ctx, "Choose new interview or resume an existing interview [new/resume]: ")
	if err != nil {
		return w.handleStop(err)
	}
	var session WizardSession
	if strings.EqualFold(strings.TrimSpace(choice), "resume") {
		session, err = w.chat.ResumeSession(name)
		if err != nil {
			return fmt.Errorf("resume private interview: %w", err)
		}
	} else {
		if err := w.newInterview(ctx, name); err != nil {
			return err
		}
		session, err = w.chat.ResumeSession(name)
		if err != nil {
			return fmt.Errorf("load private interview after setup: %w", err)
		}
	}
	return w.interview(ctx, session)
}

func (w *Wizard) newInterview(ctx context.Context, name string) error {
	var draft wizardDraft
	key := "wizard-" + name
	if err := w.store.Load(key, &draft); err == nil && draft.SchemaVersion == 1 {
		answer, err := w.prompt(ctx, "Saved private progress found. Resume it? [yes/no]: ")
		if err != nil {
			return w.pause(draft, err)
		}
		if strings.EqualFold(strings.TrimSpace(answer), "yes") {
			if err := w.completeDraft(ctx, &draft); err != nil {
				return err
			}
			if err := w.selectBrief(ctx, &draft); err != nil {
				return err
			}
			if err := w.previewAndConsent(ctx, draft); err != nil {
				return err
			}
			return w.finishSetup(ctx, name, draft)
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("load private wizard progress: %w", err)
	}
	draft = wizardDraft{SchemaVersion: 1, Name: name}
	if err := w.completeDraft(ctx, &draft); err != nil {
		return err
	}
	if err := w.selectBrief(ctx, &draft); err != nil {
		return err
	}
	if err := w.previewAndConsent(ctx, draft); err != nil {
		return err
	}
	return w.finishSetup(ctx, name, draft)
}

func (w *Wizard) completeDraft(ctx context.Context, draft *wizardDraft) error {
	if draft.Experience == "" {
		for {
			value, err := w.prompt(ctx, "Experience level [beginner/developer]: ")
			if err != nil {
				return w.pause(*draft, err)
			}
			value = strings.ToLower(strings.TrimSpace(value))
			if value == "beginner" || value == "developer" {
				draft.Experience = value
				break
			}
			fmt.Fprintln(w.in.writer, "Enter beginner or developer.")
		}
		if draft.Experience == "beginner" {
			fmt.Fprintln(w.in.writer, "I’ll explain terms and ask one question at a time. You can type skip, back, or resume during the interview.")
		} else {
			fmt.Fprintln(w.in.writer, "I’ll use concise technical language and ask one question at a time. You can type skip, back, or resume during the interview.")
		}
		if err := w.saveDraft(*draft); err != nil {
			return err
		}
	}
	if draft.ProjectSummary == "" {
		summary, err := w.prompt(ctx, "What are you building? ")
		if err != nil {
			return w.pause(*draft, err)
		}
		draft.ProjectSummary = summary
		if strings.TrimSpace(summary) == "" {
			draft.ProjectSummary = "Project details to be clarified in interview"
		}
		if err := w.saveDraft(*draft); err != nil {
			return err
		}
	}
	if !draft.ProjectCaptured {
		slice, err := w.prompt(ctx, "What is the first useful part you want to make? ")
		if err != nil {
			return w.pause(*draft, err)
		}
		if strings.TrimSpace(slice) != "skip" {
			draft.DesiredSlice = slice
		}
		draft.ProjectCaptured = true
		if err := w.saveDraft(*draft); err != nil {
			return err
		}
	}
	if draft.Brief == nil && draft.BriefText == nil {
		draft.Brief = &domain.ProjectBrief{
			SchemaVersion: 1, ProjectSummary: draft.ProjectSummary, DesiredSlice: draft.DesiredSlice, Experience: draft.Experience,
			Constraints: []string{}, KnownStack: []string{},
		}
	}
	return nil
}

func (w *Wizard) selectBrief(ctx context.Context, draft *wizardDraft) error {
	if draft.BriefSelected {
		return nil
	}
	path, err := w.prompt(ctx, "Optional reviewed brief file path (press Enter to skip): ")
	if err != nil {
		return w.pause(*draft, err)
	}
	if strings.TrimSpace(path) != "" {
		preview, err := brief.ReadFile(strings.TrimSpace(path))
		if err != nil {
			return fmt.Errorf("read selected brief: %w", err)
		}
		text, err := preview.Display()
		if err != nil {
			return err
		}
		fmt.Fprintf(w.in.writer, "%s\n", terminalText(text))
		approve, err := w.prompt(ctx, "Upload this reviewed brief with your setup? [yes/no]: ")
		if err != nil {
			return w.pause(*draft, err)
		}
		if strings.EqualFold(strings.TrimSpace(approve), "yes") {
			context, err := preview.Approve(true)
			if err != nil {
				return err
			}
			if context.Brief != nil {
				if context.Brief.Constraints == nil {
					context.Brief.Constraints = []string{}
				}
				if context.Brief.KnownStack == nil {
					context.Brief.KnownStack = []string{}
				}
				draft.Brief = context.Brief
			}
			if context.ApprovedBriefText != "" {
				draft.Brief = nil
				text := context.ApprovedBriefText
				draft.BriefText = &text
			}
		}
	}
	draft.BriefSelected = true
	return w.saveDraft(*draft)
}

func (w *Wizard) previewAndConsent(ctx context.Context, draft wizardDraft) error {
	if w.facts != nil {
		encoded, err := json.MarshalIndent(w.facts, "", "  ")
		if err != nil {
			return fmt.Errorf("preview machine facts: %w", err)
		}
		fmt.Fprintf(w.in.writer, "Machine facts to share:\n%s\n", terminalText(string(encoded)))
	} else {
		fmt.Fprintln(w.in.writer, "No machine facts will be shared.")
	}
	if draft.Brief != nil {
		encoded, err := json.MarshalIndent(draft.Brief, "", "  ")
		if err != nil {
			return fmt.Errorf("preview project details: %w", err)
		}
		fmt.Fprintf(w.in.writer, "Project details to share:\n%s\n", terminalText(string(encoded)))
	} else if draft.BriefText != nil {
		fmt.Fprintf(w.in.writer, "Reviewed brief text to share:\n%s\n", terminalText(*draft.BriefText))
	}
	fmt.Fprintln(w.in.writer, terminalText(w.privacy))
	consent, err := w.prompt(ctx, "Send these details to the shared chat service and start setup? [yes/no]: ")
	if err != nil {
		return w.pause(draft, err)
	}
	if !strings.EqualFold(strings.TrimSpace(consent), "yes") {
		return errors.New("setup not started; consent was not recorded")
	}
	return nil
}

func (w *Wizard) finishSetup(ctx context.Context, name string, draft wizardDraft) error {
	if _, err := w.chat.Capabilities(ctx); err != nil {
		return fmt.Errorf("check shared chat setup capability: %w", err)
	}
	start := interviewclient.SetupStart{ConsentVersion: SetupConsentVersion, Brief: draft.Brief, ApprovedBriefText: draft.BriefText, MachineFacts: w.facts}
	_, err := w.chat.NewSession(ctx, name, start)
	if err != nil {
		return fmt.Errorf("start shared chat setup: %w", err)
	}
	return nil
}

func (w *Wizard) interview(ctx context.Context, session WizardSession) error {
	for {
		answer, err := w.prompt(ctx, "Your answer (skip/back/resume, or :recommend): ")
		if err != nil {
			return w.handleStop(err)
		}
		trimmed := strings.TrimSpace(answer)
		switch strings.ToLower(trimmed) {
		case "resume":
			fmt.Fprintln(w.in.writer, "This private interview is already resumed. Continue with your next answer.")
			continue
		case "back":
			fmt.Fprintln(w.in.writer, "Send the corrected detail below; I’ll update shared context before continuing.")
			corrected, err := w.prompt(ctx, "Corrected detail: ")
			if err != nil {
				return w.handleStop(err)
			}
			if _, err := session.UpdateContext(ctx, nil, &corrected, nil); err != nil {
				return fmt.Errorf("update interview context: %w", err)
			}
			trimmed = "Please replace the prior answer with this correction: " + corrected
		case "skip":
			trimmed = "I would like to skip this question."
		case ":recommend":
			recommendation, err := session.Recommend(ctx)
			if err != nil {
				return fmt.Errorf("get typed setup recommendation: %w", err)
			}
			w.recommendation = &recommendation
			fmt.Fprintf(w.in.writer, "Typed recommendation:\n%s\n", terminalText(recommendation.Recommendation.Summary))
			return nil
		}
		response, err := session.Ask(ctx, trimmed)
		if err != nil {
			return fmt.Errorf("send interview answer: %w", err)
		}
		fmt.Fprintf(w.in.writer, "%s\n", terminalText(response.Answer))
	}
}

func (w *Wizard) prompt(ctx context.Context, label string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", w.handleStop(err)
	}
	value, err := w.in.ask(label)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return "", w.handleStop(ctxErr)
	}
	return value, err
}

func (w *Wizard) saveDraft(draft wizardDraft) error {
	if err := w.store.Save("wizard-"+draft.Name, draft); err != nil {
		return fmt.Errorf("save private wizard progress: %w", err)
	}
	return nil
}
func (w *Wizard) pause(draft wizardDraft, cause error) error {
	if errors.Is(cause, io.EOF) || errors.Is(cause, context.Canceled) {
		if draft.Name != "" {
			if err := w.saveDraft(draft); err != nil {
				return err
			}
		}
		if errors.Is(cause, io.EOF) {
			return ErrWizardEOF
		}
		return ErrWizardCancelled
	}
	return w.handleStop(cause)
}
func (w *Wizard) handleStop(err error) error {
	if errors.Is(err, io.EOF) {
		return ErrWizardEOF
	}
	if errors.Is(err, context.Canceled) {
		return ErrWizardCancelled
	}
	return err
}

// ExitCode maps contract-defined user exits for a caller to use in main.
func ExitCode(err error) int {
	switch {
	case err == nil:
		return 0
	case errors.Is(err, ErrWizardEOF):
		return 3
	case errors.Is(err, ErrWizardCancelled):
		return 130
	default:
		return 4
	}
}

func validWizardName(name string) bool {
	if len(name) == 0 || len(name) > 48 {
		return false
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}
