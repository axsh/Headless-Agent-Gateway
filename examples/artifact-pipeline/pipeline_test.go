package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	client "github.com/axsh/arctic-tern/client/v1"
)

// stubServer creates a httptest.Server that simulates the tern artifact API
// without requiring a real server or LLM.
func stubServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		// PUT /api/v1/artifacts/user/{key}
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/api/v1/artifacts/user/"):
			key := strings.TrimPrefix(r.URL.Path, "/api/v1/artifacts/user/")
			body, _ := io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"source": "user",
				"key":    key,
				"status": "created",
				"sha":    "deadbeef",
				"size":   int64(len(body)),
			})

		// GET /api/v1/artifacts/user/{key}/content
		case r.Method == http.MethodGet &&
			strings.Contains(r.URL.Path, "/artifacts/user/") &&
			strings.HasSuffix(r.URL.Path, "/content"):
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, "Name: Alice\nRole: Engineer\n")

		// POST /api/v1/sessions
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/sessions":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"session_id": "sess-stub-001"})

		// POST /api/v1/sessions/{id}/messages — SSE stream
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "data: {\"type\":\"text\",\"content\":\"output.txt created.\"}\n\n")
			fmt.Fprintf(w, "data: [DONE]\n\n")
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}

		// POST /api/v1/sessions/{id}/terminate
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/terminate"):
			w.WriteHeader(http.StatusNoContent)

		// DELETE /api/v1/sessions/{id} — Terminate (legacy)
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/v1/sessions/"):
			w.WriteHeader(http.StatusNoContent)

		// GET /api/v1/artifacts/system — list
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/artifacts/system":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"total_count": 1,
				"page":        1,
				"per_page":    20,
				"items": []map[string]any{
					{
						"key":        "output.txt",
						"operation":  "create",
						"agent_id":   "wayfinder",
						"session_id": "sess-stub-001",
					},
				},
			})

		// GET /api/v1/artifacts/system/{key}/content
		case r.Method == http.MethodGet &&
			strings.Contains(r.URL.Path, "/artifacts/system/") &&
			strings.HasSuffix(r.URL.Path, "/content"):
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, "Key Points:\n- Name: Alice\n- Role: Engineer\n")

		default:
			t.Logf("stub: unhandled %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
}

func TestRunUpload(t *testing.T) {
	srv := stubServer(t)
	defer srv.Close()

	inputPath := filepath.Join("testdata", "input.txt")
	if _, err := os.Stat(inputPath); err != nil {
		t.Fatalf("fixture not found: %v", err)
	}

	c := client.New(srv.URL)
	if err := runUpload(context.Background(), c, inputPath, "inputs/profile.txt"); err != nil {
		t.Fatalf("runUpload: %v", err)
	}
}

func TestRunUpload_FileNotFound(t *testing.T) {
	srv := stubServer(t)
	defer srv.Close()

	c := client.New(srv.URL)
	err := runUpload(context.Background(), c, "nonexistent.txt", "inputs/profile.txt")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestRunGenerate(t *testing.T) {
	srv := stubServer(t)
	defer srv.Close()

	c := client.New(srv.URL, client.WithNoTimeout())
	sessionID, err := runGenerate(context.Background(), c, "inputs/profile.txt", "wayfinder", "", ".")
	if err != nil {
		t.Fatalf("runGenerate: %v", err)
	}
	if sessionID != "sess-stub-001" {
		t.Errorf("session ID: want %q, got %q", "sess-stub-001", sessionID)
	}
}

func TestRunDownload_WithExplicitKey(t *testing.T) {
	srv := stubServer(t)
	defer srv.Close()

	dir := t.TempDir()
	savePath := filepath.Join(dir, "result.txt")

	c := client.New(srv.URL)
	if err := runDownload(context.Background(), c, "sess-stub-001", "output.txt", savePath); err != nil {
		t.Fatalf("runDownload: %v", err)
	}

	data, err := os.ReadFile(savePath)
	if err != nil {
		t.Fatalf("output file missing: %v", err)
	}
	if !strings.Contains(string(data), "Alice") {
		t.Errorf("output content: want 'Alice', got %q", string(data))
	}
}

func TestRunDownload_AutoSelectFirstFile(t *testing.T) {
	srv := stubServer(t)
	defer srv.Close()

	dir := t.TempDir()
	savePath := filepath.Join(dir, "auto.txt")

	c := client.New(srv.URL)
	// outputKey = "" → should auto-select first item from list
	if err := runDownload(context.Background(), c, "sess-stub-001", "", savePath); err != nil {
		t.Fatalf("runDownload auto-select: %v", err)
	}

	if _, err := os.Stat(savePath); err != nil {
		t.Fatalf("saved file should exist: %v", err)
	}
}

func TestFullPipeline_WithFixture(t *testing.T) {
	srv := stubServer(t)
	defer srv.Close()

	inputPath := filepath.Join("testdata", "input.txt")
	if _, err := os.Stat(inputPath); err != nil {
		t.Fatalf("fixture not found: %v", err)
	}

	c := client.New(srv.URL, client.WithNoTimeout())
	ctx := context.Background()

	// Step 1: upload fixture
	if err := runUpload(ctx, c, inputPath, "inputs/profile.txt"); err != nil {
		t.Fatalf("[Step 1] upload: %v", err)
	}

	// Step 2: generate via agent (stub SSE)
	sessionID, err := runGenerate(ctx, c, "inputs/profile.txt", "wayfinder", "", ".")
	if err != nil {
		t.Fatalf("[Step 2] generate: %v", err)
	}

	// Step 3: download generated file
	dir := t.TempDir()
	savePath := filepath.Join(dir, "output.txt")
	if err := runDownload(ctx, c, sessionID, "output.txt", savePath); err != nil {
		t.Fatalf("[Step 3] download: %v", err)
	}

	data, err := os.ReadFile(savePath)
	if err != nil {
		t.Fatalf("output file missing after full pipeline: %v", err)
	}
	if len(data) == 0 {
		t.Error("output file should not be empty")
	}
}
