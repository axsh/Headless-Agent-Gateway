package wayfinder

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axsh/arctic-tern/wayfinder/session"
)

func TestAgentCore_SimpleResponse(t *testing.T) {
	mock := &MockLLMClient{
		Responses: []*LLMResponse{
			{Content: "Hello, I can help you!"},
		},
	}
	core := NewAgentCore(mock, &AgentConfig{
		WorkDir:      t.TempDir(),
		SessionDir:   t.TempDir(),
		LogicalModel: "test-model",
	}, nil)

	result, err := core.Run(context.Background(), "say hello")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if result != "Hello, I can help you!" {
		t.Errorf("result = %q, want %q", result, "Hello, I can help you!")
	}
	if mock.CallCount != 1 {
		t.Errorf("CallCount = %d, want 1", mock.CallCount)
	}
}

func TestAgentCore_SingleToolCall(t *testing.T) {
	workDir := t.TempDir()
	mock := &MockLLMClient{
		Responses: []*LLMResponse{
			// First response: tool call
			{
				ToolCalls: []ToolCall{
					{
						ID:   "call_1",
						Name: "read_file",
						Input: map[string]any{
							"path": "nonexistent.txt",
						},
					},
				},
			},
			// Second response: final answer after tool result
			{Content: "The file does not exist."},
		},
	}
	core := NewAgentCore(mock, &AgentConfig{
		WorkDir:      workDir,
		SessionDir:   t.TempDir(),
		LogicalModel: "test-model",
	}, nil)

	result, err := core.Run(context.Background(), "read a file")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if result != "The file does not exist." {
		t.Errorf("result = %q, want %q", result, "The file does not exist.")
	}
	// Should have called LLM twice: initial + after tool result.
	if mock.CallCount != 2 {
		t.Errorf("CallCount = %d, want 2", mock.CallCount)
	}
}

func TestAgentCore_MultipleToolCalls(t *testing.T) {
	workDir := t.TempDir()
	mock := &MockLLMClient{
		Responses: []*LLMResponse{
			// First: two tool calls at once
			{
				ToolCalls: []ToolCall{
					{ID: "call_1", Name: "list_directory", Input: map[string]any{"path": "."}},
					{ID: "call_2", Name: "list_directory", Input: map[string]any{"path": "."}},
				},
			},
			// Second: final answer
			{Content: "Directory listed successfully."},
		},
	}
	core := NewAgentCore(mock, &AgentConfig{
		WorkDir:      workDir,
		SessionDir:   t.TempDir(),
		LogicalModel: "test-model",
	}, nil)

	result, err := core.Run(context.Background(), "list directories")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if result != "Directory listed successfully." {
		t.Errorf("result = %q, want %q", result, "Directory listed successfully.")
	}
	if mock.CallCount != 2 {
		t.Errorf("CallCount = %d, want 2", mock.CallCount)
	}
}

func TestAgentCore_UnknownTool(t *testing.T) {
	mock := &MockLLMClient{
		Responses: []*LLMResponse{
			{
				ToolCalls: []ToolCall{
					{ID: "call_1", Name: "nonexistent_tool", Input: map[string]any{}},
				},
			},
			{Content: "I apologize, that tool is not available."},
		},
	}
	core := NewAgentCore(mock, &AgentConfig{
		WorkDir:      t.TempDir(),
		SessionDir:   t.TempDir(),
		LogicalModel: "test-model",
	}, nil)

	result, err := core.Run(context.Background(), "use unknown tool")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	// The error message about unknown tool should have been sent back to LLM.
	if mock.CallCount != 2 {
		t.Errorf("CallCount = %d, want 2", mock.CallCount)
	}
	// Check that tool result with error was sent to LLM.
	lastMessages := mock.CallArgs[1].Messages
	foundToolResult := false
	for _, msg := range lastMessages {
		if msg.Role == "tool" && msg.ToolCallID == "call_1" {
			foundToolResult = true
		}
	}
	if !foundToolResult {
		t.Error("expected tool result message with error for unknown tool")
	}
	if result == "" {
		t.Error("expected non-empty result")
	}
}

func TestAgentCore_MaxIterations(t *testing.T) {
	// Create a mock that always returns tool calls (infinite loop scenario).
	mock := &MockLLMClient{}
	for range 50 {
		mock.Responses = append(mock.Responses, &LLMResponse{
			ToolCalls: []ToolCall{
				{ID: "call_loop", Name: "list_directory", Input: map[string]any{"path": "."}},
			},
		})
	}

	core := NewAgentCore(mock, &AgentConfig{
		WorkDir:      t.TempDir(),
		SessionDir:   t.TempDir(),
		LogicalModel: "test-model",
	}, nil)

	_, err := core.Run(context.Background(), "loop forever")
	if err == nil {
		t.Fatal("expected error for max iterations exceeded")
	}
}

func TestAgentCore_SessionPersistence_SaveOnComplete(t *testing.T) {
	sessionDir := t.TempDir()
	mock := &MockLLMClient{
		Responses: []*LLMResponse{
			{Content: "Done."},
		},
	}

	core := NewAgentCore(mock, &AgentConfig{
		WorkDir:      t.TempDir(),
		SessionDir:   sessionDir,
		LogicalModel: "test-model",
	}, nil)
	core.SetSessionID("persist-test")

	_, err := core.Run(context.Background(), "do something")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Verify session folder was created.
	sessionFolder := filepath.Join(sessionDir, "persist-test", "metadata.json")
	if _, err := os.Stat(sessionFolder); err != nil {
		t.Fatalf("session metadata.json should exist: %v", err)
	}

	// Verify session content.
	data, _ := os.ReadFile(sessionFolder)
	var state map[string]any
	json.Unmarshal(data, &state)
	if state["status"] != "completed" {
		t.Errorf("status = %v, want %q", state["status"], "completed")
	}
}

func TestAgentCore_SessionPersistence_ResumeSession(t *testing.T) {
	sessionDir := t.TempDir()

	// First run: create session.
	mock1 := &MockLLMClient{
		Responses: []*LLMResponse{
			{Content: "First response."},
		},
	}
	core1 := NewAgentCore(mock1, &AgentConfig{
		WorkDir:      t.TempDir(),
		SessionDir:   sessionDir,
		LogicalModel: "test-model",
	}, nil)
	core1.SetSessionID("resume-test")
	_, err := core1.Run(context.Background(), "first prompt")
	if err != nil {
		t.Fatalf("First run failed: %v", err)
	}

	// Second run: resume the session.
	mock2 := &MockLLMClient{
		Responses: []*LLMResponse{
			{Content: "Second response."},
		},
	}
	core2 := NewAgentCore(mock2, &AgentConfig{
		WorkDir:      t.TempDir(),
		SessionDir:   sessionDir,
		LogicalModel: "test-model",
	}, nil)
	core2.SetSessionID("resume-test")
	result, err := core2.Run(context.Background(), "second prompt")
	if err != nil {
		t.Fatalf("Second run failed: %v", err)
	}
	if result != "Second response." {
		t.Errorf("result = %q, want %q", result, "Second response.")
	}

	// The second call should have received messages from the first session + new prompt.
	// Messages: [user:first] [assistant:first] [user:second]
	if mock2.CallCount != 1 {
		t.Errorf("CallCount = %d, want 1", mock2.CallCount)
	}
	sentMessages := mock2.CallArgs[0].Messages
	if len(sentMessages) < 3 {
		t.Errorf("expected at least 3 messages (restored + new), got %d", len(sentMessages))
	}
}

// ---- Summarizer Tests ----

func TestDefaultSummarizer_CallsLLM(t *testing.T) {
	mock := &MockLLMClient{
		Responses: []*LLMResponse{
			{Content: "This is a summary of the conversation."},
		},
	}
	core := NewAgentCore(mock, &AgentConfig{
		WorkDir:      t.TempDir(),
		SessionDir:   t.TempDir(),
		LogicalModel: "test-model",
	}, nil)

	msgs := []session.Message{
		{Role: "user", Content: "Create a file"},
		{Role: "assistant", Content: "I will create it"},
	}

	summary, err := core.compactionSummarizer(msgs)
	if err != nil {
		t.Fatalf("compactionSummarizer failed: %v", err)
	}

	if summary != "This is a summary of the conversation." {
		t.Errorf("summary = %q, want LLM response", summary)
	}

	// Verify LLM was called.
	if mock.CallCount != 1 {
		t.Errorf("LLM call count = %d, want 1", mock.CallCount)
	}

	// Verify the prompt contains summarizer instructions.
	systemMsg := mock.CallArgs[0].Messages[0]
	if systemMsg.Role != "system" {
		t.Errorf("first message role = %q, want 'system'", systemMsg.Role)
	}
	if !strings.Contains(systemMsg.Content, "conversation summarizer") {
		t.Errorf("system prompt should contain 'conversation summarizer', got %q", systemMsg.Content)
	}

	// Verify tools were not sent.
	if len(mock.CallArgs[0].Tools) != 0 {
		t.Errorf("tools should be nil/empty, got %d", len(mock.CallArgs[0].Tools))
	}
}

func TestDefaultSummarizer_FallbackOnLLMError(t *testing.T) {
	mock := &MockLLMClient{
		Errors: []error{fmt.Errorf("API unavailable")},
	}
	core := NewAgentCore(mock, &AgentConfig{
		WorkDir:      t.TempDir(),
		SessionDir:   t.TempDir(),
		LogicalModel: "test-model",
	}, nil)

	msgs := []session.Message{
		{Role: "user", Content: "Create a file"},
		{Role: "assistant", Content: "Done", ToolCalls: []session.ToolCallRecord{
			{ID: "tc1", Name: "edit_file"},
		}},
		{Role: "tool", Content: "File created", ToolCallID: "tc1"},
	}

	summary, err := core.compactionSummarizer(msgs)
	if err != nil {
		t.Fatalf("compactionSummarizer should not fail on LLM error: %v", err)
	}

	// Fallback should include tool info.
	if !strings.Contains(summary, "edit_file") {
		t.Errorf("fallback summary should contain tool name 'edit_file', got %q", summary)
	}
	if !strings.Contains(summary, "File created") {
		t.Errorf("fallback summary should contain tool result, got %q", summary)
	}
}

func TestStructuredFallbackSummary_IncludesToolInfo(t *testing.T) {
	core := NewAgentCore(&MockLLMClient{}, &AgentConfig{
		WorkDir:      t.TempDir(),
		SessionDir:   t.TempDir(),
		LogicalModel: "test-model",
	}, nil)

	msgs := []session.Message{
		{Role: "assistant", Content: "I will edit", ToolCalls: []session.ToolCallRecord{
			{ID: "tc1", Name: "edit_file"},
			{ID: "tc2", Name: "execute_command"},
		}},
	}

	result := core.structuredFallbackSummary(msgs)

	if !strings.Contains(result, "edit_file") {
		t.Errorf("should contain tool name 'edit_file', got %q", result)
	}
	if !strings.Contains(result, "execute_command") {
		t.Errorf("should contain tool name 'execute_command', got %q", result)
	}
}

func TestStructuredFallbackSummary_IncludesToolResults(t *testing.T) {
	core := NewAgentCore(&MockLLMClient{}, &AgentConfig{
		WorkDir:      t.TempDir(),
		SessionDir:   t.TempDir(),
		LogicalModel: "test-model",
	}, nil)

	msgs := []session.Message{
		{Role: "tool", Content: "Successfully created file.txt", ToolCallID: "tc1"},
	}

	result := core.structuredFallbackSummary(msgs)

	if !strings.Contains(result, "Successfully created file.txt") {
		t.Errorf("should contain tool result, got %q", result)
	}
}

func TestBuildConversationLog_StructuredFormat(t *testing.T) {
	core := NewAgentCore(&MockLLMClient{}, &AgentConfig{
		WorkDir:      t.TempDir(),
		SessionDir:   t.TempDir(),
		LogicalModel: "test-model",
	}, nil)

	msgs := []session.Message{
		{Role: "user", Content: "Create a file"},
		{Role: "assistant", Content: "I will create it", ToolCalls: []session.ToolCallRecord{
			{ID: "tc1", Name: "edit_file"},
		}},
		{Role: "tool", Content: "File created", ToolCallID: "tc1"},
	}

	log := core.buildConversationLog(msgs)

	if !strings.Contains(log, "USER: Create a file") {
		t.Errorf("should contain user message, got %q", log)
	}
	if !strings.Contains(log, "ASSISTANT: I will create it") {
		t.Errorf("should contain assistant message, got %q", log)
	}
	if !strings.Contains(log, "[TOOL CALL: edit_file (id=tc1)]") {
		t.Errorf("should contain tool call info, got %q", log)
	}
	if !strings.Contains(log, "[TOOL RESULT (id=tc1): File created]") {
		t.Errorf("should contain tool result, got %q", log)
	}
}

func TestAgentCore_EmptyResponse_Retry(t *testing.T) {
	// MockLLM returns empty response first, then a valid response.
	mock := &MockLLMClient{
		Responses: []*LLMResponse{
			{Content: "", StopReason: "end_turn"},
			{Content: "Hello!", StopReason: "end_turn"},
		},
	}
	core := NewAgentCore(mock, &AgentConfig{
		WorkDir:      t.TempDir(),
		SessionDir:   t.TempDir(),
		LogicalModel: "test-model",
	}, nil)

	result, err := core.Run(context.Background(), "say hello")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if result != "Hello!" {
		t.Errorf("result = %q, want %q", result, "Hello!")
	}
	if mock.CallCount != 2 {
		t.Errorf("CallCount = %d, want 2 (1 empty retry + 1 success)", mock.CallCount)
	}
}

func TestAgentCore_EmptyResponse_MaxRetry_Fails(t *testing.T) {
	// MockLLM always returns empty response.
	mock := &MockLLMClient{
		Responses: []*LLMResponse{
			{Content: "", StopReason: "max_tokens"},
			{Content: "", StopReason: "max_tokens"},
		},
	}
	core := NewAgentCore(mock, &AgentConfig{
		WorkDir:      t.TempDir(),
		SessionDir:   t.TempDir(),
		LogicalModel: "test-model",
	}, nil)

	_, err := core.Run(context.Background(), "generate big code")
	if err == nil {
		t.Fatal("expected error for persistent empty response")
	}
	if !strings.Contains(err.Error(), "empty response") {
		t.Errorf("error should mention 'empty response', got %q", err.Error())
	}
	if mock.CallCount != 2 {
		t.Errorf("CallCount = %d, want 2 (1 original + 1 retry)", mock.CallCount)
	}
}


