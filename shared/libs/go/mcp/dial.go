package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	mcplib "github.com/mark3labs/mcp-go/mcp"
)

type goClient struct {
	c *mcpclient.Client
}

// DialMCP opens a stdio or HTTP MCP client using mark3labs/mcp-go.
func DialMCP(ctx context.Context, name string, cfg ResolvedServer) (ServerClient, error) {
	_ = name
	switch cfg.Transport {
	case "stdio":
		return dialStdio(ctx, cfg)
	case "http":
		return dialHTTP(ctx, cfg)
	default:
		return nil, fmt.Errorf("unsupported transport %q", cfg.Transport)
	}
}

func dialStdio(ctx context.Context, cfg ResolvedServer) (ServerClient, error) {
	env := os.Environ()
	for k, v := range cfg.Env {
		env = append(env, k+"="+v)
	}
	// cwd is best-effort: prefer absolute command paths; mcp-go stdio has no dedicated cwd option.
	_ = cfg.Cwd
	c, err := mcpclient.NewStdioMCPClient(cfg.Command, env, cfg.Args...)
	if err != nil {
		return nil, err
	}
	gc := &goClient{c: c}
	if err := gc.initialize(ctx); err != nil {
		_ = c.Close()
		return nil, err
	}
	return gc, nil
}

func dialHTTP(ctx context.Context, cfg ResolvedServer) (ServerClient, error) {
	timeout := 30 * time.Second
	if cfg.TimeoutMS > 0 {
		timeout = time.Duration(cfg.TimeoutMS) * time.Millisecond
	}
	opts := []transport.StreamableHTTPCOption{
		transport.WithHTTPTimeout(timeout),
	}
	if len(cfg.Headers) > 0 {
		opts = append(opts, transport.WithHTTPHeaders(cfg.Headers))
	}
	c, err := mcpclient.NewStreamableHttpClient(cfg.URL, opts...)
	if err != nil {
		return nil, err
	}
	if err := c.Start(ctx); err != nil {
		_ = c.Close()
		return nil, err
	}
	gc := &goClient{c: c}
	if err := gc.initialize(ctx); err != nil {
		_ = c.Close()
		return nil, err
	}
	return gc, nil
}

func (g *goClient) initialize(ctx context.Context) error {
	req := mcplib.InitializeRequest{}
	req.Params.ProtocolVersion = mcplib.LATEST_PROTOCOL_VERSION
	req.Params.ClientInfo = mcplib.Implementation{Name: "tern", Version: "0.1.0"}
	_, err := g.c.Initialize(ctx, req)
	return err
}

func (g *goClient) ListTools(ctx context.Context) ([]ToolInfo, error) {
	res, err := g.c.ListTools(ctx, mcplib.ListToolsRequest{})
	if err != nil {
		return nil, err
	}
	out := make([]ToolInfo, 0, len(res.Tools))
	for _, t := range res.Tools {
		schema := map[string]any{}
		if t.InputSchema.Type != "" || len(t.InputSchema.Properties) > 0 {
			b, _ := json.Marshal(t.InputSchema)
			_ = json.Unmarshal(b, &schema)
		}
		out = append(out, ToolInfo{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: schema,
		})
	}
	return out, nil
}

func (g *goClient) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	req := mcplib.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args
	res, err := g.c.CallTool(ctx, req)
	if err != nil {
		return "", err
	}
	if res.IsError {
		return formatContent(res), fmt.Errorf("mcp tool error: %s", formatContent(res))
	}
	return formatContent(res), nil
}

func (g *goClient) Close() error {
	if g.c == nil {
		return nil
	}
	return g.c.Close()
}

func formatContent(res *mcplib.CallToolResult) string {
	if res == nil {
		return ""
	}
	var parts []string
	for _, c := range res.Content {
		switch v := c.(type) {
		case mcplib.TextContent:
			parts = append(parts, v.Text)
		default:
			b, _ := json.Marshal(v)
			parts = append(parts, string(b))
		}
	}
	return strings.Join(parts, "\n")
}
