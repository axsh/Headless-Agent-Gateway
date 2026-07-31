// Package analyzer extracts SystemArtifactEvents from Coding Agent tool call logs.
package analyzer

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
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
	"Write":     {{store.OperationCreate, "path"}, {store.OperationCreate, "file_path"}},
	"StrReplace": {{store.OperationUpdate, "path"}},
	"Delete":    {{store.OperationDelete, "path"}},
	// Claude Code
	"Edit":      {{store.OperationUpdate, "file_path"}, {store.OperationUpdate, "path"}},
	"MultiEdit": {{store.OperationUpdate, "file_path"}, {store.OperationUpdate, "path"}},
}

// ToolCallAnalyzer attaches to a TaskLog and writes SystemArtifactEvents to an ArtifactStore
// whenever a recognized file-writing tool call is observed.
type ToolCallAnalyzer struct {
	st          store.ArtifactStore
	projectRoot string
}

// New creates a ToolCallAnalyzer and registers it as the TaskLog entry handler.
// The analyzer replaces any previously registered handler on tl.
func New(tl *tasklog.TaskLog, s store.ArtifactStore, projectRoot string) *ToolCallAnalyzer {
	a := &ToolCallAnalyzer{
		st:          s,
		projectRoot: filepath.ToSlash(filepath.Clean(projectRoot)),
	}
	tl.SetOnEntry(a.onEntry)
	return a
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

	event := a.analyzeEvent(ev, agentLog.AgentID)
	if event == nil {
		return
	}

	// Fire-and-forget; errors are silently discarded to avoid disrupting the agent stream.
	_ = a.st.SaveSystemArtifactEvent(context.Background(), *event)
}

// analyzeEvent returns a SystemArtifactEvent if ev is a recognized file-write tool_use, else nil.
func (a *ToolCallAnalyzer) analyzeEvent(ev codingagent.StreamEvent, sessionID string) *store.SystemArtifactEvent {
	if ev.Type != codingagent.EventToolUse {
		return nil
	}

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

	key := a.toRelativePath(filePath)
	return &store.SystemArtifactEvent{
		SessionID:  sessionID,
		AgentID:    sessionID, // agentID == sessionID in current agentservice design
		Key:        key,
		ActualPath: filePath,
		Operation:  operation,
		OccurredAt: time.Now(),
		ToolName:   ev.ToolName,
	}
}

// toRelativePath converts an absolute path to a project-root-relative slash path.
// If the path is not under projectRoot, it is returned as-is (slash-normalised).
func (a *ToolCallAnalyzer) toRelativePath(absPath string) string {
	clean := filepath.ToSlash(filepath.Clean(absPath))
	root := a.projectRoot
	if root != "" && strings.HasPrefix(clean, root+"/") {
		return clean[len(root)+1:]
	}
	return clean
}
