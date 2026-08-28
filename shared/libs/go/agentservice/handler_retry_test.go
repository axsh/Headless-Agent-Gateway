package agentservice_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	"github.com/axsh/arctic-tern/shared/libs/go/logger"
	"github.com/axsh/arctic-tern/shared/libs/go/wayfinder/portable"
)

type captureLogEntry struct {
	level string
	msg   string
	kv    []any
}

type captureLogger struct {
	mu      sync.Mutex
	entries []captureLogEntry
}

func (l *captureLogger) append(level, msg string, fields []any) {
	copied := append([]any(nil), fields...)
	l.mu.Lock()
	l.entries = append(l.entries, captureLogEntry{level: level, msg: msg, kv: copied})
	l.mu.Unlock()
}

func (l *captureLogger) Trace(msg string, fields ...any) { l.append("trace", msg, fields) }
func (l *captureLogger) Debug(msg string, fields ...any) { l.append("debug", msg, fields) }
func (l *captureLogger) Info(msg string, fields ...any)  { l.append("info", msg, fields) }
func (l *captureLogger) Warn(msg string, fields ...any)  { l.append("warn", msg, fields) }
func (l *captureLogger) Error(msg string, fields ...any) { l.append("error", msg, fields) }
func (l *captureLogger) WithFields(map[string]any) logger.Logger {
	return l
}
func (l *captureLogger) WithComponent(string) logger.Logger { return l }

func (l *captureLogger) find(level, msg string) (captureLogEntry, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, e := range l.entries {
		if e.level == level && e.msg == msg {
			return e, true
		}
	}
	return captureLogEntry{}, false
}

func kvLookup(kv []any, key string) (any, bool) {
	for i := 0; i+1 < len(kv); i += 2 {
		k, ok := kv[i].(string)
		if ok && k == key {
			return kv[i+1], true
		}
	}
	return nil, false
}

func kvString(t *testing.T, kv []any, key string) string {
	t.Helper()
	v, ok := kvLookup(kv, key)
	if !ok {
		t.Fatalf("missing log field %q", key)
	}
	return fmt.Sprint(v)
}

func kvBool(t *testing.T, kv []any, key string) bool {
	t.Helper()
	v, ok := kvLookup(kv, key)
	if !ok {
		t.Fatalf("missing log field %q", key)
	}
	b, ok := v.(bool)
	if !ok {
		t.Fatalf("field %q is %T, want bool", key, v)
	}
	return b
}

type retryAgent struct {
	name            string
	nativeID        string
	failTimes       int
	emptyError      bool
	nonRetry        bool
	delay           time.Duration
	failResumeID    string
	nextNativeID    string
	genericExit     bool
	hangAfterResult bool
	afterResult     []codingagent.StreamEvent
	lastPrompt      string
	mu              sync.Mutex
	creates         int
	closes          int
	cfgs            []*codingagent.SessionConfig
	sendDone        atomic.Bool
	earlyClose      atomic.Int32
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
func (s *retrySession) Send(_ context.Context, prompt string) (<-chan codingagent.StreamEvent, error) {
	ch := make(chan codingagent.StreamEvent, 4)
	s.agent.sendDone.Store(false)
	go func() {
		defer func() {
			if !s.agent.hangAfterResult {
				close(ch)
			}
			s.agent.sendDone.Store(true)
		}()
		if s.agent.delay > 0 {
			time.Sleep(s.agent.delay)
		}
		s.agent.mu.Lock()
		n := s.agent.creates
		s.agent.lastPrompt = prompt
		var cfg *codingagent.SessionConfig
		if len(s.agent.cfgs) > 0 {
			cfg = s.agent.cfgs[len(s.agent.cfgs)-1]
		}
		s.agent.mu.Unlock()
		resumeID := ""
		if cfg != nil {
			resumeID = cfg.AgentSessionID
		}
		if s.agent.nonRetry {
			ch <- codingagent.StreamEvent{Type: codingagent.EventError, Content: "unauthorized"}
			return
		}
		if s.agent.failResumeID != "" && resumeID == s.agent.failResumeID {
			ch <- codingagent.StreamEvent{
				Type:      codingagent.EventError,
				Content:   "exit status 1",
				Retryable: true,
			}
			return
		}
		if s.agent.failTimes > 0 && n <= s.agent.failTimes {
			failContent := "Reconnecting... 1/5 (We're currently experiencing high demand)"
			if s.agent.genericExit {
				failContent = "exit status 1"
			}
			if s.agent.emptyError {
				failContent = ""
			}
			if !s.agent.genericExit && !s.agent.emptyError {
				ch <- codingagent.StreamEvent{Type: codingagent.EventSystem, SessionID: s.agent.nativeID}
			}
			ch <- codingagent.StreamEvent{
				Type:      codingagent.EventError,
				Content:   failContent,
				Retryable: true,
			}
			return
		}
		sid := s.agent.nativeID
		if resumeID != "" {
			sid = resumeID
		} else if s.agent.nextNativeID != "" && n > 1 {
			sid = s.agent.nextNativeID
		}
		ch <- codingagent.StreamEvent{Type: codingagent.EventSystem, SessionID: sid}
		ch <- codingagent.StreamEvent{Type: codingagent.EventText, Content: "ok"}
		ch <- codingagent.StreamEvent{Type: codingagent.EventResult}
		for _, ev := range s.agent.afterResult {
			ch <- ev
		}
		if s.agent.hangAfterResult {
			select {}
		}
	}()
	return ch, nil
}

func newRetryHandler(t *testing.T, agent *retryAgent, retryCfg config.ProcessRetryConfig, opts ...agentservice.ServerOption) (*agentservice.Server, http.Handler) {
	t.Helper()
	all := append([]agentservice.ServerOption{agentservice.WithProcessRetry(retryCfg)}, opts...)
	srv := agentservice.New(all...)
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
	if last == nil || last.AgentSessionID != "" {
		t.Fatalf("second resume id = %+v, want empty after self-heal", last)
	}
	_ = srv
}

func TestHandleSendMessage_PostResultDrainClosesWhenSourceHangs(t *testing.T) {
	agent := &retryAgent{name: "codex", nativeID: "codex-native", hangAfterResult: true}
	logs := &captureLogger{}
	_, handler := newRetryHandler(t, agent, config.ProcessRetryConfig{MaxAttempts: 1},
		agentservice.WithLogger(logs),
		agentservice.WithSSEPostResultDrain(40*time.Millisecond),
	)
	sessionID := createCodexSessionHTTP(t, handler)
	start := time.Now()
	w := postSSE(t, handler, sessionID, "hello")
	elapsed := time.Since(start)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"type":"result"`) {
		t.Fatalf("expected EventResult, body=%s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "data: [DONE]") {
		t.Fatalf("expected [DONE] after post-result drain, body=%s", w.Body.String())
	}
	if elapsed > 2*time.Second {
		t.Fatalf("SSE hung too long despite drain: %s", elapsed)
	}
	if _, ok := logs.find("debug", "SSE post-EventResult drain elapsed; closing stream"); !ok {
		t.Fatalf("expected post-result drain debug log, entries=%v", logs.entries)
	}
}

func TestHandleSendMessage_PostResultDrainDeliversTrailingEvents(t *testing.T) {
	agent := &retryAgent{
		name:            "codex",
		nativeID:        "codex-native",
		hangAfterResult: true,
		afterResult: []codingagent.StreamEvent{
			{Type: codingagent.EventText, Content: "trailing"},
		},
	}
	_, handler := newRetryHandler(t, agent, config.ProcessRetryConfig{MaxAttempts: 1},
		agentservice.WithSSEPostResultDrain(80*time.Millisecond),
	)
	sessionID := createCodexSessionHTTP(t, handler)
	w := postSSE(t, handler, sessionID, "hello")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"content":"trailing"`) {
		t.Fatalf("expected trailing text before DONE, body=%s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "data: [DONE]") {
		t.Fatalf("expected [DONE], body=%s", w.Body.String())
	}
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

func TestHandleSendMessage_GenericExit1ExhaustedUpstreamError(t *testing.T) {
	agent := &retryAgent{name: "codex", nativeID: "n1", failTimes: 99, genericExit: true}
	_, handler := newRetryHandler(t, agent, config.ProcessRetryConfig{MaxAttempts: 2, IntervalSeconds: 0})
	sessionID := createCodexSessionHTTP(t, handler)
	w := postSSE(t, handler, sessionID, "hello")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	_, errCount := sseEvents(t, w.Body.String())
	if errCount != 1 {
		t.Fatalf("EventError count = %d, want 1 body=%s", errCount, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "[upstream_error]") {
		t.Fatalf("missing [upstream_error]: %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "[upstream_overloaded]") {
		t.Fatalf("unexpected [upstream_overloaded]: %s", w.Body.String())
	}
	w2 := postSSE(t, handler, sessionID, "again")
	if w2.Code == http.StatusConflict {
		t.Fatal("session still busy after classified failure")
	}
}

func TestHandleSendMessage_GenericExit1ExhaustedLogsCause(t *testing.T) {
	logs := &captureLogger{}
	agent := &retryAgent{name: "codex", nativeID: "n1", failTimes: 99, genericExit: true}
	_, handler := newRetryHandler(t, agent, config.ProcessRetryConfig{MaxAttempts: 3, IntervalSeconds: 0},
		agentservice.WithLogger(logs))
	sessionID := createCodexSessionHTTP(t, handler)
	w := postSSE(t, handler, sessionID, "hello")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	_, errCount := sseEvents(t, w.Body.String())
	if errCount != 1 {
		t.Fatalf("EventError count = %d, want 1 body=%s", errCount, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "[upstream_error]") {
		t.Fatalf("missing [upstream_error]: %s", w.Body.String())
	}
	entry, ok := logs.find("error", "codex process retry exhausted")
	if !ok {
		t.Fatalf("missing ERROR log %q; entries=%v", "codex process retry exhausted", logs.entries)
	}
	if kvString(t, entry.kv, "session_id") != sessionID {
		t.Fatalf("session_id = %q, want %q", kvString(t, entry.kv, "session_id"), sessionID)
	}
	if kvString(t, entry.kv, "attempt") != "3" {
		t.Fatalf("attempt = %q, want 3", kvString(t, entry.kv, "attempt"))
	}
	if kvString(t, entry.kv, "max_attempts") != "3" {
		t.Fatalf("max_attempts = %q, want 3", kvString(t, entry.kv, "max_attempts"))
	}
	if kvString(t, entry.kv, "resume_mode") != "fresh" {
		t.Fatalf("resume_mode = %q, want fresh", kvString(t, entry.kv, "resume_mode"))
	}
	if kvString(t, entry.kv, "agent_session_id") != "" {
		t.Fatalf("agent_session_id = %q, want empty", kvString(t, entry.kv, "agent_session_id"))
	}
	if !kvBool(t, entry.kv, "agent_session_id_empty") {
		t.Fatal("agent_session_id_empty = false, want true")
	}
	stderr := kvString(t, entry.kv, "stderr")
	if !strings.Contains(stderr, "exit status 1") {
		t.Fatalf("stderr = %q, want exit status 1", stderr)
	}
	if kvBool(t, entry.kv, "stderr_empty") {
		t.Fatal("stderr_empty = true, want false")
	}
	if kvString(t, entry.kv, "exit_status") != "1" {
		t.Fatalf("exit_status = %q, want 1", kvString(t, entry.kv, "exit_status"))
	}
}

func TestHandleSendMessage_BrokenResumeExhaustedLogsResumeMode(t *testing.T) {
	logs := &captureLogger{}
	agent := &retryAgent{name: "codex", nativeID: "thr-broken", nextNativeID: "thr-fresh"}
	_, handler := newRetryHandler(t, agent, config.ProcessRetryConfig{MaxAttempts: 3, IntervalSeconds: 0},
		agentservice.WithLogger(logs))
	sessionID := createCodexSessionHTTP(t, handler)
	if w := postSSE(t, handler, sessionID, "turn 1"); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"type":"result"`) {
		t.Fatalf("turn 1 failed: %d %s", w.Code, w.Body.String())
	}
	agent.mu.Lock()
	agent.failResumeID = "thr-broken"
	agent.failTimes = 99
	agent.genericExit = true
	agent.mu.Unlock()
	w := postSSE(t, handler, sessionID, "turn 2")
	if w.Code != http.StatusOK {
		t.Fatalf("turn 2 status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "[upstream_error]") {
		t.Fatalf("missing classified error: %s", w.Body.String())
	}
	entry, ok := logs.find("error", "codex process retry exhausted")
	if !ok {
		t.Fatalf("missing exhaust ERROR; entries=%v", logs.entries)
	}
	if kvString(t, entry.kv, "resume_mode") != "fresh" {
		t.Fatalf("resume_mode = %q, want fresh after self-heal", kvString(t, entry.kv, "resume_mode"))
	}
}

func TestHandleSendMessage_ResumeAttemptExhaustedLogsResumeMode(t *testing.T) {
	logs := &captureLogger{}
	agent := &retryAgent{name: "codex", nativeID: "thr-keep"}
	_, handler := newRetryHandler(t, agent, config.ProcessRetryConfig{MaxAttempts: 1, IntervalSeconds: 0},
		agentservice.WithLogger(logs))
	sessionID := createCodexSessionHTTP(t, handler)
	if w := postSSE(t, handler, sessionID, "turn 1"); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"type":"result"`) {
		t.Fatalf("turn 1 failed: %d %s", w.Code, w.Body.String())
	}
	agent.mu.Lock()
	agent.failTimes = 99
	agent.genericExit = true
	agent.mu.Unlock()
	w := postSSE(t, handler, sessionID, "turn 2")
	if w.Code != http.StatusOK {
		t.Fatalf("turn 2 status=%d body=%s", w.Code, w.Body.String())
	}
	entry, ok := logs.find("error", "codex process retry exhausted")
	if !ok {
		t.Fatalf("missing exhaust ERROR; entries=%v", logs.entries)
	}
	if kvString(t, entry.kv, "resume_mode") != "resume" {
		t.Fatalf("resume_mode = %q, want resume", kvString(t, entry.kv, "resume_mode"))
	}
	if kvString(t, entry.kv, "agent_session_id") != "thr-keep" {
		t.Fatalf("agent_session_id = %q, want thr-keep", kvString(t, entry.kv, "agent_session_id"))
	}
	if kvBool(t, entry.kv, "agent_session_id_empty") {
		t.Fatal("agent_session_id_empty = true, want false")
	}
}

func TestHandleSendMessage_EmptyStderrExhaustedLogsEmptyNote(t *testing.T) {
	logs := &captureLogger{}
	agent := &retryAgent{name: "codex", nativeID: "n1", failTimes: 99, genericExit: true, emptyError: true}
	_, handler := newRetryHandler(t, agent, config.ProcessRetryConfig{MaxAttempts: 2, IntervalSeconds: 0},
		agentservice.WithLogger(logs))
	sessionID := createCodexSessionHTTP(t, handler)
	w := postSSE(t, handler, sessionID, "hello")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	entry, ok := logs.find("error", "codex process retry exhausted")
	if !ok {
		t.Fatalf("missing exhaust ERROR; entries=%v", logs.entries)
	}
	if kvString(t, entry.kv, "stderr") != "" {
		t.Fatalf("stderr = %q, want empty", kvString(t, entry.kv, "stderr"))
	}
	if !kvBool(t, entry.kv, "stderr_empty") {
		t.Fatal("stderr_empty = false, want true")
	}
}

func TestStreamSSERelay_DisconnectLogsClientGone(t *testing.T) {
	logs := &captureLogger{}
	agent := &retryAgent{name: "codex", nativeID: "n1", delay: 400 * time.Millisecond}
	srv := agentservice.New(
		agentservice.WithProcessRetry(config.ProcessRetryConfig{MaxAttempts: 1}),
		agentservice.WithLogger(logs),
	)
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
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := logs.find("warn", "client disconnected during SSE stream"); ok {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	entry, ok := logs.find("warn", "client disconnected during SSE stream")
	if !ok {
		t.Fatalf("missing disconnect Warn; entries=%v", logs.entries)
	}
	if kvString(t, entry.kv, "session_id") != sessionID {
		t.Fatalf("session_id = %q, want %q", kvString(t, entry.kv, "session_id"), sessionID)
	}
}

func TestStreamSSERelay_DrainTimeoutLogsClientDrain(t *testing.T) {
	logs := &captureLogger{}
	agent := &retryAgent{name: "codex", nativeID: "n1", delay: 10 * time.Second}
	srv, handler := newRetryHandler(t, agent, config.ProcessRetryConfig{MaxAttempts: 1},
		agentservice.WithSSEDrainTimeout(80*time.Millisecond),
		agentservice.WithLogger(logs))
	ts := httptest.NewServer(handler)
	defer ts.Close()

	sessionID := createCodexSessionHTTP(t, handler)
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
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	resp, err := http.DefaultClient.Do(req)
	if err == nil {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := logs.find("warn", "SSE drain timed out; stopping agent process"); ok {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	entry, ok := logs.find("warn", "SSE drain timed out; stopping agent process")
	if !ok {
		t.Fatalf("missing drain Warn; entries=%v", logs.entries)
	}
	if kvString(t, entry.kv, "session_id") != sessionID {
		t.Fatalf("session_id = %q, want %q", kvString(t, entry.kv, "session_id"), sessionID)
	}
	if kvString(t, entry.kv, "timeout") == "" {
		t.Fatal("missing timeout field")
	}
	_ = srv
}

func TestHandleSendMessage_BrokenResumeThreadSelfHeals(t *testing.T) {
	agent := &retryAgent{
		name:         "codex",
		nativeID:     "thr-broken",
		failResumeID: "thr-broken",
		nextNativeID: "thr-fresh",
	}
	_, handler := newRetryHandler(t, agent, config.ProcessRetryConfig{MaxAttempts: 3, IntervalSeconds: 0})
	sessionID := createCodexSessionHTTP(t, handler)

	w1 := postSSE(t, handler, sessionID, "turn 1")
	if w1.Code != http.StatusOK || strings.Contains(w1.Body.String(), `"type":"error"`) || !strings.Contains(w1.Body.String(), `"type":"result"`) {
		t.Fatalf("turn 1 failed: code=%d body=%s", w1.Code, w1.Body.String())
	}

	w2 := postSSE(t, handler, sessionID, "turn 2")
	if w2.Code != http.StatusOK {
		t.Fatalf("turn 2 status=%d body=%s", w2.Code, w2.Body.String())
	}
	_, errCount := sseEvents(t, w2.Body.String())
	if errCount != 0 {
		t.Fatalf("turn 2 SSE errors=%d body=%s", errCount, w2.Body.String())
	}
	if !strings.Contains(w2.Body.String(), `"type":"result"`) {
		t.Fatalf("turn 2 missing result: %s", w2.Body.String())
	}
	agent.mu.Lock()
	cfgs := append([]*codingagent.SessionConfig{}, agent.cfgs...)
	creates := agent.creates
	agent.mu.Unlock()
	if creates != 3 {
		t.Fatalf("creates after turn 2 = %d, want 3 (1 + resume fail + fresh)", creates)
	}
	if cfgs[1].AgentSessionID != "thr-broken" {
		t.Fatalf("turn 2 first attempt resume = %q, want thr-broken", cfgs[1].AgentSessionID)
	}
	if cfgs[2].AgentSessionID != "" {
		t.Fatalf("turn 2 self-heal resume = %q, want empty", cfgs[2].AgentSessionID)
	}

	w3 := postSSE(t, handler, sessionID, "turn 3")
	if w3.Code != http.StatusOK || sseErrorCountFromRetry(t, w3.Body.String()) != 0 || !strings.Contains(w3.Body.String(), `"type":"result"`) {
		t.Fatalf("turn 3 failed: code=%d body=%s", w3.Code, w3.Body.String())
	}
	if agent.creates != 4 {
		t.Fatalf("creates = %d, want 4", agent.creates)
	}
	last := agent.lastCfg()
	if last == nil || last.AgentSessionID != "thr-fresh" {
		t.Fatalf("turn 3 resume = %+v, want thr-fresh", last)
	}
}

func sseErrorCountFromRetry(t *testing.T, raw string) int {
	t.Helper()
	_, n := sseEvents(t, raw)
	return n
}

func TestHandleSendMessage_SelfHealWrapsCanonicalHistory(t *testing.T) {
	agent := &retryAgent{
		name:         "codex",
		nativeID:     "thr-broken",
		failResumeID: "thr-broken",
		nextNativeID: "thr-fresh",
	}
	_, handler := newRetryHandler(t, agent, config.ProcessRetryConfig{MaxAttempts: 3, IntervalSeconds: 0})
	sessionID := createCodexSessionHTTP(t, handler)
	if w := postSSE(t, handler, sessionID, "remember SECRET-FACT"); w.Code != http.StatusOK {
		t.Fatalf("turn 1: %d %s", w.Code, w.Body.String())
	}
	if w := postSSE(t, handler, sessionID, "what was the fact?"); w.Code != http.StatusOK {
		t.Fatalf("turn 2: %d %s", w.Code, w.Body.String())
	}
	agent.mu.Lock()
	prompt := agent.lastPrompt
	cfgPrompt := ""
	if len(agent.cfgs) > 0 {
		cfgPrompt = agent.cfgs[len(agent.cfgs)-1].Prompt
	}
	agent.mu.Unlock()
	if !strings.Contains(prompt, portable.TransferHeader) && !strings.Contains(cfgPrompt, portable.TransferHeader) {
		t.Fatalf("self-heal prompt missing transfer header: send=%q cfg=%q", prompt, cfgPrompt)
	}
	if strings.Count(prompt, "what was the fact?") > 1 {
		t.Fatalf("current user prompt duplicated: %s", prompt)
	}
}

func TestStreamSSERelay_DrainTimeoutStopsProcess(t *testing.T) {
	agent := &retryAgent{name: "codex", nativeID: "n1", delay: 10 * time.Second}
	srv, handler := newRetryHandler(t, agent, config.ProcessRetryConfig{MaxAttempts: 1},
		agentservice.WithSSEDrainTimeout(80*time.Millisecond))
	ts := httptest.NewServer(handler)
	defer ts.Close()

	sessionID := createCodexSessionHTTP(t, handler)
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
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	resp, err := http.DefaultClient.Do(req)
	if err == nil {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	deadline := time.Now().Add(500 * time.Millisecond)
	var closes int
	for time.Now().Before(deadline) {
		agent.mu.Lock()
		closes = agent.closes
		agent.mu.Unlock()
		if closes >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if closes < 1 {
		t.Fatalf("Close count = %d, want >= 1 after drain timeout", closes)
	}
	deadline2 := time.Now().Add(1 * time.Second)
	var nextCode int
	for time.Now().Before(deadline2) {
		w := postSSE(t, srv.HTTPHandler(), sessionID, "next")
		nextCode = w.Code
		if nextCode != http.StatusConflict {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if nextCode == http.StatusConflict {
		t.Fatal("session still busy after drain timeout")
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

type toolResultOnlyAgent struct {
	name  string
	delay time.Duration
}

func (a *toolResultOnlyAgent) Name() string { return a.name }
func (a *toolResultOnlyAgent) Close() error { return nil }
func (a *toolResultOnlyAgent) CreateSession(_ context.Context, _ ...codingagent.SessionOption) (codingagent.Session, error) {
	return &toolResultOnlySession{delay: a.delay}, nil
}

type toolResultOnlySession struct {
	delay time.Duration
}

func (s *toolResultOnlySession) ID() string   { return "recover-native" }
func (s *toolResultOnlySession) Close() error { return nil }
func (s *toolResultOnlySession) Send(_ context.Context, _ string) (<-chan codingagent.StreamEvent, error) {
	ch := make(chan codingagent.StreamEvent, 4)
	go func() {
		defer close(ch)
		ch <- codingagent.StreamEvent{
			Type:     codingagent.EventToolUse,
			ToolName: "command_execution",
		}
		if s.delay > 0 {
			time.Sleep(s.delay)
		}
		ch <- codingagent.StreamEvent{
			Type:    codingagent.EventToolResult,
			Content: "Rejected(\"rm -f style commands are not permitted\")",
		}
	}()
	return ch, nil
}

func TestSessionRecoverTerminal_PostMessagesGetsSyntheticError(t *testing.T) {
	agent := &toolResultOnlyAgent{name: "codex"}
	srv := agentservice.New()
	srv.RegisterAgent(agent)
	handler := srv.HTTPHandler()
	sessionID := createCodexSessionHTTP(t, handler)
	w := postSSE(t, handler, sessionID, "run")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "[DONE]") {
		t.Fatalf("missing DONE in body: %s", body)
	}
	_, errCount := sseEvents(t, body)
	if errCount == 0 {
		t.Fatalf("expected synthetic terminal error on SSE, body: %s", body)
	}
}
