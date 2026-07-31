// Package mcp exposes user-uploaded artifacts to Coding Agents via MCP tools.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/artifact/storage"
	"github.com/axsh/arctic-tern/shared/libs/go/artifact/store"
)

// ToolResult is the response from a tool call.
type ToolResult struct {
	Text  string
	Error bool
}

// Options controls optional behavior of the MCP server.
type Options struct {
	// PutDisabled disables the put_user_artifact tool (default: false → tool enabled).
	PutDisabled bool
}

// ArtifactMCPServer exposes user artifact operations as callable MCP tools.
// It does NOT start an HTTP server; instead, consumers embed it and forward
// MCP JSON-RPC requests directly via CallTool.
type ArtifactMCPServer struct {
	store   store.ArtifactStore
	storage *storage.UserArtifactStorage
	opts    Options
}

// New creates an ArtifactMCPServer with default options (put_user_artifact enabled).
func New(s store.ArtifactStore, st *storage.UserArtifactStorage) *ArtifactMCPServer {
	return NewWithOptions(s, st, Options{})
}

// NewWithOptions creates an ArtifactMCPServer with explicit options.
func NewWithOptions(s store.ArtifactStore, st *storage.UserArtifactStorage, opts Options) *ArtifactMCPServer {
	return &ArtifactMCPServer{store: s, storage: st, opts: opts}
}

// CallTool dispatches a tool call and returns the result.
// Returns an error only for unknown tools; tool-level errors are represented in ToolResult.Error.
func (a *ArtifactMCPServer) CallTool(ctx context.Context, name string, args map[string]any) (*ToolResult, error) {
	switch name {
	case "list_user_artifacts":
		return a.handleListUserArtifacts(ctx, args)
	case "get_user_artifact":
		return a.handleGetUserArtifact(ctx, args)
	case "put_user_artifact":
		return a.handlePutUserArtifact(ctx, args)
	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

// ToolNames returns the list of tools registered by this server.
func (a *ArtifactMCPServer) ToolNames() []string {
	return []string{"list_user_artifacts", "get_user_artifact", "put_user_artifact"}
}

// handleListUserArtifacts implements the list_user_artifacts tool.
//
// Parameters:
//   - q (string, optional): doublestar glob to filter keys.
//   - page (int, optional): 1-indexed page number.
//   - per_page (int, optional): items per page (max 100).
func (a *ArtifactMCPServer) handleListUserArtifacts(ctx context.Context, args map[string]any) (*ToolResult, error) {
	f := store.UserArtifactFilter{
		Q:       stringArg(args, "q"),
		Sort:    stringArg(args, "sort"),
		Order:   stringArg(args, "order"),
		Page:    intArg(args, "page"),
		PerPage: intArg(args, "per_page"),
	}
	page, err := a.store.ListUserArtifacts(ctx, f)
	if err != nil {
		return &ToolResult{Text: fmt.Sprintf("error listing artifacts: %v", err), Error: true}, nil
	}

	lines := []string{
		fmt.Sprintf("User artifacts (total: %d, page: %d/%d)",
			page.TotalCount, page.Page, pageCount(page.TotalCount, page.PerPage)),
	}
	for _, art := range page.Items {
		lines = append(lines, fmt.Sprintf("  %s  (%s, %d bytes, updated: %s)",
			art.Key, art.MIMEType, art.Size,
			art.UpdatedAt.UTC().Format(time.RFC3339)))
	}
	if len(page.Items) == 0 {
		lines = append(lines, "  (no artifacts found)")
	}
	return &ToolResult{Text: strings.Join(lines, "\n")}, nil
}

// handleGetUserArtifact implements the get_user_artifact tool.
//
// Parameters:
//   - key (string, required): the logical path of the artifact.
func (a *ArtifactMCPServer) handleGetUserArtifact(ctx context.Context, args map[string]any) (*ToolResult, error) {
	key := stringArg(args, "key")
	if key == "" {
		return &ToolResult{Text: "parameter 'key' is required", Error: true}, nil
	}

	art, err := a.store.GetUserArtifactByKey(ctx, key)
	if err != nil {
		return &ToolResult{Text: fmt.Sprintf("error: %v", err), Error: true}, nil
	}
	if art == nil {
		return &ToolResult{Text: fmt.Sprintf("not found: artifact with key %q does not exist", key), Error: true}, nil
	}

	rc, err := a.storage.Read(art.ID)
	if err != nil {
		return &ToolResult{Text: fmt.Sprintf("not found: file for key %q: %v", key, err), Error: true}, nil
	}
	defer rc.Close()

	content, err := io.ReadAll(rc)
	if err != nil {
		return &ToolResult{Text: fmt.Sprintf("error reading file: %v", err), Error: true}, nil
	}
	return &ToolResult{Text: string(content)}, nil
}

// handlePutUserArtifact implements the put_user_artifact tool.
//
// Parameters:
//   - key (string, required): the logical path of the artifact.
//   - content (string, required): the text content to store.
func (a *ArtifactMCPServer) handlePutUserArtifact(ctx context.Context, args map[string]any) (*ToolResult, error) {
	if a.opts.PutDisabled {
		return &ToolResult{Text: "put_user_artifact is disabled in this environment", Error: true}, nil
	}

	key := stringArg(args, "key")
	content := stringArg(args, "content")
	if key == "" {
		return &ToolResult{Text: "parameter 'key' is required", Error: true}, nil
	}

	existing, err := a.store.GetUserArtifactByKey(ctx, key)
	if err != nil {
		return &ToolResult{Text: fmt.Sprintf("error: %v", err), Error: true}, nil
	}

	id := generateID()
	if existing != nil {
		id = existing.ID
		_ = a.storage.Delete(existing.ID)
	}

	info, err := a.storage.Write(id, strings.NewReader(content))
	if err != nil {
		return &ToolResult{Text: fmt.Sprintf("storage error: %v", err), Error: true}, nil
	}

	now := time.Now()
	createdAt := now
	if existing != nil {
		createdAt = existing.CreatedAt
	}
	art := store.UserArtifact{
		ID:         id,
		Key:        key,
		ActualPath: info.ActualPath,
		Filename:   lastSegment(key),
		Size:       info.Size,
		MIMEType:   info.MIMEType,
		ContentSHA: info.SHA256,
		CreatedAt:  createdAt,
		UpdatedAt:  now,
	}
	if err := a.store.SaveUserArtifact(ctx, art); err != nil {
		return &ToolResult{Text: fmt.Sprintf("db error: %v", err), Error: true}, nil
	}

	return &ToolResult{Text: fmt.Sprintf("saved: artifact %q (%d bytes, sha256: %s)", key, info.Size, info.SHA256)}, nil
}

// ---- Schema descriptions (used when integrating with an MCP host) ----

// ToolSchema describes one MCP tool's schema for registration with an MCP host.
type ToolSchema struct {
	Name        string
	Description string
	Parameters  map[string]any // JSON Schema object
}

// Schemas returns the JSON Schema descriptions for all registered tools.
func (a *ArtifactMCPServer) Schemas() []ToolSchema {
	return []ToolSchema{
		{
			Name:        "list_user_artifacts",
			Description: "List user-uploaded artifacts stored in Tern. Supports glob filtering (q parameter), pagination, and sorting.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"q":        map[string]any{"type": "string", "description": "Doublestar glob pattern to filter keys (e.g. '**.csv')"},
					"page":     map[string]any{"type": "integer", "description": "Page number (1-indexed)"},
					"per_page": map[string]any{"type": "integer", "description": "Items per page (max 100)"},
					"sort":     map[string]any{"type": "string", "description": "Sort field: 'key', 'size', 'created_at', 'updated_at'"},
					"order":    map[string]any{"type": "string", "description": "Sort direction: 'asc' or 'desc'"},
				},
			},
		},
		{
			Name:        "get_user_artifact",
			Description: "Retrieve the content of a user-uploaded artifact by its logical key.",
			Parameters: map[string]any{
				"type":     "object",
				"required": []string{"key"},
				"properties": map[string]any{
					"key": map[string]any{"type": "string", "description": "Logical path of the artifact (e.g. 'datasets/train.csv')"},
				},
			},
		},
		{
			Name:        "put_user_artifact",
			Description: "Store text content as a user artifact with the given logical key. Creates or replaces the artifact.",
			Parameters: map[string]any{
				"type":     "object",
				"required": []string{"key", "content"},
				"properties": map[string]any{
					"key":     map[string]any{"type": "string", "description": "Logical path of the artifact"},
					"content": map[string]any{"type": "string", "description": "Text content to store"},
				},
			},
		},
	}
}

// ---- helpers ----

func stringArg(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

func intArg(args map[string]any, key string) int {
	if args == nil {
		return 0
	}
	switch v := args[key].(type) {
	case int:
		return v
	case float64:
		return int(v)
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	}
	return 0
}

func pageCount(total, perPage int) int {
	if perPage <= 0 {
		return 1
	}
	return (total + perPage - 1) / perPage
}

func lastSegment(p string) string {
	parts := strings.Split(p, "/")
	if len(parts) == 0 {
		return p
	}
	return parts[len(parts)-1]
}

func generateID() string {
	// Use current nanosecond as a simple unique ID for MCP-created artifacts.
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
