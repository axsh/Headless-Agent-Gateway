package wayfinder

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
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

	// Verify session file was created.
	sessionFile := filepath.Join(sessionDir, "persist-test.json")
	if _, err := os.Stat(sessionFile); err != nil {
		t.Fatalf("session file should exist: %v", err)
	}

	// Verify session content.
	data, _ := os.ReadFile(sessionFile)
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
