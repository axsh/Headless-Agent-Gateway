package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

const testModelProfilesYAML = `
default_profile:
  provider: "anthropic"
  model: "claude-sonnet-4-20250514"

providers:
  anthropic:
    api_keys:
      - name: "primary"
        secret: "vault://providers/anthropic/primary"
        models:
          - name: "claude-sonnet-4-20250514"
          - name: "claude-haiku-3-5-20241022"
    network_config:
      base_url: ""
  openai:
    api_keys:
      - name: "primary"
        secret: "vault://providers/openai/primary"
        models:
          - name: "gpt-4o"
          - name: "o3-mini"
    network_config:
      base_url: ""
  ollama:
    api_keys:
      - name: "default"
        secret: "vault://providers/ollama/default"
        models:
          - name: "qwen2.5-coder:7b"
            behavior:
              tool_call_fallback: true
    network_config:
      base_url: "http://localhost:11434"

governance:
  routing_rules: []
`

func TestModelProfilesConfig_YAMLUnmarshal(t *testing.T) {
	var cfg ModelProfilesConfig
	if err := yaml.Unmarshal([]byte(testModelProfilesYAML), &cfg); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}

	// Verify default_profile.
	if cfg.DefaultProfile.Provider != "anthropic" {
		t.Errorf("DefaultProfile.Provider = %q, want %q", cfg.DefaultProfile.Provider, "anthropic")
	}
	if cfg.DefaultProfile.Model != "claude-sonnet-4-20250514" {
		t.Errorf("DefaultProfile.Model = %q, want %q", cfg.DefaultProfile.Model, "claude-sonnet-4-20250514")
	}

	// Verify providers count.
	if len(cfg.Providers) != 3 {
		t.Fatalf("len(Providers) = %d, want 3", len(cfg.Providers))
	}

	// Verify anthropic provider.
	anth := cfg.Providers["anthropic"]
	if len(anth.ApiKeys) != 1 {
		t.Fatalf("anthropic api_keys = %d, want 1", len(anth.ApiKeys))
	}
	if anth.ApiKeys[0].Secret != "vault://providers/anthropic/primary" {
		t.Errorf("anthropic key secret = %q", anth.ApiKeys[0].Secret)
	}
	if len(anth.ApiKeys[0].Models) != 2 {
		t.Errorf("anthropic models = %d, want 2", len(anth.ApiKeys[0].Models))
	}

	// Verify ollama with behavior.
	ollama := cfg.Providers["ollama"]
	if len(ollama.ApiKeys) != 1 {
		t.Fatalf("ollama api_keys = %d, want 1", len(ollama.ApiKeys))
	}
	if len(ollama.ApiKeys[0].Models) != 1 {
		t.Fatalf("ollama models = %d, want 1", len(ollama.ApiKeys[0].Models))
	}
	model := ollama.ApiKeys[0].Models[0]
	if model.Name != "qwen2.5-coder:7b" {
		t.Errorf("model name = %q, want %q", model.Name, "qwen2.5-coder:7b")
	}
	if model.Behavior == nil {
		t.Fatal("model behavior is nil")
	}
	if !model.Behavior.ToolCallFallback {
		t.Error("tool_call_fallback should be true")
	}

	// Verify network_config.
	if ollama.NetworkConfig == nil {
		t.Fatal("ollama network_config is nil")
	}
	if ollama.NetworkConfig.BaseURL != "http://localhost:11434" {
		t.Errorf("base_url = %q, want %q", ollama.NetworkConfig.BaseURL, "http://localhost:11434")
	}
}

func TestModelProfilesConfig_Validate(t *testing.T) {
	validConfig := func() ModelProfilesConfig {
		return ModelProfilesConfig{
			DefaultProfile: DefaultProfileConfig{Provider: "anthropic", Model: "claude-sonnet-4-20250514"},
			Providers: map[string]ProviderConfig{
				"anthropic": {
					ApiKeys: []KeyConfig{
						{Name: "primary", Secret: "vault://x", Models: []ModelConfig{{Name: "claude-sonnet-4-20250514"}}},
					},
				},
			},
		}
	}

	tests := []struct {
		name    string
		modify  func(*ModelProfilesConfig)
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid",
			modify:  func(c *ModelProfilesConfig) {},
			wantErr: false,
		},
		{
			name:    "empty providers",
			modify:  func(c *ModelProfilesConfig) { c.Providers = nil },
			wantErr: true,
			errMsg:  "no providers",
		},
		{
			name: "empty api_keys",
			modify: func(c *ModelProfilesConfig) {
				c.Providers["anthropic"] = ProviderConfig{ApiKeys: nil}
			},
			wantErr: true,
			errMsg:  "no api_keys",
		},
		{
			name: "empty model name",
			modify: func(c *ModelProfilesConfig) {
				p := c.Providers["anthropic"]
				p.ApiKeys[0].Models = []ModelConfig{{Name: ""}}
				c.Providers["anthropic"] = p
			},
			wantErr: true,
			errMsg:  "empty model name",
		},
		{
			name: "invalid default provider",
			modify: func(c *ModelProfilesConfig) {
				c.DefaultProfile.Provider = "nonexistent"
			},
			wantErr: true,
			errMsg:  "default profile provider",
		},
		{
			name: "duplicate logical_name",
			modify: func(c *ModelProfilesConfig) {
				c.Providers["anthropic"] = ProviderConfig{
					ApiKeys: []KeyConfig{
						{Name: "primary", Secret: "vault://x", Models: []ModelConfig{
							{Name: "claude-sonnet-4-20250514", LogicalName: "fast-coder"},
						}},
					},
				}
				c.Providers["openai"] = ProviderConfig{
					ApiKeys: []KeyConfig{
						{Name: "primary", Secret: "vault://y", Models: []ModelConfig{
							{Name: "gpt-4o", LogicalName: "fast-coder"},
						}},
					},
				}
			},
			wantErr: true,
			errMsg:  "duplicate logical_name",
		},
		{
			name: "empty secret is valid",
			modify: func(c *ModelProfilesConfig) {
				c.Providers["anthropic"] = ProviderConfig{
					ApiKeys: []KeyConfig{
						{Name: "default", Secret: "", Models: []ModelConfig{{Name: "claude-sonnet-4-20250514"}}},
					},
				}
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.modify(&cfg)
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestModelConfigLogicalName(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		wantLN string
	}{
		{
			name:   "logical_name set",
			input:  "name: gpt-4o\nlogical_name: fast-coder",
			wantLN: "fast-coder",
		},
		{
			name:   "logical_name omitted",
			input:  "name: gpt-4o",
			wantLN: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mc ModelConfig
			if err := yaml.Unmarshal([]byte(tt.input), &mc); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if mc.LogicalName != tt.wantLN {
				t.Errorf("LogicalName = %q, want %q", mc.LogicalName, tt.wantLN)
			}
		})
	}
}

func TestModelBehavior_StructuredOutput(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "structured_output true",
			input: "name: gemini-2.5-flash\nbehavior:\n  structured_output: true",
			want:  true,
		},
		{
			name:  "structured_output false",
			input: "name: claude-sonnet-4\nbehavior:\n  structured_output: false",
			want:  false,
		},
		{
			name:  "structured_output not set defaults to false",
			input: "name: gpt-4o",
			want:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mc ModelConfig
			if err := yaml.Unmarshal([]byte(tt.input), &mc); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			got := false
			if mc.Behavior != nil {
				got = mc.Behavior.StructuredOutput
			}
			if got != tt.want {
				t.Errorf("StructuredOutput = %v, want %v", got, tt.want)
			}
		})
	}
}
