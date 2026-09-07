package config

import (
	"reflect"
	"strings"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
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

func TestModelBehavior_MaxOutputTokens(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{
			name:  "max_output_tokens set",
			input: "name: gemini-2.5-flash\nbehavior:\n  max_output_tokens: 65536",
			want:  65536,
		},
		{
			name:  "max_output_tokens not set defaults to zero",
			input: "name: gpt-4o",
			want:  0,
		},
		{
			name:  "max_output_tokens with other behavior fields",
			input: "name: gemini-2.5-flash\nbehavior:\n  structured_output: true\n  max_output_tokens: 32768",
			want:  32768,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mc ModelConfig
			if err := yaml.Unmarshal([]byte(tt.input), &mc); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			got := 0
			if mc.Behavior != nil {
				got = mc.Behavior.MaxOutputTokens
			}
			if got != tt.want {
				t.Errorf("MaxOutputTokens = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAgentConfig_WithDefaults(t *testing.T) {
	cfg := AgentConfig{
		MaxPromptBytes:      2048,
		MaxExecutionSeconds: 0,
		IdleTimeoutSeconds:  0,
		ExecutionMode:       "",
	}.WithDefaults()

	if cfg.MaxPromptBytes != 2048 {
		t.Errorf("MaxPromptBytes = %d, want 2048", cfg.MaxPromptBytes)
	}
	if cfg.MaxExecutionSeconds != DefaultMaxExecutionSeconds {
		t.Errorf("MaxExecutionSeconds = %d, want %d", cfg.MaxExecutionSeconds, DefaultMaxExecutionSeconds)
	}
	if cfg.IdleTimeoutSeconds != DefaultIdleTimeoutSeconds {
		t.Errorf("IdleTimeoutSeconds = %d, want %d", cfg.IdleTimeoutSeconds, DefaultIdleTimeoutSeconds)
	}
	if cfg.ExecutionMode != codingagent.ExecutionModeInteractive {
		t.Errorf("ExecutionMode = %q, want interactive", cfg.ExecutionMode)
	}
}

func TestAgentConfig_ScannerAndToolResultDefaults(t *testing.T) {
	cfg := AgentConfig{}.WithDefaults()
	if cfg.ScannerMaxTokenBytes != codingagent.DefaultScannerMaxTokenSize {
		t.Errorf("ScannerMaxTokenBytes = %d, want %d", cfg.ScannerMaxTokenBytes, codingagent.DefaultScannerMaxTokenSize)
	}
	if cfg.MaxToolResultBytes != codingagent.DefaultMaxToolResultBytes {
		t.Errorf("MaxToolResultBytes = %d, want %d", cfg.MaxToolResultBytes, codingagent.DefaultMaxToolResultBytes)
	}
}

func TestAgentConfig_ScannerAndToolResultOverride(t *testing.T) {
	cfg := AgentConfig{
		ScannerMaxTokenBytes: 8192,
		MaxToolResultBytes:     4096,
	}.WithDefaults()
	if cfg.ScannerMaxTokenBytes != 8192 {
		t.Errorf("ScannerMaxTokenBytes = %d, want 8192", cfg.ScannerMaxTokenBytes)
	}
	if cfg.MaxToolResultBytes != 4096 {
		t.Errorf("MaxToolResultBytes = %d, want 4096", cfg.MaxToolResultBytes)
	}
}

func TestResolveAgentConfig(t *testing.T) {
	profiles := &ModelProfilesConfig{
		CodingAgents: map[string]AgentConfig{
			"codex": {ExecutionMode: "single_shot", MaxPromptBytes: 512},
		},
	}
	got := ResolveAgentConfig(profiles, "codex")
	if got.ExecutionMode != codingagent.ExecutionModeSingleShot {
		t.Errorf("ExecutionMode = %q", got.ExecutionMode)
	}
	if got.MaxPromptBytes != 512 {
		t.Errorf("MaxPromptBytes = %d", got.MaxPromptBytes)
	}

	defaults := ResolveAgentConfig(profiles, "unknown")
	if defaults.ExecutionMode != codingagent.ExecutionModeInteractive {
		t.Errorf("default ExecutionMode = %q", defaults.ExecutionMode)
	}
}

func TestAgentConfig_YAMLUnmarshal(t *testing.T) {
	input := `
coding_agents:
  codex:
    max_prompt_bytes: 1048576
    max_execution_seconds: 7200
    idle_timeout_seconds: 120
    execution_mode: single_shot
`
	var cfg ModelProfilesConfig
	if err := yaml.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	agent := cfg.CodingAgents["codex"].WithDefaults()
	if agent.MaxExecutionSeconds != 7200 {
		t.Errorf("MaxExecutionSeconds = %d", agent.MaxExecutionSeconds)
	}
	if agent.IdleTimeoutSeconds != 120 {
		t.Errorf("IdleTimeoutSeconds = %d", agent.IdleTimeoutSeconds)
	}
	if agent.ExecutionMode != codingagent.ExecutionModeSingleShot {
		t.Errorf("ExecutionMode = %q", agent.ExecutionMode)
	}
}

func TestModelBehavior_Reasoning_YAMLParse(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		wantReasoning *ModelReasoning
	}{
		{
			name: "full reasoning config",
			input: `
name: gpt-6-astra
behavior:
  reasoning:
    required: true
    supported_efforts: ["low", "medium", "high", "xhigh", "max"]
    default_effort: "medium"
`,
			wantReasoning: &ModelReasoning{
				Required:         true,
				SupportedEfforts: []string{"low", "medium", "high", "xhigh", "max"},
				DefaultEffort:    "medium",
			},
		},
		{
			name: "reasoning omitted",
			input: `
name: gpt-4o
behavior:
  structured_output: true
`,
			wantReasoning: nil,
		},
		{
			name: "behavior omitted",
			input: `
name: gpt-4o
`,
			wantReasoning: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mc ModelConfig
			if err := yaml.Unmarshal([]byte(tt.input), &mc); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if tt.wantReasoning == nil {
				if mc.Behavior != nil && mc.Behavior.Reasoning != nil {
					t.Fatalf("expected nil Reasoning, got %+v", mc.Behavior.Reasoning)
				}
				return
			}
			if mc.Behavior == nil || mc.Behavior.Reasoning == nil {
				t.Fatal("expected non-nil Reasoning")
			}
			r := mc.Behavior.Reasoning
			if r.Required != tt.wantReasoning.Required {
				t.Errorf("Required = %v, want %v", r.Required, tt.wantReasoning.Required)
			}
			if !reflect.DeepEqual(r.SupportedEfforts, tt.wantReasoning.SupportedEfforts) {
				t.Errorf("SupportedEfforts = %v, want %v", r.SupportedEfforts, tt.wantReasoning.SupportedEfforts)
			}
			if r.DefaultEffort != tt.wantReasoning.DefaultEffort {
				t.Errorf("DefaultEffort = %q, want %q", r.DefaultEffort, tt.wantReasoning.DefaultEffort)
			}
		})
	}
}

func TestModelProfilesConfig_Validate_Reasoning(t *testing.T) {
	baseConfig := func() ModelProfilesConfig {
		return ModelProfilesConfig{
			DefaultProfile: DefaultProfileConfig{Provider: "openai", Model: "gpt-6-astra"},
			Providers: map[string]ProviderConfig{
				"openai": {
					ApiKeys: []KeyConfig{
						{
							Name:   "primary",
							Secret: "vault://openai/key",
							Models: []ModelConfig{
								{
									Name: "gpt-6-astra",
									Behavior: &ModelBehavior{
										Reasoning: &ModelReasoning{
											Required:         true,
											SupportedEfforts: []string{"low", "medium", "high", "xhigh", "max"},
											DefaultEffort:    "medium",
										},
									},
								},
								{
									Name: "gpt-4o",
								},
							},
						},
					},
				},
			},
		}
	}

	tests := []struct {
		name      string
		modify    func(*ModelProfilesConfig)
		wantErr   bool
		errSubstr string
	}{
		{
			name:    "valid reasoning config",
			modify:  func(c *ModelProfilesConfig) {},
			wantErr: false,
		},
		{
			name: "valid optional reasoning with none",
			modify: func(c *ModelProfilesConfig) {
				c.Providers["openai"].ApiKeys[0].Models[0].Behavior.Reasoning = &ModelReasoning{
					Required:         false,
					SupportedEfforts: []string{"none", "low", "medium"},
					DefaultEffort:    "none",
				}
			},
			wantErr: false,
		},
		{
			name: "valid optional reasoning with empty default",
			modify: func(c *ModelProfilesConfig) {
				c.Providers["openai"].ApiKeys[0].Models[0].Behavior.Reasoning = &ModelReasoning{
					Required:         false,
					SupportedEfforts: []string{"none", "low", "medium"},
					DefaultEffort:    "",
				}
			},
			wantErr: false,
		},
		{
			name: "unknown supported effort",
			modify: func(c *ModelProfilesConfig) {
				c.Providers["openai"].ApiKeys[0].Models[0].Behavior.Reasoning.SupportedEfforts = []string{"low", "super-fast"}
			},
			wantErr:   true,
			errSubstr: "unknown reasoning effort",
		},
		{
			name: "unknown default effort",
			modify: func(c *ModelProfilesConfig) {
				c.Providers["openai"].ApiKeys[0].Models[0].Behavior.Reasoning.DefaultEffort = "super-fast"
			},
			wantErr:   true,
			errSubstr: "unknown default_effort",
		},
		{
			name: "required true but empty supported_efforts",
			modify: func(c *ModelProfilesConfig) {
				c.Providers["openai"].ApiKeys[0].Models[0].Behavior.Reasoning.SupportedEfforts = nil
			},
			wantErr:   true,
			errSubstr: "supported_efforts cannot be empty",
		},
		{
			name: "required true but contains none",
			modify: func(c *ModelProfilesConfig) {
				c.Providers["openai"].ApiKeys[0].Models[0].Behavior.Reasoning.SupportedEfforts = []string{"none", "low", "medium"}
			},
			wantErr:   true,
			errSubstr: "effort none is not permitted",
		},
		{
			name: "default_effort not in supported_efforts",
			modify: func(c *ModelProfilesConfig) {
				c.Providers["openai"].ApiKeys[0].Models[0].Behavior.Reasoning.DefaultEffort = "max"
				c.Providers["openai"].ApiKeys[0].Models[0].Behavior.Reasoning.SupportedEfforts = []string{"low", "medium"}
			},
			wantErr:   true,
			errSubstr: "default_effort \"max\" is not in supported_efforts",
		},
		{
			name: "required true but empty default_effort",
			modify: func(c *ModelProfilesConfig) {
				c.Providers["openai"].ApiKeys[0].Models[0].Behavior.Reasoning.DefaultEffort = ""
			},
			wantErr:   true,
			errSubstr: "default_effort is required",
		},
		{
			name: "duplicate supported effort",
			modify: func(c *ModelProfilesConfig) {
				c.Providers["openai"].ApiKeys[0].Models[0].Behavior.Reasoning.SupportedEfforts = []string{"low", "medium", "low"}
			},
			wantErr:   true,
			errSubstr: "duplicate effort",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseConfig()
			tt.modify(&cfg)
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && tt.errSubstr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("Validate() error = %v, want substr %q", err, tt.errSubstr)
				}
			}
		})
	}
}


