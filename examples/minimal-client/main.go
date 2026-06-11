// minimal-client demonstrates the simplest way to interact with a running
// tern server using the client library. It creates a session, sends a message,
// and streams the response to stdout.
//
// Prerequisites:
//   - A running tern server (e.g., via minimal-server or cawa-server)
//   - Claude CLI on PATH (for the claudecode agent)
//
// Usage:
//
//	go run . [server-url]
package main

import (
	"context"
	"log"
	"os"

	"github.com/axsh/arctic-tern/client"
)

func main() {
	serverURL := "http://localhost:3100"
	if len(os.Args) > 1 {
		serverURL = os.Args[1]
	}

	ctx := context.Background()
	c := client.New(serverURL)

	session, err := c.CreateSession(ctx, client.SessionRequest{
		Agent:   "claudecode",
		Model:   "sonnet",
		WorkDir: ".",
	})
	if err != nil {
		log.Fatalf("create session: %v", err)
	}
	defer session.Terminate(ctx)
	log.Printf("Session: %s", session.ID)

	stream, err := session.SendMessage(ctx, "Create a file called hello.txt with the content 'Hello, World!'")
	if err != nil {
		log.Fatalf("send message: %v", err)
	}

	if err := stream.Output(os.Stdout); err != nil {
		log.Fatalf("stream output: %v", err)
	}
}
