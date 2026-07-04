package v1

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// EventType identifies the type of streaming event.
type EventType string

const (
	EventText         EventType = "text"
	EventToolUse      EventType = "tool_use"
	EventToolResult   EventType = "tool_result"
	EventSystem       EventType = "system"
	EventResult       EventType = "result"
	EventError        EventType = "error"
	EventNodeStart    EventType = "node_start"
	EventNodeComplete EventType = "node_complete"
	EventNodeFailed   EventType = "node_failed"
	EventProgress     EventType = "progress"
)

// Event is a single streaming event from the server.
type Event struct {
	Type     EventType
	Text     string
	ToolName string
	Error    string
}

// Stream processes SSE events from a session message.
type Stream struct {
	body      io.ReadCloser
	onText    func(text string)
	onResult  func(ev Event)
	onError   func(err string)
	onToolUse func(toolName string)
}

// newStream creates a Stream from an HTTP response body.
func newStream(body io.ReadCloser) *Stream {
	return &Stream{body: body}
}

// Output writes all text events to the given writer and blocks until completion.
// Returns error if an error event is received.
// Output writes all text events to the given writer and blocks until completion.
// Returns error if an error event is received.
func (s *Stream) Output(w io.Writer) error {
	defer s.body.Close()
	for ev := range s.events() {
		switch ev.Type {
		case EventText:
			fmt.Fprint(w, ev.Text)
		case EventToolUse:
			fmt.Fprintf(w, "\n[Tool: %s]\n", ev.ToolName)
		case EventToolResult:
			fmt.Fprintf(w, "[Tool Result] %s\n", ev.Text)
		case EventSystem:
			fmt.Fprintf(w, "[System] %s\n", ev.Text)
		case EventError:
			return fmt.Errorf("%s", ev.Error)
		case EventResult:
			// Result event, no output needed.
		case EventNodeStart:
			fmt.Fprintf(w, "\n[Node Start: %s]\n", ev.Text)
		case EventNodeComplete:
			fmt.Fprintf(w, "[Node Complete: %s]\n", ev.Text)
		case EventNodeFailed:
			fmt.Fprintf(w, "[Node Failed: %s]\n", ev.Text)
		case EventProgress:
			fmt.Fprintf(w, "[WBS %s]\n", ev.Text)
		}
	}
	return nil
}

// OnText sets a custom handler for text events.
func (s *Stream) OnText(fn func(text string)) *Stream {
	s.onText = fn
	return s
}

// OnResult sets a custom handler for result events.
func (s *Stream) OnResult(fn func(ev Event)) *Stream {
	s.onResult = fn
	return s
}

// OnError sets a custom handler for error events.
func (s *Stream) OnError(fn func(err string)) *Stream {
	s.onError = fn
	return s
}

// OnToolUse sets a custom handler for tool_use events.
func (s *Stream) OnToolUse(fn func(toolName string)) *Stream {
	s.onToolUse = fn
	return s
}

// Run executes the stream with the configured handlers.
// Blocks until the stream is completed.
func (s *Stream) Run() error {
	defer s.body.Close()
	for ev := range s.events() {
		switch ev.Type {
		case EventText:
			if s.onText != nil {
				s.onText(ev.Text)
			}
		case EventToolUse:
			if s.onToolUse != nil {
				s.onToolUse(ev.ToolName)
			}
		case EventResult:
			if s.onResult != nil {
				s.onResult(ev)
			}
		case EventError:
			if s.onError != nil {
				s.onError(ev.Error)
			}
			return fmt.Errorf("%s", ev.Error)
		}
	}
	return nil
}

// Events returns a channel of raw events for full control.
func (s *Stream) Events() <-chan Event {
	ch := make(chan Event)
	go func() {
		defer s.body.Close()
		defer close(ch)
		for ev := range s.events() {
			ch <- ev
		}
	}()
	return ch
}

// events is the internal event iterator using iter pattern.
func (s *Stream) events() <-chan Event {
	ch := make(chan Event, 8)
	go func() {
		defer close(ch)
		scanner := bufio.NewScanner(s.body)
		receivedDone := false
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				receivedDone = true
				return
			}
			var raw struct {
				Type     string `json:"type"`
				Content  string `json:"content"`
				ToolName string `json:"tool_name,omitempty"`
			}
			if err := json.Unmarshal([]byte(data), &raw); err != nil {
				continue
			}
			ev := Event{
				Type:     EventType(raw.Type),
				Text:     raw.Content,
				ToolName: raw.ToolName,
			}
			if ev.Type == EventError {
				ev.Error = raw.Content
			}
			ch <- ev
		}
		// Detect abnormal stream termination.
		if err := scanner.Err(); err != nil {
			ch <- Event{
				Type:  EventError,
				Error: fmt.Sprintf("stream read error: %v", err),
			}
		} else if !receivedDone {
			ch <- Event{
				Type:  EventError,
				Error: "stream terminated unexpectedly without completion marker",
			}
		}
	}()
	return ch
}
