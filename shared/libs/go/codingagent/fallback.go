package codingagent

import (
	"encoding/json"
	"regexp"
	"strings"
)

// FallbackToolCall is a tool call parsed from text output.
type FallbackToolCall struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// ParseFallbackToolCalls extracts tool calls from text output.
// Supported formats:
//   - Single object: {"name": "Write", "arguments": {...}}
//   - Array: [{"name": "Write", ...}, ...]
//   - Markdown code fence: ```json\n{...}\n```
func ParseFallbackToolCalls(text string) ([]FallbackToolCall, bool) {
	cleaned := StripMarkdownCodeFence(text)
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return nil, false
	}

	// Try as array
	if strings.HasPrefix(cleaned, "[") {
		var calls []FallbackToolCall
		if err := json.Unmarshal([]byte(cleaned), &calls); err == nil {
			if len(calls) > 0 && calls[0].Name != "" {
				return calls, true
			}
		}
	}

	// Try as single object
	if strings.HasPrefix(cleaned, "{") {
		var call FallbackToolCall
		if err := json.Unmarshal([]byte(cleaned), &call); err == nil {
			if call.Name != "" {
				return []FallbackToolCall{call}, true
			}
		}
	}

	return nil, false
}

var markdownFenceRe = regexp.MustCompile(`(?s)` + "```" + `(?:json|\w*)?\n?(.*?)\n?` + "```")

// StripMarkdownCodeFence removes markdown code fences and returns the inner content.
// If no code fence is found, the original text is returned unchanged.
func StripMarkdownCodeFence(text string) string {
	matches := markdownFenceRe.FindStringSubmatch(text)
	if len(matches) >= 2 {
		return matches[1]
	}
	return text
}
