package llm_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/agentservice"
	"github.com/axsh/arctic-tern/shared/libs/go/artifact/store"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/tasklog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReconcile_SessionEndGitSupplement(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	workDir := t.TempDir()
	require.NoError(t, exec.Command("git", "-C", workDir, "init").Run())
	require.NoError(t, exec.Command("git", "-C", workDir, "config", "user.email", "t@t.com").Run())
	require.NoError(t, exec.Command("git", "-C", workDir, "config", "user.name", "t").Run())

	dbPath := filepath.Join(t.TempDir(), "artifacts.db")
	artifactStore, err := store.NewSQLiteStore(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { artifactStore.Close() })

	tl := tasklog.New()
	srv := agentservice.NewWithStore(agentservice.NewMemorySessionStore(),
		agentservice.WithTaskLog(tl),
		agentservice.WithArtifactStore(artifactStore, workDir),
	)
	srv.RegisterAgent(&fakeAgent{name: "codex"})

	ts := httptest.NewServer(srv.HTTPHandler())
	t.Cleanup(ts.Close)

	body := map[string]any{
		"agent":    "codex",
		"work_dir": workDir,
	}
	b, _ := json.Marshal(body)
	resp, err := http.Post(ts.URL+"/api/v1/sessions", "application/json", bytes.NewReader(b))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var created map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&created))
	resp.Body.Close()
	sessionID := created["session_id"]
	require.NotEmpty(t, sessionID)

	require.NoError(t, os.WriteFile(filepath.Join(workDir, "reconcile_only.txt"), []byte("x"), 0o644))

	termResp, err := http.Post(ts.URL+"/api/v1/sessions/"+sessionID+"/terminate", "application/json", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, termResp.StatusCode)
	termResp.Body.Close()

	page, err := artifactStore.ListSystemArtifacts(context.Background(), store.SystemArtifactFilter{
		SessionIDs: []string{sessionID},
	})
	require.NoError(t, err)

	found := false
	for _, item := range page.Items {
		if item.Key == "reconcile_only.txt" && item.ToolName == "reconcile:git" {
			found = true
		}
	}
	assert.True(t, found, "expected reconcile:git supplement for untracked file")
}

func TestReconcile_ManyExistingEvents_NoDuplicateSupplement(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	workDir := t.TempDir()
	require.NoError(t, exec.Command("git", "-C", workDir, "init").Run())
	require.NoError(t, exec.Command("git", "-C", workDir, "config", "user.email", "t@t.com").Run())
	require.NoError(t, exec.Command("git", "-C", workDir, "config", "user.name", "t").Run())
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "seed.txt"), []byte("s"), 0o644))
	require.NoError(t, exec.Command("git", "-C", workDir, "add", "seed.txt").Run())
	require.NoError(t, exec.Command("git", "-C", workDir, "commit", "-m", "seed").Run())

	dbPath := filepath.Join(t.TempDir(), "artifacts.db")
	artifactStore, err := store.NewSQLiteStore(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { artifactStore.Close() })

	tl := tasklog.New()
	srv := agentservice.NewWithStore(agentservice.NewMemorySessionStore(),
		agentservice.WithTaskLog(tl),
		agentservice.WithArtifactStore(artifactStore, workDir),
	)
	srv.RegisterAgent(&fakeAgent{name: "codex"})

	ts := httptest.NewServer(srv.HTTPHandler())
	t.Cleanup(ts.Close)

	body := map[string]any{"agent": "codex", "work_dir": workDir}
	b, _ := json.Marshal(body)
	resp, err := http.Post(ts.URL+"/api/v1/sessions", "application/json", bytes.NewReader(b))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var created map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&created))
	resp.Body.Close()
	sessionID := created["session_id"]

	for i := 0; i < 70; i++ {
		rel := filepath.ToSlash(filepath.Join("bulk", "f_"+padInt(i)+".txt"))
		full := filepath.Join(workDir, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte("v1"), 0o644))
	}
	require.NoError(t, exec.Command("git", "-C", workDir, "add", ".").Run())
	require.NoError(t, exec.Command("git", "-C", workDir, "commit", "-m", "add70").Run())

	ctx := context.Background()
	require.NoError(t, artifactStore.UpsertSession(ctx, store.Session{
		ID: sessionID, AgentID: sessionID, StartedAt: time.Now(),
	}))
	for i := 0; i < 70; i++ {
		rel := filepath.ToSlash(filepath.Join("bulk", "f_"+padInt(i)+".txt"))
		require.NoError(t, artifactStore.SaveSystemArtifactEvent(ctx, store.SystemArtifactEvent{
			SessionID: sessionID, AgentID: sessionID, Key: rel,
			ActualPath: filepath.Join(workDir, filepath.FromSlash(rel)),
			Operation:  store.OperationCreate, OccurredAt: time.Now(),
			ToolName: "Write",
		}))
		require.NoError(t, os.WriteFile(filepath.Join(workDir, filepath.FromSlash(rel)), []byte("v2"), 0o644))
	}

	termResp, err := http.Post(ts.URL+"/api/v1/sessions/"+sessionID+"/terminate", "application/json", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, termResp.StatusCode)
	termResp.Body.Close()

	all, err := artifactStore.ListAllSystemArtifacts(ctx, store.SystemArtifactFilter{
		SessionIDs: []string{sessionID}, IncludeDeleted: true,
	})
	require.NoError(t, err)
	dup := 0
	for _, item := range all {
		if item.ToolName == "reconcile:git" && strings.HasPrefix(item.Key, "bulk/") {
			dup++
		}
	}
	assert.Equal(t, 0, dup, "existing Write events must not be re-added as reconcile:git")
}

func padInt(i int) string {
	return fmt.Sprintf("%03d", i)
}

type fakeAgent struct {
	name string
}

func (f *fakeAgent) Name() string { return f.name }
func (f *fakeAgent) CreateSession(context.Context, ...codingagent.SessionOption) (codingagent.Session, error) {
	return nil, nil
}
func (f *fakeAgent) Close() error { return nil }

var _ codingagent.CodingAgent = (*fakeAgent)(nil)
