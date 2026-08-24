package v1_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestCreateSession_SandboxModeMarshaled(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/sessions" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"session_id": "s-new"})
	}))
	defer srv.Close()

	c := v1.New(srv.URL)
	_, err := c.CreateSession(context.Background(), v1.SessionRequest{
		Agent:       "codex",
		WorkDir:     "/tmp/w",
		SandboxMode: v1.SandboxModeWorkspaceWrite,
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if gotBody["sandbox_mode"] != v1.SandboxModeWorkspaceWrite {
		t.Fatalf("sandbox_mode = %v, want %s", gotBody["sandbox_mode"], v1.SandboxModeWorkspaceWrite)
	}
}

func TestCreateSession_SandboxModeOmittedWhenEmpty(t *testing.T) {
	var raw []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		raw, err = io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"session_id": "s-new"})
	}))
	defer srv.Close()

	c := v1.New(srv.URL)
	_, err := c.CreateSession(context.Background(), v1.SessionRequest{
		Agent:   "codex",
		WorkDir: "/tmp/w",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if strings.Contains(string(raw), "sandbox_mode") {
		t.Fatalf("sandbox_mode must be omitted when empty, body=%s", raw)
	}
}

func TestGetSession_SandboxMode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":           "s1",
			"agent_name":   "codex",
			"status":       "active",
			"work_dir":     "/tmp/w",
			"session_dir":  "/tmp/s",
			"sandbox_mode": v1.SandboxModeDangerFullAccess,
		})
	}))
	defer srv.Close()

	c := v1.New(srv.URL)
	info, err := c.GetSession(context.Background(), "s1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if info.SandboxMode != v1.SandboxModeDangerFullAccess {
		t.Fatalf("SandboxMode = %q", info.SandboxMode)
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

func TestFollow_UsesEventsPath(t *testing.T) {
	var gotPath, gotQuery, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotAccept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"type\":\"result\"}\n\n")
		fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()
	c := v1.New(srv.URL, v1.WithNoTimeout())
	sess := v1.ResumeSession(c, "sess-follow")
	if _, err := sess.Follow(context.Background()); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/sessions/sess-follow/events" {
		t.Fatalf("path = %s", gotPath)
	}
	if gotQuery != "" {
		t.Fatalf("query = %s, want empty", gotQuery)
	}
	if !strings.Contains(gotAccept, "text/event-stream") {
		t.Fatalf("accept = %s", gotAccept)
	}
	if _, err := sess.FollowFrom(context.Background(), "3"); err != nil {
		t.Fatal(err)
	}
	if gotQuery != "from=3" {
		t.Fatalf("from query = %s", gotQuery)
	}
}
