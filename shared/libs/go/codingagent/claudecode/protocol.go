package claudecode

import (
	"encoding/json"

	"github.com/axsh/hag/codingagent"
)

// rawEvent is the raw structure of Claude CLI JSON Lines output.
type rawEvent struct {
	Type      string          `json:"type"`
	Subtype   string          `json:"subtype,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
	Event     json.RawMessage `json:"event,omitempty"`
	Message   json.RawMessage `json:"message,omitempty"`
	Result    string          `json:"result,omitempty"`
}

// streamEventPayload is the event field inside a stream_event.
type streamEventPayload struct {
	Type  string `json:"type"`
	Delta struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"delta,omitempty"`
}

// messagePayload is the content array in assistant/user messages.
type messagePayload struct {
	Content []contentBlock `json:"content"`
}

type contentBlock struct {
	Type      string         `json:"type"`
	Name      string         `json:"name,omitempty"`
	Input     map[string]any `json:"input,omitempty"`
	ToolUseID string         `json:"tool_use_id,omitempty"`
	Content   string         `json:"content,omitempty"`
}

// ParseJSONLinesEvent converts a single JSON Lines output line to a StreamEvent.
// Returns nil for events that should be ignored.
func ParseJSONLinesEvent(line string) *codingagent.StreamEvent {
	if line == "" {
		return nil
	}

	var raw rawEvent
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return &codingagent.StreamEvent{Type: codingagent.EventError, Error: err}
	}

	switch raw.Type {
	case "system":
		if raw.Subtype == "init" {
			return &codingagent.StreamEvent{
				Type:      codingagent.EventSystem,
				SessionID: raw.SessionID,
			}
		}
		return nil

	case "stream_event":
		var payload streamEventPayload
		if err := json.Unmarshal(raw.Event, &payload); err != nil {
			return &codingagent.StreamEvent{Type: codingagent.EventError, Error: err}
		}
		if payload.Type == "content_block_delta" && payload.Delta.Type == "text_delta" {
			return &codingagent.StreamEvent{
				Type:    codingagent.EventText,
				Content: payload.Delta.Text,
			}
		}
		return nil

	case "assistant":
		var msg messagePayload
		if err := json.Unmarshal(raw.Message, &msg); err != nil {
			return nil
		}
		for _, block := range msg.Content {
			if block.Type == "tool_use" {
				return &codingagent.StreamEvent{
					Type:      codingagent.EventToolUse,
					ToolName:  block.Name,
					ToolInput: block.Input,
				}
			}
		}
		return nil

	case "user":
		var msg messagePayload
		if err := json.Unmarshal(raw.Message, &msg); err != nil {
			return nil
		}
		for _, block := range msg.Content {
			if block.Type == "tool_result" {
				return &codingagent.StreamEvent{
					Type:    codingagent.EventToolResult,
					Content: block.Content,
				}
			}
		}
		return nil

	case "result":
		return &codingagent.StreamEvent{Type: codingagent.EventResult}

	default:
		return nil
	}
}
