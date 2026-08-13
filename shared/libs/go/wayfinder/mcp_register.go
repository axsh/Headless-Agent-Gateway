package wayfinder

import (
	"context"

	"github.com/axsh/arctic-tern/shared/libs/go/mcp"
	"github.com/axsh/arctic-tern/shared/libs/go/toolconfig"
	"github.com/axsh/arctic-tern/shared/libs/go/wayfinder/tools"
)

// RegisterMCPTools registers MCP tools into the Wayfinder registry using
// names of the form mcp__{server}__{tool}.
func RegisterMCPTools(reg *tools.Registry, mgr *mcp.Manager, toolsByServer map[string][]mcp.ToolInfo) {
	if reg == nil || mgr == nil {
		return
	}
	for server, list := range toolsByServer {
		serverName := server
		for _, t := range list {
			toolName := t.Name
			schema := t.InputSchema
			if schema == nil {
				schema = map[string]any{"type": "object"}
			}
			name := toolconfig.MCPToolNamePrefix + serverName + "__" + toolName
			desc := t.Description
			reg.Register(name, desc, schema, func(ctx context.Context, input map[string]any) (string, error) {
				return mgr.Call(ctx, serverName, toolName, input)
			})
		}
	}
}
