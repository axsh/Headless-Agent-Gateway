package codex

import "github.com/axsh/arctic-tern/shared/libs/go/codingagent"

var codexConfigAllowlist = []string{
	"skills", "rules", "config.toml", "AGENTS.md",
}

// ApplyCodexConfigDir overlays configDir into sessionDir when configDir != "".
func ApplyCodexConfigDir(sessionDir, configDir string) error {
	if configDir == "" {
		return nil
	}
	return codingagent.OverlayConfigDir(sessionDir, configDir, codexConfigAllowlist)
}
