package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/sanifu-run/anza/internal/domain"
	"github.com/sanifu-run/anza/internal/interviewclient"
)

type memoryState struct{ values map[string]any }

func (s *memoryState) Save(key string, value any) error {
	if s.values == nil {
		s.values = map[string]any{}
	}
	s.values[key] = value
	return nil
}
func (s *memoryState) Load(key string, value any) error {
	v, ok := s.values[key]
	if !ok {
		return os.ErrNotExist
	}
	d, ok := value.(*wizardDraft)
	if !ok {
		return errors.New("unexpected type")
	}
	*d = v.(wizardDraft)
	return nil
}

type scriptedChat struct {
	session *scriptedSession
	starts  int
	start   interviewclient.SetupStart
}

func (c *scriptedChat) Capabilities(context.Context) (interviewclient.Capabilities, error) {
	return interviewclient.Capabilities{Enabled: true}, nil
}
func (c *scriptedChat) NewSession(_ context.Context, _ string, start interviewclient.SetupStart) (WizardSession, error) {
	c.starts++
	c.start = start
	return c.session, nil
}
func (c *scriptedChat) ResumeSession(string) (WizardSession, error) { return c.session, nil }

type scriptedSession struct {
	asks         []string
	updates      int
	answer       string
	privateToken string
	recommended  bool
}

func (s *scriptedSession) Ask(_ context.Context, message string) (interviewclient.AskResponse, error) {
	s.asks = append(s.asks, message)
	return interviewclient.AskResponse{Mode: "llm", Answer: s.answer}, nil
}
func (s *scriptedSession) UpdateContext(context.Context, *domain.ProjectBrief, *string, *domain.MachineFacts) (interviewclient.Conversation, error) {
	s.updates++
	return interviewclient.Conversation{Version: 2}, nil
}
func (s *scriptedSession) Recommend(context.Context) (interviewclient.RecommendationResponse, error) {
	s.recommended = true
	return interviewclient.RecommendationResponse{Recommendation: domain.Recommendation{Summary: "A typed recommendation"}}, nil
}

func TestWizardBeginnerDeveloper(t *testing.T) {
	for _, level := range []string{"beginner", "developer"} {
		t.Run(level, func(t *testing.T) {
			store := &memoryState{}
			session := &scriptedSession{}
			chat := &scriptedChat{session: session}
			input := strings.Join([]string{"demo", "new", level, "A learning app", "A login page", "", "yes", ":recommend"}, "\n") + "\n"
			var output strings.Builder
			w, err := NewWizardWithChat(strings.NewReader(input), &output, chat, store, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err = w.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			if chat.starts != 1 || chat.start.ConsentVersion != SetupConsentVersion || chat.start.Brief == nil || chat.start.Brief.Experience != level {
				t.Fatalf("setup did not receive reviewed typed project context: %#v", chat.start)
			}
			if !session.recommended || !strings.Contains(output.String(), "A typed recommendation") {
				t.Fatal("typed recommendation was not shown")
			}
		})
	}
}

func TestWizardEOFResume(t *testing.T) {
	store := &memoryState{}
	session := &scriptedSession{}
	chat := &scriptedChat{session: session}
	var firstOut strings.Builder
	w, _ := NewWizardWithChat(strings.NewReader("resume-me\nnew\nbeginner\nA saved project summary\n"), &firstOut, chat, store, nil)
	err := w.Run(context.Background())
	if !errors.Is(err, ErrWizardEOF) || ExitCode(err) != 3 {
		t.Fatalf("EOF exit=%v code=%d", err, ExitCode(err))
	}
	if _, ok := store.values["wizard-resume-me"]; !ok {
		t.Fatal("EOF did not persist private wizard progress")
	}
	var secondOut strings.Builder
	w, _ = NewWizardWithChat(strings.NewReader("resume-me\nnew\nyes\nA saved feature\n\nyes\n:recommend\n"), &secondOut, chat, store, nil)
	if err = w.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if chat.starts != 1 || !session.recommended {
		t.Fatal("saved setup did not resume through shared chat")
	}
}

func TestWizardConsent(t *testing.T) {
	store := &memoryState{}
	chat := &scriptedChat{session: &scriptedSession{}}
	input := strings.Join([]string{"declined", "new", "developer", "Build a tool", "Ship one feature", "", "no"}, "\n") + "\n"
	w, _ := NewWizardWithChat(strings.NewReader(input), io.Discard, chat, store, nil)
	err := w.Run(context.Background())
	if err == nil || chat.starts != 0 {
		t.Fatal("declined consent still started setup")
	}
	if !strings.Contains(err.Error(), "consent was not recorded") {
		t.Fatalf("unexpected decline result: %v", err)
	}
}

func TestPlainTerminal(t *testing.T) {
	store := &memoryState{}
	session := &scriptedSession{answer: "\x1b[31mplain reply\x1b[0m", privateToken: "recovery-token-must-never-print"}
	chat := &scriptedChat{session: session}
	input := strings.Join([]string{"plain", "new", "developer", "Project", "Feature", "", "yes", "hello", "skip", "back", "corrected", "resume", ":recommend"}, "\n") + "\n"
	var output strings.Builder
	w, _ := NewWizardWithChat(strings.NewReader(input), &output, chat, store, nil)
	if err := w.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "\x1b") || strings.Contains(output.String(), session.privateToken) {
		t.Fatalf("terminal output leaked control or private token: %q", output.String())
	}
	if session.updates != 1 || !session.recommended {
		t.Fatalf("back/recommend flow incomplete: updates=%d recommend=%v", session.updates, session.recommended)
	}
}

func TestWizardResumeSession(t *testing.T) {
	chat := &scriptedChat{session: &scriptedSession{}}
	w, _ := NewWizardWithChat(strings.NewReader("existing-session\nresume\n:recommend\n"), io.Discard, chat, &memoryState{}, nil)
	if err := w.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if chat.starts != 0 || !chat.session.recommended {
		t.Fatal("existing shared conversation was not resumed")
	}
}

type cancelOnTextWriter struct {
	writer io.Writer
	cancel context.CancelFunc
	needle string
}

func (w cancelOnTextWriter) Write(value []byte) (int, error) {
	n, err := w.writer.Write(value)
	if strings.Contains(string(value), w.needle) {
		w.cancel()
	}
	return n, err
}

func TestWizardCancellationExit(t *testing.T) {
	chat := &scriptedChat{session: &scriptedSession{}}
	store := &memoryState{}
	ctx, cancel := context.WithCancel(context.Background())
	var output strings.Builder
	writer := cancelOnTextWriter{writer: &output, cancel: cancel, needle: "What are you building?"}
	w, _ := NewWizardWithChat(strings.NewReader("cancel-me\nnew\nbeginner\n"), writer, chat, store, nil)
	err := w.Run(ctx)
	if !errors.Is(err, ErrWizardCancelled) || ExitCode(err) != 130 {
		t.Fatalf("cancellation exit=%v code=%d", err, ExitCode(err))
	}
	if _, ok := store.values["wizard-cancel-me"]; !ok {
		t.Fatal("Ctrl-C did not save private wizard progress")
	}
}
