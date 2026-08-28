package agentservice

import (
	"testing"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

func TestExecRegistry_RegisterRejectsDuplicate(t *testing.T) {
	reg := newExecRegistry()
	exec := &activeExecution{sessionID: "s1", status: codingagent.StatusActive}
	if err := reg.Register("s1", exec); err != nil {
		t.Fatalf("first register: %v", err)
	}
	if err := reg.Register("s1", exec); err == nil {
		t.Fatal("expected ErrSessionBusy")
	}
}

func TestExecRegistry_SetStatus(t *testing.T) {
	reg := newExecRegistry()
	exec := &activeExecution{sessionID: "s1", status: codingagent.StatusActive}
	_ = reg.Register("s1", exec)
	reg.SetStatus("s1", codingagent.StatusSuspended)
	got, ok := reg.Get("s1")
	if !ok || got.status != codingagent.StatusSuspended {
		t.Fatalf("status = %q, ok=%v", got.status, ok)
	}
}

func TestEventRelay_IsSourceDone(t *testing.T) {
	ch := make(chan codingagent.StreamEvent)
	relay := newEventRelay(ch)
	if relay.isSourceDone() {
		t.Fatal("expected sourceDone=false before close")
	}
	close(ch)
	deadline := time.Now().Add(2 * time.Second)
	for !relay.isSourceDone() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for sourceDone")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !relay.isSourceDone() {
		t.Fatal("expected sourceDone=true after close")
	}
}

func TestEventRelay_StreamDrainsAfterSourceClose(t *testing.T) {
	src := make(chan codingagent.StreamEvent, 4)
	relay := newEventRelay(src)
	src <- codingagent.StreamEvent{Type: codingagent.EventText, Content: "a"}
	src <- codingagent.StreamEvent{Type: codingagent.EventText, Content: "b"}
	src <- codingagent.StreamEvent{Type: codingagent.EventResult}
	close(src)

	out := relay.stream(0, false)
	var got []codingagent.StreamEvent
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev, ok := <-out:
			if !ok {
				if len(got) != 3 {
					t.Fatalf("got %d events, want 3", len(got))
				}
				return
			}
			got = append(got, ev)
		case <-deadline:
			t.Fatalf("timed out draining stream, got %d events", len(got))
		}
	}
}

func TestEventRelay_NoHangWhenDoneRacesWithWait(t *testing.T) {
	const rounds = 100
	for i := 0; i < rounds; i++ {
		src := make(chan codingagent.StreamEvent)
		relay := newEventRelay(src)
		out := relay.stream(0, false)

		done := make(chan struct{})
		go func() {
			defer close(done)
			for range out {
			}
		}()

		// Let the subscriber reach the wait path with an empty buffer.
		time.Sleep(time.Millisecond)
		close(src)

		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatalf("round %d: stream hung after source close", i)
		}
	}
}

func TestEventRelay_StopOnUserInputStillCloses(t *testing.T) {
	src := make(chan codingagent.StreamEvent, 4)
	relay := newEventRelay(src)
	src <- codingagent.StreamEvent{Type: codingagent.EventText, Content: "before"}
	src <- codingagent.StreamEvent{Type: codingagent.EventUserInputRequired, Content: "ask"}
	src <- codingagent.StreamEvent{Type: codingagent.EventText, Content: "after"}
	close(src)

	out := relay.stream(0, true)
	var got []codingagent.StreamEvent
	for ev := range out {
		got = append(got, ev)
	}
	if len(got) != 2 {
		t.Fatalf("got %d events, want 2 (stop on user input)", len(got))
	}
	if got[1].Type != codingagent.EventUserInputRequired {
		t.Fatalf("last event = %v, want user_input_required", got[1].Type)
	}
}
