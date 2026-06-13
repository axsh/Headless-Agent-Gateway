package wayfinder

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/axsh/arctic-tern/codingagent"
	"github.com/axsh/arctic-tern/logger"
	"github.com/axsh/arctic-tern/wayfinder/planning"
	"github.com/axsh/arctic-tern/wayfinder/session"
	"github.com/axsh/arctic-tern/wayfinder/subagent"
	"github.com/axsh/arctic-tern/wayfinder/tools"
)

const (
	// maxIterations prevents infinite tool-calling loops.
	maxIterations = 25
)

// SubagentRunner is the interface for subagent execution.
// This breaks the cyclic dependency between wayfinder and wayfinder/subagent.
type SubagentRunner interface {
	Execute(ctx context.Context, parentMessages []subagent.ParentMessage, toolName string, toolInput map[string]any) (string, error)
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
	runner         subagent.AgentRunner // Runner for creating child sessions (WBS)
	subagentLLM    subagent.LLMClient   // LLM client for subagent summarization
	emitter        *EventEmitter        // Streaming event emitter (nil = no-op)
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

		// Use streaming if the client supports it.
		var resp *LLMResponse
		var err error
		streamed := false
		if streamClient, ok := ac.llm.(StreamingLLMClient); ok && ac.emitter != nil {
			onDelta := func(delta string) {
				ac.emitter.Emit(codingagent.StreamEvent{
					Type:    codingagent.EventText,
					Content: delta,
				})
			}
			resp, err = streamClient.GenerateMessageStream(ctx, ac.config.LogicalModel, ac.messages, toolDefs, onDelta)
			streamed = true
		} else {
			resp, err = ac.llm.GenerateMessage(ctx, ac.config.LogicalModel, ac.messages, toolDefs)
		}
		if err != nil {
			ac.logger.Error("LLM call failed", "iteration", iteration, "error", err.Error())
			ac.saveSession(session.StatusFailed)
			return "", fmt.Errorf("agent core: LLM call failed at iteration %d: %w", iteration, err)
		}

		// If no tool calls, return the text response.
		if len(resp.ToolCalls) == 0 {
			ac.logger.Debug("agent core completed", "iteration", iteration, "response_len", len(resp.Content))
			// Only emit full text for non-streaming (streaming already emitted deltas).
			if !streamed {
				ac.emitter.Emit(codingagent.StreamEvent{
					Type:    codingagent.EventText,
					Content: resp.Content,
				})
			}
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
			ac.emitter.Emit(codingagent.StreamEvent{
				Type:     codingagent.EventToolUse,
				Content:  tc.Name,
				ToolName: tc.Name,
				ToolInput: tc.Input,
			})
			result := ac.executeTool(ctx, tc)
			ac.emitter.Emit(codingagent.StreamEvent{
				Type:    codingagent.EventToolResult,
				Content: result,
			})
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

// summarizationSystemPrompt is the system prompt for LLM-based context summarization.
const summarizationSystemPrompt = `You are a conversation summarizer. Summarize the following conversation concisely.
Rules:
- Preserve the meaning and intent of user requests and assistant responses.
- MUST preserve all tool call names and their outcomes (success/failure/key results).
- MUST preserve specific file paths, command outputs, and operation results.
- Keep causal relationships between user requests and assistant actions.
- Output in the same language as the conversation.
- Be concise but do not lose important facts.`

// defaultSummarizer creates a summary of old messages using the LLM.
// Falls back to structured format if LLM call fails.
func (ac *AgentCore) defaultSummarizer(msgs []session.Message) (string, error) {
	conversationLog := ac.buildConversationLog(msgs)
	summaryPrompt := []ChatMessage{
		{Role: "system", Content: summarizationSystemPrompt},
		{Role: "user", Content: "Summarize this conversation:\n\n" + conversationLog},
	}

	resp, err := ac.llm.GenerateMessage(context.Background(), ac.config.LogicalModel, summaryPrompt, nil)
	if err != nil {
		ac.logger.Warn("LLM summarization failed, using structured fallback", "error", err.Error())
		return ac.structuredFallbackSummary(msgs), nil
	}
	return resp.Content, nil
}

// buildConversationLog converts a message list into structured text for summarization.
func (ac *AgentCore) buildConversationLog(msgs []session.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		switch m.Role {
		case "user":
			b.WriteString(fmt.Sprintf("USER: %s\n", m.Content))
		case "assistant":
			b.WriteString(fmt.Sprintf("ASSISTANT: %s\n", m.Content))
			for _, tc := range m.ToolCalls {
				b.WriteString(fmt.Sprintf("  [TOOL CALL: %s (id=%s)]\n", tc.Name, tc.ID))
			}
		case "tool":
			b.WriteString(fmt.Sprintf("  [TOOL RESULT (id=%s): %s]\n", m.ToolCallID, m.Content))
		}
	}
	return b.String()
}

// structuredFallbackSummary produces a structured summary when LLM is unavailable.
// Unlike simple string clipping, it preserves tool call structure and results.
func (ac *AgentCore) structuredFallbackSummary(msgs []session.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		switch m.Role {
		case "user":
			b.WriteString("USER: " + truncateWithEllipsis(m.Content, 300) + "\n")
		case "assistant":
			b.WriteString("ASSISTANT: " + truncateWithEllipsis(m.Content, 300) + "\n")
			for _, tc := range m.ToolCalls {
				b.WriteString(fmt.Sprintf("  [TOOL: %s]\n", tc.Name))
			}
		case "tool":
			b.WriteString("  [RESULT: " + truncateWithEllipsis(m.Content, 150) + "]\n")
		}
	}
	return b.String()
}

// truncateWithEllipsis truncates a string to maxLen and adds ellipsis.
func truncateWithEllipsis(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
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

// SetEmitter configures the event emitter for streaming.
func (ac *AgentCore) SetEmitter(emitter *EventEmitter) {
	ac.emitter = emitter
}

// SetSubagentExecutor configures the subagent executor for delegating heavy tool calls.
func (ac *AgentCore) SetSubagentExecutor(exec SubagentRunner) {
	ac.subagent = exec
}

// executeTool runs a single tool call and returns the result string.
func (ac *AgentCore) executeTool(ctx context.Context, tc ToolCall) string {
	ac.logger.Debug("executing tool", "tool", tc.Name, "id", tc.ID)

	// Delegate execute_command to subagent if configured and enabled.
	if tc.Name == "execute_command" && ac.subagent != nil && ac.config.EnableSubagent {
		ac.logger.Debug("delegating to subagent", "tool", tc.Name)

		// Convert ChatMessage to ParentMessage for subagent.
		parentMsgs := make([]subagent.ParentMessage, 0, len(ac.messages))
		for _, m := range ac.messages {
			parentMsgs = append(parentMsgs, subagent.ParentMessage{
				Role:    m.Role,
				Content: m.Content,
			})
		}

		result, err := ac.subagent.Execute(ctx, parentMsgs, tc.Name, tc.Input)
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

// SetRunner configures the AgentRunner for child session creation.
func (ac *AgentCore) SetRunner(runner subagent.AgentRunner) {
	ac.runner = runner
}

// SetSubagentLLM configures the LLM client for subagent operations.
func (ac *AgentCore) SetSubagentLLM(llm subagent.LLMClient) {
	ac.subagentLLM = llm
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
	var nodeExec planning.NodeExecutor

	if ac.runner != nil && ac.subagentLLM != nil && ac.config.EnableSubagent {
		// Child session mode: each WBS node runs in an independent child session.
		childCfg := &subagent.AgentRunnerConfig{
			WorkDir:             ac.config.WorkDir,
			SessionDir:          ac.config.SessionDir,
			LogicalModel:        ac.config.LogicalModel,
			AllowedPathPatterns: ac.config.AllowedPathPatterns,
		}
		nodeExec = &agentNodeExecutor{
			parentSessionID: ac.sessionID,
			childConfig:     childCfg,
			runner:          ac.runner,
			llm:             ac.subagentLLM,
			summarizer:      subagent.NewSummarizer(ac.subagentLLM),
			logger:          ac.logger,
		}
	} else {
		// Fallback: run in parent AgentCore (existing behavior).
		nodeExec = &agentNodeExecutorSimple{core: ac}
	}

	persister := &agentWBSPersister{core: ac}

	// Bridge emitter to planning.EventEmitFunc callback.
	var orchOpts []planning.OrchestratorOption
	if ac.emitter != nil {
		emitFn := func(eventType string, content string) {
			ac.emitter.Emit(codingagent.StreamEvent{
				Type:    codingagent.EventType(eventType),
				Content: content,
			})
		}
		orchOpts = append(orchOpts, planning.WithEventEmitter(emitFn))
	}

	orch := planning.NewWBSOrchestrator(nodeExec, persister, ac.logger, orchOpts...)
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

// agentNodeExecutor executes WBS nodes in child sessions via AgentRunner.
type agentNodeExecutor struct {
	parentSessionID string
	childConfig     *subagent.AgentRunnerConfig
	runner          subagent.AgentRunner
	llm             subagent.LLMClient
	summarizer      *subagent.Summarizer
	logger          logger.Logger
}

func (e *agentNodeExecutor) ExecuteNode(ctx context.Context, node planning.WBSNode) (string, error) {
	prompt := fmt.Sprintf("[WBS Step %s: %s]\n%s", node.ID, node.Name, node.Description)
	childSessionID := fmt.Sprintf("%s-wbs-%s", e.parentSessionID, node.ID)

	e.logger.Debug("executing WBS node in child session",
		"node_id", node.ID, "child_session", childSessionID)

	childResult, err := e.runner.RunChild(ctx, e.childConfig, childSessionID, e.llm, e.logger, prompt)
	if err != nil {
		return "", err
	}

	// Summarize child result for parent.
	hints := &subagent.Hints{Objective: node.Name, Context: node.Description}
	summary, err := e.summarizer.SummarizeForParent(ctx, hints, childResult)
	if err != nil {
		e.logger.Warn("WBS node summarization failed, using raw result", "error", err.Error())
		return childResult, nil
	}
	return summary, nil
}

// agentNodeExecutorSimple is the fallback executor using parent AgentCore.
type agentNodeExecutorSimple struct {
	core *AgentCore
}

func (e *agentNodeExecutorSimple) ExecuteNode(ctx context.Context, node planning.WBSNode) (string, error) {
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

