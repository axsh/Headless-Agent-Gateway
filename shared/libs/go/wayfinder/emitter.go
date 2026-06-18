package wayfinder

import "github.com/axsh/arctic-tern/shared/libs/go/codingagent"

// EventEmitter sends streaming events from AgentCore to the adapter channel.
// If ch is nil or emitter is nil, Emit is a no-op.
type EventEmitter struct {
	ch chan<- codingagent.StreamEvent
}

// NewEventEmitter creates an EventEmitter wrapping the given channel.
func NewEventEmitter(ch chan<- codingagent.StreamEvent) *EventEmitter {
	return &EventEmitter{ch: ch}
}

// Emit sends a single event. Safe to call on nil receiver.
func (e *EventEmitter) Emit(ev codingagent.StreamEvent) {
	if e == nil || e.ch == nil {
		return
	}
	e.ch <- ev
}
