package llm_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent/claudecode"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent/codex"
	"github.com/axsh/arctic-tern/shared/libs/go/toolconfig"
)

func TestMCPInject_Claude(t *testing.T) {
	workDir := t.TempDir()
	servers := map[string]toolconfig.MCPServerConfig{
		"playwright": {
			Transport: "stdio",
			Command:   "npx",
			Args:      []string{"-y", "@playwright/mcp@latest"},
		},
		"docs": {
			Transport: "http",
			URL:       "https://code.claude.com/docs/mcp",
		},
	}
	if err := claudecode.InjectMCPServers(workDir, claudecode.ManagedMCPKeys(servers), servers, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(workDir, ".mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	mcpServers := root["mcpServers"].(map[string]any)
	if _, ok := mcpServers["playwright"]; !ok {
		t.Fatal("playwright missing")
	}
	if _, ok := mcpServers["docs"]; !ok {
		t.Fatal("docs missing")
	}
}

func TestMCPInject_Codex(t *testing.T) {
	sessionDir := t.TempDir()
	servers := map[string]toolconfig.MCPServerConfig{
		"filesystem": {Transport: "stdio", Command: "npx", Args: []string{"-y", "pkg"}},
	}
	if err := codex.InjectMCPServers(sessionDir, codex.ManagedMCPKeys(servers), servers, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(sessionDir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, "[mcp_servers.filesystem]") {
		t.Fatalf("missing mcp section: %s", s)
	}
}
