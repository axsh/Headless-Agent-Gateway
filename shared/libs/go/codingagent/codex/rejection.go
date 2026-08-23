package codex

import (
	"strings"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

// ExtractSandboxRejectionContent returns rejection text from accumulated stderr.
func ExtractSandboxRejectionContent(stderr string) string {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return ""
	}
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lineLower := strings.ToLower(line)
		if strings.Contains(lineLower, "rejected(") ||
			strings.Contains(lineLower, "rm -f style commands are not permitted") {
			return line
		}
	}
	if codingagent.IsSandboxRejection(stderr) {
		return stderr
	}
	return stderr
}
