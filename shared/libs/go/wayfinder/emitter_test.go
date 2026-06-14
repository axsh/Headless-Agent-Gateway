package wayfinder

import (
	"testing"

	"github.com/axsh/arctic-tern/codingagent"
)

func TestEventEmitter_Emit(t *testing.T) {
	ch := make(chan codingagent.StreamEvent, 10)
	emitter := NewEventEmitter(ch)

	ev := codingagent.StreamEvent{
		Type:    codingagent.EventText,
		Content: "hello",
	}
	emitter.Emit(ev)

	select {
	case got := <-ch:
		if got.Type != codingagent.EventText {
			t.Errorf("expected EventText, got %s", got.Type)
		}
		if got.Content != "hello" {
			t.Errorf("expected 'hello', got %q", got.Content)
		}
	default:
		t.Fatal("expected event on channel, got nothing")
	}
}

func TestEventEmitter_NilSafe(t *testing.T) {
	// Calling Emit on a nil receiver must not panic.
	var emitter *EventEmitter
	emitter.Emit(codingagent.StreamEvent{Type: codingagent.EventText})
}

func TestEventEmitter_NilChannel(t *testing.T) {
	// Calling Emit with a nil channel must not panic.
	emitter := &EventEmitter{ch: nil}
	emitter.Emit(codingagent.StreamEvent{Type: codingagent.EventText})
}

func TestEventEmitter_ClosedChannel(t *testing.T) {
	// Calling Emit after the channel is closed must not panic.
	ch := make(chan codingagent.StreamEvent, 1)
	close(ch)
	emitter := NewEventEmitter(ch)
	emitter.Emit(codingagent.StreamEvent{Type: codingagent.EventText, Content: "test"})
}
