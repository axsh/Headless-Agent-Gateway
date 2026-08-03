package codex

import (
	"encoding/json"
	"strings"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

// ExecEvent represents a JSONL event from "codex exec --json" output.
// Codex CLI 0.139.0+ wraps events in response_item/event_msg envelopes
// with the actual event type in payload.type. Older flat-format events
// (e.g. {"type":"function_call",...}) are also supported for backward compatibility.
type ExecEvent struct {
	Type     string          `json:"type"`
	Message  string          `json:"message,omitempty"`
	ThreadID string          `json:"thread_id,omitempty"`
	Error    json.RawMessage `json:"error,omitempty"`
	Payload  json.RawMessage `json:"payload,omitempty"`
	Item     json.RawMessage `json:"item,omitempty"`
}

// ExecEventMessage is the data payload for message-type events.
type ExecEventMessage struct {
	Type    string `json:"type"`
	Text    string `json:"text,omitempty"`
	Name    string `json:"name,omitempty"`
	Content string `json:"content,omitempty"`
}

// ParseExecEvent converts a JSONL line from "codex exec --json" to a StreamEvent.
// Handles both nested format (Codex CLI 0.139.0+) and flat format (backward compat).
// Returns nil for events that don't map to StreamEvent types.
func ParseExecEvent(line string) *codingagent.StreamEvent {
	var ev ExecEvent
	if err := json.Unmarshal([]byte(line), &ev); err != nil {
		return nil
	}

	switch ev.Type {
	// --- Codex CLI 0.139.0 stdout format (item.started / item.completed) ---

	case "item.started":
		// Tool use start: {"type":"item.started","item":{"type":"command_execution","command":"..."}}
		return parseItemEvent(ev.Item, false)

	case "item.completed":
		// Tool result or message: {"type":"item.completed","item":{"type":"command_execution","aggregated_output":"..."}}
		// or: {"type":"item.completed","item":{"type":"agent_message","text":"..."}}
		return parseItemEvent(ev.Item, true)

	// --- Nested format (session rollout logs) ---

	case "response_item":
		// Envelope: {"type":"response_item","payload":{"type":"function_call",...}}
		var header struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(ev.Payload, &header); err != nil {
			return nil
		}
		return parsePayloadEvent(header.Type, ev.Payload)

	case "event_msg":
		// Envelope: {"type":"event_msg","payload":{"type":"agent_message","message":"..."}}
		var msg struct {
			Type    string `json:"type"`
			Message string `json:"message,omitempty"`
		}
		if err := json.Unmarshal(ev.Payload, &msg); err != nil {
			return nil
		}
		switch msg.Type {
		case "agent_message":
			return &codingagent.StreamEvent{Type: codingagent.EventText, Content: msg.Message}
		case "task_complete":
			return &codingagent.StreamEvent{Type: codingagent.EventResult}
		default:
			// token_count, user_message, task_started etc. - ignore
			return nil
		}

	case "session_meta", "turn_context":
		// Lifecycle metadata events - no mapping needed
		return nil

	// --- Flat format (backward compatibility) ---

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
		// Tool use event (flat format)
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
		// Tool result (flat format) - parse the output content.
		var out struct {
			Output string `json:"output"`
		}
		json.Unmarshal([]byte(line), &out)
		return &codingagent.StreamEvent{
			Type:    codingagent.EventToolResult,
			Content: out.Output,
		}

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

// parsePayloadEvent converts a response_item payload to a StreamEvent.
// Handles function_call, function_call_output, and message payloads.
func parsePayloadEvent(payloadType string, payload json.RawMessage) *codingagent.StreamEvent {
	switch payloadType {
	case "function_call":
		var tc struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}
		json.Unmarshal(payload, &tc)
		return &codingagent.StreamEvent{
			Type:     codingagent.EventToolUse,
			ToolName: tc.Name,
			ToolInput: map[string]any{
				"arguments": tc.Arguments,
			},
		}

	case "function_call_output":
		var out struct {
			Output string `json:"output"`
		}
		json.Unmarshal(payload, &out)
		return &codingagent.StreamEvent{
			Type:    codingagent.EventToolResult,
			Content: out.Output,
		}

	case "message":
		// Assistant message with content array
		var msg struct {
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text,omitempty"`
			} `json:"content,omitempty"`
		}
		json.Unmarshal(payload, &msg)
		if msg.Role == "assistant" && len(msg.Content) > 0 {
			var texts []string
			for _, c := range msg.Content {
				if c.Type == "output_text" || c.Type == "text" {
					texts = append(texts, c.Text)
				}
			}
			if len(texts) > 0 {
				return &codingagent.StreamEvent{
					Type:    codingagent.EventText,
					Content: strings.Join(texts, ""),
				}
			}
		}
		return nil

	default:
		return nil
	}
}

// parseItemEvent converts an item.started or item.completed payload to a StreamEvent.
// Codex CLI 0.139.0 stdout uses {"type":"item.started/completed","item":{...}} format.
func parseItemEvent(item json.RawMessage, completed bool) *codingagent.StreamEvent {
	if item == nil {
		return nil
	}
	type fileChangeEntry struct {
		Path string `json:"path"`
		Kind string `json:"kind"` // add | update | delete
	}

	var header struct {
		Type             string            `json:"type"`
		Command          string            `json:"command,omitempty"`
		AggregatedOutput string            `json:"aggregated_output,omitempty"`
		ExitCode         *int              `json:"exit_code,omitempty"`
		Text             string            `json:"text,omitempty"`
		Changes          []fileChangeEntry `json:"changes,omitempty"`
	}
	if err := json.Unmarshal(item, &header); err != nil {
		return nil
	}

	switch header.Type {
	case "command_execution":
		if completed {
			// Emit ToolUse on completed so ToolCallAnalyzer sees the command even when
			// item.started was not streamed (common in codex exec --json batch output).
			if header.Command != "" {
				return &codingagent.StreamEvent{
					Type:     codingagent.EventToolUse,
					ToolName: "command_execution",
					ToolInput: map[string]any{
						"command": header.Command,
					},
				}
			}
			return &codingagent.StreamEvent{
				Type:    codingagent.EventToolResult,
				Content: header.AggregatedOutput,
			}
		}
		// item.started with command_execution -> tool use
		return &codingagent.StreamEvent{
			Type:     codingagent.EventToolUse,
			ToolName: "command_execution",
			ToolInput: map[string]any{
				"command": header.Command,
			},
		}

	case "agent_message":
		if completed {
			return &codingagent.StreamEvent{
				Type:    codingagent.EventText,
				Content: header.Text,
			}
		}
		return nil

	case "file_change":
		if !completed || len(header.Changes) == 0 {
			return nil
		}
		if len(header.Changes) == 1 {
			c := header.Changes[0]
			return &codingagent.StreamEvent{
				Type:     codingagent.EventToolUse,
				ToolName: "file_change",
				ToolInput: map[string]any{
					"path": c.Path,
					"kind": c.Kind,
				},
			}
		}
		changes := make([]map[string]any, len(header.Changes))
		for i, c := range header.Changes {
			changes[i] = map[string]any{"path": c.Path, "kind": c.Kind}
		}
		return &codingagent.StreamEvent{
			Type:     codingagent.EventToolUse,
			ToolName: "file_change",
			ToolInput: map[string]any{
				"changes": changes,
			},
		}

	default:
		return nil
	}
}
