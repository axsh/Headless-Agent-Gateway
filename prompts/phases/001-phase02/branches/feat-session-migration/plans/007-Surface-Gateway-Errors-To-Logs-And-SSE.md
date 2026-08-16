# 007-Surface-Gateway-Errors-To-Logs-And-SSE

> **Source Specification**: [ideas/006-Surface-Gateway-Errors-To-Logs-And-SSE.md](file://prompts/phases/001-phase02/branches/feat-session-migration/ideas/006-Surface-Gateway-Errors-To-Logs-And-SSE.md)
>
> **関連 Issue**: [axsh/arctic-tern#41](https://github.com/axsh/arctic-tern/issues/41)

## Goal Description

Gateway が 404/500 を無ログで返す状態をやめ、ERROR `llm gateway error response` を出す。Codex が stderr 空の `exit status 1` で死ぬときは stdout の最後の `EventError` を終端分類と SSE に使う。`process_retry` 既定と 15 秒ドレイン、Gateway JSON schema は変えない。R7（SSE に `[vault_error]` タグ追加）は実装しない。

## User Review Required

仕様 URR は次で固定する。

1. `process_retry` 既定（3 / 3 秒）と 15 秒ドレインは変えない。
2. HTTP JSON の `type` / `message` / `code` は変えない。
3. Codex が 500 本文を捨て `high demand` だけ出す場合、ParseExecEvent は `IsRetryableUpstream` なら EventError を落とす。SSE は過負荷に見え得る。Gateway ERROR には `vault_error` が残る。
4. kanban-gui は移植しない。R7 は実装しない。R8 は README に 1 段落だけ足す。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1 ERROR `llm gateway error response`、フィールド status/code/type/message/path/method/model。全 WriteErrorResponse 経路 | Proposed Changes > handlerctx + 全呼び出し |
| R1 vault 本文 `failed to resolve API key from vault`、code `vault_error`、status 500 は変更しない | openai/handler.go 現行 GatewayError |
| R1 モデル未登録 `model not found: ` + req.Model、code `model_not_found`、404 | 同上 |
| R2 stderr 空なら最後の EventError.Content。それも空なら err.Error() | codex/process.go |
| R3 IsNonRetryableError に failed to resolve api key from vault / vault_error / unexpected status 404 / 401 / 403。unexpected status 500 は足さない | codingagent/retry.go |
| R4 httptest 404 + vault 500 でログと JSON | openai handler テスト + handlerctx テスト |
| R5 `--specify`、`--categories` なし | Verification Plan |
| R6 Chat / Anthropic / embeddings も同じラッパ | handlerctx 一箇所 |
| R8 README 1 段落 | README.md |
| R7 SSE タグ | 対象外 |

## Proposed Changes

依存順: `_test.go` 先（Failed First）→ handlerctx ログ → 呼び出し更新 → retry.go → process.go → README。

定数（仕様から継承）:

```go
const LogLLMGatewayErrorResponse = "llm gateway error response"
```

HTTP 本文（変更禁止）:

```text
failed to resolve API key from vault
model not found: ` + req.Model
```

`IsNonRetryableError` 追加ニードル（小文字比較）:

```text
failed to resolve api key from vault
vault_error
unexpected status 404
unexpected status 401
unexpected status 403
```

現行 Wait 失敗（変更前）:

```go
errMsg := strings.TrimSpace(stderrBuf.String())
if errMsg == "" {
    errMsg = err.Error()
}
```

ParseExecEvent の `case "error"` で `IsRetryableUpstream` なら nil（変更しない）。high demand は EventError にならない。

### テスト

#### [NEW] [shared/libs/go/llmgateway/handlerctx/context_test.go](file://shared/libs/go/llmgateway/handlerctx/context_test.go)
*   **Description**: WriteErrorResponse が ERROR ログし、JSON schema は現行どおり。
*   **Logic**: captureLogger を渡し `status=404` `code=model_not_found`。logger nil では panic せず JSON のみ。

#### [MODIFY] [shared/libs/go/codingagent/retry_test.go](file://shared/libs/go/codingagent/retry_test.go)
*   **Description**: R3 のケースをテーブルに追加。
*   **Logic**: `failed to resolve API key from vault` / `vault_error` / `unexpected status 404 Not Found: model not found: gpt-4o` / `unexpected status 401` / `unexpected status 403` は true。`unexpected status 500 Internal Server Error` と `exit status 1` は false。

#### [MODIFY] [shared/libs/go/codingagent/codex/process_repro_test.go](file://shared/libs/go/codingagent/codex/process_repro_test.go)
*   **Description**: `TestStartProcess_EmptyStderrUsesStdoutError`（統合フィルタ名 `TestCodexEmptyStderrUsesStdoutError` は tests/ 側のエイリアス）。
*   **Logic**: testfake `Lines: []string{\`{"type":"error","message":"unexpected status 404 Not Found: model not found: gpt-4o"}\`}`、Stderr 空、ExitCode 1。終端 EventError.Content は 404 本文を含む。Retryable false。`TestStartProcess_GenericExit1IsRetryable` は Lines 無しのまま exit status 1。

#### [NEW] [shared/libs/go/llmgateway/openai/handler_error_log_test.go](file://shared/libs/go/llmgateway/openai/handler_error_log_test.go)
*   **Description**: HandleResponses の 404 と vault 500。
*   **Logic**: stub HandlerContext（Router / Vault / Logger / Config）。未知モデル POST `{"model":"no-such-model"}` → 404、ERROR `code=model_not_found`、`via bifrost` 無し。登録モデル + Vault.Resolve エラー → 500、`message=failed to resolve API key from vault`、`code=vault_error`。

#### [NEW] [tests/llm_gateway_error_log_test.go](file://tests/llm_gateway_error_log_test.go)
*   **Description**: 統合 `TestGatewayErrorResponseLog`。実サーバの Gateway に未知モデルを POST。
*   **Logic**: 既存 `startE2EServer`。`POST http://127.0.0.1:{gwPort}/v1/responses` body `{"model":"no-such-e2e-model"}`。404 JSON `code=model_not_found`。`t.Skip` 禁止。

### handlerctx

#### [MODIFY] [shared/libs/go/llmgateway/handlerctx/context.go](file://shared/libs/go/llmgateway/handlerctx/context.go)
*   **Technical Design**:

```go
const LogLLMGatewayErrorResponse = "llm gateway error response"

func WriteErrorResponse(w http.ResponseWriter, err *GatewayError, log logger.Logger, kv ...any) {
    status := err.Status
    if status == 0 {
        status = http.StatusInternalServerError
    }
    if log != nil && err != nil {
        fields := []any{
            "status", status,
            "code", err.Code,
            "type", err.Type,
            "message", err.Message,
        }
        fields = append(fields, kv...)
        log.Error(LogLLMGatewayErrorResponse, fields...)
    }
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    json.NewEncoder(w).Encode(errorResponse{Error: errorBody{Type: err.Type, Message: err.Message, Code: err.Code}})
}
```

#### [MODIFY] 全 `handlerctx.WriteErrorResponse` 呼び出し
openai/handler.go、openai/embeddings.go、anthropic/handler.go、llmgateway/test_helpers_test.go: `ctx.Logger()` と `"path", r.URL.Path, "method", r.Method, "model", model`（未パースなら model 空文字）。
llmgateway/errors.go:

```go
func WriteErrorResponse(w http.ResponseWriter, err *GatewayError) {
    handlerctx.WriteErrorResponse(w, err, nil)
}
```

proxy.go の `WriteErrorResponse` は `p.logger` を渡すよう `handlerctx.WriteErrorResponse(w, err, p.logger, "path", r.URL.Path, "method", r.Method)` に変更。errors.go の 2 引数ラッパは既存 errors_test 用に残す。

### Codex / retry

#### [MODIFY] [shared/libs/go/codingagent/retry.go](file://shared/libs/go/codingagent/retry.go)
needles に仕様の 5 文字列を追加。`unexpected status 500` は入れない。

#### [MODIFY] [shared/libs/go/codingagent/codex/process.go](file://shared/libs/go/codingagent/codex/process.go)

```go
lastErrContent := ""
// in stdout loop after ParseExecEvent:
if ev != nil && ev.Type == codingagent.EventError && ev.Content != "" {
    lastErrContent = ev.Content
}
// Wait:
errMsg := strings.TrimSpace(stderrBuf.String())
if errMsg == "" {
    errMsg = lastErrContent
}
if errMsg == "" {
    errMsg = err.Error()
}
```

### ドキュメント

#### [MODIFY] [README.md](file://README.md)
LIVE / merge gate 付近に: env vault は `vault://providers/openai/default` → `TERN_VAULT_OPENAI_DEFAULT`（`OPENAI_API_KEY` ではない）。Gateway はルーティング成功かつ vault 成功のあと Info `openai responses request via bifrost`。失敗は ERROR `llm gateway error response`。Debug `openai responses request received` だけでは成功ではない。

## Step-by-Step Implementation Guide

1.  [x] **retry テスト**: `retry_test.go` に R3 ケース。
2.  [x] **handlerctx テスト**: logger 付き 404。
3.  [x] **process テスト**: EmptyStderrUsesStdoutError。
4.  [x] **openai テスト**: 404 と vault 500。
5.  [x] **実装**: WriteErrorResponse シグネチャと全呼び出し。
6.  [x] **実装**: retry needles と process lastErrContent。
7.  [x] **統合**: `tests/llm_gateway_error_log_test.go`。
8.  [x] **README**。
9.  [x] **検証**: 下のコマンド。`process_retry` / 15s 既存テストが残っていることを確認。

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**: `./scripts/process/build.sh`
2.  **Integration**: `./scripts/process/integration_test.sh --specify "TestGatewayErrorResponseLog"`
3.  **E2E**: 上記が Gateway HTTP。Codex stdout 終端は単体 `TestStartProcess_EmptyStderrUsesStdoutError`（build.sh）。
4.  **退行（任意）**: `./scripts/process/integration_test.sh --specify "TestLiveCodex_SingleCardReady"`

Windows:

```bash
./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestGatewayErrorResponseLog"
```

Linux / Remote-SSH（Linux）:

```bash
./scripts/process/build.sh --skip-etc && xvfb-run -a ./scripts/process/integration_test.sh --specify "TestGatewayErrorResponseLog"
```

`--categories` は付けない。kanban は実行しない。

## Documentation

README の vault env と `received` / `via bifrost` / ERROR の段落。ReferenceManual の Send schema は変えない。
