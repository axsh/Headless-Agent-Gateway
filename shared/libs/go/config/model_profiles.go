package config

import "fmt"

// ModelProfilesConfig represents model_profiles.yaml.
type ModelProfilesConfig struct {
	// DefaultProfile specifies the default provider and model.
	DefaultProfile DefaultProfileConfig `yaml:"default_profile"`

	// Providers maps provider name to its configuration.
	Providers map[string]ProviderConfig `yaml:"providers"`

	// Governance holds routing rules (future implementation).
	Governance GovernanceConfig `yaml:"governance,omitempty"`
}

// DefaultProfileConfig holds the default provider/model selection.
type DefaultProfileConfig struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
}

// ProviderConfig holds per-provider configuration.
type ProviderConfig struct {
	Keys          []KeyConfig    `yaml:"keys"`
	NetworkConfig *NetworkConfig `yaml:"network_config,omitempty"`
}

// KeyConfig holds an API key configuration.
type KeyConfig struct {
	Name   string        `yaml:"name"`
	Value  string        `yaml:"value"`
	Weight float64       `yaml:"weight,omitempty"`
	Models []ModelConfig `yaml:"models"`
}

// ModelConfig holds per-model configuration.
type ModelConfig struct {
	Name     string         `yaml:"name"`
	Behavior *ModelBehavior `yaml:"behavior,omitempty"`
}

// ModelBehavior holds model-specific behavior settings.
type ModelBehavior struct {
	// ToolCallFallback enables text-to-tool-call conversion for local LLMs.
	ToolCallFallback bool `yaml:"tool_call_fallback"`
}

// NetworkConfig holds provider-specific network settings.
type NetworkConfig struct {
	BaseURL               string `yaml:"base_url,omitempty"`
	RequestTimeoutSeconds int    `yaml:"request_timeout_seconds,omitempty"`
}

// GovernanceConfig holds routing rules (future implementation).
// TODO: CEL-based routing control
type GovernanceConfig struct {
	RoutingRules []any `yaml:"routing_rules,omitempty"`
}

// Validate checks the ModelProfilesConfig for correctness.
func (c *ModelProfilesConfig) Validate() error {
	if len(c.Providers) == 0 {
		return fmt.Errorf("no providers defined in model_profiles")
	}

	for provName, prov := range c.Providers {
		if len(prov.Keys) == 0 {
			return fmt.Errorf("provider %q has no keys defined", provName)
		}
		for _, key := range prov.Keys {
			for _, model := range key.Models {
				if model.Name == "" {
					return fmt.Errorf("provider %q key %q has empty model name", provName, key.Name)
				}
			}
		}
	}

	// Validate default profile references an existing provider.
	if c.DefaultProfile.Provider != "" {
		if _, ok := c.Providers[c.DefaultProfile.Provider]; !ok {
			return fmt.Errorf("default profile provider %q not found in providers", c.DefaultProfile.Provider)
		}
	}

	return nil
}
