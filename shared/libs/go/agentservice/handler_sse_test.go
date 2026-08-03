package agentservice_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/agentservice"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

type mockLargeToolResultAgent struct {
	name string
}

func (m *mockLargeToolResultAgent) CreateSession(_ context.Context, _ ...codingagent.SessionOption) (codingagent.Session, error) {
	return &mockLargeToolResultSession{}, nil
}
func (m *mockLargeToolResultAgent) Name() string { return m.name }
func (m *mockLargeToolResultAgent) Close() error { return nil }

type mockLargeToolResultSession struct{}

func (s *mockLargeToolResultSession) Send(_ context.Context, _ string) (<-chan codingagent.StreamEvent, error) {
	ch := make(chan codingagent.StreamEvent, 4)
	ch <- codingagent.StreamEvent{
		Type:    codingagent.EventToolResult,
		Content: strings.Repeat("z", codingagent.DefaultMaxToolResultBytes),
	}
	ch <- codingagent.StreamEvent{Type: codingagent.EventResult}
	close(ch)
	return ch, nil
}
func (s *mockLargeToolResultSession) ID() string   { return "mock-large-session" }
func (s *mockLargeToolResultSession) Close() error { return nil }

func TestHandleSendMessage_SSEChunkedToolResult(t *testing.T) {
	srv := agentservice.New()
	srv.RegisterAgent(&mockLargeToolResultAgent{name: "codex"})

	body, _ := json.Marshal(map[string]string{
		"agent":       "codex",
		"session_dir": t.TempDir(),
	})
	req := httptest.NewRequest("POST", "/api/v1/sessions", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.HTTPHandler().ServeHTTP(rec, req)

	var created map[string]string
	json.NewDecoder(rec.Body).Decode(&created)
	sessionID := created["session_id"]

	msgBody, _ := json.Marshal(map[string]any{
		"content": []map[string]string{{"type": "text", "text": "run"}},
	})
	msgReq := httptest.NewRequest("POST", "/api/v1/sessions/"+sessionID+"/messages", bytes.NewReader(msgBody))
	msgReq.Header.Set("Content-Type", "application/json")
	msgReq.Header.Set("Accept", "text/event-stream")
	msgRec := httptest.NewRecorder()
	srv.HTTPHandler().ServeHTTP(msgRec, msgReq)

	if msgRec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", msgRec.Code, msgRec.Body.String())
	}

	bodyStr := msgRec.Body.String()
	if !strings.Contains(bodyStr, "tool_result_part") {
		t.Fatal("expected tool_result_part in SSE body")
	}
	if !strings.Contains(bodyStr, `"type":"result"`) {
		t.Fatal("expected result event in SSE body")
	}

	partCount := strings.Count(bodyStr, "tool_result_part")
	if partCount == 0 {
		t.Fatal("expected at least one tool_result_part")
	}

	scanner := codingagent.NewLargeLineScanner(strings.NewReader(bodyStr), 0)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		if len(data) >= codingagent.DefaultMaxSSEDataLineBytes {
			t.Fatalf("SSE data line len %d >= max %d", len(data), codingagent.DefaultMaxSSEDataLineBytes)
		}
	}

	getReq := httptest.NewRequest("GET", "/api/v1/sessions/"+sessionID, nil)
	getRec := httptest.NewRecorder()
	srv.HTTPHandler().ServeHTTP(getRec, getReq)

	var session map[string]interface{}
	json.NewDecoder(getRec.Body).Decode(&session)
	status, _ := session["status"].(string)
	if status != "completed" {
		t.Fatalf("status = %q, want completed", status)
	}
}
