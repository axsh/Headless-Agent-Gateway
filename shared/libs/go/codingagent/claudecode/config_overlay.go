package claudecode

import "github.com/axsh/arctic-tern/shared/libs/go/codingagent"

var claudeConfigAllowlist = []string{
	"skills", "rules", "CLAUDE.md", "settings.json",
}

// ApplyClaudeConfigDir overlays configDir into sessionDir when configDir != "".
// Does not write into work_dir.
func ApplyClaudeConfigDir(sessionDir, configDir string) error {
	if configDir == "" {
		return nil
	}
	return codingagent.OverlayConfigDir(sessionDir, configDir, claudeConfigAllowlist)
}
