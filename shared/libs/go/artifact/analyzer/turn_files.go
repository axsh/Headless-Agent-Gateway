package analyzer

import (
	"context"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

// ToolNameTurnFiles is the Tern-synthesized Claude turn aggregate (Tier1).
// Distinct from Codex ToolNameTurnDiff (unified diff).
const ToolNameTurnFiles = "turn_files"

// TurnFileOp describes one path change collected from Claude Tier1 tools.
type TurnFileOp struct {
	Path string
	Kind string // "add" | "update" | "delete"
}

// CollectClaudeTier1OpsFromStream extracts Write/Edit/MultiEdit/NotebookEdit/StrReplace/Delete
// ops from StreamEvents. Bash is intentionally excluded.
func CollectClaudeTier1OpsFromStream(events []codingagent.StreamEvent) []TurnFileOp {
	var ops []TurnFileOp
	for _, ev := range events {
		if ev.Type != codingagent.EventToolUse {
			continue
		}
		kind := ""
		switch ev.ToolName {
		case "Write":
			kind = "add"
		case "Delete":
			kind = "delete"
		case "Edit", "MultiEdit", "NotebookEdit", "StrReplace":
			kind = "update"
		default:
			continue
		}
		path := ""
		for _, key := range []string{"file_path", "path", "notebook_path"} {
			if p, ok := ev.ToolInput[key].(string); ok && p != "" {
				path = p
				break
			}
		}
		if path == "" {
			continue
		}
		ops = append(ops, TurnFileOp{Path: path, Kind: kind})
	}
	return ops
}

// SynthesizeTurnFilesEvent builds EventToolUse with ToolNameTurnFiles.
// Returns nil if ops is empty.
func SynthesizeTurnFilesEvent(ops []TurnFileOp) *codingagent.StreamEvent {
	if len(ops) == 0 {
		return nil
	}
	ev := &codingagent.StreamEvent{
		Type:     codingagent.EventToolUse,
		ToolName: ToolNameTurnFiles,
		ToolInput: map[string]any{},
	}
	if len(ops) == 1 {
		ev.ToolInput["path"] = ops[0].Path
		ev.ToolInput["kind"] = ops[0].Kind
		return ev
	}
	changes := make([]any, 0, len(ops))
	for _, op := range ops {
		changes = append(changes, map[string]any{
			"path": op.Path,
			"kind": op.Kind,
		})
	}
	ev.ToolInput["changes"] = changes
	return ev
}

// FlushTurnFiles analyzes a synthesized turn_files event into the store
// (respecting structured_tool gate and Tier1 first-wins).
func (a *ToolCallAnalyzer) FlushTurnFiles(ctx context.Context, sessionID, turnID, correlationID string, ops []TurnFileOp) error {
	if a == nil || a.st == nil {
		return nil
	}
	ev := SynthesizeTurnFilesEvent(ops)
	if ev == nil {
		return nil
	}
	ev.TurnID = turnID
	ev.CorrelationID = correlationID
	for _, event := range a.analyzeEvents(*ev, sessionID, turnID, correlationID) {
		if event == nil {
			continue
		}
		if err := a.st.SaveSystemArtifactEvent(ctx, *event); err != nil {
			return err
		}
	}
	return nil
}
