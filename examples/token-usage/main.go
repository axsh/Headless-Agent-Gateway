// token-usage demonstrates Session / Turn / LLM-call token usage via the Client API.
//
// Prerequisites:
//   - A running tern server (e.g. minimal-server) with Claude Code configured
//
// Usage:
//
//	go run . [server-url] [agent] [model]
//
// Examples:
//
//	go run .
//	go run . http://localhost:3100 claudecode
//	go run . http://localhost:3100 claudecode sonnet
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	client "github.com/axsh/arctic-tern/client/v1"
)

func main() {
	serverURL := "http://localhost:3100"
	agent := "claudecode"
	model := ""
	if len(os.Args) > 1 {
		serverURL = os.Args[1]
	}
	if len(os.Args) > 2 {
		agent = os.Args[2]
	}
	if len(os.Args) > 3 {
		model = os.Args[3]
	}

	ctx := context.Background()
	c := client.New(serverURL, client.WithNoTimeout())

	workDir, err := os.MkdirTemp("", "token-usage-")
	if err != nil {
		log.Fatalf("temp work dir: %v", err)
	}
	defer os.RemoveAll(workDir)

	session, err := c.CreateSession(ctx, client.SessionRequest{
		Agent:   agent,
		Model:   model,
		WorkDir: workDir,
	})
	if err != nil {
		log.Fatalf("create session: %v", err)
	}
	defer session.Terminate(ctx)
	log.Printf("session_id=%s work_dir=%s", session.ID, workDir)

	if err := sendAndStream(ctx, session, "Create a file named ping.txt with content pong using a tool, then reply with exactly: done"); err != nil {
		log.Fatalf("send #1: %v", err)
	}
	if err := sendAndStream(ctx, session, "Reply with exactly: ok"); err != nil {
		log.Fatalf("send #2: %v", err)
	}

	repAll, err := session.GetUsage(ctx)
	if err != nil {
		log.Fatalf("GetUsage all: %v", err)
	}
	info, err := c.GetSession(ctx, session.ID)
	if err != nil {
		log.Fatalf("GetSession: %v", err)
	}

	fmt.Println("=== Session usage ===")
	fmt.Printf("(GetUsage without query)\n")
	printUsage("  ", &repAll.Usage)
	if info.Usage != nil {
		fmt.Printf("  (GetSession.Usage check) input=%d output=%d\n",
			info.Usage.InputTokens, info.Usage.OutputTokens)
	} else {
		fmt.Println("  (GetSession.Usage: missing)")
	}

	fmt.Println("=== Turn usage ===")
	fmt.Printf("(persisted turns from GetUsage)\n")
	if len(repAll.Turns) == 0 {
		fmt.Println("  (none)")
	}
	for i, tr := range repAll.Turns {
		fmt.Printf("  turn[%d] id=%s\n", i, tr.TurnID)
		printUsage("    ", &tr.Usage)
	}

	fmt.Println("=== LLM call usage ===")
	if len(repAll.Turns) == 0 {
		fmt.Println("  (none)")
	}
	for i, tr := range repAll.Turns {
		fmt.Printf("  turn[%d] id=%s\n", i, tr.TurnID)
		if len(tr.Calls) == 0 {
			fmt.Println("    calls: (none for this turn)")
			continue
		}
		for j, call := range tr.Calls {
			fmt.Printf("    call[%d] id=%s\n", j, call.CallID)
			printUsage("      ", &call)
		}
	}

	repLast, err := session.GetUsage(ctx, client.UsageQuery{LastN: 1})
	if err != nil {
		log.Fatalf("GetUsage LastN=1: %v", err)
	}
	fmt.Println("=== LastN=1 (GetUsage with UsageQuery) ===")
	fmt.Println("=== Session usage ===")
	fmt.Printf("(filtered: sum of returned turns)\n")
	printUsage("  ", &repLast.Usage)
	fmt.Println("=== Turn usage ===")
	if len(repLast.Turns) == 0 {
		fmt.Println("  (none)")
	}
	for i, tr := range repLast.Turns {
		fmt.Printf("  turn[%d] id=%s\n", i, tr.TurnID)
		printUsage("    ", &tr.Usage)
	}
	fmt.Println("=== LLM call usage ===")
	for i, tr := range repLast.Turns {
		fmt.Printf("  turn[%d] id=%s\n", i, tr.TurnID)
		if len(tr.Calls) == 0 {
			fmt.Println("    calls: (none for this turn)")
			continue
		}
		for j, call := range tr.Calls {
			fmt.Printf("    call[%d] id=%s\n", j, call.CallID)
			printUsage("      ", &call)
		}
	}

	log.Printf("done turns=%d last_n_turns=%d", len(repAll.Turns), len(repLast.Turns))
}

func sendAndStream(ctx context.Context, session *client.Session, text string) error {
	stream, err := session.SendText(ctx, text)
	if err != nil {
		return err
	}
	return stream.Output(os.Stdout)
}

func printUsage(prefix string, u *client.TokenUsage) {
	if u == nil {
		fmt.Printf("%s(missing)\n", prefix)
		return
	}
	fmt.Printf("%sinput=%d output=%d cached_in=%d cache_create=%d reasoning_out=%d total=%d source=%s confidence=%s",
		prefix,
		u.InputTokens, u.OutputTokens, u.CachedInputTokens, u.CacheCreationInputTokens,
		u.ReasoningOutputTokens, u.TotalTokens, u.Source, u.Confidence,
	)
	if u.Model != "" {
		fmt.Printf(" model=%s model_source=%s", u.Model, u.ModelSource)
	}
	if u.TotalCostUSD != nil {
		fmt.Printf(" total_cost_usd=%.6f (estimate)", *u.TotalCostUSD)
	}
	fmt.Println()
}
