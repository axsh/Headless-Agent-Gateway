package llm_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/agentservice"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/tasklog"
)

// persistentMockAgent captures the prompt received in Send.
type persistentMockAgent struct {
	lastPrompt string
}

func (a *persistentMockAgent) Name() string { return "persistent-mock" }
func (a *persistentMockAgent) Close() error { return nil }
func (a *persistentMockAgent) CreateSession(
	_ context.Context, opts ...codingagent.SessionOption,
) (codingagent.Session, error) {
	cfg := codingagent.NewSessionConfig(opts...)
	a.lastPrompt = cfg.Prompt
	return &persistentMockSession{agent: a}, nil
}
func (a *persistentMockAgent) SupportsMultimodal() bool { return true }

type persistentMockSession struct {
	agent *persistentMockAgent
}

func (s *persistentMockSession) Send(_ context.Context, prompt string) (<-chan codingagent.StreamEvent, error) {
	s.agent.lastPrompt = prompt
	ch := make(chan codingagent.StreamEvent, 2)
	// Emit session created event to set AgentSessionID in the record.
	ch <- codingagent.StreamEvent{Type: codingagent.EventSystem, SessionID: "agent-session-123"}
	ch <- codingagent.StreamEvent{Type: codingagent.EventText, Content: "Response to: " + prompt}
	close(ch)
	return ch, nil
}
func (s *persistentMockSession) ID() string { return "mock-session" }
func (s *persistentMockSession) Close() error { return nil }

func TestMultimodal_Persistence_And_Restoration(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "tern-persistence-test-*")
	defer os.RemoveAll(tmpDir)

	tl := tasklog.New()
	srv := agentservice.New(
		agentservice.WithTaskLog(tl),
	)
	agent := &persistentMockAgent{}
	srv.RegisterAgent(agent)
	ts := httptest.NewServer(srv.HTTPHandler())
	defer ts.Close()

	// 1. Create session with SessionDir.
	createReq, _ := json.Marshal(map[string]string{
		"agent":       "persistent-mock",
		"work_dir":    tmpDir,
		"session_dir": tmpDir,
	})
	resp, _ := http.Post(ts.URL+"/api/v1/sessions", "application/json", bytes.NewReader(createReq))
	var createResp struct {
		SessionID string `json:"session_id"`
	}
	json.NewDecoder(resp.Body).Decode(&createResp)
	sessionID := createResp.SessionID
	resp.Body.Close()

	// 2. Send message with image.
	imageB64 := testBase64PNGData()
	msg1, _ := json.Marshal(map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": "First message with image"},
			{"type": "image", "source": map[string]any{
				"type":       "base64",
				"media_type": "image/png",
				"data":       imageB64,
			}},
		},
	})
	http.Post(ts.URL+"/api/v2/sessions/"+sessionID+"/messages", "application/json", bytes.NewReader(msg1))

	// Verify image file exists in session dir.
	multimodalDir := filepath.Join(tmpDir, "multimodal")
	files, _ := os.ReadDir(multimodalDir)
	if len(files) != 1 {
		t.Errorf("expected 1 image file in %s, got %d", multimodalDir, len(files))
	}

	// Verify history file contains content_parts.
	histFile := filepath.Join(tmpDir, "history", "0000001.json")
	histData, _ := os.ReadFile(histFile)
	if !strings.Contains(string(histData), "content_parts") || !strings.Contains(string(histData), "image") {
		t.Errorf("history file missing multimodal parts: %s", string(histData))
	}

	// 3. Send second message (text only) and verify restoration.
	msg2, _ := json.Marshal(map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": "Second message"},
		},
	})
	http.Post(ts.URL+"/api/v2/sessions/"+sessionID+"/messages", "application/json", bytes.NewReader(msg2))

	// The last prompt received by the agent should contain the "System Note" with the previous image.
	if !strings.Contains(agent.lastPrompt, "[System Note: Previous images in this session for context]") {
		t.Errorf("context restoration failed, prompt: %s", agent.lastPrompt)
	}
	if !strings.Contains(agent.lastPrompt, filepath.Base(files[0].Name())) {
		t.Errorf("prompt missing previous image filename: %s", agent.lastPrompt)
	}

	t.Logf("Verification SUCCESS: prompt restored with context: %s", agent.lastPrompt)
}
