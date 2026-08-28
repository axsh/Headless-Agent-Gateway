package llm_test

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
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/agentservice"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/llmgateway"
	"github.com/axsh/arctic-tern/shared/libs/go/wayfinder/portable"
	"github.com/axsh/arctic-tern/shared/libs/go/wayfinder/session"
)

const portabilityToken = "CTX-TOKEN-7F3A"

type portabilityAgent struct {
	name     string
	nativeID string
	mu       sync.Mutex
	lastCfg  *codingagent.SessionConfig
}

func (a *portabilityAgent) Name() string { return a.name }
func (a *portabilityAgent) Close() error { return nil }
func (a *portabilityAgent) CreateSession(_ context.Context, opts ...codingagent.SessionOption) (codingagent.Session, error) {
	cfg := codingagent.NewSessionConfig(opts...)
	a.mu.Lock()
	a.lastCfg = cfg
	a.mu.Unlock()
	return &portabilitySession{agent: a}, nil
}
func (a *portabilityAgent) last() *codingagent.SessionConfig {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastCfg
}

type portabilitySession struct{ agent *portabilityAgent }

func (s *portabilitySession) ID() string   { return s.agent.nativeID }
func (s *portabilitySession) Close() error { return nil }
func (s *portabilitySession) Send(_ context.Context, _ string) (<-chan codingagent.StreamEvent, error) {
	ch := make(chan codingagent.StreamEvent, 3)
	ch <- codingagent.StreamEvent{Type: codingagent.EventSystem, SessionID: s.agent.nativeID}
	ch <- codingagent.StreamEvent{Type: codingagent.EventText, Content: "ok from " + s.agent.name}
	ch <- codingagent.StreamEvent{Type: codingagent.EventResult}
	close(ch)
	return ch, nil
}

type portabilitySummarizer struct {
	mu    sync.Mutex
	calls int
	seen  []string
}

func (s *portabilitySummarizer) Summarize(_ context.Context, _ string, msgs []session.Message) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	for _, m := range msgs {
		s.seen = append(s.seen, m.Content)
	}
	return "MR-SUMMARY", nil
}
func (s *portabilitySummarizer) Merge(_ context.Context, _ string, a, b string) (string, error) {
	return a + " " + b, nil
}

func newPortabilityHTTP(t *testing.T) (*httptest.Server, *portabilityAgent, *portabilityAgent, *portabilitySummarizer) {
	t.Helper()
	sum := &portabilitySummarizer{}
	claude := &portabilityAgent{name: "claudecode", nativeID: "claude-native"}
	codex := &portabilityAgent{name: "codex", nativeID: "codex-native"}
	srv := agentservice.New(agentservice.WithSummarizer(sum))
	srv.RegisterAgent(claude)
	srv.RegisterAgent(codex)
	srv.SetGatewayModels(
		[]llmgateway.ModelInfo{{Model: "claude-sonnet-4-6"}, {Model: "gpt-4o"}},
		&llmgateway.ModelInfo{Model: "claude-sonnet-4-6"},
	)
	ts := httptest.NewServer(srv.HTTPHandler())
	t.Cleanup(ts.Close)
	return ts, claude, codex, sum
}

func portCreate(t *testing.T, ts *httptest.Server, agent, workDir, sessionDir, model string, storageRoot ...string) string {
	t.Helper()
	body := map[string]string{
		"agent": agent, "model": model, "work_dir": workDir,
	}
	if sessionDir != "" {
		body["session_dir"] = sessionDir
	}
	if len(storageRoot) > 0 && storageRoot[0] != "" {
		body["storage_root"] = storageRoot[0]
	}
	raw, _ := json.Marshal(body)
	resp, err := http.Post(ts.URL+"/api/v1/sessions", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		buf, _ := json.Marshal(resp.Status)
		t.Fatalf("create %d %s %s", resp.StatusCode, buf, mustRead(resp))
	}
	var created map[string]string
	json.NewDecoder(resp.Body).Decode(&created)
	return created["session_id"]
}

func mustRead(resp *http.Response) string {
	b, _ := json.Marshal(resp.Status)
	return string(b)
}

func portSend(t *testing.T, ts *httptest.Server, id, text string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"content": []map[string]string{{"type": "text", "text": text}}})
	resp, err := http.Post(ts.URL+"/api/v1/sessions/"+id+"/messages", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := json.Marshal(struct{}{})
		buf := make([]byte, 4096)
		n, _ := resp.Body.Read(buf)
		t.Fatalf("send %d %s %s", resp.StatusCode, raw, buf[:n])
	}
}

func portPatch(t *testing.T, ts *httptest.Server, id string, body any, want int) {
	t.Helper()
	raw, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPatch, ts.URL+"/api/v1/sessions/"+id, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != want {
		buf := make([]byte, 2048)
		n, _ := resp.Body.Read(buf)
		t.Fatalf("patch %d want %d %s", resp.StatusCode, want, buf[:n])
	}
}

func TestSessionPortabilityBaselineSameAgent(t *testing.T) {
	ts, claude, _, _ := newPortabilityHTTP(t)
	sessionDir := t.TempDir()
	id := portCreate(t, ts, "claudecode", t.TempDir(), sessionDir, "claude-sonnet-4-6")
	portSend(t, ts, id, portabilityToken)
	portSend(t, ts, id, "second")
	cfg := claude.last()
	if cfg.AgentSessionID != "claude-native" {
		t.Errorf("resume = %q", cfg.AgentSessionID)
	}
	if strings.Contains(cfg.Prompt, portable.TransferHeader) {
		t.Errorf("baseline must not wrap: %s", cfg.Prompt)
	}
	data, _ := os.ReadFile(filepath.Join(sessionDir, "history", "0000001.json"))
	if !strings.Contains(string(data), portabilityToken) {
		t.Errorf("token missing: %s", data)
	}
}

func TestSessionPortabilityModelSwitchKeepsResume(t *testing.T) {
	ts, claude, _, _ := newPortabilityHTTP(t)
	sessionDir := t.TempDir()
	id := portCreate(t, ts, "claudecode", t.TempDir(), sessionDir, "claude-sonnet-4-6")
	portSend(t, ts, id, portabilityToken)
	portPatch(t, ts, id, map[string]string{"model": "gpt-4o"}, http.StatusOK)
	portSend(t, ts, id, "continue")
	cfg := claude.last()
	if cfg.Model != "gpt-4o" || cfg.AgentSessionID != "claude-native" {
		t.Errorf("cfg = %+v", cfg)
	}
	if strings.Contains(cfg.Prompt, portable.TransferHeader) {
		t.Errorf("model switch wrap: %s", cfg.Prompt)
	}
}

func TestSessionPortabilityIngestOrigin(t *testing.T) {
	ts, _, _, _ := newPortabilityHTTP(t)
	sessionDir := t.TempDir()
	id := portCreate(t, ts, "claudecode", t.TempDir(), sessionDir, "claude-sonnet-4-6")
	portSend(t, ts, id, "hello")
	msgs, err := session.OpenCanonical(sessionDir).LoadRange(1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) == 0 {
		t.Fatal("empty history")
	}
	for _, m := range msgs {
		if m.Origin != session.OriginClaudeCode {
			t.Errorf("origin = %q", m.Origin)
		}
	}
	_ = id
}

func TestSessionPortabilityAgentSwitchSupplement(t *testing.T) {
	ts, _, codex, _ := newPortabilityHTTP(t)
	sessionDir := t.TempDir()
	id := portCreate(t, ts, "claudecode", t.TempDir(), sessionDir, "claude-sonnet-4-6")
	portSend(t, ts, id, portabilityToken)
	portPatch(t, ts, id, map[string]string{"agent": "codex"}, http.StatusOK)
	portSend(t, ts, id, "ask")
	cfg := codex.last()
	if cfg.AgentSessionID != "" {
		t.Errorf("foreign resume %q", cfg.AgentSessionID)
	}
	if !strings.Contains(cfg.Prompt, portable.TransferHeader) || !strings.Contains(cfg.Prompt, portabilityToken) {
		t.Errorf("prompt = %s", cfg.Prompt)
	}
	// workDir was t.TempDir() in portCreate — recover from cfg via VendorHome contract
	if filepath.Base(cfg.SessionDir) != ".codex" {
		t.Errorf("codex vendor home = %q, want .../.codex", cfg.SessionDir)
	}
}

func TestSessionPortabilitySwitchBackResumesOwn(t *testing.T) {
	ts, claude, _, _ := newPortabilityHTTP(t)
	sessionDir := t.TempDir()
	id := portCreate(t, ts, "claudecode", t.TempDir(), sessionDir, "claude-sonnet-4-6")
	portSend(t, ts, id, "from-claude")
	portPatch(t, ts, id, map[string]string{"agent": "codex"}, http.StatusOK)
	portSend(t, ts, id, "from-codex")
	portPatch(t, ts, id, map[string]string{"agent": "claudecode"}, http.StatusOK)
	portSend(t, ts, id, "back")
	cfg := claude.last()
	if cfg.AgentSessionID != "claude-native" {
		t.Errorf("own resume = %q", cfg.AgentSessionID)
	}
	if !strings.Contains(cfg.Prompt, "from-codex") {
		t.Errorf("missing codex delta: %s", cfg.Prompt)
	}
}

func TestSessionPortabilityAgentRoundTrip(t *testing.T) {
	ts, _, codex, _ := newPortabilityHTTP(t)
	sessionDir := t.TempDir()
	id := portCreate(t, ts, "claudecode", t.TempDir(), sessionDir, "claude-sonnet-4-6")
	portSend(t, ts, id, "c1")
	portPatch(t, ts, id, map[string]string{"agent": "codex"}, http.StatusOK)
	portSend(t, ts, id, "x1")
	portPatch(t, ts, id, map[string]string{"agent": "claudecode"}, http.StatusOK)
	portSend(t, ts, id, "c2")
	portPatch(t, ts, id, map[string]string{"agent": "codex"}, http.StatusOK)
	portSend(t, ts, id, "x2")
	cfg := codex.last()
	if cfg.AgentSessionID != "codex-native" {
		t.Errorf("codex resume = %q", cfg.AgentSessionID)
	}
}

func TestSessionPortabilityMixedOriginImmutable(t *testing.T) {
	ts, _, codex, _ := newPortabilityHTTP(t)
	sessionDir := t.TempDir()
	c := session.OpenCanonical(sessionDir)
	if err := c.Init("pre", session.OriginWayfinder); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := c.Append([]session.Message{
		{Role: "assistant", Origin: session.OriginClaudeCode, Content: "claude-tool", ToolCalls: []session.ToolCallRecord{{Name: "Read"}}, Timestamp: now},
		{Role: "assistant", Origin: session.OriginWayfinder, Content: "wayfinder-plan", Timestamp: now},
	}); err != nil {
		t.Fatal(err)
	}
	id := portCreate(t, ts, "codex", t.TempDir(), sessionDir, "gpt-4o")
	portSend(t, ts, id, "go")
	cfg := codex.last()
	if !strings.Contains(cfg.Prompt, "[origin=claudecode]") || !strings.Contains(cfg.Prompt, "[origin=wayfinder]") {
		t.Errorf("prompt = %s", cfg.Prompt)
	}
	msgs, _ := c.LoadRange(1, 2)
	if msgs[0].Origin != session.OriginClaudeCode || msgs[1].Origin != session.OriginWayfinder {
		t.Fatalf("origins rewritten: %+v", msgs)
	}
}

func TestSessionPortabilitySameAgentResume(t *testing.T) {
	TestSessionPortabilityBaselineSameAgent(t)
}

func TestSessionPortabilityBusyRejectsSwitch(t *testing.T) {
	sum := &portabilitySummarizer{}
	claude := &portabilityAgent{name: "claudecode", nativeID: "claude-native"}
	codex := &portabilityAgent{name: "codex", nativeID: "codex-native"}
	srv := agentservice.New(agentservice.WithSummarizer(sum))
	srv.RegisterAgent(claude)
	srv.RegisterAgent(codex)
	ts := httptest.NewServer(srv.HTTPHandler())
	t.Cleanup(ts.Close)
	id := portCreate(t, ts, "claudecode", t.TempDir(), t.TempDir(), "claude-sonnet-4-6")
	if err := srv.MarkSessionBusy(id, codingagent.StatusActive); err != nil {
		t.Fatal(err)
	}
	portPatch(t, ts, id, map[string]string{"agent": "codex"}, http.StatusConflict)
}

func TestSessionPortabilityMapReduceKeepsToken(t *testing.T) {
	ts, _, _, sum := newPortabilityHTTP(t)
	sessionDir := t.TempDir()
	c := session.OpenCanonical(sessionDir)
	if err := c.Init("mr", session.OriginClaudeCode); err != nil {
		t.Fatal(err)
	}
	var msgs []session.Message
	msgs = append(msgs, session.Message{Role: "user", Origin: session.OriginClaudeCode, Content: portabilityToken + strings.Repeat("a", 2000)})
	for i := 0; i < 10; i++ {
		msgs = append(msgs, session.Message{Role: "assistant", Origin: session.OriginClaudeCode, Content: strings.Repeat("b", 4000)})
	}
	if err := c.Append(msgs); err != nil {
		t.Fatal(err)
	}
	before, _ := c.LoadRange(1, 0)
	id := portCreate(t, ts, "codex", t.TempDir(), sessionDir, "gpt-4o")
	portPatch(t, ts, id, map[string]any{
		"supplement": map[string]any{"algorithm": "map_reduce", "threshold_bytes": 100, "recent_keep": 2},
	}, http.StatusOK)
	portSend(t, ts, id, "ask")
	found := false
	for _, s := range sum.seen {
		if strings.Contains(s, portabilityToken) {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("summarize input missing token")
	}
	after, _ := c.LoadRange(1, 0)
	if len(after) < len(before) {
		t.Fatalf("history compacted: %d -> %d", len(before), len(after))
	}
	portPatch(t, ts, id, map[string]any{"supplement": map[string]any{"algorithm": "full"}}, http.StatusOK)
}

func TestSessionPortabilitySupplementStrategy(t *testing.T) {
	ts, _, _, sum := newPortabilityHTTP(t)
	id := portCreate(t, ts, "claudecode", t.TempDir(), t.TempDir(), "claude-sonnet-4-6")
	portSend(t, ts, id, "x")
	resp, err := http.Get(ts.URL + "/api/v1/sessions/" + id)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	json.NewDecoder(resp.Body).Decode(&got)
	resp.Body.Close()
	sup, _ := got["supplement"].(map[string]any)
	if sup["algorithm"] != "map_reduce" {
		t.Errorf("default algorithm = %#v", sup)
	}
	portPatch(t, ts, id, map[string]any{"agent": "codex", "supplement": map[string]any{"algorithm": "structured"}}, http.StatusOK)
	before := sum.calls
	portSend(t, ts, id, "ask")
	if sum.calls != before {
		t.Errorf("structured called LLM")
	}
}

func TestSessionPortabilityReloadFromWorkspace(t *testing.T) {
	workDir := t.TempDir()
	tsA, _, _, _ := newPortabilityHTTP(t)
	id := portCreate(t, tsA, "claudecode", workDir, "", "claude-sonnet-4-6")
	portSend(t, tsA, id, portabilityToken)
	tsB, claude, _, _ := newPortabilityHTTP(t)
	resp, err := http.Get(tsB.URL + "/api/v1/sessions?work_dir=" + url.QueryEscape(workDir))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var recs []map[string]any
	json.NewDecoder(resp.Body).Decode(&recs)
	if len(recs) != 1 || recs[0]["id"] != id {
		t.Fatalf("reload recs = %+v", recs)
	}
	portSend(t, tsB, id, "again")
	if claude.last() == nil {
		t.Fatal("send after reload failed to reach agent")
	}
	if _, err := os.Stat(filepath.Join(workDir, ".tern", "session.db")); !os.IsNotExist(err) {
		t.Fatal("session.db must not exist")
	}
}
