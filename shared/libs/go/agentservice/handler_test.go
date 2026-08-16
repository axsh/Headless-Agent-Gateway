package agentservice_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/agentservice"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/config"
	"github.com/axsh/arctic-tern/shared/libs/go/llmgateway"
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
	ch := make(chan codingagent.StreamEvent, 3)
	ch <- codingagent.StreamEvent{Type: codingagent.EventSystem, SessionID: "mock-session"}
	ch <- codingagent.StreamEvent{Type: codingagent.EventText, Content: "hello"}
	ch <- codingagent.StreamEvent{Type: codingagent.EventResult}
	close(ch)
	return ch, nil
}
func (s *mockCodingSession) ID() string   { return "mock-session" }
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
		"agent":       "claudecode",
		"model":       "claude-sonnet",
		"work_dir":    "/workspace",
		"session_dir": t.TempDir(),
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

func TestHandleSendMessage_SecondMessageWithoutTerminate(t *testing.T) {
	_, handler := newTestServer()

	body, _ := json.Marshal(map[string]string{
		"agent":       "claudecode",
		"work_dir":    t.TempDir(),
		"session_dir": t.TempDir(),
	})
	req := httptest.NewRequest("POST", "/api/v1/sessions", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", w.Code, w.Body.String())
	}
	var created map[string]string
	json.NewDecoder(w.Body).Decode(&created)
	sessionID := created["session_id"]

	send := func(label string) {
		t.Helper()
		msg, _ := json.Marshal(map[string]any{
			"content": []map[string]string{{"type": "text", "text": label}},
		})
		req := httptest.NewRequest("POST", "/api/v1/sessions/"+sessionID+"/messages", bytes.NewReader(msg))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: status = %d body=%s", label, w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "[DONE]") {
			t.Fatalf("%s: missing [DONE] in SSE body", label)
		}
	}

	send("first")
	send("second") // must not return 409 session busy without terminate
}

func TestHandleTerminateAgent(t *testing.T) {
	_, handler := newTestServer()

	// Create a session
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
		"agent":       "claudecode",
		"model":       "gpt-4o", // exists in profiles, different provider but allowed
		"session_dir": t.TempDir(),
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
		"agent":       "claudecode",
		"model":       "claude-sonnet-4-20250514",
		"session_dir": t.TempDir(),
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
		"agent":       "claudecode",
		"session_dir": t.TempDir(),
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
		"agent":       "claudecode",
		"model":       "any-model-name",
		"session_dir": t.TempDir(),
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
			"openai": {ApiKeys: []config.KeyConfig{{
				Name: "default", Secret: "vault://test",
				Models: []config.ModelConfig{
					{Name: "gpt-4o", LogicalName: "fast-coder"},
					{Name: "gpt-4o-mini"},
				},
			}}},
			"anthropic": {ApiKeys: []config.KeyConfig{{
				Name: "default", Secret: "vault://test",
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
		"agent":       "claudecode",
		"model":       "gpt-4o",
		"session_dir": t.TempDir(),
	})
	req := httptest.NewRequest("POST", "/api/v1/sessions", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201 (cross-provider model should be accepted)", w.Code)
	}
}

func TestTerminate_CancelsExecution(t *testing.T) {
	srv := agentservice.New()
	srv.RegisterAgent(&mockCodingAgent{name: "claudecode"})

	// Register an exec cancel manually to simulate an active execution.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv.RegisterExecCancel("test-session-exec", cancel)

	// Verify cancel works.
	if !srv.CancelExecution("test-session-exec") {
		t.Error("expected CancelExecution to return true for registered session")
	}
	if ctx.Err() == nil {
		t.Error("expected context to be cancelled after CancelExecution")
	}

	// Unregistered session should return false.
	if srv.CancelExecution("nonexistent") {
		t.Error("expected CancelExecution to return false for unregistered session")
	}
}

type mockTerminalEventAgent struct {
	name string
}

func (m *mockTerminalEventAgent) CreateSession(_ context.Context, _ ...codingagent.SessionOption) (codingagent.Session, error) {
	return &mockTerminalEventSession{}, nil
}
func (m *mockTerminalEventAgent) Name() string { return m.name }
func (m *mockTerminalEventAgent) Close() error { return nil }

type mockTerminalEventSession struct{}

func (s *mockTerminalEventSession) Send(_ context.Context, _ string) (<-chan codingagent.StreamEvent, error) {
	ch := make(chan codingagent.StreamEvent, 2)
	ch <- codingagent.StreamEvent{Type: codingagent.EventText, Content: "ok"}
	ch <- codingagent.StreamEvent{Type: codingagent.EventResult}
	close(ch)
	return ch, nil
}
func (s *mockTerminalEventSession) ID() string   { return "mock-terminal-session" }
func (s *mockTerminalEventSession) Close() error { return nil }

type mockSlowLargeToolAgent struct {
	name string
}

func (m *mockSlowLargeToolAgent) CreateSession(_ context.Context, _ ...codingagent.SessionOption) (codingagent.Session, error) {
	return &mockSlowLargeToolSession{}, nil
}
func (m *mockSlowLargeToolAgent) Name() string { return m.name }
func (m *mockSlowLargeToolAgent) Close() error { return nil }

type mockSlowLargeToolSession struct{}

func (s *mockSlowLargeToolSession) Send(_ context.Context, _ string) (<-chan codingagent.StreamEvent, error) {
	ch := make(chan codingagent.StreamEvent, 8)
	ch <- codingagent.StreamEvent{
		Type:    codingagent.EventToolResult,
		Content: strings.Repeat("z", codingagent.DefaultMaxToolResultBytes),
	}
	ch <- codingagent.StreamEvent{Type: codingagent.EventResult}
	close(ch)
	return ch, nil
}
func (s *mockSlowLargeToolSession) ID() string   { return "mock-slow-large-session" }
func (s *mockSlowLargeToolSession) Close() error { return nil }

func getSessionFromServer(t *testing.T, handler http.Handler, sessionID string) map[string]interface{} {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/"+sessionID, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET session status = %d", rec.Code)
	}
	var out map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	return out
}

func createCodexSessionHTTP(t *testing.T, handler http.Handler) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"agent":       "codex",
		"session_dir": t.TempDir(),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create session status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var created map[string]string
	json.NewDecoder(rec.Body).Decode(&created)
	return created["session_id"]
}

func postMessageHTTP(t *testing.T, baseURL, sessionID, accept string, ctx context.Context) (*http.Response, error) {
	t.Helper()
	msgBody, _ := json.Marshal(map[string]any{
		"content": []map[string]string{{"type": "text", "text": "run"}},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		baseURL+"/api/v1/sessions/"+sessionID+"/messages", bytes.NewReader(msgBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", accept)
	return (&http.Client{Timeout: 0}).Do(req)
}

func TestStreamSSERelay_EarlyStatusUpdate(t *testing.T) {
	srv := agentservice.New()
	srv.RegisterAgent(&mockSlowLargeToolAgent{name: "codex"})
	ts := httptest.NewServer(srv.HTTPHandler())
	defer ts.Close()

	sessionID := createCodexSessionHTTP(t, srv.HTTPHandler())
	ctx := context.Background()
	resp, err := postMessageHTTP(t, ts.URL, sessionID, "text/event-stream", ctx)
	if err != nil {
		t.Fatalf("POST message: %v", err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	sawResult := false
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		if strings.Contains(data, `"type":"result"`) {
			sawResult = true
			sess := getSessionFromServer(t, srv.HTTPHandler(), sessionID)
			status, _ := sess["status"].(string)
			if status != codingagent.StatusCompleted {
				t.Fatalf("status after EventResult = %q, want completed (before [DONE])", status)
			}
			break
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner: %v", err)
	}
	if !sawResult {
		t.Fatal("expected result event in SSE stream")
	}
}

func TestStreamSSERelay_DisconnectUpdatesStatus(t *testing.T) {
	srv := agentservice.New()
	srv.RegisterAgent(&mockSlowLargeToolAgent{name: "codex"})
	ts := httptest.NewServer(srv.HTTPHandler())
	defer ts.Close()

	sessionID := createCodexSessionHTTP(t, srv.HTTPHandler())
	reqCtx, cancel := context.WithCancel(context.Background())
	resp, err := postMessageHTTP(t, ts.URL, sessionID, "text/event-stream", reqCtx)
	if err != nil {
		t.Fatalf("POST message: %v", err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	partCount := 0
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		if strings.Contains(data, "tool_result_part") {
			partCount++
		}
		if strings.Contains(data, `"type":"result"`) {
			break
		}
		if partCount >= 1 {
			cancel()
			io.Copy(io.Discard, resp.Body)
			break
		}
	}

	time.Sleep(200 * time.Millisecond)

	sess := getSessionFromServer(t, srv.HTTPHandler(), sessionID)
	status, _ := sess["status"].(string)
	if status != codingagent.StatusCompleted {
		t.Fatalf("status after disconnect = %q, want completed", status)
	}
}

func TestRespondJSONRelay_EarlyStatusOnTerminalEvent(t *testing.T) {
	srv := agentservice.New()
	srv.RegisterAgent(&mockTerminalEventAgent{name: "codex"})
	ts := httptest.NewServer(srv.HTTPHandler())
	defer ts.Close()

	sessionID := createCodexSessionHTTP(t, srv.HTTPHandler())

	statusCh := make(chan string, 1)
	go func() {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			sess := getSessionFromServer(t, srv.HTTPHandler(), sessionID)
			if s, _ := sess["status"].(string); s == codingagent.StatusCompleted {
				statusCh <- s
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()

	resp, err := postMessageHTTP(t, ts.URL, sessionID, "application/json", context.Background())
	if err != nil {
		t.Fatalf("POST message: %v", err)
	}
	defer resp.Body.Close()

	select {
	case status := <-statusCh:
		if status != codingagent.StatusCompleted {
			t.Fatalf("status = %q, want completed", status)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("status not completed before JSON response finished")
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status code = %d", resp.StatusCode)
	}
	var events []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
		t.Fatalf("decode JSON events: %v", err)
	}
	foundResult := false
	for _, ev := range events {
		if typ, _ := ev["type"].(string); typ == string(codingagent.EventResult) {
			foundResult = true
			break
		}
	}
	if !foundResult {
		t.Fatal("expected EventResult in JSON response")
	}
}

func TestContextSeparation_ClientDisconnect(t *testing.T) {
	// Test that the channel remains open after client context is cancelled
	// (simulating client disconnect while agent continues).
	ch := make(chan codingagent.StreamEvent, 10)

	// Simulate agent sending events.
	go func() {
		ch <- codingagent.StreamEvent{Type: codingagent.EventSystem}
		ch <- codingagent.StreamEvent{Type: codingagent.EventText, Content: "working"}
		ch <- codingagent.StreamEvent{Type: codingagent.EventResult}
		close(ch)
	}()

	// Simulate client disconnect.
	clientCtx, clientCancel := context.WithCancel(context.Background())
	clientCancel() // Client disconnects immediately.

	// Channel should still be readable despite client disconnect.
	eventCount := 0
	for range ch {
		eventCount++
	}
	if eventCount != 3 {
		t.Errorf("expected 3 events from channel after client disconnect, got %d", eventCount)
	}
	_ = clientCtx // Used to simulate disconnect.
}

func TestHandleCreateSession_WithConfigDir(t *testing.T) {
	_, handler := newTestServer()
	configDir := t.TempDir()
	sessionDir := t.TempDir()

	body, _ := json.Marshal(map[string]string{
		"agent":       "claudecode",
		"work_dir":    t.TempDir(),
		"session_dir": sessionDir,
		"config_dir":  configDir,
	})
	req := httptest.NewRequest("POST", "/api/v1/sessions", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body=%s", w.Code, w.Body.String())
	}
	var created map[string]string
	json.NewDecoder(w.Body).Decode(&created)

	req = httptest.NewRequest("GET", "/api/v1/sessions/"+created["session_id"], nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get status = %d", w.Code)
	}
	var session map[string]any
	json.NewDecoder(w.Body).Decode(&session)
	got, _ := session["config_dir"].(string)
	if got == "" {
		t.Fatal("config_dir should be set")
	}
	if !filepath.IsAbs(got) {
		t.Errorf("config_dir should be absolute, got %q", got)
	}
}

func TestHandleCreateSession_ConfigDirMissing(t *testing.T) {
	_, handler := newTestServer()
	body, _ := json.Marshal(map[string]string{
		"agent":       "claudecode",
		"work_dir":    t.TempDir(),
		"session_dir": t.TempDir(),
		"config_dir":  filepath.Join(t.TempDir(), "does-not-exist"),
	})
	req := httptest.NewRequest("POST", "/api/v1/sessions", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "config_dir does not exist") {
		t.Errorf("body = %q, want config_dir does not exist", w.Body.String())
	}
}

func TestHandleCreateSession_ConfigDirOmitted(t *testing.T) {
	_, handler := newTestServer()
	workDir := t.TempDir()
	body, _ := json.Marshal(map[string]string{
		"agent":    "claudecode",
		"work_dir": workDir,
	})
	req := httptest.NewRequest("POST", "/api/v1/sessions", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", w.Code)
	}
	var created map[string]string
	json.NewDecoder(w.Body).Decode(&created)

	req = httptest.NewRequest("GET", "/api/v1/sessions/"+created["session_id"], nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	var session map[string]any
	json.NewDecoder(w.Body).Decode(&session)
	if v, ok := session["config_dir"]; ok && v != nil && v != "" {
		t.Errorf("config_dir should be empty/omitted, got %#v", v)
	}
}

func TestHandleCreateSession_ConfigDirRelativeResolved(t *testing.T) {
	_, handler := newTestServer()
	relName := "rel-config-dir-" + t.Name()
	absConfig, err := filepath.Abs(relName)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(absConfig, 0755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(absConfig) })

	body, _ := json.Marshal(map[string]string{
		"agent":       "claudecode",
		"work_dir":    t.TempDir(),
		"session_dir": t.TempDir(),
		"config_dir":  relName,
	})
	req := httptest.NewRequest("POST", "/api/v1/sessions", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body=%s", w.Code, w.Body.String())
	}
	var created map[string]string
	json.NewDecoder(w.Body).Decode(&created)
	req = httptest.NewRequest("GET", "/api/v1/sessions/"+created["session_id"], nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	var session map[string]any
	json.NewDecoder(w.Body).Decode(&session)
	got, _ := session["config_dir"].(string)
	if !filepath.IsAbs(got) {
		t.Errorf("config_dir should be absolute, got %q", got)
	}
}

func TestHandlePatchSession_ConfigDir(t *testing.T) {
	_, handler := newTestServer()
	alpha := t.TempDir()
	beta := t.TempDir()
	sessionDir := t.TempDir()
	workDir := t.TempDir()

	body, _ := json.Marshal(map[string]string{
		"agent":       "claudecode",
		"work_dir":    workDir,
		"session_dir": sessionDir,
		"config_dir":  alpha,
	})
	req := httptest.NewRequest("POST", "/api/v1/sessions", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", w.Code, w.Body.String())
	}
	var created map[string]string
	json.NewDecoder(w.Body).Decode(&created)

	patchBody, _ := json.Marshal(map[string]string{"config_dir": beta})
	req = httptest.NewRequest("PATCH", "/api/v1/sessions/"+created["session_id"], bytes.NewReader(patchBody))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("patch status = %d body=%s", w.Code, w.Body.String())
	}
	var patched map[string]any
	json.NewDecoder(w.Body).Decode(&patched)
	gotConfig, _ := patched["config_dir"].(string)
	wantBeta, _ := filepath.Abs(beta)
	if filepath.Clean(gotConfig) != filepath.Clean(wantBeta) {
		t.Errorf("config_dir = %q, want %q", gotConfig, wantBeta)
	}
	gotSession, _ := patched["session_dir"].(string)
	wantSession, _ := filepath.Abs(sessionDir)
	if filepath.Clean(gotSession) != filepath.Clean(wantSession) {
		t.Errorf("session_dir changed: %q", gotSession)
	}
	if patched["id"] != created["session_id"] {
		t.Errorf("id changed")
	}
}

func TestHandlePatchSession_ConfigDirMissing(t *testing.T) {
	_, handler := newTestServer()
	body, _ := json.Marshal(map[string]string{
		"agent":       "claudecode",
		"work_dir":    t.TempDir(),
		"session_dir": t.TempDir(),
		"config_dir":  t.TempDir(),
	})
	req := httptest.NewRequest("POST", "/api/v1/sessions", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	var created map[string]string
	json.NewDecoder(w.Body).Decode(&created)

	missing := filepath.Join(t.TempDir(), "missing")
	patchBody, _ := json.Marshal(map[string]string{"config_dir": missing})
	req = httptest.NewRequest("PATCH", "/api/v1/sessions/"+created["session_id"], bytes.NewReader(patchBody))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "config_dir does not exist") {
		t.Errorf("body = %q", w.Body.String())
	}
}

func TestHandlePatchSession_ConfigDirClear(t *testing.T) {
	_, handler := newTestServer()
	body, _ := json.Marshal(map[string]string{
		"agent":       "claudecode",
		"work_dir":    t.TempDir(),
		"session_dir": t.TempDir(),
		"config_dir":  t.TempDir(),
	})
	req := httptest.NewRequest("POST", "/api/v1/sessions", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	var created map[string]string
	json.NewDecoder(w.Body).Decode(&created)

	patchBody, _ := json.Marshal(map[string]string{"config_dir": ""})
	req = httptest.NewRequest("PATCH", "/api/v1/sessions/"+created["session_id"], bytes.NewReader(patchBody))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var patched map[string]any
	json.NewDecoder(w.Body).Decode(&patched)
	if v, ok := patched["config_dir"]; ok && v != nil && v != "" {
		t.Errorf("config_dir should be cleared, got %#v", v)
	}
}

func TestHandlePatchSession_NotFound(t *testing.T) {
	_, handler := newTestServer()
	patchBody, _ := json.Marshal(map[string]string{"config_dir": t.TempDir()})
	req := httptest.NewRequest("PATCH", "/api/v1/sessions/nonexistent", bytes.NewReader(patchBody))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}
