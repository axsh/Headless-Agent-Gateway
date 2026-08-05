package codex_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent/codex"
)

func TestApplyCodexConfigDir(t *testing.T) {
	t.Run("empty configDir is no-op", func(t *testing.T) {
		if err := codex.ApplyCodexConfigDir(t.TempDir(), ""); err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
	})

	t.Run("overlays skills and config.toml; preserves sessions", func(t *testing.T) {
		configDir := t.TempDir()
		sessionDir := t.TempDir()

		skill := filepath.Join(configDir, "skills", "demo", "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(skill), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(skill, []byte("demo"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte("model = \"x\"\n"), 0644); err != nil {
			t.Fatal(err)
		}

		keep := filepath.Join(sessionDir, "sessions", "keep")
		if err := os.MkdirAll(filepath.Dir(keep), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(keep, []byte("safe"), 0644); err != nil {
			t.Fatal(err)
		}

		if err := codex.ApplyCodexConfigDir(sessionDir, configDir); err != nil {
			t.Fatalf("ApplyCodexConfigDir: %v", err)
		}
		if _, err := os.Stat(filepath.Join(sessionDir, "skills", "demo", "SKILL.md")); err != nil {
			t.Fatalf("skill missing: %v", err)
		}
		if _, err := os.Stat(filepath.Join(sessionDir, "config.toml")); err != nil {
			t.Fatalf("config.toml missing: %v", err)
		}
		data, err := os.ReadFile(keep)
		if err != nil || string(data) != "safe" {
			t.Fatalf("sessions keep lost: %q %v", data, err)
		}
	})
}
