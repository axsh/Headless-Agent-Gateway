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

// --- Google Gemini API Types ---

type GeminiRequest struct {
	Contents          []GeminiContent          `json:"contents"`
	SystemInstruction *GeminiContent           `json:"systemInstruction,omitempty"`
	GenerationConfig  *GeminiGenerationConfig  `json:"generationConfig,omitempty"`
	Tools             []GeminiTool             `json:"tools,omitempty"`
}

type GeminiContent struct {
	Role  string       `json:"role,omitempty"` // "user" or "model"
	Parts []GeminiPart `json:"parts"`
}

type GeminiPart struct {
	Text             string                  `json:"text,omitempty"`
	FunctionCall     *GeminiFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *GeminiFunctionResponse `json:"functionResponse,omitempty"`
	ThoughtSignature string                  `json:"thought_signature,omitempty"`
}

type GeminiFunctionCall struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

type GeminiFunctionResponse struct {
	Name     string          `json:"name"`
	Response json.RawMessage `json:"response"`
}

type GeminiTool struct {
	FunctionDeclarations []GeminiFunctionDeclaration `json:"functionDeclarations"`
}

type GeminiFunctionDeclaration struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type GeminiGenerationConfig struct {
	Temperature     *float64 `json:"temperature,omitempty"`
	MaxOutputTokens *int     `json:"maxOutputTokens,omitempty"`
}

type GeminiResponse struct {
	Candidates    []GeminiCandidate    `json:"candidates"`
	UsageMetadata *GeminiUsageMetadata `json:"usageMetadata,omitempty"`
}

type GeminiCandidate struct {
	Content      GeminiContent `json:"content"`
	FinishReason string        `json:"finishReason,omitempty"` // e.g. "STOP"
}

type GeminiUsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

// cleanseGeminiSchema recursively removes unsupported JSON Schema properties
// (like "$schema", "additionalProperties", "const", "exclusiveMinimum", "propertyNames")
// for Gemini compatibility.
func cleanseGeminiSchema(val interface{}) interface{} {
	switch v := val.(type) {
	case map[string]interface{}:
		newMap := make(map[string]interface{})
		for k, v2 := range v {
			// Skip unsupported keys
			if k == "$schema" || k == "additionalProperties" || k == "const" || k == "exclusiveMinimum" || k == "propertyNames" {
				continue
			}
			newMap[k] = cleanseGeminiSchema(v2)
		}
		return newMap
	case []interface{}:
		newSlice := make([]interface{}, len(v))
		for i, v2 := range v {
			newSlice[i] = cleanseGeminiSchema(v2)
		}
		return newSlice
	default:
		return val
	}
}

// convertSchemaTypesToUppercase recursively traverses the schema map or array
// and converts all schema "type" values to uppercase.
func convertSchemaTypesToUppercase(val interface{}) interface{} {
	switch v := val.(type) {
	case map[string]interface{}:
		newMap := make(map[string]interface{})
		for k, v2 := range v {
			if k == "type" {
				if strType, ok := v2.(string); ok {
					newMap[k] = strings.ToUpper(strType)
					continue
				}
			}
			newMap[k] = convertSchemaTypesToUppercase(v2)
		}
		return newMap
	case []interface{}:
		newSlice := make([]interface{}, len(v))
		for i, v2 := range v {
			newSlice[i] = convertSchemaTypesToUppercase(v2)
		}
		return newSlice
	default:
		return val
	}
}

// ConvertAnthropicRequestToGemini converts Anthropic request body to Google Gemini API request body.
func ConvertAnthropicRequestToGemini(body []byte, logs ...logger.Logger) ([]byte, error) {
	var anthReq AnthropicFullRequest
	if err := json.Unmarshal(body, &anthReq); err != nil {
		return nil, fmt.Errorf("unmarshal anthropic request: %w", err)
	}

	var log logger.Logger
	if len(logs) > 0 {
		log = logs[0]
	}
	if log != nil {
		log.Debug("converting anthropic to gemini",
			"model", anthReq.Model,
			"msg_count", len(anthReq.Messages),
			"tool_count", len(anthReq.Tools))
	}

	geminiReq := GeminiRequest{}

	// Convert system prompt to systemInstruction.
	if len(anthReq.System) > 0 {
		var systemText string
		// Try parsing as simple string first.
		if err := json.Unmarshal(anthReq.System, &systemText); err != nil {
			// Try parsing as array of ContentBlocks.
			var blocks []ContentBlock
			if err2 := json.Unmarshal(anthReq.System, &blocks); err2 == nil {
				var sb strings.Builder
				for _, b := range blocks {
					if b.Type == "text" {
						sb.WriteString(b.Text)
					}
				}
				systemText = sb.String()
			}
		}
		if systemText != "" {
			geminiReq.SystemInstruction = &GeminiContent{
				Parts: []GeminiPart{{Text: systemText}},
			}
		}
	}

	// Convert messages history.
	for _, msg := range anthReq.Messages {
		content, err := convertAnthropicMsgToGeminiContent(msg)
		if err != nil {
			return nil, fmt.Errorf("convert message: %w", err)
		}
		geminiReq.Contents = append(geminiReq.Contents, content)
	}

	// Convert tools.
	if len(anthReq.Tools) > 0 {
		var tool GeminiTool
		for _, t := range anthReq.Tools {
			var paramsRaw json.RawMessage
			if len(t.InputSchema) > 0 {
				var schema interface{}
				if err := json.Unmarshal(t.InputSchema, &schema); err == nil {
					uppercaseSchema := convertSchemaTypesToUppercase(schema)
					cleansedSchema := cleanseGeminiSchema(uppercaseSchema)
					if marshaled, err2 := json.Marshal(cleansedSchema); err2 == nil {
						paramsRaw = marshaled
					}
				}
			}

			tool.FunctionDeclarations = append(tool.FunctionDeclarations, GeminiFunctionDeclaration{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  paramsRaw,
			})
		}
		geminiReq.Tools = []GeminiTool{tool}
	}

	// Convert generation configurations.
	config := GeminiGenerationConfig{}
	if anthReq.MaxTokens > 0 {
		mt := anthReq.MaxTokens
		config.MaxOutputTokens = &mt
	}
	if anthReq.Temperature != nil {
		config.Temperature = anthReq.Temperature
	}
	geminiReq.GenerationConfig = &config

	marshaled, err := json.Marshal(geminiReq)
	if err != nil {
		return nil, err
	}

	if log != nil {
		bodyStr := string(marshaled)
		if len(bodyStr) > 10240 {
			bodyStr = bodyStr[:10240] + "..."
		}
		log.Trace("converted gemini request", "body", bodyStr)

		// R1 output summary for tools.
		if len(geminiReq.Tools) > 0 {
			var toolNames []string
			for _, fd := range geminiReq.Tools[0].FunctionDeclarations {
				toolNames = append(toolNames, fd.Name)
			}
			log.Debug("converted gemini request tools summary",
				"tool_count", len(toolNames),
				"tool_names", strings.Join(toolNames, ", "))
		}
	}

	return marshaled, nil
}

func convertAnthropicMsgToGeminiContent(msg AnthropicMsg) (GeminiContent, error) {
	role := "user"
	if msg.Role == "assistant" {
		role = "model"
	}

	content := GeminiContent{
		Role: role,
	}

	// Try parsing content as raw string.
	var contentStr string
	if err := json.Unmarshal(msg.Content, &contentStr); err == nil {
		content.Parts = []GeminiPart{{Text: contentStr}}
		return content, nil
	}

	// Try parsing content as ContentBlocks.
	var blocks []ContentBlock
	if err := json.Unmarshal(msg.Content, &blocks); err != nil {
		return content, fmt.Errorf("unmarshal content blocks: %w", err)
	}

	for _, block := range blocks {
		switch block.Type {
		case "text":
			if block.Text != "" {
				content.Parts = append(content.Parts, GeminiPart{Text: block.Text})
			}
		case "tool_use":
			argsStr := "{}"
			if len(block.Input) > 0 {
				argsStr = string(block.Input)
			}
			content.Parts = append(content.Parts, GeminiPart{
				FunctionCall: &GeminiFunctionCall{
					Name: block.Name,
					Args: json.RawMessage(argsStr),
				},
				ThoughtSignature: "skip_thought_signature_validator",
			})
		case "tool_result":
			funcName := block.ToolUseID
			funcName = strings.TrimPrefix(funcName, "call_gemini_")
			content.Parts = append(content.Parts, GeminiPart{
				FunctionResponse: &GeminiFunctionResponse{
					Name:     funcName,
					Response: json.RawMessage(fmt.Sprintf(`{"content": %q}`, block.Content)),
				},
			})
		}
	}

	return content, nil
}

// ConvertGeminiResponseToAnthropic converts Gemini API response body to Anthropic Messages response.
func ConvertGeminiResponseToAnthropic(body []byte, model string, logs ...logger.Logger) ([]byte, error) {
	var geminiResp GeminiResponse
	if err := json.Unmarshal(body, &geminiResp); err != nil {
		return nil, fmt.Errorf("unmarshal gemini response: %w", err)
	}

	var log logger.Logger
	if len(logs) > 0 {
		log = logs[0]
	}
	if log != nil {
		log.Debug("converting gemini to anthropic", "model", model)
	}

	anthResp := AnthropicResponse{
		ID:    "msg_gemini_" + fmt.Sprintf("%d", len(body)), // Generate pseudo ID
		Type:  "message",
		Role:  "assistant",
		Model: model,
	}

	hasToolUse := false

	if len(geminiResp.Candidates) > 0 {
		candidate := geminiResp.Candidates[0]
		for _, part := range candidate.Content.Parts {
			if part.Text != "" {
				anthResp.Content = append(anthResp.Content, ContentBlock{
					Type: "text",
					Text: part.Text,
				})
			}
			if part.FunctionCall != nil {
				hasToolUse = true
				anthResp.Content = append(anthResp.Content, ContentBlock{
					Type:  "tool_use",
					ID:    "call_gemini_" + part.FunctionCall.Name,
					Name:  part.FunctionCall.Name,
					Input: part.FunctionCall.Args,
				})
			}
		}
	}

	if hasToolUse {
		anthResp.StopReason = "tool_use"
	} else {
		anthResp.StopReason = "end_turn"
	}

	if geminiResp.UsageMetadata != nil {
		anthResp.Usage = AnthropicUsage{
			InputTokens:  geminiResp.UsageMetadata.PromptTokenCount,
			OutputTokens: geminiResp.UsageMetadata.CandidatesTokenCount,
		}
	}

	marshaled, err := json.Marshal(anthResp)
	if err != nil {
		return nil, err
	}
	return marshaled, nil
}

// ConvertGeminiStreamToAnthropic converts Google Gemini SSE stream to Anthropic Messages SSE events.
func ConvertGeminiStreamToAnthropic(reader io.Reader, writer io.Writer, model string, logs ...logger.Logger) error {
	var log logger.Logger
	if len(logs) > 0 {
		log = logs[0]
	}
	if log != nil {
		log.Debug("starting SSE stream conversion", "direction", "gemini->anthropic", "model", model)
	}

	br := bufio.NewReader(reader)
	flusher, hasFlusher := writer.(http.Flusher)

	messageSent := false
	hadFunctionCall := false
	eventsCount := 0

	writeSSE := func(event, data string) {
		fmt.Fprintf(writer, "event: %s\ndata: %s\n\n", event, data)
		if hasFlusher {
			flusher.Flush()
		}
	}

	sendMessageStart := func() {
		if messageSent {
			return
		}
		messageSent = true
		data := fmt.Sprintf(`{"type":"message_start","message":{"id":"msg_gemini_stream","type":"message","role":"assistant","model":"%s","content":[],"stop_reason":null,"usage":{"input_tokens":0,"output_tokens":0}}}`, model)
		writeSSE("message_start", data)
		writeSSE("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
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
		eventsCount++

		if !strings.HasPrefix(line, "data: ") {
			if err == io.EOF {
				break
			}
			continue
		}

		dataStr := strings.TrimPrefix(line, "data: ")
		if dataStr == "" {
			if err == io.EOF {
				break
			}
			continue
		}

		var geminiResp GeminiResponse
		if jsonErr := json.Unmarshal([]byte(dataStr), &geminiResp); jsonErr != nil {
			if log != nil {
				log.Warn("SSE event parse warning", "line", dataStr, "error", jsonErr.Error())
			}
			if err == io.EOF {
				break
			}
			continue
		}

		sendMessageStart()

		if len(geminiResp.Candidates) > 0 {
			candidate := geminiResp.Candidates[0]
			for _, part := range candidate.Content.Parts {
				if part.Text != "" {
					textJSON, _ := json.Marshal(part.Text)
					data := fmt.Sprintf(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":%s}}`, string(textJSON))
					writeSSE("content_block_delta", data)
				}
				if part.FunctionCall != nil {
					if !hadFunctionCall {
						hadFunctionCall = true
						// Close the previous text block first.
						writeSSE("content_block_stop", `{"type":"content_block_stop","index":0}`)
						// Start a tool use block at index 1.
						data := fmt.Sprintf(`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"call_gemini_%s","name":"%s","input":{}}}`,
							part.FunctionCall.Name, part.FunctionCall.Name)
						writeSSE("content_block_start", data)
					}
					// Output argument delta.
					argsJSON, _ := json.Marshal(string(part.FunctionCall.Args))
					data := fmt.Sprintf(`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":%s}}`, string(argsJSON))
					writeSSE("content_block_delta", data)
				}
			}
		}

		if err == io.EOF {
			break
		}
	}

	// Close final content blocks.
	if messageSent {
		if hadFunctionCall {
			writeSSE("content_block_stop", `{"type":"content_block_stop","index":1}`)
		} else {
			writeSSE("content_block_stop", `{"type":"content_block_stop","index":0}`)
		}

		stopReason := "end_turn"
		if hadFunctionCall {
			stopReason = "tool_use"
		}
		data := fmt.Sprintf(`{"type":"message_delta","delta":{"stop_reason":"%s"},"usage":{"output_tokens":0}}`, stopReason)
		writeSSE("message_delta", data)
		writeSSE("message_stop", `{"type":"message_stop"}`)
	}

	if log != nil {
		log.Debug("SSE stream conversion completed", "events_count", eventsCount)
	}
	return nil
}
