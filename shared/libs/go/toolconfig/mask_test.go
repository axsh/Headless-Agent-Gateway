package toolconfig

import "testing"

func TestMaskMCPServers(t *testing.T) {
	in := map[string]MCPServerConfig{
		"remote": {
			Transport: "http",
			URL:       "https://mcp.example.com/mcp",
			Headers:   map[string]string{"Authorization": "Bearer secret"},
			Env:       map[string]string{"TOKEN": "secret-env"},
		},
	}
	out := MaskMCPServers(in)
	if in["remote"].Headers["Authorization"] != "Bearer secret" {
		t.Fatalf("input mutated: %v", in["remote"].Headers)
	}
	if in["remote"].Env["TOKEN"] != "secret-env" {
		t.Fatalf("input env mutated: %v", in["remote"].Env)
	}
	if out["remote"].Headers["Authorization"] != "***" {
		t.Fatalf("header not masked: %v", out["remote"].Headers)
	}
	if out["remote"].Env["TOKEN"] != "***" {
		t.Fatalf("env not masked: %v", out["remote"].Env)
	}
	if out["remote"].URL != "https://mcp.example.com/mcp" {
		t.Fatalf("url should be preserved")
	}
	if MaskMCPServers(nil) != nil {
		t.Fatalf("nil should stay nil")
	}
	empty := MaskMCPServers(map[string]MCPServerConfig{})
	if empty == nil || len(empty) != 0 {
		t.Fatalf("empty map should stay empty, got %#v", empty)
	}
}
