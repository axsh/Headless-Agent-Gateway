package agentservice

import (
	"errors"
	"sync"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

// ErrSessionBusy is returned when a session already has an active execution.
var ErrSessionBusy = errors.New("session busy")

type activeExecution struct {
	sessionID     string
	turnID        string
	correlationID string
	agentSess     codingagent.Session
	stdin         codingagent.StdinWriter
	relay         *eventRelay
	status        string
	streamOffset  int
}

type execRegistry struct {
	mu   sync.Mutex
	exec map[string]*activeExecution
}

func newExecRegistry() *execRegistry {
	return &execRegistry{exec: make(map[string]*activeExecution)}
}

func (r *execRegistry) Register(id string, exec *activeExecution) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.exec[id]; ok {
		return ErrSessionBusy
	}
	r.exec[id] = exec
	return nil
}

func (r *execRegistry) Get(id string) (*activeExecution, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	exec, ok := r.exec[id]
	return exec, ok
}

func (r *execRegistry) SetStatus(id, status string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if exec, ok := r.exec[id]; ok {
		exec.status = status
	}
}

func (r *execRegistry) Unregister(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.exec, id)
}

// eventRelay buffers events from a single agent channel for sequential SSE consumers.
type eventRelay struct {
	mu         sync.Mutex
	events     []codingagent.StreamEvent
	notify     chan struct{}
	sourceDone bool
}

func newEventRelay(source <-chan codingagent.StreamEvent) *eventRelay {
	r := &eventRelay{notify: make(chan struct{}, 1)}
	go func() {
		for ev := range source {
			r.mu.Lock()
			r.events = append(r.events, ev)
			r.mu.Unlock()
			select {
			case r.notify <- struct{}{}:
			default:
			}
		}
		r.mu.Lock()
		r.sourceDone = true
		r.mu.Unlock()
		select {
		case r.notify <- struct{}{}:
		default:
		}
	}()
	return r
}

// stream returns events starting at startIdx. When stopOnUserInput is true,
// the channel closes after delivering EventUserInputRequired.
func (r *eventRelay) stream(startIdx int, stopOnUserInput bool) <-chan codingagent.StreamEvent {
	ch := make(chan codingagent.StreamEvent, 8)
	go func() {
		defer close(ch)
		idx := startIdx
		for {
			r.mu.Lock()
			for idx < len(r.events) {
				ev := r.events[idx]
				idx++
				r.mu.Unlock()
				ch <- ev
				if stopOnUserInput && ev.Type == codingagent.EventUserInputRequired {
					return
				}
				r.mu.Lock()
			}
			done := r.sourceDone
			r.mu.Unlock()
			if done {
				return
			}
			<-r.notify
		}
	}()
	return ch
}

func (r *eventRelay) eventCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.events)
}

// EventsSnapshot returns a copy of buffered relay events.
func (r *eventRelay) EventsSnapshot() []codingagent.StreamEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]codingagent.StreamEvent(nil), r.events...)
}

func (r *eventRelay) snapshot() []codingagent.StreamEvent {
	if r == nil {
		return nil
	}
	return r.EventsSnapshot()
}
