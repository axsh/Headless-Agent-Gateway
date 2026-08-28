// Package analyzer extracts SystemArtifactEvents from Coding Agent tool call logs.
package analyzer

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/artifact/store"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/tasklog"
)

// toolMapping maps a tool name to its file operation and the ToolInput field holding the path.
type toolMapping struct {
	operation string // store.OperationCreate | Update | Delete
	pathField string // key in ToolInput that contains the file path
}

// defaultToolMappings covers Cursor Agent and Claude Code tools.
// For "Write", both "path" (Cursor) and "file_path" (Claude Code) are tried.
var defaultToolMappings = map[string][]toolMapping{
	// Cursor Agent
	"Write":      {{store.OperationCreate, "path"}, {store.OperationCreate, "file_path"}},
	"StrReplace": {{store.OperationUpdate, "path"}},
	"Delete":     {{store.OperationDelete, "path"}},
	// Claude Code
	"Edit":         {{store.OperationUpdate, "file_path"}, {store.OperationUpdate, "path"}},
	"MultiEdit":    {{store.OperationUpdate, "file_path"}, {store.OperationUpdate, "path"}},
	"NotebookEdit": {{store.OperationUpdate, "notebook_path"}, {store.OperationUpdate, "file_path"}},
}

// WorkDirResolver returns the agent working directory for a session ID.
type WorkDirResolver func(sessionID string) string

// CollectorConfigResolver returns the resolved file-change collector config for a session.
type CollectorConfigResolver func(sessionID string) codingagent.FileChangeCollectors

// ToolCallAnalyzer attaches to a TaskLog and writes SystemArtifactEvents to an ArtifactStore
// whenever a recognized file-writing tool call is observed.
type ToolCallAnalyzer struct {
	st                store.ArtifactStore
	projectRoot       string
	workDirResolver   WorkDirResolver
	collectorResolver CollectorConfigResolver
	seenMu            sync.Mutex
	// seenTier1Keys tracks session|turn|key claimed by Tier1 (first-wins).
	seenTier1Keys map[string]struct{}
}

// New creates a ToolCallAnalyzer and registers it as the TaskLog entry handler.
// workDirResolver may be nil; when set, relative tool paths are resolved against the session work dir.
// collectorResolver may be nil; when nil, DefaultFileChangeCollectors is used.
func New(tl *tasklog.TaskLog, s store.ArtifactStore, projectRoot string, workDirResolver WorkDirResolver, collectorResolver CollectorConfigResolver) *ToolCallAnalyzer {
	a := &ToolCallAnalyzer{
		st:                s,
		projectRoot:       filepath.ToSlash(filepath.Clean(projectRoot)),
		workDirResolver:   workDirResolver,
		collectorResolver: collectorResolver,
		seenTier1Keys:     make(map[string]struct{}),
	}
	tl.SetOnEntry(a.onEntry)
	return a
}

func (a *ToolCallAnalyzer) collectorsFor(sessionID string) codingagent.FileChangeCollectors {
	if a.collectorResolver != nil {
		return a.collectorResolver(sessionID)
	}
	return codingagent.DefaultFileChangeCollectors()
}

// onEntry is invoked synchronously for each new TaskLog entry.
func (a *ToolCallAnalyzer) onEntry(e tasklog.Entry) {
	agentLog, ok := e.(*tasklog.AgentLogEntry)
	if !ok || agentLog.Phase != "send" {
		return
	}

	var ev codingagent.StreamEvent
	if err := json.Unmarshal([]byte(agentLog.Body), &ev); err != nil {
		return
	}

	events := a.analyzeEvents(ev, agentLog.AgentID, ev.TurnID, ev.CorrelationID)
	for _, event := range events {
		if event != nil {
			_ = a.st.SaveSystemArtifactEvent(context.Background(), *event)
		}
	}
}

// analyzeEvents returns SystemArtifactEvents for recognized file-write tool_use events.
func (a *ToolCallAnalyzer) analyzeEvents(ev codingagent.StreamEvent, sessionID, turnID, correlationID string) []*store.SystemArtifactEvent {
	if ev.Type != codingagent.EventToolUse {
		return nil
	}

	cfg := a.collectorsFor(sessionID)

	switch ev.ToolName {
	case "file_change", "turn_diff", ToolNameTurnFiles:
		if !cfg.StructuredTool {
			return nil
		}
		return a.analyzeFileChange(ev, sessionID, turnID, correlationID)
	case "command_execution", "Bash", "shell", "shell_command":
		if !cfg.ShellParser {
			return nil
		}
		return a.analyzeShellTool(ev, sessionID, turnID, correlationID)
	}

	if !cfg.StructuredTool {
		return nil
	}
	if event := a.analyzeMappedTool(ev, sessionID, turnID, correlationID); event != nil {
		return []*store.SystemArtifactEvent{event}
	}
	return nil
}

func (a *ToolCallAnalyzer) analyzeMappedTool(ev codingagent.StreamEvent, sessionID, turnID, correlationID string) *store.SystemArtifactEvent {
	mappings, ok := defaultToolMappings[ev.ToolName]
	if !ok {
		return nil
	}

	var filePath, operation string
	for _, m := range mappings {
		if p, ok := ev.ToolInput[m.pathField].(string); ok && p != "" {
			filePath = p
			operation = m.operation
			break
		}
	}
	if filePath == "" {
		return nil
	}
	return a.buildEvent(sessionID, turnID, correlationID, ev.ToolName, filePath, operation)
}

func kindToOperation(kind string) string {
	switch kind {
	case "add", "create":
		return store.OperationCreate
	case "update":
		return store.OperationUpdate
	case "delete":
		return store.OperationDelete
	default:
		return ""
	}
}

func (a *ToolCallAnalyzer) analyzeFileChange(ev codingagent.StreamEvent, sessionID, turnID, correlationID string) []*store.SystemArtifactEvent {
	var entries []struct {
		path string
		kind string
	}

	if path, ok := ev.ToolInput["path"].(string); ok && path != "" {
		kind, _ := ev.ToolInput["kind"].(string)
		entries = append(entries, struct {
			path string
			kind string
		}{path, kind})
	} else if raw, ok := ev.ToolInput["changes"].([]any); ok {
		for _, item := range raw {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			path, _ := m["path"].(string)
			kind, _ := m["kind"].(string)
			if path != "" {
				entries = append(entries, struct {
					path string
					kind string
				}{path, kind})
			}
		}
	}

	var out []*store.SystemArtifactEvent
	for _, e := range entries {
		op := kindToOperation(e.kind)
		if op == "" {
			continue
		}
		if event := a.buildEvent(sessionID, turnID, correlationID, ev.ToolName, e.path, op); event != nil {
			out = append(out, event)
		}
	}
	return out
}

func (a *ToolCallAnalyzer) analyzeShellTool(ev codingagent.StreamEvent, sessionID, turnID, correlationID string) []*store.SystemArtifactEvent {
	cmd := ExtractShellCommand(ev.ToolName, ev.ToolInput)
	if cmd == "" {
		return nil
	}
	ops := ParseShellCommand(cmd)
	if len(ops) == 0 {
		return nil
	}
	var out []*store.SystemArtifactEvent
	for _, op := range ops {
		if event := a.buildEvent(sessionID, turnID, correlationID, ev.ToolName, op.Path, op.Operation); event != nil {
			out = append(out, event)
		}
	}
	return out
}

func (a *ToolCallAnalyzer) buildEvent(sessionID, turnID, correlationID, toolName, filePath, operation string) *store.SystemArtifactEvent {
	if filePath == "" || operation == "" {
		return nil
	}
	resolved := a.resolvePath(filePath, sessionID)
	key := a.toRelativePath(resolved, sessionID)

	tier1 := isTier1ToolName(toolName)
	if tier1 {
		if !a.tryClaimTier1Key(sessionID, turnID, key) {
			return nil
		}
	} else if a.hasTier1Key(sessionID, turnID, key) {
		// Tier2 must not duplicate a path already claimed by Tier1 in this turn.
		return nil
	}

	return &store.SystemArtifactEvent{
		SessionID:     sessionID,
		AgentID:       sessionID,
		TurnID:        turnID,
		CorrelationID: correlationID,
		Key:           key,
		ActualPath:    resolved,
		Operation:     operation,
		OccurredAt:    time.Now(),
		ToolName:      toolName,
	}
}

func isTier1ToolName(toolName string) bool {
	switch toolName {
	case "file_change", "turn_diff", ToolNameTurnFiles, "Write", "Edit", "MultiEdit", "NotebookEdit", "StrReplace", "Delete":
		return true
	default:
		return false
	}
}

func tier1SeenKey(sessionID, turnID, key string) string {
	return sessionID + "\x00" + turnID + "\x00" + key
}

func (a *ToolCallAnalyzer) tryClaimTier1Key(sessionID, turnID, key string) bool {
	a.seenMu.Lock()
	defer a.seenMu.Unlock()
	k := tier1SeenKey(sessionID, turnID, key)
	if _, ok := a.seenTier1Keys[k]; ok {
		return false
	}
	a.seenTier1Keys[k] = struct{}{}
	return true
}

func (a *ToolCallAnalyzer) hasTier1Key(sessionID, turnID, key string) bool {
	a.seenMu.Lock()
	defer a.seenMu.Unlock()
	_, ok := a.seenTier1Keys[tier1SeenKey(sessionID, turnID, key)]
	return ok
}

// resolvePath converts agent-reported paths to absolute filesystem paths.
func (a *ToolCallAnalyzer) resolvePath(filePath, sessionID string) string {
	clean := filepath.Clean(filePath)
	if filepath.IsAbs(clean) {
		return clean
	}
	if a.workDirResolver != nil {
		if wd := a.workDirResolver(sessionID); wd != "" {
			return filepath.Join(wd, clean)
		}
	}
	if a.projectRoot != "" {
		return filepath.Join(a.projectRoot, clean)
	}
	return clean
}

// toRelativePath converts an absolute path to a project-root-relative slash path.
// If the path is not under projectRoot, it falls back to workDir-relative or basename.
func (a *ToolCallAnalyzer) toRelativePath(absPath, sessionID string) string {
	clean := filepath.ToSlash(filepath.Clean(absPath))
	root := a.projectRoot
	if root != "" && strings.HasPrefix(clean, root+"/") {
		return clean[len(root)+1:]
	}
	if a.workDirResolver != nil {
		if wd := a.workDirResolver(sessionID); wd != "" {
			wdSlash := filepath.ToSlash(filepath.Clean(wd))
			if strings.HasPrefix(clean, wdSlash+"/") || clean == wdSlash {
				rel := strings.TrimPrefix(clean, wdSlash+"/")
				if rel != "" {
					return rel
				}
			}
		}
	}
	if base := filepath.Base(clean); base != "." && base != "/" {
		return base
	}
	return clean
}

// PathHelpers exposes path normalization for reconciliation (Tier 3).
func (a *ToolCallAnalyzer) PathHelpers() (WorkDirResolver, string) {
	return a.workDirResolver, a.projectRoot
}

// ResolvePathForSession resolves a path using the analyzer's work dir / project root rules.
func (a *ToolCallAnalyzer) ResolvePathForSession(filePath, sessionID string) (key, absPath string) {
	abs := a.resolvePath(filePath, sessionID)
	return a.toRelativePath(abs, sessionID), abs
}
