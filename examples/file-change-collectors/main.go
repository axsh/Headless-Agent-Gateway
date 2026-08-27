// file-change-collectors demonstrates CreateSession file_change_collectors via client/v1.
//
// Algorithms:
//   - structured_tool (Tier 1, default ON): Write / Edit / file_change, etc.
//   - shell_parser (Tier 2, default ON): Bash / command_execution parsing
//   - workdir_reconcile (Tier 3, default OFF): git diff / snapshot supplement
//
// Usage:
//
//	go run . [server-url] [mode]
//
// Modes:
//
//	default | (omit)  — omit field (server defaults: Tier1-2 ON, Tier3 OFF)
//	full              — workdir_reconcile true (partial override)
//	off               — all collectors false
//
// Examples:
//
//	go run . http://localhost:3100
//	go run . http://localhost:3100 full
//	go run . http://localhost:3100 off
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
	mode := "default"
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
	case "", "default":
		log.Printf("creating session with file_change_collectors omitted (Tier1-2 ON / Tier3 OFF)")
	case "full":
		req.FileChangeCollectors = &client.FileChangeCollectors{
			WorkdirReconcile: client.BoolPtr(true),
		}
		log.Printf("creating session with workdir_reconcile=true")
	case "off":
		f := false
		req.FileChangeCollectors = &client.FileChangeCollectors{
			StructuredTool:   &f,
			ShellParser:      &f,
			WorkdirReconcile: &f,
		}
		log.Printf("creating session with all collectors false")
	default:
		log.Fatalf("unknown mode %q (allowed: default, full, off)", mode)
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
	if info.FileChangeCollectors == nil {
		log.Fatalf("expected resolved file_change_collectors")
	}
	fcc := info.FileChangeCollectors
	log.Printf("resolved file_change_collectors: structured_tool=%v shell_parser=%v workdir_reconcile=%v",
		fcc.StructuredTool, fcc.ShellParser, fcc.WorkdirReconcile)
}
