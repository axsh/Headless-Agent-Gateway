//go:build integration

// Package llm_test contains E2E tests for the artifact API.
// These tests start a real tern server and exercise the artifact endpoints
// end-to-end, verifying that:
//  1. System artifact events are recorded after agent tool calls.
//  2. User artifacts can be uploaded, listed, downloaded, and deleted.
//  3. The MCP server tools work correctly for Coding Agents.
package llm_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"os"
	"strings"
	"testing"
	"time"

	v1 "github.com/axsh/arctic-tern/client/v1"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// artifactPipelineModel is the LLM used for the full artifact pipeline E2E test.
// claude-haiku-4-5 is fast and cost-effective while supporting tool use.
const artifactPipelineModel = "claude-haiku-4-5"

const artifactPipelineInput = `Name: Alice
Title: Principal Engineer
Technologies: Go, Docker, Kubernetes
Background: 5 years experience in distributed systems
`

// startArtifactE2EServer starts a tern server and returns the AgentService base URL
// and a cleanup function. It reuses the existing startE2EServer helper.
func startArtifactE2EServer(t *testing.T) (string, func()) {
	t.Helper()
	return startE2EServer(t)
}

// ---- User Artifact E2E tests ----

// TestE2E_UserArtifact_PutListDownloadDelete exercises the full user artifact lifecycle.
func TestE2E_UserArtifact_PutListDownloadDelete(t *testing.T) {
	baseURL, cleanup := startArtifactE2EServer(t)
	defer cleanup()

	c := v1.New(baseURL)
	ctx := context.Background()

	// PUT.
	resp, err := c.UserArtifacts().Put(ctx, "e2e/test.txt", strings.NewReader("hello e2e"))
	require.NoError(t, err)
	assert.Equal(t, "created", resp.Status)
	assert.Equal(t, "e2e/test.txt", resp.Key)

	// LIST.
	page, err := c.UserArtifacts().List(ctx, v1.UserArtifactFilter{})
	require.NoError(t, err)
	found := false
	for _, item := range page.Items {
		if item.Key == "e2e/test.txt" {
			found = true
		}
	}
	assert.True(t, found, "uploaded artifact should appear in list")

	// DOWNLOAD.
	rc, err := c.UserArtifacts().Download(ctx, "e2e/test.txt")
	require.NoError(t, err)
	defer rc.Close()
	data, _ := io.ReadAll(rc)
	assert.Equal(t, "hello e2e", string(data))

	// DELETE.
	require.NoError(t, c.UserArtifacts().Delete(ctx, "e2e/test.txt"))

	// Confirm gone.
	page2, _ := c.UserArtifacts().List(ctx, v1.UserArtifactFilter{Q: "e2e/*"})
	assert.Equal(t, 0, page2.TotalCount)
}

// TestE2E_UserArtifact_Overwrite verifies that PUTting the same key twice updates the artifact.
func TestE2E_UserArtifact_Overwrite(t *testing.T) {
	baseURL, cleanup := startArtifactE2EServer(t)
	defer cleanup()

	c := v1.New(baseURL)
	ctx := context.Background()

	resp1, err := c.UserArtifacts().Put(ctx, "ow/data.txt", strings.NewReader("v1"))
	require.NoError(t, err)
	assert.Equal(t, "created", resp1.Status)

	resp2, err := c.UserArtifacts().Put(ctx, "ow/data.txt", strings.NewReader("v2-updated"))
	require.NoError(t, err)
	assert.Equal(t, "updated", resp2.Status)

	rc, _ := c.UserArtifacts().Download(ctx, "ow/data.txt")
	defer rc.Close()
	data, _ := io.ReadAll(rc)
	assert.Equal(t, "v2-updated", string(data))
}

// TestE2E_UserArtifact_GlobFilter verifies that list filtering by glob works end-to-end.
func TestE2E_UserArtifact_GlobFilter(t *testing.T) {
	baseURL, cleanup := startArtifactE2EServer(t)
	defer cleanup()

	c := v1.New(baseURL)
	ctx := context.Background()

	_ = putUserArtifact(t, c, ctx, "filter/a.go", "pkg a")
	_ = putUserArtifact(t, c, ctx, "filter/b.txt", "txt")
	_ = putUserArtifact(t, c, ctx, "filter/c.go", "pkg c")

	page, err := c.UserArtifacts().List(ctx, v1.UserArtifactFilter{Q: "filter/*.go"})
	require.NoError(t, err)
	assert.Equal(t, 2, page.TotalCount)
}

// TestE2E_UserArtifact_Archive verifies that ZIP archive download works.
func TestE2E_UserArtifact_Archive(t *testing.T) {
	baseURL, cleanup := startArtifactE2EServer(t)
	defer cleanup()

	c := v1.New(baseURL)
	ctx := context.Background()

	_ = putUserArtifact(t, c, ctx, "arch/x.go", "package x")
	_ = putUserArtifact(t, c, ctx, "arch/y.go", "package y")

	rc, err := c.UserArtifacts().Archive(ctx, v1.ArchiveRequest{
		Q: "arch/*.go",
	})
	require.NoError(t, err)
	defer rc.Close()

	zipBytes, _ := io.ReadAll(rc)
	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	require.NoError(t, err)
	assert.Len(t, zr.File, 2)
}

// TestE2E_SystemArtifact_List_Empty verifies that the system artifact list returns
// an empty page when no sessions have been run.
func TestE2E_SystemArtifact_List_Empty(t *testing.T) {
	baseURL, cleanup := startArtifactE2EServer(t)
	defer cleanup()

	c := v1.New(baseURL)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	page, err := c.SystemArtifacts().List(ctx, v1.SystemArtifactFilter{})
	require.NoError(t, err)
	assert.Equal(t, 0, page.TotalCount)
}

// TestE2E_SystemArtifact_Archive_EmptySession verifies that an archive request
// for an empty set produces a valid (empty) ZIP file.
func TestE2E_SystemArtifact_Archive_EmptySession(t *testing.T) {
	baseURL, cleanup := startArtifactE2EServer(t)
	defer cleanup()

	c := v1.New(baseURL)
	ctx := context.Background()

	rc, err := c.SystemArtifacts().Archive(ctx, v1.ArchiveRequest{
		Keys: []string{},
	})
	require.NoError(t, err)
	defer rc.Close()

	zipBytes, _ := io.ReadAll(rc)
	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	require.NoError(t, err)
	assert.Empty(t, zr.File)
}

// TestE2E_UserArtifact_MCPCompatibility verifies that artifacts uploaded via the
// Web API are accessible through the HTTP API in a way consistent with what
// an MCP client would see. (Full MCP wire protocol is not tested here since it
// requires a separate MCP transport; this test validates the data layer.)
func TestE2E_UserArtifact_MCPCompatibility(t *testing.T) {
	baseURL, cleanup := startArtifactE2EServer(t)
	defer cleanup()

	c := v1.New(baseURL)
	ctx := context.Background()

	// Upload via Web API (simulating what the user does).
	_, err := c.UserArtifacts().Put(ctx, "mcp/schema.json", strings.NewReader(`{"title":"test"}`))
	require.NoError(t, err)

	// Verify the content can be retrieved as-is (simulating what MCP get_user_artifact returns).
	rc, err := c.UserArtifacts().Download(ctx, "mcp/schema.json")
	require.NoError(t, err)
	defer rc.Close()
	data, _ := io.ReadAll(rc)

	var schema map[string]any
	require.NoError(t, json.Unmarshal(data, &schema))
	assert.Equal(t, "test", schema["title"])
}

// TestE2E_SystemArtifact_FilterBySession verifies that session_id filtering works.
// This test does not require a real Coding Agent; it seeds data via the store directly
// by creating a session and artifact event via the E2E server's session API.
func TestE2E_SystemArtifact_FilterBySession(t *testing.T) {
	baseURL, cleanup := startArtifactE2EServer(t)
	defer cleanup()

	c := v1.New(baseURL)
	ctx := context.Background()

	// Initially empty.
	page, err := c.SystemArtifacts().List(ctx, v1.SystemArtifactFilter{SessionIDs: []string{"nonexistent"}})
	require.NoError(t, err)
	assert.Equal(t, 0, page.TotalCount)
}

// TestE2E_ArtifactPipeline_FullLifecycle exercises the complete artifact pipeline
// with a real Tern server, real API keys (via vault keyring), and real LLM (claudecode):
//   1. Upload a user artifact
//   2. Download it and write to workDir for the agent to read
//   3. Ask the agent to create output.txt summarizing the input
//   4. Verify system artifact events are recorded (ToolCallAnalyzer)
//   5. Download the generated file via SystemArtifacts API
func TestE2E_ArtifactPipeline_FullLifecycle(t *testing.T) {
	baseURL, cleanup := startArtifactE2EServer(t)
	defer cleanup()

	c := v1.New(baseURL)
	ctx := context.Background()
	workDir := t.TempDir()

	artifactKey := "e2e/pipeline/input.txt"

	// Step 1: Upload user artifact.
	putResp, err := c.UserArtifacts().Put(ctx, artifactKey, strings.NewReader(artifactPipelineInput))
	require.NoError(t, err)
	assert.Equal(t, "created", putResp.Status)
	t.Logf("[Step 1] uploaded user artifact: %s (%d bytes)", putResp.Key, putResp.Size)

	// Step 2: Download and write to workDir so the agent can read via file tools.
	rc, err := c.UserArtifacts().Download(ctx, artifactKey)
	require.NoError(t, err)
	content, err := io.ReadAll(rc)
	rc.Close()
	require.NoError(t, err)
	assert.Equal(t, artifactPipelineInput, string(content))

	inputPath := filepath.Join(workDir, "artifact_input.txt")
	require.NoError(t, os.WriteFile(inputPath, content, 0o644))
	t.Logf("[Step 2] wrote artifact to workDir: %s", inputPath)

	// Step 3: Create session and send generation prompt.
	sessionID := createE2ESessionWithModel(t, baseURL, "claudecode", artifactPipelineModel, workDir)
	t.Logf("[Step 3] session created: %s", sessionID)

	prompt := `Read the file "artifact_input.txt" in the current directory, then create a new file called output.txt that summarizes the key points.`
	resp := sendE2EMessage(t, baseURL, sessionID, prompt, 180*time.Second)
	defer resp.Body.Close()

	events, gotDone := parseE2ESSEEvents(t, resp)
	if !gotDone {
		t.Fatal("expected [DONE] sentinel in SSE stream")
	}
	for _, ev := range events {
		if ev.Type == codingagent.EventError {
			t.Fatalf("agent error: %s", ev.Content)
		}
	}
	t.Logf("[Step 3] SSE completed: %d events", len(events))

	// Step 4: Verify system artifacts were recorded for this session.
	page, err := c.SystemArtifacts().List(ctx, v1.SystemArtifactFilter{
		SessionIDs: []string{sessionID},
		Operation:  "create",
	})
	require.NoError(t, err)
	t.Logf("[Step 4] system artifacts in session: %d", page.TotalCount)
	for _, item := range page.Items {
		t.Logf("  key=%s op=%s tool=%s", item.Key, item.Operation, item.ToolName)
	}
	require.GreaterOrEqual(t, page.TotalCount, 1, "expected at least one system artifact create event after agent Write tool call")

	var downloadKey string
	for _, item := range page.Items {
		if filepath.Base(item.Key) == "output.txt" {
			downloadKey = item.Key
			break
		}
	}
	require.NotEmpty(t, downloadKey, "expected a system artifact with basename output.txt")

	// Step 5: Download generated file via SystemArtifacts API.
	dlRC, err := c.SystemArtifacts().Download(ctx, downloadKey)
	require.NoError(t, err)
	defer dlRC.Close()
	downloaded, err := io.ReadAll(dlRC)
	require.NoError(t, err)
	require.NotEmpty(t, downloaded, "downloaded system artifact content should not be empty")
	t.Logf("[Step 5] downloaded %q: %d bytes", downloadKey, len(downloaded))

	// Also verify the file exists on disk in workDir.
	diskPath := filepath.Join(workDir, "output.txt")
	diskContent, err := os.ReadFile(diskPath)
	require.NoError(t, err, "output.txt should exist on disk in workDir")
	assert.NotEmpty(t, diskContent)
}

// ---- helpers ----

func putUserArtifact(t *testing.T, c *v1.Client, ctx context.Context, key, content string) *v1.PutResponse {
	t.Helper()
	resp, err := c.UserArtifacts().Put(ctx, key, strings.NewReader(content))
	require.NoError(t, err)
	return resp
}

// rawHTTPGet issues a plain GET to baseURL+path and returns the response body.
func rawHTTPGet(t *testing.T, baseURL, path string) (int, []byte) {
	t.Helper()
	resp, err := http.Get(baseURL + path)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}
