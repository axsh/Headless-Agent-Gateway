# config-dir-switch example

Demonstrates switching `config_dir` on the **same** Tern `session_id` and continuing the conversation on the next message. Terminate is **not** used between turns.

## Prerequisites

- A running Tern server (default `http://localhost:3100`)
- Vault API keys for the chosen agent provider
- `claude` CLI for `--agent claudecode` (default), or `codex` CLI for `--agent codex`

## How to run

From the repository root (or this directory):

```bash
# Default: claudecode
go run ./examples/config-dir-switch

# Codex
go run ./examples/config-dir-switch --agent codex

# Custom server / prompts / config dirs
go run ./examples/config-dir-switch \
  --server http://localhost:3100 \
  --work-dir . \
  --prompt1 'Reply with exactly the word turn-1 and nothing else.' \
  --config-dir-alpha /path/to/alpha \
  --config-dir-beta /path/to/beta
```

Built binary (after `./scripts/process/build.sh`):

```bash
./bin/config-dir-switch --help
./bin/config-dir-switch --agent claudecode
```

## What this demonstrates

| Proposition | How this example shows it |
| :--- | :--- |
| Same Tern `session_id` | Logged before and after PATCH; mismatch is fatal |
| `config_dir` switch mid-session | `UpdateConfigDir` (PATCH) from alpha → beta |
| Conversation continuity | Second `SendText` on the same session without terminate |
| Terminate not required to switch | No terminate between turn 1 and turn 2; cleanup terminate only at exit |

Alpha/beta config directories are created under a temp base with marker files (`CLAUDE.md` or `AGENTS.md` for Codex) containing `TERN_CONFIG_ALPHA` / `TERN_CONFIG_BETA`.

## API mapping

| Client method | HTTP |
| :--- | :--- |
| `CreateSession` (+ `ConfigDir`) | `POST /api/v1/sessions` |
| `Session.SendText` | `POST /api/v1/sessions/:id/messages` |
| `Session.UpdateConfigDir` | `PATCH /api/v1/sessions/:id` |
| `Client.GetSession` | `GET /api/v1/sessions/:id` |

## Notes

- Do **not** terminate between turns merely to switch `config_dir`.
- Overlay from the new `config_dir` applies on the **next** `Send*` call, not immediately at PATCH time.
- Passing an empty `config_dir` via PATCH clears overlay (Codex restores `--ignore-user-config` on later launches).
- See also: [Reference Manual — Web APIs](../../docs/ReferenceManual-WebAPIs.md) (PATCH session).

## LIVE billing (optional)

Default prompts are short to limit cost. Running against a real Claude/Codex backend incurs provider charges; that is optional and the caller's responsibility. Compilation and `--help` do not call the LLM.
