package claudecode_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent/claudecode"
)

func TestApplyClaudeConfigDir(t *testing.T) {
	t.Run("empty configDir is no-op", func(t *testing.T) {
		if err := claudecode.ApplyClaudeConfigDir(t.TempDir(), ""); err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
	})

	t.Run("overlays skills and preserves projects", func(t *testing.T) {
		configDir := t.TempDir()
		sessionDir := t.TempDir()

		skill := filepath.Join(configDir, "skills", "demo", "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(skill), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(skill, []byte("demo"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(configDir, "settings.json"), []byte(`{}`), 0644); err != nil {
			t.Fatal(err)
		}

		marker := filepath.Join(sessionDir, "projects", "marker")
		if err := os.MkdirAll(filepath.Dir(marker), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(marker, []byte("keep"), 0644); err != nil {
			t.Fatal(err)
		}

		if err := claudecode.ApplyClaudeConfigDir(sessionDir, configDir); err != nil {
			t.Fatalf("ApplyClaudeConfigDir: %v", err)
		}

		if _, err := os.Stat(filepath.Join(sessionDir, "skills", "demo", "SKILL.md")); err != nil {
			t.Fatalf("skill missing: %v", err)
		}
		data, err := os.ReadFile(marker)
		if err != nil || string(data) != "keep" {
			t.Fatalf("projects marker lost: %q %v", data, err)
		}
	})
}
