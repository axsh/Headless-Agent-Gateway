package llm_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

// liveCodexModel matches TestStreamReconnectLiveResumeSend. Do not pin reporter model names.
const liveCodexModel = "gpt-4o"

func createLiveCodexSession(t *testing.T, baseURL, workDir string) string {
	t.Helper()
	initGitRepo(t, workDir)
	body, err := json.Marshal(map[string]string{
		"agent":    "codex",
		"model":    liveCodexModel,
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
