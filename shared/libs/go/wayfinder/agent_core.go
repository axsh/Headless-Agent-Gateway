package wayfinder

import (
	"context"
	"fmt"

	"github.com/axsh/arctic-tern/logger"
	"github.com/axsh/arctic-tern/wayfinder/tools"
)

const (
	// maxIterations prevents infinite tool-calling loops.
	maxIterations = 25
)

// AgentCore drives the main LLM tool-calling loop.
type AgentCore struct {
	llm      LLMClient
	config   *AgentConfig
	registry *tools.Registry
	tracker  *FileTracker
	logger   logger.Logger
	messages []ChatMessage
}

// NewAgentCore creates a new AgentCore.
// If log is nil, a default noop logger is used.
func NewAgentCore(llm LLMClient, config *AgentConfig, log logger.Logger) *AgentCore {
	if log == nil {
		log = &noopLogger{}
	}

	tracker := NewFileTracker()
	registry := tools.NewRegistry()

	// Create tool context with wayfinder guardrail functions injected.
	tc := &tools.ToolContext{
		WorkDir:          config.WorkDir,
		ValidatePath:     ValidatePath,
		IsBlockedCommand: IsBlockedCommand,
		Tracker:          tracker,
	}
	tools.RegisterAllTools(registry, tc)

	return &AgentCore{
		llm:      llm,
		config:   config,
		registry: registry,
		tracker:  tracker,
		logger:   log,
	}
}

// Run executes the agent with a user prompt.
// It enters the tool-calling loop: send messages to LLM, process tool calls,
// feed results back, repeat until the LLM responds with text only.
func (ac *AgentCore) Run(ctx context.Context, prompt string) (string, error) {
	ac.logger.Debug("agent core run started", "model", ac.config.LogicalModel, "prompt_len", len(prompt))

	// Initialize messages with optional system prompt and user prompt.
	ac.messages = nil
	if ac.config.SystemPrompt != "" {
		ac.messages = append(ac.messages, ChatMessage{
			Role:    "system",
			Content: ac.config.SystemPrompt,
		})
	}
	ac.messages = append(ac.messages, ChatMessage{
		Role:    "user",
		Content: prompt,
	})

	// Convert tool definitions for LLM client.
	rawDefs := ac.registry.Definitions()
	toolDefs := make([]ToolDefinition, len(rawDefs))
	for i, d := range rawDefs {
		toolDefs[i] = ToolDefinition{
			Name:        d.Name,
			Description: d.Description,
			InputSchema: d.InputSchema,
		}
	}

	for iteration := range maxIterations {
		ac.logger.Debug("LLM call", "iteration", iteration, "messages_count", len(ac.messages))

		resp, err := ac.llm.GenerateMessage(ctx, ac.config.LogicalModel, ac.messages, toolDefs)
		if err != nil {
			ac.logger.Error("LLM call failed", "iteration", iteration, "error", err.Error())
			return "", fmt.Errorf("agent core: LLM call failed at iteration %d: %w", iteration, err)
		}

		// If no tool calls, return the text response.
		if len(resp.ToolCalls) == 0 {
			ac.logger.Debug("agent core completed", "iteration", iteration, "response_len", len(resp.Content))
			return resp.Content, nil
		}

		// Append the assistant message with tool calls.
		ac.messages = append(ac.messages, ChatMessage{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		// Process each tool call.
		for _, tc := range resp.ToolCalls {
			result := ac.executeTool(ctx, tc)
			ac.messages = append(ac.messages, ChatMessage{
				Role:       "tool",
				Content:    result,
				ToolCallID: tc.ID,
			})
		}
	}

	return "", fmt.Errorf("agent core: max iterations (%d) exceeded", maxIterations)
}

// executeTool runs a single tool call and returns the result string.
func (ac *AgentCore) executeTool(ctx context.Context, tc ToolCall) string {
	ac.logger.Debug("executing tool", "tool", tc.Name, "id", tc.ID)

	tool, ok := ac.registry.Get(tc.Name)
	if !ok {
		errMsg := fmt.Sprintf("Error: unknown tool %q", tc.Name)
		ac.logger.Warn("unknown tool requested", "tool", tc.Name)
		return errMsg
	}

	result, err := tool.Handler(ctx, tc.Input)
	if err != nil {
		ac.logger.Debug("tool execution failed", "tool", tc.Name, "error", err.Error())
		return fmt.Sprintf("Error: %v", err)
	}

	ac.logger.Debug("tool execution completed", "tool", tc.Name, "result_len", len(result))
	return result
}

// Messages returns the current conversation messages (for session persistence).
func (ac *AgentCore) Messages() []ChatMessage {
	return ac.messages
}

// SetMessages restores conversation messages (for session resume).
func (ac *AgentCore) SetMessages(messages []ChatMessage) {
	ac.messages = messages
}

// Tracker returns the file/process tracker.
func (ac *AgentCore) Tracker() *FileTracker {
	return ac.tracker
}

// noopLogger is a no-operation logger used when no logger is provided.
type noopLogger struct{}

func (n *noopLogger) Trace(msg string, fields ...any) {}
func (n *noopLogger) Debug(msg string, fields ...any) {}
func (n *noopLogger) Info(msg string, fields ...any)  {}
func (n *noopLogger) Warn(msg string, fields ...any)  {}
func (n *noopLogger) Error(msg string, fields ...any) {}
func (n *noopLogger) WithFields(fields map[string]any) logger.Logger {
	return n
}
func (n *noopLogger) WithComponent(name string) logger.Logger { return n }
