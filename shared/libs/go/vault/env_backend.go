package vault

import (
	"fmt"
	"os"
	"strings"
)

// EnvVaultBackend resolves secrets from environment variables.
// Path mapping: vault://providers/{provider}/{key} -> TERN_VAULT_{PROVIDER}_{KEY}
type EnvVaultBackend struct{}

// NewEnvVaultBackend creates a new EnvVaultBackend.
func NewEnvVaultBackend() *EnvVaultBackend {
	return &EnvVaultBackend{}
}

// Resolve resolves a vault:// reference to the actual secret from environment variables.
func (b *EnvVaultBackend) Resolve(ref string) (string, error) {
	path, ok := ParseVaultRef(ref)
	if !ok {
		return "", fmt.Errorf("not a vault reference: %s", ref)
	}
	envName := pathToEnvName(path)
	val := os.Getenv(envName)
	if val == "" {
		return "", fmt.Errorf("environment variable %s not set (for vault ref %s)", envName, ref)
	}
	return val, nil
}

// Set stores a secret by setting the corresponding environment variable.
func (b *EnvVaultBackend) Set(path string, value string) error {
	return os.Setenv(pathToEnvName(path), value)
}

// Delete removes a secret by unsetting the corresponding environment variable.
func (b *EnvVaultBackend) Delete(path string) error {
	return os.Unsetenv(pathToEnvName(path))
}

// List returns all secret paths by scanning environment variables with TERN_VAULT_ prefix.
func (b *EnvVaultBackend) List() ([]string, error) {
	var paths []string
	for _, env := range os.Environ() {
		parts := strings.SplitN(env, "=", 2)
		if strings.HasPrefix(parts[0], envPrefix) {
			paths = append(paths, envNameToPath(parts[0]))
		}
	}
	return paths, nil
}

const envPrefix = "TERN_VAULT_"

// pathToEnvName converts a vault path to an environment variable name.
// "providers/anthropic/primary" -> "TERN_VAULT_ANTHROPIC_PRIMARY"
// "providers/anthropic/team-a"  -> "TERN_VAULT_ANTHROPIC_TEAM_A"
func pathToEnvName(path string) string {
	// Strip "providers/" prefix if present.
	path = strings.TrimPrefix(path, "providers/")
	// Replace path separators and hyphens with underscores.
	name := strings.ReplaceAll(path, "/", "_")
	name = strings.ReplaceAll(name, "-", "_")
	return envPrefix + strings.ToUpper(name)
}

// envNameToPath converts an environment variable name back to a vault path.
// "TERN_VAULT_ANTHROPIC_PRIMARY" -> "anthropic_primary" (normalized, not exact inverse)
func envNameToPath(envName string) string {
	name := strings.TrimPrefix(envName, envPrefix)
	return strings.ToLower(name)
}
