package codex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/toolconfig"
)

func TestInjectMCPServers(t *testing.T) {
	dir := t.TempDir()
	base := "model = \"gpt-4o\"\n\n[model_providers.gateway]\nname = \"tern\"\n"
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(base), 0644); err != nil {
		t.Fatal(err)
	}
	servers := map[string]toolconfig.MCPServerConfig{
		"filesystem": {Transport: "stdio", Command: "npx", Args: []string{"-y", "pkg"}},
		"remote":     {Transport: "http", URL: "https://mcp.example.com/mcp"},
	}
	if err := InjectMCPServers(dir, ManagedMCPKeys(servers), servers, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, "model_providers.gateway") {
		t.Fatal("gateway section lost")
	}
	if !strings.Contains(s, "[mcp_servers.filesystem]") {
		t.Fatal("filesystem missing")
	}
	if !strings.Contains(s, `url = "https://mcp.example.com/mcp"`) {
		t.Fatal("remote url missing")
	}
	// Re-inject with only one server; old Tern block replaced.
	next := map[string]toolconfig.MCPServerConfig{
		"only": {Transport: "http", URL: "https://only.example/mcp"},
	}
	if err := InjectMCPServers(dir, ManagedMCPKeys(next), next, nil); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(filepath.Join(dir, "config.toml"))
	s = string(data)
	if strings.Contains(s, "[mcp_servers.filesystem]") {
		t.Fatal("old filesystem should be gone")
	}
	if !strings.Contains(s, "[mcp_servers.only]") {
		t.Fatal("only missing")
	}
}
