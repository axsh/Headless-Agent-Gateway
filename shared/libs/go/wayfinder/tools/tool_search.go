package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// newSearchFiles creates the search_files tool handler.
func newSearchFiles(tc *ToolContext) ToolHandler {
	return func(ctx context.Context, input map[string]any) (string, error) {
		pattern, _ := input["pattern"].(string)
		searchPath, _ := input["path"].(string)
		if pattern == "" {
			return "", fmt.Errorf("search_files: pattern is required")
		}
		if searchPath == "" {
			searchPath = "."
		}
		absPath, err := tc.ValidatePath(tc.WorkDir, searchPath)
		if err != nil {
			return "", fmt.Errorf("search_files: %w", err)
		}

		var matches []string
		err = filepath.Walk(absPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() {
				return nil
			}
			matched, _ := filepath.Match(pattern, info.Name())
			if matched {
				relPath, _ := filepath.Rel(tc.WorkDir, path)
				matches = append(matches, relPath)
			}
			return nil
		})
		if err != nil {
			return "", fmt.Errorf("search_files: %w", err)
		}

		if len(matches) == 0 {
			return "No files found matching pattern: " + pattern, nil
		}
		return strings.Join(matches, "\n"), nil
	}
}

// newGrepFiles creates the grep_files tool handler.
func newGrepFiles(tc *ToolContext) ToolHandler {
	return func(ctx context.Context, input map[string]any) (string, error) {
		pattern, _ := input["pattern"].(string)
		searchPath, _ := input["path"].(string)
		if pattern == "" {
			return "", fmt.Errorf("grep_files: pattern is required")
		}
		if searchPath == "" {
			searchPath = "."
		}
		absPath, err := tc.ValidatePath(tc.WorkDir, searchPath)
		if err != nil {
			return "", fmt.Errorf("grep_files: %w", err)
		}

		var results []string
		err = filepath.Walk(absPath, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if info.Size() > 1<<20 {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			lines := strings.Split(string(data), "\n")
			relPath, _ := filepath.Rel(tc.WorkDir, path)
			for i, line := range lines {
				if strings.Contains(line, pattern) {
					results = append(results, fmt.Sprintf("%s:%d:%s", relPath, i+1, line))
				}
			}
			return nil
		})
		if err != nil {
			return "", fmt.Errorf("grep_files: %w", err)
		}

		if len(results) == 0 {
			return "No matches found for pattern: " + pattern, nil
		}
		if len(results) > 50 {
			results = results[:50]
			results = append(results, "... (results truncated at 50 matches)")
		}
		return strings.Join(results, "\n"), nil
	}
}
