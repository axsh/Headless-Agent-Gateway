package agentservice_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/agentservice"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockMultimodalAgent supports multimodal content.
type mockMultimodalAgent struct {
	name string
}

func (m *mockMultimodalAgent) CreateSession(_ context.Context, _ ...codingagent.SessionOption) (codingagent.Session, error) {
	return &mockCodingSession{}, nil
}
func (m *mockMultimodalAgent) Name() string              { return m.name }
func (m *mockMultimodalAgent) Close() error              { return nil }
func (m *mockMultimodalAgent) SupportsMultimodal() bool  { return true }
func (m *mockMultimodalAgent) SetGatewayToken(_ string)  {}

// mockNoMultimodalAgent does NOT support multimodal content.
type mockNoMultimodalAgent struct {
	name string
}

func (m *mockNoMultimodalAgent) CreateSession(_ context.Context, _ ...codingagent.SessionOption) (codingagent.Session, error) {
	return &mockCodingSession{}, nil
}
func (m *mockNoMultimodalAgent) Name() string              { return m.name }
func (m *mockNoMultimodalAgent) Close() error              { return nil }
func (m *mockNoMultimodalAgent) SupportsMultimodal() bool  { return false }
func (m *mockNoMultimodalAgent) SetGatewayToken(_ string)  {}

func newTestServerV2() http.Handler {
	srv := agentservice.New()
	srv.RegisterAgent(&mockMultimodalAgent{name: "claudecode"})
	srv.RegisterAgent(&mockNoMultimodalAgent{name: "wayfinder"})
	return srv.HTTPHandler()
}

func createSessionForV2(t *testing.T, handler http.Handler, agentName, sessionDir string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"agent":       agentName,
		"session_dir": sessionDir,
	})
	req := httptest.NewRequest("POST", "/api/v1/sessions", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	return resp["session_id"]
}

// testBase64PNG returns a minimal base64-encoded PNG-like data.
func testBase64PNG() string {
	data := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	return base64.StdEncoding.EncodeToString(data)
}

func TestHandleV2SendMessage_TextOnly(t *testing.T) {
	handler := newTestServerV2()
	sessionID := createSessionForV2(t, handler, "claudecode", t.TempDir())

	reqBody, _ := json.Marshal(map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": "Hello, agent!"},
		},
	})
	req := httptest.NewRequest("POST", "/api/v1/sessions/"+sessionID+"/messages", bytes.NewReader(reqBody))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	// The response should contain event data (JSON array from respondJSON).
	assert.NotEmpty(t, w.Body.String())
}

func TestHandleV2SendMessage_WithImage(t *testing.T) {
	handler := newTestServerV2()
	sessionID := createSessionForV2(t, handler, "claudecode", t.TempDir())

	reqBody, _ := json.Marshal(map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": "What is in this image?"},
			{"type": "image", "source": map[string]any{
				"type":       "base64",
				"media_type": "image/png",
				"data":       testBase64PNG(),
			}},
		},
	})
	req := httptest.NewRequest("POST", "/api/v1/sessions/"+sessionID+"/messages", bytes.NewReader(reqBody))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NotEmpty(t, w.Body.String())
}

func TestHandleV2SendMessage_EmptyContent(t *testing.T) {
	handler := newTestServerV2()
	sessionID := createSessionForV2(t, handler, "claudecode", t.TempDir())

	reqBody, _ := json.Marshal(map[string]any{
		"content": []map[string]any{},
	})
	req := httptest.NewRequest("POST", "/api/v1/sessions/"+sessionID+"/messages", bytes.NewReader(reqBody))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "content must not be empty")
}

func TestHandleV2SendMessage_InvalidBase64(t *testing.T) {
	handler := newTestServerV2()
	sessionID := createSessionForV2(t, handler, "claudecode", t.TempDir())

	reqBody, _ := json.Marshal(map[string]any{
		"content": []map[string]any{
			{"type": "image", "source": map[string]any{
				"type":       "base64",
				"media_type": "image/png",
				"data":       "!!!invalid-base64!!!",
			}},
		},
	})
	req := httptest.NewRequest("POST", "/api/v1/sessions/"+sessionID+"/messages", bytes.NewReader(reqBody))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid base64")
}

func TestHandleV2SendMessage_SessionNotFound(t *testing.T) {
	handler := newTestServerV2()

	reqBody, _ := json.Marshal(map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": "Hello"},
		},
	})
	req := httptest.NewRequest("POST", "/api/v1/sessions/nonexistent/messages", bytes.NewReader(reqBody))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleV2SendMessage_WayfinderRejects(t *testing.T) {
	handler := newTestServerV2()
	sessionID := createSessionForV2(t, handler, "wayfinder", t.TempDir())

	reqBody, _ := json.Marshal(map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": "What is in this image?"},
			{"type": "image", "source": map[string]any{
				"type":       "base64",
				"media_type": "image/png",
				"data":       testBase64PNG(),
			}},
		},
	})
	req := httptest.NewRequest("POST", "/api/v1/sessions/"+sessionID+"/messages", bytes.NewReader(reqBody))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotImplemented, w.Code)
	assert.Contains(t, w.Body.String(), "multimodal")
}

func TestHandleV2SendMessage_WayfinderTextOnly(t *testing.T) {
	handler := newTestServerV2()
	sessionID := createSessionForV2(t, handler, "wayfinder", t.TempDir())

	// Text-only should work even for non-multimodal agents.
	reqBody, _ := json.Marshal(map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": "Hello from v2"},
		},
	})
	req := httptest.NewRequest("POST", "/api/v1/sessions/"+sessionID+"/messages", bytes.NewReader(reqBody))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestV2Route_NonMessages(t *testing.T) {
	handler := newTestServerV2()

	req := httptest.NewRequest("GET", "/api/v1/sessions/some-id", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}
