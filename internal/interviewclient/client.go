// Package interviewclient speaks the additive setup API on the shared Sanifu
// chat service. It never sends participant agent credentials or repository data.
package interviewclient

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/sanifu-run/anza/internal/domain"
)

const DefaultBaseURL = "https://sanifu.run"
const protocolVersion = 1
const maxResponseBytes = 64 << 10

var credentialAssignment = regexp.MustCompile(`(?i)(api[_-]?key|access[_-]?token|secret|password|authorization)\s*[:=]\s*[^\s,;]+`)
var privateKeyMarker = regexp.MustCompile(`(?i)-----BEGIN (RSA |EC |OPENSSH )?PRIVATE KEY-----|\b(?:sk-[A-Za-z0-9]{12,}|gh[pousr]_[A-Za-z0-9]{12,}|AKIA[A-Z0-9]{16})\b`)

var (
	ErrFeatureDisabled    = errors.New("setup is unavailable; continue with the local interview")
	ErrUnsupportedCatalog = errors.New("the shared chat service has a different setup catalog; update Anza before continuing")
	ErrExpiredOrMissing   = errors.New("this conversation expired or is missing; start a new interview")
	ErrAmbiguousMutation  = errors.New("the request outcome is unknown; conversation state was checked and generation was not repeated")
)

type StateStore interface {
	Save(string, any) error
	Load(string, any) error
}
type CatalogSnapshot struct {
	Version     string
	Digest      string
	RecipeIDs   map[string]bool
	PackIDs     map[string]bool
	ExerciseIDs map[string]bool
}
type Client struct {
	base    *url.URL
	http    *http.Client
	store   StateStore
	catalog CatalogSnapshot
}
type SetupStart struct {
	ConsentVersion    string               `json:"consentVersion"`
	Brief             *domain.ProjectBrief `json:"brief,omitempty"`
	ApprovedBriefText *string              `json:"approvedBriefText,omitempty"`
	MachineFacts      *domain.MachineFacts `json:"machineFacts,omitempty"`
}
type Capabilities struct {
	ProtocolVersion int                        `json:"protocolVersion"`
	Enabled         bool                       `json:"enabled"`
	Reason          string                     `json:"reason,omitempty"`
	CatalogVersion  string                     `json:"catalogVersion,omitempty"`
	CatalogDigest   string                     `json:"catalogDigest,omitempty"`
	Limits          map[string]json.RawMessage `json:"limits,omitempty"`
}
type AskResponse struct {
	Mode      string     `json:"mode"`
	Answer    string     `json:"answer"`
	Citations []Citation `json:"citations"`
}
type Citation struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}
type Conversation struct {
	ID        string             `json:"id"`
	Version   uint64             `json:"version"`
	Title     string             `json:"title,omitempty"`
	CreatedAt string             `json:"createdAt,omitempty"`
	UpdatedAt string             `json:"updatedAt,omitempty"`
	Brief     json.RawMessage    `json:"brief,omitempty"`
	Booking   json.RawMessage    `json:"booking,omitempty"`
	Purpose   string             `json:"purpose,omitempty"`
	Turns     []json.RawMessage  `json:"turns"`
	Setup     *ConversationSetup `json:"setup,omitempty"`
}
type ConversationSetup struct {
	CatalogVersion              string                 `json:"catalogVersion"`
	CatalogDigest               string                 `json:"catalogDigest"`
	Recommendation              *domain.Recommendation `json:"recommendation,omitempty"`
	RecommendationSourceVersion uint64                 `json:"recommendationSourceVersion,omitempty"`
}
type RecommendationResponse struct {
	Version        uint64                `json:"version"`
	Recommendation domain.Recommendation `json:"recommendation"`
	SourceVersion  uint64                `json:"sourceVersion"`
	ContextDigest  string                `json:"contextDigest"`
	CatalogDigest  string                `json:"catalogDigest"`
}
type ErrorEnvelope struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
	RequestID string `json:"requestId"`
}

func NewClient(httpClient *http.Client, store StateStore, catalog *CatalogSnapshot) (*Client, error) {
	return newClient(DefaultBaseURL, httpClient, store, catalog, false)
}

// newTestClient is the only origin override and accepts loopback fixtures only,
// so production callers cannot redirect a recovery token to another service.
func newTestClient(baseURL string, httpClient *http.Client, store StateStore, catalog *CatalogSnapshot) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || !isLoopbackHost(u.Hostname()) {
		return nil, errors.New("test chat origin must be loopback")
	}
	return newClient(baseURL, httpClient, store, catalog, true)
}

func newClient(baseURL string, httpClient *http.Client, store StateStore, catalog *CatalogSnapshot, testOrigin bool) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (!testOrigin && (u.Scheme != "https" || baseURL != DefaultBaseURL)) || (testOrigin && !isLoopbackHost(u.Hostname())) {
		return nil, errors.New("chat base URL must be the shared HTTPS origin (loopback override is test-only)")
	}
	if u.Path != "" && u.Path != "/" {
		return nil, errors.New("chat base URL must be an origin without a path")
	}
	if store == nil || catalog == nil || catalog.Version == "" || !validDigest(catalog.Digest) {
		return nil, errors.New("private state store and pinned catalog identity are required")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 45 * time.Second}
	}
	clientCopy := *httpClient
	// Conversation tokens are scoped to the selected chat origin. Never follow a
	// redirect where request headers could leave that origin.
	clientCopy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	pinned := *catalog
	pinned.RecipeIDs = cloneSet(catalog.RecipeIDs)
	pinned.PackIDs = cloneSet(catalog.PackIDs)
	pinned.ExerciseIDs = cloneSet(catalog.ExerciseIDs)
	return &Client{base: u, http: &clientCopy, store: store, catalog: pinned}, nil
}
func cloneSet(input map[string]bool) map[string]bool {
	output := make(map[string]bool, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
func validDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && s == strings.ToLower(s)
}
func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func validStateName(s string) bool {
	if len(s) < 1 || len(s) > 48 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func (c *Client) Capabilities(ctx context.Context) (Capabilities, error) {
	var v Capabilities
	resp, err := c.request(ctx, http.MethodGet, "/api/setup/capabilities", "", nil)
	if err != nil {
		return v, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return v, ErrFeatureDisabled
	}
	if err := decodeResponse(resp, &v); err != nil {
		return v, err
	}
	if !v.Enabled {
		return v, ErrFeatureDisabled
	}
	if v.ProtocolVersion != protocolVersion || v.CatalogVersion != c.catalog.Version || v.CatalogDigest != c.catalog.Digest {
		return v, ErrUnsupportedCatalog
	}
	return v, nil
}
func (c *Client) NewSession(ctx context.Context, name string, start SetupStart) (*Session, error) {
	if !validStateName(name) {
		return nil, errors.New("session name must contain 1..48 safe characters")
	}
	if err := validateStart(start); err != nil {
		return nil, err
	}
	key := "interview-" + name
	var existing sessionState
	if err := c.store.Load(key, &existing); err == nil {
		if existing.Token != "" {
			return nil, errors.New("a local interview with this name already exists; resume or delete it first")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect existing private interview state: %w", err)
	}
	token, err := randomHex(32)
	if err != nil {
		return nil, fmt.Errorf("generate conversation token: %w", err)
	}
	s := &Session{client: c, key: key, state: sessionState{SchemaVersion: 1, Name: name, Token: token}}
	if err := c.store.Save(s.key, s.state); err != nil {
		return nil, fmt.Errorf("persist private interview state: %w", err)
	}
	if _, err := s.StartSetup(ctx, start); err != nil {
		return s, err
	}
	return s, nil
}
func ResumeSession(c *Client, name string) (*Session, error) {
	if !validStateName(name) {
		return nil, errors.New("invalid session name")
	}
	s := &Session{client: c, key: "interview-" + name}
	if err := c.store.Load(s.key, &s.state); err != nil {
		return nil, fmt.Errorf("load private interview state: %w", err)
	}
	decoded, decodeErr := hex.DecodeString(s.state.Token)
	if s.state.SchemaVersion != 1 || s.state.Name != name || decodeErr != nil || len(decoded) != 32 || strings.ToLower(s.state.Token) != s.state.Token {
		return nil, errors.New("private interview state is invalid")
	}
	return s, nil
}
func (s *Session) save() error              { return s.client.store.Save(s.key, s.state) }
func (s *Session) PendingRequestID() string { return s.state.PendingRequestID }
func (s *Session) newMutation(kind string) (string, error) {
	id, err := randomHex(16)
	if err != nil {
		return "", err
	}
	s.state.PendingRequestID = id
	s.state.PendingKind = kind
	s.state.PendingBody = nil
	s.state.PendingBaseVersion = s.state.Version
	if err := s.save(); err != nil {
		return "", err
	}
	return id, nil
}
func (s *Session) persistPendingBody(body any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	s.state.PendingBody = data
	return s.save()
}

func (s *Session) StartSetup(ctx context.Context, start SetupStart) (Conversation, error) {
	var zero Conversation
	if err := validateStart(start); err != nil {
		return zero, err
	}
	id, err := s.newMutation("setup-session")
	if err != nil {
		return zero, err
	}
	body := struct {
		ProtocolVersion   int                  `json:"protocolVersion"`
		RequestID         string               `json:"requestId"`
		CatalogVersion    string               `json:"catalogVersion"`
		ConsentVersion    string               `json:"consentVersion"`
		Brief             *domain.ProjectBrief `json:"brief,omitempty"`
		ApprovedBriefText *string              `json:"approvedBriefText,omitempty"`
		MachineFacts      *domain.MachineFacts `json:"machineFacts,omitempty"`
	}{protocolVersion, id, s.client.catalog.Version, start.ConsentVersion, start.Brief, start.ApprovedBriefText, start.MachineFacts}
	return s.mutate(ctx, http.MethodPost, "/api/setup/session", body)
}
func (s *Session) Ask(ctx context.Context, message string) (AskResponse, error) {
	var out AskResponse
	if strings.TrimSpace(message) == "" {
		return out, errors.New("question is empty")
	}
	if containsCredential(message) {
		return out, errors.New("message appears to contain a credential; remove it before sending")
	}
	id, err := s.newMutation("ask")
	if err != nil {
		return out, err
	}
	body := map[string]any{"requestId": id, "message": message}
	if s.state.Version > 0 {
		body["expectedVersion"] = s.state.Version
	}
	if err := s.persistPendingBody(body); err != nil {
		return out, err
	}
	resp, err := s.client.request(ctx, http.MethodPost, "/api/ask", s.state.Token, body)
	if err != nil {
		return out, s.recoverMutation(ctx, err)
	}
	defer resp.Body.Close()
	if err := checkStatus(resp); err != nil {
		return out, err
	}
	if err := decodeResponse(resp, &out); err != nil {
		return out, err
	}
	if out.Mode != "llm" || strings.TrimSpace(out.Answer) == "" {
		return out, errors.New("chat returned a non-legacy or incomplete ask response")
	}
	// AskResponse has no version field. Fetch the canonical shared state after
	// success so subsequent setup writes use its current expectedVersion.
	if _, err := s.Resume(ctx); err != nil {
		return out, fmt.Errorf("answer received but conversation state could not be refreshed: %w", err)
	}
	return out, nil
}

// RetryPendingAsk is an explicit replay after a user reviews the recovery state.
// It reads shared conversation state first and reuses the exact persisted body
// and requestId; it is never invoked automatically after a transport error.
func (s *Session) RetryPendingAsk(ctx context.Context) (AskResponse, error) {
	var out AskResponse
	if s.state.PendingKind != "ask" || len(s.state.PendingBody) == 0 || s.state.PendingRequestID == "" {
		return out, errors.New("there is no recoverable pending question")
	}
	if _, err := s.Resume(ctx); err != nil {
		return out, err
	}
	var request struct {
		RequestID string `json:"requestId"`
		Message   string `json:"message"`
	}
	if err := json.Unmarshal(s.state.PendingBody, &request); err != nil || request.RequestID != s.state.PendingRequestID || strings.TrimSpace(request.Message) == "" {
		return out, errors.New("pending question state is invalid")
	}
	resp, err := s.client.request(ctx, http.MethodPost, "/api/ask", s.state.Token, json.RawMessage(s.state.PendingBody))
	if err != nil {
		return out, s.recoverMutation(ctx, err)
	}
	defer resp.Body.Close()
	if err := decodeResponse(resp, &out); err != nil {
		return out, err
	}
	if out.Mode != "llm" || strings.TrimSpace(out.Answer) == "" {
		return out, errors.New("chat returned a non-legacy or incomplete ask response")
	}
	if _, err := s.Resume(ctx); err != nil {
		return out, fmt.Errorf("answer recovered but conversation state could not be refreshed: %w", err)
	}
	return out, nil
}

func (s *Session) UpdateContext(ctx context.Context, brief *domain.ProjectBrief, approvedBriefText *string, facts *domain.MachineFacts) (Conversation, error) {
	if brief == nil && approvedBriefText == nil && facts == nil {
		return Conversation{}, errors.New("context update is empty")
	}
	if brief != nil && approvedBriefText != nil {
		return Conversation{}, errors.New("provide one reviewed brief form")
	}
	if brief != nil {
		if err := validateBrief(brief); err != nil {
			return Conversation{}, err
		}
	}
	if approvedBriefText != nil && (strings.TrimSpace(*approvedBriefText) == "" || len(*approvedBriefText) > 16<<10) {
		return Conversation{}, errors.New("approved brief text must contain 1..16384 bytes")
	}
	if approvedBriefText != nil && containsCredential(*approvedBriefText) {
		return Conversation{}, errors.New("approved brief appears to contain a credential; remove it before sharing")
	}
	if facts != nil {
		if err := validateFacts(facts); err != nil {
			return Conversation{}, err
		}
	}
	id, err := s.newMutation("setup-context")
	if err != nil {
		return Conversation{}, err
	}
	body := map[string]any{"requestId": id, "expectedVersion": s.state.Version}
	if brief != nil {
		body["brief"] = brief
	}
	if approvedBriefText != nil {
		body["approvedBriefText"] = approvedBriefText
	}
	if facts != nil {
		body["machineFacts"] = facts
	}
	return s.mutate(ctx, http.MethodPost, "/api/setup/context", body)
}
func (s *Session) Recommend(ctx context.Context) (RecommendationResponse, error) {
	var out RecommendationResponse
	id, err := s.newMutation("setup-recommendation")
	if err != nil {
		return out, err
	}
	body := map[string]any{"requestId": id, "expectedVersion": s.state.Version, "catalogVersion": s.client.catalog.Version}
	if err := s.persistPendingBody(body); err != nil {
		return out, err
	}
	resp, err := s.client.request(ctx, http.MethodPost, "/api/setup/recommendation", s.state.Token, body)
	if err != nil {
		return out, s.recoverMutation(ctx, err)
	}
	defer resp.Body.Close()
	if err := checkStatus(resp); err != nil {
		return out, err
	}
	if err := decodeResponse(resp, &out); err != nil {
		return out, err
	}
	if out.Version == 0 || out.SourceVersion == 0 || !validDigest(out.ContextDigest) {
		return out, errors.New("chat returned an incomplete recommendation identity")
	}
	if out.CatalogDigest != s.client.catalog.Digest || out.Recommendation.CatalogVersion != s.client.catalog.Version {
		return out, ErrUnsupportedCatalog
	}
	if err := validateRecommendation(out.Recommendation, s.client.catalog); err != nil {
		return out, err
	}
	s.state.Version = out.Version
	s.state.PendingRequestID = ""
	s.state.PendingKind = ""
	s.state.PendingBody = nil
	if err := s.save(); err != nil {
		return out, err
	}
	return out, nil
}
func (s *Session) Resume(ctx context.Context) (Conversation, error) {
	resp, err := s.client.request(ctx, http.MethodGet, "/api/conversation", s.state.Token, nil)
	if err != nil {
		return Conversation{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return Conversation{}, ErrExpiredOrMissing
	}
	if err := checkStatus(resp); err != nil {
		return Conversation{}, err
	}
	var conv Conversation
	if err := decodeConversation(resp, &conv); err != nil {
		return conv, err
	}
	if conv.ID == "" || conv.Version == 0 {
		return conv, ErrExpiredOrMissing
	}
	if conv.Purpose != "setup" || conv.Setup == nil {
		return conv, ErrExpiredOrMissing
	}
	if conv.Setup.CatalogVersion != s.client.catalog.Version || conv.Setup.CatalogDigest != s.client.catalog.Digest {
		return conv, ErrUnsupportedCatalog
	}
	if conv.Version < s.state.Version {
		return conv, errors.New("shared conversation version moved backwards")
	}
	s.state.Version = conv.Version
	if err := s.save(); err != nil {
		return conv, err
	}
	return conv, nil
}
func (s *Session) Delete(ctx context.Context) error {
	resp, err := s.client.request(ctx, http.MethodDelete, "/api/conversation", s.state.Token, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
		return checkStatus(resp)
	}
	s.state.Token = ""
	s.state.PendingKind = ""
	s.state.PendingRequestID = ""
	s.state.PendingBody = nil
	return s.save()
}
func (s *Session) mutate(ctx context.Context, method, path string, body any) (Conversation, error) {
	if err := s.persistPendingBody(body); err != nil {
		return Conversation{}, err
	}
	resp, err := s.client.request(ctx, method, path, s.state.Token, body)
	if err != nil {
		return Conversation{}, s.recoverMutation(ctx, err)
	}
	defer resp.Body.Close()
	if err := checkStatus(resp); err != nil {
		return Conversation{}, err
	}
	var conv Conversation
	if err := decodeConversation(resp, &conv); err != nil {
		return conv, err
	}
	if conv.ID == "" || conv.Version == 0 || conv.Purpose != "setup" || conv.Setup == nil {
		return conv, errors.New("chat did not return setup conversation state")
	}
	if conv.Setup.CatalogVersion != s.client.catalog.Version || conv.Setup.CatalogDigest != s.client.catalog.Digest {
		return conv, ErrUnsupportedCatalog
	}
	s.state.Version = conv.Version
	s.state.PendingKind = ""
	s.state.PendingRequestID = ""
	s.state.PendingBody = nil
	if err := s.save(); err != nil {
		return conv, err
	}
	return conv, nil
}
func (s *Session) recoverMutation(ctx context.Context, cause error) error {
	// A status/body may be lost after a server commit. Fetching shared state is
	// the only safe recovery action; never repeat a prompt or mutation here.
	_, readErr := s.Resume(ctx)
	if readErr != nil {
		return fmt.Errorf("%w: state lookup failed (%v)", ErrAmbiguousMutation, readErr)
	}
	return fmt.Errorf("%w: %v", ErrAmbiguousMutation, cause)
}
func (c *Client) request(ctx context.Context, method, path, token string, body any) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(b)
	}
	u := *c.base
	u.Path = strings.TrimRight(c.base.Path, "/") + path
	req, err := http.NewRequestWithContext(ctx, method, u.String(), reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("X-Conversation-Token", token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.ContentLength > maxResponseBytes {
		resp.Body.Close()
		return nil, errors.New("chat response exceeds size limit")
	}
	return resp, nil
}
func decodeResponse(resp *http.Response, dst any) error {
	if err := checkStatus(resp); err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read chat response: %w", err)
	}
	if len(data) > maxResponseBytes {
		return errors.New("chat response exceeds size limit")
	}
	canonical, err := domain.CanonicalJSON(data)
	if err != nil {
		return fmt.Errorf("chat response JSON is invalid: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(canonical))
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("decode chat response: %w", err)
	}
	if dec.Decode(new(any)) != io.EOF {
		return errors.New("chat response contains trailing data")
	}
	return nil
}
func decodeConversation(resp *http.Response, dst *Conversation) error {
	if err := checkStatus(resp); err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxResponseBytes {
		return errors.New("chat response exceeds size limit")
	}
	canonical, err := domain.CanonicalJSON(data)
	if err != nil {
		return fmt.Errorf("conversation JSON is invalid: %w", err)
	}
	if err := json.Unmarshal(canonical, dst); err != nil {
		return fmt.Errorf("decode conversation: %w", err)
	}
	return nil
}
func checkStatus(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	var envelope ErrorEnvelope
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	_ = json.Unmarshal(data, &envelope)
	switch resp.StatusCode {
	case 401:
		return ErrExpiredOrMissing
	case 404:
		return ErrFeatureDisabled
	case 409:
		return fmt.Errorf("conversation changed or request conflicted; resume and review before continuing")
	case 410:
		return ErrExpiredOrMissing
	case 422:
		return fmt.Errorf("chat returned a recommendation that failed validation")
	case 429:
		return fmt.Errorf("chat is busy; resume before trying again")
	case 502, 503:
		return fmt.Errorf("chat service unavailable; resume before trying again")
	default:
		if envelope.Code != "" {
			return fmt.Errorf("chat request failed (%s): %s", envelope.Code, envelope.Message)
		}
		return fmt.Errorf("chat request failed with status %d", resp.StatusCode)
	}
}
func validateRecommendation(r domain.Recommendation, c CatalogSnapshot) error {
	if r.SchemaVersion != 1 || r.CatalogVersion != c.Version {
		return ErrUnsupportedCatalog
	}
	for _, id := range r.SelectedRecipeIDs {
		if !c.RecipeIDs[id] {
			return fmt.Errorf("recommendation references unknown recipe %q", id)
		}
	}
	for _, id := range r.SelectedPackIDs {
		if !c.PackIDs[id] {
			return fmt.Errorf("recommendation references unknown pack %q", id)
		}
	}
	if r.SelectedExerciseID != "" && !c.ExerciseIDs[r.SelectedExerciseID] {
		return fmt.Errorf("recommendation references unknown exercise %q", r.SelectedExerciseID)
	}
	_, err := domain.DecodeRecommendation(mustJSON(r))
	return err
}
func validateStart(start SetupStart) error {
	if strings.TrimSpace(start.ConsentVersion) == "" || len(start.ConsentVersion) > 100 || (start.Brief != nil && start.ApprovedBriefText != nil) {
		return errors.New("setup consent and at most one reviewed brief form are required")
	}
	if start.ApprovedBriefText != nil && (strings.TrimSpace(*start.ApprovedBriefText) == "" || len(*start.ApprovedBriefText) > 16<<10) {
		return errors.New("approved brief text must contain 1..16384 bytes")
	}
	if start.ApprovedBriefText != nil && containsCredential(*start.ApprovedBriefText) {
		return errors.New("approved brief appears to contain a credential; remove it before sharing")
	}
	if start.Brief != nil {
		if err := validateBrief(start.Brief); err != nil {
			return err
		}
	}
	if start.MachineFacts != nil {
		if err := validateFacts(start.MachineFacts); err != nil {
			return err
		}
	}
	return nil
}
func validateBrief(brief *domain.ProjectBrief) error {
	for _, field := range []string{brief.ProjectSummary, brief.DesiredSlice, brief.ProjectKind} {
		if containsCredential(field) {
			return errors.New("project brief appears to contain a credential; remove it before sharing")
		}
	}
	for _, field := range append(append([]string{}, brief.Constraints...), brief.KnownStack...) {
		if containsCredential(field) {
			return errors.New("project brief appears to contain a credential; remove it before sharing")
		}
	}
	data, err := json.Marshal(brief)
	if err != nil {
		return err
	}
	_, err = domain.DecodeProjectBrief(data)
	if err != nil {
		return fmt.Errorf("project facts are not valid reviewed input: %w", err)
	}
	return nil
}
func validateFacts(facts *domain.MachineFacts) error {
	for _, field := range []string{facts.OS, facts.Arch, facts.OSVersion, facts.DistroID, facts.DistroVersion, facts.ShellKind} {
		if containsCredential(field) {
			return errors.New("machine facts appear to contain a credential; remove it before sharing")
		}
	}
	for _, capability := range facts.Capabilities {
		if containsCredential(capability.Version) {
			return errors.New("machine facts appear to contain a credential; remove it before sharing")
		}
	}
	data, err := json.Marshal(facts)
	if err != nil {
		return err
	}
	_, err = domain.DecodeMachineFacts(data)
	if err != nil {
		return fmt.Errorf("machine facts are not safe to share: %w", err)
	}
	return nil
}
func containsCredential(value string) bool {
	return credentialAssignment.MatchString(value) || privateKeyMarker.MatchString(value)
}
func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }
