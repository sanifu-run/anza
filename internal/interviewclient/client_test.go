package interviewclient

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/sanifu-run/anza/internal/domain"
	"github.com/sanifu-run/anza/internal/state"
)

func testClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server, *state.Store, *CatalogSnapshot) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	store, err := state.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	catalog := &CatalogSnapshot{Version: "2026.09", Digest: strings.Repeat("a", 64), RecipeIDs: map[string]bool{"editor": true}, PackIDs: map[string]bool{"base": true}, ExerciseIDs: map[string]bool{"starter": true}}
	client, err := newTestClient(server.URL, server.Client(), store, catalog)
	if err != nil {
		t.Fatal(err)
	}
	return client, server, store, catalog
}

func writeSetupConversation(w http.ResponseWriter, version int) {
	_, _ = io.WriteString(w, `{"id":"conversation","version":`+strconv.Itoa(version)+`,"turns":[],"purpose":"setup","setup":{"catalogVersion":"2026.09","catalogDigest":"`+strings.Repeat("a", 64)+`"}}`)
}

func TestClientChatContract(t *testing.T) {
	client, _, _, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/setup/session" {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["protocolVersion"] != float64(1) || body["catalogVersion"] != "2026.09" {
				t.Errorf("setup wire envelope mismatch: %#v", body)
			}
			if r.Header.Get("X-Conversation-Token") == "" {
				t.Error("session start missing token header")
			}
			token := r.Header.Get("X-Conversation-Token")
			if decoded, err := hex.DecodeString(token); err != nil || len(decoded) != 32 || token != strings.ToLower(token) {
				t.Errorf("invalid conversation token representation: %q", token)
			}
			writeSetupConversation(w, 1)
			return
		}
		if r.URL.Path == "/api/conversation" {
			writeSetupConversation(w, 1)
			return
		}
		if r.URL.Path != "/api/ask" || r.Method != http.MethodPost {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("X-Conversation-Token") == "" {
			t.Error("private request missing token header")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if _, ok := body["requestId"]; !ok {
			t.Errorf("requestId missing from camelCase request: %#v", body)
		}
		if _, ok := body["request_id"]; ok {
			t.Error("requestId unexpectedly snake_case")
		}
		_, _ = io.WriteString(w, `{"mode":"llm","answer":"Synthetic answer","citations":[]}`)
	})
	session, err := client.NewSession(context.Background(), "test", SetupStart{ConsentVersion: "consent-1"})
	if err != nil {
		t.Fatal(err)
	}
	answer, err := session.Ask(context.Background(), "synthetic question")
	if err != nil {
		t.Fatal(err)
	}
	if answer.Answer != "Synthetic answer" || answer.Mode != "llm" {
		t.Fatalf("unexpected legacy answer: %#v", answer)
	}
}

func TestResumeRequestIdentity(t *testing.T) {
	var requestIDs []string
	client, _, _, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/setup/session" {
			writeSetupConversation(w, 1)
			return
		}
		if r.URL.Path == "/api/conversation" {
			writeSetupConversation(w, 1)
			return
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		requestIDs = append(requestIDs, req["requestId"].(string))
		_, _ = io.WriteString(w, `{"mode":"llm","answer":"ok","citations":[]}`)
	})
	s, err := client.NewSession(context.Background(), "resume", SetupStart{ConsentVersion: "consent-1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Ask(context.Background(), "same question"); err != nil {
		t.Fatal(err)
	}
	requestID := s.PendingRequestID()
	resumed, err := ResumeSession(client, "resume")
	if err != nil {
		t.Fatal(err)
	}
	if resumed.PendingRequestID() != requestID {
		t.Fatalf("resume changed request identity %q to %q", requestID, resumed.PendingRequestID())
	}
	if _, err := resumed.Resume(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(requestIDs) != 1 || requestIDs[0] != requestID {
		t.Fatalf("resume should not repeat or replace request identity: %v", requestIDs)
	}
	if _, err := resumed.RetryPendingAsk(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(requestIDs) != 2 || requestIDs[1] != requestID {
		t.Fatalf("explicit replay changed request identity: %v", requestIDs)
	}
}

func TestClientSetupCapabilities(t *testing.T) {
	client, _, _, catalog := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/setup/capabilities" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"protocolVersion":1,"enabled":true,"catalogVersion":"2026.09","catalogDigest":"`+strings.Repeat("a", 64)+`","limits":{"maxBodyBytes":32768}}`)
	})
	caps, err := client.Capabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !caps.Enabled || caps.CatalogDigest != catalog.Digest {
		t.Fatalf("unexpected capabilities: %#v", caps)
	}
}

func TestUnsupportedCatalogRejected(t *testing.T) {
	client, _, _, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"protocolVersion":1,"enabled":true,"catalogVersion":"2026.10","catalogDigest":"`+strings.Repeat("b", 64)+`","limits":{}}`)
	})
	if _, err := client.Capabilities(context.Background()); !errors.Is(err, ErrUnsupportedCatalog) {
		t.Fatalf("want unsupported catalog error, got %v", err)
	}
}

func TestNoSecretUpload(t *testing.T) {
	const secret = "synthetic-agent-secret-never-upload"
	client, _, _, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), secret) {
			t.Fatal("agent credential was uploaded")
		}
		if strings.Contains(r.URL.String(), secret) {
			t.Fatal("secret in URL or token header")
		}
		if strings.Contains(string(body), "agent_credentials") || strings.Contains(string(body), "api_key") {
			t.Fatal("credential field uploaded")
		}
		if r.URL.Path == "/api/setup/session" {
			writeSetupConversation(w, 1)
			return
		}
		if r.URL.Path == "/api/conversation" {
			writeSetupConversation(w, 1)
			return
		}
		_, _ = io.WriteString(w, `{"mode":"llm","answer":"ok","citations":[]}`)
	})
	unsafeBrief := "reviewed sample api_key=" + secret
	if _, err := client.NewSession(context.Background(), "blocked", SetupStart{ConsentVersion: "consent-1", ApprovedBriefText: &unsafeBrief}); err == nil {
		t.Fatal("credential-like reviewed text was accepted")
	}
	session, err := client.NewSession(context.Background(), "privacy", SetupStart{ConsentVersion: "consent-1", MachineFacts: &domain.MachineFacts{OS: "darwin", Arch: "arm64", OSVersion: "14", ShellKind: "zsh", Capabilities: map[string]domain.Capability{}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Ask(context.Background(), "synthetic only"); err != nil {
		t.Fatal(err)
	}
}

func TestTimeoutIsAmbiguousAndNotRetried(t *testing.T) {
	calls := 0
	client, _, _, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/setup/session" {
			writeSetupConversation(w, 1)
			return
		}
		if r.URL.Path == "/api/ask" {
			calls++
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Fatal(err)
			}
			_ = conn.Close()
			return
		}
		writeSetupConversation(w, 1)
	})
	s, err := client.NewSession(context.Background(), "timeout", SetupStart{ConsentVersion: "consent-1"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Ask(context.Background(), "question")
	if !errors.Is(err, ErrAmbiguousMutation) {
		t.Fatalf("want ambiguous mutation, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("ask was automatically retried %d times", calls)
	}
}

func TestContextUpdatesExpectedVersion(t *testing.T) {
	client, _, _, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/setup/session":
			writeSetupConversation(w, 1)
		case "/api/setup/context":
			var request map[string]any
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			if request["requestId"] == nil || request["expectedVersion"] != float64(1) {
				t.Errorf("bad context envelope: %#v", request)
			}
			facts, ok := request["machineFacts"].(map[string]any)
			if !ok || facts["os_version"] != "14" {
				t.Errorf("machine facts must use domain snake_case: %#v", request["machineFacts"])
			}
			writeSetupConversation(w, 2)
		default:
			t.Errorf("unexpected request path %q", r.URL.Path)
		}
	})
	facts := &domain.MachineFacts{OS: "darwin", Arch: "arm64", OSVersion: "14", ShellKind: "zsh", Capabilities: map[string]domain.Capability{}}
	session, err := client.NewSession(context.Background(), "context", SetupStart{ConsentVersion: "consent-1"})
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := session.UpdateContext(context.Background(), nil, nil, facts)
	if err != nil {
		t.Fatal(err)
	}
	if conversation.Version != 2 {
		t.Fatalf("context version = %d, want 2", conversation.Version)
	}
}

func TestTypedRecommendationCatalogValidation(t *testing.T) {
	client, _, _, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/setup/session" {
			writeSetupConversation(w, 1)
			return
		}
		if r.URL.Path != "/api/setup/recommendation" {
			t.Errorf("unexpected path %q", r.URL.Path)
			return
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request["requestId"] == nil || request["expectedVersion"] != float64(1) || request["catalogVersion"] != "2026.09" {
			t.Errorf("bad recommendation envelope: %#v", request)
		}
		_, _ = io.WriteString(w, `{"version":2,"recommendation":{"schema_version":1,"catalog_version":"2026.09","summary":"Synthetic selection","selected_recipe_ids":["editor"],"selected_pack_ids":["base"],"selected_exercise_id":"starter","reasons":{"editor":"fits the synthetic goal"},"unresolved_questions":[],"manual_steps":[],"readiness_constraints":[]},"sourceVersion":1,"contextDigest":"`+strings.Repeat("b", 64)+`","catalogDigest":"`+strings.Repeat("a", 64)+`"}`)
	})
	session, err := client.NewSession(context.Background(), "recommendation", SetupStart{ConsentVersion: "consent-1"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := session.Recommend(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Recommendation.SelectedRecipeIDs[0] != "editor" {
		t.Fatalf("unexpected typed recommendation: %#v", got.Recommendation)
	}
}

func TestProductionOriginPinned(t *testing.T) {
	store, err := state.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	catalog := &CatalogSnapshot{Version: "2026.09", Digest: strings.Repeat("a", 64)}
	client, err := NewClient(nil, store, catalog)
	if err != nil {
		t.Fatal(err)
	}
	// The website is a static frontend; the shared Chat deployment record pins
	// this existing API origin. Never send recovery tokens to the website.
	const deployedChatOrigin = "https://vg78ulztb1.execute-api.us-west-2.amazonaws.com"
	if client.base.String() != deployedChatOrigin {
		t.Fatalf("production origin = %q, want deployed Chat %q", client.base, deployedChatOrigin)
	}
	if _, err := newClient("https://sanifu.run", nil, store, catalog, false); err == nil {
		t.Fatal("website origin accepted as the production Chat API")
	}
	if _, err := newTestClient("https://example.invalid", nil, store, catalog); err == nil {
		t.Fatal("non-loopback fixture origin accepted")
	}
}

func TestAskDoesNotTreatRecommendationAsAnswer(t *testing.T) {
	client, _, _, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/setup/session" {
			writeSetupConversation(w, 1)
			return
		}
		_, _ = io.WriteString(w, `{"version":2,"recommendation":{"schema_version":1}}`)
	})
	session, err := client.NewSession(context.Background(), "typed", SetupStart{ConsentVersion: "consent-1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Ask(context.Background(), "synthetic question"); err == nil {
		t.Fatal("typed recommendation envelope was accepted as a legacy answer")
	}
}
