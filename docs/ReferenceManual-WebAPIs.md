# CAWA v1 Web API Reference Manual

CAWA (Coding Agent Web API) v1 provides RESTful API and Server-Sent Events (SSE) endpoints for managing the lifecycle of coding agents, creating sessions, sending asynchronous messages, and streaming execution task logs.

## Overview

By default, all API endpoints are exposed at `http://localhost:3100` (customizable via `agent_service.port` in the configuration file `config.yaml`).

---

## Endpoint List

| Method | Path | Description |
| :--- | :--- | :--- |
| `GET` | `/health` | Health check of the agent service, LLMGP status, and server settings. |
| `GET` | `/api/v1/agents` | Retrieve the list of available coding agents. |
| `GET` | `/api/v1/models` | Retrieve available LLM models and the default model. |
| `POST` | `/api/v1/embeddings` | Create text embeddings (bypasses Coding Agents; proxied to LLMGP). |
| `GET` | `/api/v1/embeddings/models` | Retrieve embedding-only models (`mode: embedding`). |
| `POST` | `/api/v1/sessions` | Initialize a new coding session. |
| `GET` | `/api/v1/sessions?work_dir=` | List sessions persisted under a workspace `.tern` directory. |
| `GET` | `/api/v1/sessions/:id` | Retrieve metadata and state of a specific session. |
| `PATCH` | `/api/v1/sessions/:id` | Update `config_dir`, `agent`, `model`, and/or `supplement`. |
| `DELETE` | `/api/v1/sessions/:id` | Delete session data. |
| `POST` | `/api/v1/sessions/:id/messages` | Send a message (text/image) to a session. |
| `GET` | `/api/v1/sessions/:id/events` | Reattach to the in-flight turn SSE (no new message). |
| `POST` | `/api/v1/sessions/:id/cancel` | Abort the in-flight turn; keep the same session id (not closed). |
| `POST` | `/api/v1/sessions/:id/terminate` | Force terminate an active session process (status becomes closed). |
| `GET` | `/api/v1/sessions/:id/logs` | Stream detailed task logs generated during session execution. |

---

## Endpoint Details

### 1. Health Check

Performs a health check, retrieves the cached LLM Gateway Proxy (LLMGP) status, and details the server configuration settings.

- **Method**: `GET`
- **Path**: `/health`
- **Response (200 OK)**:
  ```json
  {
    "status": "ok",
    "cli_versions": {
      "claudecode": "0.1.0",
      "codex": "1.2.3"
    },
    "gateway": {
      "status": "ok",
      "url": "http://localhost:3101",
      "last_checked_at": "2026-06-29T17:45:00+09:00"
    },
    "server_settings": {
      "disable_sandbox": true,
      "enable_subagent": false,
      "enabled_versions": [1]
    }
  }
  ```

---

### 2. List Agents

Retrieves the names of all available coding agents.

- **Method**: `GET`
- **Path**: `/api/v1/agents`
- **Response (200 OK)**:
  ```json
  [
    {"name": "claudecode"},
    {"name": "codex"},
    {"name": "wayfinder"}
  ]
  ```

---

### 3. List Models

Retrieves the list of all available LLM models and the default model, obtained via the LLM Gateway Proxy (LLMGP).

- **Method**: `GET`
- **Path**: `/api/v1/models`
- **Response (200 OK)**:
  ```json
  {
    "models": [
      {
        "provider": "anthropic",
        "model": "claude-3-5-sonnet-20241022",
        "tool_call_fallback": false
      },
      {
        "provider": "ollama",
        "model": "qwen2.5-coder:7b",
        "tool_call_fallback": true
      }
    ],
    "default_model": {
      "provider": "anthropic",
      "model": "claude-3-5-sonnet-20241022",
      "tool_call_fallback": false
    }
  }
  ```

---

### 3.1 Create Embeddings

Creates text embeddings via LLMGP. This endpoint **does not** start a Coding Agent session; AgentService proxies the request to `POST /v1/embeddings` on the gateway.

- **Method**: `POST`
- **Path**: `/api/v1/embeddings`
- **Request Body (JSON)**: OpenAI Embeddings API compatible (`model`, `input` as string or string array, optional `encoding_format`, `dimensions`)
- **Response (200 OK)**: OpenAI-compatible embedding list (`object`, `data`, `model`, `usage`)

### 3.2 List Embedding Models

Returns models declared with `mode: embedding` in `model_profiles.yaml`. These models are excluded from `GET /api/v1/models`.

- **Method**: `GET`
- **Path**: `/api/v1/embeddings/models`
- **Response (200 OK)**:
  ```json
  {
    "models": [
      {
        "provider": "openai",
        "model": "text-embedding-3-small"
      }
    ]
  }
  ```

---

### 4. Create Session

Initializes a new coding session.

- **Method**: `POST`
- **Path**: `/api/v1/sessions`
- **Terminology**:
  - `storage_root`: Parent directory for Agent Homes (default = `work_dir`). Agent Homes are `{storage_root}/.tern` (Wayfinder Home), `{storage_root}/.codex`, and `{storage_root}/.claude`.
  - **Canonical session folder**: `{storage_root}/.tern/{session_id}` — persisted as API `session_dir` when defaulted. Contains `record.json`, `metadata.json`, and `history/`.
  - `session_id`: HTTP session identifier issued by Tern; not part of the Home path definition.
- **Request Body (JSON)**:
  - `agent` (string, Required): The name of the agent to use (`claudecode`, `wayfinder`, etc.).
  - `model` (string, Optional): The LLM model to use. If not specified, the default model is applied.
  - `work_dir` (string, Required): The absolute workspace directory path where the agent will operate.
  - `storage_root` (string, Optional): Parent directory for Agent Homes. Defaults to `work_dir`. When set to a path other than `work_dir`, `.tern`, `.codex`, and `.claude` are created under this parent.
  - `session_dir` (string, Optional): Override for the **canonical session folder** (Tern leaf). Defaults to `{storage_root}/.tern/{session_id}`. This is not the Codex/Claude CLI home; do not place CLI homes under `{session_dir}/native`.
  - `config_dir` (string, Optional): Agent config set directory (skills / rules / settings). When set, Tern overlays allowlisted entries into the agent vendor home before launching the agent (`{storage_root}/.claude` for Claude Code, `{storage_root}/.codex` for Codex, or the canonical session folder for Wayfinder). When omitted, behavior is unchanged from previous versions (no overlay).
  - `sandbox_mode` (string, Optional): Per-session Codex/Claude sandbox policy. Allowed values:
    - `read-only` (default when omitted and `agent_service.disable_sandbox` is false): Codex `-s read-only` (workspace writes blocked).
    - `workspace-write`: Codex `-s workspace-write` (R/W under the workspace; sandbox retained). Preferred for Agent Service workloads that need file edits without full bypass.
    - `danger-full-access`: Codex `--dangerously-bypass-approvals-and-sandbox` (full bypass). Also used when CreateSession omits `sandbox_mode` and the server has `agent_service.disable_sandbox: true`.
    - Precedence: explicit `sandbox_mode` > server `disable_sandbox` > `read-only`.
    - Claude Code mapping: `danger-full-access` sets `CLAUDE_CODE_SKIP_SANDBOX=1`; `read-only` / `workspace-write` do not (Claude still uses `--permission-mode bypassPermissions`). See Issue [#54](https://github.com/axsh/arctic-tern/issues/54).
    - `sandbox_mode` is fixed at CreateSession time (not changeable via PATCH).
  - `file_change_collectors` (object, Optional): Per-session System Artifact collection algorithms. Tier meanings:
    - **Tier1 (`structured_tool`, bool, default `true`)**: Coding Agent **native** file-change surfaces. For Codex this includes App Server `turn/diff/updated` (recorded as `tool_name=turn_diff`) and compatible `file_change` items; for Claude Code / Cursor, `Write` / `Edit` / `StrReplace` / etc. Claude turns may also record a Tern-synthesized aggregate as `tool_name=turn_files` (not the same as Codex `turn_diff`). Shell-only edits are **not** Tier1.
    - **Tier2 (`shell_parser`, bool, default `true`)**: Infer paths from native **non-file** tools (`Bash`, `command_execution`, …). For create/update, Tern records a path only when the resolved filesystem path exists after the shell command completes (`command_execution` completed / Bash `tool_result`). Delete ops from the parser are kept without an existence check. Missing paths are treated as false positives and omitted.
    - **Tier3 (`workdir_reconcile`, bool, default `false`)**: External observation (end-of-turn git diff / directory snapshot). May include changes from background processes or edits outside the agent session; `.gitignore` paths are not detected via git. Enable explicitly when maximizing coverage.
    - Note: Tern’s default Codex transport is still `codex exec --json`. `turn/diff/updated` is an App Server notification shape that Tern parses and records as Tier1 when present (fixtures / future App Server integration). Exec alone does not emit `turn/diff`.
    - Partial objects apply **per-key defaults** for omitted keys. Unknown keys or non-booleans → `400`.
    - Fixed at CreateSession (not changeable via PATCH). Tracking records path + operation metadata only (not unified diffs).
  - Paths (`work_dir`, `storage_root`, `session_dir`, `config_dir`) must be visible to the Tern process (for example, mounted into the container when Tern runs in Docker).
  ```json
  {
    "agent": "codex",
    "model": "gpt-5.3-codex",
    "work_dir": "/path/to/workspace",
    "storage_root": "/path/to/storage",
    "session_dir": "/path/to/tern-sessions/card-1",
    "config_dir": "/path/to/config-sets/alpha",
    "sandbox_mode": "workspace-write",
    "file_change_collectors": {
      "workdir_reconcile": true
    }
  }
  ```
  - Persistence env mapping:
    - Claude Code: `CLAUDE_CONFIG_DIR={storage_root}/.claude`
    - Codex: `CODEX_HOME={storage_root}/.codex`
    - Wayfinder: agent `SessionDir` is the Tern canonical session folder (`session_dir`, default `{storage_root}/.tern/{session_id}`); same root as the canonical store (no `native/` subdirectory).
  - Precedence:
    - Claude Code: CLI flags > project `.claude` under `work_dir` > user config under `CLAUDE_CONFIG_DIR` (after overlay). Project `.claude` nesting of `config_dir` is not supported.
    - Codex: CLI `-c` > (when `config_dir` is set) `$CODEX_HOME` user config/skills > project `.codex`; when `config_dir` is omitted, `--ignore-user-config` + `-c` as today.
  - Overlay is re-applied on each agent process start; session-only data under the vendor home (`projects/`, `sessions/`, …) is preserved.
  - Leftover `{session_dir}/native` trees from older Tern versions are unused and may be deleted manually (do not delete `history/`).
- **Response (201 Created)**:
  ```json
  {
    "session_id": "a95db64cb646901efb395a18d817a37d",
    "status": "created"
  }
  ```

---

### 5. Get Session

Retrieves metadata and the active state of a created session.

- **Method**: `GET`
- **Path**: `/api/v1/sessions/:id`
- **Response (200 OK)**:
  - `status`: The execution state of the session (`active`, `completed`, `error`, `closed`).
  ```json
  {
    "id": "a95db64cb646901efb395a18d817a37d",
    "agent_name": "claudecode",
    "model": "claude-3-5-sonnet-20241022",
    "status": "active",
    "work_dir": "/path/to/workspace",
    "storage_root": "/path/to/workspace",
    "session_dir": "/path/to/workspace/.tern/a95db64cb646901efb395a18d817a37d",
    "config_dir": "/path/to/config-sets/alpha",
    "sandbox_mode": "workspace-write",
    "file_change_collectors": {
      "structured_tool": true,
      "shell_parser": true,
      "workdir_reconcile": false
    },
    "agent_session_id": "agent-internal-session-id",
    "active_agent": "claudecode",
    "agent_bindings": {
      "claudecode": {
        "agent_session_id": "agent-internal-session-id",
        "ingested_through_seq": 4
      }
    },
    "supplement": {
      "algorithm": "map_reduce",
      "max_chunk_messages": 20,
      "threshold_bytes": 32768,
      "recent_keep": 8
    },
    "error": ""
  }
  ```
  - `config_dir` is included when set at CreateSession time (or later via PATCH).
  - `file_change_collectors` is always present as the resolved three-key object (defaults applied for legacy records).
  - `agent_bindings` and `supplement` are the canonical metadata (effective supplement merges server defaults with the session strategy; turn override is not stored).

---

### 5.1 List Sessions

Lists session records persisted under `{storage_root}/.tern/*/record.json` (or `{work_dir}/.tern/*/record.json` when `storage_root` is omitted). Memory is only a cache; there is no `session.db`.

- **Method**: `GET`
- **Path**: `/api/v1/sessions?work_dir=`
- **Query**:
  - `work_dir` (required) — workspace path used when `storage_root` is omitted; also used with `storage_root` for client compatibility.
  - `storage_root` (optional) — parent directory whose `.tern` folder is scanned. When omitted, defaults to `work_dir`.
- **Response (200 OK)**: JSON array of session records (same shape as Get Session).

---

### 6. Update Session

Updates `config_dir`, `agent`, `model`, and/or `supplement` on an existing session. At least one of these fields is required. Does **not** change `work_dir`, `session_dir`, or the Tern `id`. Overlay of a new `config_dir` runs on the **next** message send. `terminate` is not part of the normal switch flow.

Switch semantics:
- **PATCH `agent`**: clears the active `agent_session_id`. Per-agent `agent_bindings` are kept. The next SendMessage for a new agent does **not** pass another agent's native resume id; it injects a Tern history supplement (header `Tern session context transfer`) for foreign origins. Returning to an agent resumes **that** agent's stored native id and injects only newer foreign-origin facts.
- **PATCH `model` only**: keeps the current native resume id and does **not** inject a transfer header.
- **PATCH `agent` and `model` together**: agent-switch semantics (active native id cleared).
- Busy or suspended sessions return `409`.

- **Method**: `PATCH`
- **Path**: `/api/v1/sessions/:id`
- **Request Body (JSON)** — at least one of:
  - `config_dir` (string): Path to the config set directory. An empty string clears overlay.
  - `agent` (string): Coding agent name (`claudecode`, `codex`, `wayfinder`).
  - `model` (string): LLM model id.
  - `supplement` (object): Partial strategy (`algorithm`, `model`, `max_chunk_messages`, `threshold_bytes`, `recent_keep`). Known algorithms: `map_reduce` (default), `full`, `structured`.
- **Example**:
  ```json
  {
    "agent": "codex",
    "supplement": {
      "algorithm": "map_reduce",
      "model": "",
      "max_chunk_messages": 20,
      "threshold_bytes": 32768,
      "recent_keep": 8
    }
  }
  ```
- **Response (200 OK)**: Full session record (same shape as Get Session).
- **Errors**:
  - `404` session not found
  - `409` session busy or suspended
  - `400` no updatable field, unknown agent/algorithm, invalid `config_dir`, or unsupported model

Server default strategy (merged under session + turn values):

```yaml
agent_service:
  supplement:
    algorithm: map_reduce
    model: ""
    max_chunk_messages: 20
    threshold_bytes: 32768
    recent_keep: 8
```

---

### 7. Delete Session

Deletes the session record from the server.

- **Method**: `DELETE`
- **Path**: `/api/v1/sessions/:id`
- **Response (204 No Content)**: (Empty body)

---

### 8. Send Message

Sends prompt text and image data to an active session, initiating agent execution.

- **Method**: `POST`
- **Path**: `/api/v1/sessions/:id/messages`
- **Request Body (JSON)**:
  Provide structured blocks (text or image) within the `content` array.
  ```json
  {
    "correlation_id": "job-20260814-001",
    "supplement": {
      "algorithm": "full"
    },
    "content": [
      {
        "type": "text",
        "text": "What is in this screenshot?"
      },
      {
        "type": "image",
        "source": {
          "type": "base64",
          "media_type": "image/png",
          "data": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJ..."
        }
      }
    ]
  }
  ```

- **Response Format (Content Negotiation)**:
  The response format varies depending on the request's `Accept` header.

  #### A. Server-Sent Events (SSE) Streaming
  If `Accept: text/event-stream` is included in the request headers, the response is streamed in real time.

  - **Content-Type**: `text/event-stream`
  - **Event Structure**: `data: <JSON>`
    - `type` (string): Event type (`text`, `system`, `error`, etc.).
    - `content` (string): Text chunk output by the agent.
    - `session_id` (string, system events only): Agent-specific internal session ID.
    - `turn_id` (string, optional): Server-generated turn identifier for this SendMessage execution.
    - `correlation_id` (string, optional): Echoed user-supplied correlation ID.
  - **Termination Signal**: Stream ends with `data: [DONE]`.
  - **Response Example**:
    ```http
    HTTP/1.1 200 OK
    Content-Type: text/event-stream
    Cache-Control: no-cache
    Connection: keep-alive

    data: {"type":"text","content":"Analyzing"}

    data: {"type":"text","content":" the image..."}

    data: [DONE]
    ```
  - Transient Codex process exits (`exit status 1`) and upstream stream failures (`Reconnecting...`, high demand, HTTP 429) are retried on the server within bounded limits. Intermediate failures are not written to SSE. Clients wait for a final `result` or a single `error`.
  - When retries are exhausted, `error.content` ends with `[upstream_overloaded]` for classified overload messages, or `[upstream_error]` for generic process failures such as `exit status 1`. Permanent failures (`unauthorized`, invalid API key, unknown model, invalid arguments) are not retried and end with `[upstream_error]`.
  - Closing the SSE connection does not immediately kill the coding-agent CLI. While no SSE subscriber is attached, the server keeps the in-flight turn for **90 seconds** (`agent_service.sse_reattach_timeout_seconds`; tests may override). After that bound it stops the process (`SSE drain timed out; stopping agent process`) and clears busy state. Reattach with `GET /api/v1/sessions/:id/events` inside the window. A follow-up `POST` on the same `session_id` is accepted after the turn ends or the reattach bound elapses (`409` only while execution is still active). If `codex exec resume` fails retryably, Tern drops the native thread id, injects canonical history into a fresh `codex exec`, and keeps the HTTP `session_id`.
  - When Codex process retries are exhausted, SSE still ends with a single classified `error` (`[upstream_error]` or `[upstream_overloaded]`). Operators inspect process logs for `codex process retry exhausted` (`session_id`, `attempt`, `max_attempts`, `resume_mode`, `agent_session_id`, stderr tail up to 8KiB, `exit_status`). Client SSE disconnect logs as `client disconnected during SSE stream`. Drain stop logs as `SSE drain timed out; stopping agent process` with terminal content `client drain timeout`. Gateway upstream deadlines log as `upstream stream read deadline exceeded`. Closing the SSE body can still surface as `stream read error: context deadline exceeded` on the HTTP client.

### 8.1 Turn-Scoped Artifact Correlation

For each `POST /api/v1/sessions/:id/messages` execution, Tern assigns a `turn_id`.

- You may provide an optional `correlation_id` in the request body.
- `turn_id` and `correlation_id` are propagated to System Artifact events created during that execution.
- `POST /api/v1/sessions/:id/respond` continues the same turn.

  #### B. Bulk JSON Response
  If `text/event-stream` is not specified in the `Accept` header, all streaming events are aggregated and returned as a single JSON array.

  - **Content-Type**: `application/json`
  - **Response Example**:
    ```json
    [
      {"type": "text", "content": "Analyzing"},
      {"type": "text", "content": " the image..."}
    ]
    ```

- **Status Codes**:
  - `200 OK`: Successful transmission and processing.
  - `400 Bad Request`: Invalid request data (e.g., sending invalid image data).
  - `404 Not Found`: Session not found.
  - `501 Not Implemented`: Returned if an image is sent to an agent that does not support multi-modal input (e.g., `wayfinder`).

- **409** session busy: JSON `error` is `session busy`, `hint` is `follow, respond, cancel or terminate`.
- When an execution is in `execRegistry`, Get Session includes `followable: true` and `turn_id` of that turn.

---

### 8.2 Follow in-flight turn SSE

Reattach to the current turn without enqueueing a user message. This is not a substitute for `GET .../logs` (task log).

- **Method**: `GET`
- **Path**: `/api/v1/sessions/:id/events`
- **Headers**: `Accept: text/event-stream` (required; otherwise `406`)
- **Query**: `from` (optional) — last fully received **logical** event index (0-based). The server sends the next event. Omit to replay from the start of the current turn buffer.
- **Header** `Last-Event-ID`: same meaning as `from` when the query is absent. Query wins if both are set.
- **Wire**: each logical relay event is prefixed with `id: <index>`. Chunked `tool_result` lines share the same id. The synthetic `turn context` system event has no `id`.
- **Concurrency**: one SSE writer per turn. A new Follow (or Respond) **steals** the previous subscriber.
- **Errors**:
  - `404` session not found
  - `409` with `{"error":"no active turn"}` when the session exists but no in-flight execution
  - `400` invalid or out-of-range `from`

`client/v1`: `Session.Follow(ctx)` and `Session.FollowFrom(ctx, lastEventID)`; `Stream.LastEventID()` tracks assembled logical ids.

**Tool rejection and turn termination**

When a coding agent's sandbox or policy layer rejects a shell command (for example Codex `Rejected(...)` for `rm -f`), Tern surfaces the rejection to subscribers as a **`tool_result`** event when possible (including synthesizing output from stderr if the agent CLI exits without stdout `item.completed`). A turn must not end with a silent HTTP stream close alone: subscribers receive an explicit terminal **`result`** or non-retryable **`error`**, then `data: [DONE]` on `POST /messages` and on Follow (`GET .../events`). Follow clients observe the same event types and termination contract as the original message SSE.

**Tool liveness (progress heartbeat)**

While a tool is in-flight (`tool_use` until the matching `tool_result`, or until the turn ends), Tern injects SSE `progress` events at least every **30 seconds** (override with `SSE_TOOL_HEARTBEAT_INTERVAL`, e.g. `30s`, or server option). Payload shape:

```json
{"type":"progress","content":"tool_still_running","tool_name":"command_execution","turn_id":"..."}
```

`content` value `tool_still_running` is a liveness signal (distinct from WBS progress strings such as `2/5`). Clients may reset stall timers on these events. Comment-only SSE keepalives (`: keepalive`) remain for transport; prefer `progress` for application-level liveness.

---

### 9. Cancel In-Flight Turn

Aborts the current turn (cancels execution context, best-effort stops the agent session / process, clears busy / exec registry) but **keeps the same session id**. Session status becomes non-active (typically `error` with reason `turn cancelled`) and is **not** `closed`. Subsequent `SendMessage` / `PATCH` on the same id remain possible. Does **not** call artifact session close / terminate teardown.

- **Method**: `POST`
- **Path**: `/api/v1/sessions/:id/cancel`
- **Response (200 OK)**:
  ```json
  {
    "status": "cancelled"
  }
  ```

`client/v1`: `Session.CancelTurn(ctx)`.

---

### 10. Terminate Session

Forcefully stops and terminates the running session process (the agent process executing in the background). Sets session status to **`closed`**. Prefer **cancel** when you only need to free `active` / busy for resume on the same id.

- **Method**: `POST`
- **Path**: `/api/v1/sessions/:id/terminate`
- **Response (200 OK)**:
  ```json
  {
    "status": "terminated"
  }
  ```

---

### 11. Stream Task Logs

Streams detailed system logs and progress states generated during session execution via SSE.

- **Method**: `GET`
- **Path**: `/api/v1/sessions/:id/logs`
- **Content-Type**: `text/event-stream`
- **Event Formats**:
  - **event**: `log`
    - `data`: JSON representation of the log entry.
  - **event**: `status` (only on completion/termination)
    - `data`: Final status (`{"status":"terminated"}` or `{"status":"failed"}`).
  - **data**: `[DONE]` (stream termination)
- **Response Example**:
  ```http
  HTTP/1.1 200 OK
  Content-Type: text/event-stream
  Cache-Control: no-cache
  Connection: keep-alive

  event: log
  data: {"id":"log-id-1","session_id":"a95db64...","type":"send","body":"..."}

  event: status
  data: {"status":"terminated"}

  data: [DONE]
  ```
