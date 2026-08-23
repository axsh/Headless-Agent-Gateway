package llm_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/agentservice"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/tasklog"
)

type recoverFollowAgent struct {
	name  string
	delay time.Duration
}

func (a *recoverFollowAgent) Name() string { return a.name }
func (a *recoverFollowAgent) Close() error { return nil }
func (a *recoverFollowAgent) CreateSession(_ context.Context, _ ...codingagent.SessionOption) (codingagent.Session, error) {
	return &recoverFollowSession{delay: a.delay}, nil
}

type recoverFollowSession struct {
	delay time.Duration
}

func (s *recoverFollowSession) ID() string { return "recover-follow-native" }
func (s *recoverFollowSession) Close() error { return nil }
func (s *recoverFollowSession) Send(_ context.Context, _ string) (<-chan codingagent.StreamEvent, error) {
	ch := make(chan codingagent.StreamEvent, 4)
	go func() {
		defer close(ch)
		ch <- codingagent.StreamEvent{
			Type:     codingagent.EventToolUse,
			ToolName: "command_execution",
			ToolInput: map[string]any{"command": "rm -f /tmp/x"},
		}
		if s.delay > 0 {
			time.Sleep(s.delay)
		}
		ch <- codingagent.StreamEvent{
			Type:    codingagent.EventToolResult,
			Content: "Rejected(\"rm -f style commands are not permitted\")",
		}
	}()
	return ch, nil
}

func newRecoverHTTP(t *testing.T, agent *recoverFollowAgent) *httptest.Server {
	t.Helper()
	tl := tasklog.New()
	srv := agentservice.New(
		agentservice.WithTaskLog(tl),
		agentservice.WithSSEDrainTimeout(5*time.Second),
	)
	srv.RegisterAgent(agent)
	return httptest.NewServer(srv.HTTPHandler())
}

func TestSessionRecover_FollowReceivesToolResultAndTerminal(t *testing.T) {
	agent := &recoverFollowAgent{name: "codex", delay: 400 * time.Millisecond}
	ts := newRecoverHTTP(t, agent)
	defer ts.Close()
	id := createFollowSession(t, ts)

	ctx, cancel := context.WithCancel(context.Background())
	resp := postFollowMessage(t, ts, id, ctx)
	lastID, _ := readSSEUntil(t, resp.Body, func(data string) bool {
		return strings.Contains(data, "command_execution") || strings.Contains(data, "tool_use")
	})
	cancel()
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if lastID == "" {
		t.Fatal("expected logical id after tool_use")
	}
	// Let the agent finish and buffer tool_result before follow attaches.
	// Otherwise a stale POST relay reader can consume notify and block relay.stream.
	time.Sleep(agent.delay + 200*time.Millisecond)

	follow := getFollowEvents(t, ts, id, lastID)
	defer follow.Body.Close()
	if follow.StatusCode != http.StatusOK {
		fb, _ := io.ReadAll(follow.Body)
		t.Fatalf("follow %d %s", follow.StatusCode, fb)
	}
	raw, _ := io.ReadAll(follow.Body)
	body := string(raw)
	if !strings.Contains(body, "tool_result") {
		t.Fatalf("missing tool_result: %s", body)
	}
	if !strings.Contains(body, "Rejected") && !strings.Contains(body, "rm -f") {
		t.Fatalf("missing rejection text: %s", body)
	}
	if !strings.Contains(body, "[DONE]") {
		t.Fatalf("missing DONE: %s", body)
	}
	if !strings.Contains(body, `"type":"error"`) {
		t.Fatalf("missing terminal error: %s", body)
	}
}
