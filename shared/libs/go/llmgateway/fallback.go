package llmgateway

import (
	"encoding/json"
	"regexp"
	"strings"
)

// ExtractedToolCall represents a parsed tool call from raw text.
type ExtractedToolCall struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// ExtractToolCallFromText attempts to find and parse a JSON tool call block or XML tool call block in the text.
// It returns the extracted tool call info and true if successful.
func ExtractToolCallFromText(text string) (*ExtractedToolCall, bool) {
	// Try parsing XML style first: <tool_call>...</tool_call>
	xmlRegex := regexp.MustCompile(`(?s)<tool_call>(.*?)</tool_call>`)
	if matches := xmlRegex.FindStringSubmatch(text); len(matches) > 1 {
		var call ExtractedToolCall
		if err := json.Unmarshal([]byte(strings.TrimSpace(matches[1])), &call); err == nil && call.Name != "" {
			return &call, true
		}
	}

	// Try finding the first JSON object block: { ... }
	// We look for a JSON block that contains "name" and "arguments" fields.
	jsonRegex := regexp.MustCompile(`(?s)\{.*\}`)
	if match := jsonRegex.FindString(text); match != "" {
		var call ExtractedToolCall
		if err := json.Unmarshal([]byte(strings.TrimSpace(match)), &call); err == nil && call.Name != "" {
			return &call, true
		}
	}

	return nil, false
}

// TryFallbackAnthropicResponse intercepts and rewrites an Anthropic JSON response to extract text tool calls.
func TryFallbackAnthropicResponse(body []byte) ([]byte, bool) {
	var resp struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Role    string `json:"role"`
		Content []struct {
			Type  string `json:"type"`
			Text  string `json:"text,omitempty"`
			ID    string `json:"id,omitempty"`
			Name  string `json:"name,omitempty"`
			Input any    `json:"input,omitempty"`
		} `json:"content"`
		Model        string `json:"model"`
		StopReason   string `json:"stop_reason"`
		StopSequence any    `json:"stop_sequence,omitempty"`
		Usage        any    `json:"usage,omitempty"`
	}

	if err := json.Unmarshal(body, &resp); err != nil {
		return body, false
	}

	if len(resp.Content) == 0 {
		return body, false
	}

	// Check if any existing block is a tool use
	for _, block := range resp.Content {
		if block.Type == "tool_use" {
			return body, false
		}
	}

	// Extract from text block
	var text string
	for _, block := range resp.Content {
		if block.Type == "text" {
			text = block.Text
			break
		}
	}

	call, ok := ExtractToolCallFromText(text)
	if !ok {
		return body, false
	}

	// Rewrite content
	resp.Content = []struct {
		Type  string `json:"type"`
		Text  string `json:"text,omitempty"`
		ID    string `json:"id,omitempty"`
		Name  string `json:"name,omitempty"`
		Input any    `json:"input,omitempty"`
	}{
		{
			Type:  "tool_use",
			ID:    "toolu_fallback_0",
			Name:  call.Name,
			Input: call.Arguments,
		},
	}
	resp.StopReason = "tool_use"

	rewritten, err := json.Marshal(resp)
	if err != nil {
		return body, false
	}
	return rewritten, true
}

// ExtractSessionID extracts the session ID from an x-api-key or Authorization header value.
// The format is: "key;sid=SESSION_ID" or "Bearer key;sid=SESSION_ID" or "key;fallback=true;sid=SESSION_ID".
func ExtractSessionID(authHeader string) string {
	if strings.HasPrefix(authHeader, "Bearer ") {
		authHeader = strings.TrimPrefix(authHeader, "Bearer ")
	}
	for _, part := range strings.Split(authHeader, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "sid=") {
			return strings.TrimPrefix(part, "sid=")
		}
	}
	return ""
}

// ExtractFallbackFlag extracts the fallback flag from an x-api-key or Authorization header value.
// The format is: "key;fallback=true;sid=SESSION_ID".
// Returns true if fallback=true is found.
func ExtractFallbackFlag(authHeader string) bool {
	if strings.HasPrefix(authHeader, "Bearer ") {
		authHeader = strings.TrimPrefix(authHeader, "Bearer ")
	}
	for _, part := range strings.Split(authHeader, ";") {
		part = strings.TrimSpace(part)
		if part == "fallback=true" {
			return true
		}
	}
	return false
}
