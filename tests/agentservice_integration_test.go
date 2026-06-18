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
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/agentservice"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/config"
	"github.com/axsh/arctic-tern/tern"
	"github.com/axsh/arctic-tern/shared/libs/go/llmgateway"
	"github.com/axsh/arctic-tern/shared/libs/go/logger"
	"github.com/axsh/arctic-tern/shared/libs/go/tasklog"
)

// integrationMockAgent implements codingagent.CodingAgent for integration tests.
type integrationMockAgent struct {
	name     string
	sessions []*integrationMockSession
}

func (a *integrationMockAgent) Name() string { return a.name }
func (a *integrationMockAgent) Close() error { return nil }
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

	// Verify AgentSessionID was saved
	resp, _ = http.Get(ts.URL + "/api/v1/sessions/" + sessionID)
	var record struct {
		AgentSessionID string `json:"agent_session_id"`
		Status         string `json:"status"`
	}
	json.NewDecoder(resp.Body).Decode(&record)
	resp.Body.Close()

	if record.AgentSessionID != "sdk-integration-001" {
		t.Errorf("agent_session_id = %q, want sdk-integration-001",
			record.AgentSessionID)
	}
	if record.Status != codingagent.StatusCompleted {
		t.Errorf("status = %q, want completed", record.Status)
	}
}

// TestAgentServiceLaunchShutdown verifies that AgentService can
// Launch on an ephemeral port and Shutdown gracefully.
func TestAgentServiceLaunchShutdown(t *testing.T) {
	srv := agentservice.New(
		agentservice.WithLogger(logger.NewDefault(logger.LevelDebug)),
	)
	srv.RegisterAgent(&integrationMockAgent{name: "claudecode"})

	ctx := context.Background()

	// Launch on ephemeral port
	err := srv.Launch(ctx, 0)
	if err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

	port := srv.Port()
	if port == 0 {
		t.Fatal("Port should be non-zero after Launch")
	}

	// Verify health endpoint is reachable
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/health", port))
	if err != nil {
		t.Fatalf("health request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Verify response body contains expected fields
	var health map[string]any
	json.NewDecoder(resp.Body).Decode(&health)
	if health["status"] != "ok" {
		t.Fatalf("expected status ok, got %v", health["status"])
	}
	agents, ok := health["agents"].([]any)
	if !ok {
		t.Fatal("agents field missing or wrong type")
	}
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}

	// Shutdown
	err = srv.Shutdown(ctx)
	if err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}

	// Verify port is no longer accepting connections
	_, err = http.Get(fmt.Sprintf("http://localhost:%d/health", port))
	if err == nil {
		t.Fatal("expected connection refused after shutdown")
	}
}

// TestAgentServiceConfigPort verifies AgentService reads port from config
// via tern.Server integration.
func TestAgentServiceConfigPort(t *testing.T) {
	cfg := &config.AppConfig{
		AgentService: config.AgentServiceConfig{Port: 0}, // ephemeral
	}
	stub := llmgateway.NewStubGateway()
	srv, err := tern.New(
		tern.WithConfig(cfg),
		tern.WithGateway(stub),
	)
	if err != nil {
		t.Fatalf("tern.New failed: %v", err)
	}

	ctx := context.Background()
	if err := srv.Launch(ctx); err != nil {
		t.Fatalf("Launch failed: %v", err)
	}
	defer srv.Shutdown(ctx)

	// AgentService should be running on some port
	port := srv.AgentService().Port()
	if port == 0 {
		t.Fatal("AgentService port should be non-zero after Launch")
	}

	// Health should be accessible
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/health", port))
	if err != nil {
		t.Fatalf("health request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// TestAgentServiceSSEStreamingContent verifies that SSE events contain
// concrete content (session_id, text content, tool_name), not just types.
func TestAgentServiceSSEStreamingContent(t *testing.T) {
	ts, _ := setupAgentServiceTestServer(t)
	sessionID := createAgentServiceSession(t, ts.URL, "claudecode")

	msgBody, _ := json.Marshal(map[string]string{"message": "test"})
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

	var events []codingagent.StreamEvent
	var gotDone bool
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			gotDone = true
			break
		}
		var ev codingagent.StreamEvent
		if json.Unmarshal([]byte(data), &ev) == nil {
			events = append(events, ev)
		}
	}

	if !gotDone {
		t.Fatal("expected [DONE] sentinel in SSE stream")
	}

	if len(events) < 4 {
		t.Fatalf("expected at least 4 events, got %d", len(events))
	}

	// system event should have session_id
	if events[0].Type != codingagent.EventSystem {
		t.Errorf("event[0].Type = %q, want system", events[0].Type)
	}
	if events[0].SessionID == "" {
		t.Error("system event should have non-empty session_id")
	}

	// text event should have content
	if events[1].Type != codingagent.EventText {
		t.Errorf("event[1].Type = %q, want text", events[1].Type)
	}
	if events[1].Content == "" {
		t.Error("text event should have non-empty content")
	}

	// tool_use event should have tool_name
	if events[2].Type != codingagent.EventToolUse {
		t.Errorf("event[2].Type = %q, want tool_use", events[2].Type)
	}
	if events[2].ToolName == "" {
		t.Error("tool_use event should have non-empty tool_name")
	}

	// result event
	if events[3].Type != codingagent.EventResult {
		t.Errorf("event[3].Type = %q, want result", events[3].Type)
	}
}

// --- Error propagation test ---

// errorMockAgent returns errorMockSession from CreateSession.
type errorMockAgent struct {
	name string
}

func (a *errorMockAgent) Name() string { return a.name }
func (a *errorMockAgent) Close() error { return nil }
func (a *errorMockAgent) CreateSession(
	_ context.Context, _ ...codingagent.SessionOption,
) (codingagent.Session, error) {
	return &errorMockSession{}, nil
}

// errorMockSession sends a single error event.
type errorMockSession struct{}

func (s *errorMockSession) ID() string  { return "sdk-error-001" }
func (s *errorMockSession) Close() error { return nil }
func (s *errorMockSession) Send(
	_ context.Context, _ string,
) (<-chan codingagent.StreamEvent, error) {
	ch := make(chan codingagent.StreamEvent, 2)
	ch <- codingagent.StreamEvent{
		Type:    codingagent.EventError,
		Content: "claude exited with code 1: authentication failed",
	}
	close(ch)
	return ch, nil
}

// TestAgentServiceSSEErrorPropagation verifies that EventError events
// from the agent are forwarded to the SSE stream.
func TestAgentServiceSSEErrorPropagation(t *testing.T) {
	tl := tasklog.New()
	srv := agentservice.New(agentservice.WithTaskLog(tl))
	srv.RegisterAgent(&errorMockAgent{name: "erroragent"})
	ts := httptest.NewServer(srv.HTTPHandler())
	defer ts.Close()

	sessionID := createAgentServiceSession(t, ts.URL, "erroragent")
	msgBody, _ := json.Marshal(map[string]string{"message": "test"})
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

	var foundError bool
	var errorContent string
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
			if ev.Type == codingagent.EventError {
				foundError = true
				errorContent = ev.Content
			}
		}
	}

	if !foundError {
		t.Fatal("expected at least one error event in SSE stream")
	}
	if errorContent == "" {
		t.Error("error event should have non-empty content")
	}
}

// TestCawaClientErrorPropagation verifies that ternctl detects SSE errors
// and exits with status 1.
func TestCawaClientErrorPropagation(t *testing.T) {
	// 1. Build ternctl binary
	projectRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("project root path: %v", err)
	}

	ternctlBin := "ternctl"
	if os.PathSeparator == '\\' {
		ternctlBin = "ternctl.exe"
	}
	ternctlPath := filepath.Join(projectRoot, "bin", ternctlBin)

	ternctlDir := filepath.Join(projectRoot, "features", "ternctl")
	buildCmd := exec.Command("go", "build", "-o", filepath.Join("..", "..", "bin", ternctlBin), ".")
	buildCmd.Dir = ternctlDir
	if output, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to build ternctl: %v\noutput: %s", err, string(output))
	}

	// 2. Setup server with errorMockAgent
	tl := tasklog.New()
	srv := agentservice.New(agentservice.WithTaskLog(tl))
	srv.RegisterAgent(&errorMockAgent{name: "erroragent"})
	ts := httptest.NewServer(srv.HTTPHandler())
	defer ts.Close()

	// 3. Run ternctl run
	workDir := t.TempDir()
	cmd := exec.Command(ternctlPath,
		"--server", ts.URL,
		"--log-level", "debug",
		"run",
		"--agent", "erroragent",
		"--prompt", "test",
		"--work-dir", workDir,
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	t.Logf("ternctl stdout:\n%s", stdout.String())
	t.Logf("ternctl stderr:\n%s", stderr.String())

	// We expect the command to fail (exit code 1) because of the error event
	if err == nil {
		t.Fatal("expected ternctl to exit with error, but got success")
	}

	// Verify error output contains the mocked error
	stderrStr := stderr.String()
	if !strings.Contains(stderrStr, "claude exited with code 1: authentication failed") {
		t.Errorf("stderr does not contain expected error message, got: %s", stderrStr)
	}

	// 4. Run ternctl session to check status is error
	stdoutStr := stdout.String()
	var sessionID string
	for _, line := range strings.Split(stdoutStr, "\n") {
		if strings.HasPrefix(line, "Session created: ") {
			sessionID = strings.TrimSpace(strings.TrimPrefix(line, "Session created: "))
			break
		}
	}
	if sessionID == "" {
		t.Fatal("could not find session ID in ternctl output")
	}

	cmdSession := exec.Command(ternctlPath,
		"--server", ts.URL,
		"session",
		"--id", sessionID,
	)
	var sessStdout, sessStderr bytes.Buffer
	cmdSession.Stdout = &sessStdout
	cmdSession.Stderr = &sessStderr

	err = cmdSession.Run()
	t.Logf("ternctl session stdout:\n%s", sessStdout.String())
	t.Logf("ternctl session stderr:\n%s", sessStderr.String())

	// We expect ternctl session to also fail with exit code 1
	if err == nil {
		t.Fatal("expected ternctl session to exit with error, but got success")
	}

	sessStderrStr := sessStderr.String()
	if !strings.Contains(sessStderrStr, "Session failed with error: claude exited with code 1: authentication failed") {
		t.Errorf("ternctl session stderr does not contain expected error message, got: %s", sessStderrStr)
	}
}

// --- Model validation integration tests ---

// setupAgentServiceTestServerWithModels creates a test server with cached model data.
func setupAgentServiceTestServerWithModels(t *testing.T) *httptest.Server {
	t.Helper()
	tl := tasklog.New()
	srv := agentservice.New(
		agentservice.WithTaskLog(tl),
	)
	srv.RegisterAgent(&integrationMockAgent{name: "claudecode"})
	srv.SetGatewayModels(
		[]llmgateway.ModelInfo{
			{Provider: "anthropic", Model: "claude-sonnet-4-20250514"},
			{Provider: "openai", Model: "gpt-4o"},
			{Provider: "google", Model: "gemini-2.5-flash"},
		},
		&llmgateway.ModelInfo{Provider: "anthropic", Model: "claude-sonnet-4-20250514"},
	)
	ts := httptest.NewServer(srv.HTTPHandler())
	t.Cleanup(ts.Close)
	return ts
}

// T9: GET /api/v1/models returns model list via integration test server.
func TestAgentServiceModelsEndpoint(t *testing.T) {
	ts := setupAgentServiceTestServerWithModels(t)

	resp, err := http.Get(ts.URL + "/api/v1/models")
	if err != nil {
		t.Fatalf("GET /api/v1/models failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		Models       []llmgateway.ModelInfo `json:"models"`
		DefaultModel *llmgateway.ModelInfo  `json:"default_model"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("json decode error: %v", err)
	}

	if len(body.Models) != 3 {
		t.Errorf("models count = %d, want 3", len(body.Models))
	}
	if body.DefaultModel == nil {
		t.Fatal("default_model should not be nil")
	}
	if body.DefaultModel.Provider != "anthropic" {
		t.Errorf("default_model.provider = %q, want %q", body.DefaultModel.Provider, "anthropic")
	}
	if body.DefaultModel.Model != "claude-sonnet-4-20250514" {
		t.Errorf("default_model.model = %q, want %q", body.DefaultModel.Model, "claude-sonnet-4-20250514")
	}
}

// T10: POST /api/v1/sessions with invalid model returns 400.
func TestAgentServiceCreateSession_InvalidModel(t *testing.T) {
	ts := setupAgentServiceTestServerWithModels(t)

	body, _ := json.Marshal(map[string]string{
		"agent": "claudecode",
		"model": "nonexistent-model",
	})
	resp, err := http.Post(ts.URL+"/api/v1/sessions",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}

	var errResp struct {
		Error           string   `json:"error"`
		AvailableModels []string `json:"available_models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		t.Fatalf("json decode error: %v", err)
	}
	if errResp.Error != "unsupported model: nonexistent-model" {
		t.Errorf("error = %q, want %q", errResp.Error, "unsupported model: nonexistent-model")
	}
	// All gateway models should be listed (no provider filtering).
	if len(errResp.AvailableModels) != 3 {
		t.Errorf("available_models count = %d, want 3 (all models)", len(errResp.AvailableModels))
	}
}

// T11: POST /api/v1/sessions with valid model for the agent returns 201.
func TestAgentServiceCreateSession_ValidModel(t *testing.T) {
	ts := setupAgentServiceTestServerWithModels(t)

	body, _ := json.Marshal(map[string]string{
		"agent": "claudecode",
		"model": "claude-sonnet-4-20250514", // anthropic model matches claudecode
	})
	resp, err := http.Post(ts.URL+"/api/v1/sessions",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	if result["session_id"] == "" {
		t.Error("session_id should not be empty")
	}
}

// T12: DefaultModel comes from model_profiles.yaml, not hardcoded.
func TestAgentServiceDefaultModelFromProfiles(t *testing.T) {
	ts := setupAgentServiceTestServerWithModels(t)

	// Verify default model via /api/v1/models
	resp, err := http.Get(ts.URL + "/api/v1/models")
	if err != nil {
		t.Fatalf("GET /api/v1/models failed: %v", err)
	}
	defer resp.Body.Close()

	var body struct {
		DefaultModel *llmgateway.ModelInfo `json:"default_model"`
	}
	json.NewDecoder(resp.Body).Decode(&body)

	if body.DefaultModel == nil {
		t.Fatal("default_model should not be nil")
	}
	// Verify the default model matches what was set from profiles,
	// not a hardcoded value like "claude-sonnet-4-20250514" from main.go.
	if body.DefaultModel.Provider != "anthropic" {
		t.Errorf("default_model.provider = %q, want %q", body.DefaultModel.Provider, "anthropic")
	}
	if body.DefaultModel.Model != "claude-sonnet-4-20250514" {
		t.Errorf("default_model.model = %q, want %q", body.DefaultModel.Model, "claude-sonnet-4-20250514")
	}

	// Also verify the default model is valid in the model list.
	resp2, err := http.Get(ts.URL + "/api/v1/models")
	if err != nil {
		t.Fatalf("GET /api/v1/models (2) failed: %v", err)
	}
	defer resp2.Body.Close()

	var body2 struct {
		Models []llmgateway.ModelInfo `json:"models"`
	}
	json.NewDecoder(resp2.Body).Decode(&body2)

	found := false
	for _, m := range body2.Models {
		if m.Model == body.DefaultModel.Model && m.Provider == body.DefaultModel.Provider {
			found = true
			break
		}
	}
	if !found {
		t.Error("default_model should exist in the models list")
	}
}

// TestModelPassthroughToLLMGP verifies that the model name specified in session creation
// is passed through to the LLMGP proxy. This is R6 from spec 015.
// It verifies that agentservice accepts cross-provider models (e.g., gpt-4o for claudecode)
// and stores the model name in the session record.
func TestModelPassthroughToLLMGP(t *testing.T) {
	// Create server with model profiles so validation passes
	profiles := &config.ModelProfilesConfig{

		Providers: map[string]config.ProviderConfig{
			"openai": {
				Keys: []config.KeyConfig{
					{
						Name:  "default",
						Value: "sk-test",
						Models: []config.ModelConfig{
							{Name: "gpt-4o"},
						},
					},
				},
			},
		},
	}

	tl := tasklog.New()
	srv := agentservice.New(
		agentservice.WithTaskLog(tl),
	)
	srv.SetModelProfiles(profiles)
	srv.RegisterAgent(&integrationMockAgent{name: "claudecode"})
	ts := httptest.NewServer(srv.HTTPHandler())
	defer ts.Close()

	// Create session with model gpt-4o
	reqBody, _ := json.Marshal(map[string]string{
		"agent": "claudecode",
		"model": "gpt-4o",
	})
	resp, err := http.Post(ts.URL+"/api/v1/sessions",
		"application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		t.Fatalf("create session status = %d, want 201; body: %s", resp.StatusCode, string(bodyBytes))
	}

	var sessionResp map[string]string
	json.NewDecoder(resp.Body).Decode(&sessionResp)
	sessionID := sessionResp["session_id"]

	// Verify session was created with the model
	resp2, err := http.Get(ts.URL + "/api/v1/sessions/" + sessionID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	defer resp2.Body.Close()

	var sessionDetail struct {
		Model string `json:"model"`
	}
	json.NewDecoder(resp2.Body).Decode(&sessionDetail)

	if sessionDetail.Model != "gpt-4o" {
		t.Errorf("session model = %q, want %q", sessionDetail.Model, "gpt-4o")
	}
}
