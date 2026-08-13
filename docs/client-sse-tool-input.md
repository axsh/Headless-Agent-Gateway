# client/v1: tool_input on tool_use SSE events

## Wire

AgentService SSE `data:` lines for `tool_use` may include:

```json
{"type":"tool_use","tool_name":"command_execution","tool_input":{"command":"ls -la"}}
```

`codingagent.StreamEvent` fields `tool_name` / `tool_input` are forwarded by AgentService.

## Official Go client

- `client/v1.Event.ToolInput` (`map[string]any`) is populated from JSON `tool_input`.
- Legacy package `github.com/axsh/arctic-tern/client` behaves the same.
- The client does **not** truncate or chunk `tool_input`. Oversized SSE lines remain a separate concern (see Issue #26 / `docs/sse-chunk-protocol.md` for `tool_result` chunking).

## Breaking change

`OnToolUse` / `StreamHandlers.OnToolUse` signature:

```go
// before
func(toolName string)
// after
func(toolName string, toolInput map[string]any)
```

No compatibility wrapper is provided.
