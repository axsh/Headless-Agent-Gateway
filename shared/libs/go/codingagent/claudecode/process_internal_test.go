package claudecode

import (
	"context"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

// TestEmitTimeout_ClosedChannel_NoPanic verifies that emitTimeout does not panic
// when the destination channel is already closed (Option B: recover guard).
func TestEmitTimeout_ClosedChannel_NoPanic(t *testing.T) {
	ch := make(chan codingagent.StreamEvent, 1)
	close(ch)
	ctx := context.Background()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("emitTimeout panicked on closed channel: %v", r)
		}
	}()
	emitTimeout(ch, ctx, "timeout test")
}

// TestEmitTimeout_OpenChannel_SendsEvent verifies that emitTimeout sends an EventError
// to an open channel and the message content is preserved.
func TestEmitTimeout_OpenChannel_SendsEvent(t *testing.T) {
	ch := make(chan codingagent.StreamEvent, 1)
	ctx := context.Background()

	emitTimeout(ch, ctx, "idle timeout after 300s")

	select {
	case ev := <-ch:
		if ev.Type != codingagent.EventError {
			t.Errorf("expected EventError, got %v", ev.Type)
		}
		if ev.Content != "idle timeout after 300s" {
			t.Errorf("unexpected content: %q", ev.Content)
		}
	default:
		t.Fatal("expected event to be sent, but channel is empty")
	}
}

// TestEmitTimeout_CanceledContext_NoSend verifies that emitTimeout does not send
// when the context is already canceled. Uses an unbuffered channel so that the
// send case always blocks, making the select deterministic.
func TestEmitTimeout_CanceledContext_NoSend(t *testing.T) {
	ch := make(chan codingagent.StreamEvent) // unbuffered: send blocks, ctx.Done wins
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	emitTimeout(ch, ctx, "should not send")

	select {
	case ev := <-ch:
		t.Fatalf("unexpected event sent: %v", ev)
	default:
		// expected: no event sent when context is already canceled
	}
}
