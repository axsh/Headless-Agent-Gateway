package wayfinder

import (
	"context"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/mcp"
	"github.com/axsh/arctic-tern/shared/libs/go/toolconfig"
	"github.com/axsh/arctic-tern/shared/libs/go/wayfinder/tools"
)

type stubMCPClient struct{}

func (stubMCPClient) ListTools(context.Context) ([]mcp.ToolInfo, error) { return nil, nil }
func (stubMCPClient) CallTool(_ context.Context, name string, _ map[string]any) (string, error) {
	return "called:" + name, nil
}
func (stubMCPClient) Close() error { return nil }

func TestRegisterMCPTools(t *testing.T) {
	mgr := mcp.NewManager("s", nil, nil).WithDial(func(_ context.Context, _ string, _ mcp.ResolvedServer) (mcp.ServerClient, error) {
		return stubMCPClient{}, nil
	})
	_ = mgr.ConnectAll(context.Background(), map[string]toolconfig.MCPServerConfig{
		"fs": {Transport: "stdio", Command: "npx"},
	})
	reg := tools.NewRegistry()
	RegisterMCPTools(reg, mgr, map[string][]mcp.ToolInfo{
		"fs": {{Name: "read", Description: "Read", InputSchema: map[string]any{"type": "object"}}},
	})
	tool, ok := reg.Get("mcp__fs__read")
	if !ok {
		t.Fatal("tool not registered")
	}
	out, err := tool.Handler(context.Background(), nil)
	if err != nil || out != "called:read" {
		t.Fatalf("out=%q err=%v", out, err)
	}
}
