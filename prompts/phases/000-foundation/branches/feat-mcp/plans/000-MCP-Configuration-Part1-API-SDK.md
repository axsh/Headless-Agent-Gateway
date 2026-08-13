# 000-MCP-Configuration-Part1-API-SDK

> **Source Specification**: [prompts/phases/000-foundation/branches/feat-mcp/ideas/000-MCP-Configuration.md](file://prompts/phases/000-foundation/branches/feat-mcp/ideas/000-MCP-Configuration.md)
>
> **Series**: Part 1 / 4 — 共有型・検証・Client API・Go SDK・API ドキュメント  
> **Next**: [001-MCP-Configuration-Part2-Host-Wayfinder](file://prompts/phases/000-foundation/branches/feat-mcp/plans/001-MCP-Configuration-Part2-Host-Wayfinder.md)
>
> **Status**: [x] Completed

## Goal Description

Client API (`POST/GET/PATCH /api/v1/sessions`) と Go SDK (`client/v1`) で、セッションに `mcp_servers` と `functions` を設定・照会・更新できるようにする。実行ランタイム (MCP ホスト / Function calling / Claude·Codex 注入) は Part 2–4。本 Part は永続化・検証・マスク・後方互換まで。

## User Review Required

1. **PATCH セマンティクス (本計画で採用)**: ボディに現れたフィールドだけ更新。`mcp_servers` / `functions` はポインタで受け取り、**省略 = 未変更**、**`{}` = クリア**、**非空 = 全置換**。`config_dir` も同様にポインタ化し、**いずれか 1 フィールド以上必須** (現状の `config_dir` 必須単体から拡張)。
2. **名前衝突 (本計画で採用)**: 同一セッション内で `functions` のキー同士の重複は不可。`functions` のキーが `mcp__` で始まる場合は `400` (Wayfinder 側 MCP ツール接頭辞と衝突回避、Part 2)。MCP サーバ名同士の重複は map キーで自然に不可。
3. **ローカル関数実行モデル / tool_results**: 仕様 D4 どおりクライアント実行。専用 `POST /api/v1/sessions/:id/tool_results` は **Part 3** で追加 (本 Part ではルート予約コメントのみ可、実装は Part 3)。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1-1〜R1-4 統一ツール設定 (API 面) | Proposed Changes > toolconfig, agentservice, client/v1 |
| R2 / R2-A / R2-B スキーマ・検証 | Proposed Changes > toolconfig Validate |
| R3-1 CreateSession | agentservice handlerCreateSession |
| R3-2 GetSession + マスク | SessionRecord + MaskForResponse |
| R3-3 PATCH | handlePatchSession 拡張 |
| R3-4 Go SDK | client/v1/session.go |
| R3-5 ReferenceManual | docs/ReferenceManual-WebAPIs.md |
| R6 マスク (API 応答) | toolconfig.MaskSecrets |
| R1-4 / VS5 / VS6 後方互換・400 | handler_test + integration |
| VS5 SDK | client/v1/session_test.go |

## Proposed Changes

### toolconfig (NEW package)

#### [NEW] [shared/libs/go/toolconfig/types.go](file://shared/libs/go/toolconfig/types.go)
*   **Description**: 仕様 R3-4 の共有型 (agentservice / client / codingagent が参照)。
*   **Technical Design**:

```go
package toolconfig

import "encoding/json"

// MCPServerConfig is one MCP server entry (Client API / SessionRecord).
type MCPServerConfig struct {
	Transport string            `json:"transport"`
	Enabled   *bool             `json:"enabled,omitempty"`
	Command   string            `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Cwd       string            `json:"cwd,omitempty"`
	URL       string            `json:"url,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	TimeoutMS int               `json:"timeout_ms,omitempty"`
}

// FunctionConfig is a client-defined function schema (no handler body).
type FunctionConfig struct {
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// MCPToolNamePrefix is reserved for Wayfinder-registered MCP tools (Part 2).
const MCPToolNamePrefix = "mcp__"
```

#### [NEW] [shared/libs/go/toolconfig/validate.go](file://shared/libs/go/toolconfig/validate.go)
*   **Description**: Create/PATCH 共通検証。
*   **Logic**:
    *   `ValidateMCPServers(servers map[string]MCPServerConfig) error`
        *   サーバ名: 空文字禁止。許可文字は `[a-zA-Z0-9_-]+` (それ以外は error)。
        *   `enabled == false` のエントリもスキーマ検証は行う (定義の正しさを保証)。
        *   `transport == "stdio"`: `command` 必須。`url` が非空なら error。
        *   `transport == "http"`: `url` 必須かつ `http://` または `https://` で始まること。`command` が非空なら error。
        *   それ以外の `transport` は error。
        *   `timeout_ms < 0` は error。
    *   `ValidateFunctions(fns map[string]FunctionConfig) error`
        *   関数名: 空禁止、同じ文字クラス。`strings.HasPrefix(name, MCPToolNamePrefix)` なら error。
        *   `description` 空は error。
        *   `parameters` は JSON object であること (`json.Unmarshal` → `map[string]any`、root が object)。`"type"` が無い場合は許容 (緩い構文検証)。不正 JSON は error。
    *   `ValidateSessionTools(mcp map[string]MCPServerConfig, fns map[string]FunctionConfig) error` — 上記を順に呼ぶ。

#### [NEW] [shared/libs/go/toolconfig/mask.go](file://shared/libs/go/toolconfig/mask.go)
*   **Description**: GET/PATCH レスポンス用マスク。
*   **Logic**:
    *   `MaskMCPServers(in map[string]MCPServerConfig) map[string]MCPServerConfig` — ディープコピーし、各 `Env` / `Headers` の **値** を `"***"` に置換 (キー名は残す)。空 map は空のまま。
    *   Functions はマスク不要 (スキーマのみ)。

#### [NEW] [shared/libs/go/toolconfig/validate_test.go](file://shared/libs/go/toolconfig/validate_test.go)
*   **Description**: テーブル駆動。ケース: stdio OK / http OK / https OK / stdio missing command / http bad url / transport mismatch / empty server name / function missing description / bad parameters JSON / function name with `mcp__` prefix / enabled false still validated。

#### [NEW] [shared/libs/go/toolconfig/mask_test.go](file://shared/libs/go/toolconfig/mask_test.go)
*   **Description**: マスク後に元 map の値が変わらないこと (非破壊)、値が `***` になること。

### codingagent SessionRecord

#### [MODIFY] [shared/libs/go/codingagent/session_store.go](file://shared/libs/go/codingagent/session_store.go)
*   **Description**: セッション永続フィールド追加。
*   **Technical Design**:

```go
type SessionRecord struct {
	ID             string    `json:"id"`
	AgentName      string    `json:"agent_name"`
	Model          string    `json:"model"`
	Status         string    `json:"status"`
	Error          string    `json:"error,omitempty"`
	WorkDir        string    `json:"work_dir"`
	AgentSessionID string    `json:"agent_session_id"`
	SessionDir     string    `json:"session_dir"`
	ConfigDir      string    `json:"config_dir,omitempty"`
	MCPServers     map[string]toolconfig.MCPServerConfig `json:"mcp_servers,omitempty"`
	Functions      map[string]toolconfig.FunctionConfig  `json:"functions,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}
```

*   **Logic**: nil map = 未設定。空 map = 明示クリア後。JSON エンコード時は Get 用に別途マスク済みコピーを返す (handler 側)。

### agentservice

#### [NEW] [shared/libs/go/agentservice/session_tools_test.go](file://shared/libs/go/agentservice/session_tools_test.go)
*   **Description**: Create/Get/PATCH のツール設定テストを先に書く (TDD)。
*   **Logic (cases)**:
    1. Create with valid `mcp_servers` + `functions` → 201、Get で同構造 (env/headers マスク)。
    2. Create with invalid stdio → 400、セッション未作成。
    3. Create omit both → 201、現行互換 (mcp/functions 空)。
    4. PATCH `mcp_servers: {}` → クリア。
    5. PATCH omit `mcp_servers` → 既存維持。
    6. PATCH only `functions` → mcp 維持。
    7. PATCH body に更新対象フィールドなし → 400。

#### [MODIFY] [shared/libs/go/agentservice/handler.go](file://shared/libs/go/agentservice/handler.go)
*   **Description**: Create/Get/PATCH 拡張。Get はマスク済み record を返す。
*   **Technical Design**:
    *   Create リクエスト匿名 struct に追加:

```go
MCPServers map[string]toolconfig.MCPServerConfig `json:"mcp_servers"`
Functions  map[string]toolconfig.FunctionConfig  `json:"functions"`
```

    *   Create: `toolconfig.ValidateSessionTools` 失敗時は `400` + メッセージ、store しない。成功時 `record.MCPServers` / `Functions` に保存 (nil のまま可)。
    *   Get: `encodeSessionRecord(w, record)` ヘルパで `MCPServers` を `MaskMCPServers` したコピーを JSON 化。
    *   PATCH リクエスト:

```go
var req struct {
	ConfigDir  *string                                  `json:"config_dir"`
	MCPServers *map[string]toolconfig.MCPServerConfig   `json:"mcp_servers"`
	Functions  *map[string]toolconfig.FunctionConfig    `json:"functions"`
}
```

    *   `ConfigDir == nil && MCPServers == nil && Functions == nil` → 400 `"at least one of config_dir, mcp_servers, functions is required"`。
    *   `ConfigDir != nil` → 既存 `validateAndResolveConfigDir`。
    *   `MCPServers != nil` → Validate 後に `record.MCPServers = *req.MCPServers` (空 map でクリア)。
    *   `Functions != nil` → 同様。
    *   レスポンスはマスク済みフル record (現行どおり Encode)。

#### [MODIFY] [shared/libs/go/agentservice/handler_test.go](file://shared/libs/go/agentservice/handler_test.go)
*   **Description**: 既存 `TestHandlePatchSession_*` を新セマンティクスに追従 (config_dir のみ PATCH は引き続き成功)。`config_dir` 必須エラーケースを「フィールド無し」ケースへ更新。

### client/v1 SDK

#### [NEW] [client/v1/session_tools_test.go](file://client/v1/session_tools_test.go)
*   **Description**: httptest で Create/Get/PATCH の JSON 往復を検証。

#### [MODIFY] [client/v1/session.go](file://client/v1/session.go)
*   **Description**: 型と PATCH ヘルパ。
*   **Technical Design**:

```go
type SessionInfo struct {
	// ...existing fields...
	MCPServers map[string]toolconfig.MCPServerConfig `json:"mcp_servers,omitempty"`
	Functions  map[string]toolconfig.FunctionConfig  `json:"functions,omitempty"`
}

type SessionRequest struct {
	Agent      string                                  `json:"agent"`
	Model      string                                  `json:"model,omitempty"`
	WorkDir    string                                  `json:"work_dir"`
	SessionDir string                                  `json:"session_dir,omitempty"`
	ConfigDir  string                                  `json:"config_dir,omitempty"`
	MCPServers map[string]toolconfig.MCPServerConfig   `json:"mcp_servers,omitempty"`
	Functions  map[string]toolconfig.FunctionConfig    `json:"functions,omitempty"`
}

// SessionPatch is PATCH /api/v1/sessions/:id body.
type SessionPatch struct {
	ConfigDir  *string                                  `json:"config_dir,omitempty"`
	MCPServers *map[string]toolconfig.MCPServerConfig   `json:"mcp_servers,omitempty"`
	Functions  *map[string]toolconfig.FunctionConfig    `json:"functions,omitempty"`
}

func (c *Client) PatchSession(ctx context.Context, sessionID string, patch SessionPatch) (*SessionInfo, error)
```

*   **Logic**: 既存 `UpdateSessionConfigDir` は `PatchSession` のラッパにリファクタ (後方互換維持)。

### Documentation

#### [MODIFY] [docs/ReferenceManual-WebAPIs.md](file://docs/ReferenceManual-WebAPIs.md)
*   **Description**: Create/Get/PATCH に `mcp_servers` / `functions` を追記。マスク、PATCH セマンティクス、エラー例。仕様書「Client API 使用例」の HTTP サンプルを転記する。`tool_results` は Part 3 で追記予定と一文。

### Runnable example (Client API)

#### [NEW] [examples/mcp-session-tools/main.go](file://examples/mcp-session-tools/main.go)
*   **Description**: `examples/config-dir-switch` / `minimal-client` と同様の実行可能サンプル。CreateSession で MCP (stdio+http) と functions を渡し、GetSession / PatchSession を行う。Wayfinder 向け。`--server` / `--work-dir` フラグ付き。
*   **Logic**: 仕様書「Client API 使用例 > Go SDK」をベースにする。SendMessage + `function_call` 往復の完了形は Part 3 で同ディレクトリに拡張 (または `examples/mcp-function-calling/`).

#### [NEW] [examples/mcp-session-tools/README.md](file://examples/mcp-session-tools/README.md)
*   **Description**: 前提 (起動中 tern)、curl による同等 HTTP 例へのリンク (ReferenceManual)、実行コマンド。

### Integration tests (API 面)

#### [NEW] [tests/common_mcp_session_api_test.go](file://tests/common_mcp_session_api_test.go)
*   **Description**: httptest AgentService で Create/Get/PATCH の API 契約 (既存 `agentservice_integration_test.go` のヘルパ流用可)。
*   **Logic**: VS6 相当 — 不正は 400、省略は互換、マスク確認。

## Step-by-Step Implementation Guide

1. **TDD validate**: Add `toolconfig/validate_test.go` (fail) → implement `types.go` / `validate.go` / `mask.go`. [x]
2. **SessionRecord**: Extend `codingagent/session_store.go`. [x]
3. **Handler tests**: Add `session_tools_test.go` (fail) → update `handler.go` Create/Get/PATCH + `encodeSessionRecord`. [x]
4. **Update existing patch tests** in `handler_test.go`. [x]
5. **SDK**: Extend `client/v1/session.go` + tests; keep `UpdateSessionConfigDir`. [x]
6. **Docs**: Update `ReferenceManual-WebAPIs.md` with HTTP examples from the specification. [x]
7. **Example**: Add `examples/mcp-session-tools/` (main.go + README) mirroring the Go SDK sample in the specification. [x]
8. **Integration**: Add `tests/common_mcp_session_api_test.go`. [x]
9. **Verify Part 1**: `./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify MCPSessionAPI` [x]

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**: `./scripts/process/build.sh`
2. **Integration Tests**: `./scripts/process/integration_test.sh --categories common --specify mcp_session_api`
3. **E2E Tests**: 本 Part は API 契約が主。実エージェント E2E は Part 2–4。`tests/common_mcp_session_api_test.go` を自動化検証とする。

## Documentation

- `docs/ReferenceManual-WebAPIs.md` (必須、仕様の HTTP 例を転記)
- `examples/mcp-session-tools/` (必須、実行可能 Client API 例)
- 仕様 R8-1 の Claude 注入先 (`settings.json`) は実ドキュメント上不正確なため、**Part 4 で `.mcp.json` に確定**し、仕様側の追記修正も Part 4 で行う
