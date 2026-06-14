package wayfinder

import (
	"os"
	"path/filepath"
)

// AgentConfig holds configuration for the Wayfinder agent.
type AgentConfig struct {
	// WorkDir is the root working directory for file operations.
	// Defaults to current working directory if empty.
	WorkDir string

	// SessionDir is the directory for session state storage.
	// Defaults to WorkDir/.claudecode if empty.
	SessionDir string

	// LogicalModel is the LLM model to use via Bifrost.
	LogicalModel string

	// AllowedPathPatterns is a list of regex patterns for paths that
	// are always permitted for file deletion, regardless of tracker state.
	AllowedPathPatterns []string

	// SystemPrompt is an optional system prompt prepended to conversations.
	SystemPrompt string

	// EnableSubagent enables subagent delegation for heavy tool calls
	// and child session creation for WBS node execution.
	EnableSubagent bool

	// StructuredOutput enables structured output (JSON schema) for models
	// that support it. Retrieved from model_profiles.yaml.
	StructuredOutput bool
}

// InitConfig resolves paths and applies defaults.
// After calling InitConfig, WorkDir and SessionDir are guaranteed to be
// absolute paths.
func InitConfig(cfg *AgentConfig) error {
	// Default WorkDir to current working directory.
	if cfg.WorkDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		cfg.WorkDir = cwd
	}

	// Resolve WorkDir to absolute path.
	absWork, err := filepath.Abs(cfg.WorkDir)
	if err != nil {
		return err
	}
	cfg.WorkDir = absWork

	// Default SessionDir to WorkDir/.claudecode.
	if cfg.SessionDir == "" {
		cfg.SessionDir = filepath.Join(cfg.WorkDir, ".claudecode")
	}

	// Resolve SessionDir to absolute path.
	absSession, err := filepath.Abs(cfg.SessionDir)
	if err != nil {
		return err
	}
	cfg.SessionDir = absSession

	return nil
}
