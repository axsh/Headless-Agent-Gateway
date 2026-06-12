package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// PathValidator validates that a path is within the allowed boundary.
type PathValidator func(workDir, rawPath string) (string, error)

// CommandChecker checks if a command is blocked.
type CommandChecker func(commandLine string) bool

// FileTrackerInterface abstracts file/process tracking.
type FileTrackerInterface interface {
	TrackFile(path string)
	IsTracked(path string) bool
	TrackProcess(pid int, commandLine string)
	UntrackProcess(pid int)
}

// ToolContext holds dependencies for tool handlers.
type ToolContext struct {
	WorkDir          string
	ValidatePath     PathValidator
	IsBlockedCommand CommandChecker
	Tracker          FileTrackerInterface
}

// newReadFile creates the read_file tool handler.
func newReadFile(tc *ToolContext) ToolHandler {
	return func(ctx context.Context, input map[string]any) (string, error) {
		path, _ := input["path"].(string)
		if path == "" {
			return "", fmt.Errorf("read_file: path is required")
		}
		absPath, err := tc.ValidatePath(tc.WorkDir, path)
		if err != nil {
			return "", fmt.Errorf("read_file: %w", err)
		}
		data, err := os.ReadFile(absPath)
		if err != nil {
			return "", fmt.Errorf("read_file: %w", err)
		}
		return string(data), nil
	}
}

// newWriteFile creates the write_file tool handler.
func newWriteFile(tc *ToolContext) ToolHandler {
	return func(ctx context.Context, input map[string]any) (string, error) {
		path, _ := input["path"].(string)
		content, _ := input["content"].(string)
		if path == "" {
			return "", fmt.Errorf("write_file: path is required")
		}
		absPath, err := tc.ValidatePath(tc.WorkDir, path)
		if err != nil {
			return "", fmt.Errorf("write_file: %w", err)
		}

		// Ensure parent directory exists.
		if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
			return "", fmt.Errorf("write_file: %w", err)
		}
		if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {
			return "", fmt.Errorf("write_file: %w", err)
		}
		tc.Tracker.TrackFile(absPath)
		return fmt.Sprintf("File written: %s (%d bytes)", absPath, len(content)), nil
	}
}

// newListDirectory creates the list_directory tool handler.
func newListDirectory(tc *ToolContext) ToolHandler {
	return func(ctx context.Context, input map[string]any) (string, error) {
		path, _ := input["path"].(string)
		if path == "" {
			path = "."
		}
		absPath, err := tc.ValidatePath(tc.WorkDir, path)
		if err != nil {
			return "", fmt.Errorf("list_directory: %w", err)
		}
		entries, err := os.ReadDir(absPath)
		if err != nil {
			return "", fmt.Errorf("list_directory: %w", err)
		}
		var result string
		for _, e := range entries {
			info, _ := e.Info()
			size := int64(0)
			if info != nil {
				size = info.Size()
			}
			entryType := "file"
			if e.IsDir() {
				entryType = "dir"
			}
			result += fmt.Sprintf("%s\t%s\t%d\n", e.Name(), entryType, size)
		}
		return result, nil
	}
}

// newCreateDirectory creates the create_directory tool handler.
func newCreateDirectory(tc *ToolContext) ToolHandler {
	return func(ctx context.Context, input map[string]any) (string, error) {
		path, _ := input["path"].(string)
		if path == "" {
			return "", fmt.Errorf("create_directory: path is required")
		}
		absPath, err := tc.ValidatePath(tc.WorkDir, path)
		if err != nil {
			return "", fmt.Errorf("create_directory: %w", err)
		}
		if err := os.MkdirAll(absPath, 0755); err != nil {
			return "", fmt.Errorf("create_directory: %w", err)
		}
		tc.Tracker.TrackFile(absPath)
		return fmt.Sprintf("Directory created: %s", absPath), nil
	}
}

// newEditFile creates the edit_file tool handler.
// It performs a simple string replacement in the file.
func newEditFile(tc *ToolContext) ToolHandler {
	return func(ctx context.Context, input map[string]any) (string, error) {
		path, _ := input["path"].(string)
		oldText, _ := input["old_text"].(string)
		newText, _ := input["new_text"].(string)
		if path == "" {
			return "", fmt.Errorf("edit_file: path is required")
		}
		if oldText == "" {
			return "", fmt.Errorf("edit_file: old_text is required")
		}
		absPath, err := tc.ValidatePath(tc.WorkDir, path)
		if err != nil {
			return "", fmt.Errorf("edit_file: %w", err)
		}
		data, err := os.ReadFile(absPath)
		if err != nil {
			return "", fmt.Errorf("edit_file: %w", err)
		}
		content := string(data)
		if count := countOccurrences(content, oldText); count == 0 {
			return "", fmt.Errorf("edit_file: old_text not found in %s", path)
		} else if count > 1 {
			return "", fmt.Errorf("edit_file: old_text found %d times in %s, must be unique", count, path)
		}
		newContent := replaceFirst(content, oldText, newText)
		if err := os.WriteFile(absPath, []byte(newContent), 0644); err != nil {
			return "", fmt.Errorf("edit_file: %w", err)
		}
		return fmt.Sprintf("File edited: %s", absPath), nil
	}
}
