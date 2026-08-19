package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	client "github.com/axsh/arctic-tern/client/v1"
)

type stubCounters struct {
	mu           sync.Mutex
	messagePOSTs int
	eventGETs    int
	eventFrom    []string
}

func (c *stubCounters) addMessage() {
	c.mu.Lock()
	c.messagePOSTs++
	c.mu.Unlock()
}

func (c *stubCounters) addEvent(rawQuery string) {
	c.mu.Lock()
	c.eventGETs++
	c.eventFrom = append(c.eventFrom, rawQuery)
	c.mu.Unlock()
}

func writeSSE(w http.ResponseWriter, id, payload string) {
	if id != "" {
		fmt.Fprintf(w, "id: %s\n", id)
	}
	fmt.Fprintf(w, "data: %s\n\n", payload)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func newFollowStub(t *testing.T, c *stubCounters, writeMessageID0 bool) *httptest.Server {
	t.Helper()
	relay := []struct {
		id      string
		payload string
	}{
		{id: "0", payload: `{"type":"text","content":"one"}`},
		{id: "1", payload: `{"type":"text","content":"two"}`},
		{id: "2", payload: `{"type":"result"}`},
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/sessions":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"session_id": "sess-stub-001"})

		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
			c.addMessage()
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.WriteHeader(http.StatusOK)
			writeSSE(w, "", `{"type":"system","content":"turn context"}`)
			if writeMessageID0 {
				writeSSE(w, "0", `{"type":"text","content":"one"}`)
			}
			<-r.Context().Done()

		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/events"):
			c.addEvent(r.URL.RawQuery)
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.WriteHeader(http.StatusOK)
			from := r.URL.Query().Get("from")
			start := 0
			if from != "" {
				n, err := strconv.Atoi(from)
				if err == nil {
					start = n + 1
				}
			}
			writeSSE(w, "", `{"type":"system","content":"turn context"}`)
			for _, ev := range relay {
				idNum, _ := strconv.Atoi(ev.id)
				if idNum < start {
					continue
				}
				writeSSE(w, ev.id, ev.payload)
			}
			fmt.Fprintf(w, "data: [DONE]\n\n")
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}

		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/sessions/"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":          "sess-stub-001",
				"status":      "active",
				"followable":  true,
				"turn_id":     "turn-stub",
				"agent_name":  "claudecode",
				"work_dir":    ".",
				"session_dir": "",
			})

		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/terminate"):
			w.WriteHeader(http.StatusOK)

		default:
			t.Logf("stub: unhandled %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
}

func testFlags(dropAfter int) *runFlags {
	return &runFlags{
		Agent:     "claudecode",
		WorkDir:   ".",
		Prompt:    "test prompt",
		DropAfter: dropAfter,
	}
}

func TestRunFollowDemo_DropThenFollowFrom(t *testing.T) {
	counters := &stubCounters{}
	srv := newFollowStub(t, counters, true)
	defer srv.Close()

	c := client.New(srv.URL, client.WithNoTimeout())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	out, err := runFollowDemo(ctx, testFlags(1), c, t.Logf)
	if err != nil {
		t.Fatalf("runFollowDemo: %v", err)
	}
	if out.DropLastID != "0" {
		t.Fatalf("DropLastID = %q, want 0", out.DropLastID)
	}
	if out.FollowMode != "FollowFrom" {
		t.Fatalf("FollowMode = %q, want FollowFrom", out.FollowMode)
	}
	if !out.SawResult {
		t.Fatal("expected SawResult")
	}
	if !out.Followable || out.Status != "active" || out.TurnID != "turn-stub" {
		t.Fatalf("GetSession snapshot status=%s followable=%v turn_id=%s", out.Status, out.Followable, out.TurnID)
	}

	counters.mu.Lock()
	defer counters.mu.Unlock()
	foundFrom0 := false
	for _, q := range counters.eventFrom {
		if strings.Contains(q, "from=0") {
			foundFrom0 = true
			break
		}
	}
	if !foundFrom0 {
		t.Fatalf("expected events query with from=0, got %v", counters.eventFrom)
	}
}

func TestRunFollowDemo_FollowWithoutFrom(t *testing.T) {
	counters := &stubCounters{}
	srv := newFollowStub(t, counters, false)
	defer srv.Close()

	c := client.New(srv.URL, client.WithNoTimeout())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	out, err := runFollowDemo(ctx, testFlags(0), c, t.Logf)
	if err != nil {
		t.Fatalf("runFollowDemo: %v", err)
	}
	if out.FollowMode != "Follow" {
		t.Fatalf("FollowMode = %q, want Follow", out.FollowMode)
	}
	if out.DropLastID != "" {
		t.Fatalf("DropLastID = %q, want empty", out.DropLastID)
	}

	counters.mu.Lock()
	defer counters.mu.Unlock()
	if len(counters.eventFrom) == 0 {
		t.Fatal("expected GET /events")
	}
	for _, q := range counters.eventFrom {
		if strings.Contains(q, "from=") {
			t.Fatalf("did not want from= query, got %v", counters.eventFrom)
		}
	}
}

func TestRunFollowDemo_NoSendOnFollow(t *testing.T) {
	counters := &stubCounters{}
	srv := newFollowStub(t, counters, true)
	defer srv.Close()

	c := client.New(srv.URL, client.WithNoTimeout())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := runFollowDemo(ctx, testFlags(1), c, t.Logf); err != nil {
		t.Fatalf("runFollowDemo: %v", err)
	}

	counters.mu.Lock()
	defer counters.mu.Unlock()
	if counters.messagePOSTs != 1 {
		t.Fatalf("POST /messages = %d, want 1", counters.messagePOSTs)
	}
}
