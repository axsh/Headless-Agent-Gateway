package v1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/toolconfig"
)

func TestCreateGetPatchSessionTools(t *testing.T) {
	var gotCreateBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		json.NewDecoder(r.Body).Decode(&gotCreateBody)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"session_id": "s1", "status": "created"})
	})
	mux.HandleFunc("/api/v1/sessions/s1", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			json.NewEncoder(w).Encode(SessionInfo{
				ID: "s1",
				MCPServers: map[string]toolconfig.MCPServerConfig{
					"remote": {
						Transport: "http",
						URL:       "https://mcp.example.com/mcp",
						Headers:   map[string]string{"Authorization": "***"},
					},
				},
			})
		case http.MethodPatch:
			var patch SessionPatch
			json.NewDecoder(r.Body).Decode(&patch)
			if patch.MCPServers == nil {
				t.Fatalf("expected mcp_servers in patch")
			}
			json.NewEncoder(w).Encode(SessionInfo{ID: "s1", MCPServers: *patch.MCPServers})
		default:
			t.Fatalf("unexpected method %s", r.Method)
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
			"remote": {Transport: "http", URL: "https://mcp.example.com/mcp", Enabled: &enabled},
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
	if sess.ID != "s1" {
		t.Fatalf("id = %s", sess.ID)
	}
	if gotCreateBody["mcp_servers"] == nil {
		t.Fatalf("create body missing mcp_servers: %#v", gotCreateBody)
	}

	info, err := c.GetSession(t.Context(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if info.MCPServers["remote"].Headers["Authorization"] != "***" {
		t.Fatalf("masked header missing: %#v", info.MCPServers)
	}

	empty := map[string]toolconfig.MCPServerConfig{}
	patched, err := c.PatchSession(t.Context(), sess.ID, SessionPatch{MCPServers: &empty})
	if err != nil {
		t.Fatal(err)
	}
	if len(patched.MCPServers) != 0 {
		t.Fatalf("expected cleared mcp_servers")
	}
}
