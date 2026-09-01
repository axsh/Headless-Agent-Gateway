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

## Recommended patterns

| Goal | API | Notes |
| :--- | :--- | :--- |
| Stream response text to stdout | `stream.Output(os.Stdout)` after `SendText` | Same as `examples/minimal-client` |
| Full session / turn / call report | `sess.GetUsage(ctx)` **after** Send completes | Primary path; persisted usage |
| Last turn only | `sess.GetUsage(ctx, client.UsageQuery{LastN: 1})` | Filtered turns; `usage` is re-summed |
| Session total only (lightweight) | `GetSession().Usage` | No per-turn / call breakdown |
| Live call-level usage during stream | `for ev := range stream.Events()` and check `ev.Usage` with `CallID` | Best-effort; optional |

**Principle:** After a successful SendText stream, one `GetUsage` per turn is enough for the full report. You do not need a separate helper to collect `result.Usage` unless you want an immediate snapshot before persistence.

For real-time call usage, iterate `stream.Events()` and log `ev.Usage` when `ev.Usage.CallID != ""`.

## What it prints

| Section | Source |
| :--- | :--- |
| Session usage | `sess.GetUsage(ctx)` → `.Usage`, cross-checked with `GetSession().Usage` |
| Turn usage | `GetUsage().Turns[i].Usage` (includes `model` / `model_source`) |
| LLM call usage | `GetUsage().Turns[i].Calls[]` (prints `(none for this turn)` when empty) |
| LastN=1 | `sess.GetUsage(ctx, client.UsageQuery{LastN: 1})` |

The demo sends **two** turns (streamed to stdout), then prints usage from `GetUsage`.

## Model attribution (`model_source`)

| Value | Meaning |
| :--- | :--- |
| `agent` | Model name from Coding Agent CLI telemetry |
| `tern_session` | Tern filled session model at turn start (e.g. Codex when CLI omits model) |

## Client snippet

```go
stream, _ := sess.SendText(ctx, "hello")
_ = stream.Output(os.Stdout)

repAll, _ := sess.GetUsage(ctx)
repLast, _ := sess.GetUsage(ctx, client.UsageQuery{LastN: 1})
```

Query parameters for `GET /api/v1/sessions/:id/usage` include `last_n`, `after_turn_id`, `from_turn_id`, `to_turn_id`, `since`, and `until` (RFC3339). See `docs/ReferenceManual-WebAPIs.md`.

## Note on cost

If `total_cost_usd` appears, treat it as an estimate from the provider, not a billing figure.
