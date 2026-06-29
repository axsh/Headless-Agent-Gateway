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
  ```json
  {
    "agent": "claudecode",
    "model": "claude-3-5-sonnet-20241022",
    "work_dir": "/path/to/workspace"
  }
  ```
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
    "session_dir": "/path/to/workspace/.claudecode",
    "agent_session_id": "agent-internal-session-id",
    "error": ""
  }
  ```

---

### 6. Delete Session

Deletes the session record from the server.

- **Method**: `DELETE`
- **Path**: `/api/v1/sessions/:id`
- **Response (204 No Content)**: (Empty body)

---

### 7. Send Message

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

### 8. Terminate Session

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

### 9. Stream Task Logs

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
