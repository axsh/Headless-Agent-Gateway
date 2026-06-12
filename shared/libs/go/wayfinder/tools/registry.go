package tools

import (
	"context"
	"sync"

	"github.com/axsh/arctic-tern/wayfinder"
)

// ToolHandler is the function signature for a tool execution handler.
type ToolHandler func(ctx context.Context, input map[string]any) (string, error)

// RegisteredTool holds a tool definition and its handler.
type RegisteredTool struct {
	Name        string
	Description string
	InputSchema map[string]any
	Handler     ToolHandler
}

// Registry manages the set of available tools.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]*RegisteredTool
}

// NewRegistry creates a new empty tool registry.
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]*RegisteredTool),
	}
}

// Register adds a tool to the registry.
func (r *Registry) Register(name, description string, schema map[string]any, handler ToolHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[name] = &RegisteredTool{
		Name:        name,
		Description: description,
		InputSchema: schema,
		Handler:     handler,
	}
}

// Get retrieves a tool by name.
func (r *Registry) Get(name string) (*RegisteredTool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tool, ok := r.tools[name]
	return tool, ok
}

// Definitions returns all registered tools as LLM ToolDefinitions.
func (r *Registry) Definitions() []wayfinder.ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	defs := make([]wayfinder.ToolDefinition, 0, len(r.tools))
	for _, t := range r.tools {
		defs = append(defs, wayfinder.ToolDefinition{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		})
	}
	return defs
}
