package agentservice_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/agentservice"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/llmgateway"
	"github.com/axsh/arctic-tern/shared/libs/go/wayfinder/portable"
	"github.com/axsh/arctic-tern/shared/libs/go/wayfinder/session"
)

type recordingAgent struct {
	name     string
	nativeID string
	reply    string
	mu       sync.Mutex
	lastCfg  *codingagent.SessionConfig
	prompts  []string
}

func (a *recordingAgent) Name() string { return a.name }
func (a *recordingAgent) Close() error { return nil }
func (a *recordingAgent) CreateSession(_ context.Context, opts ...codingagent.SessionOption) (codingagent.Session, error) {
	cfg := codingagent.NewSessionConfig(opts...)
	a.mu.Lock()
	a.lastCfg = cfg
	a.prompts = append(a.prompts, cfg.Prompt)
	a.mu.Unlock()
	return &recordingSession{agent: a}, nil
}
func (a *recordingAgent) Last() *codingagent.SessionConfig {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastCfg
}

type recordingSession struct {
	agent *recordingAgent
}

func (s *recordingSession) ID() string   { return s.agent.nativeID }
func (s *recordingSession) Close() error { return nil }
func (s *recordingSession) Send(_ context.Context, _ string) (<-chan codingagent.StreamEvent, error) {
	reply := s.agent.reply
	if reply == "" {
		reply = "hello"
	}
	ch := make(chan codingagent.StreamEvent, 3)
	ch <- codingagent.StreamEvent{Type: codingagent.EventSystem, SessionID: s.agent.nativeID}
	ch <- codingagent.StreamEvent{Type: codingagent.EventText, Content: reply}
	ch <- codingagent.StreamEvent{Type: codingagent.EventResult}
	close(ch)
	return ch, nil
}

type countingSummarizer struct {
	mu    sync.Mutex
	calls int
}

func (c *countingSummarizer) Summarize(_ context.Context, _ string, msgs []session.Message) (string, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	return "MR-SUMMARY", nil
}
func (c *countingSummarizer) Merge(_ context.Context, _ string, a, b string) (string, error) {
	return a + " " + b, nil
}

func newPortabilityServer(t *testing.T, sum portable.Summarizer) (*agentservice.Server, http.Handler, *recordingAgent, *recordingAgent) {
	t.Helper()
	claude := &recordingAgent{name: "claudecode", nativeID: "claude-native"}
	codex := &recordingAgent{name: "codex", nativeID: "codex-native"}
	opts := []agentservice.ServerOption{}
	if sum != nil {
		opts = append(opts, agentservice.WithSummarizer(sum))
	}
	srv := agentservice.New(opts...)
	srv.RegisterAgent(claude)
	srv.RegisterAgent(codex)
	srv.SetGatewayModels(
		[]llmgateway.ModelInfo{
			{Provider: "anthropic", Model: "claude-sonnet-4-20250514"},
			{Provider: "openai", Model: "gpt-4o"},
		},
		&llmgateway.ModelInfo{Provider: "anthropic", Model: "claude-sonnet-4-20250514"},
	)
	return srv, srv.HTTPHandler(), claude, codex
}

func createPortabilitySession(t *testing.T, handler http.Handler, agent, workDir, sessionDir, model string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"agent":       agent,
		"model":       model,
		"work_dir":    workDir,
		"session_dir": sessionDir,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var created map[string]string
	json.NewDecoder(w.Body).Decode(&created)
	return created["session_id"]
}

func sendJSON(t *testing.T, handler http.Handler, sessionID, text string, supplement map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	payload := map[string]any{
		"content": []map[string]string{{"type": "text", "text": text}},
	}
	if supplement != nil {
		payload["supplement"] = supplement
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/"+sessionID+"/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("send: %d %s", w.Code, w.Body.String())
	}
	return w
}

func getSessionMap(t *testing.T, handler http.Handler, sessionID string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/"+sessionID, nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get: %d %s", w.Code, w.Body.String())
	}
	var out map[string]any
	json.NewDecoder(w.Body).Decode(&out)
	return out
}

func patchJSON(t *testing.T, handler http.Handler, sessionID string, body any, want int) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/sessions/"+sessionID, bytes.NewReader(raw))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != want {
		t.Fatalf("patch status = %d want %d body=%s", w.Code, want, w.Body.String())
	}
	return w
}

func TestHandleCreateSession_DefaultSessionDirTern(t *testing.T) {
	_, handler, _, _ := newPortabilityServer(t, &countingSummarizer{})
	workDir := t.TempDir()
	id := createPortabilitySession(t, handler, "claudecode", workDir, "", "claude-sonnet-4-20250514")
	got := getSessionMap(t, handler, id)
	want := filepath.Join(workDir, ".tern", id)
	if got["session_dir"] != want {
		t.Errorf("session_dir = %v, want %s", got["session_dir"], want)
	}
}

func TestHandleCreateSession_InitCanonicalMetadata(t *testing.T) {
	_, handler, _, _ := newPortabilityServer(t, &countingSummarizer{})
	workDir := t.TempDir()
	id := createPortabilitySession(t, handler, "claudecode", workDir, "", "claude-sonnet-4-20250514")
	dir := filepath.Join(workDir, ".tern", id)
	if _, err := os.Stat(filepath.Join(dir, "record.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "metadata.json")); err != nil {
		t.Fatal(err)
	}
}

func TestHandleListSessions_ByWorkDirReloadsFromDisk(t *testing.T) {
	workDir := t.TempDir()
	_, handlerA, _, _ := newPortabilityServer(t, &countingSummarizer{})
	id := createPortabilitySession(t, handlerA, "claudecode", workDir, "", "claude-sonnet-4-20250514")
	_, handlerB, _, _ := newPortabilityServer(t, &countingSummarizer{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions?work_dir="+url.QueryEscape(workDir), nil)
	w := httptest.NewRecorder()
	handlerB.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	var recs []map[string]any
	json.NewDecoder(w.Body).Decode(&recs)
	if len(recs) != 1 || recs[0]["id"] != id {
		t.Fatalf("recs = %+v", recs)
	}
}

func TestHandleSendMessage_IngestsAssistantWithOrigin(t *testing.T) {
	_, handler, _, _ := newPortabilityServer(t, &countingSummarizer{})
	sessionDir := t.TempDir()
	id := createPortabilitySession(t, handler, "claudecode", t.TempDir(), sessionDir, "claude-sonnet-4-20250514")
	sendJSON(t, handler, id, "hello user", nil)
	got := getSessionMap(t, handler, id)
	bindings, _ := got["agent_bindings"].(map[string]any)
	b, _ := bindings["claudecode"].(map[string]any)
	if b["agent_session_id"] != "claude-native" {
		t.Errorf("bindings = %#v", got["agent_bindings"])
	}
	msgs, err := session.OpenCanonical(sessionDir).LoadRange(1, 0)
	if err != nil {
		t.Fatal(err)
	}
	var sawUser, sawAsst bool
	for _, m := range msgs {
		if m.Origin != session.OriginClaudeCode {
			t.Errorf("origin = %q", m.Origin)
		}
		if m.Role == "user" {
			sawUser = true
		}
		if m.Role == "assistant" {
			sawAsst = true
		}
	}
	if !sawUser || !sawAsst {
		t.Fatalf("history missing roles: %+v", msgs)
	}
}

func TestHandleSendMessage_SameAgentSecondTurnResumesAndKeepsFact(t *testing.T) {
	_, handler, claude, _ := newPortabilityServer(t, &countingSummarizer{})
	sessionDir := t.TempDir()
	id := createPortabilitySession(t, handler, "claudecode", t.TempDir(), sessionDir, "claude-sonnet-4-20250514")
	const token = "CTX-TOKEN-7F3A"
	sendJSON(t, handler, id, token, nil)
	sendJSON(t, handler, id, "second", nil)
	cfg := claude.Last()
	if cfg.AgentSessionID != "claude-native" {
		t.Errorf("resume = %q", cfg.AgentSessionID)
	}
	if strings.Contains(cfg.Prompt, portable.TransferHeader) {
		t.Errorf("same-agent prompt should not wrap: %s", cfg.Prompt)
	}
	data, _ := os.ReadFile(filepath.Join(sessionDir, "history", "0000001.json"))
	if !strings.Contains(string(data), token) {
		t.Errorf("token missing from history: %s", data)
	}
}

func TestHandlePatchSession_SwitchAgentClearsActiveNativeID(t *testing.T) {
	_, handler, _, _ := newPortabilityServer(t, &countingSummarizer{})
	sessionDir := t.TempDir()
	id := createPortabilitySession(t, handler, "claudecode", t.TempDir(), sessionDir, "claude-sonnet-4-20250514")
	sendJSON(t, handler, id, "hi", nil)
	patchJSON(t, handler, id, map[string]string{"agent": "codex"}, http.StatusOK)
	got := getSessionMap(t, handler, id)
	if got["agent_name"] != "codex" {
		t.Errorf("agent_name = %v", got["agent_name"])
	}
	if got["agent_session_id"] != "" {
		t.Errorf("active native should be cleared, got %v", got["agent_session_id"])
	}
	if got["id"] != id || got["session_dir"] != sessionDir && filepath.Clean(got["session_dir"].(string)) != filepath.Clean(sessionDir) {
		// session_dir is absolutized
	}
	bindings, _ := got["agent_bindings"].(map[string]any)
	if _, ok := bindings["claudecode"]; !ok {
		t.Errorf("claude binding lost: %#v", bindings)
	}
}

func TestHandlePatchSession_BusyRejected(t *testing.T) {
	srv, handler, _, _ := newPortabilityServer(t, &countingSummarizer{})
	id := createPortabilitySession(t, handler, "claudecode", t.TempDir(), t.TempDir(), "claude-sonnet-4-20250514")
	if err := srv.MarkSessionBusy(id, codingagent.StatusActive); err != nil {
		t.Fatal(err)
	}
	w := patchJSON(t, handler, id, map[string]string{"agent": "codex"}, http.StatusConflict)
	if !strings.Contains(w.Body.String(), "session busy") {
		t.Errorf("body = %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "follow, respond or terminate") {
		t.Errorf("hint missing follow: %s", w.Body.String())
	}
	got := getSessionMap(t, handler, id)
	if got["followable"] != true {
		t.Errorf("followable = %v", got["followable"])
	}
	if got["turn_id"] != "busy-turn" {
		t.Errorf("turn_id = %v", got["turn_id"])
	}
	if got["agent_name"] != "claudecode" {
		t.Errorf("agent changed while busy: %v", got["agent_name"])
	}
}

func TestHandlePatchSession_UnknownAgent400(t *testing.T) {
	_, handler, _, _ := newPortabilityServer(t, &countingSummarizer{})
	id := createPortabilitySession(t, handler, "claudecode", t.TempDir(), t.TempDir(), "claude-sonnet-4-20250514")
	patchJSON(t, handler, id, map[string]string{"agent": "nope"}, http.StatusBadRequest)
}

func TestHandlePatchSession_AgentOrConfigDirRequired(t *testing.T) {
	_, handler, _, _ := newPortabilityServer(t, &countingSummarizer{})
	id := createPortabilitySession(t, handler, "claudecode", t.TempDir(), t.TempDir(), "claude-sonnet-4-20250514")
	patchJSON(t, handler, id, map[string]any{}, http.StatusBadRequest)
	patchJSON(t, handler, id, map[string]string{"agent": "codex"}, http.StatusOK)
	patchJSON(t, handler, id, map[string]string{"model": "gpt-4o"}, http.StatusOK)
	patchJSON(t, handler, id, map[string]any{"supplement": map[string]string{"algorithm": "full"}}, http.StatusOK)
	patchJSON(t, handler, id, map[string]any{"supplement": map[string]string{"algorithm": "unknown"}}, http.StatusBadRequest)
}

func TestHandlePatchSession_StoresSupplementStrategy(t *testing.T) {
	_, handler, _, _ := newPortabilityServer(t, &countingSummarizer{})
	id := createPortabilitySession(t, handler, "claudecode", t.TempDir(), t.TempDir(), "claude-sonnet-4-20250514")
	patchJSON(t, handler, id, map[string]any{
		"agent":      "codex",
		"supplement": map[string]any{"algorithm": "structured", "recent_keep": 2},
	}, http.StatusOK)
	got := getSessionMap(t, handler, id)
	sup, _ := got["supplement"].(map[string]any)
	if sup["algorithm"] != "structured" {
		t.Errorf("supplement = %#v", sup)
	}
}

func TestHandleSendMessage_AfterSwitchDoesNotResumeForeign(t *testing.T) {
	_, handler, _, codex := newPortabilityServer(t, &countingSummarizer{})
	sessionDir := t.TempDir()
	id := createPortabilitySession(t, handler, "claudecode", t.TempDir(), sessionDir, "claude-sonnet-4-20250514")
	sendJSON(t, handler, id, "remember CTX-TOKEN-7F3A", nil)
	patchJSON(t, handler, id, map[string]string{"agent": "codex"}, http.StatusOK)
	sendJSON(t, handler, id, "what was the token?", nil)
	cfg := codex.Last()
	if cfg.AgentSessionID != "" {
		t.Errorf("must not resume foreign native id, got %q", cfg.AgentSessionID)
	}
	if !strings.Contains(cfg.Prompt, portable.TransferHeader) || !strings.Contains(cfg.Prompt, "CTX-TOKEN-7F3A") {
		t.Errorf("prompt missing transfer: %s", cfg.Prompt)
	}
}

func TestHandleSendMessage_SwitchBackResumesOwnNative(t *testing.T) {
	_, handler, claude, _ := newPortabilityServer(t, &countingSummarizer{})
	sessionDir := t.TempDir()
	id := createPortabilitySession(t, handler, "claudecode", t.TempDir(), sessionDir, "claude-sonnet-4-20250514")
	sendJSON(t, handler, id, "from claude", nil)
	patchJSON(t, handler, id, map[string]string{"agent": "codex"}, http.StatusOK)
	sendJSON(t, handler, id, "from codex", nil)
	patchJSON(t, handler, id, map[string]string{"agent": "claudecode"}, http.StatusOK)
	sendJSON(t, handler, id, "back", nil)
	cfg := claude.Last()
	if cfg.AgentSessionID != "claude-native" {
		t.Errorf("resume own native = %q", cfg.AgentSessionID)
	}
	if !strings.Contains(cfg.Prompt, portable.TransferHeader) || !strings.Contains(cfg.Prompt, "from codex") {
		t.Errorf("should inject codex turn: %s", cfg.Prompt)
	}
}

func TestHandleSendMessage_TurnSupplementOverridesSession(t *testing.T) {
	sum := &countingSummarizer{}
	_, handler, _, codex := newPortabilityServer(t, sum)
	sessionDir := t.TempDir()
	id := createPortabilitySession(t, handler, "claudecode", t.TempDir(), sessionDir, "claude-sonnet-4-20250514")
	sendJSON(t, handler, id, "token-A", nil)
	patchJSON(t, handler, id, map[string]any{
		"agent":      "codex",
		"supplement": map[string]string{"algorithm": "map_reduce"},
	}, http.StatusOK)
	sendJSON(t, handler, id, "ask", map[string]any{"algorithm": "full"})
	if sum.calls != 0 {
		t.Errorf("full turn override called summarizer %d times", sum.calls)
	}
	if !strings.Contains(codex.Last().Prompt, "token-A") {
		t.Errorf("full inject missing token")
	}
}

func TestHandlePatchSession_ModelSwitchKeepsNativeResume(t *testing.T) {
	_, handler, claude, _ := newPortabilityServer(t, &countingSummarizer{})
	id := createPortabilitySession(t, handler, "claudecode", t.TempDir(), t.TempDir(), "claude-sonnet-4-20250514")
	sendJSON(t, handler, id, "CTX-TOKEN-7F3A", nil)
	patchJSON(t, handler, id, map[string]string{"model": "gpt-4o"}, http.StatusOK)
	got := getSessionMap(t, handler, id)
	if got["model"] != "gpt-4o" {
		t.Errorf("model = %v", got["model"])
	}
	if got["agent_session_id"] != "claude-native" {
		t.Errorf("native cleared: %v", got["agent_session_id"])
	}
	sendJSON(t, handler, id, "continue", nil)
	cfg := claude.Last()
	if cfg.Model != "gpt-4o" {
		t.Errorf("model = %q", cfg.Model)
	}
	if cfg.AgentSessionID != "claude-native" {
		t.Errorf("resume = %q", cfg.AgentSessionID)
	}
	if strings.Contains(cfg.Prompt, portable.TransferHeader) {
		t.Errorf("model switch must not inject: %s", cfg.Prompt)
	}
}

func TestHandleSendMessage_DoesNotRewriteOldOrigin(t *testing.T) {
	_, handler, _, _ := newPortabilityServer(t, &countingSummarizer{})
	sessionDir := t.TempDir()
	id := createPortabilitySession(t, handler, "claudecode", t.TempDir(), sessionDir, "claude-sonnet-4-20250514")
	sendJSON(t, handler, id, "old fact", nil)
	patchJSON(t, handler, id, map[string]string{"agent": "codex"}, http.StatusOK)
	sendJSON(t, handler, id, "new fact", nil)
	msgs, err := session.OpenCanonical(sessionDir).LoadRange(1, 0)
	if err != nil {
		t.Fatal(err)
	}
	var sawClaude, sawCodex bool
	for _, m := range msgs {
		if m.Origin == session.OriginClaudeCode {
			sawClaude = true
		}
		if m.Origin == session.OriginCodex {
			sawCodex = true
		}
	}
	if !sawClaude || !sawCodex {
		t.Fatalf("expected mixed origins, got %+v", msgs)
	}
}
