package v1_test

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

	v1 "github.com/axsh/arctic-tern/client/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubArtifactServer builds a minimal HTTP server for artifact API testing.
func stubArtifactServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	// System artifact list.
	mux.HandleFunc("/api/v1/artifacts/system", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"source":      "system",
			"total_count": 2,
			"page":        1,
			"per_page":    30,
			"items": []map[string]any{
				{"key": "a.go", "operation": "create", "occurred_at": time.Now().Format(time.RFC3339)},
				{"key": "b.go", "operation": "update", "occurred_at": time.Now().Format(time.RFC3339)},
			},
		})
	})

	// System artifact by key / content / archive.
	mux.HandleFunc("/api/v1/artifacts/system/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/artifacts/system/")
		path = strings.TrimPrefix(path, "/")
		switch {
		case path == "archive" && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/zip")
			zw := zip.NewWriter(w)
			f, _ := zw.Create("a.go")
			f.Write([]byte("package main")) //nolint:errcheck
			zw.Close()
		case strings.HasSuffix(path, "/content"):
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte("package main\n")) //nolint:errcheck
		default:
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"source": "system", "key": path,
				"operations": []map[string]any{{"operation": "create"}},
			})
		}
	})

	// User artifact root.
	mux.HandleFunc("/api/v1/artifacts/user", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"source":      "user",
			"total_count": 1,
			"page":        1,
			"per_page":    30,
			"items": []map[string]any{
				{"key": "dataset.csv", "size": 100, "mime_type": "text/csv"},
			},
		})
	})

	// User artifact by key.
	mux.HandleFunc("/api/v1/artifacts/user/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/artifacts/user/")
		path = strings.TrimPrefix(path, "/")
		switch {
		case r.Method == http.MethodPut:
			body, _ := io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"source": "user", "key": path, "status": "created", "size": len(body),
			})
		case strings.HasSuffix(path, "/content") && r.Method == http.MethodGet:
			w.Write([]byte("col1,col2\n1,2\n")) //nolint:errcheck
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		case path == "archive" && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/zip")
			zw := zip.NewWriter(w)
			f, _ := zw.Create("dataset.csv")
			f.Write([]byte("col1,col2")) //nolint:errcheck
			zw.Close()
		default:
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"source": "user", "key": path, "size": 100,
			})
		}
	})

	return httptest.NewServer(mux)
}

// ---- SystemArtifactClient tests ----

func TestSystemClient_List(t *testing.T) {
	srv := stubArtifactServer(t)
	defer srv.Close()

	c := v1.New(srv.URL)
	page, err := c.SystemArtifacts().List(context.Background(), v1.SystemArtifactFilter{})
	require.NoError(t, err)
	assert.Equal(t, 2, page.TotalCount)
	assert.Len(t, page.Items, 2)
}

func TestSystemClient_List_WithFilter(t *testing.T) {
	srv := stubArtifactServer(t)
	defer srv.Close()

	c := v1.New(srv.URL)
	page, err := c.SystemArtifacts().List(context.Background(), v1.SystemArtifactFilter{
		Q:          "**/*.go",
		SessionIDs: []string{"sess-1"},
		PerPage:    10,
	})
	require.NoError(t, err)
	assert.NotNil(t, page)
}

func TestSystemClient_Download(t *testing.T) {
	srv := stubArtifactServer(t)
	defer srv.Close()

	c := v1.New(srv.URL)
	rc, err := c.SystemArtifacts().Download(context.Background(), "main.go")
	require.NoError(t, err)
	defer rc.Close()

	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.NotEmpty(t, data)
}

func TestSystemClient_Archive(t *testing.T) {
	srv := stubArtifactServer(t)
	defer srv.Close()

	c := v1.New(srv.URL)
	rc, err := c.SystemArtifacts().Archive(context.Background(), v1.ArchiveRequest{
		Keys: []string{"a.go"},
	})
	require.NoError(t, err)
	defer rc.Close()

	data, _ := io.ReadAll(rc)
	assert.NotEmpty(t, data)
}

// ---- UserArtifactClient tests ----

func TestUserClient_Put(t *testing.T) {
	srv := stubArtifactServer(t)
	defer srv.Close()

	c := v1.New(srv.URL)
	resp, err := c.UserArtifacts().Put(context.Background(), "notes.txt", strings.NewReader("hello"))
	require.NoError(t, err)
	assert.Equal(t, "notes.txt", resp.Key)
	assert.Equal(t, "created", resp.Status)
}

func TestUserClient_PutFile(t *testing.T) {
	srv := stubArtifactServer(t)
	defer srv.Close()

	fpath := filepath.Join(t.TempDir(), "data.csv")
	require.NoError(t, os.WriteFile(fpath, []byte("a,b,c"), 0o644))

	c := v1.New(srv.URL)
	resp, err := c.UserArtifacts().PutFile(context.Background(), "uploads/data.csv", fpath)
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestUserClient_Download(t *testing.T) {
	srv := stubArtifactServer(t)
	defer srv.Close()

	c := v1.New(srv.URL)
	rc, err := c.UserArtifacts().Download(context.Background(), "dataset.csv")
	require.NoError(t, err)
	defer rc.Close()

	data, _ := io.ReadAll(rc)
	assert.NotEmpty(t, data)
}

func TestUserClient_Delete(t *testing.T) {
	srv := stubArtifactServer(t)
	defer srv.Close()

	c := v1.New(srv.URL)
	err := c.UserArtifacts().Delete(context.Background(), "old.txt")
	require.NoError(t, err)
}

func TestUserClient_List(t *testing.T) {
	srv := stubArtifactServer(t)
	defer srv.Close()

	c := v1.New(srv.URL)
	page, err := c.UserArtifacts().List(context.Background(), v1.UserArtifactFilter{})
	require.NoError(t, err)
	assert.Equal(t, 1, page.TotalCount)
}

func TestUserClient_Archive(t *testing.T) {
	srv := stubArtifactServer(t)
	defer srv.Close()

	c := v1.New(srv.URL)
	rc, err := c.UserArtifacts().Archive(context.Background(), v1.ArchiveRequest{
		Keys: []string{"dataset.csv"},
	})
	require.NoError(t, err)
	defer rc.Close()

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rc)
	assert.NotEmpty(t, buf.Bytes())
}
