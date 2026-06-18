package subagent

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/axsh/arctic-tern/shared/libs/go/logger"
)

// LLMClient is the interface for LLM communication.
// Defined locally to avoid cyclic import with the wayfinder root package.
type LLMClient interface {
	GenerateMessage(ctx context.Context, model string, messages []ChatMessage, tools []ToolDefinition) (*LLMResponse, error)
}

// ChatMessage is a message in the LLM conversation.
type ChatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall represents a tool invocation from LLM response.
type ToolCall struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

// ToolDefinition describes a tool available to the LLM.
type ToolDefinition struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"input_schema"`
}

// LLMResponse is the response from an LLM call.
type LLMResponse struct {
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// AgentRunnerConfig is a simplified config for creating child sessions.
type AgentRunnerConfig struct {
	WorkDir             string
	SessionDir          string
	LogicalModel        string
	AllowedPathPatterns []string
}

// AgentRunner creates and runs child AgentCore instances.
// Injected by the caller to avoid cyclic import.
type AgentRunner interface {
	RunChild(ctx context.Context, cfg *AgentRunnerConfig, sessionID string, llm LLMClient, log logger.Logger, prompt string) (string, error)
}

// SubagentExecutor manages child session lifecycle.
type SubagentExecutor struct {
	parentConfig *AgentRunnerConfig
	llm          LLMClient
	runner       AgentRunner
	hints        *HintGenerator
	summarizer   *Summarizer
	logger       logger.Logger
}

// NewSubagentExecutor creates a new SubagentExecutor.
// If log is nil, a default no-op logger is used.
func NewSubagentExecutor(
	parentCfg *AgentRunnerConfig,
	llm LLMClient,
	runner AgentRunner,
	log logger.Logger,
) *SubagentExecutor {
	if log == nil {
		log = &noopLog{}
	}
	return &SubagentExecutor{
		parentConfig: parentCfg,
		llm:          llm,
		runner:       runner,
		hints:        NewHintGenerator(llm),
		summarizer:   NewSummarizer(llm),
		logger:       log,
	}
}

// Execute runs a tool in a child session and returns summarized result.
func (e *SubagentExecutor) Execute(
	ctx context.Context,
	parentMessages []ParentMessage,
	toolName string,
	toolInput map[string]any,
) (string, error) {
	e.logger.Debug("subagent execute started", "tool", toolName)

	// 1. Generate hints from parent context.
	hints, err := e.hints.GenerateHints(ctx, parentMessages, toolName, toolInput)
	if err != nil {
		e.logger.Warn("hint generation failed, proceeding without hints", "error", err.Error())
		hints = &Hints{Objective: "Execute the requested tool and report results."}
	}

	// 2. Create child session config (inherit WorkDir and SessionDir).
	childSessionID := uuid.New().String()
	childConfig := &AgentRunnerConfig{
		WorkDir:             e.parentConfig.WorkDir,
		SessionDir:          e.parentConfig.SessionDir,
		LogicalModel:        e.parentConfig.LogicalModel,
		AllowedPathPatterns: e.parentConfig.AllowedPathPatterns,
	}

	e.logger.Debug("child session created", "child_id", childSessionID, "parent_work_dir", childConfig.WorkDir)

	// 3. Build child prompt with hints and tool execution instruction.
	childPrompt := fmt.Sprintf(
		"[SUBAGENT TASK]\nObjective: %s\nContext: %s\n\n"+
			"Execute the following tool and provide a summary of the results:\n"+
			"Tool: %s\nInput: %v",
		hints.Objective, hints.Context, toolName, toolInput,
	)

	// 4. Run child via injected runner (avoids direct wayfinder import).
	childResult, err := e.runner.RunChild(ctx, childConfig, childSessionID, e.llm, e.logger, childPrompt)
	if err != nil {
		return "", fmt.Errorf("child session %s failed: %w", childSessionID, err)
	}

	e.logger.Debug("child session completed", "child_id", childSessionID, "result_len", len(childResult))

	// 5. Summarize child result for parent consumption.
	summary, err := e.summarizer.SummarizeForParent(ctx, hints, childResult)
	if err != nil {
		e.logger.Warn("summarization failed, returning raw result", "error", err.Error())
		// Fallback: return raw result if summarization fails.
		return childResult, nil
	}

	return summary, nil
}

// noopLog is a no-op logger.
type noopLog struct{}

func (n *noopLog) Trace(msg string, fields ...any) {}
func (n *noopLog) Debug(msg string, fields ...any) {}
func (n *noopLog) Info(msg string, fields ...any)  {}
func (n *noopLog) Warn(msg string, fields ...any)  {}
func (n *noopLog) Error(msg string, fields ...any) {}
func (n *noopLog) WithFields(fields map[string]any) logger.Logger {
	return n
}
func (n *noopLog) WithComponent(name string) logger.Logger { return n }
