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
	ApiKeys       []KeyConfig    `yaml:"api_keys"`
	NetworkConfig *NetworkConfig `yaml:"network_config,omitempty"`
}

// KeyConfig holds an API key configuration.
type KeyConfig struct {
	Name   string        `yaml:"name"`
	Secret string        `yaml:"secret,omitempty"`
	Weight float64       `yaml:"weight,omitempty"`
	Models []ModelConfig `yaml:"models"`
}

// ModelConfig holds per-model configuration.
type ModelConfig struct {
	Name         string         `yaml:"name"`
	LogicalName  string         `yaml:"logical_name,omitempty"`
	Mode         string         `yaml:"mode,omitempty"` // "chat" (default) or "responses"
	Behavior     *ModelBehavior `yaml:"behavior,omitempty"`
}

// ModelBehavior holds model-specific behavior settings.
type ModelBehavior struct {
	// ToolCallFallback enables text-to-tool-call conversion for local LLMs.
	ToolCallFallback bool `yaml:"tool_call_fallback"`
	// StructuredOutput indicates the model supports structured output (JSON schema).
	StructuredOutput bool `yaml:"structured_output"`
	// MaxOutputTokens overrides the default max_tokens for LLM responses.
	// When set to 0 (default), the system default is used.
	MaxOutputTokens int `yaml:"max_output_tokens,omitempty"`
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
		if len(prov.ApiKeys) == 0 {
			return fmt.Errorf("provider %q has no api_keys defined", provName)
		}
		for _, key := range prov.ApiKeys {
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

	// Validate logical_name uniqueness across all providers.
	logicalNames := make(map[string]string) // logical_name -> "provider/model"
	for provName, prov := range c.Providers {
		for _, key := range prov.ApiKeys {
			for _, model := range key.Models {
				if model.LogicalName != "" {
					ref := provName + "/" + model.Name
					if existing, ok := logicalNames[model.LogicalName]; ok {
						return fmt.Errorf("duplicate logical_name %q: %s and %s", model.LogicalName, existing, ref)
					}
					logicalNames[model.LogicalName] = ref
				}
			}
		}
	}

	return nil
}
