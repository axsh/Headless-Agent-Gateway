package wayfinder

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/axsh/arctic-tern/logger"
	"github.com/axsh/arctic-tern/wayfinder/planning"
	"github.com/axsh/arctic-tern/wayfinder/session"
	"github.com/axsh/arctic-tern/wayfinder/tools"
)

const (
	// maxIterations prevents infinite tool-calling loops.
	maxIterations = 25
)

// SubagentRunner is the interface for subagent execution.
// This breaks the cyclic dependency between wayfinder and wayfinder/subagent.
type SubagentRunner interface {
	Execute(ctx context.Context, parentMessages []ChatMessage, toolName string, toolInput map[string]any) (string, error)
}

// AgentCore drives the main LLM tool-calling loop.
type AgentCore struct {
	llm            LLMClient
	config         *AgentConfig
	registry       *tools.Registry
	tracker        *FileTracker
	logger         logger.Logger
	messages       []ChatMessage
	store          *session.Store
	sessionID      string
	compactionCfg  *session.CompactionConfig
	subagent       SubagentRunner     // nil if subagent is disabled
	router         *ExecutionRouter   // nil if routing is disabled
	planner        *planning.WBSPlanner // nil if planning is disabled
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

	var store *session.Store
	if config.SessionDir != "" {
		store = session.NewStore(config.SessionDir)
	}

	return &AgentCore{
		llm:           llm,
		config:        config,
		registry:      registry,
		tracker:       tracker,
		logger:        log,
		store:         store,
		compactionCfg: session.DefaultCompactionConfig(),
	}
}

// Run executes the agent with a user prompt.
// It determines the execution route and dispatches accordingly.
func (ac *AgentCore) Run(ctx context.Context, prompt string) (string, error) {
	ac.logger.Debug("agent core run started", "model", ac.config.LogicalModel, "prompt_len", len(prompt))

	// Try to restore session if sessionID is set.
	if ac.sessionID != "" && ac.store != nil {
		if err := ac.restoreSession(); err != nil {
			ac.logger.Warn("session restore failed, starting fresh", "error", err.Error())
		}
	}

	// Check if we have a WBS tree to resume.
	if ac.store != nil && ac.sessionID != "" {
		if wbsTree := ac.loadWBSFromSession(); wbsTree != nil && !wbsTree.IsComplete() {
			ac.logger.Info("resuming WBS execution from session")
			return ac.runWithWBSTree(ctx, wbsTree)
		}
	}

	// Determine execution route if router is configured.
	if ac.router != nil {
		route, reason, _ := ac.router.Route(ctx, ac.config.LogicalModel, prompt)
		ac.logger.Info("execution route determined", "route", route, "reason", reason)

		if route == RoutePlanning && ac.planner != nil {
			return ac.runWithPlanning(ctx, prompt)
		}
	}

	// Simple execution (existing loop).
	return ac.runSimple(ctx, prompt)
}

// runSimple is the existing tool-calling loop.
func (ac *AgentCore) runSimple(ctx context.Context, prompt string) (string, error) {
	// If no messages restored, initialize with system prompt and user prompt.
	if len(ac.messages) == 0 {
		if ac.config.SystemPrompt != "" {
			ac.messages = append(ac.messages, ChatMessage{
				Role:    "system",
				Content: ac.config.SystemPrompt,
			})
		}
	}

	// Append new user prompt.
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

		// Apply compaction if needed before LLM call.
		ac.applyCompaction()

		resp, err := ac.llm.GenerateMessage(ctx, ac.config.LogicalModel, ac.messages, toolDefs)
		if err != nil {
			ac.logger.Error("LLM call failed", "iteration", iteration, "error", err.Error())
			ac.saveSession(session.StatusFailed)
			return "", fmt.Errorf("agent core: LLM call failed at iteration %d: %w", iteration, err)
		}

		// If no tool calls, return the text response.
		if len(resp.ToolCalls) == 0 {
			ac.logger.Debug("agent core completed", "iteration", iteration, "response_len", len(resp.Content))
			ac.messages = append(ac.messages, ChatMessage{
				Role:    "assistant",
				Content: resp.Content,
			})
			ac.saveSession(session.StatusCompleted)
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

		// Save session after each tool round.
		ac.saveSession(session.StatusActive)
	}

	ac.saveSession(session.StatusFailed)
	return "", fmt.Errorf("agent core: max iterations (%d) exceeded", maxIterations)
}

// restoreSession loads session state and restores messages and tracker.
func (ac *AgentCore) restoreSession() error {
	state, err := ac.store.Load(ac.sessionID)
	if err != nil {
		return err
	}
	if state == nil {
		ac.logger.Debug("no existing session found", "session_id", ac.sessionID)
		return nil
	}

	// Validate tracker state.
	session.ValidateTrackerState(state)
	ac.logger.Debug("session restored", "session_id", ac.sessionID, "messages", len(state.Messages))

	// Restore messages.
	ac.messages = convertFromSessionMessages(state.Messages)

	// Restore file tracker.
	for _, f := range state.CreatedFiles {
		ac.tracker.TrackFile(f.Path)
	}
	for _, p := range state.RunningProcesses {
		ac.tracker.TrackProcess(p.PID, p.Command)
	}

	return nil
}

// saveSession persists the current session state.
func (ac *AgentCore) saveSession(status string) {
	if ac.store == nil || ac.sessionID == "" {
		return
	}

	state := &session.SessionState{
		SessionID:        ac.sessionID,
		Status:           status,
		Messages:         convertToSessionMessages(ac.messages),
		CreatedFiles:     ac.tracker.TrackedFilesSnapshot(),
		RunningProcesses: ac.tracker.TrackedProcessesSnapshot(),
		CreatedAt:        time.Now(),
	}

	if err := ac.store.Save(state); err != nil {
		ac.logger.Error("failed to save session", "error", err.Error())
	}
}

// applyCompaction applies context compaction if the message history is too long.
func (ac *AgentCore) applyCompaction() {
	sessionMsgs := convertToSessionMessages(ac.messages)
	if !session.NeedsCompaction(sessionMsgs, ac.compactionCfg) {
		return
	}

	ac.logger.Debug("applying compaction", "messages_before", len(ac.messages))

	// Trim long content first.
	sessionMsgs = session.TrimLongContent(sessionMsgs, ac.compactionCfg.MaxContentLen)

	// Apply compaction with a simple built-in summarizer.
	compacted, err := session.Compact(sessionMsgs, ac.compactionCfg, ac.defaultSummarizer)
	if err != nil {
		ac.logger.Warn("compaction failed, continuing with full history", "error", err.Error())
		return
	}

	ac.messages = convertFromSessionMessages(compacted)
	ac.logger.Debug("compaction applied", "messages_after", len(ac.messages))
}

// defaultSummarizer creates a simple summary of old messages.
func (ac *AgentCore) defaultSummarizer(msgs []session.Message) (string, error) {
	var summary string
	for _, m := range msgs {
		if m.Role == "user" || m.Role == "assistant" {
			if len(m.Content) > 200 {
				summary += m.Role + ": " + m.Content[:200] + "...\n"
			} else {
				summary += m.Role + ": " + m.Content + "\n"
			}
		}
	}
	return summary, nil
}

// convertToSessionMessages converts ChatMessages to session.Messages.
func convertToSessionMessages(msgs []ChatMessage) []session.Message {
	result := make([]session.Message, len(msgs))
	for i, m := range msgs {
		sm := session.Message{
			Role:       m.Role,
			Content:    m.Content,
			Timestamp:  time.Now(),
			ToolCallID: m.ToolCallID,
		}
		for _, tc := range m.ToolCalls {
			sm.ToolCalls = append(sm.ToolCalls, session.ToolCallRecord{
				ID:    tc.ID,
				Name:  tc.Name,
				Input: tc.Input,
			})
		}
		result[i] = sm
	}
	return result
}

// convertFromSessionMessages converts session.Messages to ChatMessages.
func convertFromSessionMessages(msgs []session.Message) []ChatMessage {
	result := make([]ChatMessage, len(msgs))
	for i, m := range msgs {
		cm := ChatMessage{
			Role:       m.Role,
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
		}
		for _, tc := range m.ToolCalls {
			cm.ToolCalls = append(cm.ToolCalls, ToolCall{
				ID:    tc.ID,
				Name:  tc.Name,
				Input: tc.Input,
			})
		}
		result[i] = cm
	}
	return result
}

// SetSessionID sets the session ID for persistence.
func (ac *AgentCore) SetSessionID(id string) {
	ac.sessionID = id
}

// SessionID returns the current session ID.
func (ac *AgentCore) SessionID() string {
	return ac.sessionID
}

// SetSubagentExecutor configures the subagent executor for delegating heavy tool calls.
func (ac *AgentCore) SetSubagentExecutor(exec SubagentRunner) {
	ac.subagent = exec
}

// executeTool runs a single tool call and returns the result string.
func (ac *AgentCore) executeTool(ctx context.Context, tc ToolCall) string {
	ac.logger.Debug("executing tool", "tool", tc.Name, "id", tc.ID)

	// Delegate execute_command to subagent if configured.
	if tc.Name == "execute_command" && ac.subagent != nil {
		ac.logger.Debug("delegating to subagent", "tool", tc.Name)
		result, err := ac.subagent.Execute(ctx, ac.messages, tc.Name, tc.Input)
		if err != nil {
			ac.logger.Debug("subagent execution failed", "tool", tc.Name, "error", err.Error())
			return fmt.Sprintf("Error: %v", err)
		}
		ac.logger.Debug("subagent execution completed", "tool", tc.Name, "result_len", len(result))
		return result
	}

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

// SetRouter configures the execution router.
func (ac *AgentCore) SetRouter(router *ExecutionRouter) {
	ac.router = router
}

// SetPlanner configures the WBS planner.
func (ac *AgentCore) SetPlanner(planner *planning.WBSPlanner) {
	ac.planner = planner
}

// runWithPlanning generates a WBS and orchestrates execution.
func (ac *AgentCore) runWithPlanning(ctx context.Context, prompt string) (string, error) {
	ac.logger.Info("generating WBS plan")

	tree, err := ac.planner.GenerateWBS(ctx, ac.config.LogicalModel, prompt)
	if err != nil {
		ac.logger.Warn("WBS generation failed, falling back to simple", "error", err.Error())
		return ac.runSimple(ctx, prompt)
	}

	ac.logger.Debug("WBS plan generated", "root_nodes", len(tree.RootNodes))
	return ac.runWithWBSTree(ctx, tree)
}

// runWithWBSTree orchestrates execution of a WBS tree.
func (ac *AgentCore) runWithWBSTree(ctx context.Context, tree *planning.WBSTree) (string, error) {
	// Create a node executor that delegates to the simple run loop.
	nodeExec := &agentNodeExecutor{core: ac}
	persister := &agentWBSPersister{core: ac}

	orch := planning.NewWBSOrchestrator(nodeExec, persister, ac.logger)
	if err := orch.Execute(ctx, tree); err != nil {
		return "", fmt.Errorf("WBS orchestration failed: %w", err)
	}

	return planning.CollectResults(tree), nil
}

// loadWBSFromSession loads WBS tree from the session state if available.
func (ac *AgentCore) loadWBSFromSession() *planning.WBSTree {
	if ac.store == nil || ac.sessionID == "" {
		return nil
	}
	state, err := ac.store.Load(ac.sessionID)
	if err != nil || state == nil {
		return nil
	}
	if len(state.WBSTreeJSON) == 0 {
		return nil
	}
	var tree planning.WBSTree
	if err := json.Unmarshal(state.WBSTreeJSON, &tree); err != nil {
		ac.logger.Warn("failed to unmarshal WBS tree from session", "error", err.Error())
		return nil
	}
	return &tree
}

// agentNodeExecutor implements planning.NodeExecutor using AgentCore.runSimple.
type agentNodeExecutor struct {
	core *AgentCore
}

func (e *agentNodeExecutor) ExecuteNode(ctx context.Context, node planning.WBSNode) (string, error) {
	prompt := fmt.Sprintf("[WBS Step %s: %s]\n%s", node.ID, node.Name, node.Description)
	return e.core.runSimple(ctx, prompt)
}

// agentWBSPersister implements planning.StatePersister using the session store.
type agentWBSPersister struct {
	core *AgentCore
}

func (p *agentWBSPersister) PersistWBS(tree *planning.WBSTree) {
	if p.core.store == nil || p.core.sessionID == "" {
		return
	}
	wbsJSON, err := json.Marshal(tree)
	if err != nil {
		p.core.logger.Warn("failed to marshal WBS tree for persistence", "error", err.Error())
		return
	}
	state := &session.SessionState{
		SessionID:      p.core.sessionID,
		Status:         session.StatusActive,
		Messages:       convertToSessionMessages(p.core.messages),
		CreatedFiles:   p.core.tracker.TrackedFilesSnapshot(),
		RunningProcesses: p.core.tracker.TrackedProcessesSnapshot(),
		WBSTreeJSON:    json.RawMessage(wbsJSON),
		LastActivityAt: time.Now(),
	}
	if err := p.core.store.Save(state); err != nil {
		p.core.logger.Warn("failed to persist WBS state", "error", err.Error())
	}
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

