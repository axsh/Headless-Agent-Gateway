package agentservice_test

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/axsh/hag/agentservice"
	"github.com/axsh/hag/codingagent"
	"github.com/axsh/hag/tasklog"
)

func TestLogStreamSSE_SessionNotFound(t *testing.T) {
	srv := agentservice.New()
	handler := srv.HTTPHandler()

	req := httptest.NewRequest("GET", "/api/v1/sessions/nonexistent/logs", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestLogStreamSSE_CompletedSession(t *testing.T) {
	tl := tasklog.New()
	srv := agentservice.New(agentservice.WithTaskLog(tl))
	srv.RegisterAgent(&mockCodingAgent{name: "claudecode", providers: []string{"anthropic"}})
	handler := srv.HTTPHandler()

	// Create and complete a session
	createBody := strings.NewReader(`{"agent":"claudecode"}`)
	createReq := httptest.NewRequest("POST", "/api/v1/sessions", createBody)
	createW := httptest.NewRecorder()
	handler.ServeHTTP(createW, createReq)

	var created map[string]string
	json.NewDecoder(createW.Body).Decode(&created)
	sessionID := created["session_id"]

	// Manually update session to completed status via terminate
	termReq := httptest.NewRequest("POST", "/api/v1/sessions/"+sessionID+"/terminate", nil)
	termW := httptest.NewRecorder()
	handler.ServeHTTP(termW, termReq)

	// Request log stream for completed session
	req := httptest.NewRequest("GET", "/api/v1/sessions/"+sessionID+"/logs", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "event: status") {
		t.Error("response should contain status event")
	}
	if !strings.Contains(body, "terminated") {
		t.Error("response should contain terminated status")
	}
	if !strings.Contains(body, "[DONE]") {
		t.Error("response should contain [DONE] marker")
	}
}

func TestLogStreamSSE_ErrorSession(t *testing.T) {
	tl := tasklog.New()
	srv := agentservice.New(agentservice.WithTaskLog(tl))
	handler := srv.HTTPHandler()

	// Create session via direct store access (test helper)
	store := agentservice.NewMemorySessionStore()
	record := &codingagent.SessionRecord{
		ID:     "err-sess",
		Status: codingagent.StatusError,
	}
	store.Create(record)

	// Use a server with this store
	srvWithStore := agentservice.NewWithStore(store, agentservice.WithTaskLog(tl))
	handlerWithStore := srvWithStore.HTTPHandler()

	req := httptest.NewRequest("GET", "/api/v1/sessions/err-sess/logs", nil)
	w := httptest.NewRecorder()
	handlerWithStore.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "failed") {
		t.Error("error session should emit 'failed' status")
	}
	if !strings.Contains(body, "[DONE]") {
		t.Error("response should contain [DONE] marker")
	}

	_ = handler // used for type check
}

func TestLogStreamSSE_Snapshot(t *testing.T) {
	tl := tasklog.New()
	// Add some log entries before connecting
	tl.Add(&tasklog.AgentLogEntry{Body: "log entry 1", IsComplete: true})
	tl.Add(&tasklog.AgentLogEntry{Body: "log entry 2", IsComplete: true})
	tl.Add(&tasklog.AgentLogEntry{Body: "log entry 3", IsComplete: true})

	store := agentservice.NewMemorySessionStore()
	record := &codingagent.SessionRecord{
		ID:     "snap-sess",
		Status: codingagent.StatusCompleted,
	}
	store.Create(record)

	srv := agentservice.NewWithStore(store, agentservice.WithTaskLog(tl))
	handler := srv.HTTPHandler()

	req := httptest.NewRequest("GET", "/api/v1/sessions/snap-sess/logs", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	// Count log events
	scanner := bufio.NewScanner(strings.NewReader(body))
	logEventCount := 0
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: log") {
			logEventCount++
		}
	}

	if logEventCount != 3 {
		t.Errorf("expected 3 snapshot log events, got %d", logEventCount)
	}

	_ = time.Second // prevent unused import
}
