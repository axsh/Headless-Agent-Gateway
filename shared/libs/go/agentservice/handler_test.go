package agentservice_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/axsh/hag/agentservice"
	"github.com/axsh/hag/codingagent"
)

// mockCodingAgent implements CodingAgent for testing.
type mockCodingAgent struct {
	name string
}

func (m *mockCodingAgent) CreateSession(_ context.Context, _ ...codingagent.SessionOption) (codingagent.Session, error) {
	return &mockCodingSession{}, nil
}
func (m *mockCodingAgent) Name() string  { return m.name }
func (m *mockCodingAgent) Close() error  { return nil }

type mockCodingSession struct{}

func (s *mockCodingSession) Send(_ context.Context, _ string) (<-chan codingagent.StreamEvent, error) {
	ch := make(chan codingagent.StreamEvent, 2)
	ch <- codingagent.StreamEvent{Type: codingagent.EventText, Content: "hello"}
	ch <- codingagent.StreamEvent{Type: codingagent.EventResult}
	close(ch)
	return ch, nil
}
func (s *mockCodingSession) ID() string  { return "mock-session" }
func (s *mockCodingSession) Close() error { return nil }

func newTestServer() (*agentservice.Server, http.Handler) {
	srv := agentservice.New()
	srv.RegisterAgent(&mockCodingAgent{name: "claudecode"})
	return srv, srv.HTTPHandler()
}

func TestHandleListAgents(t *testing.T) {
	_, handler := newTestServer()

	req := httptest.NewRequest("GET", "/api/v1/agents", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var agents []struct {
		Name string `json:"name"`
	}
	json.NewDecoder(w.Body).Decode(&agents)
	if len(agents) != 1 || agents[0].Name != "claudecode" {
		t.Errorf("agents = %v, want [{claudecode}]", agents)
	}
}

func TestHandleCreateSession(t *testing.T) {
	_, handler := newTestServer()

	body, _ := json.Marshal(map[string]string{
		"agent":    "claudecode",
		"model":    "claude-sonnet",
		"work_dir": "/workspace",
	})
	req := httptest.NewRequest("POST", "/api/v1/sessions", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", w.Code)
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["session_id"] == "" {
		t.Error("session_id should not be empty")
	}
	if resp["status"] != "created" {
		t.Errorf("status = %v, want created", resp["status"])
	}
}

func TestHandleGetSession(t *testing.T) {
	srv, handler := newTestServer()

	// Create a session first
	body, _ := json.Marshal(map[string]string{"agent": "claudecode"})
	req := httptest.NewRequest("POST", "/api/v1/sessions", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var created map[string]string
	json.NewDecoder(w.Body).Decode(&created)
	sessionID := created["session_id"]

	// Get the session
	req = httptest.NewRequest("GET", "/api/v1/sessions/"+sessionID, nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	_ = srv // used for setup
}

func TestHandleGetSession_NotFound(t *testing.T) {
	_, handler := newTestServer()

	req := httptest.NewRequest("GET", "/api/v1/sessions/nonexistent", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleDeleteSession(t *testing.T) {
	_, handler := newTestServer()

	// Create a session
	body, _ := json.Marshal(map[string]string{"agent": "claudecode"})
	req := httptest.NewRequest("POST", "/api/v1/sessions", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var created map[string]string
	json.NewDecoder(w.Body).Decode(&created)
	sessionID := created["session_id"]

	// Delete the session
	req = httptest.NewRequest("DELETE", "/api/v1/sessions/"+sessionID, nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", w.Code)
	}

	// Verify deletion
	req = httptest.NewRequest("GET", "/api/v1/sessions/"+sessionID, nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("after delete: status = %d, want 404", w.Code)
	}
}

func TestHandleTerminateAgent(t *testing.T) {
	_, handler := newTestServer()

	// Create a session
	body, _ := json.Marshal(map[string]string{"agent": "claudecode"})
	req := httptest.NewRequest("POST", "/api/v1/sessions", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var created map[string]string
	json.NewDecoder(w.Body).Decode(&created)
	sessionID := created["session_id"]

	// Terminate
	req = httptest.NewRequest("POST", "/api/v1/sessions/"+sessionID+"/terminate", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	// Verify status changed
	req = httptest.NewRequest("GET", "/api/v1/sessions/"+sessionID, nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var record codingagent.SessionRecord
	json.NewDecoder(w.Body).Decode(&record)
	if record.Status != codingagent.StatusClosed {
		t.Errorf("status = %v, want closed", record.Status)
	}
}
