// Package analyzer provides git-based change detection for session reconciliation.
//
// Limitations: paths listed in .gitignore are NOT detected by git diff or
// git ls-files --others --exclude-standard. Use Tier 1/2 realtime tracking for those files.
package analyzer

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/axsh/arctic-tern/shared/libs/go/artifact/store"
)

// GitDiffResult holds paths detected via git commands.
type GitDiffResult struct {
	Path      string
	Operation string // store.OperationCreate | update | delete
	Source    string // always "git"
}

// IsGitRepo reports whether workDir contains a .git directory.
func IsGitRepo(workDir string) bool {
	st, err := os.Stat(filepath.Join(workDir, ".git"))
	return err == nil && st.IsDir()
}

// DetectGitChanges runs git diff --name-status HEAD and
// git ls-files --others --exclude-standard in workDir.
// Returns empty slice if workDir is not a git repository.
func DetectGitChanges(workDir string) ([]GitDiffResult, error) {
	if !IsGitRepo(workDir) {
		return nil, nil
	}

	seen := make(map[string]string)

	if hasGitHead(workDir) {
		diffOut, err := exec.Command("git", "-C", workDir, "diff", "--name-status", "HEAD").Output()
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(string(diffOut), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			parts := strings.Fields(line)
			if len(parts) < 2 {
				continue
			}
			op := gitStatusToOperation(parts[0])
			if op == "" {
				continue
			}
			path := parts[len(parts)-1]
			seen[path] = op
		}
	}

	untrackedOut, err := exec.Command("git", "-C", workDir, "ls-files", "--others", "--exclude-standard").Output()
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(untrackedOut), "\n") {
		path := strings.TrimSpace(line)
		if path == "" {
			continue
		}
		if _, ok := seen[path]; !ok {
			seen[path] = store.OperationCreate
		}
	}

	out := make([]GitDiffResult, 0, len(seen))
	for path, op := range seen {
		out = append(out, GitDiffResult{Path: path, Operation: op, Source: "git"})
	}
	return out, nil
}

func hasGitHead(workDir string) bool {
	err := exec.Command("git", "-C", workDir, "rev-parse", "HEAD").Run()
	return err == nil
}

func gitStatusToOperation(status string) string {
	switch status {
	case "A", "??":
		return store.OperationCreate
	case "M":
		return store.OperationUpdate
	case "D":
		return store.OperationDelete
	default:
		if strings.HasPrefix(status, "R") {
			return store.OperationUpdate
		}
		return ""
	}
}
