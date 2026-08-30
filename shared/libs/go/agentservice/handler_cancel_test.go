package agentservice_test

import (
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

type cancelHangAgent struct {
	closes atomic.Int32
	mu     sync.Mutex
	sess   *cancelHangSession
}

func (a *cancelHangAgent) Name() string { return "codex" }
func (a *cancelHangAgent) Close() error { return nil }
func (a *cancelHangAgent) CreateSession(_ context.Context, _ ...codingagent.SessionOption) (codingagent.Session, error) {
	s := &cancelHangSession{agent: a}
	a.mu.Lock()
	a.sess = s
	a.mu.Unlock()
	return s, nil
}

type cancelHangSession struct {
	agent *cancelHangAgent
	close atomic.Bool
}

func (s *cancelHangSession) ID() string { return "cancel-native" }
func (s *cancelHangSession) Close() error {
	s.close.Store(true)
	s.agent.closes.Add(1)
	return nil
}
func (s *cancelHangSession) Send(ctx context.Context, _ string) (<-chan codingagent.StreamEvent, error) {
	ch := make(chan codingagent.StreamEvent, 4)
	go func() {
		defer close(ch)
		ch <- codingagent.StreamEvent{
			Type:     codingagent.EventToolUse,
			ToolName: "command_execution",
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
			ch <- codingagent.StreamEvent{Type: codingagent.EventToolResult, Content: "late"}
			ch <- codingagent.StreamEvent{Type: codingagent.EventResult}
		}
	}()
	return ch, nil
}

func TestHandleSendMessage_ToolHeartbeatOnSSE(t *testing.T) {
	agent := &toolResultOnlyAgent{name: "codex", delay: 150 * time.Millisecond}
	srv := agentservice.New(
		agentservice.WithProcessRetry(config.ProcessRetryConfig{MaxAttempts: 1}),
		agentservice.WithToolHeartbeatInterval(40*time.Millisecond),
	)
	srv.RegisterAgent(agent)
	handler := srv.HTTPHandler()
	sessionID := createCodexSessionHTTP(t, handler)
	w := postSSE(t, handler, sessionID, "run")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `"type":"progress"`) {
		t.Fatalf("missing progress heartbeat in SSE: %s", body)
	}
	if !strings.Contains(body, "tool_still_running") {
		t.Fatalf("missing tool_still_running content: %s", body)
	}
	if !strings.Contains(body, `"tool_name":"command_execution"`) {
		t.Fatalf("missing tool_name on progress: %s", body)
	}
}

func TestHandleCancel_KeepsSessionIDNonClosed(t *testing.T) {
	agent := &cancelHangAgent{}
	srv := agentservice.New(
		agentservice.WithProcessRetry(config.ProcessRetryConfig{MaxAttempts: 1}),
		agentservice.WithToolHeartbeatInterval(50*time.Millisecond),
	)
	srv.RegisterAgent(agent)
	ts := httptest.NewServer(srv.HTTPHandler())
	defer ts.Close()

	sessionID := createCodexSessionHTTP(t, srv.HTTPHandler())

	msgBody, _ := json.Marshal(map[string]any{
		"content": []map[string]string{{"type": "text", "text": "run long tool"}},
	})
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/sessions/"+sessionID+"/messages", bytes.NewReader(msgBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept", "text/event-stream")

	done := make(chan struct{})
	go func() {
		defer close(done)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	// Wait until the in-flight turn is registered (followable), not merely
	// session status active from CreateSession.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		getReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/sessions/"+sessionID, nil)
		getResp, err := http.DefaultClient.Do(getReq)
		if err == nil {
			var info map[string]any
			_ = json.NewDecoder(getResp.Body).Decode(&info)
			getResp.Body.Close()
			if followable, _ := info["followable"].(bool); followable {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}

	cancelReq, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/sessions/"+sessionID+"/cancel", nil)
	if err != nil {
		t.Fatal(err)
	}
	cancelResp, err := http.DefaultClient.Do(cancelReq)
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	body, _ := io.ReadAll(cancelResp.Body)
	cancelResp.Body.Close()
	if cancelResp.StatusCode != http.StatusOK {
		t.Fatalf("cancel status=%d body=%s", cancelResp.StatusCode, body)
	}
	if !strings.Contains(string(body), `"cancelled"`) {
		t.Fatalf("cancel body=%s", body)
	}

	getReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/sessions/"+sessionID, nil)
	getResp, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatal(err)
	}
	defer getResp.Body.Close()
	var info map[string]any
	if err := json.NewDecoder(getResp.Body).Decode(&info); err != nil {
		t.Fatal(err)
	}
	if id, _ := info["id"].(string); id != sessionID {
		t.Fatalf("session id = %q, want same %q", id, sessionID)
	}
	status, _ := info["status"].(string)
	if status == codingagent.StatusClosed {
		t.Fatal("status must not be closed after cancel")
	}
	if status == codingagent.StatusActive || status == codingagent.StatusSuspended {
		t.Fatalf("status = %q, want non-active after cancel", status)
	}

	closeDeadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(closeDeadline) && agent.closes.Load() == 0 {
		time.Sleep(20 * time.Millisecond)
	}
	if agent.closes.Load() == 0 {
		t.Fatal("expected best-effort agent session Close after cancel")
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("SSE request did not finish after cancel")
	}

	// Same session id must accept a new message (not busy / not destroyed).
	w := postSSE(t, srv.HTTPHandler(), sessionID, "next turn")
	if w.Code == http.StatusConflict {
		t.Fatalf("session still busy after cancel: %s", w.Body.String())
	}
	if w.Code == http.StatusNotFound {
		t.Fatal("session id destroyed after cancel")
	}
}

func TestHandleCancel_DoesNotCloseLikeTerminate(t *testing.T) {
	_, handler := newTestServer()
	body, _ := json.Marshal(map[string]string{
		"agent":       "claudecode",
		"session_dir": t.TempDir(),
	})
	req := httptest.NewRequest("POST", "/api/v1/sessions", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	var created map[string]string
	json.NewDecoder(w.Body).Decode(&created)
	sessionID := created["session_id"]

	req = httptest.NewRequest("POST", "/api/v1/sessions/"+sessionID+"/cancel", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("cancel status=%d body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest("GET", "/api/v1/sessions/"+sessionID, nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	var record codingagent.SessionRecord
	json.NewDecoder(w.Body).Decode(&record)
	if record.ID != sessionID {
		t.Fatalf("id = %q, want %q", record.ID, sessionID)
	}
	if record.Status == codingagent.StatusClosed {
		t.Fatal("cancel must not set closed")
	}
	if record.Status == codingagent.StatusActive {
		t.Fatal("cancel should leave non-active status")
	}
}
