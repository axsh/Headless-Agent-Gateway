package codex_test

import (
	"strings"
	"testing"

	"github.com/axsh/hag/codingagent"
	"github.com/axsh/hag/codingagent/codex"
)

func TestCodexBuildArgs(t *testing.T) {
	args := codex.BuildArgs("/tmp/codex-config/config.toml")

	argsStr := strings.Join(args, " ")
	if !strings.Contains(argsStr, "--config") {
		t.Errorf("args should contain --config, got %q", argsStr)
	}
	if !strings.Contains(argsStr, "/tmp/codex-config/config.toml") {
		t.Errorf("args should contain config path, got %q", argsStr)
	}
}

func TestCodexBuildEnv(t *testing.T) {
	tests := []struct {
		name    string
		ac      *codingagent.AdapterConfig
		cfg     *codingagent.SessionConfig
		wantKey string
		wantVal string
	}{
		{
			name:    "OPENAI_API_KEY is set to not-needed",
			ac:      &codingagent.AdapterConfig{},
			cfg:     &codingagent.SessionConfig{},
			wantKey: "OPENAI_API_KEY",
			wantVal: "not-needed",
		},
		{
			name: "additional env vars are included",
			ac:   &codingagent.AdapterConfig{},
			cfg: &codingagent.SessionConfig{
				EnvVars: map[string]string{"CUSTOM_VAR": "custom_value"},
			},
			wantKey: "CUSTOM_VAR",
			wantVal: "custom_value",
		},
		{
			name:    "session dir sets CODEX_HOME",
			ac:      &codingagent.AdapterConfig{},
			cfg:     &codingagent.SessionConfig{SessionDir: "/data/sessions"},
			wantKey: "CODEX_HOME",
			wantVal: "/data/sessions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := codex.BuildEnv(tt.ac, tt.cfg)
			envMap := make(map[string]string)
			for _, e := range env {
				parts := strings.SplitN(e, "=", 2)
				if len(parts) == 2 {
					envMap[parts[0]] = parts[1]
				}
			}

			if val, ok := envMap[tt.wantKey]; !ok || val != tt.wantVal {
				t.Errorf("%s = %q, want %q", tt.wantKey, val, tt.wantVal)
			}
		})
	}
}

func TestCodexBuildEnv_NoGatewayRelatedVars(t *testing.T) {
	ac := &codingagent.AdapterConfig{GatewayURL: "http://localhost:14000"}
	cfg := &codingagent.SessionConfig{}

	env := codex.BuildEnv(ac, cfg)
	envMap := make(map[string]string)
	for _, e := range env {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}

	// Codex uses config.toml for gateway URL, not env vars
	if _, ok := envMap["ANTHROPIC_BASE_URL"]; ok {
		t.Error("ANTHROPIC_BASE_URL should not be set for Codex (uses config.toml)")
	}
}
