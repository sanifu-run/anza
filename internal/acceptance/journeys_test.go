package acceptance

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
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
	conversationPath := filepath.Join(root, "Library", "Application Support", "Anza", "State", "interview-interrupted-network.json")
	if runtime.GOOS != "darwin" {
		conversationPath = filepath.Join(root, "state", "anza", "interview-interrupted-network.json")
	}
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
	resp, body = fixtureRequest(t, h, http.MethodPost, "/api/ask", legacyToken, map[string]any{"requestId": "legacy-ask-01", "message": "I am using the existing website intake."})
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
	req, err := http.NewRequest(method, "https://sanifu.run"+path, body)
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
