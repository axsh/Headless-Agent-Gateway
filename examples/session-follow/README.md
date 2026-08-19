# session-follow example

This is a **Client API example**. It talks HTTP to a **separate** Tern process (default `http://localhost:3100`) using [`github.com/axsh/arctic-tern/client/v1`](../../client/v1). It is an extension of [minimal-client](../minimal-client): after `SendText` it **drops** the in-flight SSE and reattaches with `Follow` / `FollowFrom` instead of sending another user message. This process does **not** call `server.New` or embed AgentService.

## Prerequisites

- A running Tern server (for example [minimal-server](../minimal-server); default `http://localhost:3100`)
- Vault API keys for the chosen agent provider
- `claude` CLI for `--agent claudecode` (default), or `codex` CLI for `--agent codex`

This example does **not** start the server.

## How to run

From the repository root:

```bash
go run ./examples/session-follow

go run ./examples/session-follow --server http://localhost:3100 --agent claudecode
```

Built binary (after `./scripts/process/build.sh`):

```bash
./bin/session-follow --help
./bin/session-follow --agent claudecode --drop-after 1
```

If the agent asks for user input during Follow, pass a fixed reply:

```bash
./bin/session-follow --respond yes
```

With an empty `-respond` (the default), `user_input_required` is a fatal error.

## What this demonstrates

| Proposition | How this example shows it |
| :--- | :--- |
| Follow does not enqueue a new user message | Reattach uses `GET /api/v1/sessions/:id/events` via `Follow` / `FollowFrom`, not a second `SendText` |
| `from` is a logical SSE `id` | Turn context has no `id:`; `LastEventID` advances only on assembled logical events |
| Reattach window | Server default is 90 seconds; reconnect inside that window |
| Task logs are not turn SSE | `GET .../logs` is not a substitute for Follow |

## Reattach procedure

1. Read SSE from `SendMessage` / `SendText`. Save last id after each fully assembled logical event.
2. After disconnect, call `GetSession`.
3. `completed` → do not Follow.
4. `error` (including after drain timeout) → the server has dropped the turn. Do not Follow. Send a new `SendMessage` if you need another turn.
5. `followable` or status `active` / `suspended` → `FollowFrom(lastID)`. If last id is empty, `Follow()` (replay from the start of the buffer).
6. Follow returns 409 `no active turn` → shortly afterward `GetSession` again (race with completion).
7. `user_input_required` during Follow → existing `Respond` (this example: `-respond`).
8. Reattach within the grace period (default 90 seconds).

## API mapping

| Client method | HTTP |
| :--- | :--- |
| `CreateSession` | `POST /api/v1/sessions` |
| `Session.SendText` | `POST /api/v1/sessions/:id/messages` |
| `Client.GetSession` | `GET /api/v1/sessions/:id` |
| `Session.Follow` / `FollowFrom` | `GET /api/v1/sessions/:id/events` (optional `?from=`) |
| `Session.Respond` | `POST /api/v1/sessions/:id/respond` |
| `Session.Terminate` | `POST /api/v1/sessions/:id/terminate` (cleanup at exit only) |

See [Reference Manual — Web APIs](../../docs/ReferenceManual-WebAPIs.md) (Follow, `GET /api/v1/sessions/:id/events`).

## LIVE billing (optional)

Running against a real Claude/Codex backend incurs provider charges; that is optional and the caller's responsibility. Compilation, `--help`, and the httptest suite in this module do not call an LLM.
