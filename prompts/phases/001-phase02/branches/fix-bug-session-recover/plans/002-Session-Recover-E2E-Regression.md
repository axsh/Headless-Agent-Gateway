# 002-Session-Recover-E2E-Regression

> **Source Specification**: [ideas/002-Session-Recover-E2E-Regression.md](../ideas/002-Session-Recover-E2E-Regression.md)

## Goal Description

Issue #51 の AC を統合テスト・ライブ E2E・API ドキュメントで固定する。

## Phase 1/2 影響確認

| 先行実装 | Phase 3 への影響 |
| :--- | :--- |
| codex `EventToolResult` 合成 | testfake ライブで stdout 無し stderr 拒否を再現可能 |
| agentservice terminal 合成 | tool_result のみ終了時に SSE `error` + `[DONE]` |
| Follow 切断再開 | `common_session_follow_test` パターンを recover agent に適用 |

## User Review Required

None.

## Requirement Traceability

| Requirement | Implementation |
| :--- | :--- |
| R1 モック Follow | `tests/session_recover_follow_test.go` |
| R2 testfake 統合 | `tests/codex_session_recover_test.go` |
| R3 回帰 | 既存 `TestSessionFollow_*` |
| R5 ライブ E2E | `tests/session_recover_live_test.go`（`t.Skip` 禁止） |
| R6 ドキュメント | `docs/ReferenceManual-WebAPIs.md` |

## Step-by-Step Implementation Guide

- [x] 1. `session_recover_follow_test.go`（recover agent + FollowFrom）
- [x] 2. `codex_session_recover_test.go`（testfake + agentservice）
- [x] 3. `session_recover_live_test.go`（require codex, Fatal if missing）
- [x] 4. `ReferenceManual-WebAPIs.md` 追記
- [x] 5. `integration_test.sh --specify "TestSessionRecover_Follow|TestSessionRecover_Codex"`
- [x] 6. `integration_test.sh --specify "TestSessionRecoverLive"`（実 codex + sandbox enforced。stderr/text 拒否から tool_result 合成を確認）

## Verification Plan

```bash
./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestSessionRecover_Follow|TestSessionRecover_Codex"
./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestSessionRecoverLive"
```

## Documentation

`docs/ReferenceManual-WebAPIs.md` §8.2 付近に tool 拒否・終端契約を追記。
