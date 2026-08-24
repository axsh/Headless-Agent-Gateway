package agentservice

import (
	"path/filepath"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

func TestResolveStorageRoot(t *testing.T) {
	tests := []struct {
		name, storageRoot, workDir, want string
	}{
		{"empty_uses_work_dir", "", "/ws", "/ws"},
		{"explicit_storage_root", "/data", "/ws", "/data"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveStorageRoot(tt.storageRoot, tt.workDir)
			if got != tt.want {
				t.Fatalf("ResolveStorageRoot(%q, %q) = %q, want %q", tt.storageRoot, tt.workDir, got, tt.want)
			}
		})
	}
}

func TestWayfinderDir(t *testing.T) {
	got := WayfinderDir("/data")
	want := filepath.Join("/data", ".tern")
	if got != want {
		t.Fatalf("WayfinderDir = %q, want %q", got, want)
	}
	if filepath.Base(got) == "session-id" {
		t.Fatal("wayfinder_dir must not include session_id")
	}
}

func TestCanonicalSessionDir(t *testing.T) {
	got := CanonicalSessionDir("/data", "abc")
	want := filepath.Join("/data", ".tern", "abc")
	if got != want {
		t.Fatalf("CanonicalSessionDir = %q, want %q", got, want)
	}
}

func TestEffectiveStorageRoot(t *testing.T) {
	tests := []struct {
		name string
		rec  *codingagent.SessionRecord
		want string
	}{
		{
			name: "storage_root_set",
			rec:  &codingagent.SessionRecord{StorageRoot: "/data", WorkDir: "/ws"},
			want: "/data",
		},
		{
			name: "legacy_work_dir_fallback",
			rec:  &codingagent.SessionRecord{WorkDir: "/ws"},
			want: "/ws",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EffectiveStorageRoot(tt.rec)
			if got != tt.want {
				t.Fatalf("EffectiveStorageRoot = %q, want %q", got, tt.want)
			}
		})
	}
}
