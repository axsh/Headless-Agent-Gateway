// Package llm_test contains E2E tests for client/v1 SSE consumption with large tool output.
package llm_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	v1 "github.com/axsh/arctic-tern/client/v1"
	"github.com/axsh/arctic-tern/shared/libs/go/agentservice"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent/codex"
	"github.com/axsh/arctic-tern/shared/libs/go/config"
	"github.com/axsh/arctic-tern/shared/libs/go/logger"
	"github.com/axsh/arctic-tern/tests/testutil"
)

func startFakeCodexE2EServer(t *testing.T) (baseURL string, cleanup func()) {
	return startFakeCodexE2EServerWithLines(t, testutil.FakeCodexOptions{})
}

func startFakeCodexE2EServerWithLines(t *testing.T, opts testutil.FakeCodexOptions) (baseURL string, cleanup func()) {
	t.Helper()
	dir := t.TempDir()
	testutil.InstallFakeCodex(t, dir, opts)
	testutil.PrependPath(t, dir)

	srv := agentservice.New(
		agentservice.WithLogger(logger.NewDefault(logger.LevelInfo)),
	)
	srv.SetModelProfiles(&config.ModelProfilesConfig{
		CodingAgents: map[string]config.AgentConfig{
			"codex": {ExecutionMode: codingagent.ExecutionModeSingleShot},
		},
	})
	adapter := codex.New(&codingagent.AdapterConfig{
		Logger: logger.NewDefault(logger.LevelInfo),
	})
	srv.RegisterAgent(adapter)

	ctx := context.Background()
	port := freePort(t)
	if err := srv.Launch(ctx, port); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	return "http://localhost:" + strconv.Itoa(port), func() {
		srv.Shutdown(context.Background())
	}
}

func assertSSEDataLinesUnder64KB(t *testing.T, sseBody string) {
	t.Helper()
	scanner := bufio.NewScanner(strings.NewReader(sseBody))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		if len(data) >= 64*1024 {
			t.Fatalf("SSE data line len %d >= 64KiB", len(data))
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("default scanner failed: %v", err)
	}
}

func rawPostSSE(t *testing.T, baseURL, sessionID, message string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"content": []map[string]string{{"type": "text", "text": message}},
	})
	req, err := http.NewRequest(http.MethodPost,
		baseURL+"/api/v1/sessions/"+sessionID+"/messages",
		bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST SSE: %v", err)
	}
	defer resp.Body.Close()

	var buf strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		buf.WriteString(scanner.Text())
		buf.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read SSE: %v", err)
	}
	return buf.String()
}

func TestCodexE2E_ClientV1_LargeToolOutputTerminalEvent(t *testing.T) {
	baseURL, cleanup := startFakeCodexE2EServer(t)
	defer cleanup()

	ctx := context.Background()
	workDir := t.TempDir()
	client := v1.New(baseURL, v1.WithNoTimeout())

	sess, err := client.CreateSession(ctx, v1.SessionRequest{
		Agent:   "codex",
		WorkDir: workDir,
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	stream, err := sess.SendText(ctx, "trigger")
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}

	wantToolContent := strings.Repeat("x", 65537)

	var gotResult bool
	var toolResults []string
	err = stream.RunWithHandlers(ctx, sess, v1.StreamHandlers{
		OnToolResult: func(content string) { toolResults = append(toolResults, content) },
		OnResult:     func() { gotResult = true },
	})
	if err != nil {
		t.Fatalf("RunWithHandlers: %v", err)
	}
	if !gotResult {
		t.Fatal("expected EventResult, got none")
	}
	if len(toolResults) != 1 {
		t.Fatalf("expected 1 tool_result callback, got %d", len(toolResults))
	}
	if toolResults[0] != wantToolContent {
		t.Fatalf("tool_result len = %d, want %d; prefix match = %v",
			len(toolResults[0]), len(wantToolContent),
			strings.HasPrefix(toolResults[0], wantToolContent[:32]))
	}

	session, err := client.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	status, _ := session["status"].(string)
	if status != "completed" {
		t.Fatalf("session status = %q, want completed", status)
	}
}

func TestCodexE2E_ClientV1_MaxTruncatedToolOutputTerminalEvent(t *testing.T) {
	lines := testutil.BuildLargeAggregatedOutputLines(codingagent.DefaultMaxToolResultBytes)
	baseURL, cleanup := startFakeCodexE2EServerWithLines(t, testutil.FakeCodexOptions{Lines: lines})
	defer cleanup()

	ctx := context.Background()
	workDir := t.TempDir()
	client := v1.New(baseURL, v1.WithNoTimeout())

	sess, err := client.CreateSession(ctx, v1.SessionRequest{
		Agent:   "codex",
		WorkDir: workDir,
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	wantToolContent := strings.Repeat("x", codingagent.DefaultMaxToolResultBytes)

	stream, err := sess.SendText(ctx, "trigger")
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}

	var gotResult bool
	var toolResults []string
	err = stream.RunWithHandlers(ctx, sess, v1.StreamHandlers{
		OnToolResult: func(content string) { toolResults = append(toolResults, content) },
		OnResult:     func() { gotResult = true },
	})
	if err != nil {
		t.Fatalf("RunWithHandlers: %v", err)
	}
	if !gotResult {
		t.Fatal("expected EventResult, got none")
	}
	if len(toolResults) != 1 {
		t.Fatalf("expected 1 tool_result, got %d", len(toolResults))
	}
	if len(toolResults[0]) != len(wantToolContent) {
		t.Fatalf("tool_result len = %d, want %d", len(toolResults[0]), len(wantToolContent))
	}

	session, err := client.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	status, _ := session["status"].(string)
	if status != "completed" {
		t.Fatalf("session status = %q, want completed", status)
	}

	// Raw SSE on a second session: default scanner must read all data lines.
	sess2, err := client.CreateSession(ctx, v1.SessionRequest{
		Agent:   "codex",
		WorkDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("CreateSession (raw): %v", err)
	}
	sseBody := rawPostSSE(t, baseURL, sess2.ID, "trigger")
	assertSSEDataLinesUnder64KB(t, sseBody)
}

func TestCodexE2E_ClientV1_NoDataSilenceDuringLargeToolTurn(t *testing.T) {
	lines := testutil.BuildDelayedLargeOutputLines(codingagent.DefaultMaxToolResultBytes)
	baseURL, cleanup := startFakeCodexE2EServerWithLines(t, testutil.FakeCodexOptions{
		Lines:   lines,
		DelayMS: 2000,
	})
	defer cleanup()

	ctx := context.Background()
	client := v1.New(baseURL, v1.WithNoTimeout())
	sess, err := client.CreateSession(ctx, v1.SessionRequest{
		Agent:   "codex",
		WorkDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	stream, err := sess.SendText(ctx, "trigger")
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}

	start := time.Now()
	var lastDataTime time.Time
	var prevDataTime time.Time
	gotResult := false

	for ev := range stream.Events() {
		now := time.Now()
		lastDataTime = now
		if !prevDataTime.IsZero() {
			gap := now.Sub(prevDataTime)
			if gap >= 30*time.Second {
				t.Fatalf("data event gap %v >= 30s", gap)
			}
		}
		prevDataTime = now

		switch ev.Type {
		case v1.EventResult:
			gotResult = true
		case v1.EventError:
			t.Fatalf("stream error: %s", ev.Error)
		}
	}

	if !gotResult {
		t.Fatal("expected EventResult")
	}
	if time.Since(start) >= 60*time.Second {
		t.Fatalf("turn took %v, want < 60s", time.Since(start))
	}
	if lastDataTime.IsZero() {
		t.Fatal("expected at least one data event")
	}

	session, err := client.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	status, _ := session["status"].(string)
	if status != "completed" {
		t.Fatalf("session status = %q, want completed", status)
	}
}

func TestCodexE2E_ClientV1_RipgrepLikeMultiLineOutput(t *testing.T) {
	content := testutil.BuildRipgrepLikeOutput(200 * 1024)
	lines := testutil.BuildAggregatedOutputLinesFromContent(content)
	baseURL, cleanup := startFakeCodexE2EServerWithLines(t, testutil.FakeCodexOptions{Lines: lines})
	defer cleanup()

	ctx := context.Background()
	client := v1.New(baseURL, v1.WithNoTimeout())
	sess, err := client.CreateSession(ctx, v1.SessionRequest{
		Agent:   "codex",
		WorkDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	stream, err := sess.SendText(ctx, "trigger")
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}

	var toolResults []string
	var gotResult bool
	err = stream.RunWithHandlers(ctx, sess, v1.StreamHandlers{
		OnToolResult: func(c string) { toolResults = append(toolResults, c) },
		OnResult:     func() { gotResult = true },
	})
	if err != nil {
		t.Fatalf("RunWithHandlers: %v", err)
	}
	if !gotResult {
		t.Fatal("expected EventResult")
	}
	if len(toolResults) != 1 {
		t.Fatalf("expected 1 tool_result, got %d", len(toolResults))
	}
	if toolResults[0] != content {
		t.Fatalf("content mismatch: len=%d want=%d", len(toolResults[0]), len(content))
	}

	session, err := client.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	status, _ := session["status"].(string)
	if status != "completed" {
		t.Fatalf("session status = %q, want completed", status)
	}
}

func TestCodexE2E_ClientV1_MultipleLargeToolResults(t *testing.T) {
	const size = 100 * 1024
	lines := testutil.BuildMultiToolOutputLines(size, size)
	baseURL, cleanup := startFakeCodexE2EServerWithLines(t, testutil.FakeCodexOptions{Lines: lines})
	defer cleanup()

	ctx := context.Background()
	client := v1.New(baseURL, v1.WithNoTimeout())
	sess, err := client.CreateSession(ctx, v1.SessionRequest{
		Agent:   "codex",
		WorkDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	stream, err := sess.SendText(ctx, "trigger")
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}

	var toolResults []string
	var gotResult bool
	err = stream.RunWithHandlers(ctx, sess, v1.StreamHandlers{
		OnToolResult: func(c string) { toolResults = append(toolResults, c) },
		OnResult:     func() { gotResult = true },
	})
	if err != nil {
		t.Fatalf("RunWithHandlers: %v", err)
	}
	if !gotResult {
		t.Fatal("expected EventResult")
	}
	if len(toolResults) != 2 {
		t.Fatalf("expected 2 tool_results, got %d", len(toolResults))
	}
	if len(toolResults[0]) != size || len(toolResults[1]) != size {
		t.Fatalf("tool result sizes = %d, %d, want %d each", len(toolResults[0]), len(toolResults[1]), size)
	}
	if toolResults[0][0] == toolResults[1][0] {
		t.Fatal("expected distinct tool result content")
	}

	session, err := client.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	status, _ := session["status"].(string)
	if status != "completed" {
		t.Fatalf("session status = %q, want completed", status)
	}
}
