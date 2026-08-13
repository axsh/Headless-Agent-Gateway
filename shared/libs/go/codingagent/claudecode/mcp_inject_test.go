package claudecode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/toolconfig"
)

func TestInjectMCPServers(t *testing.T) {
	dir := t.TempDir()
	// Pre-existing non-Tern server should remain.
	pre := []byte(`{"mcpServers":{"user-tool":{"type":"stdio","command":"echo"}}}`)
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), pre, 0644); err != nil {
		t.Fatal(err)
	}
	enabled := true
	servers := map[string]toolconfig.MCPServerConfig{
		"filesystem": {
			Transport: "stdio",
			Command:   "npx",
			Args:      []string{"-y", "pkg"},
			Enabled:   &enabled,
		},
		"remote": {
			Transport: "http",
			URL:       "https://mcp.example.com/mcp",
			Headers:   map[string]string{"Authorization": "Bearer x"},
			Enabled:   &enabled,
		},
	}
	if err := InjectMCPServers(dir, ManagedMCPKeys(servers), servers, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	mcpServers := root["mcpServers"].(map[string]any)
	if _, ok := mcpServers["user-tool"]; !ok {
		t.Fatal("user-tool missing")
	}
	fs := mcpServers["filesystem"].(map[string]any)
	if fs["type"] != "stdio" || fs["command"] != "npx" {
		t.Fatalf("filesystem = %#v", fs)
	}
	remote := mcpServers["remote"].(map[string]any)
	if remote["type"] != "http" {
		t.Fatalf("remote = %#v", remote)
	}
	// Replace removes old Tern key when not in new set.
	next := map[string]toolconfig.MCPServerConfig{
		"only": {Transport: "http", URL: "https://only.example/mcp"},
	}
	if err := InjectMCPServers(dir, []string{"filesystem", "remote", "only"}, next, nil); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(filepath.Join(dir, ".mcp.json"))
	_ = json.Unmarshal(data, &root)
	mcpServers = root["mcpServers"].(map[string]any)
	if _, ok := mcpServers["filesystem"]; ok {
		t.Fatal("filesystem should be removed")
	}
	if _, ok := mcpServers["only"]; !ok {
		t.Fatal("only missing")
	}
	if _, ok := mcpServers["user-tool"]; !ok {
		t.Fatal("user-tool should remain")
	}
}
