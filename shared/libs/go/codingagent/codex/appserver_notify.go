package codex

import (
	"encoding/json"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

// ParseAppServerNotification parses one JSON-RPC notification line from Codex App Server.
// Supports method "turn/diff/updated". Returns nil for unrelated methods or parse failures.
func ParseAppServerNotification(line string) *codingagent.StreamEvent {
	line = trimJSONL(line)
	if line == "" {
		return nil
	}
	var envelope struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal([]byte(line), &envelope); err != nil {
		return nil
	}
	switch envelope.Method {
	case "turn/diff/updated":
		return ParseTurnDiffUpdatedParams(envelope.Params)
	default:
		return nil
	}
}

func trimJSONL(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\r' || s[0] == '\n') {
		s = s[1:]
	}
	for len(s) > 0 {
		last := s[len(s)-1]
		if last == ' ' || last == '\t' || last == '\r' || last == '\n' {
			s = s[:len(s)-1]
			continue
		}
		break
	}
	return s
}
