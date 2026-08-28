package codingagent

import (
	"encoding/json"
	"fmt"
)

// File change collector algorithm IDs (API / docs).
//
// Tier meanings (agent-native vs inferred vs external):
//   - structured_tool (Tier1): Coding Agent native file-change surfaces
//     (Codex: turn/diff → tool_name turn_diff, plus file_change; Claude/Cursor: Write/Edit/…).
//   - shell_parser (Tier2): Infer paths from native non-file tools (Bash, command_execution).
//   - workdir_reconcile (Tier3): External observation (git diff / directory snapshot).
const (
	CollectorStructuredTool   = "structured_tool"
	CollectorShellParser      = "shell_parser"
	CollectorWorkdirReconcile = "workdir_reconcile"
)

// FileChangeCollectors is the resolved per-session System Artifact collection config.
// All three keys are always serialized (no omitempty on the bool fields).
// structured_tool gates the entire Tier1 surface including Codex turn_diff.
type FileChangeCollectors struct {
	StructuredTool   bool `json:"structured_tool"`
	ShellParser      bool `json:"shell_parser"`
	WorkdirReconcile bool `json:"workdir_reconcile"`
}

// DefaultFileChangeCollectors returns Tier1–2 ON / Tier3 OFF.
func DefaultFileChangeCollectors() FileChangeCollectors {
	return FileChangeCollectors{
		StructuredTool:   true,
		ShellParser:      true,
		WorkdirReconcile: false,
	}
}

// EffectiveFileChangeCollectors returns *c, or defaults when c is nil (legacy records).
func EffectiveFileChangeCollectors(c *FileChangeCollectors) FileChangeCollectors {
	if c == nil {
		return DefaultFileChangeCollectors()
	}
	return *c
}

// ResolveFileChangeCollectors applies per-key defaults (partial override).
// Empty / null raw yields DefaultFileChangeCollectors.
// Unknown keys or non-boolean values return an error suitable for HTTP 400.
func ResolveFileChangeCollectors(raw json.RawMessage) (FileChangeCollectors, error) {
	out := DefaultFileChangeCollectors()
	if len(raw) == 0 || string(raw) == "null" {
		return out, nil
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return FileChangeCollectors{}, fmt.Errorf("file_change_collectors must be an object: %w", err)
	}

	allowed := map[string]struct{}{
		CollectorStructuredTool:   {},
		CollectorShellParser:      {},
		CollectorWorkdirReconcile: {},
	}
	for k := range obj {
		if _, ok := allowed[k]; !ok {
			return FileChangeCollectors{}, fmt.Errorf(
				"unknown file_change_collectors key: %q (allowed: %s, %s, %s)",
				k, CollectorStructuredTool, CollectorShellParser, CollectorWorkdirReconcile,
			)
		}
	}

	if v, ok := obj[CollectorStructuredTool]; ok {
		b, err := unmarshalCollectorBool(CollectorStructuredTool, v)
		if err != nil {
			return FileChangeCollectors{}, err
		}
		out.StructuredTool = b
	}
	if v, ok := obj[CollectorShellParser]; ok {
		b, err := unmarshalCollectorBool(CollectorShellParser, v)
		if err != nil {
			return FileChangeCollectors{}, err
		}
		out.ShellParser = b
	}
	if v, ok := obj[CollectorWorkdirReconcile]; ok {
		b, err := unmarshalCollectorBool(CollectorWorkdirReconcile, v)
		if err != nil {
			return FileChangeCollectors{}, err
		}
		out.WorkdirReconcile = b
	}
	return out, nil
}

func unmarshalCollectorBool(key string, raw json.RawMessage) (bool, error) {
	var b bool
	if err := json.Unmarshal(raw, &b); err != nil {
		return false, fmt.Errorf("file_change_collectors.%s must be a boolean", key)
	}
	return b, nil
}
