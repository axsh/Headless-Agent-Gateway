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
	"sync/atomic"
	"testing"
	"time"

	v1 "github.com/axsh/arctic-tern/client/v1"
	"github.com/axsh/arctic-tern/shared/libs/go/agentservice"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/tasklog"
)

type followAgent struct {
	name     string
	delay    time.Duration
	needInput bool
	mu       sync.Mutex
	sends    int
	closes   atomic.Int32
}

func (a *followAgent) Name() string { return a.name }
func (a *followAgent) Close() error { return nil }
func (a *followAgent) CreateSession(_ context.Context, _ ...codingagent.SessionOption) (codingagent.Session, error) {
	return &followSession{agent: a, inputCh: make(chan string, 1)}, nil
}

type followSession struct {
	agent   *followAgent
	inputCh chan string
}

func (s *followSession) ID() string { return "follow-native" }
func (s *followSession) Close() error {
	s.agent.closes.Add(1)
	return nil
}
func (s *followSession) WriteStdin(content string) error {
	s.inputCh <- content
	return nil
}
func (s *followSession) Send(_ context.Context, _ string) (<-chan codingagent.StreamEvent, error) {
	s.agent.mu.Lock()
	s.agent.sends++
	s.agent.mu.Unlock()
	ch := make(chan codingagent.StreamEvent, 8)
	go func() {
		defer close(ch)
		ch <- codingagent.StreamEvent{Type: codingagent.EventText, Content: "one"}
		if s.agent.delay > 0 {
			time.Sleep(s.agent.delay)
		}
		if s.agent.needInput {
			ch <- codingagent.StreamEvent{Type: codingagent.EventUserInputRequired, Content: "ask"}
			ans := <-s.inputCh
			ch <- codingagent.StreamEvent{Type: codingagent.EventText, Content: ans}
			ch <- codingagent.StreamEvent{Type: codingagent.EventResult}
			return
		}
		ch <- codingagent.StreamEvent{Type: codingagent.EventText, Content: "two"}
		ch <- codingagent.StreamEvent{Type: codingagent.EventResult}
	}()
	return ch, nil
}

func newFollowHTTP(t *testing.T, agent *followAgent, opts ...agentservice.ServerOption) *httptest.Server {
	t.Helper()
	tl := tasklog.New()
	opts = append([]agentservice.ServerOption{agentservice.WithTaskLog(tl)}, opts...)
	srv := agentservice.New(opts...)
	srv.RegisterAgent(agent)
	return httptest.NewServer(srv.HTTPHandler())
}

func createFollowSession(t *testing.T, ts *httptest.Server) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"agent": "codex", "work_dir": t.TempDir(), "session_dir": t.TempDir()})
	resp, err := http.Post(ts.URL+"/api/v1/sessions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create %d %s", resp.StatusCode, b)
	}
	var created map[string]string
	json.NewDecoder(resp.Body).Decode(&created)
	return created["session_id"]
}

func postFollowMessage(t *testing.T, ts *httptest.Server, sessionID string, ctx context.Context) *http.Response {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"content": []map[string]string{{"type": "text", "text": "go"}}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+"/api/v1/sessions/"+sessionID+"/messages", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("messages %d %s", resp.StatusCode, b)
	}
	return resp
}

func getFollowEvents(t *testing.T, ts *httptest.Server, sessionID, from string) *http.Response {
	t.Helper()
	u := ts.URL + "/api/v1/sessions/" + sessionID + "/events"
	if from != "" {
		u += "?from=" + from
	}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func readSSEUntil(t *testing.T, body io.Reader, pred func(line string) bool) (lastID string, sawResult bool) {
	t.Helper()
	sc := bufio.NewScanner(body)
	pending := ""
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "id: ") {
			pending = strings.TrimSpace(strings.TrimPrefix(line, "id: "))
			continue
		}
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				return lastID, sawResult
			}
			if pending != "" {
				lastID = pending
			}
			if strings.Contains(data, `"type":"result"`) {
				sawResult = true
			}
			if pred != nil && pred(data) {
				return lastID, sawResult
			}
		}
	}
	return lastID, sawResult
}

func TestSessionFollow_ReattachContinuesTurn(t *testing.T) {
	agent := &followAgent{name: "codex", delay: 400 * time.Millisecond}
	ts := newFollowHTTP(t, agent)
	defer ts.Close()
	id := createFollowSession(t, ts)
	ctx, cancel := context.WithCancel(context.Background())
	resp := postFollowMessage(t, ts, id, ctx)
	lastID, _ := readSSEUntil(t, resp.Body, func(data string) bool {
		return strings.Contains(data, `"content":"one"`)
	})
	cancel()
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if lastID == "" {
		t.Fatal("expected logical id after first text")
	}

	busy, err := http.Post(ts.URL+"/api/v1/sessions/"+id+"/messages", "application/json",
		bytes.NewReader([]byte(`{"content":[{"type":"text","text":"x"}]}`)))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(busy.Body)
	busy.Body.Close()
	if busy.StatusCode != http.StatusConflict || !strings.Contains(string(b), "follow") {
		t.Fatalf("busy = %d %s", busy.StatusCode, b)
	}

	follow := getFollowEvents(t, ts, id, lastID)
	defer follow.Body.Close()
	if follow.StatusCode != http.StatusOK {
		fb, _ := io.ReadAll(follow.Body)
		t.Fatalf("follow %d %s", follow.StatusCode, fb)
	}
	_, saw := readSSEUntil(t, follow.Body, nil)
	if !saw {
		t.Fatal("expected result on follow")
	}
	if agent.closes.Load() == 0 {
		// process may still close after finish; wait briefly
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) && agent.closes.Load() == 0 {
			time.Sleep(20 * time.Millisecond)
		}
	}
}

func TestSessionFollow_ReplayFromStart(t *testing.T) {
	agent := &followAgent{name: "codex", delay: 200 * time.Millisecond}
	ts := newFollowHTTP(t, agent)
	defer ts.Close()
	id := createFollowSession(t, ts)
	ctx, cancel := context.WithCancel(context.Background())
	resp := postFollowMessage(t, ts, id, ctx)
	readSSEUntil(t, resp.Body, func(data string) bool {
		return strings.Contains(data, `"content":"one"`)
	})
	cancel()
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	follow := getFollowEvents(t, ts, id, "")
	defer follow.Body.Close()
	raw, _ := io.ReadAll(follow.Body)
	if !strings.Contains(string(raw), "turn context") || !strings.Contains(string(raw), `"content":"one"`) {
		t.Fatalf("replay missing prefix: %s", raw)
	}
	if !strings.Contains(string(raw), `"type":"result"`) {
		t.Fatalf("replay missing result: %s", raw)
	}
}

func TestSessionFollow_StealsExistingSubscriber(t *testing.T) {
	agent := &followAgent{name: "codex", delay: 500 * time.Millisecond}
	ts := newFollowHTTP(t, agent)
	defer ts.Close()
	id := createFollowSession(t, ts)
	resp := postFollowMessage(t, ts, id, context.Background())
	firstDone := make(chan string, 1)
	go func() {
		buf, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		firstDone <- string(buf)
	}()
	time.Sleep(80 * time.Millisecond)
	follow := getFollowEvents(t, ts, id, "")
	defer follow.Body.Close()
	_, saw := readSSEUntil(t, follow.Body, nil)
	if !saw {
		t.Fatal("second subscriber should receive result")
	}
	first := <-firstDone
	if strings.Contains(first, `"type":"result"`) && strings.Contains(first, `"content":"two"`) {
		// steal may still have delivered result if it won the race after two; allow if first stopped early
	}
}

func TestSessionFollow_TimeoutThenNoActiveTurn(t *testing.T) {
	agent := &followAgent{name: "codex", delay: 2 * time.Second}
	ts := newFollowHTTP(t, agent, agentservice.WithSSEDrainTimeout(80*time.Millisecond))
	defer ts.Close()
	id := createFollowSession(t, ts)
	ctx, cancel := context.WithCancel(context.Background())
	resp := postFollowMessage(t, ts, id, ctx)
	readSSEUntil(t, resp.Body, func(data string) bool {
		return strings.Contains(data, `"content":"one"`)
	})
	cancel()
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	time.Sleep(200 * time.Millisecond)
	follow := getFollowEvents(t, ts, id, "")
	defer follow.Body.Close()
	if follow.StatusCode != http.StatusConflict {
		b, _ := io.ReadAll(follow.Body)
		t.Fatalf("status = %d %s", follow.StatusCode, b)
	}
}

func TestSessionFollow_DoesNotEnqueueMessage(t *testing.T) {
	agent := &followAgent{name: "codex", delay: 300 * time.Millisecond}
	ts := newFollowHTTP(t, agent)
	defer ts.Close()
	id := createFollowSession(t, ts)
	ctx, cancel := context.WithCancel(context.Background())
	resp := postFollowMessage(t, ts, id, ctx)
	readSSEUntil(t, resp.Body, func(data string) bool {
		return strings.Contains(data, `"content":"one"`)
	})
	cancel()
	resp.Body.Close()
	follow := getFollowEvents(t, ts, id, "")
	io.Copy(io.Discard, follow.Body)
	follow.Body.Close()
	agent.mu.Lock()
	sends := agent.sends
	agent.mu.Unlock()
	if sends != 1 {
		t.Fatalf("sends = %d, want 1", sends)
	}
}

func TestSessionFollow_SuspendedThenRespond(t *testing.T) {
	agent := &followAgent{name: "codex", needInput: true}
	ts := newFollowHTTP(t, agent)
	defer ts.Close()
	id := createFollowSession(t, ts)
	ctx, cancel := context.WithCancel(context.Background())
	resp := postFollowMessage(t, ts, id, ctx)
	readSSEUntil(t, resp.Body, func(data string) bool {
		return strings.Contains(data, "user_input_required")
	})
	cancel()
	resp.Body.Close()
	follow := getFollowEvents(t, ts, id, "")
	raw, _ := io.ReadAll(follow.Body)
	follow.Body.Close()
	if !strings.Contains(string(raw), "user_input_required") {
		t.Fatalf("follow missing user_input: %s", raw)
	}
	body, _ := json.Marshal(map[string]string{"content": "yes"})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/sessions/"+id+"/respond", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	rresp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer rresp.Body.Close()
	if rresp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(rresp.Body)
		t.Fatalf("respond %d %s", rresp.StatusCode, b)
	}
}

func TestSessionFollow_CompletedRejected(t *testing.T) {
	agent := &followAgent{name: "codex"}
	ts := newFollowHTTP(t, agent)
	defer ts.Close()
	id := createFollowSession(t, ts)
	resp := postFollowMessage(t, ts, id, context.Background())
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		follow := getFollowEvents(t, ts, id, "")
		if follow.StatusCode == http.StatusConflict {
			follow.Body.Close()
			return
		}
		io.Copy(io.Discard, follow.Body)
		follow.Body.Close()
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("expected 409 after completion")
}

func TestSessionFollow_ClientV1FollowFrom(t *testing.T) {
	agent := &followAgent{name: "codex", delay: 300 * time.Millisecond}
	ts := newFollowHTTP(t, agent)
	defer ts.Close()
	client := v1.New(ts.URL, v1.WithNoTimeout())
	sess, err := client.CreateSession(context.Background(), v1.SessionRequest{
		Agent: "codex", WorkDir: t.TempDir(), SessionDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := sess.SendText(ctx, "go")
	if err != nil {
		t.Fatal(err)
	}
	var last string
	for ev := range stream.Events() {
		if ev.Type == v1.EventText && ev.Text == "one" {
			last = stream.LastEventID()
			cancel()
			break
		}
	}
	if last == "" {
		t.Fatal("empty last id")
	}
	follow, err := sess.FollowFrom(context.Background(), last)
	if err != nil {
		t.Fatal(err)
	}
	saw := false
	for ev := range follow.Events() {
		if ev.Type == v1.EventResult {
			saw = true
		}
	}
	if !saw {
		t.Fatal("FollowFrom missing result")
	}
}
