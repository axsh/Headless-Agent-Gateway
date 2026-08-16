package llm_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/logger"
)

// liveCodexModel matches TestStreamReconnectLiveResumeSend. Do not pin reporter model names.
const liveCodexModel = "gpt-4o"

const (
	liveCodexReporterModel  = "gpt-5.6-terra"
	reporterShortSSETimeout = 35 * time.Second
	reporterLongSSETimeout  = 4 * time.Minute
)

type captureLogEntry struct {
	level string
	msg   string
	kv    []any
}

type captureLogger struct {
	mu      sync.Mutex
	entries []captureLogEntry
}

func (l *captureLogger) append(level, msg string, fields []any) {
	copied := append([]any(nil), fields...)
	l.mu.Lock()
	l.entries = append(l.entries, captureLogEntry{level: level, msg: msg, kv: copied})
	l.mu.Unlock()
}

func (l *captureLogger) Trace(msg string, fields ...any) { l.append("trace", msg, fields) }
func (l *captureLogger) Debug(msg string, fields ...any) { l.append("debug", msg, fields) }
func (l *captureLogger) Info(msg string, fields ...any)  { l.append("info", msg, fields) }
func (l *captureLogger) Warn(msg string, fields ...any)  { l.append("warn", msg, fields) }
func (l *captureLogger) Error(msg string, fields ...any) { l.append("error", msg, fields) }
func (l *captureLogger) WithFields(map[string]any) logger.Logger {
	return l
}
func (l *captureLogger) WithComponent(string) logger.Logger { return l }

func (l *captureLogger) find(level, msg string) (captureLogEntry, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, e := range l.entries {
		if e.level == level && e.msg == msg {
			return e, true
		}
	}
	return captureLogEntry{}, false
}

func kvLookup(kv []any, key string) (any, bool) {
	for i := 0; i+1 < len(kv); i += 2 {
		k, ok := kv[i].(string)
		if ok && k == key {
			return kv[i+1], true
		}
	}
	return nil, false
}

func kvFmt(kv []any, key string) string {
	v, ok := kvLookup(kv, key)
	if !ok {
		return "<missing>"
	}
	return fmt.Sprint(v)
}

func createLiveCodexSessionWithModel(t *testing.T, baseURL, workDir, model string) string {
	t.Helper()
	initGitRepo(t, workDir)
	body, err := json.Marshal(map[string]string{
		"agent":    "codex",
		"model":    model,
		"work_dir": workDir,
	})
	if err != nil {
		t.Fatalf("marshal create session: %v", err)
	}
	resp, err := http.Post(baseURL+"/api/v1/sessions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create session: expected 201, got %d", resp.StatusCode)
	}
	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode create session: %v", err)
	}
	sid := result["session_id"]
	if sid == "" {
		t.Fatal("create session: empty session_id")
	}
	return sid
}

func createLiveCodexSession(t *testing.T, baseURL, workDir string) string {
	return createLiveCodexSessionWithModel(t, baseURL, workDir, liveCodexModel)
}

func sendE2EMessageAllowErr(t *testing.T, baseURL, sessionID, message string, timeout time.Duration) (*http.Response, error) {
	t.Helper()
	type contentPart struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
	}
	body, _ := json.Marshal(map[string]any{
		"content": []contentPart{{Type: "text", Text: message}},
	})
	req, _ := http.NewRequest("POST",
		baseURL+"/api/v1/sessions/"+sessionID+"/messages",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	client := &http.Client{Timeout: timeout}
	return client.Do(req)
}

func listLiveSessionsByWorkDir(t *testing.T, baseURL, workDir, wantID string) {
	t.Helper()
	resp, err := http.Get(baseURL + "/api/v1/sessions?work_dir=" + url.QueryEscape(workDir))
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list sessions: expected 200, got %d", resp.StatusCode)
	}
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		t.Fatalf("read list sessions: %v", err)
	}
	if !strings.Contains(buf.String(), wantID) {
		t.Fatalf("list sessions missing %s: %s", wantID, buf.String())
	}
}

func TestLiveCodex_SingleCardReady(t *testing.T) {
	baseURL, cleanup := mustStartCodexE2EServer(t)
	defer cleanup()

	workDir := t.TempDir()
	sessionID := createLiveCodexSession(t, baseURL, workDir)
	liveReconnectTurnMustSucceed(t, baseURL, sessionID,
		"Reply with exactly: live-card-ready",
		"live-card-ready")
}

func TestLiveCodex_ResumeSameSession(t *testing.T) {
	baseURL, cleanup := mustStartCodexE2EServer(t)
	defer cleanup()

	workDir := t.TempDir()
	sessionID := createLiveCodexSession(t, baseURL, workDir)
	liveReconnectTurnMustSucceed(t, baseURL, sessionID,
		"Reply with exactly: live-resume-1", "live-resume-1")
	liveReconnectTurnMustSucceed(t, baseURL, sessionID,
		"Reply with exactly: live-resume-2", "live-resume-2")
	got := getE2ESession(t, baseURL, sessionID)
	if fmt.Sprint(got["id"]) != sessionID {
		t.Fatalf("session_id changed: %v", got["id"])
	}
}

func TestLiveCodex_ResumeAfterInProcessRestart(t *testing.T) {
	workDir := t.TempDir()
	baseURL1, cleanup1 := mustStartCodexE2EServer(t)
	sessionID := createLiveCodexSession(t, baseURL1, workDir)
	liveReconnectTurnMustSucceed(t, baseURL1, sessionID,
		"Reply with exactly: live-restart-1", "live-restart-1")
	cleanup1()

	baseURL2, cleanup2 := mustStartCodexE2EServer(t)
	defer cleanup2()
	listLiveSessionsByWorkDir(t, baseURL2, workDir, sessionID)
	rec := getE2ESession(t, baseURL2, sessionID)
	if rec == nil || fmt.Sprint(rec["id"]) == "" || fmt.Sprint(rec["id"]) == "<nil>" {
		t.Fatal("session not found after in-process restart")
	}
	if fmt.Sprint(rec["id"]) != sessionID {
		t.Fatalf("session id after restart = %v, want %s", rec["id"], sessionID)
	}

	resp := sendE2EMessage(t, baseURL2, sessionID, "Reply with exactly: live-restart-2", 4*time.Minute)
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusConflict {
		t.Fatal("got 409 after restart, session still busy")
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("send status = %d", resp.StatusCode)
	}
	events, _ := parseE2ESSEEvents(t, resp)
	sawResult := false
	var allText strings.Builder
	for _, ev := range events {
		if ev.Type == codingagent.EventError {
			t.Fatalf("received error event in live turn: %s", ev.Content)
		}
		if ev.Type == codingagent.EventResult {
			sawResult = true
		}
		if ev.Type == codingagent.EventText {
			allText.WriteString(ev.Content)
		}
	}
	if !sawResult {
		t.Fatal("turn ended without EventResult")
	}
	if !strings.Contains(allText.String(), "live-restart-2") {
		t.Fatalf("response text %q does not contain live-restart-2", allText.String())
	}
}

func TestLiveCodex_ReporterConditions_SingleCardReady(t *testing.T) {
	logs := &captureLogger{}
	baseURL, cleanup := mustStartCodexE2EServerWithLogger(t, logs)
	defer cleanup()
	workDir := t.TempDir()
	sessionID := createLiveCodexSessionWithModel(t, baseURL, workDir, liveCodexReporterModel)
	resp := sendE2EMessage(t, baseURL, sessionID, "Reply with exactly: live-terra-ready", reporterLongSSETimeout)
	defer resp.Body.Close()
	events, _ := parseE2ESSEEvents(t, resp)

	var sawResult, sawErr bool
	var errContent string
	var allText strings.Builder
	for _, ev := range events {
		if ev.Type == codingagent.EventResult {
			sawResult = true
		}
		if ev.Type == codingagent.EventError {
			sawErr = true
			errContent = ev.Content
		}
		if ev.Type == codingagent.EventText {
			allText.WriteString(ev.Content)
		}
	}
	entry, hasExhaust := logs.find("error", "codex process retry exhausted")
	stderr := kvFmt(entry.kv, "stderr")
	reproduced := !sawResult && sawErr &&
		strings.Contains(errContent, "exit status 1") &&
		strings.Contains(errContent, "["+codingagent.ErrorCodeUpstreamError+"]") &&
		hasExhaust &&
		kvFmt(entry.kv, "attempt") == "3" &&
		kvFmt(entry.kv, "max_attempts") == "3" &&
		kvFmt(entry.kv, "resume_mode") == "fresh" &&
		kvFmt(entry.kv, "exit_status") == "1" &&
		strings.Contains(stderr, "exit status 1") &&
		strings.Contains(stderr, "["+codingagent.ErrorCodeUpstreamError+"]")

	if sawResult && !sawErr && strings.Contains(allText.String(), "live-terra-ready") {
		t.Logf("reproduced_reporter_exhaustion=false")
		t.Logf("gpt-5.6-terra returned EventResult in this environment")
		return
	}
	t.Logf("reproduced_reporter_exhaustion=%v", reproduced)
	t.Logf("session_id=%s saw_result=%v saw_error=%v err=%q exhaust=%v", sessionID, sawResult, sawErr, errContent, hasExhaust)
	if hasExhaust {
		t.Logf("attempt=%s max_attempts=%s resume_mode=%s exit_status=%s stderr=%q terminal_content=%s agent_session_id_empty=%s",
			kvFmt(entry.kv, "attempt"),
			kvFmt(entry.kv, "max_attempts"),
			kvFmt(entry.kv, "resume_mode"),
			kvFmt(entry.kv, "exit_status"),
			stderr,
			kvFmt(entry.kv, "terminal_content"),
			kvFmt(entry.kv, "agent_session_id_empty"))
	}
	if !reproduced {
		return
	}
	resend, resendErr := sendE2EMessageAllowErr(t, baseURL, sessionID, "Reply with exactly: live-terra-resend", 30*time.Second)
	if resend != nil {
		t.Logf("resend_status=%d", resend.StatusCode)
		_, _ = io.Copy(io.Discard, resend.Body)
		resend.Body.Close()
	} else {
		t.Logf("resend_status=0 resend_err=%v", resendErr)
	}
}

func TestLiveCodex_ReporterConditions_ShortSSERead(t *testing.T) {
	logs := &captureLogger{}
	baseURL, cleanup := mustStartCodexE2EServerWithLogger(t, logs)
	defer cleanup()
	workDir := t.TempDir()
	sessionID := createLiveCodexSessionWithModel(t, baseURL, workDir, liveCodexReporterModel)
	resp, err := sendE2EMessageAllowErr(t, baseURL, sessionID, "Reply with exactly: live-terra-short", reporterShortSSETimeout)
	if resp != nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	t.Logf("short_sse_do_err=%v", err)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := logs.find("warn", "client disconnected during SSE stream"); ok {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	entry, ok := logs.find("warn", "client disconnected during SSE stream")
	if ok {
		t.Logf("disconnect_session_id=%s events_sent=%s", kvFmt(entry.kv, "session_id"), kvFmt(entry.kv, "events_sent"))
	} else {
		t.Logf("client disconnected during SSE stream not observed (stream may have finished before 35s)")
	}
	if _, ok := logs.find("warn", "SSE drain timed out; stopping agent process"); ok {
		t.Logf("observed drain timeout log")
	}
}
