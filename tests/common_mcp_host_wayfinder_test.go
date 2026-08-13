package llm_test

import (
	"context"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/mcp"
	"github.com/axsh/arctic-tern/shared/libs/go/toolconfig"
	"github.com/axsh/arctic-tern/shared/libs/go/wayfinder"
	"github.com/axsh/arctic-tern/shared/libs/go/wayfinder/tools"
)

type hostStubClient struct {
	tools []mcp.ToolInfo
}

func (h *hostStubClient) ListTools(context.Context) ([]mcp.ToolInfo, error) { return h.tools, nil }
func (h *hostStubClient) CallTool(_ context.Context, name string, _ map[string]any) (string, error) {
	return "ok:" + name, nil
}
func (h *hostStubClient) Close() error { return nil }

func TestMCPHost_RegisterAndCall(t *testing.T) {
	stub := &hostStubClient{tools: []mcp.ToolInfo{{
		Name: "ping", Description: "Ping", InputSchema: map[string]any{"type": "object"},
	}}}
	mgr := mcp.NewManager("integ", nil, nil).WithDial(func(_ context.Context, _ string, _ mcp.ResolvedServer) (mcp.ServerClient, error) {
		return stub, nil
	})
	if err := mgr.ConnectAll(context.Background(), map[string]toolconfig.MCPServerConfig{
		"demo": {Transport: "stdio", Command: "npx"},
	}); err != nil {
		t.Fatal(err)
	}
	listed, err := mgr.ListAllTools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	reg := tools.NewRegistry()
	wayfinder.RegisterMCPTools(reg, mgr, listed)
	tool, ok := reg.Get("mcp__demo__ping")
	if !ok {
		t.Fatal("missing registered tool")
	}
	out, err := tool.Handler(context.Background(), map[string]any{})
	if err != nil || out != "ok:ping" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if err := mgr.Close(); err != nil {
		t.Fatal(err)
	}
}
