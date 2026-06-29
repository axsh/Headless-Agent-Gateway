// multimodal-client demonstrates how to interact with a running tern server
// using the client library to send a multimodal message (text + image).
//
// Prerequisites:
//   - A running tern server (e.g., via minimal-server)
//   - An agent that supports multimodal (e.g., claudecode with sonnet model)
//
// Usage:
//
//	go run . [options]
//
// Examples:
//
//	# Send a query with a local image (using claudecode agent)
//	go run . -image screenshot.png -prompt "Describe this image"
//
//	# Connect to a custom server URL
//	go run . -server http://192.168.1.100:3100 -image screenshot.png
package main

import (
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	client "github.com/axsh/arctic-tern/client/v1"
)

// 1x1 red transparent PNG image (dummy fallback)
const dummyPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

func main() {
	serverURL := flag.String("server", "http://localhost:3100", "Tern server URL")
	imagePath := flag.String("image", "", "Path to the image file (optional, uses dummy image if empty)")
	prompt := flag.String("prompt", "Describe what you see in this image.", "Text prompt to send alongside the image")
	agent := flag.String("agent", "claudecode", "Coding agent to use")
	flag.Parse()

	ctx := context.Background()

	// Create a new client pointing to the tern server.
	c := client.New(*serverURL)

	var mediaType string
	var base64Data string

	if *imagePath != "" {
		data, err := os.ReadFile(*imagePath)
		if err != nil {
			log.Fatalf("failed to read image file: %v", err)
		}
		base64Data = base64.StdEncoding.EncodeToString(data)
		ext := strings.ToLower(filepath.Ext(*imagePath))
		switch ext {
		case ".jpg", ".jpeg":
			mediaType = "image/jpeg"
		case ".gif":
			mediaType = "image/gif"
		case ".webp":
			mediaType = "image/webp"
		default:
			mediaType = "image/png"
		}
	} else {
		mediaType = "image/png"
		base64Data = dummyPNG
		fmt.Println("[INFO] No image file specified. Using a 1x1 dummy PNG as fallback.")
	}

	// Create a session with the specified agent.
	session, err := c.CreateSession(ctx, client.SessionRequest{
		Agent:   *agent,
		WorkDir: ".",
	})
	if err != nil {
		log.Fatalf("failed to create session: %v", err)
	}
	defer session.Terminate(ctx)
	fmt.Printf("Session created: %s\n", session.ID)

	// Send a multimodal message (text + image) and stream the response to stdout.
	stream, err := session.SendMessage(ctx, []client.ContentPart{
		{Type: "text", Text: *prompt},
		{Type: "image", Source: &client.ImageSource{
			Type:      "base64",
			MediaType: mediaType,
			Data:      base64Data,
		}},
	})
	if err != nil {
		log.Fatalf("failed to send message: %v", err)
	}

	// Output prints each streamed event to the provided writer.
	if err := stream.Output(os.Stdout); err != nil {
		log.Fatalf("stream output error: %v", err)
	}
}
