package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateSession(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/sessions" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var req SessionRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Agent != "claudecode" {
			t.Errorf("agent = %q, want %q", req.Agent, "claudecode")
		}
		if req.WorkDir != "/tmp/work" {
			t.Errorf("work_dir = %q, want %q", req.WorkDir, "/tmp/work")
		}

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"session_id": "test-session-123"})
	}))
	defer srv.Close()

	c := New(srv.URL)
	session, err := c.CreateSession(context.Background(), SessionRequest{
		Agent:   "claudecode",
		WorkDir: "/tmp/work",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if session.ID != "test-session-123" {
		t.Errorf("session ID = %q, want %q", session.ID, "test-session-123")
	}
}

func TestCreateSession_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"agent not found"}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, err := c.CreateSession(context.Background(), SessionRequest{
		Agent:   "unknown",
		WorkDir: "/tmp/work",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestTerminate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/sessions/test-123/terminate" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"terminated"}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	session := &Session{ID: "test-123", client: c}
	if err := session.Terminate(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHealth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer srv.Close()

	c := New(srv.URL)
	health, err := c.Health(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if health.Status != "ok" {
		t.Errorf("status = %q, want %q", health.Status, "ok")
	}
}

func TestListAgents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/agents" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode([]Agent{{Name: "claudecode"}, {Name: "codex"}})
	}))
	defer srv.Close()

	c := New(srv.URL)
	agents, err := c.ListAgents(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(agents) != 2 {
		t.Fatalf("got %d agents, want 2", len(agents))
	}
	if agents[0].Name != "claudecode" {
		t.Errorf("agents[0].Name = %q, want %q", agents[0].Name, "claudecode")
	}
}

func TestListModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/models" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(ModelsResponse{
			Models: []ModelInfo{
				{Provider: "anthropic", Model: "claude-sonnet"},
				{Provider: "openai", Model: "gpt-4"},
			},
			DefaultModel: &ModelInfo{Provider: "anthropic", Model: "claude-sonnet"},
		})
	}))
	defer srv.Close()

	c := New(srv.URL)
	models, err := c.ListModels(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models.Models) != 2 {
		t.Fatalf("got %d models, want 2", len(models.Models))
	}
	if models.DefaultModel == nil || models.DefaultModel.Model != "claude-sonnet" {
		t.Errorf("default model = %v, want claude-sonnet", models.DefaultModel)
	}
}

func TestGetSession_Typed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/sessions/s1" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":               "s1",
			"agent_name":       "claudecode",
			"status":           "active",
			"work_dir":         "/tmp/work",
			"session_dir":      "/tmp/session",
			"config_dir":       "/tmp/alpha",
			"agent_session_id": "agent-1",
		})
	}))
	defer srv.Close()

	c := New(srv.URL)
	info, err := c.GetSession(context.Background(), "s1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if info.ConfigDir != "/tmp/alpha" {
		t.Errorf("ConfigDir = %q, want /tmp/alpha", info.ConfigDir)
	}
	if info.ID != "s1" {
		t.Errorf("ID = %q, want s1", info.ID)
	}
}

func TestUpdateSessionConfigDir_Typed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/api/v1/sessions/s1" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":         "s1",
			"config_dir": "/tmp/beta",
		})
	}))
	defer srv.Close()

	c := New(srv.URL)
	info, err := c.UpdateSessionConfigDir(context.Background(), "s1", "/tmp/beta")
	if err != nil {
		t.Fatalf("UpdateSessionConfigDir: %v", err)
	}
	if info.ConfigDir != "/tmp/beta" {
		t.Errorf("ConfigDir = %q, want /tmp/beta", info.ConfigDir)
	}
}

func TestSession_UpdateConfigDir_Delegates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/api/v1/sessions/s1" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":         "s1",
			"config_dir": "/tmp/beta",
		})
	}))
	defer srv.Close()

	c := New(srv.URL)
	session := ResumeSession(c, "s1")
	info, err := session.UpdateConfigDir(context.Background(), "/tmp/beta")
	if err != nil {
		t.Fatalf("UpdateConfigDir: %v", err)
	}
	if info.ConfigDir != "/tmp/beta" {
		t.Errorf("ConfigDir = %q, want /tmp/beta", info.ConfigDir)
	}
}
