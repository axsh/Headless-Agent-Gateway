// Package llm_test contains integration tests for the AgentService.
// These tests use mock agents (no real CLI required) and verify
// the HTTP API endpoints including health check, session lifecycle,
// SSE streaming, TaskLog recording, log streaming, and SDKSessionID.
package llm_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/axsh/hag/agentservice"
	"github.com/axsh/hag/codingagent"
	"github.com/axsh/hag/tasklog"
)

// integrationMockAgent implements codingagent.CodingAgent for integration tests.
type integrationMockAgent struct {
	name     string
	sessions []*integrationMockSession
}

func (a *integrationMockAgent) Name() string { return a.name }
func (a *integrationMockAgent) Close() error  { return nil }
func (a *integrationMockAgent) CreateSession(
	_ context.Context, _ ...codingagent.SessionOption,
) (codingagent.Session, error) {
	s := &integrationMockSession{}
	a.sessions = append(a.sessions, s)
	return s, nil
}

// integrationMockSession returns multiple event types including EventSystem.
type integrationMockSession struct{}

func (s *integrationMockSession) ID() string  { return "sdk-integration-001" }
func (s *integrationMockSession) Close() error { return nil }
func (s *integrationMockSession) Send(
	_ context.Context, _ string,
) (<-chan codingagent.StreamEvent, error) {
	ch := make(chan codingagent.StreamEvent, 4)
	ch <- codingagent.StreamEvent{
		Type:      codingagent.EventSystem,
		SessionID: "sdk-integration-001",
	}
	ch <- codingagent.StreamEvent{
		Type:    codingagent.EventText,
		Content: "Integration test response",
	}
	ch <- codingagent.StreamEvent{
		Type:     codingagent.EventToolUse,
		ToolName: "write_file",
		Content:  `{"path":"hello.py","content":"print('hello')"}`,
	}
	ch <- codingagent.StreamEvent{Type: codingagent.EventResult}
	close(ch)
	return ch, nil
}

// setupAgentServiceTestServer creates a test server with mock agent and TaskLog.
func setupAgentServiceTestServer(t *testing.T) (*httptest.Server, *tasklog.TaskLog) {
	t.Helper()
	tl := tasklog.New()
	srv := agentservice.New(
		agentservice.WithTaskLog(tl),
	)
	srv.RegisterAgent(&integrationMockAgent{name: "claudecode"})
	ts := httptest.NewServer(srv.HTTPHandler())
	t.Cleanup(ts.Close)
	return ts, tl
}

// createAgentServiceSession creates a session via POST /api/v1/sessions.
func createAgentServiceSession(t *testing.T, baseURL, agent string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"agent": agent})
	resp, err := http.Post(baseURL+"/api/v1/sessions",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create session: status=%d", resp.StatusCode)
	}
	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	return result["session_id"]
}

// --- Test Scenarios ---

func TestAgentServiceHealthCheck(t *testing.T) {
	ts, _ := setupAgentServiceTestServer(t)

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("health check: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var health struct {
		Status      string            `json:"status"`
		Agents      []string          `json:"agents"`
		CLIVersions map[string]string `json:"cli_versions"`
		Gateway     struct {
			Status string `json:"status"`
		} `json:"gateway"`
	}
	json.NewDecoder(resp.Body).Decode(&health)

	if health.Status != "ok" {
		t.Errorf("health.status = %q, want ok", health.Status)
	}
	if len(health.Agents) != 1 || health.Agents[0] != "claudecode" {
		t.Errorf("agents = %v, want [claudecode]", health.Agents)
	}
	// cli_versions should exist (may be "unavailable" in test env)
	if health.CLIVersions == nil {
		t.Error("cli_versions should not be nil")
	}
	if _, ok := health.CLIVersions["claudecode"]; !ok {
		t.Error("cli_versions should contain claudecode entry")
	}
}

func TestAgentServiceSessionLifecycle(t *testing.T) {
	ts, _ := setupAgentServiceTestServer(t)

	// 1. Create session
	sessionID := createAgentServiceSession(t, ts.URL, "claudecode")
	if sessionID == "" {
		t.Fatal("session_id should not be empty")
	}

	// 2. Get session
	resp, _ := http.Get(ts.URL + "/api/v1/sessions/" + sessionID)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get session: status=%d", resp.StatusCode)
	}
	var record codingagent.SessionRecord
	json.NewDecoder(resp.Body).Decode(&record)
	resp.Body.Close()
	if record.Status != codingagent.StatusActive {
		t.Errorf("status = %q, want active", record.Status)
	}

	// 3. Terminate
	resp, _ = http.Post(ts.URL+"/api/v1/sessions/"+sessionID+"/terminate",
		"application/json", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("terminate: status=%d", resp.StatusCode)
	}
	resp.Body.Close()

	// 4. Verify status = closed
	resp, _ = http.Get(ts.URL + "/api/v1/sessions/" + sessionID)
	json.NewDecoder(resp.Body).Decode(&record)
	resp.Body.Close()
	if record.Status != codingagent.StatusClosed {
		t.Errorf("after terminate: status = %q, want closed", record.Status)
	}

	// 5. Delete
	req, _ := http.NewRequest("DELETE",
		ts.URL+"/api/v1/sessions/"+sessionID, nil)
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: status=%d", resp.StatusCode)
	}
	resp.Body.Close()

	// 6. Verify not found
	resp, _ = http.Get(ts.URL + "/api/v1/sessions/" + sessionID)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("after delete: status = %d, want 404", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestAgentServiceSSEStreaming(t *testing.T) {
	ts, _ := setupAgentServiceTestServer(t)
	sessionID := createAgentServiceSession(t, ts.URL, "claudecode")

	// Send message with SSE
	msgBody, _ := json.Marshal(map[string]string{"message": "test prompt"})
	req, _ := http.NewRequest("POST",
		ts.URL+"/api/v1/sessions/"+sessionID+"/messages",
		bytes.NewReader(msgBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send message: %v", err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream",
			resp.Header.Get("Content-Type"))
	}

	// Parse SSE events
	var events []codingagent.StreamEvent
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		var ev codingagent.StreamEvent
		if json.Unmarshal([]byte(data), &ev) == nil {
			events = append(events, ev)
		}
	}

	if len(events) < 3 {
		t.Fatalf("expected at least 3 events, got %d", len(events))
	}

	// Verify event types
	expectedTypes := []codingagent.EventType{
		codingagent.EventSystem,
		codingagent.EventText,
		codingagent.EventToolUse,
	}
	for i, expected := range expectedTypes {
		if events[i].Type != expected {
			t.Errorf("event[%d].Type = %q, want %q", i, events[i].Type, expected)
		}
	}
}

func TestAgentServiceTaskLogIntegration(t *testing.T) {
	ts, tl := setupAgentServiceTestServer(t)
	sessionID := createAgentServiceSession(t, ts.URL, "claudecode")

	// Verify TaskLog is empty before message
	if len(tl.Entries()) != 0 {
		t.Fatalf("TaskLog should be empty before message, got %d entries",
			len(tl.Entries()))
	}

	// Send message (JSON mode to collect all at once)
	msgBody, _ := json.Marshal(map[string]string{"message": "test"})
	resp, _ := http.Post(
		ts.URL+"/api/v1/sessions/"+sessionID+"/messages",
		"application/json", bytes.NewReader(msgBody))
	io.ReadAll(resp.Body)
	resp.Body.Close()

	// Wait briefly for async processing
	time.Sleep(100 * time.Millisecond)

	// Verify TaskLog has entries
	entries := tl.Entries()
	if len(entries) == 0 {
		t.Fatal("TaskLog should have entries after message send")
	}

	// Verify entries are AgentLogEntry type
	for _, entry := range entries {
		if entry.Type() != tasklog.AgentLogEntryType {
			t.Errorf("entry.Type() = %q, want %q",
				entry.Type(), tasklog.AgentLogEntryType)
		}
	}
}

func TestAgentServiceLogStreamSSE(t *testing.T) {
	ts, tl := setupAgentServiceTestServer(t)
	_ = createAgentServiceSession(t, ts.URL, "claudecode")

	// Add some TaskLog entries manually
	tl.Add(tasklog.NewAgentLogSendEntry("log-1", "claudecode", "test body"))

	// Request log stream - but this test needs a session whose status is terminal
	// otherwise the handler will poll forever. So we first send a message to complete,
	// then check logs.

	// Create another session and send a message to put it into completed state
	sessionID2 := createAgentServiceSession(t, ts.URL, "claudecode")
	msgBody, _ := json.Marshal(map[string]string{"message": "test"})
	resp, _ := http.Post(
		ts.URL+"/api/v1/sessions/"+sessionID2+"/messages",
		"application/json", bytes.NewReader(msgBody))
	io.ReadAll(resp.Body)
	resp.Body.Close()

	// Wait for status to transition to completed
	time.Sleep(200 * time.Millisecond)

	// Request log stream for the completed session
	req, _ := http.NewRequest("GET",
		ts.URL+"/api/v1/sessions/"+sessionID2+"/logs", nil)
	req.Header.Set("Accept", "text/event-stream")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("log stream: %v", err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream",
			resp.Header.Get("Content-Type"))
	}

	// Read events - expect at least log entries and a status event
	body, _ := io.ReadAll(resp.Body)
	output := string(body)

	if !strings.Contains(output, "event: log") && !strings.Contains(output, "event: status") {
		t.Error("expected at least one 'event: log' or 'event: status' in SSE stream")
	}
}

func TestAgentServiceSDKSessionID(t *testing.T) {
	ts, _ := setupAgentServiceTestServer(t)
	sessionID := createAgentServiceSession(t, ts.URL, "claudecode")

	// Send message with SSE (triggers EventSystem with SessionID)
	msgBody, _ := json.Marshal(map[string]string{"message": "test"})
	req, _ := http.NewRequest("POST",
		ts.URL+"/api/v1/sessions/"+sessionID+"/messages",
		bytes.NewReader(msgBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, _ := http.DefaultClient.Do(req)
	io.ReadAll(resp.Body)
	resp.Body.Close()

	// Verify SDKSessionID was saved
	resp, _ = http.Get(ts.URL + "/api/v1/sessions/" + sessionID)
	var record struct {
		SDKSessionID string `json:"sdk_session_id"`
		Status       string `json:"status"`
	}
	json.NewDecoder(resp.Body).Decode(&record)
	resp.Body.Close()

	if record.SDKSessionID != "sdk-integration-001" {
		t.Errorf("sdk_session_id = %q, want sdk-integration-001",
			record.SDKSessionID)
	}
	if record.Status != codingagent.StatusCompleted {
		t.Errorf("status = %q, want completed", record.Status)
	}
}
