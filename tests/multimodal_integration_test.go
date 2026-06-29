// Package llm_test contains integration tests for the v1 multimodal API.
// These tests verify end-to-end behavior of POST /api/v1/sessions/:id/messages
// using mock agents with multimodal support checks.
package llm_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/agentservice"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/tasklog"
)

// multimodalMockAgent supports multimodal content.
type multimodalMockAgent struct {
	name string
}

func (a *multimodalMockAgent) Name() string { return a.name }
func (a *multimodalMockAgent) Close() error { return nil }
func (a *multimodalMockAgent) CreateSession(
	_ context.Context, _ ...codingagent.SessionOption,
) (codingagent.Session, error) {
	return &integrationMockSession{}, nil
}
func (a *multimodalMockAgent) SupportsMultimodal() bool { return true }

// noMultimodalMockAgent does NOT support multimodal content.
type noMultimodalMockAgent struct {
	name string
}

func (a *noMultimodalMockAgent) Name() string { return a.name }
func (a *noMultimodalMockAgent) Close() error { return nil }
func (a *noMultimodalMockAgent) CreateSession(
	_ context.Context, _ ...codingagent.SessionOption,
) (codingagent.Session, error) {
	return &integrationMockSession{}, nil
}
func (a *noMultimodalMockAgent) SupportsMultimodal() bool { return false }

// setupMultimodalTestServer creates a test server with both multimodal and
// non-multimodal mock agents.
func setupMultimodalTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	tl := tasklog.New()
	srv := agentservice.New(
		agentservice.WithTaskLog(tl),
	)
	srv.RegisterAgent(&multimodalMockAgent{name: "claudecode"})
	srv.RegisterAgent(&noMultimodalMockAgent{name: "wayfinder"})
	ts := httptest.NewServer(srv.HTTPHandler())
	t.Cleanup(ts.Close)
	return ts
}

// testBase64PNGData returns a small base64-encoded PNG-like data.
func testBase64PNGData() string {
	data := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	return base64.StdEncoding.EncodeToString(data)
}

// --- V2 API Integration Tests ---

func TestMultimodal_V2_TextOnly_JSON(t *testing.T) {
	ts := setupMultimodalTestServer(t)
	sessionID := createAgentServiceSession(t, ts.URL, "claudecode")

	reqBody, _ := json.Marshal(map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": "Hello from v2 integration test"},
		},
	})
	resp, err := http.Post(
		ts.URL+"/api/v1/sessions/"+sessionID+"/messages",
		"application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("POST v2 message: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200, body: %s", resp.StatusCode, string(body))
	}

	// Parse JSON array response.
	var events []codingagent.StreamEvent
	if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
		t.Fatalf("decode events: %v", err)
	}
	if len(events) < 2 {
		t.Fatalf("expected at least 2 events, got %d", len(events))
	}
}

func TestMultimodal_V2_TextOnly_SSE(t *testing.T) {
	ts := setupMultimodalTestServer(t)
	sessionID := createAgentServiceSession(t, ts.URL, "claudecode")

	reqBody, _ := json.Marshal(map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": "Hello SSE v2"},
		},
	})
	req, _ := http.NewRequest("POST",
		ts.URL+"/api/v1/sessions/"+sessionID+"/messages",
		bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST v2 message (SSE): %v", err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream",
			resp.Header.Get("Content-Type"))
	}

	var events []codingagent.StreamEvent
	var gotDone bool
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			gotDone = true
			break
		}
		var ev codingagent.StreamEvent
		if json.Unmarshal([]byte(data), &ev) == nil {
			events = append(events, ev)
		}
	}

	if !gotDone {
		t.Fatal("expected [DONE] sentinel in SSE stream")
	}
	if len(events) < 3 {
		t.Fatalf("expected at least 3 events, got %d", len(events))
	}
}

func TestMultimodal_V2_WithImage(t *testing.T) {
	ts := setupMultimodalTestServer(t)
	sessionID := createAgentServiceSession(t, ts.URL, "claudecode")

	reqBody, _ := json.Marshal(map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": "Describe this image:"},
			{"type": "image", "source": map[string]any{
				"type":       "base64",
				"media_type": "image/png",
				"data":       testBase64PNGData(),
			}},
			{"type": "text", "text": "What do you see?"},
		},
	})
	resp, err := http.Post(
		ts.URL+"/api/v1/sessions/"+sessionID+"/messages",
		"application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("POST v2 message with image: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200, body: %s", resp.StatusCode, string(body))
	}

	var events []codingagent.StreamEvent
	if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
		t.Fatalf("decode events: %v", err)
	}
	if len(events) < 2 {
		t.Fatalf("expected at least 2 events, got %d", len(events))
	}
}

func TestMultimodal_V2_WayfinderRejectsImage(t *testing.T) {
	ts := setupMultimodalTestServer(t)
	sessionID := createAgentServiceSession(t, ts.URL, "wayfinder")

	reqBody, _ := json.Marshal(map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": "Look at this:"},
			{"type": "image", "source": map[string]any{
				"type":       "base64",
				"media_type": "image/png",
				"data":       testBase64PNGData(),
			}},
		},
	})
	resp, err := http.Post(
		ts.URL+"/api/v1/sessions/"+sessionID+"/messages",
		"application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("POST v2 message to wayfinder: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotImplemented {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("status = %d, want 501, body: %s", resp.StatusCode, string(body))
	}
}

func TestMultimodal_V2_WayfinderAcceptsTextOnly(t *testing.T) {
	ts := setupMultimodalTestServer(t)
	sessionID := createAgentServiceSession(t, ts.URL, "wayfinder")

	reqBody, _ := json.Marshal(map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": "Hello from v2, text only"},
		},
	})
	resp, err := http.Post(
		ts.URL+"/api/v1/sessions/"+sessionID+"/messages",
		"application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("POST v2 text message to wayfinder: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("status = %d, want 200, body: %s", resp.StatusCode, string(body))
	}
}

func TestMultimodal_V2_EmptyContent(t *testing.T) {
	ts := setupMultimodalTestServer(t)
	sessionID := createAgentServiceSession(t, ts.URL, "claudecode")

	reqBody, _ := json.Marshal(map[string]any{
		"content": []map[string]any{},
	})
	resp, err := http.Post(
		ts.URL+"/api/v1/sessions/"+sessionID+"/messages",
		"application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("POST v2 empty content: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("status = %d, want 400, body: %s", resp.StatusCode, string(body))
	}
}

func TestMultimodal_V2_InvalidBase64(t *testing.T) {
	ts := setupMultimodalTestServer(t)
	sessionID := createAgentServiceSession(t, ts.URL, "claudecode")

	reqBody, _ := json.Marshal(map[string]any{
		"content": []map[string]any{
			{"type": "image", "source": map[string]any{
				"type":       "base64",
				"media_type": "image/png",
				"data":       "!!!not-valid-base64!!!",
			}},
		},
	})
	resp, err := http.Post(
		ts.URL+"/api/v1/sessions/"+sessionID+"/messages",
		"application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("POST v2 invalid base64: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("status = %d, want 400, body: %s", resp.StatusCode, string(body))
	}
}

func TestMultimodal_V2_SessionNotFound(t *testing.T) {
	ts := setupMultimodalTestServer(t)

	reqBody, _ := json.Marshal(map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": "Hello"},
		},
	})
	resp, err := http.Post(
		ts.URL+"/api/v1/sessions/nonexistent-session-id/messages",
		"application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("POST v2 session not found: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("status = %d, want 404, body: %s", resp.StatusCode, string(body))
	}
}

func TestMultimodal_V1_ContentBlock(t *testing.T) {
	ts := setupMultimodalTestServer(t)
	sessionID := createAgentServiceSession(t, ts.URL, "claudecode")

	// v1 now uses the content block format ({"content": [...]}).
	msgBody, _ := json.Marshal(map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": "Hello v1 content block"},
		},
	})
	resp, err := http.Post(
		ts.URL+"/api/v1/sessions/"+sessionID+"/messages",
		"application/json", bytes.NewReader(msgBody))
	if err != nil {
		t.Fatalf("POST v1 message: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("v1 status = %d, want 200, body: %s", resp.StatusCode, string(body))
	}
}

func TestMultimodal_V2_UnsupportedContentType(t *testing.T) {
	ts := setupMultimodalTestServer(t)
	sessionID := createAgentServiceSession(t, ts.URL, "claudecode")

	reqBody, _ := json.Marshal(map[string]any{
		"content": []map[string]any{
			{"type": "video"}, // unsupported type
		},
	})
	resp, err := http.Post(
		ts.URL+"/api/v1/sessions/"+sessionID+"/messages",
		"application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("POST v2 unsupported type: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("status = %d, want 400, body: %s", resp.StatusCode, string(body))
	}
}
