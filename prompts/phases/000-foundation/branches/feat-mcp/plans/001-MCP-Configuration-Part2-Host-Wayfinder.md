# 001-MCP-Configuration-Part2-Host-Wayfinder

> **Source Specification**: [prompts/phases/000-foundation/branches/feat-mcp/ideas/000-MCP-Configuration.md](file://prompts/phases/000-foundation/branches/feat-mcp/ideas/000-MCP-Configuration.md)
>
> **Series**: Part 2 / 4 — MCP ホスト (stdio/HTTP) + Wayfinder への MCP ツール供給  
> **Depends on**: [000-MCP-Configuration-Part1-API-SDK](file://prompts/phases/000-foundation/branches/feat-mcp/plans/000-MCP-Configuration-Part1-API-SDK.md)  
> **Next**: [002-MCP-Configuration-Part3-FunctionCalling](file://prompts/phases/000-foundation/branches/feat-mcp/plans/002-MCP-Configuration-Part3-FunctionCalling.md)

## Goal Description

セッションの `mcp_servers` に基づき Tern が MCP ホストとして stdio / HTTP(S) 接続し、`tools/list`・`tools/call` を行う。Wayfinder エージェント実行時に MCP ツールを Registry へ載せて LLM Function calling から実行できるようにする。Claude/Codex 注入とローカル関数往復は含まない (Part 3–4)。

## User Review Required

1. **HTTP 方言 (本計画で採用)**: Tern ホストは `mark3labs/mcp-go` の Streamable HTTP クライアントを第一実装とする。SSE-only エンドポイントは初回非対応 (接続失敗として当該サーバ unavailable)。
2. **Wayfinder ツール名 (本計画で採用)**: MCP ツールは Registry 上で `mcp__{serverName}__{toolName}` とする (`toolconfig.MCPToolNamePrefix`)。LLM に見える名前も同一。
3. **Vault**: `env`/`headers` の `vault://` は接続前に `VaultStore.Resolve` で解決。Vault 未設定サーバでは平文のみ (Resolve 失敗は当該サーバ unavailable)。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point |
| :--- | :--- |
| R5-1〜R5-7 MCP ホスト | shared/libs/go/mcp/* |
| R4-2 MCP 実行 (Wayfinder) | wayfinder Registry + executeTool |
| R4-4 Wayfinder MCP 必須 | adapter / agent_core 配線 |
| R7 観測性 | 構造化ログ (秘密除外) |
| R6 Vault 解決 (実行時) | mcp.Manager resolveSecrets |
| VS1 / VS2 / VS6 部分障害 | unit + tests/common_mcp_host_* |
| D3 Wayfinder=Tern host | 二重接続しない (Claude 経路は Part 4) |

## Proposed Changes

### MCP Host package

#### [NEW] [shared/libs/go/mcp/client.go](file://shared/libs/go/mcp/client.go)
*   **Description**: サーバ単位クライアント抽象。
*   **Technical Design**:

```go
package mcp

type ToolInfo struct {
	Name        string
	Description string
	InputSchema map[string]any
}

type ServerClient interface {
	ListTools(ctx context.Context) ([]ToolInfo, error)
	CallTool(ctx context.Context, name string, args map[string]any) (string, error)
	Close() error
}
```

#### [NEW] [shared/libs/go/mcp/stdio.go](file://shared/libs/go/mcp/stdio.go)
*   **Description**: stdio トランスポート。
*   **Logic**: `exec.CommandContext` で `command`+`args`、`Env`/`Dir=cwd` 設定。stdin/stdout で MCP 初期化。`mark3labs/mcp-go` の stdio クライアントを使用。`Close` で process kill + wait。stderr はデバッグログへ (秘密を含む可能性のある行は冗長に出さない; デフォルト Info では出さない)。

#### [NEW] [shared/libs/go/mcp/http.go](file://shared/libs/go/mcp/http.go)
*   **Description**: HTTP(S) Streamable HTTP クライアント。
*   **Logic**: `url` + 解決済み `headers`。`timeout_ms` が >0 なら http.Client Timeout / context に反映。未設定時デフォルト 30000ms。

#### [NEW] [shared/libs/go/mcp/manager.go](file://shared/libs/go/mcp/manager.go)
*   **Description**: セッション単位マネージャ。
*   **Technical Design**:

```go
type Manager struct {
	sessionID string
	vault     VaultResolver // optional interface { Resolve(string) (string, error) }
	logger    *slog.Logger
	mu        sync.Mutex
	clients   map[string]ServerClient // key = server name
	failed    map[string]string       // server -> last error
}

func NewManager(sessionID string, vault VaultResolver, logger *slog.Logger) *Manager

// ConnectAll connects enabled servers. Per-server failure records in failed; does not return hard error.
func (m *Manager) ConnectAll(ctx context.Context, servers map[string]toolconfig.MCPServerConfig) error

func (m *Manager) ListAllTools(ctx context.Context) (map[string][]ToolInfo, error) // server -> tools
func (m *Manager) Call(ctx context.Context, server, tool string, args map[string]any) (string, error)
func (m *Manager) Close() error
```

*   **Logic**:
    *   `enabled != nil && *enabled == false` はスキップ。
    *   Connect 前に env/headers の各値について `strings.HasPrefix(v, "vault://")` なら Resolve。
    *   失敗サーバは `failed` に入れログ: `session_id`, `mcp_server`, `transport`, `err` (command 全体・header 値は出さない)。
    *   `Close` は全 client Close。idempotent。

#### [NEW] [shared/libs/go/mcp/manager_test.go](file://shared/libs/go/mcp/manager_test.go)
*   **Description**: モック ServerClient / stdio fixture。
*   **Logic**:
    *   部分障害: 2 サーバ中 1 失敗でももう一方 List/Call 可。
    *   Close 後に再 Call は error。
    *   Vault モック解決。

#### [NEW] [shared/libs/go/mcp/stdio_test.go](file://shared/libs/go/mcp/stdio_test.go)
*   **Description**: テスト用ダミー MCP stdio プロセス (fixtures: `shared/libs/go/mcp/testdata/mock_mcp_server.go` を `go run` するか、同パッケージの helper が最小 JSON-RPC を話す)。少なくとも initialize + tools/list + tools/call echo をカバー。

#### [NEW] [shared/libs/go/mcp/http_test.go](file://shared/libs/go/mcp/http_test.go)
*   **Description**: `httptest` で Streamable HTTP 相当のモック (mcp-go が要求する最小ハンドシェイク)。接続・list・call。

#### [MODIFY] [go.mod](file://go.mod)
*   **Description**: `github.com/mark3labs/mcp-go` を direct require に昇格。

### Wayfinder 配線

#### [NEW] [shared/libs/go/wayfinder/mcp_register.go](file://shared/libs/go/wayfinder/mcp_register.go)
*   **Description**: Manager のツールを Registry へ登録。
*   **Logic**:

```go
func RegisterMCPTools(reg *tools.Registry, mgr *mcp.Manager, toolsByServer map[string][]mcp.ToolInfo) {
  for server, list := range toolsByServer {
    for _, t := range list {
      name := toolconfig.MCPToolNamePrefix + server + "__" + t.Name
      // capture server, toolName
      reg.Register(name, t.Description, t.InputSchema, func(ctx context.Context, input map[string]any) (string, error) {
        return mgr.Call(ctx, server, t.Name, input)
      })
    }
  }
}
```

#### [MODIFY] [shared/libs/go/codingagent/options.go](file://shared/libs/go/codingagent/options.go)
*   **Description**: `SessionConfig` にツール設定を追加。

```go
MCPServers map[string]toolconfig.MCPServerConfig
Functions  map[string]toolconfig.FunctionConfig // Part 3 で使用; 本 Part では保持のみ可
```

*   `WithMCPServers` / `WithFunctions` SessionOption を追加。

#### [MODIFY] [shared/libs/go/wayfinder/adapter.go](file://shared/libs/go/wayfinder/adapter.go)
*   **Description**: CreateSession 時に Manager を生成・接続し AgentCore に渡す。セッション終了/Close で `Manager.Close`。
*   **Logic**: `cfg.MCPServers` が空なら何もしない。Vault は server から注入できる場合のみ (agentservice が持つ Vault を option で渡す。無ければ nil)。

#### [MODIFY] [shared/libs/go/wayfinder/agent_core.go](file://shared/libs/go/wayfinder/agent_core.go)
*   **Description**: `NewAgentCore` / セッション開始パスで `RegisterAllTools` の後に `RegisterMCPTools`。Manager をフィールド保持し `Close` 連携。
*   **Logic**: MCP 登録失敗はサーバ単位 (Manager 側)。ツール 0 件でもセッションは継続。

#### [MODIFY] [shared/libs/go/agentservice/handler.go](file://shared/libs/go/agentservice/handler.go)
*   **Description**: `handleSendMessage` の SessionOption 組み立てで `WithMCPServers(record.MCPServers)` (および Part 3 用に Functions) を渡す。
*   **Logic**: agent が Wayfinder のときのみホスト接続が実効。Claude/Codex は Part 4 で注入。

#### [NEW] [shared/libs/go/wayfinder/mcp_register_test.go](file://shared/libs/go/wayfinder/mcp_register_test.go)
*   **Description**: 名前生成と Call 委譲の単体テスト。

### Integration / E2E

#### [NEW] [tests/common_mcp_host_wayfinder_test.go](file://tests/common_mcp_host_wayfinder_test.go)
*   **Description**: VS1/VS2/VS6 — CreateSession(wayfinder) + mcp_servers → 内部またはメッセージ経路で MCP ツールが呼べること。stdio mock / httptest mock を使用。子プロセス残骸が無いこと (Manager.Close)。
*   **Logic**: 実 LLM が重い場合は AgentCore を直接起動するヘルパ、または mock LLM が特定 tool_call を返すフィクスチャを用いる (既存 wayfinder テスト踏襲)。

## Step-by-Step Implementation Guide

1. Promote `mcp-go` in go.mod; add `mcp` package tests (fail first) for stdio/http/manager.
2. Implement stdio/http/client/manager.
3. Add SessionConfig options; wire Wayfinder adapter + RegisterMCPTools.
4. Pass MCPServers from agentservice SendMessage.
5. Add integration test `common_mcp_host_wayfinder_test.go`.
6. Verify: `./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories common --specify mcp_host`

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**: `./scripts/process/build.sh`
2. **Integration Tests**: `./scripts/process/integration_test.sh --categories common --specify mcp_host`
3. **E2E Tests**: `tests/common_mcp_host_wayfinder_test.go` (Wayfinder + mock MCP). 実 Claude/Codex は Part 4。

## Documentation

- Part 1 の ReferenceManual に「Wayfinder は Tern ホストが MCP 実行」を一文追記してよい。
