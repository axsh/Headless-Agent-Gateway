package v1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/toolconfig"
)

func TestCreateGetPatchSessionTools(t *testing.T) {
	mux := http.NewServeMux()
	var stored map[string]any
	mux.HandleFunc("/api/v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		var req SessionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		stored = map[string]any{
			"id":          "sess-1",
			"agent_name":  req.Agent,
			"work_dir":    req.WorkDir,
			"mcp_servers": toolconfig.MaskMCPServers(req.MCPServers),
			"functions":   req.Functions,
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"session_id": "sess-1", "status": "created"})
	})
	mux.HandleFunc("/api/v1/sessions/sess-1", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			json.NewEncoder(w).Encode(stored)
		case http.MethodPatch:
			var patch SessionPatch
			json.NewDecoder(r.Body).Decode(&patch)
			if patch.MCPServers != nil {
				stored["mcp_servers"] = toolconfig.MaskMCPServers(*patch.MCPServers)
			}
			json.NewEncoder(w).Encode(stored)
		default:
			http.Error(w, "method", http.StatusMethodNotAllowed)
		}
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	c := New(ts.URL)
	enabled := true
	sess, err := c.CreateSession(t.Context(), SessionRequest{
		Agent:   "wayfinder",
		WorkDir: "/tmp/ws",
		MCPServers: map[string]toolconfig.MCPServerConfig{
			"remote": {
				Transport: "http",
				URL:       "https://mcp.example.com/mcp",
				Headers:   map[string]string{"Authorization": "Bearer x"},
				Enabled:   &enabled,
			},
		},
		Functions: map[string]toolconfig.FunctionConfig{
			"lookup_ticket": {
				Description: "Look up",
				Parameters:  json.RawMessage(`{"type":"object"}`),
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	info, err := c.GetSession(t.Context(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if info.MCPServers["remote"].Headers["Authorization"] != "***" {
		t.Fatalf("expected masked header, got %#v", info.MCPServers)
	}
	empty := map[string]toolconfig.MCPServerConfig{}
	info, err = c.PatchSession(t.Context(), sess.ID, SessionPatch{MCPServers: &empty})
	if err != nil {
		t.Fatal(err)
	}
	if len(info.MCPServers) != 0 {
		t.Fatalf("expected cleared mcp_servers, got %#v", info.MCPServers)
	}
}
