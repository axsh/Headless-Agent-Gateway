# 000-Codex-Sandbox-Rejection-ToolResult

> **Source Specification**: [ideas/000-Codex-Sandbox-Rejection-ToolResult.md](../ideas/000-Codex-Sandbox-Rejection-ToolResult.md)

## Goal Description

Codex CLI がサンドボックスでコマンドを拒否し stdout `item.completed` が来ない場合でも、Tern codex アダプタが `EventToolResult` を合成して relay に載せ、retryable `EventError` だけが残る経路を閉じる（Issue #51 Phase 1）。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1 `IsSandboxRejection` パターン | `retry.go` + `retry_test.go` |
| R2 pending `command_execution` 追跡 | `process.go` > `commandExecTracker` |
| R3 exit 時 `EventToolResult` 合成 | `process.go` > `maybeSynthesizeSandboxToolResult` |
| R4 合成後 retryable `EventError` 抑制 | `process.go` > stdout goroutine exit path |
| R5 `wait: no child`（任意） | 本計画では実装しない（仕様どおり） |
| 旧 R6/R7 対象外 | 計画・実装に含めない |

## Proposed Changes

### codingagent (shared)

#### [MODIFY] [shared/libs/go/codingagent/retry_test.go](file://shared/libs/go/codingagent/retry_test.go)
* **Description**: `IsSandboxRejection` のテーブル駆動テスト（TDD: 先に追加し RED）。
* **Logic**:
  - `Rejected("rm -f...")` → true
  - `rm -f style commands are not permitted` → true
  - `exec_command failed` + `Rejected` 同一文字列 → true
  - `exit status 1` 単体 → false
  - `unexpected status 500` → false

#### [MODIFY] [shared/libs/go/codingagent/retry.go](file://shared/libs/go/codingagent/retry.go)
* **Description**: サンドボックス拒否検出関数を追加。
* **Technical Design**:
```go
// IsSandboxRejection reports Codex sandbox/policy rejection in stderr or log text.
func IsSandboxRejection(msg string) bool
```
* **Logic**:
  - `msg == ""` → false
  - `lower := strings.ToLower(msg)`
  - `strings.Contains(lower, "rejected(")` → true
  - `strings.Contains(lower, "rm -f style commands are not permitted")` → true
  - `strings.Contains(lower, "exec_command failed") && strings.Contains(lower, "rejected")` → true
  - それ以外（`exit status 1` 単体含む）→ false

### codex adapter

#### [NEW] [shared/libs/go/codingagent/codex/rejection.go](file://shared/libs/go/codingagent/codex/rejection.go)
* **Description**: stderr バッファから拒否テキストを抽出するヘルパ。
* **Technical Design**:
```go
// ExtractSandboxRejectionContent returns the best rejection text from stderr buffer.
// Prefers a line containing Rejected(, else trimmed full buffer when IsSandboxRejection.
func ExtractSandboxRejectionContent(stderr string) string
```

#### [MODIFY] [shared/libs/go/codingagent/codex/process_repro_test.go](file://shared/libs/go/codingagent/codex/process_repro_test.go)
* **Description**: sandbox 拒否の testfake 再現テスト（TDD: RED → GREEN）。
* **Tests**:
  - `TestStartProcess_SandboxRejectionSynthesizesToolResult`
  - `TestStartProcess_SandboxRejectionNoDuplicateToolResult`

#### [MODIFY] [shared/libs/go/codingagent/codex/process.go](file://shared/libs/go/codingagent/codex/process.go)
* **Description**: pending 追跡と exit 時合成、終了時 EventError 抑制。
* **Technical Design** — stdout goroutine 内の追跡構造:
```go
type commandExecTracker struct {
    mu              sync.Mutex
    pending         bool   // last command_execution started without matching tool result
    toolResultSent  bool   // stdout or synthesis already delivered tool result for pending cmd
    synthesizedReject bool // sandbox tool_result synthesized this turn
}
```
* **Logic**:
  - `markToolUse(ev)` when `ev.Type == EventToolUse && ev.ToolName == "command_execution"`
  - `markToolResult()` when `ev.Type == EventToolResult`（stdout 由来）
  - stdout ループ終了後、`<-stderrDone` のあと `cmd.Wait()` の **前** に:
    - `stderrText := strings.TrimSpace(stderrBuf.String())`
    - `tracker.pending && !tracker.toolResultSent && codingagent.IsSandboxRejection(stderrText)` のとき:
      - `content := ExtractSandboxRejectionContent(stderrText)`
      - `content = codingagent.TruncateToolResult(content, cfg.MaxToolResultBytes)`
      - ch に `EventToolResult{Content: content}` を送る
      - `tracker.synthesizedReject = true`, `markToolResult()`
  - `cmd.Wait()` エラー時:
    - `tracker.synthesizedReject == true` → **EventError を送らない**（R4 推奨）
    - 既存分類は `!IsNonRetryableError` のまま維持

## Step-by-Step Implementation Guide

- [x] 1. **Tests RED**: `retry_test.go` に `TestIsSandboxRejection` を追加。
- [x] 2. **Implement R1**: `retry.go` に `IsSandboxRejection`。
- [x] 3. **Tests RED**: `process_repro_test.go` に sandbox 拒否テスト 2 件。
- [x] 4. **NEW** `rejection.go` に `ExtractSandboxRejectionContent`。
- [x] 5. **MODIFY** `process.go` に tracker + 合成 + EventError 抑制。
- [x] 6. **GREEN**: `./scripts/process/build.sh`。
- [x] 7. 計画チェックボックスを `[x]` に更新。

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**: `./scripts/process/build.sh`
2. 本 Phase では integration テスト追加なし（Phase 3）。

## Documentation

Phase 1 ではドキュメント変更なし（Phase 3 R6）。
