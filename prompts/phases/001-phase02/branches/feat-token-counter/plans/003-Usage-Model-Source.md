# 003-Usage-Model-Source

> **Source Specification**: [ideas/003-Usage-Model-Source.md](file://prompts/phases/001-phase02/branches/feat-token-counter/ideas/003-Usage-Model-Source.md)

## Goal Description

`TokenUsage` に `model_source`（`agent` | `tern_session`）を追加。agentservice 層でエージェント共通のセッション model 補完を行い、Claude/Codex いずれもテレメトリに model が無い場合は `tern_session` にフォールバックする。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point |
| :--- | :--- |
| R1 `model_source` フィールド | `codingagent/usage.go`, `client/v1/usage.go` |
| R2 優先順位（agent → tern_session → 省略） | `codingagent/usage_model.go`, `agentservice/handler_usage.go` |
| R3 エージェント非依存補完 | `applyModelAttribution`, `activeExecution.sessionModel` |
| R4 Client ドキュメント | `docs/ReferenceManual-WebAPIs.md` |
| R5 後方互換読み込み | `usage_store.go` load 時推定（任意） |
| T1 Claude parser agent | `claudecode/protocol.go` + test |
| T2/T2b agentservice backfill | `handler_usage_test.go` |
| T3 SumTurnUsage 集計行 | `usage_test.go` |
| T4/T5 E2E | `tests/token_usage_e2e_test.go` |

## Proposed Changes

### codingagent

#### [MODIFY] [usage_test.go](file://shared/libs/go/codingagent/usage_test.go)（先にテスト）
*   **Logic**: `TestApplyModelAttribution_*`, `TestSumTurnUsage_OmitsModelFields`.

#### [NEW] [usage_model.go](file://shared/libs/go/codingagent/usage_model.go)
*   **Technical Design**:
```go
const (
    ModelSourceAgent       = "agent"
    ModelSourceTernSession = "tern_session"
)

// ApplyModelAttribution sets ModelSource and optionally backfills Model from sessionModel.
// Priority: non-empty Model -> agent (if ModelSource unset); empty Model + sessionModel -> tern_session.
func ApplyModelAttribution(u *TokenUsage, sessionModel string)
```
*   **Logic**:
```go
func ApplyModelAttribution(u *TokenUsage, sessionModel string) {
    if u == nil { return }
    if u.Model != "" {
        if u.ModelSource == "" {
            u.ModelSource = ModelSourceAgent
        }
        return
    }
    if sessionModel != "" {
        u.Model = sessionModel
        u.ModelSource = ModelSourceTernSession
    }
}
```

#### [MODIFY] [usage.go](file://shared/libs/go/codingagent/usage.go)
*   **Technical Design**: add `ModelSource string `json:"model_source,omitempty"`` to `TokenUsage`.

### claudecode

#### [MODIFY] [protocol_test.go](file://shared/libs/go/codingagent/claudecode/protocol_test.go)（先にテスト）
*   **Logic**: `TestParseJSONLinesEvent_Result_ModelUsageFallback` asserts `ModelSource == agent`.

#### [MODIFY] [protocol.go](file://shared/libs/go/codingagent/claudecode/protocol.go)
*   **Logic**: in `parseClaudeResultUsage`, when `u.Model != ""` after modelUsage loop, set `u.ModelSource = codingagent.ModelSourceAgent`.

### agentservice

#### [MODIFY] [exec_registry.go](file://shared/libs/go/agentservice/exec_registry.go)
*   **Technical Design**: `activeExecution` add `sessionModel string`.

#### [MODIFY] [handler_retry.go](file://shared/libs/go/agentservice/handler_retry.go)
*   **Logic**: when creating `activeExecution`, set `sessionModel: record.Model` from session record at Send start.

#### [MODIFY] [handler_usage_test.go](file://shared/libs/go/agentservice/handler_usage_test.go)（先にテスト）
*   **Logic**:
  - `TestHandleGetSessionUsage_ModelSourceAgent` — existing mock with modelUsage path.
  - `TestHandleSendMessage_ModelSourceTernSession` — mock agent emits result usage without model; assert `tern_session` + session model.

#### [MODIFY] [handler_usage.go](file://shared/libs/go/agentservice/handler_usage.go)
*   **Logic**:
```go
// before persist and before ev.Usage wire:
codingagent.ApplyModelAttribution(&rec.Usage, exec.sessionModel)
for i := range rec.Calls {
    codingagent.ApplyModelAttribution(&rec.Calls[i], exec.sessionModel)
}
```
*   Change `persistTurnUsage` call to pass `exec` or `sessionModel`.

### client/v1

#### [MODIFY] [usage.go](file://client/v1/usage.go)
*   **Technical Design**: add `ModelSource string` + constants mirror.

#### [MODIFY] [usage_test.go](file://client/v1/usage_test.go)
*   **Logic**: decode JSON with `model_source`.

### tests

#### [MODIFY] [token_usage_e2e_test.go](file://tests/token_usage_e2e_test.go)
*   **Logic**: `TestClaudeCodeE2E_TokenUsage_TurnAndSession` assert `rep.Turns[0].Usage.ModelSource == "agent"` when model present.

### docs

#### [MODIFY] [ReferenceManual-WebAPIs.md](file://docs/ReferenceManual-WebAPIs.md)
*   **Logic**: document `model`, `model_source` on usage objects.

## Step-by-Step Implementation Guide

- [x] 1. TDD: `usage_model_test.go` + `ApplyModelAttribution` in `usage_model.go`; add field to `TokenUsage`.
- [x] 2. Claude parser: set `ModelSource=agent` when model set; update protocol test.
- [x] 3. `activeExecution.sessionModel`; wire in `handler_retry.go`.
- [x] 4. TDD: `handler_usage_test` for tern_session backfill mock.
- [x] 5. `applyUsageSideEffects`: call `ApplyModelAttribution` before wire/persist.
- [x] 6. Client `usage.go` + test; docs; E2E assert.
- [x] 7. `./scripts/process/build.sh` && `./scripts/process/integration_test.sh --specify "TokenUsage"`

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**: `./scripts/process/build.sh`
2. **Integration Tests**: `./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TokenUsage"`

### E2E Tests

- Extend `TestClaudeCodeE2E_TokenUsage_TurnAndSession` with `model_source` assert.
- Optional: mock-based unit test for Claude-no-model → `tern_session` (T2b).

## Documentation

- `docs/ReferenceManual-WebAPIs.md`
