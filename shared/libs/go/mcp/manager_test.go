package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/toolconfig"
)

type mockClient struct {
	tools []ToolInfo
	calls int
	closed bool
}

func (m *mockClient) ListTools(context.Context) ([]ToolInfo, error) {
	return m.tools, nil
}
func (m *mockClient) CallTool(_ context.Context, name string, args map[string]any) (string, error) {
	m.calls++
	return name + "-ok", nil
}
func (m *mockClient) Close() error { m.closed = true; return nil }

func TestManager_PartialFailure(t *testing.T) {
	okClient := &mockClient{tools: []ToolInfo{{Name: "echo", Description: "d", InputSchema: map[string]any{"type": "object"}}}}
	mgr := NewManager("s1", nil, nil).WithDial(func(_ context.Context, name string, _ ResolvedServer) (ServerClient, error) {
		if name == "bad" {
			return nil, errors.New("boom")
		}
		return okClient, nil
	})
	err := mgr.ConnectAll(context.Background(), map[string]toolconfig.MCPServerConfig{
		"good": {Transport: "stdio", Command: "npx"},
		"bad":  {Transport: "stdio", Command: "npx"},
	})
	if err != nil {
		t.Fatal(err)
	}
	failed := mgr.Failed()
	if _, ok := failed["bad"]; !ok {
		t.Fatalf("expected bad in failed: %#v", failed)
	}
	tools, err := mgr.ListAllTools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tools["good"]) != 1 {
		t.Fatalf("expected good tools: %#v", tools)
	}
	out, err := mgr.Call(context.Background(), "good", "echo", nil)
	if err != nil || out != "echo-ok" {
		t.Fatalf("call = %q err=%v", out, err)
	}
	if err := mgr.Close(); err != nil {
		t.Fatal(err)
	}
	if !okClient.closed {
		t.Fatal("client not closed")
	}
	if _, err := mgr.Call(context.Background(), "good", "echo", nil); err == nil {
		t.Fatal("expected unavailable after close")
	}
}

func TestResolveServerSecrets(t *testing.T) {
	r := SecretResolverFunc(func(ref string) (string, error) {
		if ref == "vault://mcp/x/token" {
			return "secret", nil
		}
		return "", errors.New("missing")
	})
	got, err := ResolveServerSecrets(toolconfig.MCPServerConfig{
		Transport: "http",
		URL:       "https://x",
		Headers:   map[string]string{"Authorization": "vault://mcp/x/token"},
	}, r)
	if err != nil {
		t.Fatal(err)
	}
	if got.Headers["Authorization"] != "secret" {
		t.Fatalf("got %q", got.Headers["Authorization"])
	}
}

// SecretResolverFunc adapts a function to SecretResolver.
type SecretResolverFunc func(string) (string, error)

func (f SecretResolverFunc) Resolve(ref string) (string, error) { return f(ref) }
