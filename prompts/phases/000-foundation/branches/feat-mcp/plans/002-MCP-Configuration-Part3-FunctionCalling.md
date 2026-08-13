# 002-MCP-Configuration-Part3-FunctionCalling

> **Source Specification**: [prompts/phases/000-foundation/branches/feat-mcp/ideas/000-MCP-Configuration.md](file://prompts/phases/000-foundation/branches/feat-mcp/ideas/000-MCP-Configuration.md)
>
> **Series**: Part 3 / 4 — ローカル関数 Function calling (クライアント往復)  
> **Depends on**: Part 1 (API 型), Part 2 (Wayfinder 配線土台)  
> **Next**: [003-MCP-Configuration-Part4-Claude-Codex-Inject](file://prompts/phases/000-foundation/branches/feat-mcp/plans/003-MCP-Configuration-Part4-Claude-Codex-Inject.md)

## Goal Description

セッションの `functions` を Wayfinder の LLM ツールとして公開し、モデルが呼んだときクライアントへ SSE で `function_call` を通知、クライアントが `POST .../tool_results` で結果を返すとループを継続する。MCP 混在 (VS4) も本 Part で確認する。

## User Review Required

1. **API 形状 (本計画で採用)**: 専用エンドポイント `POST /api/v1/sessions/:id/tool_results`。`/respond` (ask_user) とは分離する。
2. **デフォルト待ち時間**: 120 秒。`FunctionConfig` に per-function timeout は初回載せない (グローバル定数 + 将来拡張)。
3. **Claude/Codex の functions**: 仕様 O1 どおり **本 Part 対象外** (Wayfinder のみ)。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point |
| :--- | :--- |
| R4 / R4-1 ローカル関数往復 | SSE function_call + tool_results |
| R4-3 タイムアウト | 120s → ツールエラーとしてモデルへ |
| R4-4 Wayfinder functions 必須 | Registry 登録 + bridgeTool |
| R3-4 SDK SubmitToolResult | client/v1 |
| R3-5 docs | ReferenceManual |
| VS3 / VS4 | integration tests |
| D4 / D5 | クライアント実行 + SSE |

## Proposed Changes

### イベント・エラー型

#### [MODIFY] [shared/libs/go/codingagent/event.go](file://shared/libs/go/codingagent/event.go) (または同等の StreamEvent 定義ファイル)
*   **Description**: クライアント向け function_call イベントを追加。
*   **Technical Design**: 既存 SSE イベント種別に合わせて定数追加。ペイロード例:

```go
// EventTypeFunctionCall = "function_call"
type FunctionCallEvent struct {
	CallID    string          `json:"call_id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}
```

*   **Logic**: `ask_user` / `user_input_required` と同列にメッセージ SSE で送出。

#### [NEW] [shared/libs/go/wayfinder/tools/function_bridge.go](file://shared/libs/go/wayfinder/tools/function_bridge.go)
*   **Description**: ローカル関数用ハンドラが返す制御エラー。
*   **Technical Design**:

```go
var ErrFunctionCallRequired = errors.New("function call required")

type FunctionCallRequest struct {
	CallID    string
	Name      string
	Arguments map[string]any
}
```

### Wayfinder 登録とループ

#### [NEW] [shared/libs/go/wayfinder/function_register.go](file://shared/libs/go/wayfinder/function_register.go)
*   **Description**: `functions` を Registry へ登録。
*   **Logic**:
    *   `parameters` (JSON Schema) を `map[string]any` に Unmarshal して InputSchema に。
    *   Handler は `ErrFunctionCallRequired` を返し、呼び出し引数を AgentCore が読めるよう side channel (例: `AgentCore.pendingFuncCall` または error に値を載せる専用型 `*FunctionCallError`) を使う。

```go
type FunctionCallError struct {
	Req FunctionCallRequest
}
func (e *FunctionCallError) Error() string { return ErrFunctionCallRequired.Error() }
func (e *FunctionCallError) Unwrap() error { return ErrFunctionCallRequired }
```

#### [MODIFY] [shared/libs/go/wayfinder/agent_core.go](file://shared/libs/go/wayfinder/agent_core.go)
*   **Description**: `executeTool` で `FunctionCallError` を検知したら、ask_user と同様にセッションを suspend 相当にし、呼び出し元へ伝播。結果待ちチャネルを用意。
*   **Technical Design**:
    *   `pendingToolResults map[string]chan ToolResultPayload` (call_id → ch)
    *   `SubmitToolResult(callID string, content string, isError bool) error`
    *   タイムアウト: `select` で 120s。タイムアウト時は `"function call timed out"` を tool result としてモデルへ戻しループ継続 (セッションは落とさない)。
*   **Logic**: MCP ツール (mcp__*) は Part 2 の同期 Call のまま。functions のみ非同期往復。

#### [MODIFY] [shared/libs/go/wayfinder/adapter.go](file://shared/libs/go/wayfinder/adapter.go)
*   **Description**: `WithFunctions` で登録。Exec ハンドルを agentservice が `SubmitToolResult` できるよう ExecRegistry に保持 (既存 ask_user と同様)。

### agentservice tool_results

#### [NEW] [shared/libs/go/agentservice/tool_results_test.go](file://shared/libs/go/agentservice/tool_results_test.go)
*   **Description**: TDD — tool_results の 200/400/404/タイムアウト前成功。

#### [MODIFY] [shared/libs/go/agentservice/handler.go](file://shared/libs/go/agentservice/handler.go)
*   **Description**: `handleToolResults` 追加。
*   **Technical Design**:

```go
// POST /api/v1/sessions/:id/tool_results
type ToolResultsRequest struct {
	CallID  string `json:"call_id"`
	Content string `json:"content"`
	IsError bool   `json:"is_error,omitempty"`
}
```

*   **Logic**: 実行中セッションの Wayfinder exec に `SubmitToolResult`。対象が無い/期限切れは 409 または 404。成功時は 200 `{ok:true}`。必要なら結果投入後に SSE 継続を既存 respond パターンに合わせる。

#### [MODIFY] [shared/libs/go/agentservice/service.go](file://shared/libs/go/agentservice/service.go)
*   **Description**: `routeSessionByID` に `/tool_results` を追加。

#### [MODIFY] [shared/libs/go/agentservice/exec_registry.go](file://shared/libs/go/agentservice/exec_registry.go) (存在する場合)
*   **Description**: tool result 提出用インターフェースを exec に追加。無ければ Wayfinder session handle へ型アサーション。

### SSE 送出

#### [MODIFY] [shared/libs/go/agentservice/handler.go](file://shared/libs/go/agentservice/handler.go) (SendMessage SSE 経路)
*   **Description**: FunctionCallError 伝播時に SSE event `function_call` を書き出したあと、結果待ち → 再開 (既存 interactive `/respond` フローを参考。可能なら同一 SendMessage ストリーム上で完結させ、クライアントは並行して tool_results を POST)。
*   **Logic**: `tests/interactive_agent_test.go` の suspend/respond を踏襲した並行モデル:
    1. SSE で function_call 送信
    2. 別 HTTP で tool_results
    3. サーバがループ再開し SSE で続きを送信

### client/v1

#### [MODIFY] [client/v1/session.go](file://client/v1/session.go)
*   **Technical Design**:

```go
type ToolResultRequest struct {
	CallID  string `json:"call_id"`
	Content string `json:"content"`
	IsError bool   `json:"is_error,omitempty"`
}

func (s *Session) SubmitToolResult(ctx context.Context, req ToolResultRequest) error
```

#### [NEW] [client/v1/tool_results_test.go](file://client/v1/tool_results_test.go)
*   **Description**: httptest で POST ボディ検証。

### Documentation

#### [MODIFY] [docs/ReferenceManual-WebAPIs.md](file://docs/ReferenceManual-WebAPIs.md)
*   **Description**: `function_call` SSE イベント、`POST .../tool_results`、タイムアウト動作を追記。

### Integration tests

#### [NEW] [tests/common_function_calling_test.go](file://tests/common_function_calling_test.go)
*   **Description**: VS3 — CreateSession(wayfinder) + functions → mock LLM が function を呼ぶ → SSE 受信 → tool_results → 完了。
*   **Description**: VS4 — mcp mock + function 混在で、MCP は同期成功・function は往復。

## Step-by-Step Implementation Guide

1. Add FunctionCallError + RegisterFunctions tests (fail) → implement register.
2. Extend agent_core executeTool + SubmitToolResult + 120s timeout.
3. Add handleToolResults + route; wire exec registry.
4. Emit SSE function_call on SendMessage path.
5. SDK SubmitToolResult + docs (ReferenceManual に tool_results / function_call 例を仕様から転記)。
6. Extend `examples/mcp-session-tools/` (or NEW `examples/mcp-function-calling/`) to handle SSE `function_call` and `SubmitToolResult` (仕様 Go SDK 例の往復部分を実装)。
7. Integration tests VS3/VS4.
8. Verify: `./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories common --specify function_calling`

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**: `./scripts/process/build.sh`
2. **Integration Tests**: `./scripts/process/integration_test.sh --categories common --specify function_calling`
3. **E2E Tests**: `tests/common_function_calling_test.go` (Wayfinder mock LLM + client round-trip)。

## Documentation

- `docs/ReferenceManual-WebAPIs.md` (`function_call` SSE、`POST .../tool_results`、仕様の HTTP 例)
- `docs/sse-chunk-protocol.md` にイベント種がある場合は `function_call` を追記
- `examples/mcp-session-tools/` または `examples/mcp-function-calling/` に往復サンプルを完成
