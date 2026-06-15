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
	logger      logger.Logger
	baseURL     string // Bifrost proxy URL
	token       string // Authentication token
	adapterCfg  *codingagent.AdapterConfig
}

// NewAdapter creates a new Wayfinder CodingAgent adapter.
func NewAdapter(cfg *codingagent.AdapterConfig) *Adapter {
	log := cfg.Logger
	if log == nil {
		log = &noopLogger{}
	}
	return &Adapter{
		logger:     log.WithComponent(AgentName),
		baseURL:    cfg.GatewayURL,
		token:      cfg.GatewayToken,
		adapterCfg: cfg,
	}
}

// Name returns the agent backend name.
func (a *Adapter) Name() string {
	return AgentName
}

// CreateSession starts a new Wayfinder agent session.
func (a *Adapter) CreateSession(ctx context.Context, opts ...codingagent.SessionOption) (codingagent.Session, error) {
	cfg := codingagent.NewSessionConfig(opts...)

	// Apply defaults from AdapterConfig (default model, work dir, etc.).
	codingagent.ApplyDefaults(cfg, a.adapterCfg)

	// Apply defaults for wayfinder.
	if cfg.WorkDir == "" {
		cfg.WorkDir = "."
	}

	agentCfg := &AgentConfig{
		WorkDir:        cfg.WorkDir,
		SessionDir:     cfg.SessionDir,
		LogicalModel:   cfg.Model,
		EnableSubagent: a.adapterCfg.EnableSubagent,
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
	planner := planning.NewWBSPlanner(newPlanningLLMAdapter(llmClient))
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
	subExec := subagent.NewSubagentExecutor(parentCfg, subLLM, runner, a.logger,
		subagent.WithParentSeqFunc(func() int { return core.NextSeq() }),
	)
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
// Events are emitted in real-time via the EventEmitter injected into AgentCore.
func (s *wayfinderSession) Send(ctx context.Context, message string) (<-chan codingagent.StreamEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ch := make(chan codingagent.StreamEvent, 64)

	// Inject EventEmitter so AgentCore streams events in real-time.
	emitter := NewEventEmitter(ch)
	s.core.SetEmitter(emitter)

	prompt := message
	if prompt == "" {
		prompt = s.prompt
	}

	go func() {
		defer close(ch)
		defer s.core.SetEmitter(nil) // Clean up emitter reference.

		// Send initial system event.
		ch <- codingagent.StreamEvent{
			Type:      codingagent.EventSystem,
			SessionID: s.id,
		}

		_, err := s.core.Run(ctx, prompt)
		if err != nil {
			ch <- codingagent.StreamEvent{
				Type:  codingagent.EventError,
				Error: err,
			}
			return
		}

		// Send completion event.
		// Note: Text and tool events are already emitted via the EventEmitter
		// during Run(), so we only need the final result marker here.
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
	opts ...subagent.GenerateOptions,
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

	// Convert subagent GenerateOptions to wayfinder GenerateOptions.
	var wfOpts []GenerateOptions
	for _, opt := range opts {
		wfOpt := GenerateOptions{}
		if opt.ResponseFormat != nil {
			wfOpt.ResponseFormat = &ResponseFormat{
				Type:       opt.ResponseFormat.Type,
				JSONSchema: opt.ResponseFormat.JSONSchema,
			}
		}
		wfOpts = append(wfOpts, wfOpt)
	}

	resp, err := a.llm.GenerateMessage(ctx, model, wfMsgs, wfTools, wfOpts...)
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

// newPlanningLLMAdapter wraps a wayfinder LLMClient as a planning.LLMClient.
func newPlanningLLMAdapter(llm LLMClient) planning.LLMClient {
	return &planningLLMAdapter{llm: llm}
}

// planningLLMAdapter bridges wayfinder.LLMClient to planning.LLMClient.
type planningLLMAdapter struct {
	llm LLMClient
}

func (a *planningLLMAdapter) GenerateMessage(
	ctx context.Context,
	model string,
	msgs []planning.ChatMessage,
	tools []planning.ToolDefinition,
	opts ...planning.GenerateOptions,
) (*planning.LLMResponse, error) {
	// Convert planning messages to wayfinder messages.
	wfMsgs := make([]ChatMessage, len(msgs))
	for i, m := range msgs {
		wfMsgs[i] = ChatMessage{Role: m.Role, Content: m.Content}
	}

	// Convert planning tool definitions to wayfinder tool definitions.
	wfTools := make([]ToolDefinition, len(tools))
	for i, t := range tools {
		schema, _ := t.InputSchema.(map[string]any)
		wfTools[i] = ToolDefinition{
			Name: t.Name, Description: t.Description, InputSchema: schema,
		}
	}

	// Convert planning GenerateOptions to wayfinder GenerateOptions.
	var wfOpts []GenerateOptions
	for _, opt := range opts {
		wfOpt := GenerateOptions{}
		if opt.ResponseFormat != nil {
			wfOpt.ResponseFormat = &ResponseFormat{
				Type:       opt.ResponseFormat.Type,
				JSONSchema: opt.ResponseFormat.JSONSchema,
			}
		}
		wfOpts = append(wfOpts, wfOpt)
	}

	resp, err := a.llm.GenerateMessage(ctx, model, wfMsgs, wfTools, wfOpts...)
	if err != nil {
		return nil, err
	}

	return &planning.LLMResponse{Content: resp.Content}, nil
}
