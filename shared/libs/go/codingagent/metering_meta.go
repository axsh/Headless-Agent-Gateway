package codingagent

import "strings"

// MeteringMetaSuffix returns ";tern_sid=...;tid=..." for LLMGP correlation.
// Empty fields are omitted. Does not include routing sid=.
func MeteringMetaSuffix(ternSessionID, turnID string) string {
	var b strings.Builder
	if ternSessionID != "" {
		b.WriteString(";tern_sid=")
		b.WriteString(ternSessionID)
	}
	if turnID != "" {
		b.WriteString(";tid=")
		b.WriteString(turnID)
	}
	return b.String()
}
