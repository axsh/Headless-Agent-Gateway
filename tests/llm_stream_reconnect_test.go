package llm_test

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
	"github.com/axsh/arctic-tern/shared/libs/go/llmgateway"
)

type reconnectAgent struct {
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

func (a *reconnectAgent) Name() string { return a.name }
func (a *reconnectAgent) Close() error { return nil }
func (a *reconnectAgent) CreateSession(_ context.Context, opts ...codingagent.SessionOption) (codingagent.Session, error) {
	cfg := codingagent.NewSessionConfig(opts...)
	a.mu.Lock()
	a.creates++
	a.cfgs = append(a.cfgs, cfg)
	a.mu.Unlock()
	return &reconnectSession{agent: a}, nil
}

type reconnectSession struct{ agent *reconnectAgent }

func (s *reconnectSession) ID() string { return s.agent.nativeID }
func (s *reconnectSession) Close() error {
	if !s.agent.sendDone.Load() {
		s.agent.earlyClose.Add(1)
	}
	s.agent.mu.Lock()
	s.agent.closes++
	s.agent.mu.Unlock()
	return nil
}
func (s *reconnectSession) Send(_ context.Context, _ string) (<-chan codingagent.StreamEvent, error) {
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
		if s.agent.failTimes > 0 && n <= s.agent.failTimes {
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

func newReconnectHTTP(t *testing.T, agent *reconnectAgent, retryCfg config.ProcessRetryConfig) *httptest.Server {
	t.Helper()
	srv := agentservice.New(agentservice.WithProcessRetry(retryCfg))
	srv.RegisterAgent(agent)
	srv.SetGatewayModels(
		[]llmgateway.ModelInfo{{Provider: "openai", Model: "gpt-4o"}},
		&llmgateway.ModelInfo{Provider: "openai", Model: "gpt-4o"},
	)
	return httptest.NewServer(srv.HTTPHandler())
}

func createReconnectSession(t *testing.T, ts *httptest.Server, agent string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"agent": agent, "work_dir": t.TempDir()})
	resp, err := http.Post(ts.URL+"/api/v1/sessions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		buf, _ := io.ReadAll(resp.Body)
		t.Fatalf("create %d %s", resp.StatusCode, buf)
	}
	var created map[string]string
	json.NewDecoder(resp.Body).Decode(&created)
	return created["session_id"]
}

func postReconnectSSE(t *testing.T, ts *httptest.Server, sessionID, prompt string) (body string, code int) {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{
		"content": []map[string]string{{"type": "text", "text": prompt}},
	})
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/sessions/"+sessionID+"/messages", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf, _ := io.ReadAll(resp.Body)
	return string(buf), resp.StatusCode
}

func sseErrorCount(raw string) int {
	n := 0
	sc := bufio.NewScanner(strings.NewReader(raw))
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "data: ") && strings.Contains(line, `"type":"error"`) {
			n++
		}
	}
	return n
}

func TestStreamReconnect_InProcessReconnectSucceeds(t *testing.T) {
	agent := &reconnectAgent{name: "codex", nativeID: "n1"}
	ts := newReconnectHTTP(t, agent, config.ProcessRetryConfig{MaxAttempts: 3})
	defer ts.Close()
	id := createReconnectSession(t, ts, "codex")
	body, code := postReconnectSSE(t, ts, id, "ping")
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if sseErrorCount(body) != 0 {
		t.Fatalf("unexpected SSE error: %s", body)
	}
	if !strings.Contains(body, `"type":"result"`) {
		t.Fatalf("missing result: %s", body)
	}
}

func TestStreamReconnect_ProcessRetrySameResume(t *testing.T) {
	agent := &reconnectAgent{name: "codex", nativeID: "codex-native", failTimes: 1}
	ts := newReconnectHTTP(t, agent, config.ProcessRetryConfig{MaxAttempts: 3})
	defer ts.Close()
	id := createReconnectSession(t, ts, "codex")
	body, code := postReconnectSSE(t, ts, id, "ping")
	if code != http.StatusOK {
		t.Fatalf("status %d %s", code, body)
	}
	if sseErrorCount(body) != 0 {
		t.Fatalf("unexpected terminal error: %s", body)
	}
	if agent.creates != 2 {
		t.Fatalf("creates = %d, want 2", agent.creates)
	}
	agent.mu.Lock()
	last := agent.cfgs[len(agent.cfgs)-1]
	agent.mu.Unlock()
	if last.AgentSessionID != "codex-native" {
		t.Fatalf("resume id = %q", last.AgentSessionID)
	}
}

func TestStreamReconnect_ExhaustedReturnsClassifiedError(t *testing.T) {
	agent := &reconnectAgent{name: "codex", nativeID: "n1", failTimes: 99}
	ts := newReconnectHTTP(t, agent, config.ProcessRetryConfig{MaxAttempts: 2})
	defer ts.Close()
	id := createReconnectSession(t, ts, "codex")
	body, code := postReconnectSSE(t, ts, id, "ping")
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if sseErrorCount(body) != 1 {
		t.Fatalf("error count = %d body=%s", sseErrorCount(body), body)
	}
	if !strings.Contains(body, "[upstream_overloaded]") {
		t.Fatalf("missing classified tag: %s", body)
	}
	if agent.creates != 2 {
		t.Fatalf("creates = %d, want 2", agent.creates)
	}
	_, code2 := postReconnectSSE(t, ts, id, "again")
	if code2 == http.StatusConflict {
		t.Fatal("session busy after classified failure")
	}
}

func TestStreamReconnect_ClientDisconnectDoesNotKillCLI(t *testing.T) {
	agent := &reconnectAgent{name: "codex", nativeID: "n1", delay: 400 * time.Millisecond}
	ts := newReconnectHTTP(t, agent, config.ProcessRetryConfig{MaxAttempts: 1})
	defer ts.Close()
	id := createReconnectSession(t, ts, "codex")
	ctx, cancel := context.WithCancel(context.Background())
	raw, _ := json.Marshal(map[string]any{
		"content": []map[string]string{{"type": "text", "text": "run"}},
	})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+"/api/v1/sessions/"+id+"/messages", bytes.NewReader(raw))
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
		t.Fatalf("CLI closed before turn finished, early=%d", agent.earlyClose.Load())
	}
	time.Sleep(400 * time.Millisecond)
	agent.mu.Lock()
	closes := agent.closes
	agent.mu.Unlock()
	if closes < 1 {
		t.Fatalf("Close = %d after drain", closes)
	}
}

func TestStreamReconnect_NonRetryableNoRetry(t *testing.T) {
	agent := &reconnectAgent{name: "codex", nativeID: "n1", nonRetry: true}
	ts := newReconnectHTTP(t, agent, config.ProcessRetryConfig{MaxAttempts: 3})
	defer ts.Close()
	id := createReconnectSession(t, ts, "codex")
	body, _ := postReconnectSSE(t, ts, id, "ping")
	if agent.creates != 1 {
		t.Fatalf("creates = %d, want 1", agent.creates)
	}
	if !strings.Contains(body, "unauthorized") {
		t.Fatalf("body = %s", body)
	}
}
