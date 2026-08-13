package codex

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/axsh/arctic-tern/shared/libs/go/mcp"
	"github.com/axsh/arctic-tern/shared/libs/go/toolconfig"
)

const (
	ternMCPBegin = "# BEGIN tern-managed mcp_servers"
	ternMCPEnd   = "# END tern-managed mcp_servers"
)

// InjectMCPServers merges Tern-managed MCP servers into sessionDir/config.toml.
// Existing non-Tern content is preserved; the Tern-managed block is replaced.
func InjectMCPServers(sessionDir string, replaceKeys []string, servers map[string]toolconfig.MCPServerConfig, resolver mcp.SecretResolver) error {
	_ = replaceKeys // keys are fully represented by the managed block contents
	if sessionDir == "" {
		return fmt.Errorf("sessionDir is required")
	}
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		return err
	}
	path := filepath.Join(sessionDir, "config.toml")

	existing := ""
	if data, err := os.ReadFile(path); err == nil {
		existing = string(data)
	}
	existing = stripManagedBlock(existing)

	block, err := buildManagedBlock(servers, resolver)
	if err != nil {
		return err
	}

	var b strings.Builder
	b.WriteString(strings.TrimRight(existing, "\n"))
	if b.Len() > 0 {
		b.WriteString("\n\n")
	}
	b.WriteString(block)
	b.WriteString("\n")

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func stripManagedBlock(s string) string {
	start := strings.Index(s, ternMCPBegin)
	if start < 0 {
		return s
	}
	end := strings.Index(s[start:], ternMCPEnd)
	if end < 0 {
		return s[:start]
	}
	end = start + end + len(ternMCPEnd)
	// Also drop trailing newlines after the block for cleanliness.
	rest := strings.TrimLeft(s[end:], "\r\n")
	head := strings.TrimRight(s[:start], "\r\n")
	if head == "" {
		return rest
	}
	if rest == "" {
		return head
	}
	return head + "\n\n" + rest
}

func buildManagedBlock(servers map[string]toolconfig.MCPServerConfig, resolver mcp.SecretResolver) (string, error) {
	var b strings.Builder
	b.WriteString(ternMCPBegin)
	b.WriteByte('\n')
	for name, cfg := range servers {
		if cfg.Enabled != nil && !*cfg.Enabled {
			continue
		}
		resolved, err := mcp.ResolveServerSecrets(cfg, resolver)
		if err != nil {
			return "", fmt.Errorf("resolve %s: %w", name, err)
		}
		switch resolved.Transport {
		case "stdio":
			b.WriteString(fmt.Sprintf("[mcp_servers.%s]\n", name))
			b.WriteString(fmt.Sprintf("command = %q\n", resolved.Command))
			if len(resolved.Args) > 0 {
				b.WriteString("args = [")
				for i, a := range resolved.Args {
					if i > 0 {
						b.WriteString(", ")
					}
					b.WriteString(fmt.Sprintf("%q", a))
				}
				b.WriteString("]\n")
			}
			for k, v := range resolved.Env {
				b.WriteString(fmt.Sprintf("env.%s = %q\n", k, v))
			}
		case "http":
			b.WriteString(fmt.Sprintf("[mcp_servers.%s]\n", name))
			b.WriteString(fmt.Sprintf("url = %q\n", resolved.URL))
			for k, v := range resolved.Headers {
				b.WriteString(fmt.Sprintf("http_headers.%s = %q\n", k, v))
			}
		default:
			return "", fmt.Errorf("unsupported transport %q", resolved.Transport)
		}
		b.WriteByte('\n')
	}
	b.WriteString(ternMCPEnd)
	return b.String(), nil
}

// ManagedMCPKeys returns map keys for replace semantics.
func ManagedMCPKeys(servers map[string]toolconfig.MCPServerConfig) []string {
	keys := make([]string, 0, len(servers))
	for k := range servers {
		keys = append(keys, k)
	}
	return keys
}
