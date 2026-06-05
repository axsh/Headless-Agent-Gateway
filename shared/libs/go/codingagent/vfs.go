package codingagent

import (
	"net/url"
	"sort"
	"strings"
)

// VFSMount defines a host path <-> container path mapping.
type VFSMount struct {
	VFSPath      string // Logical path (e.g., "vfs://workspace/")
	PhysicalPath string // Host physical path (e.g., "file:///home/user/project")
}

// VfsToContainerPath converts a VFS URI to a container path.
// "vfs://workspace/"      -> "/workspace"
// "vfs://workspace/data/" -> "/workspace/data"
func VfsToContainerPath(vfsPath string) string {
	path := strings.TrimPrefix(vfsPath, "vfs://")
	path = strings.TrimRight(path, "/")
	if path == "" {
		return "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}

// PhysicalToHostPath converts a "file://" URI to a native file path.
// "file:///home/user/project" -> "/home/user/project"
func PhysicalToHostPath(physical string) string {
	if u, err := url.Parse(physical); err == nil && u.Scheme == "file" {
		return u.Path
	}
	return physical
}

// SortVFSMounts sorts mounts by parent directory first (ascending VFSPath length).
func SortVFSMounts(mounts []VFSMount) {
	sort.SliceStable(mounts, func(i, j int) bool {
		return len(mounts[i].VFSPath) < len(mounts[j].VFSPath)
	})
}

// VFSMountsToDockerArgs generates Docker -v arguments from VFSMount list.
// Returns: ["-v", "/host/path:/container/path", "-v", ...]
func VFSMountsToDockerArgs(mounts []VFSMount) []string {
	sorted := make([]VFSMount, len(mounts))
	copy(sorted, mounts)
	SortVFSMounts(sorted)

	var args []string
	for _, m := range sorted {
		hostPath := PhysicalToHostPath(m.PhysicalPath)
		containerPath := VfsToContainerPath(m.VFSPath)
		args = append(args, "-v", hostPath+":"+containerPath)
	}
	return args
}
