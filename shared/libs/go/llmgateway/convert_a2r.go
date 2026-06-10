package llmgateway

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/axsh/hag/logger"
)

// --- Responses API Request Types ---

// ResponsesRequest represents an OpenAI Responses API request body.
type ResponsesRequest struct {
	Model           string          `json:"model"`
	Input           []ResponsesInput `json:"input"`
	MaxOutputTokens *int            `json:"max_output_tokens,omitempty"`
	Temperature     *float64        `json:"temperature,omitempty"`
	Stream          *bool           `json:"stream,omitempty"`
	Tools           []ResponsesTool `json:"tools,omitempty"`
}

// ResponsesInput represents an input message for Responses API.
// For regular messages: Role + Content are set.
// For function_call_output: Type + CallID + Output are set.
// For function_call (from assistant tool_use): Type + CallID + Name + Arguments are set.
type ResponsesInput struct {
	Role      string `json:"role,omitempty"`
	Content   string `json:"content,omitempty"`
	Type      string `json:"type,omitempty"`
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	Output    string `json:"output,omitempty"`
}

// ResponsesTool represents a tool definition for Responses API.
type ResponsesTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

// --- Responses API Response Types ---

// ResponsesResponse represents an OpenAI Responses API response.
type ResponsesResponse struct {
	ID     string            `json:"id"`
	Status string            `json:"status"`
	Output []ResponsesOutput `json:"output"`
	Usage  *ResponsesUsage   `json:"usage,omitempty"`
}

// ResponsesOutput represents an output item in Responses API.
type ResponsesOutput struct {
	Type      string                  `json:"type"`
	Content   []ResponsesContentBlock `json:"content,omitempty"`
	CallID    string                  `json:"call_id,omitempty"`
	Name      string                  `json:"name,omitempty"`
	Arguments string                  `json:"arguments,omitempty"`
}

// ResponsesContentBlock represents a content block in Responses API output.
type ResponsesContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ResponsesUsage represents usage stats in Responses API.
type ResponsesUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// --- Responses API Streaming Types ---

// responsesStreamEvent represents a parsed SSE event from Responses API.
type responsesStreamEvent struct {
	Type        string          `json:"type"`
	Delta       string          `json:"delta,omitempty"`
	OutputIndex int             `json:"output_index,omitempty"`
	ContentIndex int            `json:"content_index,omitempty"`
	Item        json.RawMessage `json:"item,omitempty"`
	Response    json.RawMessage `json:"response,omitempty"`
	Arguments   string          `json:"arguments,omitempty"`
}

// responsesOutputItem represents an item in response.output_item.added event.
type responsesOutputItem struct {
	Type   string `json:"type"`
	CallID string `json:"call_id,omitempty"`
	Name   string `json:"name,omitempty"`
}

// --- Request Conversion: Anthropic -> Responses API ---

// ConvertAnthropicRequestToResponses converts an Anthropic Messages API request body
// to an OpenAI Responses API request body.
func ConvertAnthropicRequestToResponses(body []byte, logs ...logger.Logger) ([]byte, error) {
	var anthReq AnthropicFullRequest
	if err := json.Unmarshal(body, &anthReq); err != nil {
		return nil, fmt.Errorf("unmarshal anthropic request: %w", err)
	}

	var log logger.Logger
	if len(logs) > 0 {
		log = logs[0]
	}
	if log != nil {
		log.Debug("converting anthropic to responses",
			"model", anthReq.Model,
			"msg_count", len(anthReq.Messages),
			"tool_count", len(anthReq.Tools))
	}

	respReq := ResponsesRequest{
		Model:       anthReq.Model,
		Temperature: anthReq.Temperature,
		Stream:      anthReq.Stream,
	}

	// Convert max_tokens with clamping.
	if anthReq.MaxTokens > 0 {
		mt := anthReq.MaxTokens
		if mt > openAIMaxCompletionTokens {
			mt = openAIMaxCompletionTokens
		}
		respReq.MaxOutputTokens = &mt
	}

	// Convert system to developer role input.
	if len(anthReq.System) > 0 {
		systemText, _ := extractText(anthReq.System)
		if systemText != "" {
			respReq.Input = append(respReq.Input, ResponsesInput{
				Role:    "developer",
				Content: systemText,
			})
		}
	}

	// Convert messages.
	for _, msg := range anthReq.Messages {
		inputs, err := convertAnthropicMsgToResponsesInput(msg)
		if err != nil {
			return nil, fmt.Errorf("convert message: %w", err)
		}
		respReq.Input = append(respReq.Input, inputs...)
	}

	// Convert tools.
	for _, tool := range anthReq.Tools {
		respReq.Tools = append(respReq.Tools, ResponsesTool{
			Type:        "function",
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  tool.InputSchema,
		})
	}

	marshaled, err := json.Marshal(respReq)
	if err != nil {
		return nil, err
	}
	if log != nil {
		bodyStr := string(marshaled)
		if len(bodyStr) > 10240 {
			bodyStr = bodyStr[:10240] + "..."
		}
		log.Trace("converted responses request", "body", bodyStr)

		if len(respReq.Tools) > 0 {
			var toolNames []string
			for _, t := range respReq.Tools {
				toolNames = append(toolNames, t.Name)
			}
			log.Debug("converted responses request tools summary",
				"tool_count", len(respReq.Tools),
				"tool_names", strings.Join(toolNames, ", "))

			for _, t := range respReq.Tools {
				log.Trace("converted tool detail", "name", t.Name, "schema", string(t.Parameters))
			}
		}
	}
	return marshaled, nil
}

// convertAnthropicMsgToResponsesInput converts a single Anthropic message
// to one or more Responses API input entries.
func convertAnthropicMsgToResponsesInput(msg AnthropicMsg) ([]ResponsesInput, error) {
	role := msg.Role

	// Map Anthropic roles to Responses API roles.
	respRole := role
	if role == "assistant" {
		respRole = "assistant"
	}

	// Try parsing content as a string.
	var contentStr string
	if err := json.Unmarshal(msg.Content, &contentStr); err == nil {
		return []ResponsesInput{{Role: respRole, Content: contentStr}}, nil
	}

	// Parse content as array of blocks.
	var blocks []ContentBlock
	if err := json.Unmarshal(msg.Content, &blocks); err != nil {
		return nil, fmt.Errorf("unmarshal content blocks: %w", err)
	}

	var inputs []ResponsesInput
	var textParts []string

	for _, block := range blocks {
		switch block.Type {
		case "text":
			textParts = append(textParts, block.Text)
		case "tool_use":
			// Flush accumulated text first.
			if len(textParts) > 0 {
				inputs = append(inputs, ResponsesInput{
					Role:    respRole,
					Content: strings.Join(textParts, ""),
				})
				textParts = nil
			}
			// Convert tool_use to function_call input.
			argsStr := ""
			if len(block.Input) > 0 {
				argsStr = string(block.Input)
			}
			inputs = append(inputs, ResponsesInput{
				Type:      "function_call",
				CallID:    block.ID,
				Name:      block.Name,
				Arguments: argsStr,
			})
		case "tool_result":
			// Flush accumulated text first.
			if len(textParts) > 0 {
				inputs = append(inputs, ResponsesInput{
					Role:    respRole,
					Content: strings.Join(textParts, ""),
				})
				textParts = nil
			}
			// Convert tool_result to function_call_output.
			inputs = append(inputs, ResponsesInput{
				Type:   "function_call_output",
				CallID: block.ToolUseID,
				Output: block.Content,
			})
		}
	}

	// Flush remaining text.
	if len(textParts) > 0 {
		inputs = append(inputs, ResponsesInput{
			Role:    respRole,
			Content: strings.Join(textParts, ""),
		})
	}

	// If no blocks were processed, add an empty message.
	if len(inputs) == 0 {
		inputs = append(inputs, ResponsesInput{Role: respRole, Content: ""})
	}

	return inputs, nil
}

// --- Response Conversion: Responses API -> Anthropic ---

// ConvertResponsesResponseToAnthropic converts an OpenAI Responses API response body
// to an Anthropic Messages API response body.
func ConvertResponsesResponseToAnthropic(body []byte, model string, logs ...logger.Logger) ([]byte, error) {
	var respResp ResponsesResponse
	if err := json.Unmarshal(body, &respResp); err != nil {
		return nil, fmt.Errorf("unmarshal responses response: %w", err)
	}

	var log logger.Logger
	if len(logs) > 0 {
		log = logs[0]
	}
	if log != nil {
		log.Debug("converting responses to anthropic", "model", model)
	}

	anthResp := AnthropicResponse{
		ID:    respResp.ID,
		Type:  "message",
		Role:  "assistant",
		Model: model,
	}

	hasToolUse := false

	for _, out := range respResp.Output {
		switch out.Type {
		case "message":
			for _, block := range out.Content {
				if block.Type == "output_text" {
					anthResp.Content = append(anthResp.Content, ContentBlock{
						Type: "text",
						Text: block.Text,
					})
				}
			}
		case "function_call":
			hasToolUse = true
			inputRaw := json.RawMessage(out.Arguments)
			anthResp.Content = append(anthResp.Content, ContentBlock{
				Type:  "tool_use",
				ID:    out.CallID,
				Name:  out.Name,
				Input: inputRaw,
			})
		}
	}

	// Determine stop reason.
	if hasToolUse {
		anthResp.StopReason = "tool_use"
	} else {
		anthResp.StopReason = "end_turn"
	}

	// Convert usage.
	if respResp.Usage != nil {
		anthResp.Usage = AnthropicUsage{
			InputTokens:  respResp.Usage.InputTokens,
			OutputTokens: respResp.Usage.OutputTokens,
		}
	}

	marshaled, err := json.Marshal(anthResp)
	if err != nil {
		return nil, err
	}
	if log != nil {
		bodyStr := string(marshaled)
		if len(bodyStr) > 10240 {
			bodyStr = bodyStr[:10240] + "..."
		}
		log.Trace("converted anthropic response", "body", bodyStr)
	}
	return marshaled, nil
}

// --- Streaming Conversion: Responses API SSE -> Anthropic SSE ---

// ConvertResponsesStreamToAnthropic reads OpenAI Responses API SSE events from reader
// and writes Anthropic Messages API SSE events to writer.
func ConvertResponsesStreamToAnthropic(reader io.Reader, writer io.Writer, model string, logs ...logger.Logger) error {
	var log logger.Logger
	if len(logs) > 0 {
		log = logs[0]
	}
	if log != nil {
		log.Debug("starting SSE stream conversion", "direction", "responses->anthropic", "model", model)
	}

	br := bufio.NewReader(reader)
	flusher, hasFlusher := writer.(http.Flusher)

	messageSent := false
	contentBlockIndex := 0
	hadFunctionCall := false
	eventsCount := 0
	var eventType string

	writeSSE := func(event, data string) {
		fmt.Fprintf(writer, "event: %s\ndata: %s\n\n", event, data)
		if hasFlusher {
			flusher.Flush()
		}
	}

	sendMessageStart := func(respID string) {
		if messageSent {
			return
		}
		messageSent = true
		data := fmt.Sprintf(`{"type":"message_start","message":{"id":"%s","type":"message","role":"assistant","model":"%s","content":[],"stop_reason":null,"usage":{"input_tokens":0,"output_tokens":0}}}`, respID, model)
		writeSSE("message_start", data)
	}

	for {
		line, err := br.ReadString('\n')
		if err != nil && err != io.EOF {
			return err
		}
		if err == io.EOF && line == "" {
			break
		}

		line = strings.TrimRight(line, "\r\n")

		if log != nil {
			log.Trace("SSE event", "event_data", line)
		}
		eventsCount++

		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
			if err == io.EOF {
				break
			}
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			if err == io.EOF {
				break
			}
			continue
		}

		dataStr := strings.TrimPrefix(line, "data: ")

		var evt responsesStreamEvent
		if jsonErr := json.Unmarshal([]byte(dataStr), &evt); jsonErr != nil {
			if log != nil {
				log.Warn("SSE event parse warning", "line", dataStr, "error", jsonErr.Error())
			}
			if err == io.EOF {
				break
			}
			continue
		}

		switch eventType {
		case "response.created":
			// Extract response ID from the response field.
			var createdResp struct {
				ID string `json:"id"`
			}
			if len(evt.Response) > 0 {
				json.Unmarshal(evt.Response, &createdResp)
			}
			sendMessageStart(createdResp.ID)

		case "response.output_item.added":
			// Parse the item to check type.
			var item responsesOutputItem
			if len(evt.Item) > 0 {
				json.Unmarshal(evt.Item, &item)
			}
			if item.Type == "function_call" {
				hadFunctionCall = true
				sendMessageStart("")
				data := fmt.Sprintf(`{"type":"content_block_start","index":%d,"content_block":{"type":"tool_use","id":"%s","name":"%s","input":{}}}`,
					contentBlockIndex, item.CallID, item.Name)
				writeSSE("content_block_start", data)
			}

		case "response.content_part.added":
			// Start a new text content block.
			sendMessageStart("")
			data := fmt.Sprintf(`{"type":"content_block_start","index":%d,"content_block":{"type":"text","text":""}}`, contentBlockIndex)
			writeSSE("content_block_start", data)

		case "response.output_text.delta":
			// Text content delta.
			deltaJSON, _ := json.Marshal(evt.Delta)
			data := fmt.Sprintf(`{"type":"content_block_delta","index":%d,"delta":{"type":"text_delta","text":%s}}`,
				contentBlockIndex, string(deltaJSON))
			writeSSE("content_block_delta", data)

		case "response.function_call_arguments.delta":
			// Tool call arguments delta.
			deltaJSON, _ := json.Marshal(evt.Delta)
			data := fmt.Sprintf(`{"type":"content_block_delta","index":%d,"delta":{"type":"input_json_delta","partial_json":%s}}`,
				contentBlockIndex, string(deltaJSON))
			writeSSE("content_block_delta", data)

		case "response.function_call_arguments.done":
			// End of function call arguments - close the content block.
			data := fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, contentBlockIndex)
			writeSSE("content_block_stop", data)
			contentBlockIndex++

		case "response.output_text.done":
			// End of text content - close the content block.
			data := fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, contentBlockIndex)
			writeSSE("content_block_stop", data)
			contentBlockIndex++

		case "response.completed":
			// Determine stop reason based on whether function_call events were seen.
			stopReason := "end_turn"
			if hadFunctionCall {
				stopReason = "tool_use"
			}
			data := fmt.Sprintf(`{"type":"message_delta","delta":{"stop_reason":"%s"},"usage":{"output_tokens":0}}`, stopReason)
			writeSSE("message_delta", data)
			writeSSE("message_stop", `{"type":"message_stop"}`)
		}

		if err == io.EOF {
			break
		}
	}

	if log != nil {
		log.Debug("SSE stream conversion completed", "events_count", eventsCount)
	}
	return nil
}
