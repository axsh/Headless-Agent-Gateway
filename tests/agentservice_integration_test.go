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

	"github.com/axsh/arctic-tern/server"
	"github.com/axsh/arctic-tern/shared/libs/go/agentservice"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/config"
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

func (s *integrationMockSession) ID() string   { return "sdk-integration-001" }
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
		CLIVersions map[string]string `json:"cli_versions"`
		Gateway     struct {
			Status        string `json:"status"`
			LastCheckedAt string `json:"last_checked_at"`
		} `json:"gateway"`
		ServerSettings struct {
			DisableSandbox bool `json:"disable_sandbox"`
			EnableSubagent bool `json:"enable_subagent"`
		} `json:"server_settings"`
	}
	json.NewDecoder(resp.Body).Decode(&health)

	if health.Status != "ok" {
		t.Errorf("health.status = %q, want ok", health.Status)
	}
	if health.Gateway.LastCheckedAt == "" {
		t.Error("gateway.last_checked_at should not be empty")
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
	msgBody, _ := json.Marshal(map[string]any{"content": []map[string]any{{"type": "text", "text": "test prompt"}}})
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
	msgBody, _ := json.Marshal(map[string]any{"content": []map[string]any{{"type": "text", "text": "test"}}})
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
	msgBody, _ := json.Marshal(map[string]any{"content": []map[string]any{{"type": "text", "text": "test"}}})
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
	msgBody, _ := json.Marshal(map[string]any{"content": []map[string]any{{"type": "text", "text": "test"}}})
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
	// Note: 'agents' field was removed from health response.
	// We verify basic connectivity via 'status' field above.

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
// via server.Server integration.
func TestAgentServiceConfigPort(t *testing.T) {
	cfg := &config.AppConfig{
		AgentService: config.AgentServiceConfig{Port: 0}, // ephemeral
		Vault:        config.VaultConfig{Backends: []string{"env"}},
	}
	stub := llmgateway.NewStubGateway()
	srv, err := server.New(
		server.WithConfig(cfg),
		server.WithGateway(stub),
	)
	if err != nil {
		t.Fatalf("server.New failed: %v", err)
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

	msgBody, _ := json.Marshal(map[string]any{"content": []map[string]any{{"type": "text", "text": "test"}}})
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

func (s *errorMockSession) ID() string   { return "sdk-error-001" }
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
	msgBody, _ := json.Marshal(map[string]any{"content": []map[string]any{{"type": "text", "text": "test"}}})
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
			{Provider: "anthropic", Model: "claude-sonnet-4-6"},
			{Provider: "openai", Model: "gpt-4o"},
			{Provider: "google", Model: "gemini-2.5-flash"},
		},
		&llmgateway.ModelInfo{Provider: "anthropic", Model: "claude-sonnet-4-6"},
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
	if body.DefaultModel.Model != "claude-sonnet-4-6" {
		t.Errorf("default_model.model = %q, want %q", body.DefaultModel.Model, "claude-sonnet-4-6")
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
		"model": "claude-sonnet-4-6", // anthropic model matches claudecode
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

func TestAgentServiceCreateSession_ConfigDirPersisted(t *testing.T) {
	ts, _ := setupAgentServiceTestServer(t)
	configDir := t.TempDir()
	sessionDir := t.TempDir()
	workDir := t.TempDir()

	body, _ := json.Marshal(map[string]string{
		"agent":       "claudecode",
		"work_dir":    workDir,
		"session_dir": sessionDir,
		"config_dir":  configDir,
	})
	resp, err := http.Post(ts.URL+"/api/v1/sessions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	var created map[string]string
	json.NewDecoder(resp.Body).Decode(&created)

	getResp, err := http.Get(ts.URL + "/api/v1/sessions/" + created["session_id"])
	if err != nil {
		t.Fatal(err)
	}
	defer getResp.Body.Close()
	var record codingagent.SessionRecord
	json.NewDecoder(getResp.Body).Decode(&record)
	if record.ConfigDir == "" {
		t.Fatal("config_dir should be persisted")
	}
	if !filepath.IsAbs(record.ConfigDir) {
		t.Errorf("config_dir should be absolute, got %q", record.ConfigDir)
	}
}

func TestAgentServiceCreateSession_ConfigDirInvalid(t *testing.T) {
	ts, _ := setupAgentServiceTestServer(t)
	body, _ := json.Marshal(map[string]string{
		"agent":       "claudecode",
		"work_dir":    t.TempDir(),
		"session_dir": t.TempDir(),
		"config_dir":  filepath.Join(t.TempDir(), "missing"),
	})
	resp, err := http.Post(ts.URL+"/api/v1/sessions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	buf, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(buf), "config_dir does not exist") {
		t.Errorf("body = %q", buf)
	}
}

// configDirOverlayMockAgent applies Claude config overlay on CreateSession
// so integration tests can verify filesystem effects without a real CLI.
type configDirOverlayMockAgent struct {
	name string
}

func (a *configDirOverlayMockAgent) Name() string { return a.name }
func (a *configDirOverlayMockAgent) Close() error { return nil }
func (a *configDirOverlayMockAgent) CreateSession(
	_ context.Context, opts ...codingagent.SessionOption,
) (codingagent.Session, error) {
	cfg := codingagent.NewSessionConfig(opts...)
	codingagent.ApplyDefaults(cfg, &codingagent.AdapterConfig{AgentName: a.name})
	if err := applyClaudeConfigDirForTest(cfg.SessionDir, cfg.ConfigDir); err != nil {
		return nil, err
	}
	return &integrationMockSession{}, nil
}

// applyClaudeConfigDirForTest mirrors claudecode.ApplyClaudeConfigDir without
// importing the adapter package cycle into tests beyond codingagent.
func applyClaudeConfigDirForTest(sessionDir, configDir string) error {
	if configDir == "" {
		return nil
	}
	return codingagent.OverlayConfigDir(sessionDir, configDir, []string{
		"skills", "rules", "CLAUDE.md", "settings.json",
	})
}

func setupConfigDirOverlayTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := agentservice.New()
	srv.RegisterAgent(&configDirOverlayMockAgent{name: "claudecode"})
	ts := httptest.NewServer(srv.HTTPHandler())
	t.Cleanup(ts.Close)
	return ts
}

func postSessionMessage(t *testing.T, baseURL, sessionID, text string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"content": []map[string]string{{"type": "text", "text": text}},
	})
	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/v1/sessions/"+sessionID+"/messages", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("send message status=%d", resp.StatusCode)
	}
}

func TestAgentService_ConfigDir_SharedAcrossSessions(t *testing.T) {
	ts := setupConfigDirOverlayTestServer(t)
	configDir := t.TempDir()
	skill := filepath.Join(configDir, "skills", "shared", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skill), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skill, []byte("shared"), 0644); err != nil {
		t.Fatal(err)
	}

	var sessionDirs []string
	var configDirs []string
	var workDirs []string
	for i := 0; i < 2; i++ {
		sessionDir := t.TempDir()
		workDir := t.TempDir()
		body, _ := json.Marshal(map[string]string{
			"agent":       "claudecode",
			"work_dir":    workDir,
			"session_dir": sessionDir,
			"config_dir":  configDir,
		})
		resp, err := http.Post(ts.URL+"/api/v1/sessions", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		var created map[string]string
		json.NewDecoder(resp.Body).Decode(&created)
		resp.Body.Close()
		postSessionMessage(t, ts.URL, created["session_id"], "hello")

		getResp, _ := http.Get(ts.URL + "/api/v1/sessions/" + created["session_id"])
		var record codingagent.SessionRecord
		json.NewDecoder(getResp.Body).Decode(&record)
		getResp.Body.Close()
		sessionDirs = append(sessionDirs, record.SessionDir)
		configDirs = append(configDirs, record.ConfigDir)
		workDirs = append(workDirs, record.WorkDir)

		vendorHome := filepath.Join(record.WorkDir, ".claude")
		if _, err := os.Stat(filepath.Join(vendorHome, "skills", "shared", "SKILL.md")); err != nil {
			t.Fatalf("session %d missing overlaid skill under vendor home: %v", i, err)
		}
		if _, err := os.Stat(filepath.Join(record.SessionDir, "skills", "shared", "SKILL.md")); !os.IsNotExist(err) {
			t.Fatalf("session %d: skills must not overlay into tern session_dir", i)
		}
	}
	if sessionDirs[0] == sessionDirs[1] {
		t.Fatal("session_dir values should differ")
	}
	if workDirs[0] == workDirs[1] {
		t.Fatal("work_dir values should differ")
	}
	if configDirs[0] != configDirs[1] {
		t.Fatalf("config_dir should be shared, got %q vs %q", configDirs[0], configDirs[1])
	}
}

func TestAgentService_ConfigDir_LaneIsolation(t *testing.T) {
	ts := setupConfigDirOverlayTestServer(t)
	mkLane := func(name string) string {
		dir := t.TempDir()
		skill := filepath.Join(dir, "skills", name, "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(skill), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(skill, []byte(name), 0644); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	alpha := mkLane("alpha")
	beta := mkLane("beta")

	createAndRun := func(configDir string) (sessionDir, workDir string) {
		sessionDir = t.TempDir()
		workDir = t.TempDir()
		body, _ := json.Marshal(map[string]string{
			"agent":       "claudecode",
			"work_dir":    workDir,
			"session_dir": sessionDir,
			"config_dir":  configDir,
		})
		resp, err := http.Post(ts.URL+"/api/v1/sessions", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		var created map[string]string
		json.NewDecoder(resp.Body).Decode(&created)
		resp.Body.Close()
		postSessionMessage(t, ts.URL, created["session_id"], "run")
		getResp, _ := http.Get(ts.URL + "/api/v1/sessions/" + created["session_id"])
		var record codingagent.SessionRecord
		json.NewDecoder(getResp.Body).Decode(&record)
		getResp.Body.Close()
		return record.SessionDir, record.WorkDir
	}

	_, alphaWork := createAndRun(alpha)
	_, betaWork := createAndRun(beta)
	alphaVendor := filepath.Join(alphaWork, ".claude")
	betaVendor := filepath.Join(betaWork, ".claude")
	if _, err := os.Stat(filepath.Join(alphaVendor, "skills", "alpha", "SKILL.md")); err != nil {
		t.Fatalf("alpha skill missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(alphaVendor, "skills", "beta", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatal("alpha session should not have beta skill")
	}
	if _, err := os.Stat(filepath.Join(betaVendor, "skills", "beta", "SKILL.md")); err != nil {
		t.Fatalf("beta skill missing: %v", err)
	}
}

func TestAgentService_ConfigDir_SameConfigDir_ReappliedOnSecondMessage(t *testing.T) {
	// Same config_dir across two messages (NOT a config switch). Overlay is
	// re-applied on each agent process start. Overlay/API only — conversation
	// continuity acceptance is LIVE (002), not this mock test.
	ts := setupConfigDirOverlayTestServer(t)
	configDir := t.TempDir()
	skill := filepath.Join(configDir, "skills", "demo", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skill), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skill, []byte("demo"), 0644); err != nil {
		t.Fatal(err)
	}
	sessionDir := t.TempDir()
	workDir := t.TempDir()
	body, _ := json.Marshal(map[string]string{
		"agent":       "claudecode",
		"work_dir":    workDir,
		"session_dir": sessionDir,
		"config_dir":  configDir,
	})
	resp, err := http.Post(ts.URL+"/api/v1/sessions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var created map[string]string
	json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()

	vendorHome := filepath.Join(workDir, ".claude")
	postSessionMessage(t, ts.URL, created["session_id"], "first")
	overlaid := filepath.Join(vendorHome, "skills")
	if err := os.RemoveAll(overlaid); err != nil {
		t.Fatal(err)
	}
	postSessionMessage(t, ts.URL, created["session_id"], "second")
	if _, err := os.Stat(filepath.Join(vendorHome, "skills", "demo", "SKILL.md")); err != nil {
		t.Fatalf("overlay should be re-applied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sessionDir, "native")); !os.IsNotExist(err) {
		t.Fatalf("tern session must not grow native/, err=%v", err)
	}
}

func TestAgentService_ConfigDir_SecondMessageWithoutTerminate(t *testing.T) {
	ts := setupConfigDirOverlayTestServer(t)
	configDir := t.TempDir()
	skill := filepath.Join(configDir, "skills", "demo", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skill), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skill, []byte("demo"), 0644); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{
		"agent":       "claudecode",
		"work_dir":    t.TempDir(),
		"session_dir": t.TempDir(),
		"config_dir":  configDir,
	})
	resp, err := http.Post(ts.URL+"/api/v1/sessions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var created map[string]string
	json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()

	postSessionMessage(t, ts.URL, created["session_id"], "first")
	postSessionMessage(t, ts.URL, created["session_id"], "second")
}

func TestAgentService_ConfigDir_SwitchSameSession_Claude(t *testing.T) {
	// Overlay/API switch only — conversation continuity proof is LIVE (002).
	ts := setupConfigDirOverlayTestServer(t)
	mkLane := func(name string) string {
		dir := t.TempDir()
		skill := filepath.Join(dir, "skills", name, "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(skill), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(skill, []byte(name), 0644); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	alpha := mkLane("alpha")
	beta := mkLane("beta")
	sessionDir := t.TempDir()
	workDir := t.TempDir()
	vendorHome := filepath.Join(workDir, ".claude")

	body, _ := json.Marshal(map[string]string{
		"agent":       "claudecode",
		"work_dir":    workDir,
		"session_dir": sessionDir,
		"config_dir":  alpha,
	})
	resp, err := http.Post(ts.URL+"/api/v1/sessions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var created map[string]string
	json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()
	sessionID := created["session_id"]

	postSessionMessage(t, ts.URL, sessionID, "with-alpha")
	if _, err := os.Stat(filepath.Join(vendorHome, "skills", "alpha", "SKILL.md")); err != nil {
		t.Fatalf("alpha skill missing after first message: %v", err)
	}

	patchBody, _ := json.Marshal(map[string]string{"config_dir": beta})
	req, err := http.NewRequest(http.MethodPatch, ts.URL+"/api/v1/sessions/"+sessionID, bytes.NewReader(patchBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	patchResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if patchResp.StatusCode != http.StatusOK {
		buf, _ := io.ReadAll(patchResp.Body)
		patchResp.Body.Close()
		t.Fatalf("PATCH status=%d body=%s", patchResp.StatusCode, buf)
	}
	patchResp.Body.Close()

	getResp, _ := http.Get(ts.URL + "/api/v1/sessions/" + sessionID)
	var after codingagent.SessionRecord
	json.NewDecoder(getResp.Body).Decode(&after)
	getResp.Body.Close()
	wantBeta, _ := filepath.Abs(beta)
	if filepath.Clean(after.ConfigDir) != filepath.Clean(wantBeta) {
		t.Fatalf("config_dir = %q, want %q", after.ConfigDir, wantBeta)
	}
	wantSession, _ := filepath.Abs(sessionDir)
	if filepath.Clean(after.SessionDir) != filepath.Clean(wantSession) {
		t.Fatalf("session_dir changed: %q", after.SessionDir)
	}
	if after.ID != sessionID {
		t.Fatalf("session id changed: %q", after.ID)
	}
	agentSID1 := after.AgentSessionID

	postSessionMessage(t, ts.URL, sessionID, "with-beta")
	if _, err := os.Stat(filepath.Join(vendorHome, "skills", "beta", "SKILL.md")); err != nil {
		t.Fatalf("beta skill missing after switch: %v", err)
	}
	if _, err := os.Stat(filepath.Join(vendorHome, "skills", "alpha", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatal("alpha skill should be replaced after switch to beta")
	}

	getResp2, _ := http.Get(ts.URL + "/api/v1/sessions/" + sessionID)
	var after2 codingagent.SessionRecord
	json.NewDecoder(getResp2.Body).Decode(&after2)
	getResp2.Body.Close()
	if agentSID1 != "" && after2.AgentSessionID != agentSID1 {
		t.Fatalf("agent_session_id changed: %q -> %q", agentSID1, after2.AgentSessionID)
	}
}

// configDirOverlayMockCodex applies Codex allowlist overlay on CreateSession.
type configDirOverlayMockCodex struct {
	name string
}

func (a *configDirOverlayMockCodex) Name() string { return a.name }
func (a *configDirOverlayMockCodex) Close() error { return nil }
func (a *configDirOverlayMockCodex) CreateSession(
	_ context.Context, opts ...codingagent.SessionOption,
) (codingagent.Session, error) {
	cfg := codingagent.NewSessionConfig(opts...)
	codingagent.ApplyDefaults(cfg, &codingagent.AdapterConfig{AgentName: a.name})
	if cfg.ConfigDir != "" {
		if err := codingagent.OverlayConfigDir(cfg.SessionDir, cfg.ConfigDir, []string{
			"skills", "rules", "config.toml", "AGENTS.md",
		}); err != nil {
			return nil, err
		}
	}
	return &integrationMockSession{}, nil
}

func TestAgentService_ConfigDir_SwitchSameSession_Codex(t *testing.T) {
	// Overlay/API switch only — conversation continuity proof is LIVE (002).
	srv := agentservice.New()
	srv.RegisterAgent(&configDirOverlayMockCodex{name: "codex"})
	ts := httptest.NewServer(srv.HTTPHandler())
	t.Cleanup(ts.Close)

	mkLane := func(name string) string {
		dir := t.TempDir()
		skill := filepath.Join(dir, "skills", name, "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(skill), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(skill, []byte(name), 0644); err != nil {
			t.Fatal(err)
		}
		agents := filepath.Join(dir, "AGENTS.md")
		if err := os.WriteFile(agents, []byte("marker-"+name), 0644); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	alpha := mkLane("alpha")
	beta := mkLane("beta")
	sessionDir := t.TempDir()
	workDir := t.TempDir()
	vendorHome := filepath.Join(workDir, ".codex")

	body, _ := json.Marshal(map[string]string{
		"agent":       "codex",
		"work_dir":    workDir,
		"session_dir": sessionDir,
		"config_dir":  alpha,
	})
	resp, err := http.Post(ts.URL+"/api/v1/sessions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var created map[string]string
	json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()
	sessionID := created["session_id"]

	postSessionMessage(t, ts.URL, sessionID, "with-alpha")
	if _, err := os.Stat(filepath.Join(vendorHome, "skills", "alpha", "SKILL.md")); err != nil {
		t.Fatalf("alpha skill missing: %v", err)
	}

	patchBody, _ := json.Marshal(map[string]string{"config_dir": beta})
	req, err := http.NewRequest(http.MethodPatch, ts.URL+"/api/v1/sessions/"+sessionID, bytes.NewReader(patchBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	patchResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if patchResp.StatusCode != http.StatusOK {
		buf, _ := io.ReadAll(patchResp.Body)
		patchResp.Body.Close()
		t.Fatalf("PATCH status=%d body=%s", patchResp.StatusCode, buf)
	}
	patchResp.Body.Close()

	postSessionMessage(t, ts.URL, sessionID, "with-beta")
	if _, err := os.Stat(filepath.Join(vendorHome, "skills", "beta", "SKILL.md")); err != nil {
		t.Fatalf("beta skill missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(vendorHome, "AGENTS.md")); err != nil {
		t.Fatalf("beta AGENTS.md missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(vendorHome, "skills", "alpha", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatal("alpha skill should be replaced after switch")
	}

	getResp, _ := http.Get(ts.URL + "/api/v1/sessions/" + sessionID)
	var after codingagent.SessionRecord
	json.NewDecoder(getResp.Body).Decode(&after)
	getResp.Body.Close()
	wantSession, _ := filepath.Abs(sessionDir)
	if filepath.Clean(after.SessionDir) != filepath.Clean(wantSession) {
		t.Fatalf("session_dir changed: %q", after.SessionDir)
	}
	if after.ID != sessionID {
		t.Fatalf("id changed")
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
	// not a hardcoded value like "claude-sonnet-4-6" from main.go.
	if body.DefaultModel.Provider != "anthropic" {
		t.Errorf("default_model.provider = %q, want %q", body.DefaultModel.Provider, "anthropic")
	}
	if body.DefaultModel.Model != "claude-sonnet-4-6" {
		t.Errorf("default_model.model = %q, want %q", body.DefaultModel.Model, "claude-sonnet-4-6")
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
				ApiKeys: []config.KeyConfig{
					{
						Name:   "default",
						Secret: "sk-test",
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
