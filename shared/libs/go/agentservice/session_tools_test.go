package agentservice_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/agentservice"
)

func TestHandleCreateSession_WithMCPAndFunctions(t *testing.T) {
	_, handler := newTestServer()
	enabled := true
	body, _ := json.Marshal(map[string]any{
		"agent":       "claudecode",
		"work_dir":    t.TempDir(),
		"session_dir": t.TempDir(),
		"mcp_servers": map[string]any{
			"remote": map[string]any{
				"transport": "http",
				"url":       "https://mcp.example.com/mcp",
				"headers":   map[string]string{"Authorization": "Bearer secret"},
				"enabled":   enabled,
			},
		},
		"functions": map[string]any{
			"lookup_ticket": map[string]any{
				"description": "Look up a ticket",
				"parameters": map[string]any{
					"type":       "object",
					"properties": map[string]any{"ticket_id": map[string]string{"type": "string"}},
					"required":   []string{"ticket_id"},
				},
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
		t.Fatalf("functions missing: %#v", fns)
	}
}

func TestHandleCreateSession_InvalidMCP(t *testing.T) {
	_, handler := newTestServer()
	body, _ := json.Marshal(map[string]any{
		"agent":    "claudecode",
		"work_dir": t.TempDir(),
		"mcp_servers": map[string]any{
			"bad": map[string]any{"transport": "stdio"},
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

func TestHandlePatchSession_MCPClearAndPreserve(t *testing.T) {
	_, handler := newTestServer()
	body, _ := json.Marshal(map[string]any{
		"agent":    "claudecode",
		"work_dir": t.TempDir(),
		"mcp_servers": map[string]any{
			"remote": map[string]any{
				"transport": "http",
				"url":       "https://mcp.example.com/mcp",
			},
		},
		"functions": map[string]any{
			"lookup_ticket": map[string]any{
				"description": "Look up",
				"parameters":   map[string]any{"type": "object"},
			},
		},
	})
	req := httptest.NewRequest("POST", "/api/v1/sessions", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	var created map[string]string
	json.NewDecoder(w.Body).Decode(&created)
	sid := created["session_id"]

	patchOnlyFn, _ := json.Marshal(map[string]any{
		"functions": map[string]any{
			"other": map[string]any{
				"description": "Other",
				"parameters":   map[string]any{"type": "object"},
			},
		},
	})
	req = httptest.NewRequest("PATCH", "/api/v1/sessions/"+sid, bytes.NewReader(patchOnlyFn))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("patch functions status = %d body=%s", w.Code, w.Body.String())
	}
	var patched map[string]any
	json.NewDecoder(w.Body).Decode(&patched)
	mcp, _ := patched["mcp_servers"].(map[string]any)
	if _, ok := mcp["remote"]; !ok {
		t.Fatalf("mcp_servers should be preserved: %#v", mcp)
	}
	fns, _ := patched["functions"].(map[string]any)
	if _, ok := fns["other"]; !ok {
		t.Fatalf("functions not updated: %#v", fns)
	}

	clearMCP, _ := json.Marshal(map[string]any{"mcp_servers": map[string]any{}})
	req = httptest.NewRequest("PATCH", "/api/v1/sessions/"+sid, bytes.NewReader(clearMCP))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("clear status = %d", w.Code)
	}
	var cleared map[string]any
	json.NewDecoder(w.Body).Decode(&cleared)
	if v, ok := cleared["mcp_servers"]; ok && v != nil {
		if m, _ := v.(map[string]any); len(m) != 0 {
			t.Fatalf("mcp_servers should be cleared, got %#v", v)
		}
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
}

// Ensure agentservice import is used when helpers live in this package.
var _ = agentservice.New
