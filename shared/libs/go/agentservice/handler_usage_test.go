package agentservice_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/agentservice"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

type usageMockAgent struct{}

func (m *usageMockAgent) CreateSession(_ context.Context, _ ...codingagent.SessionOption) (codingagent.Session, error) {
	return &usageMockSession{}, nil
}
func (m *usageMockAgent) Name() string { return "claudecode" }
func (m *usageMockAgent) Close() error { return nil }

type usageMockSession struct{}

func (s *usageMockSession) Send(_ context.Context, _ string) (<-chan codingagent.StreamEvent, error) {
	ch := make(chan codingagent.StreamEvent, 4)
	ch <- codingagent.StreamEvent{Type: codingagent.EventSystem, SessionID: "mock-u"}
	ch <- codingagent.StreamEvent{
		Type: codingagent.EventText,
		Content: "hi",
		Usage: &codingagent.TokenUsage{
			InputTokens: 100, OutputTokens: 5, CallID: "msg_1",
			Source: codingagent.UsageSourceClaudeAssistant, Confidence: codingagent.UsageConfidenceHigh,
		},
	}
	ch <- codingagent.StreamEvent{
		Type: codingagent.EventResult,
		Usage: &codingagent.TokenUsage{
			InputTokens: 120, OutputTokens: 8,
			Source: codingagent.UsageSourceClaudeResult, Confidence: codingagent.UsageConfidenceHigh,
		},
	}
	close(ch)
	return ch, nil
}
func (s *usageMockSession) ID() string   { return "mock-u" }
func (s *usageMockSession) Close() error { return nil }

func TestHandleSendMessage_ResultUsageAndGetUsage(t *testing.T) {
	srv := agentservice.New()
	srv.RegisterAgent(&usageMockAgent{})
	handler := srv.HTTPHandler()

	sessionDir := t.TempDir()
	body, _ := json.Marshal(map[string]string{
		"agent": "claudecode", "model": "m", "work_dir": t.TempDir(), "session_dir": sessionDir,
	})
	req := httptest.NewRequest("POST", "/api/v1/sessions", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", w.Code, w.Body.String())
	}
	var created map[string]string
	json.NewDecoder(w.Body).Decode(&created)
	sessionID := created["session_id"]

	msgBody, _ := json.Marshal(map[string]any{
		"content": []map[string]string{{"type": "text", "text": "hello"}},
	})
	req = httptest.NewRequest("POST", "/api/v1/sessions/"+sessionID+"/messages", bytes.NewReader(msgBody))
	req.Header.Set("Accept", "application/json")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("send status = %d body=%s", w.Code, w.Body.String())
	}
	var events []codingagent.StreamEvent
	if err := json.NewDecoder(w.Body).Decode(&events); err != nil {
		t.Fatal(err)
	}
	var gotUsage *codingagent.TokenUsage
	for _, ev := range events {
		if ev.Type == codingagent.EventResult {
			gotUsage = ev.Usage
		}
	}
	if gotUsage == nil || gotUsage.InputTokens != 120 || gotUsage.OutputTokens != 8 {
		t.Fatalf("result usage = %+v", gotUsage)
	}
	if gotUsage.Model != "m" || gotUsage.ModelSource != codingagent.ModelSourceTernSession {
		t.Fatalf("result model attribution = %+v", gotUsage)
	}

	req = httptest.NewRequest("GET", "/api/v1/sessions/"+sessionID+"/usage", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("usage status = %d body=%s", w.Code, w.Body.String())
	}
	var rep codingagent.SessionUsageReport
	if err := json.NewDecoder(w.Body).Decode(&rep); err != nil {
		t.Fatal(err)
	}
	if rep.Usage.InputTokens != 120 || len(rep.Turns) != 1 || len(rep.Turns[0].Calls) != 1 {
		t.Fatalf("report = %+v", rep)
	}

	req = httptest.NewRequest("GET", "/api/v1/sessions/"+sessionID, nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	var sess map[string]any
	json.NewDecoder(w.Body).Decode(&sess)
	usage, _ := sess["usage"].(map[string]any)
	if usage == nil || usage["input_tokens"].(float64) != 120 {
		t.Fatalf("session usage = %v", sess["usage"])
	}
}

func TestHandleGetSessionUsage_LastN(t *testing.T) {
	srv := agentservice.New()
	srv.RegisterAgent(&usageMockAgent{})
	handler := srv.HTTPHandler()

	sessionDir := t.TempDir()
	body, _ := json.Marshal(map[string]string{
		"agent": "claudecode", "model": "m", "work_dir": t.TempDir(), "session_dir": sessionDir,
	})
	req := httptest.NewRequest("POST", "/api/v1/sessions", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", w.Code, w.Body.String())
	}
	var created map[string]string
	json.NewDecoder(w.Body).Decode(&created)
	sessionID := created["session_id"]

	msgBody, _ := json.Marshal(map[string]any{
		"content": []map[string]string{{"type": "text", "text": "hello"}},
	})
	for i := 0; i < 2; i++ {
		req = httptest.NewRequest("POST", "/api/v1/sessions/"+sessionID+"/messages", bytes.NewReader(msgBody))
		req.Header.Set("Accept", "application/json")
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("send %d status = %d body=%s", i+1, w.Code, w.Body.String())
		}
	}

	req = httptest.NewRequest("GET", "/api/v1/sessions/"+sessionID+"/usage?last_n=1", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("usage status = %d body=%s", w.Code, w.Body.String())
	}
	var rep codingagent.SessionUsageReport
	if err := json.NewDecoder(w.Body).Decode(&rep); err != nil {
		t.Fatal(err)
	}
	if len(rep.Turns) != 1 {
		t.Fatalf("expected 1 turn, got %d: %+v", len(rep.Turns), rep)
	}
	if rep.Usage.InputTokens != 120 || rep.Usage.OutputTokens != 8 {
		t.Fatalf("filtered usage should match last turn, got %+v", rep.Usage)
	}

	req = httptest.NewRequest("GET", "/api/v1/sessions/"+sessionID+"/usage", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	var full codingagent.SessionUsageReport
	json.NewDecoder(w.Body).Decode(&full)
	if len(full.Turns) != 2 || full.Usage.InputTokens != 240 {
		t.Fatalf("full report = %+v", full)
	}
}
