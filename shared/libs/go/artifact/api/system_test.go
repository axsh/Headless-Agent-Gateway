package api_test

import (
	"archive/zip"
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

	"github.com/axsh/arctic-tern/shared/libs/go/artifact/api"
	"github.com/axsh/arctic-tern/shared/libs/go/artifact/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newSystemTestStore returns a fresh in-memory-backed SQLite store for testing.
func newSystemTestStore(t *testing.T) store.ArtifactStore {
	t.Helper()
	s, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	return s
}

func seedSession(t *testing.T, s store.ArtifactStore, id, agentID string) {
	t.Helper()
	require.NoError(t, s.UpsertSession(context.Background(), store.Session{
		ID: id, AgentID: agentID, StartedAt: time.Now(),
	}))
}

func seedEvent(t *testing.T, s store.ArtifactStore, sess, key, op, actualPath string) {
	t.Helper()
	require.NoError(t, s.SaveSystemArtifactEvent(context.Background(), store.SystemArtifactEvent{
		SessionID: sess, AgentID: "cursor", Key: key,
		ActualPath: actualPath, Operation: op, OccurredAt: time.Now(), ToolName: "Write",
	}))
}

func newSystemHandler(s store.ArtifactStore) http.Handler {
	h := api.NewSystemArtifactHandler(s)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "/api/v1/artifacts/system")
	return mux
}

// ---- List tests ----

func TestSystemAPI_List_ReturnsItems(t *testing.T) {
	s := newSystemTestStore(t)
	seedSession(t, s, "s1", "cursor")
	seedEvent(t, s, "s1", "a.go", store.OperationCreate, "/proj/a.go")
	seedEvent(t, s, "s1", "b.go", store.OperationCreate, "/proj/b.go")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/system", nil)
	newSystemHandler(s).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, float64(2), resp["total_count"])
	assert.Len(t, resp["items"], 2)
}

func TestSystemAPI_List_GlobFilter(t *testing.T) {
	s := newSystemTestStore(t)
	seedSession(t, s, "s1", "cursor")
	seedEvent(t, s, "s1", "a.go", store.OperationCreate, "/proj/a.go")
	seedEvent(t, s, "s1", "b.txt", store.OperationCreate, "/proj/b.txt")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/system?q=**%2F*.go", nil)
	newSystemHandler(s).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, float64(1), resp["total_count"])
}

func TestSystemAPI_List_SessionFilter(t *testing.T) {
	s := newSystemTestStore(t)
	seedSession(t, s, "s1", "cursor")
	seedSession(t, s, "s2", "cursor")
	seedEvent(t, s, "s1", "a.go", store.OperationCreate, "/proj/a.go")
	seedEvent(t, s, "s2", "b.go", store.OperationCreate, "/proj/b.go")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/system?session_id=s1", nil)
	newSystemHandler(s).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, float64(1), resp["total_count"])
	items := resp["items"].([]any)
	assert.Equal(t, "a.go", items[0].(map[string]any)["key"])
}

func TestSystemAPI_List_Pagination(t *testing.T) {
	s := newSystemTestStore(t)
	seedSession(t, s, "s1", "cursor")
	for i := range 5 {
		key := string(rune('a'+i)) + ".go"
		seedEvent(t, s, "s1", key, store.OperationCreate, "/proj/"+key)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/system?page=2&per_page=2&sort=key&order=asc", nil)
	newSystemHandler(s).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, float64(5), resp["total_count"])
	assert.Len(t, resp["items"], 2)
}

func TestSystemAPI_MethodNotAllowed(t *testing.T) {
	s := newSystemTestStore(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/artifacts/system", nil)
	newSystemHandler(s).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

// ---- Single key metadata ----

func TestSystemAPI_GetByKey(t *testing.T) {
	s := newSystemTestStore(t)
	seedSession(t, s, "s1", "cursor")
	seedEvent(t, s, "s1", "handler.go", store.OperationCreate, "/proj/handler.go")
	seedEvent(t, s, "s1", "handler.go", store.OperationUpdate, "/proj/handler.go")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/system/handler.go", nil)
	newSystemHandler(s).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	ops := resp["operations"].([]any)
	assert.Len(t, ops, 2)
}

func TestSystemAPI_GetByKey_NotFound(t *testing.T) {
	s := newSystemTestStore(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/system/missing.go", nil)
	newSystemHandler(s).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// ---- Content download ----

func TestSystemAPI_Content_Download(t *testing.T) {
	s := newSystemTestStore(t)
	seedSession(t, s, "s1", "cursor")

	// Create a real temp file to download.
	dir := t.TempDir()
	fpath := filepath.Join(dir, "hello.go")
	require.NoError(t, os.WriteFile(fpath, []byte("package main"), 0o644))

	seedEvent(t, s, "s1", "hello.go", store.OperationCreate, fpath)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/system/hello.go/content", nil)
	newSystemHandler(s).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "package main", rec.Body.String())
}

func TestSystemAPI_Content_FileNotFound(t *testing.T) {
	s := newSystemTestStore(t)
	seedSession(t, s, "s1", "cursor")
	seedEvent(t, s, "s1", "ghost.go", store.OperationCreate, "/nonexistent/ghost.go")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/system/ghost.go/content", nil)
	newSystemHandler(s).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// ---- Archive ----

func TestSystemAPI_Archive_ByKeys(t *testing.T) {
	s := newSystemTestStore(t)
	seedSession(t, s, "s1", "cursor")

	dir := t.TempDir()
	f1 := filepath.Join(dir, "a.go")
	f2 := filepath.Join(dir, "b.go")
	require.NoError(t, os.WriteFile(f1, []byte("package a"), 0o644))
	require.NoError(t, os.WriteFile(f2, []byte("package b"), 0o644))

	seedEvent(t, s, "s1", "a.go", store.OperationCreate, f1)
	seedEvent(t, s, "s1", "b.go", store.OperationCreate, f2)

	body, _ := json.Marshal(map[string]any{"keys": []string{"a.go", "b.go"}})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/system/archive", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	newSystemHandler(s).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/zip", rec.Header().Get("Content-Type"))

	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	require.NoError(t, err)
	assert.Len(t, zr.File, 2)
}

func TestSystemAPI_Archive_ByGlob(t *testing.T) {
	s := newSystemTestStore(t)
	seedSession(t, s, "s1", "cursor")

	dir := t.TempDir()
	f1 := filepath.Join(dir, "a.go")
	fTxt := filepath.Join(dir, "note.txt")
	require.NoError(t, os.WriteFile(f1, []byte("package a"), 0o644))
	require.NoError(t, os.WriteFile(fTxt, []byte("note"), 0o644))

	seedEvent(t, s, "s1", "a.go", store.OperationCreate, f1)
	seedEvent(t, s, "s1", "note.txt", store.OperationCreate, fTxt)

	body, _ := json.Marshal(map[string]any{"q": "**/*.go"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/system/archive",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	newSystemHandler(s).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	require.NoError(t, err)
	require.Len(t, zr.File, 1)
	assert.Equal(t, "a.go", zr.File[0].Name)

	rc, _ := zr.File[0].Open()
	content, _ := io.ReadAll(rc)
	rc.Close()
	assert.Equal(t, "package a", string(content))
}

func TestSystemAPI_Archive_EmptyResult(t *testing.T) {
	s := newSystemTestStore(t)

	body, _ := json.Marshal(map[string]any{"keys": []string{}})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/system/archive",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	newSystemHandler(s).ServeHTTP(rec, req)

	// Empty archive is still a valid zip (with 0 files).
	assert.Equal(t, http.StatusOK, rec.Code)
	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	require.NoError(t, err)
	assert.Empty(t, zr.File)
}

// Helper to suppress unused import warning.
var _ = strings.NewReader
