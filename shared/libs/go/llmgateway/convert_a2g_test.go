package llmgateway

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestConvertSchemaTypesToUppercase(t *testing.T) {
	inputSchema := `{
		"type": "object",
		"properties": {
			"query": {
				"type": "string",
				"description": "The search query"
			},
			"items": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"id": {
							"type": "integer"
						}
					}
				}
			}
		},
		"required": ["query"]
	}`

	var schema interface{}
	if err := json.Unmarshal([]byte(inputSchema), &schema); err != nil {
		t.Fatalf("failed to unmarshal input schema: %v", err)
	}

	converted := convertSchemaTypesToUppercase(schema)
	marshaled, err := json.Marshal(converted)
	if err != nil {
		t.Fatalf("failed to marshal converted schema: %v", err)
	}

	res := string(marshaled)
	expected := []string{
		`"type":"OBJECT"`,
		`"type":"STRING"`,
		`"type":"ARRAY"`,
		`"type":"INTEGER"`,
	}

	for _, exp := range expected {
		if !strings.Contains(res, exp) {
			t.Errorf("converted schema does not contain %q, result: %s", exp, res)
		}
	}
}

func TestConvertAnthropicRequestToGemini_BasicText(t *testing.T) {
	input := `{
		"model": "gemini-3.5-flash",
		"messages": [{"role": "user", "content": "hello world"}],
		"max_tokens": 1024,
		"temperature": 0.7
	}`

	result, err := ConvertAnthropicRequestToGemini([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var req GeminiRequest
	if err := json.Unmarshal(result, &req); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if len(req.Contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(req.Contents))
	}
	if req.Contents[0].Role != "user" {
		t.Errorf("expected role 'user', got %q", req.Contents[0].Role)
	}
	if len(req.Contents[0].Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(req.Contents[0].Parts))
	}
	if req.Contents[0].Parts[0].Text != "hello world" {
		t.Errorf("expected text 'hello world', got %q", req.Contents[0].Parts[0].Text)
	}
	if req.GenerationConfig == nil || *req.GenerationConfig.MaxOutputTokens != 1024 {
		t.Errorf("expected maxOutputTokens 1024, got %v", req.GenerationConfig)
	}
	if req.GenerationConfig == nil || *req.GenerationConfig.Temperature != 0.7 {
		t.Errorf("expected temperature 0.7, got %v", req.GenerationConfig)
	}
}

func TestConvertAnthropicRequestToGemini_WithTools(t *testing.T) {
	input := `{
		"model": "gemini-3.5-flash",
		"messages": [{"role": "user", "content": "find files"}],
		"tools": [{
			"name": "Glob",
			"description": "Find files",
			"input_schema": {
				"type": "object",
				"properties": {
					"patterns": {
						"type": "string"
					}
				},
				"required": ["patterns"]
			}
		}]
	}`

	result, err := ConvertAnthropicRequestToGemini([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var req GeminiRequest
	if err := json.Unmarshal(result, &req); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if len(req.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(req.Tools))
	}
	tool := req.Tools[0]
	if len(tool.FunctionDeclarations) != 1 {
		t.Fatalf("expected 1 function declaration, got %d", len(tool.FunctionDeclarations))
	}
	fd := tool.FunctionDeclarations[0]
	if fd.Name != "Glob" {
		t.Errorf("expected name 'Glob', got %q", fd.Name)
	}
	if fd.Description != "Find files" {
		t.Errorf("expected description 'Find files', got %q", fd.Description)
	}

	paramsStr := string(fd.Parameters)
	if !strings.Contains(paramsStr, `"type":"OBJECT"`) {
		t.Errorf("parameters should contain type OBJECT, got %s", paramsStr)
	}
	if !strings.Contains(paramsStr, `"type":"STRING"`) {
		t.Errorf("parameters should contain type STRING, got %s", paramsStr)
	}
}

func TestConvertGeminiResponseToAnthropic_Text(t *testing.T) {
	respBody := `{
		"candidates": [{
			"content": {
				"role": "model",
				"parts": [{"text": "Hello from Gemini"}]
			},
			"finishReason": "STOP"
		}],
		"usageMetadata": {
			"promptTokenCount": 10,
			"candidatesTokenCount": 5,
			"totalTokenCount": 15
		}
	}`

	result, err := ConvertGeminiResponseToAnthropic([]byte(respBody), "gemini-3.5-flash")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var resp AnthropicResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if resp.Model != "gemini-3.5-flash" {
		t.Errorf("expected model 'gemini-3.5-flash', got %q", resp.Model)
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("expected stop_reason 'end_turn', got %q", resp.StopReason)
	}
	if len(resp.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(resp.Content))
	}
	if resp.Content[0].Type != "text" || resp.Content[0].Text != "Hello from Gemini" {
		t.Errorf("invalid content: %v", resp.Content[0])
	}
	if resp.Usage.InputTokens != 10 || resp.Usage.OutputTokens != 5 {
		t.Errorf("invalid usage tokens: %v", resp.Usage)
	}
}

func TestConvertGeminiResponseToAnthropic_ToolCall(t *testing.T) {
	respBody := `{
		"candidates": [{
			"content": {
				"role": "model",
				"parts": [{
					"functionCall": {
						"name": "Write",
						"args": {"path": "hello.py", "content": "print('hello')"}
					}
				}]
			},
			"finishReason": "STOP"
		}]
	}`

	result, err := ConvertGeminiResponseToAnthropic([]byte(respBody), "gemini-3.5-flash")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var resp AnthropicResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if resp.StopReason != "tool_use" {
		t.Errorf("expected stop_reason 'tool_use', got %q", resp.StopReason)
	}
	if len(resp.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(resp.Content))
	}
	block := resp.Content[0]
	if block.Type != "tool_use" {
		t.Errorf("expected block type 'tool_use', got %q", block.Type)
	}
	if block.Name != "Write" {
		t.Errorf("expected tool name 'Write', got %q", block.Name)
	}
	argsStr := string(block.Input)
	if !strings.Contains(argsStr, `"hello.py"`) {
		t.Errorf("arguments do not contain hello.py: %s", argsStr)
	}
}

func TestConvertGeminiStreamToAnthropic_TextStream(t *testing.T) {
	events := strings.Join([]string{
		`data: {"candidates":[{"content":{"parts":[{"text":"Hello"}]}}]}`,
		``,
		`data: {"candidates":[{"content":{"parts":[{"text":" world!"}]}}]}`,
		``,
		`data: {"candidates":[{"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"totalTokenCount":15}}`,
		``,
	}, "\n")

	reader := strings.NewReader(events)
	var buf bytes.Buffer
	err := ConvertGeminiStreamToAnthropic(reader, &buf, "gemini-3.5-flash")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	if !strings.Contains(output, "event: message_start") {
		t.Error("expected message_start event")
	}
	if !strings.Contains(output, `"text":"Hello"`) {
		t.Error("expected 'Hello' text delta")
	}
	if !strings.Contains(output, `"text":" world!"`) {
		t.Error("expected ' world!' text delta")
	}
	if !strings.Contains(output, `"stop_reason":"end_turn"`) {
		t.Error("expected stop_reason end_turn")
	}
	if !strings.Contains(output, "event: message_stop") {
		t.Error("expected message_stop event")
	}
}

func TestConvertGeminiStreamToAnthropic_ToolCallStream(t *testing.T) {
	events := strings.Join([]string{
		`data: {"candidates":[{"content":{"parts":[{"functionCall":{"name":"Write","args":{"path":"hello.py"}}}]}}]}`,
		``,
		`data: {"candidates":[{"content":{"parts":[{"functionCall":{"name":"Write","args":{"content":"print('hello')"}}}]}}]}`,
		``,
		`data: {"candidates":[{"finishReason":"STOP"}]}`,
		``,
	}, "\n")

	reader := strings.NewReader(events)
	var buf bytes.Buffer
	err := ConvertGeminiStreamToAnthropic(reader, &buf, "gemini-3.5-flash")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	if !strings.Contains(output, `"type":"tool_use"`) {
		t.Error("expected tool_use block start")
	}
	if !strings.Contains(output, `"name":"Write"`) {
		t.Error("expected tool name 'Write'")
	}
	if !strings.Contains(output, `"stop_reason":"tool_use"`) {
		t.Error("expected stop_reason tool_use")
	}
}
