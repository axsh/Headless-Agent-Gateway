# 000-Live-E2E-Tool-Heartbeat-And-Turn-Cancel

> **Source Specification**: `prompts/phases/001-phase02/branches/feat-tool-heartbeat-and-turn-cancel/ideas/000-Live-E2E-Tool-Heartbeat-And-Turn-Cancel.md`

## Goal Description

PR #58（tool SSE heartbeat + turn cancel）に対し、実 Codex / Claude Code CLI を使うライブ E2E を `tests/` に追加し、`integration_test.sh --specify` で PASS まで確認する。プロダクト本体の追加機能は想定しない（テストと検証ヘルパのみ）。

## User Review Required

None. 仕様で確定済みの必須ゲート（Codex heartbeat / Codex cancel+resume / Claude 同型1本 / Terminate 対比）を実装する。任意要件 O1–O3 は本計画では実装しない。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| M1 ライブファイル・プレフィックス・Skip禁止 | Proposed Changes > NEW `tests/tool_heartbeat_cancel_live_test.go` |
| M2 Tool heartbeat（Codex・間隔短縮・progress） | `TestLiveToolHeartbeat_Codex` + `startHeartbeatCancelLiveServer` |
| M3 Turn cancel + 同一 id 再開 | `TestLiveTurnCancel_CodexResume` |
| M4 Terminate 対比 | `TestLiveTurnCancel_TerminateClosesSession` |
| M5 ヘルパ再利用・sandbox・専用 start | `startHeartbeatCancelLiveServer`（`SSE_TOOL_HEARTBEAT_INTERVAL=1s`、`disable_sandbox: true`）+ 既存 create/send/parse |
| M6 Claude Code 同型1本以上 | `TestLiveToolHeartbeat_ClaudeCode` |
| M7 build + `--specify` PASS 記録 | Verification Plan |
| O1–O3 | Out of scope（本計画では未実施） |

## Proposed Changes

### Live E2E tests

#### [NEW] [tests/tool_heartbeat_cancel_live_test.go](file://tests/tool_heartbeat_cancel_live_test.go)

*   **Description**: ハートビート・キャンセル・Terminate 対比のライブ E2E。パッケージ `llm_test`。
*   **Technical Design**:

定数:

```go
const (
	liveHeartbeatIntervalEnv = "SSE_TOOL_HEARTBEAT_INTERVAL"
	liveHeartbeatInterval    = "1s"
	liveCodexHeartbeatModel  = "gpt-4o" // align with liveCodexModel / Codex E2E
	liveToolStillRunning     = "tool_still_running"
)
```

ヘルパ（同ファイル）:

```go
// requireLiveCodexCLI Fatals if codex is not on PATH (no Skip).
func requireLiveCodexCLI(t *testing.T)

// requireLiveClaudeCLI Fatals if claude is not on PATH (no Skip).
func requireLiveClaudeCLI(t *testing.T)

// startHeartbeatCancelLiveServer sets SSE_TOOL_HEARTBEAT_INTERVAL=1s via t.Setenv,
// writes config with disable_sandbox: true (same shape as startCodexE2EServer),
// Launch server, returns AgentService base URL + cleanup. Does not Skip.
func startHeartbeatCancelLiveServer(t *testing.T) (baseURL string, cleanup func())

// sleepToolPrompt returns an explicit prompt forcing a shell sleep of at least nSeconds.
// Mentions Unix `sleep N` and Windows `timeout /t N /nobreak`.
func sleepToolPrompt(nSeconds int) string

// shortResumePrompt is "Reply with exactly: OK" (no tools).
func shortResumePrompt() string

// getE2ESessionMap GETs /api/v1/sessions/:id into map[string]any (reuse getE2ESession if already present).
func waitSessionFollowable(t *testing.T, baseURL, sessionID string, timeout time.Duration)

// postCancelTurn POSTs /api/v1/sessions/:id/cancel; asserts 200 and body contains "cancelled".
func postCancelTurn(t *testing.T, baseURL, sessionID string)

// postTerminateSession POSTs .../terminate; asserts 200.
func postTerminateSession(t *testing.T, baseURL, sessionID string)

// startMessageSSEAsync starts POST /messages in a goroutine; returns resp channel / err channel
// or a small struct { resp *http.Response; err error; done <-chan struct{} } after headers,
// plus a drain goroutine that reads until EOF. Used so cancel can run while SSE is open.
```

テスト関数:

| 関数名 | 内容 |
| :--- | :--- |
| `TestLiveToolHeartbeat_Codex` | Codex + sleep≥8s。SSE に `progress`/`tool_still_running`/非空 `tool_name`。終端+`[DONE]`。全体≤120s |
| `TestLiveToolHeartbeat_ClaudeCode` | 同上・`claudecode` + `e2eDefaultModel`。対称アサーション |
| `TestLiveTurnCancel_CodexResume` | sleep≥60 開始 → followable → cancel → status≠closed かつ error=`turn cancelled` → SSE終了≤30s → 同一idで short prompt、≠409/404、完了 |
| `TestLiveTurnCancel_TerminateClosesSession` | セッション作成 → terminate → status=`closed`（長ツール不要） |

*   **Logic**:

**Heartbeat（Codex / Claude 共通）**:

1. `requireLive*CLI`
2. `startHeartbeatCancelLiveServer`（`t.Setenv("SSE_TOOL_HEARTBEAT_INTERVAL", "1s")` を Launch 前に実行。`resolvedToolHeartbeatInterval` が毎ターン env を読む）
3. `createE2ESessionWithModel(t, baseURL, agent, model, workDir)`
4. `sendE2EMessage(..., sleepToolPrompt(8), 120*time.Second)`
5. `parseE2ESSEEvents` → 少なくとも1件 `Type==EventProgress && Content=="tool_still_running" && ToolName!=""`
6. 終端: いずれかの event が `EventResult` または `EventError`、かつ `gotDone==true`
7. モデルがツールを起動せず heartbeat が無い場合は **Fail**（リトライは最大1回まで: 同一テスト内で1回だけプロンプト再送して再判定。2回目も無ければ Fatal）

**Cancel + resume（Codex）**:

1. サーバ起動後セッション作成
2. 非同期で `POST /messages` + `sleepToolPrompt(60)`（`http.Client` に全体 Timeout 120s。ボディ読みは別 goroutine）
3. `waitSessionFollowable`（最大 60s、`followable==true`）
4. `postCancelTurn`
5. `GET` session: `id` 同一、`status != "closed"`、`status == "error"`、`error == "turn cancelled"`（JSON の error フィールド）
6. SSE drain 完了を cancel 後 30s 以内に待つ
7. `sendE2EMessage(..., shortResumePrompt(), 90*time.Second)` → status ≠ 409/404、SSE が終端

**Terminate**:

1. サーバ + セッション作成（Codex）
2. `postTerminateSession`
3. GET status == `closed`

既存 `startCodexE2EServer` の `t.Skip` は流用しない（仕様 M1）。

### Documentation

#### [MODIFY] [docs/ReferenceManual-WebAPIs.md](file://docs/ReferenceManual-WebAPIs.md)（任意・最小）

*   **Description**: Cancel / Tool liveness 節に「ライブ検証: `TestLiveToolHeartbeat_*` / `TestLiveTurnCancel_*`」を1行追記してよい。必須ではない。実装時に追記する場合のみ。

## Step-by-Step Implementation Guide

1.  **[x] Failed-first スケルトン**: [tests/tool_heartbeat_cancel_live_test.go](file://tests/tool_heartbeat_cancel_live_test.go) に4テストとヘルパの骨格を追加する（アサーション含む）。
2.  **[x] ヘルパ完成**: `startHeartbeatCancelLiveServer`（env + config）、`sleepToolPrompt`、`waitSessionFollowable`、`postCancelTurn`、非同期 SSE 開始を実装する。
3.  **[x] Heartbeat Codex / Claude**: `TestLiveToolHeartbeat_Codex` と `TestLiveToolHeartbeat_ClaudeCode` を完成させる。
4.  **[x] Cancel resume + Terminate**: `TestLiveTurnCancel_CodexResume` と `TestLiveTurnCancel_TerminateClosesSession` を完成させる。
5.  **[x] Build**: `./scripts/process/build.sh`（Windows）。失敗時は修正して再実行。
6.  **[x] Live gate**: `./scripts/process/integration_test.sh --specify "TestLiveToolHeartbeat_|TestLiveTurnCancel_"`。失敗時は Fix Loop（プロンプト強化・タイムアウト・実装バグ修正）。Skip 禁止。
7.  **[x] 検証記録**: 本計画の Verification チェックを `[x]` にし、実行結果を下欄に書く。
8.  **[/] Commit / Push**: 意味単位で commit し、全 PASS 後に `git push`。

## Verification Plan

> `integration_test.sh` に `--categories` は無い。付けない。

### Automated Verification

1.  **[x] Build & Unit Tests**: `./scripts/process/build.sh`
2.  **[x] Live E2E (必須ゲート)**: `./scripts/process/integration_test.sh --specify "TestLiveToolHeartbeat_|TestLiveTurnCancel_"`
3.  **[x] E2E Tests (コード)**: `tests/tool_heartbeat_cancel_live_test.go` に以下が存在する:
    - `TestLiveToolHeartbeat_Codex`
    - `TestLiveToolHeartbeat_ClaudeCode`
    - `TestLiveTurnCancel_CodexResume`
    - `TestLiveTurnCancel_TerminateClosesSession`

### Linux / Remote-SSH（参考）

```bash
./scripts/process/build.sh --skip-etc
xvfb-run -a ./scripts/process/integration_test.sh --specify "TestLiveToolHeartbeat_|TestLiveTurnCancel_"
```

### Verification Record

| 日時 | コマンド | 結果 |
| :--- | :--- | :--- |
| 2026-08-31 | `./scripts/process/build.sh` | PASS (129s) |
| 2026-08-31 | `./scripts/process/integration_test.sh --specify "TestLiveToolHeartbeat_|TestLiveTurnCancel_"` | PASS (61s): Heartbeat Codex 14s / Claude 31s / CancelResume 12s / Terminate 0.7s |
| （補足） | cancel 後 SSE 残存対策で `handleCancel` が `relay.markSourceDone()` を呼ぶよう修正 | ライブで確認済み |

## Documentation

- ReferenceManual への1行追記は任意。
- prompts 配下の本計画・仕様のチェックボックス更新は実装完了時に行う。
