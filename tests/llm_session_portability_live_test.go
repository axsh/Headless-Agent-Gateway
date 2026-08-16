package llm_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os/exec"
	"testing"
)

func requireCLI(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Fatalf("LIVE test requires %s CLI on PATH: %v", name, err)
	}
}

func liveCreateAndSend(t *testing.T, baseURL, agent, model, workDir, prompt string) (sessionID string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"agent":    agent,
		"model":    model,
		"work_dir": workDir,
	})
	resp, err := http.Post(baseURL+"/api/v1/sessions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		buf := make([]byte, 1024)
		n, _ := resp.Body.Read(buf)
		resp.Body.Close()
		t.Fatalf("create %d %s", resp.StatusCode, buf[:n])
	}
	var created map[string]string
	json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()
	sessionID = created["session_id"]
	liveSend(t, baseURL, sessionID, prompt)
	return sessionID
}

func liveSend(t *testing.T, baseURL, sessionID, prompt string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"content": []map[string]string{{"type": "text", "text": prompt}},
	})
	resp, err := http.Post(baseURL+"/api/v1/sessions/"+sessionID+"/messages", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		buf := make([]byte, 2048)
		n, _ := resp.Body.Read(buf)
		t.Fatalf("send %d %s", resp.StatusCode, buf[:n])
	}
}

func livePatch(t *testing.T, baseURL, sessionID string, payload any) {
	t.Helper()
	raw, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPatch, baseURL+"/api/v1/sessions/"+sessionID, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		buf := make([]byte, 2048)
		n, _ := resp.Body.Read(buf)
		t.Fatalf("patch %d %s", resp.StatusCode, buf[:n])
	}
}

func TestSessionPortabilityLiveBaseline(t *testing.T) {
	requireCLI(t, "claude")
	baseURL, cleanup := startE2EServer(t)
	defer cleanup()
	workDir := t.TempDir()
	initGitRepo(t, workDir)
	id := liveCreateAndSend(t, baseURL, "claudecode", e2eDefaultModel, workDir,
		"Remember the exact token CTX-TOKEN-7F3A. Reply with a short ack.")
	liveSend(t, baseURL, id, "Reply with the exact token I asked you to remember.")
}

func TestSessionPortabilityLiveModelSwitch(t *testing.T) {
	requireCLI(t, "claude")
	baseURL, cleanup := startE2EServer(t)
	defer cleanup()
	workDir := t.TempDir()
	initGitRepo(t, workDir)
	id := liveCreateAndSend(t, baseURL, "claudecode", e2eDefaultModel, workDir,
		"Remember the exact token CTX-TOKEN-7F3A. Reply with a short ack.")
	livePatch(t, baseURL, id, map[string]string{"model": e2eDefaultModel})
	liveSend(t, baseURL, id, "Reply with the exact token I asked you to remember.")
}

func TestSessionPortabilityLiveSwitch(t *testing.T) {
	requireCLI(t, "claude")
	requireCLI(t, "codex")
	baseURL, cleanup := startE2EServer(t)
	defer cleanup()
	workDir := t.TempDir()
	initGitRepo(t, workDir)
	id := liveCreateAndSend(t, baseURL, "claudecode", e2eDefaultModel, workDir,
		"Remember the exact token CTX-TOKEN-7F3A. Reply with a short ack.")
	livePatch(t, baseURL, id, map[string]string{"agent": "codex"})
	liveSend(t, baseURL, id, "Reply with the exact token from the prior transferred context: CTX-TOKEN-7F3A if you see it.")
}

func TestSessionPortabilityLiveRoundTrip(t *testing.T) {
	requireCLI(t, "claude")
	requireCLI(t, "codex")
	baseURL, cleanup := startE2EServer(t)
	defer cleanup()
	workDir := t.TempDir()
	initGitRepo(t, workDir)
	id := liveCreateAndSend(t, baseURL, "claudecode", e2eDefaultModel, workDir,
		"Remember token CTX-TOKEN-7F3A.")
	livePatch(t, baseURL, id, map[string]string{"agent": "codex"})
	liveSend(t, baseURL, id, "Remember Codex note CODEX-DELTA-1 as well.")
	livePatch(t, baseURL, id, map[string]string{"agent": "claudecode"})
	liveSend(t, baseURL, id, "Repeat CTX-TOKEN-7F3A and mention CODEX-DELTA-1 if present in transferred context.")
}
