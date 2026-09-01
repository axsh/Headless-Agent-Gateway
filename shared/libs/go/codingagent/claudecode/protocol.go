package claudecode

import (
	"encoding/json"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/logger"
)

// rawEvent is the raw structure of Claude CLI JSON Lines output.
type rawEvent struct {
	Type         string          `json:"type"`
	Subtype      string          `json:"subtype,omitempty"`
	SessionID    string          `json:"session_id,omitempty"`
	Event        json.RawMessage `json:"event,omitempty"`
	Message      json.RawMessage `json:"message,omitempty"`
	Result       string          `json:"result,omitempty"`
	Usage        json.RawMessage `json:"usage,omitempty"`
	TotalCostUSD *float64        `json:"total_cost_usd,omitempty"`
	ModelUsage   json.RawMessage `json:"modelUsage,omitempty"`
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
	ID      string           `json:"id,omitempty"`
	Content []contentBlock   `json:"content"`
	Usage   *claudeUsageNums `json:"usage,omitempty"`
}

type contentBlock struct {
	Type      string         `json:"type"`
	ID        string         `json:"id,omitempty"`
	Text      string         `json:"text,omitempty"`
	Name      string         `json:"name,omitempty"`
	Input     map[string]any `json:"input,omitempty"`
	ToolUseID string         `json:"tool_use_id,omitempty"`
	Content   string         `json:"content,omitempty"`
}

type claudeUsageNums struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CachedInputTokens        int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

// ParseJSONLinesEvent converts a single JSON Lines output line to a StreamEvent.
// Returns nil for events that should be ignored.
func ParseJSONLinesEvent(line string, logs ...logger.Logger) *codingagent.StreamEvent {
	events := ParseJSONLinesEvents(line, logs...)
	if len(events) == 0 {
		return nil
	}
	return events[0]
}

// ParseJSONLinesEvents converts a JSON Lines output line to zero or more StreamEvents.
// Assistant messages with multiple tool_use blocks emit one event per tool_use.
func ParseJSONLinesEvents(line string, logs ...logger.Logger) []*codingagent.StreamEvent {
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
		return []*codingagent.StreamEvent{{Type: codingagent.EventError, Error: err}}
	}

	var out []*codingagent.StreamEvent
	switch raw.Type {
	case "system":
		if raw.Subtype == "init" {
			out = append(out, &codingagent.StreamEvent{
				Type:      codingagent.EventSystem,
				SessionID: raw.SessionID,
			})
		}

	case "stream_event":
		var payload streamEventPayload
		if err := json.Unmarshal(raw.Event, &payload); err != nil {
			out = append(out, &codingagent.StreamEvent{Type: codingagent.EventError, Error: err})
		} else if payload.Type == "content_block_delta" && payload.Delta.Type == "text_delta" {
			out = append(out, &codingagent.StreamEvent{
				Type:    codingagent.EventText,
				Content: payload.Delta.Text,
			})
		}

	case "assistant":
		var msg messagePayload
		if err := json.Unmarshal(raw.Message, &msg); err == nil {
			for _, block := range msg.Content {
				if block.Type == "tool_use" {
					out = append(out, &codingagent.StreamEvent{
						Type:       codingagent.EventToolUse,
						ToolName:   block.Name,
						ToolInput:  block.Input,
						ToolCallID: block.ID,
					})
				}
			}
			if len(out) == 0 {
				for _, block := range msg.Content {
					if block.Type == "text" && block.Text != "" {
						out = append(out, &codingagent.StreamEvent{
							Type:    codingagent.EventText,
							Content: block.Text,
						})
						break
					}
				}
			}
			if callUsage := claudeCallUsage(msg); callUsage != nil {
				if len(out) == 0 {
					out = append(out, &codingagent.StreamEvent{Type: codingagent.EventSystem})
				}
				for _, ev := range out {
					u := *callUsage
					ev.Usage = &u
				}
			}
		}

	case "user":
		var msg messagePayload
		if err := json.Unmarshal(raw.Message, &msg); err == nil {
			for _, block := range msg.Content {
				if block.Type == "tool_result" {
					out = append(out, &codingagent.StreamEvent{
						Type:       codingagent.EventToolResult,
						Content:    block.Content,
						ToolCallID: block.ToolUseID,
					})
					break
				}
			}
		}

	case "result":
		ev := &codingagent.StreamEvent{Type: codingagent.EventResult}
		if u := parseClaudeResultUsage(raw); u != nil {
			ev.Usage = u
		}
		out = append(out, ev)
	}

	if len(out) > 0 && log != nil {
		log.Debug("parsed event", "type", raw.Type, "subtype", raw.Subtype, "event_count", len(out))
	}
	return out
}

func claudeCallUsage(msg messagePayload) *codingagent.TokenUsage {
	if msg.Usage == nil {
		return nil
	}
	if msg.Usage.InputTokens == 0 && msg.Usage.OutputTokens == 0 &&
		msg.Usage.CachedInputTokens == 0 && msg.Usage.CacheCreationInputTokens == 0 {
		return nil
	}
	return &codingagent.TokenUsage{
		InputTokens:              msg.Usage.InputTokens,
		OutputTokens:             msg.Usage.OutputTokens,
		CachedInputTokens:        msg.Usage.CachedInputTokens,
		CacheCreationInputTokens: msg.Usage.CacheCreationInputTokens,
		Source:                   codingagent.UsageSourceClaudeAssistant,
		Confidence:               codingagent.UsageConfidenceHigh,
		CallID:                   msg.ID,
	}
}

func parseClaudeResultUsage(raw rawEvent) *codingagent.TokenUsage {
	u := &codingagent.TokenUsage{
		Source:       codingagent.UsageSourceClaudeResult,
		Confidence:   codingagent.UsageConfidenceHigh,
		TotalCostUSD: raw.TotalCostUSD,
	}
	if len(raw.Usage) > 0 {
		var nums claudeUsageNums
		if err := json.Unmarshal(raw.Usage, &nums); err == nil {
			u.InputTokens = nums.InputTokens
			u.OutputTokens = nums.OutputTokens
			u.CachedInputTokens = nums.CachedInputTokens
			u.CacheCreationInputTokens = nums.CacheCreationInputTokens
		}
	}
	// modelUsage may carry additional per-model totals (camelCase fields).
	if len(raw.ModelUsage) > 0 {
		var models map[string]struct {
			InputTokens              int     `json:"inputTokens"`
			OutputTokens             int     `json:"outputTokens"`
			CacheReadInputTokens     int     `json:"cacheReadInputTokens"`
			CacheCreationInputTokens int     `json:"cacheCreationInputTokens"`
			CostUSD                  float64 `json:"costUSD"`
		}
		if err := json.Unmarshal(raw.ModelUsage, &models); err == nil {
			var in, out, cacheRead, cacheCreate int
			for name, m := range models {
				in += m.InputTokens
				out += m.OutputTokens
				cacheRead += m.CacheReadInputTokens
				cacheCreate += m.CacheCreationInputTokens
				if u.Model == "" {
					u.Model = name
				}
			}
			if in > u.InputTokens {
				u.InputTokens = in
			}
			if out > u.OutputTokens {
				u.OutputTokens = out
			}
			if cacheRead > u.CachedInputTokens {
				u.CachedInputTokens = cacheRead
			}
			if cacheCreate > u.CacheCreationInputTokens {
				u.CacheCreationInputTokens = cacheCreate
			}
			if u.Model != "" {
				u.ModelSource = codingagent.ModelSourceAgent
			}
		}
	}
	if u.InputTokens == 0 && u.OutputTokens == 0 && u.CachedInputTokens == 0 &&
		u.CacheCreationInputTokens == 0 && u.TotalCostUSD == nil {
		return nil
	}
	return u
}
