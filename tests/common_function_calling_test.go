package llm_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

func TestFunctionCalling_ToolResultsNoActiveExec(t *testing.T) {
	ts, _ := setupAgentServiceTestServer(t)
	sid := createAgentServiceSession(t, ts.URL, "claudecode")

	body, _ := json.Marshal(map[string]any{
		"call_id": "call-1",
		"content": `{"ok":true}`,
	})
	resp, err := http.Post(ts.URL+"/api/v1/sessions/"+sid+"/tool_results",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status=%d want 409", resp.StatusCode)
	}
}

func TestFunctionCalling_ToolResultsValidation(t *testing.T) {
	ts, _ := setupAgentServiceTestServer(t)
	sid := createAgentServiceSession(t, ts.URL, "claudecode")

	resp, err := http.Post(ts.URL+"/api/v1/sessions/"+sid+"/tool_results",
		"application/json", bytes.NewReader([]byte(`{"content":"x"}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d want 400", resp.StatusCode)
	}
}
