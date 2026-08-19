# session-follow example

This is a **Client API example**. It talks HTTP to a **separate** Tern process (default `http://localhost:3100`) using [`github.com/axsh/arctic-tern/client/v1`](../../client/v1). It is an extension of [minimal-client](../minimal-client): after `SendText` it **drops** the in-flight SSE and reattaches with `Follow` / `FollowFrom` instead of sending another user message. This process does **not** call `server.New` or embed AgentService.

**Success** means the logs include `follow mode=Follow` or `follow mode=FollowFrom` and `follow saw result=true`. Exit 0 without Follow is a failure. `session completed after drop; follow not attempted` means the hold window was already gone (the turn finished before reattach).

## Prerequisites

- A running Tern server (for example [minimal-server](../minimal-server); default `http://localhost:3100`)
- Vault API keys for the chosen agent provider
- `claude` CLI for `--agent claudecode` (default), or `codex` CLI for `--agent codex`

This example does **not** start the server. If port 3100 is already in use, pass `--server http://localhost:<port>`.

## How to run

From the repository root:

```bash
go run ./examples/session-follow

go run ./examples/session-follow --server http://localhost:3100 --agent claudecode
```

Built binary (after `./scripts/process/build.sh`):

```bash
./bin/session-follow --help
./bin/session-follow --agent claudecode --drop-after 1 --hold-seconds 60
```

Defaults:

- `-hold-seconds 60`: the default prompt asks the agent to run a shell `sleep` (or `time.sleep`) for N seconds **after** the first tool call, then reply in one sentence. That keep-alive window is what makes Follow a real test. If N elapses before drop, GetSession is `completed` and the example fails.
- `-respond yes`: used if the agent asks for user input (sandbox permission). Empty `-respond` is a fatal error on `user_input_required`.
- `-hold-seconds 0` with no `-prompt`: uses a long essay prompt (not a Follow test).

## What this demonstrates

| Proposition | How this example shows it |
| :--- | :--- |
| Follow does not enqueue a new user message | Reattach uses `GET /api/v1/sessions/:id/events` via `Follow` / `FollowFrom`, not a second `SendText` |
| `from` is a logical SSE `id` captured at drop | Turn context has no `id:`; the example stores the id at drop time and does not use later buffered ids |
| Reattach window | Server default is 90 seconds; the sleep hold must still be running at GetSession |
| Task logs are not turn SSE | `GET .../logs` is not a substitute for Follow |

## Reattach procedure

1. Read SSE from `SendMessage` / `SendText`. Save last id after each fully assembled logical event.
2. After disconnect, call `GetSession`.
3. `completed` → do not Follow (API rule). **This example treats that as failure** because the hold window was missed.
4. `error` (including after drain timeout) → the server has dropped the turn. Do not Follow. Send a new `SendMessage` if you need another turn.
5. `followable` or status `active` / `suspended` → `FollowFrom(lastID)`. If last id is empty, `Follow()` (replay from the start of the buffer).
6. Follow returns 409 `no active turn` → shortly afterward `GetSession` again (race with completion). If the session is already `completed` without a Follow result, this example exits non-zero.
7. `user_input_required` during Follow → existing `Respond` (this example: `-respond`, default `yes`).
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
