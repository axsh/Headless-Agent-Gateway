package subagent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/axsh/arctic-tern/wayfinder"
)

func TestSubagentExecutor_Execute_Success(t *testing.T) {
	// Mock LLM: hint generation -> child AgentCore response -> summarization
	mock := &mockLLM{
		responses: []*wayfinder.LLMResponse{
			// 1. Hint generation
			{Content: `{"objective":"Run build","context":"User wants to build","constraints":""}`},
			// 2. Child AgentCore run (simple text response, no tool calls)
			{Content: "Build succeeded with 0 errors."},
			// 3. Summarization
			{Content: "Status: SUCCESS\nSummary: Build completed.\nKey Findings: None"},
		},
	}

	cfg := &wayfinder.AgentConfig{
		WorkDir:      t.TempDir(),
		SessionDir:   t.TempDir(),
		LogicalModel: "test-model",
	}

	executor := NewSubagentExecutor(cfg, mock, nil)

	result, err := executor.Execute(
		context.Background(),
		[]ParentMessage{{Role: "user", Content: "Build the project"}},
		"execute_command",
		map[string]any{"command": "go build ./..."},
	)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result")
	}
}

func TestSubagentExecutor_InheritConfig(t *testing.T) {
	workDir := t.TempDir()
	sessionDir := t.TempDir()

	mock := &mockLLM{
		responses: []*wayfinder.LLMResponse{
			{Content: `{"objective":"test","context":"","constraints":""}`},
			{Content: "Done."},
			{Content: "Status: SUCCESS\nSummary: Done."},
		},
	}

	cfg := &wayfinder.AgentConfig{
		WorkDir:      workDir,
		SessionDir:   sessionDir,
		LogicalModel: "parent-model",
	}

	executor := NewSubagentExecutor(cfg, mock, nil)

	result, err := executor.Execute(
		context.Background(),
		[]ParentMessage{{Role: "user", Content: "test"}},
		"list_directory",
		map[string]any{"path": "."},
	)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result")
	}
}

func TestSubagentExecutor_HintFallback(t *testing.T) {
	// First call (hint) fails, but execution continues.
	callCount := 0
	mock := &mockLLM{
		responses: []*wayfinder.LLMResponse{
			// Hints will fail (simulated by returning error on first call)
		},
	}
	// Override to make first call fail, rest succeed.
	failFirstMock := &failFirstLLM{
		failCount: 1,
		responses: []*wayfinder.LLMResponse{
			{Content: "Child result."},
			{Content: "Status: SUCCESS\nSummary: Done."},
		},
	}
	_ = mock
	_ = callCount

	cfg := &wayfinder.AgentConfig{
		WorkDir:      t.TempDir(),
		SessionDir:   t.TempDir(),
		LogicalModel: "test-model",
	}

	executor := NewSubagentExecutor(cfg, failFirstMock, nil)

	result, err := executor.Execute(
		context.Background(),
		[]ParentMessage{{Role: "user", Content: "test"}},
		"execute_command",
		map[string]any{"command": "echo hello"},
	)
	if err != nil {
		t.Fatalf("Execute should succeed despite hint failure: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result")
	}
}

func TestSubagentExecutor_SummarizationFallback(t *testing.T) {
	// Hints succeed, child succeeds, summarization fails -> raw result returned.
	failLastMock := &failLastLLM{
		successResponses: []*wayfinder.LLMResponse{
			{Content: `{"objective":"test","context":"","constraints":""}`},
			{Content: "Raw child output."},
		},
	}

	cfg := &wayfinder.AgentConfig{
		WorkDir:      t.TempDir(),
		SessionDir:   t.TempDir(),
		LogicalModel: "test-model",
	}

	executor := NewSubagentExecutor(cfg, failLastMock, nil)

	result, err := executor.Execute(
		context.Background(),
		[]ParentMessage{{Role: "user", Content: "test"}},
		"execute_command",
		map[string]any{"command": "echo hello"},
	)
	if err != nil {
		t.Fatalf("Execute should succeed despite summarization failure: %v", err)
	}
	// Should return raw result as fallback.
	if result != "Raw child output." {
		t.Errorf("result = %q, want %q (raw fallback)", result, "Raw child output.")
	}
}

func TestSubagentExecutor_ChildSessionFileCreated(t *testing.T) {
	sessionDir := t.TempDir()
	mock := &mockLLM{
		responses: []*wayfinder.LLMResponse{
			{Content: `{"objective":"test","context":"","constraints":""}`},
			{Content: "Done."},
			{Content: "Status: SUCCESS\nSummary: Done."},
		},
	}

	cfg := &wayfinder.AgentConfig{
		WorkDir:      t.TempDir(),
		SessionDir:   sessionDir,
		LogicalModel: "test-model",
	}

	executor := NewSubagentExecutor(cfg, mock, nil)

	_, err := executor.Execute(
		context.Background(),
		[]ParentMessage{{Role: "user", Content: "test"}},
		"execute_command",
		map[string]any{"command": "echo test"},
	)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Check that a session file was created in sessionDir.
	entries, _ := os.ReadDir(sessionDir)
	found := false
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".json" {
			found = true
		}
	}
	if !found {
		t.Error("expected child session file to be created in sessionDir")
	}
}

// failFirstLLM fails the first N calls then succeeds.
type failFirstLLM struct {
	failCount int
	responses []*wayfinder.LLMResponse
	callCount int
}

func (m *failFirstLLM) GenerateMessage(_ context.Context, _ string, _ []wayfinder.ChatMessage, _ []wayfinder.ToolDefinition) (*wayfinder.LLMResponse, error) {
	m.callCount++
	if m.callCount <= m.failCount {
		return nil, fmt.Errorf("simulated failure %d", m.callCount)
	}
	idx := m.callCount - m.failCount - 1
	if idx < len(m.responses) {
		return m.responses[idx], nil
	}
	return &wayfinder.LLMResponse{Content: ""}, nil
}

// failLastLLM succeeds for the first N calls then fails.
type failLastLLM struct {
	successResponses []*wayfinder.LLMResponse
	callCount        int
}

func (m *failLastLLM) GenerateMessage(_ context.Context, _ string, _ []wayfinder.ChatMessage, _ []wayfinder.ToolDefinition) (*wayfinder.LLMResponse, error) {
	m.callCount++
	if m.callCount <= len(m.successResponses) {
		return m.successResponses[m.callCount-1], nil
	}
	return nil, fmt.Errorf("simulated failure at call %d", m.callCount)
}
