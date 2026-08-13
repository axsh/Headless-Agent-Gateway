package toolconfig

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateMCPServers(t *testing.T) {
	enabledFalse := false
	tests := []struct {
		name    string
		servers map[string]MCPServerConfig
		wantErr string
	}{
		{
			name: "stdio ok",
			servers: map[string]MCPServerConfig{
				"filesystem": {Transport: "stdio", Command: "npx", Args: []string{"-y", "pkg"}},
			},
		},
		{
			name: "http ok",
			servers: map[string]MCPServerConfig{
				"remote": {Transport: "http", URL: "http://localhost:8080/mcp"},
			},
		},
		{
			name: "https ok",
			servers: map[string]MCPServerConfig{
				"remote": {Transport: "http", URL: "https://mcp.example.com/mcp"},
			},
		},
		{
			name: "enabled false still validated",
			servers: map[string]MCPServerConfig{
				"bad": {Transport: "stdio", Enabled: &enabledFalse},
			},
			wantErr: "command is required",
		},
		{
			name: "stdio missing command",
			servers: map[string]MCPServerConfig{
				"fs": {Transport: "stdio"},
			},
			wantErr: "command is required",
		},
		{
			name: "http bad url",
			servers: map[string]MCPServerConfig{
				"r": {Transport: "http", URL: "ftp://x"},
			},
			wantErr: "http:// or https://",
		},
		{
			name: "stdio with url",
			servers: map[string]MCPServerConfig{
				"fs": {Transport: "stdio", Command: "npx", URL: "http://x"},
			},
			wantErr: "url must not be set",
		},
		{
			name: "http with command",
			servers: map[string]MCPServerConfig{
				"r": {Transport: "http", URL: "https://x", Command: "npx"},
			},
			wantErr: "command must not be set",
		},
		{
			name: "empty server name",
			servers: map[string]MCPServerConfig{
				"": {Transport: "stdio", Command: "npx"},
			},
			wantErr: "must not be empty",
		},
		{
			name: "invalid transport",
			servers: map[string]MCPServerConfig{
				"x": {Transport: "sse"},
			},
			wantErr: "unsupported transport",
		},
		{
			name: "negative timeout",
			servers: map[string]MCPServerConfig{
				"r": {Transport: "http", URL: "https://x", TimeoutMS: -1},
			},
			wantErr: "timeout_ms",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMCPServers(tt.servers)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateFunctions(t *testing.T) {
	validParams := json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}}}`)
	tests := []struct {
		name    string
		fns     map[string]FunctionConfig
		wantErr string
	}{
		{
			name: "ok",
			fns: map[string]FunctionConfig{
				"lookup_ticket": {Description: "Look up", Parameters: validParams},
			},
		},
		{
			name: "missing description",
			fns: map[string]FunctionConfig{
				"f": {Parameters: validParams},
			},
			wantErr: "description is required",
		},
		{
			name: "bad parameters json",
			fns: map[string]FunctionConfig{
				"f": {Description: "d", Parameters: json.RawMessage(`[]`)},
			},
			wantErr: "JSON object",
		},
		{
			name: "reserved prefix",
			fns: map[string]FunctionConfig{
				"mcp__foo": {Description: "d", Parameters: validParams},
			},
			wantErr: "reserved prefix",
		},
		{
			name: "empty name",
			fns: map[string]FunctionConfig{
				"": {Description: "d", Parameters: validParams},
			},
			wantErr: "must not be empty",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateFunctions(tt.fns)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}
