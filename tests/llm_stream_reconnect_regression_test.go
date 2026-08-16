package llm_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/agentservice"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent/codex"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent/codex/testfake"
	"github.com/axsh/arctic-tern/shared/libs/go/config"
	"github.com/axsh/arctic-tern/shared/libs/go/llmgateway"
	"github.com/axsh/arctic-tern/shared/libs/go/logger"
)

func newFakeCodexHTTP(t *testing.T, retry config.ProcessRetryConfig, opts ...agentservice.ServerOption) (*httptest.Server, *codex.CodexAdapter) {
	t.Helper()
	log := logger.NewDefault(logger.LevelInfo)
	adapter := codex.New(&codingagent.AdapterConfig{
		Logger: log,
	})
	all := []agentservice.ServerOption{
		agentservice.WithProcessRetry(retry),
		agentservice.WithSandboxDisabled(true),
		agentservice.WithLogger(log),
	}
	all = append(all, opts...)
	srv := agentservice.New(all...)
	srv.RegisterAgent(adapter)
	srv.SetGatewayModels(
		[]llmgateway.ModelInfo{{Provider: "openai", Model: "gpt-4o"}},
		&llmgateway.ModelInfo{Provider: "openai", Model: "gpt-4o"},
	)
	return httptest.NewServer(srv.HTTPHandler()), adapter
}

func TestStreamReconnectRegression_FakeCLIInProcessJSONL(t *testing.T) {
	fakeDir := t.TempDir()
	launchLog := filepath.Join(fakeDir, "launch.log")
	testfake.Install(t, fakeDir, testfake.Options{
		Lines: []string{
			`{"type":"thread.started","thread_id":"thr-regress-1"}`,
			`{"type":"error","message":"Reconnecting... 1/5 (We're currently experiencing high demand, which may cause temporary errors.)"}`,
			`{"type":"turn.completed"}`,
		},
		LineDelay:     80 * time.Millisecond,
		LaunchLogPath: launchLog,
		ExitCode:      0,
	})

	ts, _ := newFakeCodexHTTP(t, config.ProcessRetryConfig{MaxAttempts: 3, IntervalSeconds: 0})
	defer ts.Close()

	sessionID := createReconnectSession(t, ts, "codex")
	body, code := postReconnectSSE(t, ts, sessionID, "ping")
	if code != http.StatusOK {
		t.Fatalf("status code = %d, want 200", code)
	}
	if errCount := sseErrorCount(body); errCount != 0 {
		t.Fatalf("unexpected SSE errors in stream (got %d): %s", errCount, body)
	}
	if !strings.Contains(body, `"type":"result"`) {
		t.Fatalf("missing result event in SSE response: %s", body)
	}
	if count := testfake.LaunchCount(t, launchLog); count != 1 {
		t.Fatalf("launch count = %d, want 1", count)
	}
}

func TestStreamReconnectRegression_ThreeResumeSends(t *testing.T) {
	fakeDir := t.TempDir()
	launchLog := filepath.Join(fakeDir, "launch.log")
	testfake.Install(t, fakeDir, testfake.Options{
		FailLaunches: []int{2}, // Turn 2 first launch fails with retryable exit 1
		Lines: []string{
			`{"type":"thread.started","thread_id":"thr-regress-resume"}`,
			`{"type":"turn.completed"}`,
		},
		LaunchLogPath: launchLog,
		ExitCode:      0,
	})

	ts, _ := newFakeCodexHTTP(t, config.ProcessRetryConfig{MaxAttempts: 3, IntervalSeconds: 0})
	defer ts.Close()

	sessionID := createReconnectSession(t, ts, "codex")

	// Turn 1: success on launch 1
	body1, code1 := postReconnectSSE(t, ts, sessionID, "turn 1")
	if code1 != http.StatusOK || sseErrorCount(body1) != 0 || !strings.Contains(body1, `"type":"result"`) {
		t.Fatalf("turn 1 failed: code=%d errs=%d body=%s", code1, sseErrorCount(body1), body1)
	}

	// Turn 2: launch 2 fails with retryable exit 1, launch 3 retries and succeeds
	body2, code2 := postReconnectSSE(t, ts, sessionID, "turn 2")
	if code2 != http.StatusOK || sseErrorCount(body2) != 0 || !strings.Contains(body2, `"type":"result"`) {
		t.Fatalf("turn 2 failed: code=%d errs=%d body=%s", code2, sseErrorCount(body2), body2)
	}

	// Turn 3: success on launch 4
	body3, code3 := postReconnectSSE(t, ts, sessionID, "turn 3")
	if code3 != http.StatusOK || sseErrorCount(body3) != 0 || !strings.Contains(body3, `"type":"result"`) {
		t.Fatalf("turn 3 failed: code=%d errs=%d body=%s", code3, sseErrorCount(body3), body3)
	}

	if count := testfake.LaunchCount(t, launchLog); count != 4 {
		t.Fatalf("total launches = %d, want 4 (1 + 2 retries + 1)", count)
	}

	// Verify session is not stuck in busy state
	body4, code4 := postReconnectSSE(t, ts, sessionID, "turn 4")
	if code4 == http.StatusConflict {
		t.Fatalf("session returned 409 busy on subsequent turn: %s", body4)
	}
}

func TestStreamReconnectRegression_DisconnectDoesNotKillFake(t *testing.T) {
	fakeDir := t.TempDir()
	pidFile := filepath.Join(fakeDir, "fake.pid")
	heartbeatFile := filepath.Join(fakeDir, "heartbeat.txt")
	testfake.Install(t, fakeDir, testfake.Options{
		Lines: []string{
			`{"type":"thread.started","thread_id":"thr-disconnect"}`,
			`{"type":"item.started"}`,
			`{"type":"turn.completed"}`,
		},
		LineDelay:     400 * time.Millisecond,
		PIDFile:       pidFile,
		HeartbeatPath: heartbeatFile,
		ExitCode:      0,
	})

	ts, _ := newFakeCodexHTTP(t, config.ProcessRetryConfig{MaxAttempts: 1, IntervalSeconds: 0})
	defer ts.Close()

	sessionID := createReconnectSession(t, ts, "codex")

	ctx, cancel := context.WithCancel(context.Background())
	raw, _ := json.Marshal(map[string]any{
		"content": []map[string]string{{"type": "text", "text": "slow turn"}},
	})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+"/api/v1/sessions/"+sessionID+"/messages", bytes.NewReader(raw))
	req.Header.Set("Accept", "text/event-stream")

	go func() {
		time.Sleep(80 * time.Millisecond)
		cancel()
	}()

	resp, err := http.DefaultClient.Do(req)
	if err == nil {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}

	// 150ms after cancel (turn takes 400ms * 2 = 800ms): verify process is still alive and heartbeat updates
	time.Sleep(150 * time.Millisecond)
	var hb1 int64
	for attempt := 0; attempt < 10; attempt++ {
		data1, err := os.ReadFile(heartbeatFile)
		if err == nil {
			if v, err := strconv.ParseInt(strings.TrimSpace(string(data1)), 10, 64); err == nil && v > 0 {
				hb1 = v
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if hb1 == 0 {
		t.Fatalf("failed to read valid heartbeat hb1 from %s", heartbeatFile)
	}

	var hb2 int64
	for attempt := 0; attempt < 15; attempt++ {
		time.Sleep(30 * time.Millisecond)
		data2, err := os.ReadFile(heartbeatFile)
		if err == nil {
			if v, err := strconv.ParseInt(strings.TrimSpace(string(data2)), 10, 64); err == nil && v > hb1 {
				hb2 = v
				break
			}
		}
	}

	if hb2 <= hb1 {
		t.Fatalf("heartbeat did not advance while turn in progress (hb1=%d, hb2=%d), process killed prematurely", hb1, hb2)
	}

	// Wait for turn to complete and drain
	time.Sleep(1200 * time.Millisecond)

	// Verify next message can be sent without 409 conflict
	_, code := postReconnectSSE(t, ts, sessionID, "after disconnect")
	if code == http.StatusConflict {
		t.Fatalf("session still busy after disconnect drain completed")
	}
}

func TestStreamReconnectRegression_GenericExit1RetriesWithoutSSEError(t *testing.T) {
	fakeDir := t.TempDir()
	launchLog := filepath.Join(fakeDir, "launch.log")
	testfake.Install(t, fakeDir, testfake.Options{
		SilentFail:   true,
		FailLaunches: []int{1},
		Lines: []string{
			`{"type":"thread.started","thread_id":"thr-generic"}`,
			`{"type":"turn.completed"}`,
		},
		LaunchLogPath: launchLog,
		ExitCode:      0,
	})

	ts, _ := newFakeCodexHTTP(t, config.ProcessRetryConfig{MaxAttempts: 3, IntervalSeconds: 0})
	defer ts.Close()

	sessionID := createReconnectSession(t, ts, "codex")
	body, code := postReconnectSSE(t, ts, sessionID, "ping")
	if code != http.StatusOK {
		t.Fatalf("status code = %d, want 200 body=%s", code, body)
	}
	if errCount := sseErrorCount(body); errCount != 0 {
		t.Fatalf("unexpected SSE errors (got %d): %s", errCount, body)
	}
	if strings.Contains(body, `"type":"error"`) && strings.Contains(body, "exit status 1") {
		t.Fatalf("client saw exit status 1 error event: %s", body)
	}
	if !strings.Contains(body, `"type":"result"`) {
		t.Fatalf("missing result event: %s", body)
	}
	if count := testfake.LaunchCount(t, launchLog); count != 2 {
		t.Fatalf("launch count = %d, want 2", count)
	}
}

func TestStreamReconnectRegression_BrokenResumeThreadSelfHeals(t *testing.T) {
	fakeDir := t.TempDir()
	launchLog := filepath.Join(fakeDir, "launch.log")
	testfake.Install(t, fakeDir, testfake.Options{
		FailResumeIDs: []string{"thr-broken"},
		ThreadIDByLaunch: map[int]string{
			1: "thr-broken",
			3: "thr-healed",
		},
		Lines: []string{
			`{"type":"thread.started","thread_id":"thr-placeholder"}`,
			`{"type":"turn.completed"}`,
		},
		LaunchLogPath: launchLog,
		ExitCode:      0,
	})

	ts, _ := newFakeCodexHTTP(t, config.ProcessRetryConfig{MaxAttempts: 3, IntervalSeconds: 0})
	defer ts.Close()

	sessionID := createReconnectSession(t, ts, "codex")
	body1, code1 := postReconnectSSE(t, ts, sessionID, "turn 1")
	if code1 != http.StatusOK || sseErrorCount(body1) != 0 || !strings.Contains(body1, `"type":"result"`) {
		t.Fatalf("turn 1 failed: code=%d body=%s", code1, body1)
	}

	body2, code2 := postReconnectSSE(t, ts, sessionID, "turn 2")
	if code2 != http.StatusOK || sseErrorCount(body2) != 0 || !strings.Contains(body2, `"type":"result"`) {
		t.Fatalf("turn 2 failed: code=%d body=%s", code2, body2)
	}

	logData, err := os.ReadFile(launchLog)
	if err != nil {
		t.Fatal(err)
	}
	logText := string(logData)
	if !strings.Contains(logText, "resume") || !strings.Contains(logText, "thr-broken") {
		t.Fatalf("expected resume of thr-broken in launch log: %s", logText)
	}
	lines := strings.Split(strings.TrimSpace(logText), "\n")
	sawBrokenResume := false
	sawFreshAfter := false
	for _, line := range lines {
		if strings.Contains(line, "resume") && strings.Contains(line, "thr-broken") {
			sawBrokenResume = true
			continue
		}
		if sawBrokenResume && strings.Contains(line, "launch") && !strings.Contains(line, "resume") {
			sawFreshAfter = true
			break
		}
	}
	if !sawFreshAfter {
		t.Fatalf("expected fresh exec without resume after broken resume: %s", logText)
	}

	body3, code3 := postReconnectSSE(t, ts, sessionID, "turn 3")
	if code3 != http.StatusOK || sseErrorCount(body3) != 0 || !strings.Contains(body3, `"type":"result"`) {
		t.Fatalf("turn 3 failed: code=%d body=%s", code3, body3)
	}
}

func TestStreamReconnectRegression_DrainTimeoutUnregistersBusy(t *testing.T) {
	fakeDir := t.TempDir()
	pidFile := filepath.Join(fakeDir, "fake.pid")
	testfake.Install(t, fakeDir, testfake.Options{
		HangForever: true,
		PIDFile:     pidFile,
		Lines: []string{
			`{"type":"thread.started","thread_id":"thr-hang"}`,
		},
	})

	ts, _ := newFakeCodexHTTP(t,
		config.ProcessRetryConfig{MaxAttempts: 1, IntervalSeconds: 0},
		agentservice.WithSSEDrainTimeout(80*time.Millisecond),
	)
	defer ts.Close()

	sessionID := createReconnectSession(t, ts, "codex")
	ctx, cancel := context.WithCancel(context.Background())
	raw, _ := json.Marshal(map[string]any{
		"content": []map[string]string{{"type": "text", "text": "hang"}},
	})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+"/api/v1/sessions/"+sessionID+"/messages", bytes.NewReader(raw))
	req.Header.Set("Accept", "text/event-stream")
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	resp, err := http.DefaultClient.Do(req)
	if err == nil {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}

	deadline := time.Now().Add(1 * time.Second)
	var code int
	for time.Now().Before(deadline) {
		_, code = postReconnectSSE(t, ts, sessionID, "after hang")
		if code != http.StatusConflict {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if code == http.StatusConflict {
		t.Fatal("session still busy after drain timeout")
	}
}
