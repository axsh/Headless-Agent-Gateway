package agentservice

import (
	"path/filepath"
	"testing"
)

func TestVendorHomeDir(t *testing.T) {
	tests := []struct {
		name, storageRoot, agent, sessionDir, want string
	}{
		{"codex", "/ws", "codex", "/ws/.tern/s1", filepath.Join("/ws", ".codex")},
		{"claude", "/ws", "claudecode", "/ws/.tern/s1", filepath.Join("/ws", ".claude")},
		{"claude_not_agentname_dir", "/ws", "claudecode", "/ws/.tern/s1", filepath.Join("/ws", ".claude")},
		{"codex_other_root", "/data", "codex", "/ws/.tern/s1", filepath.Join("/data", ".codex")},
		{"claude_other_root", "/data", "claudecode", "/ws/.tern/s1", filepath.Join("/data", ".claude")},
		{"wayfinder_uses_tern_session", "/ws", "wayfinder", "/ws/.tern/s1", "/ws/.tern/s1"},
		{"wayfinder_empty_session", "/ws", "wayfinder", "", ""},
		{"empty_root_codex", "", "codex", "/x", ""},
		{"empty_root_claude", "", "claudecode", "/x", ""},
		{"empty_agent", "/ws", "", "/ws/.tern/s1", ""},
		{"unknown_agent", "/ws", "other", "/ws/.tern/s1", filepath.Join("/ws", ".other")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := VendorHomeDir(tt.storageRoot, tt.agent, tt.sessionDir)
			if got != tt.want {
				t.Fatalf("VendorHomeDir(%q, %q, %q) = %q, want %q", tt.storageRoot, tt.agent, tt.sessionDir, got, tt.want)
			}
			if tt.agent == "claudecode" && got != "" && filepath.Base(got) == ".claudecode" {
				t.Fatalf("claudecode must not use .claudecode dirname, got %q", got)
			}
			if tt.agent == "wayfinder" && got != "" {
				if filepath.Base(got) == "native" || filepath.Base(got) == ".wayfinder" {
					t.Fatalf("wayfinder must use tern session dir, got %q", got)
				}
			}
		})
	}
}
