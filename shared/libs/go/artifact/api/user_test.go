package api_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/artifact/api"
	"github.com/axsh/arctic-tern/shared/libs/go/artifact/storage"
	"github.com/axsh/arctic-tern/shared/libs/go/artifact/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newUserTestSetup(t *testing.T) (store.ArtifactStore, *storage.UserArtifactStorage) {
	t.Helper()
	s, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })

	st, err := storage.New(t.TempDir())
	require.NoError(t, err)

	return s, st
}

func newUserHandler(s store.ArtifactStore, st *storage.UserArtifactStorage) http.Handler {
	h := api.NewUserArtifactHandler(s, st)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "/api/v1/artifacts/user")
	return mux
}

// ---- PUT ----

func TestUserAPI_Put_NewKey(t *testing.T) {
	s, st := newUserTestSetup(t)
	body := strings.NewReader("hello artifact")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/artifacts/user/notes/hello.txt", body)
	req.Header.Set("Content-Type", "text/plain")
	newUserHandler(s, st).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "notes/hello.txt", resp["key"])
	assert.Equal(t, "created", resp["status"])
}

func TestUserAPI_Put_Overwrite(t *testing.T) {
	s, st := newUserTestSetup(t)
	h := newUserHandler(s, st)

	put := func(content string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/api/v1/artifacts/user/doc.md", strings.NewReader(content))
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	assert.Equal(t, http.StatusCreated, put("v1"))
	assert.Equal(t, http.StatusOK, put("v2"))
}

// ---- GET single ----

func TestUserAPI_Get_ExistingKey(t *testing.T) {
	s, st := newUserTestSetup(t)
	h := newUserHandler(s, st)

	// Upload first.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/artifacts/user/data.csv", strings.NewReader("a,b,c"))
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code)

	// Download.
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/user/data.csv/content", nil)
	h.ServeHTTP(rec2, req2)

	assert.Equal(t, http.StatusOK, rec2.Code)
	assert.Equal(t, "a,b,c", rec2.Body.String())
}

func TestUserAPI_Get_NotFound(t *testing.T) {
	s, st := newUserTestSetup(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/user/missing.txt/content", nil)
	newUserHandler(s, st).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// ---- DELETE ----

func TestUserAPI_Delete(t *testing.T) {
	s, st := newUserTestSetup(t)
	h := newUserHandler(s, st)

	// Upload.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/v1/artifacts/user/tmp.txt", strings.NewReader("temp")))
	require.Equal(t, http.StatusCreated, rec.Code)

	// Delete.
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, httptest.NewRequest(http.MethodDelete, "/api/v1/artifacts/user/tmp.txt", nil))
	assert.Equal(t, http.StatusNoContent, rec2.Code)

	// Confirm gone.
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/user/tmp.txt/content", nil))
	assert.Equal(t, http.StatusNotFound, rec3.Code)
}

func TestUserAPI_Delete_NotFound(t *testing.T) {
	s, st := newUserTestSetup(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/artifacts/user/ghost.txt", nil)
	newUserHandler(s, st).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// ---- List ----

func TestUserAPI_List(t *testing.T) {
	s, st := newUserTestSetup(t)
	h := newUserHandler(s, st)

	for _, k := range []string{"a.txt", "b.csv", "configs/x.yaml"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/v1/artifacts/user/"+k, strings.NewReader("x")))
		require.Equal(t, http.StatusCreated, rec.Code)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/user", nil)
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, float64(3), resp["total_count"])
}

func TestUserAPI_List_GlobFilter(t *testing.T) {
	s, st := newUserTestSetup(t)
	h := newUserHandler(s, st)

	for _, k := range []string{"a.go", "b.txt"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/v1/artifacts/user/"+k, strings.NewReader("x")))
		require.Equal(t, http.StatusCreated, rec.Code)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/user?q=*.go", nil)
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, float64(1), resp["total_count"])
}

// ---- Archive ----

func TestUserAPI_Archive_ByKeys(t *testing.T) {
	s, st := newUserTestSetup(t)
	h := newUserHandler(s, st)

	for _, k := range []string{"x.go", "y.go"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/v1/artifacts/user/"+k, strings.NewReader("pkg "+k)))
		require.Equal(t, http.StatusCreated, rec.Code)
	}

	body, _ := json.Marshal(map[string]any{"keys": []string{"x.go", "y.go"}})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/user/archive", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/zip", rec.Header().Get("Content-Type"))

	// Validate zip contents.
	zipBytes := rec.Body.Bytes()
	require.NotEmpty(t, zipBytes)

	content, _ := io.ReadAll(bytes.NewReader(zipBytes))
	assert.NotEmpty(t, content)
}

// Helper to suppress unused import warning.
var _ = strings.NewReader
