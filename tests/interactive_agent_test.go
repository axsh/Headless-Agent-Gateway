package llm_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	v1 "github.com/axsh/arctic-tern/client/v1"
	"github.com/axsh/arctic-tern/shared/libs/go/agentservice"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/config"
)

type interactiveMockAgent struct {
	name string
}

func (a *interactiveMockAgent) Name() string { return a.name }
func (a *interactiveMockAgent) Close() error { return nil }
func (a *interactiveMockAgent) CreateSession(
	_ context.Context, _ ...codingagent.SessionOption,
) (codingagent.Session, error) {
	return newInteractiveMockSession(), nil
}

type interactiveMockSession struct {
	mu         sync.Mutex
	stdin      []string
	inputReady chan struct{}
	ch         chan codingagent.StreamEvent
}

func newInteractiveMockSession() *interactiveMockSession {
	return &interactiveMockSession{inputReady: make(chan struct{}, 1)}
}

func (s *interactiveMockSession) ID() string  { return "interactive-mock-001" }
func (s *interactiveMockSession) Close() error { return nil }

func (s *interactiveMockSession) WriteStdin(text string) error {
	s.mu.Lock()
	s.stdin = append(s.stdin, text)
	s.mu.Unlock()
	select {
	case s.inputReady <- struct{}{}:
	default:
	}
	return nil
}

func (s *interactiveMockSession) Send(_ context.Context, _ string) (<-chan codingagent.StreamEvent, error) {
	s.ch = make(chan codingagent.StreamEvent, 8)
	go func() {
		defer close(s.ch)
		s.ch <- codingagent.StreamEvent{Type: codingagent.EventText, Content: "starting"}
		s.ch <- codingagent.StreamEvent{
			Type:    codingagent.EventUserInputRequired,
			Content: "Please confirm",
			Choices: []string{"yes", "no"},
		}
		select {
		case <-s.inputReady:
		case <-time.After(5 * time.Second):
			s.ch <- codingagent.StreamEvent{Type: codingagent.EventError, Content: "timeout waiting for stdin"}
			return
		}
		s.mu.Lock()
		reply := ""
		if len(s.stdin) > 0 {
			reply = s.stdin[len(s.stdin)-1]
		}
		s.mu.Unlock()
		s.ch <- codingagent.StreamEvent{Type: codingagent.EventText, Content: "received: " + reply}
		s.ch <- codingagent.StreamEvent{Type: codingagent.EventResult}
	}()
	return s.ch, nil
}

func setupInteractiveTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := agentservice.New()
	srv.RegisterAgent(&interactiveMockAgent{name: "mockagent"})
	srv.SetModelProfiles(&config.ModelProfilesConfig{
		CodingAgents: map[string]config.AgentConfig{
			"mockagent": {
				ExecutionMode:       codingagent.ExecutionModeInteractive,
				IdleTimeoutSeconds:  300,
				MaxExecutionSeconds: 3600,
			},
		},
	})
	ts := httptest.NewServer(srv.HTTPHandler())
	t.Cleanup(ts.Close)
	return ts
}

func createInteractiveSession(t *testing.T, baseURL string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"agent": "mockagent"})
	resp, err := http.Post(baseURL+"/api/v1/sessions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer resp.Body.Close()
	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	return result["session_id"]
}

func readSSEEvents(t *testing.T, body io.Reader) []map[string]any {
	t.Helper()
	var events []map[string]any
	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		var ev map[string]any
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			t.Fatalf("decode event: %v", err)
		}
		events = append(events, ev)
	}
	return events
}

func TestInteractive_UserInputRequired_MockAdapter(t *testing.T) {
	ts := setupInteractiveTestServer(t)
	sessionID := createInteractiveSession(t, ts.URL)

	msgBody, _ := json.Marshal(map[string]any{"content": []map[string]string{{"type": "text", "text": "go"}}})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/sessions/"+sessionID+"/messages", bytes.NewReader(msgBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send message: %v", err)
	}
	defer resp.Body.Close()
	events := readSSEEvents(t, resp.Body)
	if len(events) < 2 {
		t.Fatalf("events = %d, want >= 2", len(events))
	}
	if events[1]["type"] != "user_input_required" {
		t.Fatalf("second event type = %v", events[1]["type"])
	}

	statusResp, err := http.Get(ts.URL + "/api/v1/sessions/" + sessionID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	defer statusResp.Body.Close()
	var status map[string]any
	json.NewDecoder(statusResp.Body).Decode(&status)
	if status["status"] != codingagent.StatusSuspended {
		t.Fatalf("status = %v, want suspended", status["status"])
	}

	respondBody, _ := json.Marshal(map[string]string{"content": "yes"})
	respondReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/sessions/"+sessionID+"/respond", bytes.NewReader(respondBody))
	respondReq.Header.Set("Content-Type", "application/json")
	respondReq.Header.Set("Accept", "text/event-stream")
	respondResp, err := http.DefaultClient.Do(respondReq)
	if err != nil {
		t.Fatalf("respond: %v", err)
	}
	defer respondResp.Body.Close()
	if respondResp.StatusCode != http.StatusOK {
		t.Fatalf("respond status = %d", respondResp.StatusCode)
	}
	respondEvents := readSSEEvents(t, respondResp.Body)
	foundResult := false
	for _, ev := range respondEvents {
		if ev["type"] == "result" {
			foundResult = true
		}
	}
	if !foundResult {
		t.Fatal("expected result event after respond")
	}
}

func TestInteractive_ConcurrentMessageRejected(t *testing.T) {
	ts := setupInteractiveTestServer(t)
	sessionID := createInteractiveSession(t, ts.URL)

	msgBody, _ := json.Marshal(map[string]any{"content": []map[string]string{{"type": "text", "text": "go"}}})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/sessions/"+sessionID+"/messages", bytes.NewReader(msgBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	done := make(chan struct{})
	go func() {
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			readSSEEvents(t, resp.Body)
			resp.Body.Close()
		}
		close(done)
	}()

	time.Sleep(100 * time.Millisecond)

	secondBody, _ := json.Marshal(map[string]any{"content": []map[string]string{{"type": "text", "text": "again"}}})
	secondResp, err := http.Post(ts.URL+"/api/v1/sessions/"+sessionID+"/messages", "application/json", bytes.NewReader(secondBody))
	if err != nil {
		t.Fatalf("second message: %v", err)
	}
	defer secondResp.Body.Close()
	if secondResp.StatusCode != http.StatusConflict {
		t.Fatalf("second message status = %d, want 409", secondResp.StatusCode)
	}
	<-done
}

func TestInteractive_ClientRunWithHandlers(t *testing.T) {
	ts := setupInteractiveTestServer(t)
	client := v1.New(ts.URL, v1.WithNoTimeout())
	ctx := context.Background()

	sess, err := client.CreateSession(ctx, v1.SessionRequest{Agent: "mockagent"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	answered := ""
	err = sess.SendTextWithHandlers(ctx, "hello", v1.StreamHandlers{
		OnUserInputRequired: func(ev v1.UserInputRequiredEvent) (string, error) {
			if ev.Content != "Please confirm" {
				t.Fatalf("prompt = %q", ev.Content)
			}
			if len(ev.Choices) != 2 {
				t.Fatalf("choices = %v", ev.Choices)
			}
			answered = "yes"
			return answered, nil
		},
	})
	if err != nil {
		t.Fatalf("SendTextWithHandlers: %v", err)
	}
	if answered != "yes" {
		t.Fatalf("answered = %q", answered)
	}
}

func TestInteractive_NoHeuristicChoices(t *testing.T) {
	ev := codingagent.StreamEvent{
		Type:    codingagent.EventUserInputRequired,
		Content: "Is this a question?",
	}
	if len(ev.Choices) != 0 {
		t.Fatalf("choices should be empty without structured source, got %v", ev.Choices)
	}
}
