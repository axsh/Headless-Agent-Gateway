package wayfinder

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitConfig_DefaultWorkDir(t *testing.T) {
	cfg := &AgentConfig{}
	if err := InitConfig(cfg); err != nil {
		t.Fatalf("InitConfig failed: %v", err)
	}
	cwd, _ := os.Getwd()
	absCwd, _ := filepath.Abs(cwd)
	if cfg.WorkDir != absCwd {
		t.Errorf("WorkDir = %q, want %q", cfg.WorkDir, absCwd)
	}
}

func TestInitConfig_DefaultSessionDir(t *testing.T) {
	cfg := &AgentConfig{WorkDir: t.TempDir()}
	if err := InitConfig(cfg); err != nil {
		t.Fatalf("InitConfig failed: %v", err)
	}
	want := filepath.Join(cfg.WorkDir, ".claudecode")
	if cfg.SessionDir != want {
		t.Errorf("SessionDir = %q, want %q", cfg.SessionDir, want)
	}
}

func TestInitConfig_ExplicitPaths(t *testing.T) {
	tmpDir := t.TempDir()
	workDir := filepath.Join(tmpDir, "work")
	sessionDir := filepath.Join(tmpDir, "sessions")
	os.MkdirAll(workDir, 0755)
	os.MkdirAll(sessionDir, 0755)

	cfg := &AgentConfig{
		WorkDir:    workDir,
		SessionDir: sessionDir,
	}
	if err := InitConfig(cfg); err != nil {
		t.Fatalf("InitConfig failed: %v", err)
	}
	absWork, _ := filepath.Abs(workDir)
	absSession, _ := filepath.Abs(sessionDir)
	if cfg.WorkDir != absWork {
		t.Errorf("WorkDir = %q, want %q", cfg.WorkDir, absWork)
	}
	if cfg.SessionDir != absSession {
		t.Errorf("SessionDir = %q, want %q", cfg.SessionDir, absSession)
	}
}

func TestInitConfig_RelativeWorkDir(t *testing.T) {
	cfg := &AgentConfig{WorkDir: "."}
	if err := InitConfig(cfg); err != nil {
		t.Fatalf("InitConfig failed: %v", err)
	}
	if !filepath.IsAbs(cfg.WorkDir) {
		t.Errorf("WorkDir should be absolute, got %q", cfg.WorkDir)
	}
	if !filepath.IsAbs(cfg.SessionDir) {
		t.Errorf("SessionDir should be absolute, got %q", cfg.SessionDir)
	}
}

func TestInitConfig_AllowedPathPatterns_Default(t *testing.T) {
	cfg := &AgentConfig{WorkDir: t.TempDir()}
	if err := InitConfig(cfg); err != nil {
		t.Fatalf("InitConfig failed: %v", err)
	}
	if cfg.AllowedPathPatterns != nil && len(cfg.AllowedPathPatterns) != 0 {
		t.Errorf("AllowedPathPatterns should be nil/empty by default, got %v", cfg.AllowedPathPatterns)
	}
}
