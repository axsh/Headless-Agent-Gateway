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

// WriteConfigTOML writes a config.toml to a temporary directory and returns the path.
// The caller is responsible for cleaning up the file after use.
func WriteConfigTOML(model, gatewayURL, wireAPI string) (string, error) {
	dir, err := os.MkdirTemp("", "codex-config-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	path := filepath.Join(dir, "config.toml")
	content := GenerateConfigTOML(model, gatewayURL, wireAPI)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("write config.toml: %w", err)
	}
	return path, nil
}
