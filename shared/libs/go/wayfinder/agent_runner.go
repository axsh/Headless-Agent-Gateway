package wayfinder

import (
	"context"

	"github.com/axsh/arctic-tern/shared/libs/go/logger"
	"github.com/axsh/arctic-tern/shared/libs/go/wayfinder/subagent"
)

// AgentRunnerImpl implements subagent.AgentRunner.
// It creates a child AgentCore instance and runs it with the given prompt.
type AgentRunnerImpl struct {
	baseURL string
	token   string
}

// NewAgentRunnerImpl creates a new AgentRunnerImpl.
func NewAgentRunnerImpl(baseURL, token string) *AgentRunnerImpl {
	return &AgentRunnerImpl{baseURL: baseURL, token: token}
}

// RunChild creates a child AgentCore and runs it with the prompt.
func (r *AgentRunnerImpl) RunChild(
	ctx context.Context,
	cfg *subagent.AgentRunnerConfig,
	sessionID string,
	llm subagent.LLMClient,
	log logger.Logger,
	prompt string,
) (string, error) {
	// Create child config with subagent disabled to prevent infinite recursion.
	childCfg := &AgentConfig{
		WorkDir:             cfg.WorkDir,
		SessionDir:          cfg.SessionDir,
		LogicalModel:        cfg.LogicalModel,
		AllowedPathPatterns: cfg.AllowedPathPatterns,
		EnableSubagent:      false, // Prevent recursive subagent delegation.
	}
	if err := InitConfig(childCfg); err != nil {
		return "", err
	}

	// Wrap subagent.LLMClient to wayfinder.LLMClient.
	wrappedLLM := &subagentToWayfinderLLM{inner: llm}

	// Create child AgentCore.
	child := NewAgentCore(wrappedLLM, childCfg, log)
	child.SetSessionID(sessionID)

	return child.Run(ctx, prompt)
}

// subagentToWayfinderLLM wraps subagent.LLMClient to satisfy wayfinder.LLMClient.
type subagentToWayfinderLLM struct {
	inner subagent.LLMClient
}

func (a *subagentToWayfinderLLM) GenerateMessage(
	ctx context.Context,
	model string,
	msgs []ChatMessage,
	tools []ToolDefinition,
) (*LLMResponse, error) {
	// Convert wayfinder messages to subagent messages.
	subMsgs := make([]subagent.ChatMessage, len(msgs))
	for i, m := range msgs {
		subMsgs[i] = subagent.ChatMessage{
			Role:       m.Role,
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
		}
		for _, tc := range m.ToolCalls {
			subMsgs[i].ToolCalls = append(subMsgs[i].ToolCalls, subagent.ToolCall{
				ID: tc.ID, Name: tc.Name, Input: tc.Input,
			})
		}
	}

	// Convert wayfinder tool definitions to subagent tool definitions.
	subTools := make([]subagent.ToolDefinition, len(tools))
	for i, t := range tools {
		subTools[i] = subagent.ToolDefinition{
			Name: t.Name, Description: t.Description, InputSchema: t.InputSchema,
		}
	}

	resp, err := a.inner.GenerateMessage(ctx, model, subMsgs, subTools)
	if err != nil {
		return nil, err
	}

	// Convert subagent response back to wayfinder types.
	result := &LLMResponse{Content: resp.Content}
	for _, tc := range resp.ToolCalls {
		result.ToolCalls = append(result.ToolCalls, ToolCall{
			ID: tc.ID, Name: tc.Name, Input: tc.Input,
		})
	}
	return result, nil
}
