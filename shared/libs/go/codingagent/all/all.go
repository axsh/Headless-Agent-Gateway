// Package all registers all built-in coding agent adapters via blank imports.
//
// Import this package to automatically register every available coding agent
// without listing them individually:
//
//	import _ "github.com/axsh/arctic-tern/codingagent/all"
package all

import (
	// Auto-register all coding agents.
	_ "github.com/axsh/arctic-tern/codingagent/claudecode"
	_ "github.com/axsh/arctic-tern/codingagent/codex"
)
