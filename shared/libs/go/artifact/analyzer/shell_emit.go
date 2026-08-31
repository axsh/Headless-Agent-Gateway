package analyzer

import (
	"os"

	"github.com/axsh/arctic-tern/shared/libs/go/artifact/store"
)

const (
	shellExecStatusKey       = "execution_status"
	shellExecStatusStarted   = "started"
	shellExecStatusCompleted = "completed"
)

// pendingShellOps holds Tier2 shell parse results until tool_result (or equivalent).
type pendingShellOps struct {
	toolName      string
	turnID        string
	correlationID string
	ops           []ParsedFileOp
}

// pathExists reports whether absPath exists on the filesystem.
// Any Stat error (including permission denied) is treated as non-existent
// so Tier2 prefers suppressing false positives over recording uncertain paths.
func pathExists(absPath string) bool {
	if absPath == "" {
		return false
	}
	_, err := os.Stat(absPath)
	return err == nil
}

// shouldEmitShellOp applies the Tier2 existence gate (R2).
// delete (D-A): always emit. create/update: only when resolved path exists.
func (a *ToolCallAnalyzer) shouldEmitShellOp(sessionID, path, operation string) bool {
	if operation == store.OperationDelete {
		return true
	}
	resolved := a.resolvePath(path, sessionID)
	return pathExists(resolved)
}

// emitShellOps builds SystemArtifactEvents for shell-parsed ops after the existence gate.
func (a *ToolCallAnalyzer) emitShellOps(sessionID, turnID, correlationID, toolName string, ops []ParsedFileOp) []*store.SystemArtifactEvent {
	var out []*store.SystemArtifactEvent
	for _, op := range ops {
		if !a.shouldEmitShellOp(sessionID, op.Path, op.Operation) {
			continue
		}
		if event := a.buildEvent(sessionID, turnID, correlationID, toolName, op.Path, op.Operation); event != nil {
			out = append(out, event)
		}
	}
	return out
}

func pendingShellKey(sessionID, toolCallID string) string {
	return sessionID + "\x00" + toolCallID
}

func (a *ToolCallAnalyzer) stashPendingShell(sessionID, toolCallID string, pending pendingShellOps) {
	a.pendingMu.Lock()
	defer a.pendingMu.Unlock()
	if toolCallID != "" {
		if a.pendingShellByCall == nil {
			a.pendingShellByCall = make(map[string]pendingShellOps)
		}
		a.pendingShellByCall[pendingShellKey(sessionID, toolCallID)] = pending
		return
	}
	if a.pendingShellFIFO == nil {
		a.pendingShellFIFO = make(map[string][]pendingShellOps)
	}
	a.pendingShellFIFO[sessionID] = append(a.pendingShellFIFO[sessionID], pending)
}

func (a *ToolCallAnalyzer) takePendingShell(sessionID, toolCallID string) (pendingShellOps, bool) {
	a.pendingMu.Lock()
	defer a.pendingMu.Unlock()
	if toolCallID != "" && a.pendingShellByCall != nil {
		k := pendingShellKey(sessionID, toolCallID)
		if p, ok := a.pendingShellByCall[k]; ok {
			delete(a.pendingShellByCall, k)
			return p, true
		}
	}
	if a.pendingShellFIFO == nil {
		return pendingShellOps{}, false
	}
	q := a.pendingShellFIFO[sessionID]
	if len(q) == 0 {
		return pendingShellOps{}, false
	}
	p := q[0]
	a.pendingShellFIFO[sessionID] = q[1:]
	return p, true
}
