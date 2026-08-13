package agentservice_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/toolconfig"
)

func TestHandleCreateSession_WithMCPAndFunctions(t *testing.T) {
	_, handler := newTestServer()
	enabled := true
	body, _ := json.Marshal(map[string]any{
		"agent":       "claudecode",
		"work_dir":    t.TempDir(),
		"session_dir": t.TempDir(),
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
	req := httptest.NewRequest("POST", "/api/v1/sessions", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", w.Code, w.Body.String())
	}
	var created map[string]string
	json.NewDecoder(w.Body).Decode(&created)

	req = httptest.NewRequest("GET", "/api/v1/sessions/"+created["session_id"], nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get status = %d", w.Code)
	}
	var got map[string]any
	json.NewDecoder(w.Body).Decode(&got)
	mcp, _ := got["mcp_servers"].(map[string]any)
	remote, _ := mcp["remote"].(map[string]any)
	headers, _ := remote["headers"].(map[string]any)
	if headers["Authorization"] != "***" {
		t.Fatalf("Authorization not masked: %#v", headers)
	}
	fns, _ := got["functions"].(map[string]any)
	if _, ok := fns["lookup_ticket"]; !ok {
		t.Fatalf("functions missing: %#v", got["functions"])
	}
}

func TestHandleCreateSession_InvalidMCP(t *testing.T) {
	_, handler := newTestServer()
	body, _ := json.Marshal(map[string]any{
		"agent":    "claudecode",
		"work_dir": t.TempDir(),
		"mcp_servers": map[string]toolconfig.MCPServerConfig{
			"fs": {Transport: "stdio"},
		},
	})
	req := httptest.NewRequest("POST", "/api/v1/sessions", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandleCreateSession_OmitToolsCompatible(t *testing.T) {
	_, handler := newTestServer()
	body, _ := json.Marshal(map[string]string{
		"agent":    "claudecode",
		"work_dir": t.TempDir(),
	})
	req := httptest.NewRequest("POST", "/api/v1/sessions", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
}

func TestHandlePatchSession_ToolsClearAndOmit(t *testing.T) {
	_, handler := newTestServer()
	enabled := true
	createBody, _ := json.Marshal(map[string]any{
		"agent":    "claudecode",
		"work_dir": t.TempDir(),
		"mcp_servers": map[string]toolconfig.MCPServerConfig{
			"remote": {Transport: "http", URL: "https://mcp.example.com/mcp", Enabled: &enabled},
		},
		"functions": map[string]toolconfig.FunctionConfig{
			"lookup_ticket": {
				Description: "Look up",
				Parameters:  json.RawMessage(`{"type":"object"}`),
			},
		},
	})
	req := httptest.NewRequest("POST", "/api/v1/sessions", bytes.NewReader(createBody))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	var created map[string]string
	json.NewDecoder(w.Body).Decode(&created)
	sid := created["session_id"]

	patchClear, _ := json.Marshal(map[string]any{"mcp_servers": map[string]any{}})
	req = httptest.NewRequest("PATCH", "/api/v1/sessions/"+sid, bytes.NewReader(patchClear))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("clear patch status = %d body=%s", w.Code, w.Body.String())
	}
	var patched map[string]any
	json.NewDecoder(w.Body).Decode(&patched)
	if mcp, ok := patched["mcp_servers"].(map[string]any); ok && len(mcp) != 0 {
		t.Fatalf("mcp_servers should be cleared, got %#v", mcp)
	}
	fns, _ := patched["functions"].(map[string]any)
	if _, ok := fns["lookup_ticket"]; !ok {
		t.Fatalf("functions should remain: %#v", patched["functions"])
	}

	patchFn, _ := json.Marshal(map[string]any{
		"functions": map[string]toolconfig.FunctionConfig{
			"other": {Description: "Other", Parameters: json.RawMessage(`{"type":"object"}`)},
		},
	})
	req = httptest.NewRequest("PATCH", "/api/v1/sessions/"+sid, bytes.NewReader(patchFn))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("functions patch status = %d", w.Code)
	}
	json.NewDecoder(w.Body).Decode(&patched)
	fns = patched["functions"].(map[string]any)
	if _, ok := fns["other"]; !ok {
		t.Fatalf("expected other function: %#v", fns)
	}
	if _, ok := fns["lookup_ticket"]; ok {
		t.Fatalf("lookup_ticket should be replaced away: %#v", fns)
	}
}

func TestHandlePatchSession_NoFields(t *testing.T) {
	_, handler := newTestServer()
	body, _ := json.Marshal(map[string]string{
		"agent":    "claudecode",
		"work_dir": t.TempDir(),
	})
	req := httptest.NewRequest("POST", "/api/v1/sessions", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	var created map[string]string
	json.NewDecoder(w.Body).Decode(&created)

	req = httptest.NewRequest("PATCH", "/api/v1/sessions/"+created["session_id"], bytes.NewReader([]byte(`{}`)))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "at least one of") {
		t.Fatalf("body = %q", w.Body.String())
	}
}

func TestHandlePatchSession_ConfigDirStillWorks(t *testing.T) {
	_, handler := newTestServer()
	alpha := t.TempDir()
	beta := t.TempDir()
	body, _ := json.Marshal(map[string]string{
		"agent":       "claudecode",
		"work_dir":    t.TempDir(),
		"session_dir": t.TempDir(),
		"config_dir":  alpha,
	})
	req := httptest.NewRequest("POST", "/api/v1/sessions", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
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
}
