package llm_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCodex_1MB_Limit_Enforcement(t *testing.T) {
	baseURL, cleanup := startE2EServer(t)
	defer cleanup()

	workDir := t.TempDir()
	sessionID := createE2ESession(t, baseURL, "codex", workDir)

	// Create a large prompt (> 1MB)
	largeText := strings.Repeat("a", 1024*1024 + 100) // 1MB + 100 bytes
	
	type contentPart struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
	}
	reqBody, _ := json.Marshal(map[string]any{
		"content": []contentPart{{Type: "text", Text: largeText}},
	})

	resp, err := http.Post(
		baseURL+"/api/v1/sessions/"+sessionID+"/messages",
		"application/json",
		bytes.NewReader(reqBody),
	)
	if err != nil {
		t.Fatalf("failed to send large message: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("expected status 413 Request Entity Too Large, got %d", resp.StatusCode)
	}

	// Verify error message in response
	var errResp string
	buf := new(bytes.Buffer)
	buf.ReadFrom(resp.Body)
	errResp = buf.String()
	if !strings.Contains(errResp, "exceeds the limit") {
		t.Errorf("expected error message about size limit, got: %s", errResp)
	}
}

func TestCodex_Stateless_Behavior(t *testing.T) {
	baseURL, cleanup := startE2EServer(t)
	defer cleanup()

	workDir := t.TempDir()
	sessionID := createE2ESession(t, baseURL, "codex", workDir)

	// Send an image (base64)
	imageData := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==" // 1x1 transparent PNG
	
	type imageSource struct {
		Type      string `json:"type"`
		MediaType string `json:"media_type"`
		Data      string `json:"data"`
	}
	type contentPart struct {
		Type   string       `json:"type"`
		Text   string       `json:"text,omitempty"`
		Source *imageSource `json:"source,omitempty"`
	}
	reqBody, _ := json.Marshal(map[string]any{
		"content": []contentPart{
			{Type: "text", Text: "describe this image"},
			{Type: "image", Source: &imageSource{
				Type:      "base64",
				MediaType: "image/png",
				Data:      imageData,
			}},
		},
	})

	// Use a mock response or expectation because we just want to check the server state
	// but the Codex CLI might fail to run in this test environment if not fully configured.
	// We'll check the session directory after the request.
	
	resp, err := http.Post(
		baseURL+"/api/v1/sessions/"+sessionID+"/messages",
		"application/json",
		bytes.NewReader(reqBody),
	)
	if err != nil {
		t.Fatalf("failed to send multimodal message: %v", err)
	}
	defer resp.Body.Close()

	// Give it a moment to process (though it might fail due to no real Codex CLI)
	time.Sleep(500 * time.Millisecond)

	// Verify that sessionDir/multimodal does NOT exist
	sessionDir := filepath.Join(workDir, "sessions") // from createE2ESession
	multimodalDir := filepath.Join(sessionDir, "multimodal")
	if _, err := os.Stat(multimodalDir); err == nil {
		t.Errorf("expected multimodal directory %s to NOT exist in stateless mode", multimodalDir)
	}

	// Verify that history contains image metadata but no path
	histDir := filepath.Join(sessionDir, "history")
	files, _ := os.ReadDir(histDir)
	if len(files) == 0 {
		t.Fatal("expected history files to be created")
	}

	// Load the last history file and check content
	latest := files[len(files)-1].Name()
	content, _ := os.ReadFile(filepath.Join(histDir, latest))
	contentStr := string(content)
	if !strings.Contains(contentStr, `"type": "image"`) && !strings.Contains(contentStr, `"type":"image"`) {
		t.Errorf("history should contain image part, got: %s", contentStr)
	}
	if strings.Contains(contentStr, `"path":`) && !strings.Contains(contentStr, `"path": ""`) && !strings.Contains(contentStr, `"path":""`) {
		t.Errorf("history should NOT contain image path, got: %s", contentStr)
	}
}
