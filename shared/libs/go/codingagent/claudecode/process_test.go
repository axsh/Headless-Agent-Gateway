package claudecode_test

import (
	"strings"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent/claudecode"
)

func TestBuildArgs(t *testing.T) {
	tests := []struct {
		name       string
		cfg        *codingagent.SessionConfig
		contains   []string
		notContain string
	}{
		{
			name: "basic with prompt and model",
			cfg: &codingagent.SessionConfig{
				Prompt: "hello world",
				Model:  "anthropic/claude-sonnet-4",
			},
			contains: []string{
				"--output-format", "stream-json",
				"-p", "hello world",
				"--model", "anthropic/claude-sonnet-4",
				"--permission-mode", "bypassPermissions",
			},
		},
		{
			name: "with allowed tools",
			cfg: &codingagent.SessionConfig{
				Prompt:       "test",
				AllowedTools: []string{"Read", "Edit", "Write"},
			},
			contains: []string{"--allowedTools", "Read,Edit,Write"},
		},
		{
			name: "with agent session ID",
			cfg: &codingagent.SessionConfig{
				Prompt:         "test",
				AgentSessionID: "sdk-abc-123",
			},
			contains: []string{"--resume", "sdk-abc-123"},
		},
		{
			name:     "includes --verbose flag",
			cfg:      &codingagent.SessionConfig{Prompt: "test"},
			contains: []string{"--verbose"},
		},
		{
			name:     "with max turns",
			cfg:      &codingagent.SessionConfig{Prompt: "test", MaxTurns: 50},
			contains: []string{"--max-turns", "50"},
		},
		{
			// R7: MaxTurns=0 should default to 200.
			name:     "zero max turns uses default 200",
			cfg:      &codingagent.SessionConfig{Prompt: "test"},
			contains: []string{"--max-turns", "200"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := claudecode.BuildArgs(tt.cfg)
			argsStr := strings.Join(args, " ")
			for _, want := range tt.contains {
				if !strings.Contains(argsStr, want) {
					t.Errorf("args %q should contain %q", argsStr, want)
				}
			}
			if tt.notContain != "" {
				if strings.Contains(argsStr, tt.notContain) {
					t.Errorf("args %q should NOT contain %q", argsStr, tt.notContain)
				}
			}
		})
	}
}

func TestBuildEnv(t *testing.T) {
	tests := []struct {
		name         string
		ac           *codingagent.AdapterConfig
		cfg          *codingagent.SessionConfig
		wantKey      string
		wantVal      string
		wantNot      string
		wantContains string // checks value Contains instead of exact match
	}{
		{
			name:    "gateway URL sets ANTHROPIC_BASE_URL",
			ac:      &codingagent.AdapterConfig{GatewayURL: "http://localhost:14000"},
			cfg:     &codingagent.SessionConfig{},
			wantKey: "ANTHROPIC_BASE_URL",
			wantVal: "http://localhost:14000",
		},
		{
			name:    "disable sandbox sets CLAUDE_CODE_SKIP_SANDBOX",
			ac:      &codingagent.AdapterConfig{DisableSandbox: true},
			cfg:     &codingagent.SessionConfig{},
			wantKey: "CLAUDE_CODE_SKIP_SANDBOX",
			wantVal: "1",
		},
		{
			name:    "sandbox enabled does not set CLAUDE_CODE_SKIP_SANDBOX",
			ac:      &codingagent.AdapterConfig{DisableSandbox: false},
			cfg:     &codingagent.SessionConfig{},
			wantNot: "CLAUDE_CODE_SKIP_SANDBOX",
		},
		{
			name: "session danger-full-access sets SKIP even when adapter false",
			ac:   &codingagent.AdapterConfig{DisableSandbox: false},
			cfg:  &codingagent.SessionConfig{SandboxMode: codingagent.SandboxModeDangerFullAccess},
			wantKey: "CLAUDE_CODE_SKIP_SANDBOX",
			wantVal: "1",
		},
		{
			name:    "session read-only clears SKIP even when adapter true",
			ac:      &codingagent.AdapterConfig{DisableSandbox: true},
			cfg:     &codingagent.SessionConfig{SandboxMode: codingagent.SandboxModeReadOnly},
			wantNot: "CLAUDE_CODE_SKIP_SANDBOX",
		},
		{
			name:    "no gateway URL: ANTHROPIC_API_KEY not set",
			ac:      &codingagent.AdapterConfig{},
			cfg:     &codingagent.SessionConfig{},
			wantNot: "ANTHROPIC_API_KEY",
		},
		{
			name:    "no gateway URL: ANTHROPIC_BASE_URL not set",
			ac:      &codingagent.AdapterConfig{},
			cfg:     &codingagent.SessionConfig{},
			wantNot: "ANTHROPIC_BASE_URL",
		},
		{
			// R4: API key now includes metadata.
			name:         "with gateway URL: ANTHROPIC_API_KEY contains not-needed",
			ac:           &codingagent.AdapterConfig{GatewayURL: "http://localhost:14000"},
			cfg:          &codingagent.SessionConfig{},
			wantKey:      "ANTHROPIC_API_KEY",
			wantContains: "not-needed",
		},
		{
			name:    "session dir sets CLAUDE_CONFIG_DIR",
			ac:      &codingagent.AdapterConfig{},
			cfg:     &codingagent.SessionConfig{SessionDir: "/data/sessions"},
			wantKey: "CLAUDE_CONFIG_DIR",
			wantVal: "/data/sessions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := claudecode.BuildEnv(tt.ac, tt.cfg)
			envMap := make(map[string]string)
			for _, e := range env {
				parts := strings.SplitN(e, "=", 2)
				if len(parts) == 2 {
					envMap[parts[0]] = parts[1]
				}
			}

			if tt.wantKey != "" {
				val, ok := envMap[tt.wantKey]
				if !ok {
					t.Errorf("%s should be set", tt.wantKey)
				} else if tt.wantVal != "" && val != tt.wantVal {
					t.Errorf("%s = %q, want %q", tt.wantKey, val, tt.wantVal)
				} else if tt.wantContains != "" && !strings.Contains(val, tt.wantContains) {
					t.Errorf("%s = %q, want to contain %q", tt.wantKey, val, tt.wantContains)
				}
			}
			if tt.wantNot != "" {
				if _, ok := envMap[tt.wantNot]; ok {
					t.Errorf("%s should not be set", tt.wantNot)
				}
			}
		})
	}
}

func TestBuildEnv_SessionEnvVarsOverride(t *testing.T) {
	ac := &codingagent.AdapterConfig{GatewayURL: "http://gw:14000"}
	cfg := &codingagent.SessionConfig{
		EnvVars: map[string]string{"ANTHROPIC_API_KEY": "real-key"},
	}
	env := claudecode.BuildEnv(ac, cfg)
	envMap := make(map[string]string)
	for _, e := range env {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}
	if envMap["ANTHROPIC_API_KEY"] != "real-key" {
		t.Errorf("ANTHROPIC_API_KEY = %q, want %q (session env should override)",
			envMap["ANTHROPIC_API_KEY"], "real-key")
	}
}

// R4: Test API key metadata format.
func TestBuildEnv_APIKeyMetadata(t *testing.T) {
	tests := []struct {
		name         string
		ac           *codingagent.AdapterConfig
		cfg          *codingagent.SessionConfig
		wantContains string
	}{
		{
			name:         "fallback_true_with_agentSessionID",
			ac:           &codingagent.AdapterConfig{GatewayURL: "http://gw:14000", ToolCallFallback: true},
			cfg:          &codingagent.SessionConfig{AgentSessionID: "sess-123"},
			wantContains: ";fallback=true;sid=sess-123",
		},
		{
			name:         "fallback_false_with_agentSessionID",
			ac:           &codingagent.AdapterConfig{GatewayURL: "http://gw:14000", ToolCallFallback: false},
			cfg:          &codingagent.SessionConfig{AgentSessionID: "sess-456"},
			wantContains: ";fallback=false;sid=sess-456",
		},
		{
			name:         "fallback_true_no_sid",
			ac:           &codingagent.AdapterConfig{GatewayURL: "http://gw:14000", ToolCallFallback: true},
			cfg:          &codingagent.SessionConfig{},
			wantContains: ";fallback=true;sid=default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := claudecode.BuildEnv(tt.ac, tt.cfg)
			envMap := make(map[string]string)
			for _, e := range env {
				parts := strings.SplitN(e, "=", 2)
				if len(parts) == 2 {
					envMap[parts[0]] = parts[1]
				}
			}
			apiKey := envMap["ANTHROPIC_API_KEY"]
			if !strings.Contains(apiKey, tt.wantContains) {
				t.Errorf("ANTHROPIC_API_KEY = %q, want to contain %q", apiKey, tt.wantContains)
			}
		})
	}
}

func TestBuildEnv_GatewayToken(t *testing.T) {
	ac := &codingagent.AdapterConfig{GatewayURL: "http://gw:14000", GatewayToken: "my-secret-token"}
	cfg := &codingagent.SessionConfig{}
	env := claudecode.BuildEnv(ac, cfg)
	envMap := make(map[string]string)
	for _, e := range env {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}
	apiKey := envMap["ANTHROPIC_API_KEY"]
	want := "not-needed;token=my-secret-token;fallback=false;sid=default"
	if apiKey != want {
		t.Errorf("ANTHROPIC_API_KEY = %q, want %q", apiKey, want)
	}
}
