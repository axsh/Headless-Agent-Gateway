package codex

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

// ToolNameTurnDiff is the System Artifact tool_name for App Server turn/diff/updated (Tier1).
const ToolNameTurnDiff = "turn_diff"

// DiffPathOp is one path change extracted from a unified diff.
type DiffPathOp struct {
	Path string // slash-separated path as in the diff (a/b prefixes stripped)
	Kind string // "add" | "update" | "delete"
}

// ExtractPathsFromUnifiedDiff parses a unified diff into path operations.
// Unparseable hunks are skipped. Returns nil/empty when nothing usable is found.
func ExtractPathsFromUnifiedDiff(diff string) []DiffPathOp {
	if strings.TrimSpace(diff) == "" {
		return nil
	}
	var ops []DiffPathOp
	lines := strings.Split(diff, "\n")
	var oldPath, newPath string
	flush := func() {
		if oldPath == "" && newPath == "" {
			return
		}
		op := classifyDiffPaths(oldPath, newPath)
		oldPath, newPath = "", ""
		if op.Path == "" || op.Kind == "" {
			return
		}
		ops = append(ops, op)
	}
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "--- "):
			flush()
			oldPath = stripDiffPath(strings.TrimPrefix(line, "--- "))
		case strings.HasPrefix(line, "+++ "):
			newPath = stripDiffPath(strings.TrimPrefix(line, "+++ "))
			flush()
		}
	}
	flush()
	return ops
}

func stripDiffPath(raw string) string {
	raw = strings.TrimSpace(raw)
	if i := strings.IndexByte(raw, '\t'); i >= 0 {
		raw = raw[:i]
	}
	if i := strings.Index(raw, " "); i >= 0 {
		// timestamps after path: "b/file.txt 2024-01-01"
		raw = raw[:i]
	}
	raw = filepath.ToSlash(raw)
	if raw == "/dev/null" || raw == "dev/null" {
		return "/dev/null"
	}
	if strings.HasPrefix(raw, "a/") || strings.HasPrefix(raw, "b/") {
		raw = raw[2:]
	}
	return raw
}

func classifyDiffPaths(oldPath, newPath string) DiffPathOp {
	oldNull := oldPath == "" || oldPath == "/dev/null"
	newNull := newPath == "" || newPath == "/dev/null"
	switch {
	case oldNull && !newNull:
		return DiffPathOp{Path: newPath, Kind: "add"}
	case !oldNull && newNull:
		return DiffPathOp{Path: oldPath, Kind: "delete"}
	case !oldNull && !newNull:
		path := newPath
		if path == "" {
			path = oldPath
		}
		return DiffPathOp{Path: path, Kind: "update"}
	default:
		return DiffPathOp{}
	}
}

// TurnDiffStreamEvent builds an EventToolUse with ToolNameTurnDiff.
func TurnDiffStreamEvent(ops []DiffPathOp) *codingagent.StreamEvent {
	if len(ops) == 0 {
		return nil
	}
	if len(ops) == 1 {
		return &codingagent.StreamEvent{
			Type:     codingagent.EventToolUse,
			ToolName: ToolNameTurnDiff,
			ToolInput: map[string]any{
				"path": ops[0].Path,
				"kind": ops[0].Kind,
			},
		}
	}
	changes := make([]any, len(ops))
	for i, op := range ops {
		changes[i] = map[string]any{"path": op.Path, "kind": op.Kind}
	}
	return &codingagent.StreamEvent{
		Type:     codingagent.EventToolUse,
		ToolName: ToolNameTurnDiff,
		ToolInput: map[string]any{
			"changes": changes,
		},
	}
}

// ParseTurnDiffUpdatedParams builds EventToolUse turn_diff from App Server params JSON:
// {"threadId":"...","turnId":"...","diff":"..."}.
func ParseTurnDiffUpdatedParams(params json.RawMessage) *codingagent.StreamEvent {
	if len(params) == 0 {
		return nil
	}
	var p struct {
		ThreadID string `json:"threadId"`
		TurnID   string `json:"turnId"`
		Diff     string `json:"diff"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil
	}
	ops := ExtractPathsFromUnifiedDiff(p.Diff)
	ev := TurnDiffStreamEvent(ops)
	if ev == nil {
		return nil
	}
	ev.TurnID = p.TurnID
	if p.ThreadID != "" {
		ev.SessionID = p.ThreadID
	}
	return ev
}
