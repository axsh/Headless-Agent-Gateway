# token-usage example

Demonstrates **Session / Turn / LLM-call** token usage with the Tern Client API.

## Prerequisites

- A running Tern server (for example `examples/minimal-server`)
- Claude Code (or another registered agent) available to that server

## Usage

```bash
go run . [server-url] [agent] [model]
```

Defaults: `http://localhost:3100`, agent `claudecode`, empty model (server default).

```bash
go run .
go run . http://localhost:3100 claudecode
```

## What it prints

| Section | Source |
| :--- | :--- |
| Session usage | `sess.GetUsage(ctx)` → `.Usage`, cross-checked with `GetSession().Usage` |
| Turn usage | Stream `result.Usage` after each `SendText`, then `GetUsage().Turns[i].Usage` |
| LLM call usage | `GetUsage().Turns[i].Calls[]` (prints `(none for this turn)` when empty) |
| LastN=1 | `sess.GetUsage(ctx, client.UsageQuery{LastN: 1})` — filtered turns and re-summed `usage` |

The demo sends **two** turns so session totals and `LastN: 1` (last turn only) are visibly different.

## Client snippet

```go
repAll, _ := sess.GetUsage(ctx)
repLast, _ := sess.GetUsage(ctx, client.UsageQuery{LastN: 1})
```

Query parameters for `GET /api/v1/sessions/:id/usage` include `last_n`, `after_turn_id`, `from_turn_id`, `to_turn_id`, `since`, and `until` (RFC3339). See `docs/ReferenceManual-WebAPIs.md`.

## Note on cost

If `total_cost_usd` appears, treat it as an estimate from the provider, not a billing figure.
