package acceptance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sanifu-run/anza/internal/catalog"
	"github.com/sanifu-run/anza/internal/domain"
	"github.com/sanifu-run/anza/internal/interviewclient"
)

type harness struct {
	fixture *chatFixture
	proxy   *localProxy
	binary  string
}

func newHarness(t *testing.T, rewrite func(*http.Response, *http.Request) error) *harness {
	t.Helper()
	chatSource := requireChatSource(t)
	fixture := startChatFixture(t, chatSource)
	proxy := startChatProxyWith(t, fixture.ready.BaseURL, fixture.root, rewrite)
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("synthetic loopback proxy CONNECTs=%v events=%v routes=%v proxy_events=%v", proxy.connectSnapshot(), proxy.connectEventSnapshot(), proxy.routeSnapshot(), proxy.proxyEventSnapshot())
		}
	})
	binary := strings.TrimSpace(os.Getenv("ANZA_BINARY"))
	if binary == "" {
		t.Fatal("ANZA_BINARY must point to the CLI built by scripts/tests/chat-contract.sh under the shared build lease")
	}
	info, err := os.Stat(binary)
	if err != nil || info.IsDir() {
		t.Fatalf("ANZA_BINARY must be an executable Anza CLI: %q", binary)
	}
	return &harness{fixture: fixture, proxy: proxy, binary: binary}
}

func TestCLIPrivateStateIsolationPreservesHome(t *testing.T) {
	root := filepath.Join(t.TempDir(), "private-cli")
	homeBefore, homeWasSet := os.LookupEnv("HOME")
	env := privateUserStateEnvironment(os.Environ(), root, nil)
	got := make(map[string]string, len(env))
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			got[key] = value
		}
	}
	if got["ANZA_STATE_DIR"] != filepath.Join(root, "state") {
		t.Fatalf("private Anza state root = %q, want %q", got["ANZA_STATE_DIR"], filepath.Join(root, "state"))
	}
	if homeAfter, ok := got["HOME"]; ok != homeWasSet || (ok && homeAfter != homeBefore) {
		t.Fatalf("fixture changed inherited HOME: before=%q present=%t after=%q present=%t", homeBefore, homeWasSet, homeAfter, ok)
	}
}

func TestFreshJourney(t *testing.T) {
	h := newHarness(t, nil)
	root := filepath.Join(t.TempDir(), "home")
	cli := startCLI(t, h.binary, root, h.proxy.addr, h.proxy.caFile, "setup")
	fillNewSetup(t, cli, "fresh-beginner", "beginner", "A beginner inventory tracker", "Add and list stock items", "", false)
	cli.answer(t, "Your answer (skip/back/resume, or :recommend): ", "I need a private local inventory list.")
	cli.waitFor(t, "Your answer (skip/back/resume, or :recommend): ")
	cli.answer(t, "Your answer (skip/back/resume, or :recommend): ", ":recommend")
	output := cli.finish(t)
	if !strings.Contains(output, "Typed recommendation:\nA synthetic review is ready.") || !strings.Contains(output, "Interview and recommendation saved privately.") {
		t.Fatalf("fresh beginner journey did not complete with a typed recommendation:\n%s", output)
	}
}

func TestExistingJourney(t *testing.T) {
	h := newHarness(t, nil)
	root := filepath.Join(t.TempDir(), "home")
	first := startCLI(t, h.binary, root, h.proxy.addr, h.proxy.caFile, "setup")
	fillNewSetup(t, first, "resume-existing", "developer", "A developer portfolio", "Add a testable projects page", "", false)
	first.answer(t, "Your answer (skip/back/resume, or :recommend): ", "Use a static site and keep deployment manual.")
	first.waitFor(t, "Your answer (skip/back/resume, or :recommend): ")
	first.answer(t, "Your answer (skip/back/resume, or :recommend): ", ":recommend")
	if output := first.finish(t); !strings.Contains(output, "Interview and recommendation saved privately.") {
		t.Fatalf("initial developer journey did not save: %s", output)
	}

	resume := startCLI(t, h.binary, root, h.proxy.addr, h.proxy.caFile, "interview")
	resume.answer(t, "Private session name (1-48 letters, numbers, dash or underscore): ", "resume-existing")
	resume.answer(t, "Choose new interview or resume an existing interview [new/resume]: ", "resume")
	resume.answer(t, "Your answer (skip/back/resume, or :recommend): ", ":recommend")
	output := resume.finish(t)
	if !strings.Contains(output, "Typed recommendation:\nA synthetic review is ready.") || !strings.Contains(output, "Interview and recommendation saved privately.") {
		t.Fatalf("existing developer journey did not resume and recommend:\n%s", output)
	}
}

func TestInterruptedJourney(t *testing.T) {
	dropped := false
	h := newHarness(t, func(resp *http.Response, req *http.Request) error {
		if !dropped && req != nil && req.Method == http.MethodPost && req.URL.Path == "/api/ask" && resp.StatusCode == http.StatusOK {
			dropped = true
			return errDropFixtureResponse
		}
		return nil
	})
	root := filepath.Join(t.TempDir(), "home")
	first := startCLI(t, h.binary, root, h.proxy.addr, h.proxy.caFile, "setup")
	first.answer(t, "Private session name (1-48 letters, numbers, dash or underscore): ", "interrupted-network")
	first.answer(t, "Choose new interview or resume an existing interview [new/resume]: ", "new")
	first.answer(t, "Experience level [beginner/developer]: ", "beginner")
	first.answer(t, "What are you building? ", "A synthetic interrupted project")
	first.answer(t, "What is the first useful part you want to make? ", "Save one local record")
	first.answer(t, "Optional reviewed brief file path (press Enter to skip): ", "")
	first.answer(t, "Send these details to the shared chat service and start setup? [yes/no]: ", "yes")
	first.answer(t, "Your answer (skip/back/resume, or :recommend): ", "Keep this first request so I can recover.")
	firstOutput := first.finishExpectFailure(t)
	if !strings.Contains(firstOutput, "outcome is unknown") {
		t.Fatalf("lost ask response was not reported as ambiguous:\n%s", firstOutput)
	}
	conversationPath := filepath.Join(root, "state", "interview-interrupted-network.json")
	stateBytes, err := os.ReadFile(conversationPath)
	if err != nil {
		t.Fatalf("read synthetic private recovery state: %v", err)
	}
	var envelope struct {
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(stateBytes, &envelope); err != nil {
		t.Fatalf("decode private recovery envelope: %v", err)
	}
	var pending struct {
		Token            string          `json:"token"`
		PendingRequestID string          `json:"pending_request_id"`
		PendingBody      json.RawMessage `json:"pending_body"`
	}
	if err := json.Unmarshal(envelope.Payload, &pending); err != nil || pending.Token == "" || pending.PendingRequestID == "" || len(pending.PendingBody) == 0 {
		t.Fatalf("ambiguous ask identity was not persisted privately: decode=%v request=%q", err, pending.PendingRequestID)
	}
	var initialAsk requestRecord
	for _, record := range h.proxy.requestSnapshot() {
		if record.Method == http.MethodPost && record.Path == "/api/ask" {
			initialAsk = record
			break
		}
	}
	if initialAsk.Token != pending.Token || !bytes.Equal(initialAsk.Body, pending.PendingBody) {
		t.Fatal("persisted recovery token/body did not match the request whose response was lost")
	}
	var requestIdentity struct {
		RequestID string `json:"requestId"`
	}
	if err := json.Unmarshal(pending.PendingBody, &requestIdentity); err != nil || requestIdentity.RequestID != pending.PendingRequestID {
		t.Fatalf("persisted recovery request ID did not match its exact body: id=%q err=%v", pending.PendingRequestID, err)
	}
	resp, body := fixtureRequest(t, h, http.MethodPost, "/api/ask", pending.Token, json.RawMessage(initialAsk.Body))
	if resp.StatusCode != http.StatusOK || !bytes.Contains(body, []byte(`"mode":"llm"`)) {
		t.Fatalf("replay of the exact pending network request was not idempotent: status=%d body=%s", resp.StatusCode, body)
	}

	resume := startCLI(t, h.binary, root, h.proxy.addr, h.proxy.caFile, "interview")
	resume.answer(t, "Private session name (1-48 letters, numbers, dash or underscore): ", "interrupted-network")
	resume.answer(t, "Choose new interview or resume an existing interview [new/resume]: ", "resume")
	resume.answer(t, "Your answer (skip/back/resume, or :recommend): ", ":recommend")
	output := resume.finish(t)
	if !strings.Contains(output, "Typed recommendation:\nA synthetic review is ready.") {
		t.Fatalf("interrupted setup could not recover across CLI/server boundary:\n%s", output)
	}
}

func TestBriefImportJourney(t *testing.T) {
	h := newHarness(t, nil)
	root := filepath.Join(t.TempDir(), "home")
	briefPath := filepath.Join(t.TempDir(), "reviewed-brief.json")
	brief := `{"schema_version":1,"project_summary":"A reviewed learner portfolio","desired_slice":"List three projects","experience":"developer","constraints":["Keep hosting manual"],"known_stack":[],"project_kind":"web","existing_project":false}`
	if err := os.WriteFile(briefPath, []byte(brief), 0600); err != nil {
		t.Fatalf("write synthetic reviewed brief: %v", err)
	}
	cli := startCLI(t, h.binary, root, h.proxy.addr, h.proxy.caFile, "setup")
	fillNewSetup(t, cli, "brief-import", "developer", "The interview draft is reused", "Review the portfolio slice", briefPath, true)
	cli.answer(t, "Your answer (skip/back/resume, or :recommend): ", "Focus the portfolio on my existing work.")
	cli.waitFor(t, "Your answer (skip/back/resume, or :recommend): ")
	cli.answer(t, "Your answer (skip/back/resume, or :recommend): ", ":recommend")
	output := cli.finish(t)
	for _, want := range []string{"Typed ProjectBrief preview:", "A reviewed learner portfolio", "Typed recommendation:\nA synthetic review is ready."} {
		if !strings.Contains(output, want) {
			t.Fatalf("brief import journey omitted %q:\n%s", want, output)
		}
	}

	textPath := filepath.Join(t.TempDir(), "learner-notes.txt")
	if err := os.WriteFile(textPath, []byte("A small project to track garden plants."), 0600); err != nil {
		t.Fatalf("write synthetic learner text export: %v", err)
	}
	textCLI := startCLI(t, h.binary, filepath.Join(t.TempDir(), "text-home"), h.proxy.addr, h.proxy.caFile, "setup")
	fillNewSetup(t, textCLI, "text-brief", "beginner", "A garden tracker", "Add a plant record", textPath, true)
	textCLI.answer(t, "Your answer (skip/back/resume, or :recommend): ", "Keep the garden list local.")
	textCLI.answer(t, "Your answer (skip/back/resume, or :recommend): ", ":recommend")
	textOutput := textCLI.finish(t)
	for _, want := range []string{"Learner text preview (untrusted context; not verified structured facts):", "A small project to track garden plants.", "Typed recommendation:\nA synthetic review is ready."} {
		if !strings.Contains(textOutput, want) {
			t.Fatalf("text brief import journey omitted %q:\n%s", want, textOutput)
		}
	}
}

func TestFeatureMismatchAndDisabledSetupAreActionable(t *testing.T) {
	h := newHarness(t, nil)
	for _, test := range []struct {
		name     string
		mutate   func(map[string]any)
		wantText string
	}{
		{
			name: "catalog mismatch",
			mutate: func(capabilities map[string]any) {
				capabilities["catalogVersion"] = "stale-catalog"
				capabilities["catalogDigest"] = strings.Repeat("0", 64)
			},
			wantText: "different setup catalog; update Anza before continuing",
		},
		{
			name: "feature disabled",
			mutate: func(capabilities map[string]any) {
				capabilities["enabled"] = false
				capabilities["reason"] = "setup_unavailable"
			},
			wantText: "setup is unavailable; continue with the local interview",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			proxy := startChatProxyWith(t, h.fixture.ready.BaseURL, h.fixture.root, func(resp *http.Response, req *http.Request) error {
				if req != nil && (req.Method != http.MethodGet || req.URL.Path != "/api/setup/capabilities") {
					return nil
				}
				data, err := io.ReadAll(resp.Body)
				if err != nil {
					return err
				}
				_ = resp.Body.Close()
				var capabilities map[string]any
				if err := json.Unmarshal(data, &capabilities); err != nil {
					return err
				}
				if _, ok := capabilities["catalogVersion"]; !ok {
					return nil
				}
				test.mutate(capabilities)
				encoded, err := json.Marshal(capabilities)
				if err != nil {
					return err
				}
				resp.Body = io.NopCloser(bytes.NewReader(encoded))
				resp.ContentLength = int64(len(encoded))
				resp.Header.Set("Content-Length", fmt.Sprint(len(encoded)))
				return nil
			})
			root := filepath.Join(t.TempDir(), "home")
			cli := startCLI(t, h.binary, root, proxy.addr, proxy.caFile, "setup")
			fillNewSetup(t, cli, "actionable-error", "beginner", "A synthetic mismatch check", "List one record", "", false)
			output := cli.finishExpectFailure(t)
			if !strings.Contains(output, test.wantText) {
				t.Fatalf("setup error was not actionable; wanted %q:\n%s", test.wantText, output)
			}
			for _, route := range proxy.routeSnapshot() {
				if route == "POST /api/setup/session" {
					t.Fatalf("Anza created a setup session despite capability error; routes=%v", proxy.routeSnapshot())
				}
			}
		})
	}
}

func TestSharedChatContract(t *testing.T) {
	h := newHarness(t, nil)
	resp, body := fixtureRequest(t, h, http.MethodGet, "/api/setup/capabilities", "", nil)
	if resp.StatusCode != http.StatusOK || !bytes.Contains(body, []byte(`"enabled":true`)) {
		t.Fatalf("setup capabilities status=%d body=%s", resp.StatusCode, body)
	}
	const setupToken = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	startBody := map[string]any{"protocolVersion": 1, "requestId": "contract-session-01", "catalogVersion": h.fixture.ready.CatalogVersion, "consentVersion": "acceptance-consent-1"}
	resp, body = fixtureRequest(t, h, http.MethodPost, "/api/setup/session", setupToken, startBody)
	if resp.StatusCode != http.StatusCreated || !bytes.Contains(body, []byte(`"purpose":"setup"`)) {
		t.Fatalf("setup session purpose status=%d body=%s", resp.StatusCode, body)
	}
	for _, route := range []string{"/api/brief", "/api/booking", "/api/conversation/title"} {
		resp, body = fixtureRequest(t, h, http.MethodPost, route, setupToken, map[string]any{})
		if resp.StatusCode != http.StatusConflict || !bytes.Contains(body, []byte("setup conversations cannot use this route")) {
			t.Fatalf("setup-purpose guard for %s status=%d body=%s", route, resp.StatusCode, body)
		}
	}

	legacyToken := "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	resp, body = fixtureRequest(t, h, http.MethodPost, "/api/ask", legacyToken, map[string]any{"requestId": "legacy-ask-contract-01", "message": "I am using the existing website intake."})
	if resp.StatusCode != http.StatusOK || !bytes.Contains(body, []byte(`"mode":"llm"`)) {
		t.Fatalf("legacy /api/ask changed under setup: status=%d body=%s", resp.StatusCode, body)
	}
	for _, route := range []string{"/api/brief", "/api/booking", "/api/conversation/title"} {
		resp, body = fixtureRequest(t, h, http.MethodPost, route, legacyToken, map[string]any{})
		if resp.StatusCode == http.StatusConflict || bytes.Contains(body, []byte("setup conversations cannot use this route")) {
			t.Fatalf("legacy intake route %s hit setup-only guard: status=%d body=%s", route, resp.StatusCode, body)
		}
	}

	askBody := map[string]any{"requestId": "contract-turn-01", "expectedVersion": 1, "message": "Please ask one question about the synthetic inventory project."}
	resp, body = fixtureRequest(t, h, http.MethodPost, "/api/ask", setupToken, askBody)
	if resp.StatusCode != http.StatusOK || !bytes.Contains(body, []byte("Tell me what you want to build first.")) {
		t.Fatalf("setup turn did not use shared /api/ask route: status=%d body=%s", resp.StatusCode, body)
	}
	resp, body = fixtureRequest(t, h, http.MethodGet, "/api/conversation", setupToken, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("restore setup conversation status=%d body=%s", resp.StatusCode, body)
	}
	var conversation struct {
		Version int64 `json:"version"`
	}
	if err := json.Unmarshal(body, &conversation); err != nil || conversation.Version <= 1 {
		t.Fatalf("conversation version did not advance after turn: version=%d err=%v body=%s", conversation.Version, err, body)
	}
	recommendationBody := map[string]any{"requestId": "contract-recommend-01", "expectedVersion": conversation.Version, "catalogVersion": h.fixture.ready.CatalogVersion}
	resp, body = fixtureRequest(t, h, http.MethodPost, "/api/setup/recommendation", setupToken, recommendationBody)
	if resp.StatusCode != http.StatusOK || !bytes.Contains(body, []byte(`"summary":"A synthetic review is ready."`)) {
		t.Fatalf("shared router did not return a persisted typed recommendation: status=%d body=%s", resp.StatusCode, body)
	}
	if !bytes.Contains(body, []byte(`"catalogDigest":"`+h.fixture.ready.CatalogDigest+`"`)) {
		t.Fatalf("typed recommendation omitted reviewed catalog identity: %s", body)
	}

	cliRoot := filepath.Join(t.TempDir(), "home")
	proxy := h.proxy
	failIfLegacyProseBecomesPlan(t, h.binary, cliRoot, proxy.addr, proxy.caFile)
}

type socketlessStateStore struct {
	mu     sync.Mutex
	values map[string][]byte
}

func (s *socketlessStateStore) Save(key string, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.values == nil {
		s.values = map[string][]byte{}
	}
	s.values[key] = encoded
	return nil
}

func (s *socketlessStateStore) Load(key string, dst any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	encoded, ok := s.values[key]
	if !ok {
		return os.ErrNotExist
	}
	return json.Unmarshal(encoded, dst)
}

func TestSharedChatProtocolContract(t *testing.T) {
	source := requireChatSource(t)
	t.Logf("reviewed Chat source: commit=%s tree=%s", chatFixtureCommit(), chatSourceTree(t, source))
	fixture := startSocketlessChatFixture(t, source)
	cat, err := catalog.LoadBundled()
	if err != nil {
		t.Fatalf("load pinned Anza catalog: %v", err)
	}
	t.Logf("Anza catalog: version=%s digest=%s", cat.Version(), cat.Digest())
	catSnapshot := &interviewclient.CatalogSnapshot{Version: cat.Version(), Digest: cat.Digest(), RecipeIDs: map[string]bool{}, PackIDs: map[string]bool{}, ExerciseIDs: map[string]bool{"mobile-desktop-exercise": true}}
	store := &socketlessStateStore{}
	httpClient := &http.Client{Transport: fixture, Timeout: 10 * time.Second}
	client, err := interviewclient.NewClient(httpClient, store, catSnapshot)
	if err != nil {
		t.Fatalf("construct Anza interview client: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	capabilities, err := client.Capabilities(ctx)
	if os.Getenv("ANZA_SOCKETLESS_NEGATIVE_CONTROL") == "capabilities-route" {
		if !errors.Is(err, interviewclient.ErrFeatureDisabled) || capabilities.Enabled {
			t.Fatalf("broken archived capabilities route was accepted: %+v", capabilities)
		}
		t.Logf("negative control passed: the real Anza client rejected a deliberately broken capabilities route (%v)", err)
		return
	}
	if err != nil {
		t.Fatalf("read real Chat setup capabilities: %v", err)
	}
	if !capabilities.Enabled || capabilities.ProtocolVersion != 1 || capabilities.CatalogVersion != catSnapshot.Version || capabilities.CatalogDigest != catSnapshot.Digest {
		t.Fatalf("real Chat capabilities do not match the pinned Anza catalog: %+v", capabilities)
	}
	mismatchedCatalog := *catSnapshot
	mismatchedCatalog.Version += "-stale"
	mismatchClient, err := interviewclient.NewClient(httpClient, &socketlessStateStore{}, &mismatchedCatalog)
	if err != nil {
		t.Fatalf("construct Anza client with stale catalog pin: %v", err)
	}
	if _, err := mismatchClient.Capabilities(ctx); !errors.Is(err, interviewclient.ErrUnsupportedCatalog) {
		t.Fatalf("real Chat catalog mismatch was not rejected: %v", err)
	}

	brief := &domain.ProjectBrief{SchemaVersion: 1, ProjectSummary: "A private inventory tracker", DesiredSlice: "Add and list one stock item", Experience: "beginner", Constraints: []string{}, KnownStack: []string{}, ProjectKind: "general", ExistingProject: false}
	session, err := client.NewSession(ctx, "socketless-contract", interviewclient.SetupStart{ConsentVersion: "acceptance-consent-1", Brief: brief})
	if err != nil {
		t.Fatalf("start real setup conversation: %v", err)
	}
	started, err := session.Resume(ctx)
	if err != nil || started.Purpose != "setup" || started.Setup == nil || started.Version == 0 {
		t.Fatalf("setup session did not resume with the shared purpose token: version=%d purpose=%q setup=%+v err=%v", started.Version, started.Purpose, started.Setup, err)
	}
	answer, err := session.Ask(ctx, "I want a private list of garden inventory items.")
	if err != nil || answer.Mode != "llm" || !strings.Contains(answer.Answer, "Tell me what you want to build first.") {
		t.Fatalf("real Chat /api/ask route did not return the synthetic provider response: answer=%+v err=%v", answer, err)
	}

	var askRequest socketlessRequest
	var askResponse socketlessResponse
	for i := range fixture.requests {
		if fixture.requests[i].Method == http.MethodPost && fixture.requests[i].Path == "/api/ask" {
			askRequest, askResponse = fixture.requests[i], fixture.responses[i]
			break
		}
	}
	if askRequest.Method == "" || len(askRequest.Body) == 0 || len(askRequest.Header.Get("X-Conversation-Token")) != 64 {
		t.Fatal("Anza did not send /api/ask with a stable private conversation token and body")
	}
	var askIdentity struct {
		RequestID string `json:"requestId"`
	}
	if err := json.Unmarshal(askRequest.Body, &askIdentity); err != nil || askIdentity.RequestID == "" {
		t.Fatalf("Anza ask request omitted its replay identity: id=%q err=%v", askIdentity.RequestID, err)
	}
	bodyDigest := sha256.Sum256(askRequest.Body)
	tokenDigest := sha256.Sum256([]byte(askRequest.Header.Get("X-Conversation-Token")))
	requestIDDigest := sha256.Sum256([]byte(askIdentity.RequestID))
	afterAsk, err := session.Resume(ctx)
	if err != nil {
		t.Fatalf("read setup version after Anza completed /api/ask: %v", err)
	}
	for _, request := range fixture.requests {
		if request.Header.Get("X-Conversation-Token") != "" && request.Header.Get("X-Conversation-Token") != askRequest.Header.Get("X-Conversation-Token") {
			t.Fatal("setup token changed between capabilities, session, ask, and recovery requests")
		}
	}
	replayReq, err := http.NewRequestWithContext(ctx, http.MethodPost, interviewclient.DefaultBaseURL+askRequest.Path, bytes.NewReader(askRequest.Body))
	if err != nil {
		t.Fatalf("create exact replay request: %v", err)
	}
	replayReq.Header = askRequest.Header.Clone()
	replay, err := httpClient.Do(replayReq)
	if err != nil {
		t.Fatalf("replay exact /api/ask request through real router: %v", err)
	}
	replayBody, readErr := io.ReadAll(replay.Body)
	_ = replay.Body.Close()
	if readErr != nil || replay.StatusCode != askResponse.Status || !bytes.Equal(replayBody, askResponse.Body) {
		var replayError struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(replayBody, &replayError)
		t.Errorf("identical /api/ask replay did not return the cached response: status=%d code=%q message=%q request_id_sha256=%x body_sha256=%x token_sha256=%x err=%v", replay.StatusCode, replayError.Code, replayError.Message, requestIDDigest, bodyDigest, tokenDigest, readErr)
	}
	resumed, err := session.Resume(ctx)
	if err != nil || resumed.Version != afterAsk.Version {
		t.Errorf("replay changed the server conversation version: before=%d after=%d err=%v", afterAsk.Version, resumed.Version, err)
	}

	updated, err := session.UpdateContext(ctx, nil, nil, &domain.MachineFacts{OS: "darwin", Arch: "arm64", OSVersion: "synthetic", ShellKind: "zsh", Capabilities: map[string]domain.Capability{}})
	if err != nil || updated.Version <= resumed.Version {
		t.Fatalf("real setup context route failed: version=%d err=%v", updated.Version, err)
	}
	recommendation, err := session.Recommend(ctx)
	if err != nil || recommendation.Recommendation.Summary != "A synthetic review is ready." || recommendation.CatalogDigest != catSnapshot.Digest {
		t.Fatalf("real setup recommendation route failed typed catalog validation: response=%+v err=%v", recommendation, err)
	}
	if !strings.Contains(string(askResponse.Body), `"mode":"llm"`) {
		t.Fatalf("shared /api/ask envelope changed: %s", askResponse.Body)
	}

	for _, route := range []string{"/api/brief", "/api/booking", "/api/conversation/title"} {
		response, err := rawSocketlessRequest(ctx, httpClient, http.MethodPost, interviewclient.DefaultBaseURL+route, askRequest.Header.Get("X-Conversation-Token"), []byte(`{}`))
		if err != nil {
			t.Fatalf("check setup-purpose guard for %s: %v", route, err)
		}
		if response.Status != http.StatusConflict || !strings.Contains(string(response.Body), "setup conversations cannot use this route") {
			t.Fatalf("setup-purpose guard for %s changed: status=%d body=%s", route, response.Status, response.Body)
		}
	}

	legacyToken := strings.Repeat("d", 64)
	legacyAsk, err := rawSocketlessRequest(ctx, httpClient, http.MethodPost, interviewclient.DefaultBaseURL+"/api/ask", legacyToken, []byte(`{"requestId":"legacy-socketless-001","message":"Keep the existing website intake available."}`))
	if err != nil || legacyAsk.Status != http.StatusOK || !bytes.Contains(legacyAsk.Body, []byte(`"mode":"llm"`)) {
		t.Fatalf("existing website /api/ask route failed under the real shared router: status=%d body=%s err=%v", legacyAsk.Status, legacyAsk.Body, err)
	}
	for _, route := range []string{"/api/brief", "/api/booking", "/api/conversation/title"} {
		response, err := rawSocketlessRequest(ctx, httpClient, http.MethodPost, interviewclient.DefaultBaseURL+route, legacyToken, []byte(`{}`))
		if err != nil {
			t.Fatalf("check legacy route %s: %v", route, err)
		}
		if response.Status == http.StatusConflict || strings.Contains(string(response.Body), "setup conversations cannot use this route") {
			t.Fatalf("existing website route %s was incorrectly treated as setup-only: status=%d body=%s", route, response.Status, response.Body)
		}
	}

	if err := session.Delete(ctx); err != nil {
		t.Fatalf("delete setup conversation through shared route: %v", err)
	}
	if _, err := session.Resume(ctx); !errors.Is(err, interviewclient.ErrExpiredOrMissing) {
		t.Errorf("Anza accepted its deleted local setup session: %v", err)
	}
	deleted, err := rawSocketlessRequest(ctx, httpClient, http.MethodGet, interviewclient.DefaultBaseURL+"/api/conversation", askRequest.Header.Get("X-Conversation-Token"), nil)
	if err != nil {
		t.Fatalf("check deleted setup transcript: %v", err)
	}
	var deletedConversation struct {
		Version uint64          `json:"version"`
		Purpose string          `json:"purpose"`
		Setup   json.RawMessage `json:"setup"`
	}
	if deleted.Status != http.StatusNotFound && (deleted.Status != http.StatusOK || json.Unmarshal(deleted.Body, &deletedConversation) != nil || deletedConversation.Version != 0 || deletedConversation.Purpose != "" || len(deletedConversation.Setup) != 0) {
		t.Errorf("deleted setup transcript remained recoverable: status=%d body=%s", deleted.Status, deleted.Body)
	}
}

func rawSocketlessRequest(ctx context.Context, client *http.Client, method, target, token string, body []byte) (*socketlessResponse, error) {
	var input io.Reader
	if body != nil {
		input = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, input)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("X-Conversation-Token", token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	return &socketlessResponse{Status: response.StatusCode, Header: response.Header.Clone(), Body: data}, nil
}

func fillNewSetup(t *testing.T, cli *cliProcess, name, experience, project, slice, briefPath string, uploadBrief bool) {
	t.Helper()
	cli.answer(t, "Private session name (1-48 letters, numbers, dash or underscore): ", name)
	cli.answer(t, "Choose new interview or resume an existing interview [new/resume]: ", "new")
	cli.answer(t, "Experience level [beginner/developer]: ", experience)
	cli.answer(t, "What are you building? ", project)
	cli.answer(t, "What is the first useful part you want to make? ", slice)
	cli.answer(t, "Optional reviewed brief file path (press Enter to skip): ", briefPath)
	if uploadBrief {
		cli.answer(t, "Upload this reviewed brief with your setup? [yes/no]: ", "yes")
	}
	cli.answer(t, "Send these details to the shared chat service and start setup? [yes/no]: ", "yes")
}

func fixtureRequest(t *testing.T, h *harness, method, path, token string, payload any) (*http.Response, []byte) {
	t.Helper()
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(mustURL(t, h.proxy.addr)), TLSClientConfig: &tls.Config{RootCAs: proxyRoots(t, h.proxy.caFile)}}, Timeout: 4 * time.Second}
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("encode synthetic Chat request: %v", err)
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, interviewclient.DefaultBaseURL+path, body)
	if err != nil {
		t.Fatalf("create synthetic Chat request: %v", err)
	}
	if token != "" {
		req.Header.Set("X-Conversation-Token", token)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request synthetic Chat route %s: %v", path, err)
	}
	data, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("read synthetic Chat route %s: %v", path, err)
	}
	return resp, data
}
