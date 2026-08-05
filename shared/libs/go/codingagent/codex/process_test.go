package codex_test

import (
	"strings"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent/codex"
)

func TestCodexBuildArgs(t *testing.T) {
	overrides := []string{"-c", `model="gpt-4o"`}
	args := codex.BuildArgs("create hello.txt", overrides, true)

	argsStr := strings.Join(args, " ")
	if !strings.Contains(argsStr, "exec") {
		t.Errorf("args should contain 'exec', got %q", argsStr)
	}
	if !strings.Contains(argsStr, "--json") {
		t.Errorf("args should contain '--json', got %q", argsStr)
	}
	if !strings.Contains(argsStr, "--ignore-user-config") {
		t.Errorf("args should contain '--ignore-user-config', got %q", argsStr)
	}
	if !strings.Contains(argsStr, `model="gpt-4o"`) {
		t.Errorf("args should contain config override, got %q", argsStr)
	}
	// When prompt is non-empty, last arg should be "-" (stdin mode)
	if args[len(args)-1] != "-" {
		t.Errorf("last arg should be '-' for stdin mode, got %q", args[len(args)-1])
	}
}

func TestCodexBuildArgs_WithConfigDirDisablesIgnoreUserConfig(t *testing.T) {
	// When ConfigDir is set, StartProcess passes ignoreUserConfig=false.
	// When ConfigDir == "", StartProcess passes ignoreUserConfig=true (restores --ignore-user-config).
	args := codex.BuildArgs("hi", nil, false)
	for _, a := range args {
		if a == "--ignore-user-config" {
			t.Fatal("ignore-user-config must be omitted when config_dir is active")
		}
	}
	cleared := codex.BuildArgs("hi", nil, true)
	found := false
	for _, a := range cleared {
		if a == "--ignore-user-config" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("ignore-user-config must be present when config_dir is cleared")
	}
}

func TestCodexBuildArgs_StdinMode(t *testing.T) {
	args := codex.BuildArgs("some prompt text", nil, true)

	// Last argument should be "-" to instruct codex to read from stdin.
	if args[len(args)-1] != "-" {
		t.Errorf("last arg should be '-' for stdin mode, got %q", args[len(args)-1])
	}

	// Prompt text itself must NOT appear in args.
	for _, a := range args {
		if a == "some prompt text" {
			t.Error("prompt text should not appear in args (should be passed via stdin)")
		}
	}
}

func TestCodexBuildArgs_EmptyPrompt(t *testing.T) {
	args := codex.BuildArgs("", nil, true)

	// When prompt is empty, "-" should NOT be in args.
	for _, a := range args {
		if a == "-" {
			t.Error("'-' should not be in args when prompt is empty")
		}
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
			name:    "OPENAI_API_KEY with default metadata",
			ac:      &codingagent.AdapterConfig{GatewayURL: "http://localhost:14000"},
			cfg:     &codingagent.SessionConfig{},
			wantKey: "OPENAI_API_KEY",
			wantVal: "tern-internal-key-placeholder;fallback=false;sid=default",
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

func TestCodexBuildEnv_SessionMetadata(t *testing.T) {
	tests := []struct {
		name      string
		ac        *codingagent.AdapterConfig
		cfg       *codingagent.SessionConfig
		wantValue string
	}{
		{
			name:      "default session ID and no fallback",
			ac:        &codingagent.AdapterConfig{GatewayURL: "http://localhost:14000"},
			cfg:       &codingagent.SessionConfig{},
			wantValue: "tern-internal-key-placeholder;fallback=false;sid=default",
		},
		{
			name: "explicit session ID and fallback true",
			ac: &codingagent.AdapterConfig{
				GatewayURL:       "http://localhost:14000",
				ToolCallFallback: true,
			},
			cfg:       &codingagent.SessionConfig{AgentSessionID: "sess-abc"},
			wantValue: "tern-internal-key-placeholder;fallback=true;sid=sess-abc",
		},
		{
			name:      "no fallback with explicit session ID",
			ac:        &codingagent.AdapterConfig{GatewayURL: "http://localhost:14000"},
			cfg:       &codingagent.SessionConfig{AgentSessionID: "sess-xyz"},
			wantValue: "tern-internal-key-placeholder;fallback=false;sid=sess-xyz",
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

			val, ok := envMap["OPENAI_API_KEY"]
			if !ok {
				t.Fatal("OPENAI_API_KEY not found in env")
			}
			if val != tt.wantValue {
				t.Errorf("OPENAI_API_KEY = %q, want %q", val, tt.wantValue)
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

func TestBuildEnv_CodexGatewayToken(t *testing.T) {
	ac := &codingagent.AdapterConfig{GatewayURL: "http://localhost:14000", GatewayToken: "codex-secret-token"}
	cfg := &codingagent.SessionConfig{}
	env := codex.BuildEnv(ac, cfg)
	envMap := make(map[string]string)
	for _, e := range env {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}

	val, ok := envMap["OPENAI_API_KEY"]
	if !ok {
		t.Fatal("OPENAI_API_KEY not found in env")
	}
	want := "tern-internal-key-placeholder;token=codex-secret-token;fallback=false;sid=default"
	if val != want {
		t.Errorf("OPENAI_API_KEY = %q, want %q", val, want)
	}

	gToken, ok := envMap["TERN_GATEWAY_TOKEN"]
	if !ok {
		t.Fatal("TERN_GATEWAY_TOKEN not found in env")
	}
	if gToken != "codex-secret-token" {
		t.Errorf("TERN_GATEWAY_TOKEN = %q, want %q", gToken, "codex-secret-token")
	}
}
