package codingagent_test

import (
	"path/filepath"
	"testing"

	"github.com/axsh/arctic-tern/codingagent"
)

func TestSessionOptionFunctions(t *testing.T) {
	tests := []struct {
		name  string
		opt   codingagent.SessionOption
		check func(t *testing.T, cfg *codingagent.SessionConfig)
	}{
		{
			name: "WithModel",
			opt:  codingagent.WithModel("anthropic/claude-sonnet-4"),
			check: func(t *testing.T, cfg *codingagent.SessionConfig) {
				if cfg.Model != "anthropic/claude-sonnet-4" {
					t.Errorf("Model = %v, want anthropic/claude-sonnet-4", cfg.Model)
				}
			},
		},
		{
			name: "WithPrompt",
			opt:  codingagent.WithPrompt("hello"),
			check: func(t *testing.T, cfg *codingagent.SessionConfig) {
				if cfg.Prompt != "hello" {
					t.Errorf("Prompt = %v, want hello", cfg.Prompt)
				}
			},
		},
		{
			name: "WithAllowedTools",
			opt:  codingagent.WithAllowedTools([]string{"Read", "Write"}),
			check: func(t *testing.T, cfg *codingagent.SessionConfig) {
				if len(cfg.AllowedTools) != 2 || cfg.AllowedTools[0] != "Read" {
					t.Errorf("AllowedTools = %v, want [Read Write]", cfg.AllowedTools)
				}
			},
		},
		{
			name: "WithWorkDir",
			opt:  codingagent.WithWorkDir("/workspace"),
			check: func(t *testing.T, cfg *codingagent.SessionConfig) {
				if cfg.WorkDir != "/workspace" {
					t.Errorf("WorkDir = %v, want /workspace", cfg.WorkDir)
				}
			},
		},
		{
			name: "WithEnvVars",
			opt:  codingagent.WithEnvVars(map[string]string{"KEY": "VALUE"}),
			check: func(t *testing.T, cfg *codingagent.SessionConfig) {
				if cfg.EnvVars["KEY"] != "VALUE" {
					t.Errorf("EnvVars[KEY] = %v, want VALUE", cfg.EnvVars["KEY"])
				}
			},
		},
		{
			name: "WithAgentSessionID",
			opt:  codingagent.WithAgentSessionID("sdk-123"),
			check: func(t *testing.T, cfg *codingagent.SessionConfig) {
				if cfg.AgentSessionID != "sdk-123" {
					t.Errorf("AgentSessionID = %v, want sdk-123", cfg.AgentSessionID)
				}
			},
		},
		{
			name: "WithVFSMounts",
			opt: codingagent.WithVFSMounts([]codingagent.VFSMount{
				{VFSPath: "vfs://workspace/", PhysicalPath: "file:///home/user/project"},
			}),
			check: func(t *testing.T, cfg *codingagent.SessionConfig) {
				if len(cfg.VFSMounts) != 1 || cfg.VFSMounts[0].VFSPath != "vfs://workspace/" {
					t.Errorf("VFSMounts = %v, want 1 mount", cfg.VFSMounts)
				}
			},
		},
		{
			name: "WithSessionDir",
			opt:  codingagent.WithSessionDir("/data/sessions"),
			check: func(t *testing.T, cfg *codingagent.SessionConfig) {
				if cfg.SessionDir != "/data/sessions" {
					t.Errorf("SessionDir = %v, want /data/sessions", cfg.SessionDir)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := codingagent.NewSessionConfig(tt.opt)
			tt.check(t, cfg)
		})
	}
}

func TestSessionOptionComposition(t *testing.T) {
	cfg := codingagent.NewSessionConfig(
		codingagent.WithModel("model-a"),
		codingagent.WithPrompt("prompt-a"),
		codingagent.WithWorkDir("/dir-a"),
		codingagent.WithModel("model-b"), // overrides model-a
	)

	if cfg.Model != "model-b" {
		t.Errorf("Model = %v, want model-b (later option should override)", cfg.Model)
	}
	if cfg.Prompt != "prompt-a" {
		t.Errorf("Prompt = %v, want prompt-a", cfg.Prompt)
	}
	if cfg.WorkDir != "/dir-a" {
		t.Errorf("WorkDir = %v, want /dir-a", cfg.WorkDir)
	}
}

func TestApplyDefaults(t *testing.T) {
	t.Run("defaults applied when fields are zero", func(t *testing.T) {
		cfg := codingagent.NewSessionConfig()
		ac := &codingagent.AdapterConfig{
			DefaultWorkDir: "/default/work",
			DefaultModel:   "default-model",
			DefaultEnvVars: map[string]string{"A": "1"},
		}
		codingagent.ApplyDefaults(cfg, ac)

		if cfg.WorkDir != "/default/work" {
			t.Errorf("WorkDir = %v, want /default/work", cfg.WorkDir)
		}
		if cfg.Model != "default-model" {
			t.Errorf("Model = %v, want default-model", cfg.Model)
		}
		if cfg.EnvVars["A"] != "1" {
			t.Errorf("EnvVars[A] = %v, want 1", cfg.EnvVars["A"])
		}
	})

	t.Run("session options take priority over defaults", func(t *testing.T) {
		cfg := codingagent.NewSessionConfig(
			codingagent.WithModel("explicit-model"),
			codingagent.WithWorkDir("/explicit/dir"),
		)
		ac := &codingagent.AdapterConfig{
			DefaultWorkDir: "/default/work",
			DefaultModel:   "default-model",
		}
		codingagent.ApplyDefaults(cfg, ac)

		if cfg.Model != "explicit-model" {
			t.Errorf("Model = %v, want explicit-model", cfg.Model)
		}
		if cfg.WorkDir != "/explicit/dir" {
			t.Errorf("WorkDir = %v, want /explicit/dir", cfg.WorkDir)
		}
	})

	t.Run("env vars not overwritten when already set", func(t *testing.T) {
		cfg := codingagent.NewSessionConfig(
			codingagent.WithEnvVars(map[string]string{"X": "Y"}),
		)
		ac := &codingagent.AdapterConfig{
			DefaultEnvVars: map[string]string{"A": "1"},
		}
		codingagent.ApplyDefaults(cfg, ac)

		if cfg.EnvVars["X"] != "Y" {
			t.Errorf("EnvVars[X] = %v, want Y", cfg.EnvVars["X"])
		}
		if _, ok := cfg.EnvVars["A"]; ok {
			t.Error("EnvVars[A] should not be set when EnvVars already specified")
		}
	})

	t.Run("session dir falls back to work dir when no agent name", func(t *testing.T) {
		cfg := codingagent.NewSessionConfig(
			codingagent.WithWorkDir("/workspace/project"),
		)
		ac := &codingagent.AdapterConfig{}
		codingagent.ApplyDefaults(cfg, ac)
		if cfg.SessionDir != "/workspace/project" {
			t.Errorf("SessionDir = %v, want /workspace/project", cfg.SessionDir)
		}
	})

	t.Run("session dir includes agent name when set", func(t *testing.T) {
		cfg := codingagent.NewSessionConfig(
			codingagent.WithWorkDir("/workspace/project"),
		)
		ac := &codingagent.AdapterConfig{
			AgentName: "claudecode",
		}
		codingagent.ApplyDefaults(cfg, ac)
		want := filepath.Join("/workspace/project", ".claudecode")
		if cfg.SessionDir != want {
			t.Errorf("SessionDir = %v, want %v", cfg.SessionDir, want)
		}
	})

	t.Run("explicit session dir takes priority", func(t *testing.T) {
		cfg := codingagent.NewSessionConfig(
			codingagent.WithWorkDir("/workspace/project"),
			codingagent.WithSessionDir("/data/sessions"),
		)
		ac := &codingagent.AdapterConfig{
			AgentName: "claudecode",
		}
		codingagent.ApplyDefaults(cfg, ac)
		if cfg.SessionDir != "/data/sessions" {
			t.Errorf("SessionDir = %v, want /data/sessions", cfg.SessionDir)
		}
	})

	t.Run("default session dir from adapter config", func(t *testing.T) {
		cfg := codingagent.NewSessionConfig(
			codingagent.WithWorkDir("/workspace/project"),
		)
		ac := &codingagent.AdapterConfig{
			DefaultSessionDir: "/default/sessions",
			AgentName:         "claudecode",
		}
		codingagent.ApplyDefaults(cfg, ac)
		if cfg.SessionDir != "/default/sessions" {
			t.Errorf("SessionDir = %v, want /default/sessions", cfg.SessionDir)
		}
	})
}
