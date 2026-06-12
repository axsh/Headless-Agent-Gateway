package claudecode

import (
	"encoding/json"

	"github.com/axsh/arctic-tern/codingagent"
	"github.com/axsh/arctic-tern/logger"
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
	Text      string         `json:"text,omitempty"`
	Name      string         `json:"name,omitempty"`
	Input     map[string]any `json:"input,omitempty"`
	ToolUseID string         `json:"tool_use_id,omitempty"`
	Content   string         `json:"content,omitempty"`
}

// ParseJSONLinesEvent converts a single JSON Lines output line to a StreamEvent.
// Returns nil for events that should be ignored.
func ParseJSONLinesEvent(line string, logs ...logger.Logger) *codingagent.StreamEvent {
	if line == "" {
		return nil
	}

	var log logger.Logger
	if len(logs) > 0 {
		log = logs[0]
	}

	if log != nil {
		log.Trace("parsing JSON Lines event", "line", line)
	}

	var raw rawEvent
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		if log != nil {
			linePreview := line
			if len(linePreview) > 100 {
				linePreview = linePreview[:100] + "..."
			}
			log.Warn("failed to parse JSON Lines event", "error", err.Error(), "line_preview", linePreview)
		}
		return &codingagent.StreamEvent{Type: codingagent.EventError, Error: err}
	}

	var ev *codingagent.StreamEvent
	switch raw.Type {
	case "system":
		if raw.Subtype == "init" {
			ev = &codingagent.StreamEvent{
				Type:      codingagent.EventSystem,
				SessionID: raw.SessionID,
			}
		}

	case "stream_event":
		var payload streamEventPayload
		if err := json.Unmarshal(raw.Event, &payload); err != nil {
			ev = &codingagent.StreamEvent{Type: codingagent.EventError, Error: err}
		} else if payload.Type == "content_block_delta" && payload.Delta.Type == "text_delta" {
			ev = &codingagent.StreamEvent{
				Type:    codingagent.EventText,
				Content: payload.Delta.Text,
			}
		}

	case "assistant":
		var msg messagePayload
		if err := json.Unmarshal(raw.Message, &msg); err == nil {
			for _, block := range msg.Content {
				switch block.Type {
				case "tool_use":
					ev = &codingagent.StreamEvent{
						Type:      codingagent.EventToolUse,
						ToolName:  block.Name,
						ToolInput: block.Input,
					}
				case "text":
					if block.Text != "" {
						ev = &codingagent.StreamEvent{
							Type:    codingagent.EventText,
							Content: block.Text,
						}
					}
				}
				if ev != nil {
					break
				}
			}
		}

	case "user":
		var msg messagePayload
		if err := json.Unmarshal(raw.Message, &msg); err == nil {
			for _, block := range msg.Content {
				if block.Type == "tool_result" {
					ev = &codingagent.StreamEvent{
						Type:    codingagent.EventToolResult,
						Content: block.Content,
					}
					break
				}
			}
		}

	case "result":
		ev = &codingagent.StreamEvent{Type: codingagent.EventResult}
	}

	if ev != nil && log != nil {
		log.Debug("parsed event", "type", raw.Type, "subtype", raw.Subtype)
	}
	return ev
}
