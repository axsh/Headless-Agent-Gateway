# 001-Agentservice-SSE-Terminal-Guarantee

> **Source Specification**: [ideas/001-Agentservice-SSE-Terminal-Guarantee.md](../ideas/001-Agentservice-SSE-Terminal-Guarantee.md)

## Goal Description

Phase 1 後、relay が `tool_result` のみで終了し terminal が空になる経路に対し、`attachSSE` が合成 `EventError` を SSE に書き、Follow が `[DONE]` まで届くよう終端契約を保証する。

## Phase 1 完了時点の影響確認

| Phase 1 の変更 | Phase 2 への影響 |
| :--- | :--- |
| sandbox 拒否で `EventToolResult` 合成 | relay は **terminal 無し** で終了しうる → **R2 合成が必須** |
| sandbox 後 `EventError` 抑制 | retryable 無音経路は縮小。terminal 空 + tool_result のみが主因 |
| `IsSandboxRejection` | agentservice は変更不要 |

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point |
| :--- | :--- |
| R1 tool_result は SSE に書く | 既存 `writeEvent` 維持（変更なし） |
| R2 relay 終了時 terminal 合成 | `handler_retry.go` > `resolveTerminalFromRelayEvents`, `ensureRelayTerminal`, `attachSSE` |
| R3 handleFollow 終端 | `handler_follow.go`（R2 後 term 非空） |
| R4 process retry 整合 | relay 差し替えは既存。terminal 合成は relay snapshot ベース |
| R5 合成 terminal で StatusError | `updateSessionStatusOnTerminal` on synthetic write |
| R6/R7 任意 | 実装しない |

## Proposed Changes

#### [MODIFY] [shared/libs/go/agentservice/handler_retry_unexported_test.go](file://shared/libs/go/agentservice/handler_retry_unexported_test.go)
* `TestResolveTerminalFromRelayEvents_*`, `TestEnsureRelayTerminal_*`（TDD 先）

#### [MODIFY] [shared/libs/go/agentservice/handler_retry.go](file://shared/libs/go/agentservice/handler_retry.go)
* **定数**: `streamEndedWithoutTerminalContent = "stream ended without terminal event"`
* **関数**:
```go
func resolveTerminalFromRelayEvents(events []codingagent.StreamEvent) streamTerminal
func ensureRelayTerminal(term streamTerminal, relay *eventRelay) streamTerminal
```
* **attachSSE**: `terminalWritten` 追跡。`ch` 終了時 `ensureRelayTerminal` + 未書き terminal を SSE 1 回書き込み。

#### [MODIFY] [shared/libs/go/agentservice/handler_follow_test.go](file://shared/libs/go/agentservice/handler_follow_test.go) または新規 `handler_session_recover_test.go`（agentservice_test）
* `TestSessionRecoverTerminal_FollowWritesDone`

## Step-by-Step Implementation Guide

- [x] 1. 単体テスト RED: `resolveTerminalFromRelayEvents` / `ensureRelayTerminal`
- [x] 2. `handler_retry.go` 実装
- [x] 3. httptest: Follow + tool_result only agent（`PostMessagesGetsSyntheticError`）。Follow E2E は Phase 3
- [x] 4. `./scripts/process/build.sh`
- [x] 5. チェックボックス更新

## Verification Plan

1. `./scripts/process/build.sh`

## Documentation

なし（Phase 3 R6）。
