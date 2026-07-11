package config

import (
	"fmt"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

const (
	DefaultMaxPromptBytes      = 1048576
	DefaultMaxExecutionSeconds = 3600
	DefaultIdleTimeoutSeconds  = 300
)

// ModelProfilesConfig represents model_profiles.yaml.
type ModelProfilesConfig struct {
	// DefaultProfile specifies the default provider and model.
	DefaultProfile DefaultProfileConfig `yaml:"default_profile"`

	// Providers maps provider name to its configuration.
	Providers map[string]ProviderConfig `yaml:"providers"`

	// Governance holds routing rules (future implementation).
	Governance GovernanceConfig `yaml:"governance,omitempty"`

	// CodingAgents holds configurations for specific coding agents (e.g. codex).
	CodingAgents map[string]AgentConfig `yaml:"coding_agents,omitempty"`
}

// AgentConfig holds per-agent configuration.
type AgentConfig struct {
	// MaxPromptBytes is the maximum allowed size for the combined message and image data.
	MaxPromptBytes int `yaml:"max_prompt_bytes"`
	// MaxExecutionSeconds is the maximum wall-clock time for a single agent execution.
	MaxExecutionSeconds int `yaml:"max_execution_seconds"`
	// IdleTimeoutSeconds is the maximum time without stdout/stderr output before timeout.
	IdleTimeoutSeconds int `yaml:"idle_timeout_seconds"`
	// ExecutionMode controls stdin behavior: "interactive" or "single_shot".
	ExecutionMode string `yaml:"execution_mode"`
}

// WithDefaults returns a copy with zero values replaced by defaults.
func (c AgentConfig) WithDefaults() AgentConfig {
	out := c
	if out.MaxPromptBytes == 0 {
		out.MaxPromptBytes = DefaultMaxPromptBytes
	}
	if out.MaxExecutionSeconds == 0 {
		out.MaxExecutionSeconds = DefaultMaxExecutionSeconds
	}
	if out.IdleTimeoutSeconds == 0 {
		out.IdleTimeoutSeconds = DefaultIdleTimeoutSeconds
	}
	out.ExecutionMode = codingagent.NormalizeExecutionMode(out.ExecutionMode)
	return out
}

// ResolveAgentConfig returns agent config with defaults applied.
func ResolveAgentConfig(profiles *ModelProfilesConfig, agentName string) AgentConfig {
	if profiles != nil && profiles.CodingAgents != nil {
		if cfg, ok := profiles.CodingAgents[agentName]; ok {
			return cfg.WithDefaults()
		}
	}
	return AgentConfig{}.WithDefaults()
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
	Name        string         `yaml:"name"`
	LogicalName string         `yaml:"logical_name,omitempty"`
	Mode        string         `yaml:"mode,omitempty"` // "chat" (default) or "responses"
	Behavior    *ModelBehavior `yaml:"behavior,omitempty"`
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
