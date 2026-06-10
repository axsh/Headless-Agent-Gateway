package codex

import (
	"encoding/json"

	"github.com/axsh/hag/codingagent"
)

// ExecEvent represents a JSONL event from "codex exec --json" output.
// Event types include: thread.started, turn.started, turn.completed,
// turn.failed, message.text, message.tool_use, error, etc.
type ExecEvent struct {
	Type     string          `json:"type"`
	Message  string          `json:"message,omitempty"`
	ThreadID string          `json:"thread_id,omitempty"`
	Error    json.RawMessage `json:"error,omitempty"`
}

// ExecEventMessage is the data payload for message-type events.
type ExecEventMessage struct {
	Type    string `json:"type"`
	Text    string `json:"text,omitempty"`
	Name    string `json:"name,omitempty"`
	Content string `json:"content,omitempty"`
}

// ParseExecEvent converts a JSONL line from "codex exec --json" to a StreamEvent.
// Returns nil for events that don't map to StreamEvent types.
func ParseExecEvent(line string) *codingagent.StreamEvent {
	var ev ExecEvent
	if err := json.Unmarshal([]byte(line), &ev); err != nil {
		return nil
	}

	switch ev.Type {
	case "message":
		// Message event with content array
		var msg struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text,omitempty"`
			} `json:"content,omitempty"`
		}
		json.Unmarshal([]byte(line), &msg)
		if len(msg.Content) > 0 {
			var texts []string
			for _, c := range msg.Content {
				if c.Type == "output_text" || c.Type == "text" {
					texts = append(texts, c.Text)
				}
			}
			if len(texts) > 0 {
				combined := ""
				for _, t := range texts {
					combined += t
				}
				return &codingagent.StreamEvent{Type: codingagent.EventText, Content: combined}
			}
		}
		return nil

	case "response.output_text.delta":
		// Streaming text delta
		var delta struct {
			Delta string `json:"delta"`
		}
		json.Unmarshal([]byte(line), &delta)
		if delta.Delta != "" {
			return &codingagent.StreamEvent{Type: codingagent.EventText, Content: delta.Delta}
		}
		return nil

	case "response.output_text.done":
		// Text completion
		var done struct {
			Text string `json:"text"`
		}
		json.Unmarshal([]byte(line), &done)
		if done.Text != "" {
			return &codingagent.StreamEvent{Type: codingagent.EventText, Content: done.Text}
		}
		return nil

	case "function_call":
		// Tool use event
		var tc struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}
		json.Unmarshal([]byte(line), &tc)
		return &codingagent.StreamEvent{
			Type:     codingagent.EventToolUse,
			ToolName: tc.Name,
			ToolInput: map[string]any{
				"arguments": tc.Arguments,
			},
		}

	case "function_call_output":
		// Tool result
		return &codingagent.StreamEvent{Type: codingagent.EventResult}

	case "error":
		return &codingagent.StreamEvent{
			Type:    codingagent.EventError,
			Content: ev.Message,
		}

	case "turn.failed":
		// Parse nested error message
		var fail struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		json.Unmarshal([]byte(line), &fail)
		msg := fail.Error.Message
		if msg == "" {
			msg = "codex turn failed"
		}
		return &codingagent.StreamEvent{
			Type:    codingagent.EventError,
			Content: msg,
		}

	case "turn.completed":
		return &codingagent.StreamEvent{Type: codingagent.EventResult}

	case "thread.started", "turn.started":
		// Lifecycle events - no mapping needed
		return nil

	default:
		return nil
	}
}
