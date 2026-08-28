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
	"strings"
	"testing"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/agentservice"
	"github.com/axsh/arctic-tern/shared/libs/go/artifact/store"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent/codex"
	"github.com/axsh/arctic-tern/shared/libs/go/tasklog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type turnDiffFakeAgent struct {
	name   string
	events []codingagent.StreamEvent
}

func (a *turnDiffFakeAgent) Name() string { return a.name }
func (a *turnDiffFakeAgent) Close() error { return nil }
func (a *turnDiffFakeAgent) CreateSession(_ context.Context, _ ...codingagent.SessionOption) (codingagent.Session, error) {
	return &turnDiffFakeSession{events: a.events}, nil
}

type turnDiffFakeSession struct {
	events []codingagent.StreamEvent
}

func (s *turnDiffFakeSession) ID() string   { return "turn-diff-fake" }
func (s *turnDiffFakeSession) Close() error { return nil }
func (s *turnDiffFakeSession) Send(_ context.Context, _ string) (<-chan codingagent.StreamEvent, error) {
	ch := make(chan codingagent.StreamEvent, len(s.events)+1)
	go func() {
		defer close(ch)
		for _, ev := range s.events {
			ch <- ev
		}
		ch <- codingagent.StreamEvent{Type: codingagent.EventResult}
	}()
	return ch, nil
}

func startTurnDiffArtifactServer(t *testing.T, agent codingagent.CodingAgent, collectors map[string]any) (baseURL, sessionID string, st store.ArtifactStore) {
	t.Helper()
	workDir := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "artifacts.db")
	artifactStore, err := store.NewSQLiteStore(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = artifactStore.Close() })

	tl := tasklog.New()
	srv := agentservice.New(
		agentservice.WithTaskLog(tl),
		agentservice.WithArtifactStore(artifactStore, workDir),
	)
	srv.RegisterAgent(agent)
	ts := httptest.NewServer(srv.HTTPHandler())
	t.Cleanup(ts.Close)

	body := map[string]any{
		"agent":    agent.Name(),
		"work_dir": workDir,
	}
	if collectors != nil {
		body["file_change_collectors"] = collectors
	}
	b, _ := json.Marshal(body)
	resp, err := http.Post(ts.URL+"/api/v1/sessions", "application/json", bytes.NewReader(b))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var created map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&created))
	resp.Body.Close()
	return ts.URL, created["session_id"], artifactStore
}

func postSSEMessage(t *testing.T, baseURL, sessionID, text string) string {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{
		"content": []map[string]string{{"type": "text", "text": text}},
	})
	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/v1/sessions/"+sessionID+"/messages", bytes.NewReader(payload))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	body := string(raw)
	require.Contains(t, body, "[DONE]")
	return body
}

func TestTurnDiff_Tier1_FromAppServerNotifyFixture(t *testing.T) {
	fixturePath := filepath.Join("..", "shared", "libs", "go", "codingagent", "codex", "testdata", "turn_diff_updated.jsonl")
	data, err := os.ReadFile(fixturePath)
	require.NoError(t, err)
	ev := codex.ParseAppServerNotification(strings.TrimSpace(string(data)))
	require.NotNil(t, ev)

	agent := &turnDiffFakeAgent{
		name:   "codex",
		events: []codingagent.StreamEvent{*ev},
	}
	baseURL, sessionID, st := startTurnDiffArtifactServer(t, agent, nil)
	postSSEMessage(t, baseURL, sessionID, "create hello")

	page, err := st.ListSystemArtifacts(context.Background(), store.SystemArtifactFilter{
		SessionIDs: []string{sessionID},
	})
	require.NoError(t, err)
	require.GreaterOrEqual(t, page.TotalCount, 1)
	found := false
	for _, item := range page.Items {
		if filepath.Base(item.Key) == "hello.txt" {
			found = true
			assert.Equal(t, "turn_diff", item.ToolName)
			assert.Equal(t, store.OperationCreate, item.Operation)
		}
	}
	require.True(t, found, "expected hello.txt from turn_diff")
}

func TestTurnDiff_StructuredToolOff(t *testing.T) {
	ev := codex.TurnDiffStreamEvent([]codex.DiffPathOp{{Path: "hello.txt", Kind: "add"}})
	require.NotNil(t, ev)
	agent := &turnDiffFakeAgent{name: "codex", events: []codingagent.StreamEvent{*ev}}
	baseURL, sessionID, st := startTurnDiffArtifactServer(t, agent, map[string]any{
		"structured_tool":   false,
		"shell_parser":      true,
		"workdir_reconcile": false,
	})
	postSSEMessage(t, baseURL, sessionID, "create hello")

	page, err := st.ListSystemArtifacts(context.Background(), store.SystemArtifactFilter{
		SessionIDs: []string{sessionID},
	})
	require.NoError(t, err)
	assert.Empty(t, page.Items)
}

func TestSSE_Tier1_AvailableImmediatelyAfterDone(t *testing.T) {
	ev := codingagent.StreamEvent{
		Type:     codingagent.EventToolUse,
		ToolName: "file_change",
		ToolInput: map[string]any{
			"path": "sse_immediate.txt",
			"kind": "add",
		},
	}
	agent := &turnDiffFakeAgent{name: "codex", events: []codingagent.StreamEvent{ev}}
	baseURL, sessionID, st := startTurnDiffArtifactServer(t, agent, nil)
	postSSEMessage(t, baseURL, sessionID, "write file")

	// Intentionally list immediately after DONE (no sleep): R8 regression.
	page, err := st.ListSystemArtifacts(context.Background(), store.SystemArtifactFilter{
		SessionIDs: []string{sessionID},
	})
	require.NoError(t, err)
	require.GreaterOrEqual(t, page.TotalCount, 1)
	found := false
	for _, item := range page.Items {
		if filepath.Base(item.Key) == "sse_immediate.txt" {
			found = true
			assert.Equal(t, "file_change", item.ToolName)
		}
	}
	require.True(t, found, "Tier1 event must be listable immediately after SSE DONE")
}
