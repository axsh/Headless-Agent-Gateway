package agentservice_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/agentservice"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/config"
)

type retryAgent struct {
	name       string
	nativeID   string
	failTimes  int
	nonRetry   bool
	delay      time.Duration
	mu         sync.Mutex
	creates    int
	closes     int
	cfgs       []*codingagent.SessionConfig
	sendDone   atomic.Bool
	earlyClose atomic.Int32
}

func (a *retryAgent) Name() string { return a.name }
func (a *retryAgent) Close() error { return nil }
func (a *retryAgent) CreateSession(_ context.Context, opts ...codingagent.SessionOption) (codingagent.Session, error) {
	cfg := codingagent.NewSessionConfig(opts...)
	a.mu.Lock()
	a.creates++
	a.cfgs = append(a.cfgs, cfg)
	a.mu.Unlock()
	return &retrySession{agent: a}, nil
}

func (a *retryAgent) lastCfg() *codingagent.SessionConfig {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.cfgs) == 0 {
		return nil
	}
	return a.cfgs[len(a.cfgs)-1]
}

type retrySession struct{ agent *retryAgent }

func (s *retrySession) ID() string { return s.agent.nativeID }
func (s *retrySession) Close() error {
	if !s.agent.sendDone.Load() {
		s.agent.earlyClose.Add(1)
	}
	s.agent.mu.Lock()
	s.agent.closes++
	s.agent.mu.Unlock()
	return nil
}
func (s *retrySession) Send(_ context.Context, _ string) (<-chan codingagent.StreamEvent, error) {
	ch := make(chan codingagent.StreamEvent, 4)
	s.agent.sendDone.Store(false)
	go func() {
		defer close(ch)
		defer s.agent.sendDone.Store(true)
		if s.agent.delay > 0 {
			time.Sleep(s.agent.delay)
		}
		s.agent.mu.Lock()
		n := s.agent.creates
		s.agent.mu.Unlock()
		if s.agent.nonRetry {
			ch <- codingagent.StreamEvent{Type: codingagent.EventError, Content: "unauthorized"}
			return
		}
		if n <= s.agent.failTimes {
			ch <- codingagent.StreamEvent{Type: codingagent.EventSystem, SessionID: s.agent.nativeID}
			ch <- codingagent.StreamEvent{
				Type:      codingagent.EventError,
				Content:   "Reconnecting... 1/5 (We're currently experiencing high demand)",
				Retryable: true,
			}
			return
		}
		ch <- codingagent.StreamEvent{Type: codingagent.EventSystem, SessionID: s.agent.nativeID}
		ch <- codingagent.StreamEvent{Type: codingagent.EventText, Content: "ok"}
		ch <- codingagent.StreamEvent{Type: codingagent.EventResult}
	}()
	return ch, nil
}

func newRetryHandler(t *testing.T, agent *retryAgent, retryCfg config.ProcessRetryConfig) (*agentservice.Server, http.Handler) {
	t.Helper()
	srv := agentservice.New(agentservice.WithProcessRetry(retryCfg))
	srv.RegisterAgent(agent)
	return srv, srv.HTTPHandler()
}

func postSSE(t *testing.T, handler http.Handler, sessionID, prompt string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"content": []map[string]string{{"type": "text", "text": prompt}},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/"+sessionID+"/messages", bytes.NewReader(body))
	req.Header.Set("Accept", "text/event-stream")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

func sseEvents(t *testing.T, raw string) (events []map[string]any, errors int) {
	t.Helper()
	scanner := bufio.NewScanner(strings.NewReader(raw))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			continue
		}
		var ev map[string]any
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			continue
		}
		events = append(events, ev)
		if ev["type"] == "error" {
			errors++
		}
	}
	return events, errors
}

func TestHandleSendMessage_CodexRetryableProcessRetriesSameResume(t *testing.T) {
	agent := &retryAgent{name: "codex", nativeID: "codex-native", failTimes: 1}
	srv, handler := newRetryHandler(t, agent, config.ProcessRetryConfig{MaxAttempts: 3})
	sessionID := createCodexSessionHTTP(t, handler)
	w := postSSE(t, handler, sessionID, "hello")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	_, errCount := sseEvents(t, w.Body.String())
	if errCount != 0 {
		t.Fatalf("expected no SSE EventError, got %d in %s", errCount, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"type":"result"`) {
		t.Fatalf("expected EventResult, body=%s", w.Body.String())
	}
	if agent.creates != 2 {
		t.Fatalf("creates = %d, want 2", agent.creates)
	}
	last := agent.lastCfg()
	if last == nil || last.AgentSessionID != "codex-native" {
		t.Fatalf("second resume id = %+v, want codex-native", last)
	}
	_ = srv
}

func TestHandleSendMessage_CodexRetryExhaustedOneClassifiedError(t *testing.T) {
	agent := &retryAgent{name: "codex", nativeID: "codex-native", failTimes: 99}
	_, handler := newRetryHandler(t, agent, config.ProcessRetryConfig{MaxAttempts: 2})
	sessionID := createCodexSessionHTTP(t, handler)
	w := postSSE(t, handler, sessionID, "hello")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	_, errCount := sseEvents(t, w.Body.String())
	if errCount != 1 {
		t.Fatalf("EventError count = %d, want 1 body=%s", errCount, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "[upstream_overloaded]") {
		t.Fatalf("missing [upstream_overloaded]: %s", w.Body.String())
	}
	if agent.creates != 2 {
		t.Fatalf("creates = %d, want 2", agent.creates)
	}
	w2 := postSSE(t, handler, sessionID, "again")
	if w2.Code == http.StatusConflict {
		t.Fatal("session still busy after classified failure")
	}
}

func TestHandleSendMessage_NonRetryableNoProcessRetry(t *testing.T) {
	agent := &retryAgent{name: "codex", nativeID: "n1", nonRetry: true}
	_, handler := newRetryHandler(t, agent, config.ProcessRetryConfig{MaxAttempts: 3})
	sessionID := createCodexSessionHTTP(t, handler)
	w := postSSE(t, handler, sessionID, "hello")
	if agent.creates != 1 {
		t.Fatalf("creates = %d, want 1", agent.creates)
	}
	if !strings.Contains(w.Body.String(), "unauthorized") {
		t.Fatalf("body = %s", w.Body.String())
	}
}

func TestHandleSendMessage_ClaudeDoesNotProcessRetry(t *testing.T) {
	agent := &retryAgent{name: "claudecode", nativeID: "claude-native", failTimes: 1}
	srv := agentservice.New(agentservice.WithProcessRetry(config.ProcessRetryConfig{MaxAttempts: 3}))
	srv.RegisterAgent(agent)
	handler := srv.HTTPHandler()
	body, _ := json.Marshal(map[string]string{"agent": "claudecode", "session_dir": t.TempDir()})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var created map[string]string
	json.NewDecoder(rec.Body).Decode(&created)
	w := postSSE(t, handler, created["session_id"], "hello")
	if agent.creates != 1 {
		t.Fatalf("creates = %d, want 1 (codex-only process retry)", agent.creates)
	}
	if !strings.Contains(w.Body.String(), "[upstream_overloaded]") && agent.failTimes == 1 {
		// swallowed? Claude should emit classified on exhaust of 1 attempt.
		if !strings.Contains(w.Body.String(), "error") {
			t.Fatalf("expected error for claude failTimes=1, body=%s", w.Body.String())
		}
	}
}

func TestHandleSendMessage_ClientDisconnectDoesNotFinishUntilTerminal(t *testing.T) {
	agent := &retryAgent{name: "codex", nativeID: "n1", delay: 400 * time.Millisecond}
	srv := agentservice.New(agentservice.WithProcessRetry(config.ProcessRetryConfig{MaxAttempts: 1}))
	srv.RegisterAgent(agent)
	ts := httptest.NewServer(srv.HTTPHandler())
	defer ts.Close()

	sessionID := createCodexSessionHTTP(t, srv.HTTPHandler())
	reqCtx, cancel := context.WithCancel(context.Background())
	msgBody, _ := json.Marshal(map[string]any{
		"content": []map[string]string{{"type": "text", "text": "run"}},
	})
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, ts.URL+"/api/v1/sessions/"+sessionID+"/messages", bytes.NewReader(msgBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept", "text/event-stream")
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	resp, err := http.DefaultClient.Do(req)
	if err == nil {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	time.Sleep(200 * time.Millisecond)
	if agent.earlyClose.Load() != 0 {
		t.Fatalf("Close called before Send finished, early=%d closes=%d", agent.earlyClose.Load(), agent.closes)
	}
	deadline := time.Now().Add(3 * time.Second)
	var closes int
	for time.Now().Before(deadline) {
		agent.mu.Lock()
		closes = agent.closes
		agent.mu.Unlock()
		if closes >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if closes < 1 {
		t.Fatalf("Close count = %d, want >= 1 after terminal", closes)
	}
	w := postSSE(t, srv.HTTPHandler(), sessionID, "next")
	if w.Code == http.StatusConflict {
		t.Fatal("session still busy after drain")
	}
}
