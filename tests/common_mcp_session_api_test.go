package llm_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

func TestMCPSessionAPI_CreateGetPatchMask(t *testing.T) {
	ts, _ := setupAgentServiceTestServer(t)

	body, _ := json.Marshal(map[string]any{
		"agent":    "claudecode",
		"work_dir": t.TempDir(),
		"mcp_servers": map[string]any{
			"remote": map[string]any{
				"transport": "http",
				"url":       "https://mcp.example.com/mcp",
				"headers":   map[string]string{"Authorization": "Bearer secret-token"},
				"env":       map[string]string{"TOKEN": "secret-env"},
			},
		},
		"functions": map[string]any{
			"lookup_ticket": map[string]any{
				"description": "Look up a ticket",
				"parameters":   map[string]any{"type": "object"},
			},
		},
	})
	resp, err := http.Post(ts.URL+"/api/v1/sessions", "application/json", bytes.NewReader(body))
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
	var record map[string]any
	json.NewDecoder(getResp.Body).Decode(&record)
	mcp := record["mcp_servers"].(map[string]any)
	remote := mcp["remote"].(map[string]any)
	headers := remote["headers"].(map[string]any)
	if headers["Authorization"] != "***" {
		t.Fatalf("header not masked: %#v", headers)
	}
	env := remote["env"].(map[string]any)
	if env["TOKEN"] != "***" {
		t.Fatalf("env not masked: %#v", env)
	}

	clearBody, _ := json.Marshal(map[string]any{"mcp_servers": map[string]any{}})
	req, _ := http.NewRequest(http.MethodPatch, ts.URL+"/api/v1/sessions/"+sid, bytes.NewReader(clearBody))
	req.Header.Set("Content-Type", "application/json")
	patchResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer patchResp.Body.Close()
	if patchResp.StatusCode != http.StatusOK {
		t.Fatalf("patch status=%d", patchResp.StatusCode)
	}
}

func TestMCPSessionAPI_InvalidCreate(t *testing.T) {
	ts, _ := setupAgentServiceTestServer(t)
	body, _ := json.Marshal(map[string]any{
		"agent": "claudecode",
		"mcp_servers": map[string]any{
			"bad": map[string]any{"transport": "stdio"},
		},
	})
	resp, err := http.Post(ts.URL+"/api/v1/sessions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d want 400", resp.StatusCode)
	}
}

func TestMCPSessionAPI_OmitToolsCompatible(t *testing.T) {
	ts, _ := setupAgentServiceTestServer(t)
	sid := createAgentServiceSession(t, ts.URL, "claudecode")
	if sid == "" {
		t.Fatal("empty session id")
	}
}
