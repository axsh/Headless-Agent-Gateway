package analyzer

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/axsh/arctic-tern/shared/libs/go/artifact/store"
)

// ParsedFileOp is a file operation extracted from a shell command string.
type ParsedFileOp struct {
	Path      string
	Operation string // store.OperationCreate | store.OperationUpdate | store.OperationDelete
}

var (
	reAppendRedirect = regexp.MustCompile(`>>\s*([^\s|;&]+)`)
	reTee            = regexp.MustCompile(`\btee\b(?:\s+-[^\s]+)*\s+([^\s|;&]+)`)
	reHeredoc        = regexp.MustCompile(`(?i)cat\s+<<[^\n>]*>\s*(\S+)`)
	reCp             = regexp.MustCompile(`\bcp\b(?:\s+-[^\s]+)*\s+\S+\s+(\S+)`)
	reMv             = regexp.MustCompile(`\bmv\b(?:\s+-[^\s]+)*\s+\S+\s+(\S+)`)
	reTouch          = regexp.MustCompile(`\btouch\b(?:\s+-[^\s]+)*\s+(\S+)`)
	reRm             = regexp.MustCompile(`\brm\b(?:\s+-[^\s]+)*\s+(\S+)`)
	reSetContent     = regexp.MustCompile(`(?i)Set-Content\b[^;]*-Path\s+(\S+)`)
	reOutFile        = regexp.MustCompile(`(?i)Out-File\s+(\S+)`)
)

// ParseShellCommand extracts file operations from a one-line shell command.
// Returns empty slice when no file operation can be determined (conservative).
func ParseShellCommand(command string) []ParsedFileOp {
	cmd := strings.TrimSpace(command)
	if cmd == "" {
		return nil
	}

	seen := make(map[string]string)

	add := func(path, op string) {
		path = stripQuotes(path)
		if path == "" {
			return
		}
		if existing, ok := seen[path]; ok {
			if priority(op) > priority(existing) {
				seen[path] = op
			}
			return
		}
		seen[path] = op
	}

	for _, m := range reAppendRedirect.FindAllStringSubmatch(cmd, -1) {
		add(m[1], store.OperationUpdate)
	}
	withoutAppend := reAppendRedirect.ReplaceAllString(cmd, "")
	for _, m := range regexp.MustCompile(`>\s*([^\s|;&]+)`).FindAllStringSubmatch(withoutAppend, -1) {
		add(m[1], store.OperationCreate)
	}
	for _, m := range reTee.FindAllStringSubmatch(cmd, -1) {
		add(m[1], store.OperationCreate)
	}
	for _, m := range reHeredoc.FindAllStringSubmatch(cmd, -1) {
		add(m[1], store.OperationCreate)
	}
	for _, m := range reCp.FindAllStringSubmatch(cmd, -1) {
		add(m[1], store.OperationCreate)
	}
	for _, m := range reMv.FindAllStringSubmatch(cmd, -1) {
		add(m[1], store.OperationUpdate)
	}
	for _, m := range reTouch.FindAllStringSubmatch(cmd, -1) {
		add(m[1], store.OperationCreate)
	}
	for _, m := range reRm.FindAllStringSubmatch(cmd, -1) {
		add(m[1], store.OperationDelete)
	}
	for _, m := range reSetContent.FindAllStringSubmatch(cmd, -1) {
		add(m[1], store.OperationCreate)
	}
	for _, m := range reOutFile.FindAllStringSubmatch(cmd, -1) {
		add(m[1], store.OperationCreate)
	}

	if len(seen) == 0 {
		return nil
	}

	out := make([]ParsedFileOp, 0, len(seen))
	for path, op := range seen {
		out = append(out, ParsedFileOp{Path: path, Operation: op})
	}
	return out
}

// ExtractShellCommand returns the shell command string from tool input for
// command_execution, Bash, or legacy Codex shell/shell_command tools.
func ExtractShellCommand(toolName string, input map[string]any) string {
	switch toolName {
	case "command_execution", "Bash":
		if cmd, ok := input["command"].(string); ok {
			return cmd
		}
	case "shell", "shell_command":
		if args, ok := input["arguments"].(string); ok && args != "" {
			var parsed struct {
				Command string `json:"command"`
			}
			if err := json.Unmarshal([]byte(args), &parsed); err == nil && parsed.Command != "" {
				return parsed.Command
			}
		}
	}
	return ""
}

func stripQuotes(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

func priority(op string) int {
	switch op {
	case store.OperationDelete:
		return 3
	case store.OperationUpdate:
		return 2
	case store.OperationCreate:
		return 1
	default:
		return 0
	}
}
