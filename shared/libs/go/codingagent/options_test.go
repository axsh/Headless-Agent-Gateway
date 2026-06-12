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

		wantWorkDir, _ := filepath.Abs("/default/work")
		if cfg.WorkDir != wantWorkDir {
			t.Errorf("WorkDir = %v, want %v", cfg.WorkDir, wantWorkDir)
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
		wantWorkDir, _ := filepath.Abs("/explicit/dir")
		if cfg.WorkDir != wantWorkDir {
			t.Errorf("WorkDir = %v, want %v", cfg.WorkDir, wantWorkDir)
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
		wantDir, _ := filepath.Abs("/workspace/project")
		if cfg.SessionDir != wantDir {
			t.Errorf("SessionDir = %v, want %v", cfg.SessionDir, wantDir)
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
		absWorkDir, _ := filepath.Abs("/workspace/project")
		want := filepath.Join(absWorkDir, ".claudecode")
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
		wantDir, _ := filepath.Abs("/data/sessions")
		if cfg.SessionDir != wantDir {
			t.Errorf("SessionDir = %v, want %v", cfg.SessionDir, wantDir)
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
		wantDir, _ := filepath.Abs("/default/sessions")
		if cfg.SessionDir != wantDir {
			t.Errorf("SessionDir = %v, want %v", cfg.SessionDir, wantDir)
		}
	})

	t.Run("relative WorkDir is resolved to absolute path", func(t *testing.T) {
		cfg := codingagent.NewSessionConfig(
			codingagent.WithWorkDir("relative/path"),
		)
		ac := &codingagent.AdapterConfig{}
		codingagent.ApplyDefaults(cfg, ac)

		if !filepath.IsAbs(cfg.WorkDir) {
			t.Errorf("WorkDir should be absolute, got %q", cfg.WorkDir)
		}
		wantWorkDir, _ := filepath.Abs("relative/path")
		if cfg.WorkDir != wantWorkDir {
			t.Errorf("WorkDir = %q, want %q", cfg.WorkDir, wantWorkDir)
		}
	})

	t.Run("relative SessionDir is resolved to absolute path", func(t *testing.T) {
		cfg := codingagent.NewSessionConfig(
			codingagent.WithSessionDir("rel/session"),
		)
		ac := &codingagent.AdapterConfig{}
		codingagent.ApplyDefaults(cfg, ac)

		if !filepath.IsAbs(cfg.SessionDir) {
			t.Errorf("SessionDir should be absolute, got %q", cfg.SessionDir)
		}
		wantSessionDir, _ := filepath.Abs("rel/session")
		if cfg.SessionDir != wantSessionDir {
			t.Errorf("SessionDir = %q, want %q", cfg.SessionDir, wantSessionDir)
		}
	})

	t.Run("SessionDir fallback with relative WorkDir produces absolute path", func(t *testing.T) {
		cfg := codingagent.NewSessionConfig(
			codingagent.WithWorkDir("tmp"),
		)
		ac := &codingagent.AdapterConfig{
			AgentName: "claudecode",
		}
		codingagent.ApplyDefaults(cfg, ac)

		// Both WorkDir and SessionDir should be absolute.
		if !filepath.IsAbs(cfg.WorkDir) {
			t.Errorf("WorkDir should be absolute, got %q", cfg.WorkDir)
		}
		if !filepath.IsAbs(cfg.SessionDir) {
			t.Errorf("SessionDir should be absolute, got %q", cfg.SessionDir)
		}
		absWorkDir, _ := filepath.Abs("tmp")
		wantSessionDir := filepath.Join(absWorkDir, ".claudecode")
		if cfg.SessionDir != wantSessionDir {
			t.Errorf("SessionDir = %q, want %q", cfg.SessionDir, wantSessionDir)
		}
	})

	t.Run("absolute WorkDir and SessionDir are not modified", func(t *testing.T) {
		// Use filepath.Abs to get platform-appropriate absolute paths.
		// On Unix /absolute/work is already absolute; on Windows it gets
		// a drive letter prefix, but the key property is that Abs(Abs(x)) == Abs(x).
		wantWork, _ := filepath.Abs("/absolute/work")
		wantSession, _ := filepath.Abs("/absolute/session")
		cfg := codingagent.NewSessionConfig(
			codingagent.WithWorkDir(wantWork),
			codingagent.WithSessionDir(wantSession),
		)
		ac := &codingagent.AdapterConfig{
			AgentName: "claudecode",
		}
		codingagent.ApplyDefaults(cfg, ac)

		if cfg.WorkDir != wantWork {
			t.Errorf("WorkDir = %q, want %q", cfg.WorkDir, wantWork)
		}
		if cfg.SessionDir != wantSession {
			t.Errorf("SessionDir = %q, want %q", cfg.SessionDir, wantSession)
		}
	})
}
