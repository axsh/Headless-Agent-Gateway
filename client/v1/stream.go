package v1

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/axsh/arctic-tern/client/internal/ssechunk"
)

// EventType identifies the type of streaming event.
type EventType string

const (
	EventText         EventType = "text"
	EventToolUse      EventType = "tool_use"
	EventToolResult   EventType = "tool_result"
	EventToolResultPart EventType = "tool_result_part"
	EventSystem       EventType = "system"
	EventResult       EventType = "result"
	EventError        EventType = "error"
	EventNodeStart    EventType = "node_start"
	EventNodeComplete EventType = "node_complete"
	EventNodeFailed   EventType = "node_failed"
	EventProgress     EventType = "progress"
	EventUserInputRequired EventType = "user_input_required"
)

// UserInputRequiredEvent carries structured user-input request data.
type UserInputRequiredEvent struct {
	Content  string
	PromptID string
	Choices  []string
}

// StreamHandlers configures callbacks for interactive streaming.
type StreamHandlers struct {
	OnText              func(text string)
	OnToolUse           func(toolName string)
	OnToolResult        func(content string)
	OnUserInputRequired func(ev UserInputRequiredEvent) (response string, err error)
	OnError             func(err string) error
	OnResult            func()
}

// Event is a single streaming event from the server.
type Event struct {
	Type               EventType
	Text               string
	ToolName           string
	Error              string
	UserInputRequired  UserInputRequiredEvent
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

// NewStreamFromReader creates a Stream from a reader (used in tests).
func NewStreamFromReader(r io.Reader) *Stream {
	return newStream(io.NopCloser(r))
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
		case EventUserInputRequired:
			fmt.Fprintf(w, "[User Input Required] %s\n", ev.UserInputRequired.Content)
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

// RunWithHandlers executes the stream with interactive handler callbacks.
func (s *Stream) RunWithHandlers(ctx context.Context, session *Session, h StreamHandlers) error {
	stream := s
	for {
		for ev := range stream.events() {
			switch ev.Type {
			case EventText:
				if h.OnText != nil {
					h.OnText(ev.Text)
				}
			case EventToolUse:
				if h.OnToolUse != nil {
					h.OnToolUse(ev.ToolName)
				}
			case EventToolResult:
				if h.OnToolResult != nil {
					h.OnToolResult(ev.Text)
				}
			case EventUserInputRequired:
				if h.OnUserInputRequired == nil {
					return fmt.Errorf("user input required but no handler configured")
				}
				answer, err := h.OnUserInputRequired(ev.UserInputRequired)
				if err != nil {
					return err
				}
				var err2 error
				stream, err2 = session.Respond(ctx, answer)
				if err2 != nil {
					return err2
				}
				goto nextStream
			case EventError:
				if h.OnError != nil {
					if err := h.OnError(ev.Error); err != nil {
						return err
					}
				}
				return fmt.Errorf("%s", ev.Error)
			case EventResult:
				if h.OnResult != nil {
					h.OnResult()
				}
			}
		}
		return nil
	nextStream:
	}
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
		assembler := ssechunk.NewAssembler()
		receivedDone := false

		emitError := func(msg string) {
			ch <- Event{Type: EventError, Error: msg}
		}

		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				receivedDone = true
				break
			}
			var raw struct {
				Type     string   `json:"type"`
				Content  string   `json:"content"`
				ToolName string   `json:"tool_name,omitempty"`
				PromptID string   `json:"prompt_id,omitempty"`
				Choices  []string `json:"choices,omitempty"`
				ChunkID  string   `json:"chunk_id,omitempty"`
				Index    int      `json:"index,omitempty"`
				Total    int      `json:"total,omitempty"`
			}
			if err := json.Unmarshal([]byte(data), &raw); err != nil {
				continue
			}

			switch EventType(raw.Type) {
			case EventToolResultPart:
				if err := assembler.AddPart(ssechunk.Part{
					ChunkID:    raw.ChunkID,
					ChunkIndex: raw.Index,
					ChunkTotal: raw.Total,
					Content:    raw.Content,
				}); err != nil {
					emitError(err.Error())
					return
				}
				continue
			case EventToolResult:
				if raw.Content != "" {
					ch <- Event{Type: EventToolResult, Text: raw.Content, ToolName: raw.ToolName}
					continue
				}
				if raw.ChunkID != "" {
					content, err := assembler.Complete(raw.ChunkID)
					if err != nil {
						emitError(err.Error())
						return
					}
					ch <- Event{Type: EventToolResult, Text: content, ToolName: raw.ToolName}
					continue
				}
				ch <- Event{Type: EventToolResult, Text: raw.Content, ToolName: raw.ToolName}
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
			if ev.Type == EventUserInputRequired {
				ev.UserInputRequired = UserInputRequiredEvent{
					Content:  raw.Content,
					PromptID: raw.PromptID,
					Choices:  raw.Choices,
				}
			}
			ch <- ev
		}

		if err := assembler.FlushIncomplete(); err != nil {
			emitError(err.Error())
			return
		}
		if err := scanner.Err(); err != nil {
			emitError(fmt.Sprintf("stream read error: %v", err))
			return
		}
		if !receivedDone {
			emitError("stream terminated unexpectedly without completion marker")
		}
	}()
	return ch
}
