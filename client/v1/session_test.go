package v1_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	v1 "github.com/axsh/arctic-tern/client/v1"
)

func TestGetSession_Typed(t *testing.T) {
	created := time.Date(2026, 8, 5, 1, 2, 3, 0, time.UTC)
	updated := time.Date(2026, 8, 5, 4, 5, 6, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/sessions/s1" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":               "s1",
			"agent_name":       "claudecode",
			"model":            "claude-sonnet",
			"status":           "active",
			"error":            "",
			"work_dir":         "/tmp/work",
			"agent_session_id": "agent-1",
			"session_dir":      "/tmp/session",
			"config_dir":       "/tmp/alpha",
			"created_at":       created,
			"updated_at":       updated,
		})
	}))
	defer srv.Close()

	c := v1.New(srv.URL)
	info, err := c.GetSession(context.Background(), "s1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if info == nil {
		t.Fatal("expected SessionInfo, got nil")
	}
	if info.ID != "s1" {
		t.Errorf("ID = %q, want s1", info.ID)
	}
	if info.AgentName != "claudecode" {
		t.Errorf("AgentName = %q", info.AgentName)
	}
	if info.ConfigDir != "/tmp/alpha" {
		t.Errorf("ConfigDir = %q, want /tmp/alpha", info.ConfigDir)
	}
	if info.SessionDir != "/tmp/session" {
		t.Errorf("SessionDir = %q", info.SessionDir)
	}
	if info.AgentSessionID != "agent-1" {
		t.Errorf("AgentSessionID = %q", info.AgentSessionID)
	}
	if info.WorkDir != "/tmp/work" {
		t.Errorf("WorkDir = %q", info.WorkDir)
	}
	if info.Status != "active" {
		t.Errorf("Status = %q", info.Status)
	}
	if info.Model != "claude-sonnet" {
		t.Errorf("Model = %q", info.Model)
	}
}

func TestGetSession_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not found"}`))
	}))
	defer srv.Close()

	c := v1.New(srv.URL)
	info, err := c.GetSession(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if info != nil {
		t.Fatalf("expected nil info, got %+v", info)
	}
}

func TestUpdateSessionConfigDir_Typed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/api/v1/sessions/s1" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		var payload map[string]string
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if payload["config_dir"] != "/tmp/beta" {
			t.Fatalf("config_dir = %q, want /tmp/beta", payload["config_dir"])
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":          "s1",
			"agent_name":  "claudecode",
			"status":      "active",
			"work_dir":    "/tmp/work",
			"session_dir": "/tmp/session",
			"config_dir":  "/tmp/beta",
		})
	}))
	defer srv.Close()

	c := v1.New(srv.URL)
	info, err := c.UpdateSessionConfigDir(context.Background(), "s1", "/tmp/beta")
	if err != nil {
		t.Fatalf("UpdateSessionConfigDir: %v", err)
	}
	if info.ID != "s1" {
		t.Errorf("ID = %q, want s1", info.ID)
	}
	if info.ConfigDir != "/tmp/beta" {
		t.Errorf("ConfigDir = %q, want /tmp/beta", info.ConfigDir)
	}
}

func TestUpdateSessionConfigDir_ClearEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload map[string]string
		_ = json.Unmarshal(body, &payload)
		if payload["config_dir"] != "" {
			t.Fatalf("config_dir = %q, want empty", payload["config_dir"])
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":         "s1",
			"status":     "active",
			"config_dir": "",
		})
	}))
	defer srv.Close()

	c := v1.New(srv.URL)
	info, err := c.UpdateSessionConfigDir(context.Background(), "s1", "")
	if err != nil {
		t.Fatalf("UpdateSessionConfigDir: %v", err)
	}
	if info.ConfigDir != "" {
		t.Errorf("ConfigDir = %q, want empty", info.ConfigDir)
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

	c := v1.New(srv.URL)
	session := v1.ResumeSession(c, "s1")
	info, err := session.UpdateConfigDir(context.Background(), "/tmp/beta")
	if err != nil {
		t.Fatalf("UpdateConfigDir: %v", err)
	}
	if info.ConfigDir != "/tmp/beta" {
		t.Errorf("ConfigDir = %q, want /tmp/beta", info.ConfigDir)
	}
}

func TestUpdateAgent_SendsAgentOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/api/v1/sessions/s1" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatal(err)
		}
		if payload["agent"] != "codex" {
			t.Errorf("agent = %v", payload["agent"])
		}
		if _, ok := payload["config_dir"]; ok {
			t.Errorf("config_dir should be omitted")
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":         "s1",
			"agent_name": "codex",
		})
	}))
	defer srv.Close()
	c := v1.New(srv.URL)
	session := v1.ResumeSession(c, "s1")
	info, err := session.UpdateAgent(context.Background(), "codex")
	if err != nil {
		t.Fatalf("UpdateAgent: %v", err)
	}
	if info.AgentName != "codex" {
		t.Errorf("AgentName = %q", info.AgentName)
	}
}

func TestUpdateSession_WithSupplement(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		_ = json.Unmarshal(body, &payload)
		if payload["agent"] != "codex" {
			t.Errorf("agent = %v", payload["agent"])
		}
		sup, _ := payload["supplement"].(map[string]any)
		if sup["algorithm"] != "full" {
			t.Errorf("supplement = %#v", payload["supplement"])
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "s1", "agent_name": "codex"})
	}))
	defer srv.Close()
	c := v1.New(srv.URL)
	agent := "codex"
	_, err := c.UpdateSession(context.Background(), "s1", v1.UpdateSessionRequest{
		Agent:      &agent,
		Supplement: &v1.SupplementStrategy{Algorithm: "full"},
	})
	if err != nil {
		t.Fatal(err)
	}
}
