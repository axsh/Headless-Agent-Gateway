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
	"github.com/axsh/hag/config"
	"github.com/axsh/hag/llmgateway"
)

// mockCodingAgent implements CodingAgent for testing.
type mockCodingAgent struct {
	name string
}

func (m *mockCodingAgent) CreateSession(_ context.Context, _ ...codingagent.SessionOption) (codingagent.Session, error) {
	return &mockCodingSession{}, nil
}
func (m *mockCodingAgent) Name() string { return m.name }
func (m *mockCodingAgent) Close() error { return nil }

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

// newTestServerWithModels creates a test server with cached gateway models.
func newTestServerWithModels() (*agentservice.Server, http.Handler) {
	srv := agentservice.New()
	srv.RegisterAgent(&mockCodingAgent{name: "claudecode"})
	srv.SetGatewayModels(
		[]llmgateway.ModelInfo{
			{Provider: "anthropic", Model: "claude-sonnet-4-20250514"},
			{Provider: "openai", Model: "gpt-4o"},
		},
		&llmgateway.ModelInfo{Provider: "anthropic", Model: "claude-sonnet-4-20250514"},
	)
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

// T4: GET /api/v1/models returns models and default_model.
func TestHandleListModels(t *testing.T) {
	_, handler := newTestServerWithModels()

	req := httptest.NewRequest("GET", "/api/v1/models", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var body struct {
		Models       []llmgateway.ModelInfo `json:"models"`
		DefaultModel *llmgateway.ModelInfo  `json:"default_model"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("json decode error: %v", err)
	}
	if len(body.Models) != 2 {
		t.Errorf("models count = %d, want 2", len(body.Models))
	}
	if body.DefaultModel == nil {
		t.Fatal("default_model should not be nil")
	}
	if body.DefaultModel.Model != "claude-sonnet-4-20250514" {
		t.Errorf("default_model.model = %q, want %q", body.DefaultModel.Model, "claude-sonnet-4-20250514")
	}
}

// T5: POST /api/v1/sessions with invalid model returns 400.
func TestHandleCreateSession_InvalidModel(t *testing.T) {
	_, handler := newTestServerWithModels()

	body, _ := json.Marshal(map[string]string{
		"agent": "claudecode",
		"model": "gpt-5-turbo",
	})
	req := httptest.NewRequest("POST", "/api/v1/sessions", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}

	var errResp struct {
		Error           string   `json:"error"`
		AvailableModels []string `json:"available_models"`
	}
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("json decode error: %v", err)
	}
	if errResp.Error != "unsupported model: gpt-5-turbo" {
		t.Errorf("error = %q, want %q", errResp.Error, "unsupported model: gpt-5-turbo")
	}
	// All gateway models should be listed (no provider filtering).
	if len(errResp.AvailableModels) != 2 {
		t.Errorf("available_models count = %d, want 2 (all models)", len(errResp.AvailableModels))
	}
}

// T5b: POST /api/v1/sessions with model from different provider returns 201 (cross-provider).
func TestHandleCreateSession_CrossProvider(t *testing.T) {
	_, handler := newTestServerWithModels()

	body, _ := json.Marshal(map[string]string{
		"agent": "claudecode",
		"model": "gpt-4o", // exists in profiles, different provider but allowed
	})
	req := httptest.NewRequest("POST", "/api/v1/sessions", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Cross-provider models should be accepted.
	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201 (cross-provider model accepted)", w.Code)
	}
}

// T6: POST /api/v1/sessions with valid model returns 201.
func TestHandleCreateSession_ValidModel(t *testing.T) {
	_, handler := newTestServerWithModels()

	body, _ := json.Marshal(map[string]string{
		"agent": "claudecode",
		"model": "claude-sonnet-4-20250514",
	})
	req := httptest.NewRequest("POST", "/api/v1/sessions", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", w.Code)
	}
}

// T7: POST /api/v1/sessions with empty model returns 201 (skip validation).
func TestHandleCreateSession_EmptyModel(t *testing.T) {
	_, handler := newTestServerWithModels()

	body, _ := json.Marshal(map[string]string{
		"agent": "claudecode",
	})
	req := httptest.NewRequest("POST", "/api/v1/sessions", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", w.Code)
	}
}

// T8: POST /api/v1/sessions when gateway models are empty (fail-open).
func TestHandleCreateSession_NoGatewayModels(t *testing.T) {
	_, handler := newTestServer() // no models cached

	body, _ := json.Marshal(map[string]string{
		"agent": "claudecode",
		"model": "any-model-name",
	})
	req := httptest.NewRequest("POST", "/api/v1/sessions", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Should succeed (fail-open) since no gateway models are cached.
	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201 (fail-open when no gateway models)", w.Code)
	}
}

func TestResolveModel(t *testing.T) {
	srv := agentservice.New()
	srv.SetModelProfiles(&config.ModelProfilesConfig{
		Providers: map[string]config.ProviderConfig{
			"openai": {Keys: []config.KeyConfig{{
				Name: "default", Value: "vault://test",
				Models: []config.ModelConfig{
					{Name: "gpt-4o", LogicalName: "fast-coder"},
					{Name: "gpt-4o-mini"},
				},
			}}},
			"anthropic": {Keys: []config.KeyConfig{{
				Name: "default", Value: "vault://test",
				Models: []config.ModelConfig{
					{Name: "claude-sonnet-4-20250514", LogicalName: "balanced-coder"},
				},
			}}},
		},
	})

	tests := []struct {
		input     string
		wantModel string
		wantOK    bool
	}{
		{"fast-coder", "gpt-4o", true},
		{"balanced-coder", "claude-sonnet-4-20250514", true},
		{"gpt-4o", "gpt-4o", true},
		{"gpt-4o-mini", "gpt-4o-mini", true},
		{"claude-sonnet-4-20250514", "claude-sonnet-4-20250514", true},
		{"unknown-model", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			model, ok := srv.ResolveModel(tt.input)
			if ok != tt.wantOK {
				t.Errorf("ResolveModel(%q) ok = %v, want %v", tt.input, ok, tt.wantOK)
			}
			if model != tt.wantModel {
				t.Errorf("ResolveModel(%q) = %q, want %q", tt.input, model, tt.wantModel)
			}
		})
	}
}

func TestIsValidModelCrossProvider(t *testing.T) {
	_, handler := newTestServerWithModels()

	// gpt-4o (OpenAI model) should be valid for claudecode agent (no provider filter).
	body, _ := json.Marshal(map[string]string{
		"agent": "claudecode",
		"model": "gpt-4o",
	})
	req := httptest.NewRequest("POST", "/api/v1/sessions", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201 (cross-provider model should be accepted)", w.Code)
	}
}
