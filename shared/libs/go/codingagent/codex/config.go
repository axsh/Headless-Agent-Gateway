package codex

import (
	"fmt"
	"os"
	"path/filepath"
)

const configTemplate = `model = "%s"
model_provider = "gateway"

[model_providers.gateway]
name = "HAG LLM Gateway"
base_url = "%s"
env_key = "OPENAI_API_KEY"
wire_api = "%s"
`

// GenerateConfigTOML generates a Codex config.toml string.
// wireAPI should be "chat" or "responses". Defaults to "chat" if empty.
func GenerateConfigTOML(model, gatewayURL, wireAPI string) string {
	if model == "" {
		model = "gpt-4o"
	}
	if wireAPI == "" {
		wireAPI = "chat"
	}
	return fmt.Sprintf(configTemplate, model, gatewayURL, wireAPI)
}

// WriteConfigTOML writes a config.toml to a CODEX_HOME directory and returns the directory path.
// Codex CLI reads config from $CODEX_HOME/config.toml automatically.
// Note: Codex CLI refuses CODEX_HOME under OS temp dirs, so we use
// the user's home directory with a unique subdirectory.
// The caller is responsible for cleaning up the directory after use.
func WriteConfigTOML(model, gatewayURL, wireAPI string) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get user home dir: %w", err)
	}
	baseDir := filepath.Join(homeDir, ".codex-hag-sessions")
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return "", fmt.Errorf("create base dir: %w", err)
	}
	dir, err := os.MkdirTemp(baseDir, "session-*")
	if err != nil {
		return "", fmt.Errorf("create codex home dir: %w", err)
	}
	configPath := filepath.Join(dir, "config.toml")
	content := GenerateConfigTOML(model, gatewayURL, wireAPI)
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("write config.toml: %w", err)
	}
	return dir, nil
}
