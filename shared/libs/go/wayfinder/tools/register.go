package tools

import "github.com/axsh/arctic-tern/wayfinder"

// RegisterAllTools registers all 9 built-in tools with the registry.
func RegisterAllTools(reg *Registry, workDir string, tracker *wayfinder.FileTracker) {
	reg.Register("read_file", "Read the contents of a file",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "description": "File path to read"},
			},
			"required": []string{"path"},
		}, newReadFile(workDir))

	reg.Register("write_file", "Write content to a file (creates or overwrites)",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string", "description": "File path to write"},
				"content": map[string]any{"type": "string", "description": "Content to write"},
			},
			"required": []string{"path", "content"},
		}, newWriteFile(workDir, tracker))

	reg.Register("list_directory", "List contents of a directory",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "description": "Directory path (default: current directory)"},
			},
		}, newListDirectory(workDir))

	reg.Register("create_directory", "Create a directory (and parent directories)",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "description": "Directory path to create"},
			},
			"required": []string{"path"},
		}, newCreateDirectory(workDir, tracker))

	reg.Register("edit_file", "Edit a file by replacing a unique text string",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":     map[string]any{"type": "string", "description": "File path to edit"},
				"old_text": map[string]any{"type": "string", "description": "Exact text to replace (must be unique)"},
				"new_text": map[string]any{"type": "string", "description": "Replacement text"},
			},
			"required": []string{"path", "old_text", "new_text"},
		}, newEditFile(workDir))

	reg.Register("search_files", "Search for files matching a glob pattern",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{"type": "string", "description": "File name glob pattern (e.g. *.go)"},
				"path":    map[string]any{"type": "string", "description": "Directory to search (default: current)"},
			},
			"required": []string{"pattern"},
		}, newSearchFiles(workDir))

	reg.Register("grep_files", "Search file contents for a text pattern",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{"type": "string", "description": "Text pattern to search for"},
				"path":    map[string]any{"type": "string", "description": "Directory to search (default: current)"},
			},
			"required": []string{"pattern"},
		}, newGrepFiles(workDir))

	reg.Register("execute_command", "Execute a shell command",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command":    map[string]any{"type": "string", "description": "Shell command to execute"},
				"background": map[string]any{"type": "boolean", "description": "Run in background (default: false)"},
			},
			"required": []string{"command"},
		}, newExecuteCommand(workDir, tracker))

	reg.Register("kill_process", "Kill a background process by PID",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pid": map[string]any{"type": "number", "description": "Process ID to kill"},
			},
			"required": []string{"pid"},
		}, newKillProcess(tracker))
}
