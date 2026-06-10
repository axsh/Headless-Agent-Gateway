package llmgateway

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// OpenAIStreamChunk represents a single chunk in OpenAI's streaming response.
type OpenAIStreamChunk struct {
	ID      string               `json:"id"`
	Choices []OpenAIStreamChoice `json:"choices"`
	Usage   *OpenAIUsage         `json:"usage,omitempty"`
}

// OpenAIStreamChoice represents a choice in a streaming chunk.
type OpenAIStreamChoice struct {
	Delta        OpenAIStreamDelta `json:"delta"`
	FinishReason *string           `json:"finish_reason"`
}

// OpenAIStreamDelta represents the delta content in a streaming chunk.
type OpenAIStreamDelta struct {
	Role      string                 `json:"role,omitempty"`
	Content   string                 `json:"content,omitempty"`
	ToolCalls []OpenAIStreamToolCall `json:"tool_calls,omitempty"`
}

// OpenAIStreamToolCall represents a tool call delta in streaming.
type OpenAIStreamToolCall struct {
	Index    int                  `json:"index"`
	ID       string               `json:"id,omitempty"`
	Type     string               `json:"type,omitempty"`
	Function OpenAIStreamToolFunc `json:"function"`
}

// OpenAIStreamToolFunc represents the function part of a streaming tool call.
type OpenAIStreamToolFunc struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// ConvertOpenAIStreamToAnthropic reads OpenAI SSE chunks from reader,
// converts them to Anthropic SSE events, and writes them to the ResponseWriter.
// It flushes after each event for real-time streaming.
func ConvertOpenAIStreamToAnthropic(
	reader io.Reader,
	w http.ResponseWriter,
	model string,
) error {
	flusher, _ := w.(http.Flusher)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // 1MB max to handle large SSE events

	var (
		messageStarted   bool
		textBlockStarted bool
		toolBlockStarted bool
		blockIndex       int
		finishReason     string
		usage            *OpenAIUsage
		msgID            string
	)

	flush := func() {
		if flusher != nil {
			flusher.Flush()
		}
	}

	writeEvent := func(event string, data any) {
		jsonData, _ := json.Marshal(data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, string(jsonData))
		flush()
	}

	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		payload := strings.TrimPrefix(line, "data: ")

		// Handle [DONE] signal
		if payload == "[DONE]" {
			// Ensure message_start was sent
			if !messageStarted {
				writeEvent("message_start", map[string]any{
					"type": "message_start",
					"message": map[string]any{
						"id":      "msg-converted",
						"type":    "message",
						"role":    "assistant",
						"model":   model,
						"content": []any{},
						"usage":   map[string]int{"input_tokens": 0, "output_tokens": 0},
					},
				})
			}

			// Close any open blocks
			if textBlockStarted {
				writeEvent("content_block_stop", map[string]any{
					"type":  "content_block_stop",
					"index": blockIndex,
				})
			}
			if toolBlockStarted {
				writeEvent("content_block_stop", map[string]any{
					"type":  "content_block_stop",
					"index": blockIndex,
				})
			}

			// Send message_delta with stop_reason
			stopReason := "end_turn"
			if finishReason != "" {
				stopReason = mapFinishReason(finishReason)
			}
			// R3: Defense - force tool_use if tool blocks were detected.
			// Some models return finish_reason="stop" even when they produced tool_calls.
			if toolBlockStarted && stopReason != "tool_use" {
				stopReason = "tool_use"
			}
			deltaData := map[string]any{
				"type":  "message_delta",
				"delta": map[string]any{"stop_reason": stopReason},
			}
			if usage != nil {
				deltaData["usage"] = map[string]int{
					"output_tokens": usage.CompletionTokens,
				}
			}
			writeEvent("message_delta", deltaData)

			// Send message_stop
			writeEvent("message_stop", map[string]any{"type": "message_stop"})
			return nil
		}

		// Parse chunk
		var chunk OpenAIStreamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue // Skip malformed chunks
		}

		if chunk.ID != "" {
			msgID = chunk.ID
		}
		if chunk.Usage != nil {
			usage = chunk.Usage
		}

		if len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]

		// Send message_start on first chunk with role
		if !messageStarted && choice.Delta.Role != "" {
			messageStarted = true
			usageData := map[string]int{"input_tokens": 0, "output_tokens": 0}
			writeEvent("message_start", map[string]any{
				"type": "message_start",
				"message": map[string]any{
					"id":      msgID,
					"type":    "message",
					"role":    "assistant",
					"model":   model,
					"content": []any{},
					"usage":   usageData,
				},
			})
		}

		// Handle text content delta
		if choice.Delta.Content != "" {
			if !textBlockStarted {
				textBlockStarted = true
				writeEvent("content_block_start", map[string]any{
					"type":          "content_block_start",
					"index":         blockIndex,
					"content_block": map[string]any{"type": "text", "text": ""},
				})
			}
			writeEvent("content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": blockIndex,
				"delta": map[string]any{"type": "text_delta", "text": choice.Delta.Content},
			})
		}

		// Handle tool calls delta
		for _, tc := range choice.Delta.ToolCalls {
			if tc.ID != "" {
				// New tool call - close previous block
				if textBlockStarted {
					writeEvent("content_block_stop", map[string]any{
						"type":  "content_block_stop",
						"index": blockIndex,
					})
					textBlockStarted = false
					blockIndex++
				}
				if toolBlockStarted {
					writeEvent("content_block_stop", map[string]any{
						"type":  "content_block_stop",
						"index": blockIndex,
					})
					blockIndex++
				}
				toolBlockStarted = true
				writeEvent("content_block_start", map[string]any{
					"type":  "content_block_start",
					"index": blockIndex,
					"content_block": map[string]any{
						"type": "tool_use",
						"id":   tc.ID,
						"name": tc.Function.Name,
					},
				})
			}
			if tc.Function.Arguments != "" {
				writeEvent("content_block_delta", map[string]any{
					"type":  "content_block_delta",
					"index": blockIndex,
					"delta": map[string]any{
						"type":         "input_json_delta",
						"partial_json": tc.Function.Arguments,
					},
				})
			}
		}

		// Capture finish_reason
		if choice.FinishReason != nil {
			finishReason = *choice.FinishReason
		}
	}

	return scanner.Err()
}
