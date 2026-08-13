package toolconfig

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var namePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// ValidateSessionTools validates mcp_servers and functions together.
func ValidateSessionTools(servers map[string]MCPServerConfig, fns map[string]FunctionConfig) error {
	if err := ValidateMCPServers(servers); err != nil {
		return err
	}
	return ValidateFunctions(fns)
}

// ValidateMCPServers validates MCP server entries.
func ValidateMCPServers(servers map[string]MCPServerConfig) error {
	for name, cfg := range servers {
		if name == "" {
			return fmt.Errorf("mcp server name must not be empty")
		}
		if !namePattern.MatchString(name) {
			return fmt.Errorf("mcp server name %q has invalid characters", name)
		}
		if err := validateMCPServer(name, cfg); err != nil {
			return err
		}
	}
	return nil
}

func validateMCPServer(name string, cfg MCPServerConfig) error {
	switch cfg.Transport {
	case "stdio":
		if strings.TrimSpace(cfg.Command) == "" {
			return fmt.Errorf("mcp server %q: command is required for stdio transport", name)
		}
		if cfg.URL != "" {
			return fmt.Errorf("mcp server %q: url must not be set for stdio transport", name)
		}
	case "http":
		if strings.TrimSpace(cfg.URL) == "" {
			return fmt.Errorf("mcp server %q: url is required for http transport", name)
		}
		if !strings.HasPrefix(cfg.URL, "http://") && !strings.HasPrefix(cfg.URL, "https://") {
			return fmt.Errorf("mcp server %q: url must start with http:// or https://", name)
		}
		if cfg.Command != "" {
			return fmt.Errorf("mcp server %q: command must not be set for http transport", name)
		}
	default:
		return fmt.Errorf("mcp server %q: unsupported transport %q", name, cfg.Transport)
	}
	if cfg.TimeoutMS < 0 {
		return fmt.Errorf("mcp server %q: timeout_ms must not be negative", name)
	}
	return nil
}

// ValidateFunctions validates client-defined function schemas.
func ValidateFunctions(fns map[string]FunctionConfig) error {
	for name, cfg := range fns {
		if name == "" {
			return fmt.Errorf("function name must not be empty")
		}
		if !namePattern.MatchString(name) {
			return fmt.Errorf("function name %q has invalid characters", name)
		}
		if strings.HasPrefix(name, MCPToolNamePrefix) {
			return fmt.Errorf("function name %q must not start with reserved prefix %q", name, MCPToolNamePrefix)
		}
		if strings.TrimSpace(cfg.Description) == "" {
			return fmt.Errorf("function %q: description is required", name)
		}
		if len(cfg.Parameters) == 0 {
			return fmt.Errorf("function %q: parameters is required", name)
		}
		var obj map[string]any
		if err := json.Unmarshal(cfg.Parameters, &obj); err != nil {
			return fmt.Errorf("function %q: parameters must be a JSON object: %w", name, err)
		}
		if obj == nil {
			return fmt.Errorf("function %q: parameters must be a JSON object", name)
		}
	}
	return nil
}
