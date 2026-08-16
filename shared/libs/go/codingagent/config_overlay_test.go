package codingagent_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

func TestOverlayConfigDir(t *testing.T) {
	t.Run("overlays allowlisted entries and leaves projects untouched", func(t *testing.T) {
		configDir := t.TempDir()
		sessionDir := t.TempDir()

		skillPath := filepath.Join(configDir, "skills", "demo", "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(skillPath), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(skillPath, []byte("skill"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(configDir, "settings.json"), []byte(`{}`), 0644); err != nil {
			t.Fatal(err)
		}

		projectsMarker := filepath.Join(sessionDir, "projects", "marker")
		if err := os.MkdirAll(filepath.Dir(projectsMarker), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(projectsMarker, []byte("keep"), 0644); err != nil {
			t.Fatal(err)
		}

		err := codingagent.OverlayConfigDir(sessionDir, configDir, []string{"skills", "settings.json", "missing"})
		if err != nil {
			t.Fatalf("OverlayConfigDir: %v", err)
		}

		gotSkill := filepath.Join(sessionDir, "skills", "demo", "SKILL.md")
		if _, err := os.Stat(gotSkill); err != nil {
			t.Fatalf("expected overlaid skill at %s: %v", gotSkill, err)
		}
		if _, err := os.Stat(filepath.Join(sessionDir, "settings.json")); err != nil {
			t.Fatalf("expected overlaid settings.json: %v", err)
		}
		data, err := os.ReadFile(projectsMarker)
		if err != nil || string(data) != "keep" {
			t.Fatalf("projects marker should survive, got %q err=%v", data, err)
		}
	})

	t.Run("re-apply updates allowlisted names", func(t *testing.T) {
		configDir := t.TempDir()
		sessionDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(configDir, "CLAUDE.md"), []byte("v1"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := codingagent.OverlayConfigDir(sessionDir, configDir, []string{"CLAUDE.md"}); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(configDir, "CLAUDE.md"), []byte("v2"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := codingagent.OverlayConfigDir(sessionDir, configDir, []string{"CLAUDE.md"}); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(sessionDir, "CLAUDE.md"))
		if err != nil {
			t.Fatal(err)
		}
		// Symlink reads live content; copy reads new content after re-apply.
		if string(data) != "v2" {
			t.Fatalf("got %q, want v2", data)
		}
	})

	t.Run("empty paths return error", func(t *testing.T) {
		if err := codingagent.OverlayConfigDir("", t.TempDir(), nil); err == nil {
			t.Fatal("expected error for empty sessionDir")
		}
		if err := codingagent.OverlayConfigDir(t.TempDir(), "", nil); err == nil {
			t.Fatal("expected error for empty configDir")
		}
	})

	t.Run("protected allowlist names are skipped", func(t *testing.T) {
		configDir := t.TempDir()
		sessionDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(configDir, "projects"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(configDir, "projects", "x"), []byte("bad"), 0644); err != nil {
			t.Fatal(err)
		}
		marker := filepath.Join(sessionDir, "projects", "keep")
		if err := os.MkdirAll(filepath.Dir(marker), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(marker, []byte("safe"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := codingagent.OverlayConfigDir(sessionDir, configDir, []string{"projects"}); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(marker)
		if err != nil || string(data) != "safe" {
			t.Fatalf("protected projects should not be replaced, got %q err=%v", data, err)
		}
	})

	t.Run("canonical files are protected", func(t *testing.T) {
		configDir := t.TempDir()
		sessionDir := t.TempDir()
		for _, name := range []string{"record.json", "metadata.json", "context.json"} {
			if err := os.WriteFile(filepath.Join(configDir, name), []byte("overlay"), 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(sessionDir, name), []byte("keep-"+name), 0644); err != nil {
				t.Fatal(err)
			}
		}
		hist := filepath.Join(sessionDir, "history")
		if err := os.MkdirAll(hist, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(hist, "0000001.json"), []byte(`{"seq":1}`), 0644); err != nil {
			t.Fatal(err)
		}
		allow := []string{"record.json", "metadata.json", "context.json", "history", "native"}
		if err := codingagent.OverlayConfigDir(sessionDir, configDir, allow); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(sessionDir, "record.json"))
		if err != nil || string(data) != "keep-record.json" {
			t.Fatalf("record.json replaced: %q err=%v", data, err)
		}
		data, err = os.ReadFile(filepath.Join(sessionDir, "history", "0000001.json"))
		if err != nil || string(data) != `{"seq":1}` {
			t.Fatalf("history replaced: %q err=%v", data, err)
		}
	})
}
