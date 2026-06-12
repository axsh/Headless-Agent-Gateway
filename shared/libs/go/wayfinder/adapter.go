package wayfinder

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/axsh/arctic-tern/codingagent"
	"github.com/axsh/arctic-tern/logger"
	"github.com/axsh/arctic-tern/wayfinder/planning"
	"github.com/axsh/arctic-tern/wayfinder/subagent"
)

const (
	// AgentName is the registered name for the Wayfinder agent.
	AgentName = "wayfinder"
)

// Adapter implements codingagent.CodingAgent for Wayfinder.
type Adapter struct {
	logger  logger.Logger
	baseURL string // Bifrost proxy URL
	token   string // Authentication token
}

// NewAdapter creates a new Wayfinder CodingAgent adapter.
func NewAdapter(baseURL, token string, log logger.Logger) *Adapter {
	if log == nil {
		log = &noopLogger{}
	}
	return &Adapter{
		logger:  log.WithComponent(AgentName),
		baseURL: baseURL,
		token:   token,
	}
}

// Name returns the agent backend name.
func (a *Adapter) Name() string {
	return AgentName
}

// CreateSession starts a new Wayfinder agent session.
func (a *Adapter) CreateSession(ctx context.Context, opts ...codingagent.SessionOption) (codingagent.Session, error) {
	cfg := codingagent.NewSessionConfig(opts...)

	// Apply defaults for wayfinder.
	if cfg.WorkDir == "" {
		cfg.WorkDir = "."
	}

	agentCfg := &AgentConfig{
		WorkDir:      cfg.WorkDir,
		SessionDir:   cfg.SessionDir,
		LogicalModel: cfg.Model,
	}
	if err := InitConfig(agentCfg); err != nil {
		return nil, fmt.Errorf("wayfinder: init config: %w", err)
	}

	llmClient := NewBifrostClient(a.baseURL, a.token)
	core := NewAgentCore(llmClient, agentCfg, a.logger)

	// === Wire all components ===

	// 1. Session ID.
	sessionID := cfg.AgentSessionID
	if sessionID == "" {
		sessionID = generateSessionID()
	}
	core.SetSessionID(sessionID)

	// 2. Session resume (restore messages from existing session).
	if cfg.AgentSessionID != "" {
		a.logger.Debug("resuming session", "session_id", cfg.AgentSessionID)
		// restoreSession is called automatically in AgentCore.Run().
	}

	// 3. ExecutionRouter (simple/planning auto-detection).
	router := NewExecutionRouter(llmClient)
	core.SetRouter(router)

	// 4. WBSPlanner (WBS plan generation).
	planner := planning.NewWBSPlanner(llmClient)
	core.SetPlanner(planner)

	// 5. AgentRunner + SubagentExecutor (child session and tool delegation).
	runner := NewAgentRunnerImpl(a.baseURL, a.token)
	core.SetRunner(runner)

	subLLM := newSubagentLLMAdapter(llmClient)
	core.SetSubagentLLM(subLLM)

	parentCfg := &subagent.AgentRunnerConfig{
		WorkDir:             agentCfg.WorkDir,
		SessionDir:          agentCfg.SessionDir,
		LogicalModel:        agentCfg.LogicalModel,
		AllowedPathPatterns: agentCfg.AllowedPathPatterns,
	}
	subExec := subagent.NewSubagentExecutor(parentCfg, subLLM, runner, a.logger)
	core.SetSubagentExecutor(subExec)

	a.logger.Info("session created",
		"session_id", sessionID,
		"model", agentCfg.LogicalModel,
		"work_dir", agentCfg.WorkDir,
	)

	return &wayfinderSession{
		id:     sessionID,
		core:   core,
		config: agentCfg,
		logger: a.logger,
		prompt: cfg.Prompt,
	}, nil
}

// Close releases adapter resources.
func (a *Adapter) Close() error {
	return nil
}

// wayfinderSession implements codingagent.Session.
type wayfinderSession struct {
	id     string
	core   *AgentCore
	config *AgentConfig
	logger logger.Logger
	prompt string
	mu     sync.Mutex
}

// ID returns the session ID.
func (s *wayfinderSession) ID() string {
	return s.id
}

// Send sends a message to the agent and returns a streaming event channel.
func (s *wayfinderSession) Send(ctx context.Context, message string) (<-chan codingagent.StreamEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ch := make(chan codingagent.StreamEvent, 64)

	prompt := message
	if prompt == "" {
		prompt = s.prompt
	}

	go func() {
		defer close(ch)

		// Send initial system event.
		ch <- codingagent.StreamEvent{
			Type:      codingagent.EventSystem,
			SessionID: s.id,
		}

		result, err := s.core.Run(ctx, prompt)
		if err != nil {
			ch <- codingagent.StreamEvent{
				Type:  codingagent.EventError,
				Error: err,
			}
			return
		}

		// Send the text result.
		if result != "" {
			ch <- codingagent.StreamEvent{
				Type:    codingagent.EventText,
				Content: result,
			}
		}

		// Send completion event.
		ch <- codingagent.StreamEvent{
			Type: codingagent.EventResult,
		}
	}()

	return ch, nil
}

// Close terminates the session.
func (s *wayfinderSession) Close() error {
	s.logger.Debug("session closed", "session_id", s.id)
	return nil
}

// generateSessionID creates a simple unique session ID.
func generateSessionID() string {
	return fmt.Sprintf("wf-%d", time.Now().UnixNano())
}

// newSubagentLLMAdapter wraps a wayfinder LLMClient as a subagent.LLMClient.
func newSubagentLLMAdapter(llm LLMClient) subagent.LLMClient {
	return &subagentLLMAdapter{llm: llm}
}

// subagentLLMAdapter bridges wayfinder.LLMClient to subagent.LLMClient.
type subagentLLMAdapter struct {
	llm LLMClient
}

func (a *subagentLLMAdapter) GenerateMessage(
	ctx context.Context,
	model string,
	msgs []subagent.ChatMessage,
	tools []subagent.ToolDefinition,
) (*subagent.LLMResponse, error) {
	// Convert subagent messages to wayfinder messages.
	wfMsgs := make([]ChatMessage, len(msgs))
	for i, m := range msgs {
		wfMsgs[i] = ChatMessage{
			Role:       m.Role,
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
		}
		for _, tc := range m.ToolCalls {
			wfMsgs[i].ToolCalls = append(wfMsgs[i].ToolCalls, ToolCall{
				ID: tc.ID, Name: tc.Name, Input: tc.Input,
			})
		}
	}

	// Convert subagent tool definitions to wayfinder tool definitions.
	wfTools := make([]ToolDefinition, len(tools))
	for i, t := range tools {
		// subagent.InputSchema is any, wayfinder.InputSchema is map[string]any.
		schema, _ := t.InputSchema.(map[string]any)
		wfTools[i] = ToolDefinition{
			Name: t.Name, Description: t.Description, InputSchema: schema,
		}
	}

	resp, err := a.llm.GenerateMessage(ctx, model, wfMsgs, wfTools)
	if err != nil {
		return nil, err
	}

	// Convert wayfinder response back to subagent response.
	result := &subagent.LLMResponse{Content: resp.Content}
	for _, tc := range resp.ToolCalls {
		result.ToolCalls = append(result.ToolCalls, subagent.ToolCall{
			ID: tc.ID, Name: tc.Name, Input: tc.Input,
		})
	}
	return result, nil
}
