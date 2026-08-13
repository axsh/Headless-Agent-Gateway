package claudecode

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/axsh/arctic-tern/shared/libs/go/mcp"
	"github.com/axsh/arctic-tern/shared/libs/go/toolconfig"
)

// InjectMCPServers merges Tern-managed MCP servers into workDir/.mcp.json.
// replaceKeys are removed first (previous Tern-managed names), then servers are written.
func InjectMCPServers(workDir string, replaceKeys []string, servers map[string]toolconfig.MCPServerConfig, resolver mcp.SecretResolver) error {
	if workDir == "" {
		return fmt.Errorf("workDir is required")
	}
	path := filepath.Join(workDir, ".mcp.json")

	root := map[string]any{"mcpServers": map[string]any{}}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &root)
	}
	mcpServers, _ := root["mcpServers"].(map[string]any)
	if mcpServers == nil {
		mcpServers = map[string]any{}
		root["mcpServers"] = mcpServers
	}
	for _, k := range replaceKeys {
		delete(mcpServers, k)
	}

	for name, cfg := range servers {
		if cfg.Enabled != nil && !*cfg.Enabled {
			continue
		}
		resolved, err := mcp.ResolveServerSecrets(cfg, resolver)
		if err != nil {
			return fmt.Errorf("resolve %s: %w", name, err)
		}
		entry, err := toClaudeMCPEntry(resolved)
		if err != nil {
			return fmt.Errorf("map %s: %w", name, err)
		}
		mcpServers[name] = entry
	}

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func toClaudeMCPEntry(cfg mcp.ResolvedServer) (map[string]any, error) {
	switch cfg.Transport {
	case "stdio":
		entry := map[string]any{
			"type":    "stdio",
			"command": cfg.Command,
		}
		if len(cfg.Args) > 0 {
			entry["args"] = cfg.Args
		}
		if len(cfg.Env) > 0 {
			entry["env"] = cfg.Env
		}
		return entry, nil
	case "http":
		entry := map[string]any{
			"type": "http",
			"url":  cfg.URL,
		}
		if len(cfg.Headers) > 0 {
			entry["headers"] = cfg.Headers
		}
		return entry, nil
	default:
		return nil, fmt.Errorf("unsupported transport %q", cfg.Transport)
	}
}

// ManagedMCPKeys returns the keys Tern currently manages for replace semantics.
func ManagedMCPKeys(servers map[string]toolconfig.MCPServerConfig) []string {
	keys := make([]string, 0, len(servers))
	for k := range servers {
		keys = append(keys, k)
	}
	return keys
}
