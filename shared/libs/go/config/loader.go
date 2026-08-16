package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Load reads a config.yaml file and returns an AppConfig.
// This is a pure function: it reads YAML, parses into struct, and returns.
// It does NOT resolve vault:// references or wire dependencies.
func Load(path string) (*AppConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}
	var cfg AppConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", path, err)
	}
	cfg.LLMGateway.ApplyDefaults()
	cfg.AgentService.ApplyDefaults()
	return &cfg, nil
}

// LoadModelProfiles reads a model_profiles.yaml file and validates it.
// vault:// references are preserved as strings; they are not resolved here.
func LoadModelProfiles(path string) (*ModelProfilesConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read model profiles %s: %w", path, err)
	}
	var cfg ModelProfilesConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse model profiles %s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("model profiles validation failed: %w", err)
	}
	return &cfg, nil
}
