package llm_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/agentservice"
	"github.com/axsh/arctic-tern/shared/libs/go/artifact/store"
	"github.com/axsh/arctic-tern/shared/libs/go/tasklog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileChangeCollectors_DefaultOmitsWorkdirReconcile(t *testing.T) {
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

	body := map[string]any{"agent": "codex", "work_dir": workDir}
	b, _ := json.Marshal(body)
	resp, err := http.Post(ts.URL+"/api/v1/sessions", "application/json", bytes.NewReader(b))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var created map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&created))
	resp.Body.Close()
	sessionID := created["session_id"]

	getResp, err := http.Get(ts.URL + "/api/v1/sessions/" + sessionID)
	require.NoError(t, err)
	var info map[string]any
	require.NoError(t, json.NewDecoder(getResp.Body).Decode(&info))
	getResp.Body.Close()
	fcc, ok := info["file_change_collectors"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, fcc["structured_tool"])
	assert.Equal(t, true, fcc["shell_parser"])
	assert.Equal(t, false, fcc["workdir_reconcile"])

	require.NoError(t, os.WriteFile(filepath.Join(workDir, "no_reconcile.txt"), []byte("x"), 0o644))

	termResp, err := http.Post(ts.URL+"/api/v1/sessions/"+sessionID+"/terminate", "application/json", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, termResp.StatusCode)
	termResp.Body.Close()

	page, err := artifactStore.ListSystemArtifacts(context.Background(), store.SystemArtifactFilter{
		SessionIDs: []string{sessionID},
	})
	require.NoError(t, err)
	for _, item := range page.Items {
		assert.NotEqual(t, "reconcile:git", item.ToolName)
		assert.NotEqual(t, "no_reconcile.txt", item.Key)
	}
}

func TestFileChangeCollectors_AllOff(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	workDir := t.TempDir()
	require.NoError(t, exec.Command("git", "-C", workDir, "init").Run())

	dbPath := filepath.Join(t.TempDir(), "artifacts.db")
	artifactStore, err := store.NewSQLiteStore(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { artifactStore.Close() })

	srv := agentservice.NewWithStore(agentservice.NewMemorySessionStore(),
		agentservice.WithTaskLog(tasklog.New()),
		agentservice.WithArtifactStore(artifactStore, workDir),
	)
	srv.RegisterAgent(&fakeAgent{name: "codex"})
	ts := httptest.NewServer(srv.HTTPHandler())
	t.Cleanup(ts.Close)

	body := map[string]any{
		"agent":    "codex",
		"work_dir": workDir,
		"file_change_collectors": map[string]any{
			"structured_tool":   false,
			"shell_parser":      false,
			"workdir_reconcile": false,
		},
	}
	b, _ := json.Marshal(body)
	resp, err := http.Post(ts.URL+"/api/v1/sessions", "application/json", bytes.NewReader(b))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var created map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&created))
	resp.Body.Close()

	require.NoError(t, os.WriteFile(filepath.Join(workDir, "x.txt"), []byte("x"), 0o644))
	termResp, err := http.Post(ts.URL+"/api/v1/sessions/"+created["session_id"]+"/terminate", "application/json", nil)
	require.NoError(t, err)
	termResp.Body.Close()

	page, err := artifactStore.ListSystemArtifacts(context.Background(), store.SystemArtifactFilter{
		SessionIDs: []string{created["session_id"]},
	})
	require.NoError(t, err)
	assert.Empty(t, page.Items)
}
