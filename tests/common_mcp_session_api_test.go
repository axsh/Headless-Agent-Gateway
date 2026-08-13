// Package llm_test: MCP / functions session API contract (Part 1).
package llm_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/toolconfig"
)

func TestMCPSessionAPI_CreateGetPatch(t *testing.T) {
	ts, _ := setupAgentServiceTestServer(t)
	enabled := true
	createBody, _ := json.Marshal(map[string]any{
		"agent":    "claudecode",
		"work_dir": t.TempDir(),
		"mcp_servers": map[string]toolconfig.MCPServerConfig{
			"remote": {
				Transport: "http",
				URL:       "https://mcp.example.com/mcp",
				Headers:   map[string]string{"Authorization": "Bearer secret"},
				Enabled:   &enabled,
			},
		},
		"functions": map[string]toolconfig.FunctionConfig{
			"lookup_ticket": {
				Description: "Look up a ticket",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"ticket_id":{"type":"string"}}}`),
			},
		},
	})
	resp, err := http.Post(ts.URL+"/api/v1/sessions", "application/json", bytes.NewReader(createBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status=%d", resp.StatusCode)
	}
	var created map[string]string
	json.NewDecoder(resp.Body).Decode(&created)
	sid := created["session_id"]

	getResp, err := http.Get(ts.URL + "/api/v1/sessions/" + sid)
	if err != nil {
		t.Fatal(err)
	}
	defer getResp.Body.Close()
	var got map[string]any
	json.NewDecoder(getResp.Body).Decode(&got)
	mcp := got["mcp_servers"].(map[string]any)
	remote := mcp["remote"].(map[string]any)
	headers := remote["headers"].(map[string]any)
	if headers["Authorization"] != "***" {
		t.Fatalf("expected masked Authorization, got %#v", headers)
	}

	// Invalid create must 400.
	badBody, _ := json.Marshal(map[string]any{
		"agent":       "claudecode",
		"work_dir":    t.TempDir(),
		"mcp_servers": map[string]toolconfig.MCPServerConfig{"fs": {Transport: "stdio"}},
	})
	badResp, err := http.Post(ts.URL+"/api/v1/sessions", "application/json", bytes.NewReader(badBody))
	if err != nil {
		t.Fatal(err)
	}
	badResp.Body.Close()
	if badResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid create status=%d", badResp.StatusCode)
	}

	// Clear mcp_servers via PATCH.
	patchBody, _ := json.Marshal(map[string]any{"mcp_servers": map[string]any{}})
	req, _ := http.NewRequest(http.MethodPatch, ts.URL+"/api/v1/sessions/"+sid, bytes.NewReader(patchBody))
	req.Header.Set("Content-Type", "application/json")
	patchResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer patchResp.Body.Close()
	if patchResp.StatusCode != http.StatusOK {
		t.Fatalf("patch status=%d", patchResp.StatusCode)
	}

	// Omit tools on create remains compatible.
	omitBody, _ := json.Marshal(map[string]string{"agent": "claudecode", "work_dir": t.TempDir()})
	omitResp, err := http.Post(ts.URL+"/api/v1/sessions", "application/json", bytes.NewReader(omitBody))
	if err != nil {
		t.Fatal(err)
	}
	omitResp.Body.Close()
	if omitResp.StatusCode != http.StatusCreated {
		t.Fatalf("omit create status=%d", omitResp.StatusCode)
	}
}
