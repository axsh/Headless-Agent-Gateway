package subagent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/axsh/arctic-tern/logger"
)

// mockRunner implements AgentRunner for testing.
type mockRunner struct {
	result string
	err    error
}

func (r *mockRunner) RunChild(_ context.Context, cfg *AgentRunnerConfig, sessionID string, llm LLMClient, log logger.Logger, prompt string) (string, error) {
	if r.err != nil {
		return "", r.err
	}
	// Simulate saving a session file.
	if cfg.SessionDir != "" {
		os.MkdirAll(cfg.SessionDir, 0755)
		os.WriteFile(filepath.Join(cfg.SessionDir, sessionID+".json"), []byte(`{"session_id":"`+sessionID+`"}`), 0644)
	}
	return r.result, nil
}

func TestSubagentExecutor_Execute_Success(t *testing.T) {
	// Mock LLM: hint generation -> summarization
	mock := &mockLLM{
		responses: []*LLMResponse{
			// 1. Hint generation
			{Content: `{"objective":"Run build","context":"User wants to build","constraints":""}`},
			// 2. Summarization (after child completes)
			{Content: "Status: SUCCESS\nSummary: Build completed.\nKey Findings: None"},
		},
	}

	runner := &mockRunner{result: "Build succeeded with 0 errors."}

	cfg := &AgentRunnerConfig{
		WorkDir:      t.TempDir(),
		SessionDir:   t.TempDir(),
		LogicalModel: "test-model",
	}

	executor := NewSubagentExecutor(cfg, mock, runner, nil)

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

	var capturedCfg *AgentRunnerConfig
	runner := &configCapturingRunner{result: "Done."}

	mock := &mockLLM{
		responses: []*LLMResponse{
			{Content: `{"objective":"test","context":"","constraints":""}`},
			{Content: "Status: SUCCESS\nSummary: Done."},
		},
	}

	cfg := &AgentRunnerConfig{
		WorkDir:      workDir,
		SessionDir:   sessionDir,
		LogicalModel: "parent-model",
	}

	executor := NewSubagentExecutor(cfg, mock, runner, nil)

	_, err := executor.Execute(
		context.Background(),
		[]ParentMessage{{Role: "user", Content: "test"}},
		"list_directory",
		map[string]any{"path": "."},
	)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	capturedCfg = runner.capturedConfig
	if capturedCfg == nil {
		t.Fatal("runner should have received config")
	}
	if capturedCfg.WorkDir != workDir {
		t.Errorf("child WorkDir = %q, want %q", capturedCfg.WorkDir, workDir)
	}
	if capturedCfg.SessionDir != sessionDir {
		t.Errorf("child SessionDir = %q, want %q", capturedCfg.SessionDir, sessionDir)
	}
}

func TestSubagentExecutor_HintFallback(t *testing.T) {
	// First call (hint) fails, rest succeed.
	failFirst := &failFirstLLM{
		failCount: 1,
		responses: []*LLMResponse{
			// Summarization
			{Content: "Status: SUCCESS\nSummary: Done."},
		},
	}

	runner := &mockRunner{result: "Child result."}

	cfg := &AgentRunnerConfig{
		WorkDir:      t.TempDir(),
		SessionDir:   t.TempDir(),
		LogicalModel: "test-model",
	}

	executor := NewSubagentExecutor(cfg, failFirst, runner, nil)

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
	failLast := &failLastLLM{
		successResponses: []*LLMResponse{
			{Content: `{"objective":"test","context":"","constraints":""}`},
		},
	}

	runner := &mockRunner{result: "Raw child output."}

	cfg := &AgentRunnerConfig{
		WorkDir:      t.TempDir(),
		SessionDir:   t.TempDir(),
		LogicalModel: "test-model",
	}

	executor := NewSubagentExecutor(cfg, failLast, runner, nil)

	result, err := executor.Execute(
		context.Background(),
		[]ParentMessage{{Role: "user", Content: "test"}},
		"execute_command",
		map[string]any{"command": "echo hello"},
	)
	if err != nil {
		t.Fatalf("Execute should succeed despite summarization failure: %v", err)
	}
	if result != "Raw child output." {
		t.Errorf("result = %q, want %q (raw fallback)", result, "Raw child output.")
	}
}

func TestSubagentExecutor_ChildSessionFileCreated(t *testing.T) {
	sessionDir := t.TempDir()
	mock := &mockLLM{
		responses: []*LLMResponse{
			{Content: `{"objective":"test","context":"","constraints":""}`},
			{Content: "Status: SUCCESS\nSummary: Done."},
		},
	}

	runner := &mockRunner{result: "Done."}

	cfg := &AgentRunnerConfig{
		WorkDir:      t.TempDir(),
		SessionDir:   sessionDir,
		LogicalModel: "test-model",
	}

	executor := NewSubagentExecutor(cfg, mock, runner, nil)

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

// configCapturingRunner captures the config passed to RunChild.
type configCapturingRunner struct {
	result         string
	capturedConfig *AgentRunnerConfig
}

func (r *configCapturingRunner) RunChild(_ context.Context, cfg *AgentRunnerConfig, sessionID string, llm LLMClient, log logger.Logger, prompt string) (string, error) {
	r.capturedConfig = cfg
	if cfg.SessionDir != "" {
		os.MkdirAll(cfg.SessionDir, 0755)
		os.WriteFile(filepath.Join(cfg.SessionDir, sessionID+".json"), []byte(`{}`), 0644)
	}
	return r.result, nil
}

// failFirstLLM fails the first N calls then succeeds.
type failFirstLLM struct {
	failCount int
	responses []*LLMResponse
	callCount int
}

func (m *failFirstLLM) GenerateMessage(_ context.Context, _ string, _ []ChatMessage, _ []ToolDefinition) (*LLMResponse, error) {
	m.callCount++
	if m.callCount <= m.failCount {
		return nil, fmt.Errorf("simulated failure %d", m.callCount)
	}
	idx := m.callCount - m.failCount - 1
	if idx < len(m.responses) {
		return m.responses[idx], nil
	}
	return &LLMResponse{Content: ""}, nil
}

// failLastLLM succeeds for the first N calls then fails.
type failLastLLM struct {
	successResponses []*LLMResponse
	callCount        int
}

func (m *failLastLLM) GenerateMessage(_ context.Context, _ string, _ []ChatMessage, _ []ToolDefinition) (*LLMResponse, error) {
	m.callCount++
	if m.callCount <= len(m.successResponses) {
		return m.successResponses[m.callCount-1], nil
	}
	return nil, fmt.Errorf("simulated failure at call %d", m.callCount)
}
