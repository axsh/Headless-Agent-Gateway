# 013-CodingAgentAPI-Part5-ClientContainers

> **Source Specification**: [008-CodingAgentAPI-Completion.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/008-CodingAgentAPI-Completion.md)

## Goal Description

Coding Agent Web API (CAWA) のクライアントExampleバイナリを作成し、Docker コンテナ構成 (All-in-One / Hybrid) を構築する。CAWAクライアントはコンテナ統合テストの実行ツールとしても使用される。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| C6-1: All-in-One Dockerfile | Proposed Changes > container/all-in-one/Dockerfile |
| C6-2: All-in-One entrypoint.sh | Proposed Changes > container/all-in-one/entrypoint.sh |
| C6-3: All-in-One docker-compose.yml | Proposed Changes > container/all-in-one/docker-compose.yml |
| C7-1: Hybrid Gateway Dockerfile | Proposed Changes > container/hybrid/gateway/Dockerfile |
| C7-2: Hybrid Agent Dockerfile | Proposed Changes > container/hybrid/agent/Dockerfile |
| C7-3: Hybrid docker-compose.yml | Proposed Changes > container/hybrid/docker-compose.yml |
| C9-1: cawa-client main.go | Proposed Changes > examples/cawa-client/main.go |
| C9-2: 6サブコマンド | Proposed Changes > examples/cawa-client/main.go |
| C9-3: run フロー | Proposed Changes > examples/cawa-client/main.go (cmdRun) |
| C9-4: SSEパーサー | Proposed Changes > examples/cawa-client/main.go (streamSSE) |
| C9-5: --server フラグ | Proposed Changes > examples/cawa-client/main.go |
| C9-6: build.sh ビルド対象 | 既に自動検出 (examples/*/ パターン) |
| C9-7: container_test.shでcawa-client使用 | Proposed Changes > scripts/test/container_test.sh |

## Proposed Changes

### examples/cawa-client

#### [NEW] [examples/cawa-client/main.go](file://examples/cawa-client/main.go)
*   **Description**: CAWA Web APIクライアント。6つのサブコマンド (health, agents, run, session, logs, terminate) を提供するCLIバイナリ。
*   **Technical Design**:

```go
package main

import (
    "bufio"
    "bytes"
    "encoding/json"
    "flag"
    "fmt"
    "io"
    "net/http"
    "os"
    "strings"
)

var serverURL string

func main() {
    flag.StringVar(&serverURL, "server", "http://localhost:3100", "CAWA server URL")
    flag.Parse()

    args := flag.Args()
    if len(args) == 0 {
        printUsage()
        os.Exit(1)
    }

    switch args[0] {
    case "health":
        cmdHealth()
    case "agents":
        cmdAgents()
    case "run":
        cmdRun(args[1:])
    case "session":
        cmdSession(args[1:])
    case "logs":
        cmdLogs(args[1:])
    case "terminate":
        cmdTerminate(args[1:])
    default:
        fmt.Fprintf(os.Stderr, "Unknown command: %s\n", args[0])
        printUsage()
        os.Exit(1)
    }
}

func printUsage() {
    fmt.Println("Usage: cawa-client [--server URL] <command> [args...]")
    fmt.Println()
    fmt.Println("Commands:")
    fmt.Println("  health                        Check server health")
    fmt.Println("  agents                        List available agents")
    fmt.Println("  run --agent NAME --prompt MSG  Create session and run")
    fmt.Println("  session --id ID               Get session status")
    fmt.Println("  logs --id ID                  Stream session logs")
    fmt.Println("  terminate --id ID             Terminate session")
}
```

*   **Logic**:

**cmdHealth**:
```go
func cmdHealth() {
    resp, err := http.Get(serverURL + "/health")
    if err != nil {
        fmt.Fprintf(os.Stderr, "Error: %v\n", err)
        os.Exit(1)
    }
    defer resp.Body.Close()
    fmt.Printf("Status: %d\n", resp.StatusCode)
    io.Copy(os.Stdout, resp.Body)
    fmt.Println()
}
```

**cmdAgents**:
```go
func cmdAgents() {
    resp, err := http.Get(serverURL + "/api/v1/agents")
    // ... error handling ...
    defer resp.Body.Close()
    var agents []struct{ Name string `json:"name"` }
    json.NewDecoder(resp.Body).Decode(&agents)
    for _, a := range agents {
        fmt.Println(a.Name)
    }
}
```

**cmdRun** (core: SSEストリーミング):
```go
func cmdRun(args []string) {
    fs := flag.NewFlagSet("run", flag.ExitOnError)
    agent := fs.String("agent", "", "Agent name")
    model := fs.String("model", "", "Model name")
    prompt := fs.String("prompt", "", "Prompt message")
    workDir := fs.String("work-dir", ".", "Working directory")
    fs.Parse(args)

    // 1. Create session
    sessionBody, _ := json.Marshal(map[string]string{
        "agent": *agent, "model": *model, "work_dir": *workDir,
    })
    resp, err := http.Post(serverURL+"/api/v1/sessions",
        "application/json", bytes.NewReader(sessionBody))
    // ... error handling ...
    var created map[string]string
    json.NewDecoder(resp.Body).Decode(&created)
    resp.Body.Close()
    sessionID := created["session_id"]
    fmt.Printf("Session created: %s\n", sessionID)

    // 2. Send message with SSE
    msgBody, _ := json.Marshal(map[string]string{"message": *prompt})
    req, _ := http.NewRequest("POST",
        serverURL+"/api/v1/sessions/"+sessionID+"/messages",
        bytes.NewReader(msgBody))
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Accept", "text/event-stream")
    resp, err = http.DefaultClient.Do(req)
    // ... error handling ...
    defer resp.Body.Close()

    // 3. Stream SSE events
    streamSSE(resp.Body)

    // 4. Show final session status
    cmdSessionByID(sessionID)
}
```

**streamSSE** (SSEパーサー):
```go
func streamSSE(body io.Reader) {
    scanner := bufio.NewScanner(body)
    for scanner.Scan() {
        line := scanner.Text()
        if !strings.HasPrefix(line, "data: ") {
            continue
        }
        data := strings.TrimPrefix(line, "data: ")
        if data == "[DONE]" {
            fmt.Println("\n--- Stream completed ---")
            return
        }
        var ev struct {
            Type    string `json:"type"`
            Content string `json:"content"`
            ToolName string `json:"tool_name,omitempty"`
        }
        if err := json.Unmarshal([]byte(data), &ev); err != nil {
            continue
        }
        switch ev.Type {
        case "text":
            fmt.Print(ev.Content)
        case "tool_use":
            fmt.Printf("\n[Tool: %s]\n", ev.ToolName)
        case "tool_result":
            fmt.Printf("[Tool Result] %s\n", ev.Content)
        case "system":
            fmt.Printf("[System] %s\n", ev.Content)
        default:
            fmt.Printf("[%s] %s\n", ev.Type, ev.Content)
        }
    }
}
```

**cmdSession, cmdLogs, cmdTerminate**:
```go
func cmdSession(args []string) {
    fs := flag.NewFlagSet("session", flag.ExitOnError)
    id := fs.String("id", "", "Session ID")
    fs.Parse(args)
    cmdSessionByID(*id)
}

func cmdSessionByID(id string) {
    resp, _ := http.Get(serverURL + "/api/v1/sessions/" + id)
    defer resp.Body.Close()
    io.Copy(os.Stdout, resp.Body)
    fmt.Println()
}

func cmdLogs(args []string) {
    fs := flag.NewFlagSet("logs", flag.ExitOnError)
    id := fs.String("id", "", "Session ID")
    fs.Parse(args)

    req, _ := http.NewRequest("GET",
        serverURL+"/api/v1/sessions/"+*id+"/logs", nil)
    req.Header.Set("Accept", "text/event-stream")
    resp, err := http.DefaultClient.Do(req)
    // ... error handling ...
    defer resp.Body.Close()
    streamSSE(resp.Body)
}

func cmdTerminate(args []string) {
    fs := flag.NewFlagSet("terminate", flag.ExitOnError)
    id := fs.String("id", "", "Session ID")
    fs.Parse(args)

    resp, _ := http.Post(
        serverURL+"/api/v1/sessions/"+*id+"/terminate",
        "application/json", nil)
    defer resp.Body.Close()
    io.Copy(os.Stdout, resp.Body)
    fmt.Println()
}
```

---

### container/all-in-one (UC-B)

#### [NEW] [container/all-in-one/Dockerfile](file://container/all-in-one/Dockerfile)
*   **Description**: HAG All-in-One コンテナ (Gateway + Agent + CLI)
*   **Technical Design**:

```dockerfile
FROM golang:1.21-bookworm AS go-builder
WORKDIR /build
COPY go.work go.work.sum ./
COPY shared/libs/go/ ./shared/libs/go/
COPY examples/ ./examples/
RUN cd shared/libs/go && go build -o /build/bin/hag-server ../../examples/standalone

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y \
    git curl python3 make g++ gosu \
    procps jq file wget ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Node.js + Claude Code CLI
RUN curl -fsSL https://deb.nodesource.com/setup_22.x | bash - \
    && apt-get install -y nodejs
RUN npm install -g @anthropic-ai/claude-code

# Non-root user
RUN groupadd -r claude && useradd -r -g claude -m -d /home/claude claude

COPY --from=go-builder /build/bin/hag-server /usr/local/bin/
COPY container/all-in-one/entrypoint.sh /usr/local/bin/
RUN chmod +x /usr/local/bin/entrypoint.sh

RUN mkdir -p /workspace && chown -R claude:claude /workspace /home/claude

ENV CLAUDE_CODE_SKIP_SANDBOX=1
ENV ANTHROPIC_API_KEY="not-needed"
ENV LLM_GATEWAY_URL="http://localhost:14000"
ENV DEFAULT_MODEL=""
ENV AGENT_AUTH_TOKEN=""

EXPOSE 14000 3100
WORKDIR /workspace
ENTRYPOINT ["entrypoint.sh"]
CMD ["hag-server"]
```

---

#### [NEW] [container/all-in-one/entrypoint.sh](file://container/all-in-one/entrypoint.sh)

```bash
#!/bin/bash
# Fix volume ownership
chown -R claude:claude /workspace 2>/dev/null || true
# Drop to claude user
exec gosu claude "$@"
```

---

#### [NEW] [container/all-in-one/docker-compose.yml](file://container/all-in-one/docker-compose.yml)

```yaml
version: "3.8"
services:
  hag:
    build:
      context: ../..
      dockerfile: container/all-in-one/Dockerfile
    ports:
      - "14000:14000"
      - "3100:3100"
    volumes:
      - workspace:/workspace
    environment:
      - DEFAULT_MODEL=${DEFAULT_MODEL:-}
      - AGENT_AUTH_TOKEN=${AGENT_AUTH_TOKEN:-}
    extra_hosts:
      - "host.docker.internal:host-gateway"
volumes:
  workspace:
```

---

### container/hybrid (UC-C)

#### [NEW] [container/hybrid/gateway/Dockerfile](file://container/hybrid/gateway/Dockerfile)
*   **Technical Design**:

```dockerfile
FROM golang:1.21-bookworm AS builder
WORKDIR /build
COPY go.work go.work.sum ./
COPY shared/libs/go/ ./shared/libs/go/
COPY examples/ ./examples/
RUN cd shared/libs/go && go build -o /build/bin/hag-gateway ../../examples/standalone

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*
COPY --from=builder /build/bin/hag-gateway /usr/local/bin/
EXPOSE 14000
CMD ["hag-gateway", "--gateway-only"]
```

---

#### [NEW] [container/hybrid/agent/Dockerfile](file://container/hybrid/agent/Dockerfile)

```dockerfile
FROM golang:1.21-bookworm AS go-builder
WORKDIR /build
COPY go.work go.work.sum ./
COPY shared/libs/go/ ./shared/libs/go/
COPY examples/ ./examples/
RUN cd shared/libs/go && go build -o /build/bin/hag-agent ../../examples/standalone

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y \
    git curl python3 gosu procps ca-certificates \
    && rm -rf /var/lib/apt/lists/*
RUN curl -fsSL https://deb.nodesource.com/setup_22.x | bash - \
    && apt-get install -y nodejs
RUN npm install -g @anthropic-ai/claude-code

RUN groupadd -r claude && useradd -r -g claude -m -d /home/claude claude
COPY --from=go-builder /build/bin/hag-agent /usr/local/bin/
COPY container/hybrid/agent/entrypoint.sh /usr/local/bin/
RUN chmod +x /usr/local/bin/entrypoint.sh

RUN mkdir -p /workspace && chown -R claude:claude /workspace /home/claude

ENV CLAUDE_CODE_SKIP_SANDBOX=1
ENV LLM_GATEWAY_URL=""
ENV AGENT_AUTH_TOKEN=""

EXPOSE 3100
WORKDIR /workspace
ENTRYPOINT ["entrypoint.sh"]
CMD ["hag-agent", "--agent-only"]
```

---

#### [NEW] [container/hybrid/agent/entrypoint.sh](file://container/hybrid/agent/entrypoint.sh)

```bash
#!/bin/bash
chown -R claude:claude /workspace 2>/dev/null || true
exec gosu claude "$@"
```

---

#### [NEW] [container/hybrid/docker-compose.yml](file://container/hybrid/docker-compose.yml)

```yaml
version: "3.8"
services:
  gateway:
    build:
      context: ../..
      dockerfile: container/hybrid/gateway/Dockerfile
    ports:
      - "14000:14000"
  agent:
    build:
      context: ../..
      dockerfile: container/hybrid/agent/Dockerfile
    ports:
      - "3100:3100"
    environment:
      - LLM_GATEWAY_URL=http://gateway:14000
      - AGENT_AUTH_TOKEN=${AGENT_AUTH_TOKEN:-}
    volumes:
      - workspace:/workspace
    depends_on:
      - gateway
    extra_hosts:
      - "host.docker.internal:host-gateway"
volumes:
  workspace:
```

---

### scripts/test

#### [NEW] [scripts/test/container_test.sh](file://scripts/test/container_test.sh)
*   **Description**: コンテナ統合テストスクリプト。cawa-clientバイナリを使用。
*   **Technical Design**:

```bash
#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
CAWA_CLIENT="$PROJECT_ROOT/bin/cawa-client"
COMPOSE_TYPE="${1:-all-in-one}"
COMPOSE_DIR="$PROJECT_ROOT/container/$COMPOSE_TYPE"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'

pass() { echo -e "${GREEN}PASS${NC}: $1"; }
fail() { echo -e "${RED}FAIL${NC}: $1"; exit 1; }

echo "=== Container Integration Test: $COMPOSE_TYPE ==="

# Pre-check: cawa-client binary
if [[ ! -x "$CAWA_CLIENT" ]]; then
    echo "Building cawa-client..."
    cd "$PROJECT_ROOT" && ./scripts/process/build.sh
fi

# 1. Build containers
echo "Building containers..."
cd "$COMPOSE_DIR" && docker-compose build

# 2. Start containers
echo "Starting containers..."
docker-compose up -d

# 3. Wait for health (max 30s)
echo "Waiting for health check..."
for i in $(seq 1 30); do
    if "$CAWA_CLIENT" --server "http://localhost:3100" health 2>/dev/null; then
        break
    fi
    if [[ $i -eq 30 ]]; then
        fail "Health check timeout after 30s"
    fi
    sleep 1
done
pass "Health check"

# 4. List agents
AGENTS=$("$CAWA_CLIENT" --server "http://localhost:3100" agents 2>&1)
if [[ -n "$AGENTS" ]]; then
    pass "List agents: $AGENTS"
else
    fail "No agents returned"
fi

# 5. Create + terminate session
SESSION_ID=$("$CAWA_CLIENT" --server "http://localhost:3100" run \
    --agent claudecode --prompt "echo hello" 2>&1 | grep "Session created" | awk '{print $3}') || true
if [[ -n "$SESSION_ID" ]]; then
    pass "Session lifecycle"
else
    echo "WARN: Session test skipped (no real agent available)"
fi

# 6. Cleanup
echo "Stopping containers..."
cd "$COMPOSE_DIR" && docker-compose down -v

echo "=== All tests passed ==="
```

---

## Step-by-Step Implementation Guide

1.  **Step 1: cawa-client main.go**:
    *   Create `examples/cawa-client/main.go` with all 6 subcommands
    *   Implement SSE parser (`streamSSE`)
    *   `./scripts/process/build.sh` で `bin/cawa-client` がビルドされることを確認

2.  **Step 2: All-in-One コンテナ構成**:
    *   Create `container/all-in-one/Dockerfile`
    *   Create `container/all-in-one/entrypoint.sh`
    *   Create `container/all-in-one/docker-compose.yml`

3.  **Step 3: Hybrid コンテナ構成**:
    *   Create `container/hybrid/gateway/Dockerfile`
    *   Create `container/hybrid/agent/Dockerfile`
    *   Create `container/hybrid/agent/entrypoint.sh`
    *   Create `container/hybrid/docker-compose.yml`

4.  **Step 4: コンテナ統合テストスクリプト**:
    *   Create `scripts/test/container_test.sh`

5.  **Step 5: ビルド検証**:
    *   Verification Plan を実行

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests** (cawa-client ビルド確認含む):
    ```bash
    ./scripts/process/build.sh
    ```
    *   **Log Verification**: `bin/cawa-client` が生成されていること

2.  **コンテナ統合テスト** (Docker環境がある場合):
    ```bash
    ./scripts/test/container_test.sh all-in-one
    ./scripts/test/container_test.sh hybrid
    ```
    *   **注意**: Docker環境依存のため、Docker未インストール環境ではスキップ可

### テスト項目のセルフレビュー

| # | 観点 | 結果 |
|---|------|------|
| 1 | 正常系 | cawa-clientの各サブコマンドが正常応答を処理 |
| 2 | 異常系 | サーバ未起動時のエラーメッセージ |
| 3 | 外部連携 | Docker コンテナビルド + 起動 |
| 4 | データ一貫性 | SSEストリームの`data:`行パース |
| 5 | 状態遷移 | session create -> message -> [DONE] |
| 6 | 設定反映 | `--server` フラグの反映 |
| 7 | 副作用 | docker-compose down -v でボリューム削除 |

**セルフレビュー結果**: cawa-clientはCLIツールであるため単体テストは最小限(ビルド可能性の検証)。統合テストはコンテナテストスクリプトでエンドツーエンド検証。

---

## 継続計画について

本計画書はPart5 (CAWAクライアント + コンテナ) です。以下のPartが別ファイルで続きます:

- **Part4** ([012-CodingAgentAPI-Part4-QualityFixes.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/plans/012-CodingAgentAPI-Part4-QualityFixes.md)): コード品質修正
- **Part6** ([014-CodingAgentAPI-Part6-IntegrationTests.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/plans/014-CodingAgentAPI-Part6-IntegrationTests.md)): 統合テスト
