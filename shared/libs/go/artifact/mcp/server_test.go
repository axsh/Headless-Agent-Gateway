package mcp_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	artifactmcp "github.com/axsh/arctic-tern/shared/libs/go/artifact/mcp"
	"github.com/axsh/arctic-tern/shared/libs/go/artifact/storage"
	"github.com/axsh/arctic-tern/shared/libs/go/artifact/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupMCPTest(t *testing.T) (store.ArtifactStore, *storage.UserArtifactStorage, *artifactmcp.ArtifactMCPServer) {
	t.Helper()
	s, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })

	st, err := storage.New(t.TempDir())
	require.NoError(t, err)

	srv := artifactmcp.New(s, st)
	return s, st, srv
}

func seedUserArtifact(t *testing.T, s store.ArtifactStore, st *storage.UserArtifactStorage, key, content string) {
	t.Helper()
	info, err := st.Write(key, strings.NewReader(content))
	require.NoError(t, err)
	now := time.Now()
	require.NoError(t, s.SaveUserArtifact(context.Background(), store.UserArtifact{
		ID:         key,
		Key:        key,
		ActualPath: info.ActualPath,
		Filename:   filepath.Base(key),
		Size:       info.Size,
		MIMEType:   info.MIMEType,
		ContentSHA: info.SHA256,
		CreatedAt:  now,
		UpdatedAt:  now,
	}))
}

// TestMCP_ListUserArtifacts verifies the list tool returns entries.
func TestMCP_ListUserArtifacts(t *testing.T) {
	s, st, srv := setupMCPTest(t)
	seedUserArtifact(t, s, st, "dataset.csv", "a,b,c")
	seedUserArtifact(t, s, st, "config.yaml", "key: val")

	result, err := srv.CallTool(context.Background(), "list_user_artifacts", map[string]any{})
	require.NoError(t, err)
	require.NotNil(t, result)

	text := toolResultText(result)
	assert.Contains(t, text, "dataset.csv")
	assert.Contains(t, text, "config.yaml")
}

// TestMCP_ListUserArtifacts_Filtered tests glob filtering via the list tool.
func TestMCP_ListUserArtifacts_Filtered(t *testing.T) {
	s, st, srv := setupMCPTest(t)
	seedUserArtifact(t, s, st, "a.csv", "data")
	seedUserArtifact(t, s, st, "b.yaml", "cfg")

	result, err := srv.CallTool(context.Background(), "list_user_artifacts", map[string]any{
		"q": "*.csv",
	})
	require.NoError(t, err)
	text := toolResultText(result)
	assert.Contains(t, text, "a.csv")
	assert.NotContains(t, text, "b.yaml")
}

// TestMCP_GetUserArtifact verifies get_user_artifact returns content.
func TestMCP_GetUserArtifact(t *testing.T) {
	s, st, srv := setupMCPTest(t)
	seedUserArtifact(t, s, st, "hello.txt", "hello world")

	result, err := srv.CallTool(context.Background(), "get_user_artifact", map[string]any{
		"key": "hello.txt",
	})
	require.NoError(t, err)
	text := toolResultText(result)
	assert.Contains(t, text, "hello world")
}

// TestMCP_GetUserArtifact_NotFound verifies a helpful error is returned.
func TestMCP_GetUserArtifact_NotFound(t *testing.T) {
	_, _, srv := setupMCPTest(t)

	result, err := srv.CallTool(context.Background(), "get_user_artifact", map[string]any{
		"key": "missing.txt",
	})
	require.NoError(t, err)
	text := toolResultText(result)
	assert.Contains(t, strings.ToLower(text), "not found")
}

// TestMCP_PutUserArtifact verifies put_user_artifact stores content.
func TestMCP_PutUserArtifact(t *testing.T) {
	s, st, srv := setupMCPTest(t)

	result, err := srv.CallTool(context.Background(), "put_user_artifact", map[string]any{
		"key":     "new.txt",
		"content": "from mcp",
	})
	require.NoError(t, err)
	text := toolResultText(result)
	assert.Contains(t, strings.ToLower(text), "saved")

	// Verify persisted in store.
	art, err := s.GetUserArtifactByKey(context.Background(), "new.txt")
	require.NoError(t, err)
	require.NotNil(t, art)

	// Verify storage content.
	rc, err := st.Read(art.ID)
	require.NoError(t, err)
	defer rc.Close()
	buf := make([]byte, 100)
	n, _ := rc.Read(buf)
	assert.Equal(t, "from mcp", string(buf[:n]))
}

// TestMCP_PutUserArtifact_Disabled verifies that when put is disabled, an error is returned.
func TestMCP_PutUserArtifact_Disabled(t *testing.T) {
	s, st, _ := setupMCPTest(t)
	srv := artifactmcp.NewWithOptions(s, st, artifactmcp.Options{PutDisabled: true})

	result, err := srv.CallTool(context.Background(), "put_user_artifact", map[string]any{
		"key":     "new.txt",
		"content": "x",
	})
	require.NoError(t, err)
	text := toolResultText(result)
	assert.Contains(t, strings.ToLower(text), "disabled")
}

// TestMCP_UnknownTool verifies that calling an unknown tool returns an error.
func TestMCP_UnknownTool(t *testing.T) {
	_, _, srv := setupMCPTest(t)
	_, err := srv.CallTool(context.Background(), "nonexistent_tool", nil)
	assert.Error(t, err)
}

// toolResultText extracts the text from a CallTool result.
func toolResultText(result *artifactmcp.ToolResult) string {
	return result.Text
}
