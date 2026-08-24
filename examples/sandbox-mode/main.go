// sandbox-mode demonstrates CreateSession sandbox_mode via client/v1.
//
// Related: https://github.com/axsh/arctic-tern/issues/54
//
// Codex CLI modes (codex exec -s):
//   - read-only (default when omitted): no workspace writes
//   - workspace-write: R/W under the workspace, still sandboxed
//   - danger-full-access: --dangerously-bypass-approvals-and-sandbox
//
// Server-wide agent_service.disable_sandbox: true still maps omitted mode to
// danger-full-access. Prefer per-session sandbox_mode when possible.
//
// Usage:
//
//	go run . [server-url] [mode]
//
// Examples:
//
//	go run . http://localhost:3100
//	go run . http://localhost:3100 workspace-write
//	go run . http://localhost:3100 danger-full-access
package main

import (
	"context"
	"log"
	"os"

	client "github.com/axsh/arctic-tern/client/v1"
)

func main() {
	serverURL := "http://localhost:3100"
	if len(os.Args) > 1 {
		serverURL = os.Args[1]
	}
	mode := ""
	if len(os.Args) > 2 {
		mode = os.Args[2]
	}

	ctx := context.Background()
	c := client.New(serverURL)

	req := client.SessionRequest{
		Agent:   "codex",
		WorkDir: ".",
	}
	switch mode {
	case "", client.SandboxModeReadOnly:
		// Omit SandboxMode for default read-only, or set explicitly:
		if mode == client.SandboxModeReadOnly {
			req.SandboxMode = client.SandboxModeReadOnly
		}
		log.Printf("creating session with sandbox_mode omitted/read-only (Codex -s read-only)")
	case client.SandboxModeWorkspaceWrite:
		req.SandboxMode = client.SandboxModeWorkspaceWrite
		log.Printf("creating session with sandbox_mode=%s", req.SandboxMode)
	case client.SandboxModeDangerFullAccess:
		req.SandboxMode = client.SandboxModeDangerFullAccess
		log.Printf("creating session with sandbox_mode=%s", req.SandboxMode)
	default:
		log.Fatalf("unknown mode %q (allowed: %s, %s, %s)",
			mode, client.SandboxModeReadOnly, client.SandboxModeWorkspaceWrite, client.SandboxModeDangerFullAccess)
	}

	session, err := c.CreateSession(ctx, req)
	if err != nil {
		log.Fatalf("create session: %v", err)
	}
	defer session.Terminate(ctx)
	log.Printf("Session: %s", session.ID)

	info, err := c.GetSession(ctx, session.ID)
	if err != nil {
		log.Fatalf("get session: %v", err)
	}
	log.Printf("resolved sandbox_mode=%s", info.SandboxMode)
}
