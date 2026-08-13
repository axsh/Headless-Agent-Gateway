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
| `POST` | `/api/v1/sessions` | Initialize a new coding session. |
| `GET` | `/api/v1/sessions/:id` | Retrieve metadata and state of a specific session. |
| `PATCH` | `/api/v1/sessions/:id` | Update session fields (currently `config_dir`). |
| `DELETE` | `/api/v1/sessions/:id` | Delete session data. |
| `POST` | `/api/v1/sessions/:id/messages` | Send a message (text/image) to a session. |
| `POST` | `/api/v1/sessions/:id/terminate` | Force terminate an active session process. |
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

### 4. Create Session

Initializes a new coding session.

- **Method**: `POST`
- **Path**: `/api/v1/sessions`
- **Request Body (JSON)**:
  - `agent` (string, Required): The name of the agent to use (`claudecode`, `wayfinder`, etc.).
  - `model` (string, Optional): The LLM model to use. If not specified, the default model is applied.
  - `work_dir` (string, Required): The absolute workspace directory path where the agent will operate.
  - `session_dir` (string, Optional): The directory path to store session data. Defaults to `work_dir/.{agent_name}`.
  - `config_dir` (string, Optional): Agent config set directory (skills / rules / settings). When set, Tern overlays allowlisted entries into `session_dir` before launching the agent. When omitted, behavior is unchanged from previous versions (no overlay).
  - `mcp_servers` (object, Optional): Named MCP server definitions (`stdio` or `http`). Validated on create; invalid configs return `400` and do not create a session. See [MCP and local functions](#mcp-and-local-functions).
  - `functions` (object, Optional): Client-defined function schemas for Function calling (Wayfinder). Names must not start with `mcp__`.
  - Paths (`work_dir`, `session_dir`, `config_dir`) must be visible to the Tern process (for example, mounted into the container when Tern runs in Docker).
  ```json
  {
    "agent": "wayfinder",
    "model": "qwen2.5-coder:7b",
    "work_dir": "/path/to/workspace",
    "session_dir": "/path/to/tern-sessions/job-1",
    "mcp_servers": {
      "filesystem": {
        "transport": "stdio",
        "command": "npx",
        "args": ["-y", "@modelcontextprotocol/server-filesystem", "/path/to/workspace"]
      },
      "remote-docs": {
        "transport": "http",
        "url": "https://mcp.example.com/mcp",
        "headers": {
          "Authorization": "vault://mcp/remote-docs/token"
        },
        "timeout_ms": 30000
      }
    },
    "functions": {
      "lookup_ticket": {
        "description": "Look up a ticket by ID",
        "parameters": {
          "type": "object",
          "properties": {
            "ticket_id": { "type": "string" }
          },
          "required": ["ticket_id"]
        }
      }
    }
  }
  ```
  - Persistence env mapping remains: Claude Code uses `CLAUDE_CONFIG_DIR=session_dir`; Codex uses `CODEX_HOME=session_dir`.
  - Precedence:
    - Claude Code: CLI flags > project `.claude` under `work_dir` > user config under `CLAUDE_CONFIG_DIR` (after overlay). Project `.claude` nesting of `config_dir` is not supported.
    - Codex: CLI `-c` > (when `config_dir` is set) `$CODEX_HOME` user config/skills > project `.codex`; when `config_dir` is omitted, `--ignore-user-config` + `-c` as today.
  - Overlay is re-applied on each agent process start; session-only data (`projects/`, `sessions/`, …) is preserved.
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
  - `mcp_servers` / `functions`: Present when configured. Values in `mcp_servers.*.headers` and `mcp_servers.*.env` are masked as `***`.
  ```json
  {
    "id": "a95db64cb646901efb395a18d817a37d",
    "agent_name": "wayfinder",
    "model": "qwen2.5-coder:7b",
    "status": "active",
    "work_dir": "/path/to/workspace",
    "session_dir": "/path/to/tern-sessions/job-1",
    "mcp_servers": {
      "remote-docs": {
        "transport": "http",
        "url": "https://mcp.example.com/mcp",
        "headers": { "Authorization": "***" },
        "timeout_ms": 30000
      }
    },
    "functions": {
      "lookup_ticket": {
        "description": "Look up a ticket by ID",
        "parameters": {
          "type": "object",
          "properties": {
            "ticket_id": { "type": "string" }
          },
          "required": ["ticket_id"]
        }
      }
    },
    "agent_session_id": "agent-internal-session-id",
    "error": ""
  }
  ```
  - `config_dir` is included when set at CreateSession time (or later via PATCH).

---

### 6. Update Session (`config_dir`, `mcp_servers`, `functions`)

Updates selected session fields without changing `work_dir`, `session_dir`, or `agent_session_id`. Changes apply on the **next** message send (when the agent process starts). Updating these fields does **not** require `terminate`.

- **Method**: `PATCH`
- **Path**: `/api/v1/sessions/:id`
- **Request Body (JSON)**: At least one of the following must be present (omitted fields are left unchanged):
  - `config_dir` (string): Path to the config set directory. An empty string clears `config_dir`.
  - `mcp_servers` (object): Full replacement of MCP servers. Use `{}` to clear.
  - `functions` (object): Full replacement of local functions. Use `{}` to clear.
- **Example** (replace MCP only):
  ```json
  {
    "mcp_servers": {
      "filesystem": {
        "transport": "stdio",
        "command": "npx",
        "args": ["-y", "@modelcontextprotocol/server-filesystem", "/path/to/workspace"]
      }
    }
  }
  ```
- **Response (200 OK)**: Full session record (same shape as Get Session, with MCP secrets masked).
- **Errors**:
  - `404` session not found
  - `400` no updatable fields, invalid path, or invalid MCP/function schema

### MCP and local functions

Session-scoped tools are configured only via the Client API (not `config.yaml`).

| Field | Purpose |
|-------|---------|
| `mcp_servers` | External MCP servers (`transport`: `stdio` or `http`) |
| `functions` | Client-executed Function calling schemas (Wayfinder) |

Claude Code / Codex receive `mcp_servers` via native config injection on agent start (see implementation notes). Wayfinder connects as an MCP host. Submitting results for local functions uses `POST /api/v1/sessions/:id/tool_results` (documented when Function calling wiring ships).

Runnable Go sample: `examples/mcp-session-tools/`.

---

### 6b. Update Session (`config_dir` only) — compatibility note

The previous `config_dir`-only PATCH body remains supported:

```json
{
  "config_dir": "/path/to/config-sets/beta"
}
```

Named `profile` resolution is out of scope; pass an absolute or process-visible directory path.

- **Errors** (config_dir path):
  - `404` session not found
  - `400` path does not exist, or path is not a directory

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

---

### 9. Terminate Session

Forcefully stops and terminates the running session process (the agent process executing in the background).

- **Method**: `POST`
- **Path**: `/api/v1/sessions/:id/terminate`
- **Response (200 OK)**:
  ```json
  {
    "status": "terminated"
  }
  ```

---

### 10. Stream Task Logs

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
