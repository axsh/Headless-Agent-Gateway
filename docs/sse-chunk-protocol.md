# SSE Chunk Protocol for Large Tool Results

This document describes how arctic-tern splits large `tool_result` events over Server-Sent Events (SSE) so that each `data:` line stays under **64 KiB**, compatible with default `bufio.Scanner` limits in Go clients.

## Overview

When a `tool_result` event exceeds the wire size limit, the AgentService splits it into:

1. One or more `tool_result_part` events (payload chunks)
2. One final `tool_result` completion marker (empty `content`, same `chunk_id`)

The official Go client (`client/v1`) reassembles chunks automatically. Other SSE consumers must implement the same logic.

## Event: `tool_result_part`

| Field | Type | Description |
|-------|------|-------------|
| `type` | string | `"tool_result_part"` |
| `chunk_id` | string | UUID identifying the chunk group |
| `index` | int | Zero-based chunk index |
| `total` | int | Total number of chunks |
| `content` | string | Fragment of the original tool result |

Example:

```json
{"type":"tool_result_part","chunk_id":"550e8400-e29b-41d4-a716-446655440000","index":0,"total":6,"content":"..."}
```

## Completion: `tool_result`

After all parts are sent, the server emits:

```json
{"type":"tool_result","chunk_id":"550e8400-e29b-41d4-a716-446655440000","content":""}
```

- `content` is **empty**
- `chunk_id` matches the preceding `tool_result_part` events
- Consumers concatenate all part `content` values in `index` order to reconstruct the full tool result

## Small payloads (backward compatible)

If the marshaled `tool_result` fits within 64 KiB, the server sends a **single** `tool_result` event with full `content` (no parts).

## Server guarantees

- Each SSE `data:` line (JSON payload) is **strictly less than 64 KiB** (`DefaultMaxSSEDataLineBytes`)
- Chunk content size defaults to **48 KiB** (`DefaultSSEChunkContentBytes`) to leave room for JSON overhead
- Agent-layer truncation (`TruncateToolResult`, default 256 KiB) applies **before** chunking

## Client behavior

| Consumer | Required action |
|----------|-----------------|
| `client/v1` | Automatic reassembly; callbacks see one `tool_result` |
| `client` (legacy) | Automatic reassembly |
| Custom SSE parsers | Must handle `tool_result_part` and completion marker |

## Constants

| Constant | Value | Location |
|----------|-------|----------|
| `DefaultMaxSSEDataLineBytes` | 65536 (64 KiB) | `codingagent/sse_chunk.go` |
| `DefaultSSEChunkContentBytes` | 49152 (48 KiB) | `codingagent/sse_chunk.go` |
| `DefaultMaxToolResultBytes` | 262144 (256 KiB) | `codingagent/stream_io.go` |
