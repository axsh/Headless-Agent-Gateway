package llmgateway

import (
	"bytes"
	"encoding/json"
	"fmt"
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

// TryFallbackOpenAIResponse intercepts and rewrites an OpenAI JSON response to extract text tool calls.
func TryFallbackOpenAIResponse(body []byte) ([]byte, bool) {
	var resp struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		Model   string `json:"model"`
		Choices []struct {
			Index   int `json:"index"`
			Message struct {
				Role      string `json:"role"`
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls,omitempty"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage any `json:"usage,omitempty"`
	}

	if err := json.Unmarshal(body, &resp); err != nil {
		return body, false
	}

	if len(resp.Choices) == 0 {
		return body, false
	}

	// If there are already tool calls, don't fallback.
	if len(resp.Choices[0].Message.ToolCalls) > 0 {
		return body, false
	}

	call, ok := ExtractToolCallFromText(resp.Choices[0].Message.Content)
	if !ok {
		return body, false
	}

	// Serialize arguments to string
	argsBytes, err := json.Marshal(call.Arguments)
	if err != nil {
		return body, false
	}

	// Rewrite choices[0]
	resp.Choices[0].Message.ToolCalls = []struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	}{
		{
			ID:   "call_fallback_0",
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{
				Name:      call.Name,
				Arguments: string(argsBytes),
			},
		},
	}
	resp.Choices[0].Message.Content = ""
	resp.Choices[0].FinishReason = "tool_calls"

	rewritten, err := json.Marshal(resp)
	if err != nil {
		return body, false
	}
	return rewritten, true
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

// rewriteModelField replaces the "model" value in the JSON body.
func rewriteModelField(body []byte, oldModel, newModel string) []byte {
	old := fmt.Sprintf(`"model":"%s"`, oldModel)
	new := fmt.Sprintf(`"model":"%s"`, newModel)
	result := bytes.Replace(body, []byte(old), []byte(new), 1)
	// Also handle space variations: "model": "value"
	old = fmt.Sprintf(`"model": "%s"`, oldModel)
	new = fmt.Sprintf(`"model": "%s"`, newModel)
	result = bytes.Replace(result, []byte(old), []byte(new), 1)
	return result
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
