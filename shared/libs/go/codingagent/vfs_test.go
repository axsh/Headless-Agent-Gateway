package codingagent_test

import (
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

func TestVfsToContainerPath(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"vfs://workspace/", "/workspace"},
		{"vfs://workspace/data/", "/workspace/data"},
		{"vfs://workspace/sub/dir", "/workspace/sub/dir"},
		{"vfs:///", "/"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := codingagent.VfsToContainerPath(tt.input)
			if got != tt.expected {
				t.Errorf("VfsToContainerPath(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestPhysicalToHostPath(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"file:///home/user/project", "/home/user/project"},
		{"/already/native/path", "/already/native/path"},
		{"file:///workspace", "/workspace"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := codingagent.PhysicalToHostPath(tt.input)
			if got != tt.expected {
				t.Errorf("PhysicalToHostPath(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestSortVFSMounts(t *testing.T) {
	mounts := []codingagent.VFSMount{
		{VFSPath: "vfs://workspace/data/sub/", PhysicalPath: "file:///c"},
		{VFSPath: "vfs://workspace/", PhysicalPath: "file:///a"},
		{VFSPath: "vfs://workspace/data/", PhysicalPath: "file:///b"},
	}

	codingagent.SortVFSMounts(mounts)

	if mounts[0].PhysicalPath != "file:///a" {
		t.Errorf("first mount should be parent, got %v", mounts[0].PhysicalPath)
	}
	if mounts[1].PhysicalPath != "file:///b" {
		t.Errorf("second mount should be mid-level, got %v", mounts[1].PhysicalPath)
	}
	if mounts[2].PhysicalPath != "file:///c" {
		t.Errorf("third mount should be deepest, got %v", mounts[2].PhysicalPath)
	}
}

func TestVFSMountsToDockerArgs(t *testing.T) {
	t.Run("single mount", func(t *testing.T) {
		mounts := []codingagent.VFSMount{
			{VFSPath: "vfs://workspace/", PhysicalPath: "file:///home/user/project"},
		}
		args := codingagent.VFSMountsToDockerArgs(mounts)
		if len(args) != 2 || args[0] != "-v" || args[1] != "/home/user/project:/workspace" {
			t.Errorf("args = %v, want [-v /home/user/project:/workspace]", args)
		}
	})

	t.Run("multiple mounts sorted parent first", func(t *testing.T) {
		mounts := []codingagent.VFSMount{
			{VFSPath: "vfs://workspace/data/", PhysicalPath: "file:///data"},
			{VFSPath: "vfs://workspace/", PhysicalPath: "file:///root"},
		}
		args := codingagent.VFSMountsToDockerArgs(mounts)
		// Parent (workspace/) should come first
		if len(args) != 4 {
			t.Fatalf("args length = %d, want 4", len(args))
		}
		if args[1] != "/root:/workspace" {
			t.Errorf("first -v arg = %v, want /root:/workspace", args[1])
		}
		if args[3] != "/data:/workspace/data" {
			t.Errorf("second -v arg = %v, want /data:/workspace/data", args[3])
		}
	})
}
