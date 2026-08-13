// mcp-session-tools demonstrates CreateSession / GetSession / PatchSession
// with mcp_servers and functions via client/v1.
//
// Prerequisites:
//   - A running tern server
//
// Usage:
//
//	go run . --server http://localhost:3100 --work-dir /path/to/workspace
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	client "github.com/axsh/arctic-tern/client/v1"
	"github.com/axsh/arctic-tern/shared/libs/go/toolconfig"
)

func main() {
	server := flag.String("server", "http://localhost:3100", "tern agent service URL")
	workDir := flag.String("work-dir", "", "workspace directory (default: temp dir)")
	agent := flag.String("agent", "wayfinder", "agent name")
	flag.Parse()

	wd := *workDir
	if wd == "" {
		var err error
		wd, err = os.MkdirTemp("", "tern-mcp-example-*")
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println("using work_dir:", wd)
	}
	wd, _ = filepath.Abs(wd)
	sessionDir := filepath.Join(wd, ".tern-session")

	ctx := context.Background()
	c := client.New(*server)
	enabled := true
	sess, err := c.CreateSession(ctx, client.SessionRequest{
		Agent:      *agent,
		WorkDir:    wd,
		SessionDir: sessionDir,
		MCPServers: map[string]toolconfig.MCPServerConfig{
			"filesystem": {
				Transport: "stdio",
				Command:   "npx",
				Args:      []string{"-y", "@modelcontextprotocol/server-filesystem", wd},
				Enabled:   &enabled,
			},
			"remote-docs": {
				Transport: "http",
				URL:       "https://mcp.example.com/mcp",
				Headers:   map[string]string{"Authorization": "vault://mcp/remote-docs/token"},
				TimeoutMS: 30000,
				Enabled:   &enabled,
			},
		},
		Functions: map[string]toolconfig.FunctionConfig{
			"lookup_ticket": {
				Description: "Look up a ticket by ID",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"ticket_id":{"type":"string"}},"required":["ticket_id"]}`),
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("created session:", sess.ID)

	info, err := c.GetSession(ctx, sess.ID)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("get: mcp_servers=%d functions=%d\n", len(info.MCPServers), len(info.Functions))
	if h := info.MCPServers["remote-docs"].Headers["Authorization"]; h != "***" && h != "" {
		log.Fatalf("expected masked Authorization, got %q", h)
	}

	fsOnly := map[string]toolconfig.MCPServerConfig{
		"filesystem": {
			Transport: "stdio",
			Command:   "npx",
			Args:      []string{"-y", "@modelcontextprotocol/server-filesystem", wd},
		},
	}
	info, err = c.PatchSession(ctx, sess.ID, client.SessionPatch{MCPServers: &fsOnly})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("patched mcp_servers=%d (functions unchanged=%d)\n", len(info.MCPServers), len(info.Functions))
}
