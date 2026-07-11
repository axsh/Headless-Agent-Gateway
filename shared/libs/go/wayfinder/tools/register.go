package tools

// RegisterAllTools registers all 10 built-in tools with the registry.
func RegisterAllTools(reg *Registry, tc *ToolContext) {
	reg.Register("read_file", "Read the contents of a file",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "description": "File path to read"},
			},
			"required": []string{"path"},
		}, newReadFile(tc))

	reg.Register("write_file", "Write content to a file (creates or overwrites)",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string", "description": "File path to write"},
				"content": map[string]any{"type": "string", "description": "Content to write"},
			},
			"required": []string{"path", "content"},
		}, newWriteFile(tc))

	reg.Register("list_directory", "List contents of a directory",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "description": "Directory path (default: current directory)"},
			},
		}, newListDirectory(tc))

	reg.Register("create_directory", "Create a directory (and parent directories)",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "description": "Directory path to create"},
			},
			"required": []string{"path"},
		}, newCreateDirectory(tc))

	reg.Register("edit_file", "Edit a file by replacing a unique text string",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":     map[string]any{"type": "string", "description": "File path to edit"},
				"old_text": map[string]any{"type": "string", "description": "Exact text to replace (must be unique)"},
				"new_text": map[string]any{"type": "string", "description": "Replacement text"},
			},
			"required": []string{"path", "old_text", "new_text"},
		}, newEditFile(tc))

	reg.Register("search_files", "Search for files matching a glob pattern",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{"type": "string", "description": "File name glob pattern (e.g. *.go)"},
				"path":    map[string]any{"type": "string", "description": "Directory to search (default: current)"},
			},
			"required": []string{"pattern"},
		}, newSearchFiles(tc))

	reg.Register("grep_files", "Search file contents for a text pattern",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{"type": "string", "description": "Text pattern to search for"},
				"path":    map[string]any{"type": "string", "description": "Directory to search (default: current)"},
			},
			"required": []string{"pattern"},
		}, newGrepFiles(tc))

	reg.Register("execute_command", "Execute a shell command (foreground only)",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command":         map[string]any{"type": "string", "description": "Shell command to execute"},
				"timeout_seconds": map[string]any{"type": "integer", "description": "Maximum execution time in seconds (default: 120)"},
			},
			"required": []string{"command"},
		}, newExecuteCommand(tc))

	reg.Register("run_background_process", "Start a command as a background process and return its PID",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{"type": "string", "description": "Shell command to run in background"},
			},
			"required": []string{"command"},
		}, newRunBackgroundProcess(tc))

	reg.Register("kill_process", "Kill a background process by PID",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pid": map[string]any{"type": "number", "description": "Process ID to kill"},
			},
			"required": []string{"pid"},
		}, newKillProcess(tc))

	reg.Register("ask_user", "Ask the user a question and wait for their response. Use this when you need user feedback, confirmation, or input before proceeding.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"prompt": map[string]any{"type": "string", "description": "The question or instruction to present to the user"},
				"choices": map[string]any{
					"type": "array",
					"items": map[string]any{"type": "string"},
					"description": "Optional list of choices for the user",
				},
			},
			"required": []string{"prompt"},
		}, newAskUser(tc))
}
