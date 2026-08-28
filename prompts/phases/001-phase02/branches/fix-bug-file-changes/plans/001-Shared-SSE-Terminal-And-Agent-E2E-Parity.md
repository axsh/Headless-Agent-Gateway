# 001-Shared-SSE-Terminal-And-Agent-E2E-Parity

> **Source Specification**: `prompts/phases/001-phase02/branches/fix-bug-file-changes/ideas/001-Shared-SSE-Terminal-And-Agent-E2E-Parity.md`

## Goal Description

AgentService 共通の SSE 終端を堅牢化し（`eventRelay` lost-wakeup 修正）、Claude Code / Codex が同じ E2E 契約（`[DONE]`・workDir 成果物・`completed`・ternctl）をクリアできるようにする。M6（モデル ID）/ M7（停滞 WARN）/ M8（post-result drain）は先行実装済みのため、本計画では残作業の完了と回帰確認を中心とする。

## User Review Required

- **M4 パス**: `claudecode.BuildEnv` には既に Windows 向け `MSYS_NO_PATHCONV` / `SHELL=cmd.exe` がある。残る主因はモデルが `file_path=/tmp/...` を生成すること。本計画は **案 B を必須**（絶対 workDir をプロンプトに明示 + 共通ヘルパで実パス検証）、案 A は既存 env の回帰テスト追加のみとする。共有パスリライトはしない。
- **実行順**: 本計画（001）を 002（Claude Tier1 List）より先に完了させる（002 のライブ List 前提）。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| M1 共有 SSE 終端・エージェント非分岐 | Proposed Changes > exec_registry.go（relay）、既存 handler_retry attachSSE |
| M2 C-Stream / C-Artifact / C-Status / C-Ternctl | Proposed Changes > tests/e2e_parity_helpers_test.go, agentservice_e2e_test.go, codex_e2e_test.go |
| M3 Codex 非回帰 | Verification Plan（TestCodexE2E_）+ BuildEnv 非変更（codex） |
| M4 Claude パス（A 検証 + B 必須） | claudecode BuildEnv テスト + E2E プロンプト／ヘルパ |
| M5 relay 終端単体回帰 | exec_registry_test.go（競合・sourceDone） |
| M6 モデル ID 同期 | 既反映。コミット対象に含める（変更なしなら確認のみ） |
| M7 EventResult 後 WARN | 既反映。回帰（stall ログ） |
| M8 post-result drain 2s | 既反映。回帰（PostResultDrain* テスト） |

## Proposed Changes

### agentservice — eventRelay 終端（M1 / M5）

#### [MODIFY] [shared/libs/go/agentservice/exec_registry_test.go](file://shared/libs/go/agentservice/exec_registry_test.go)

*   **Description**: TDD — lost-wakeup 耐性と sourceDone 後の購読終了を先に書く。
*   **Logic**（テーブル／競合）:
    *   `TestEventRelay_StreamDrainsAfterSourceClose`: 複数イベント送信 → close(source) → `stream(0,false)` が全件受信後に閉じる（既存 `isSourceDone` と併用）。
    *   `TestEventRelay_NoHangWhenDoneRacesWithWait`: ゴルーチン N=100 回。購読側が「バッファ空・まだ done でない」状態で wait に入る直前に source を閉じる。**必ず 1s 以内に stream が閉じる**（ハングしたら Fail）。
    *   `TestEventRelay_StopOnUserInputStillCloses`: `EventUserInputRequired` で stream が閉じ、残イベントを読まない（現行契約維持）。

#### [MODIFY] [shared/libs/go/agentservice/exec_registry.go](file://shared/libs/go/agentservice/exec_registry.go)

*   **Description**: `notify` の buffer-1 + send-drop による lost-wakeup を解消する。
*   **Technical Design**:

```go
type eventRelay struct {
    mu         sync.Mutex
    events     []codingagent.StreamEvent
    notify     chan struct{} // capacity 1; event wakeups (may drop)
    doneCh     chan struct{} // closed exactly once when source exhausts
    sourceDone bool
    doneOnce   sync.Once
}

func newEventRelay(source <-chan codingagent.StreamEvent) *eventRelay {
    r := &eventRelay{
        notify: make(chan struct{}, 1),
        doneCh: make(chan struct{}),
    }
    go func() {
        for ev := range source {
            r.mu.Lock()
            r.events = append(r.events, ev)
            r.mu.Unlock()
            select {
            case r.notify <- struct{}{}:
            default:
            }
        }
        r.mu.Lock()
        r.sourceDone = true
        r.mu.Unlock()
        r.doneOnce.Do(func() { close(r.doneCh) })
        select {
        case r.notify <- struct{}{}:
        default:
        }
    }()
    return r
}

// stream wait loop (conceptual): after draining buffered events,
// if sourceDone { return }; else select { case <-r.notify:; case <-r.doneCh: }
```

*   **Logic**:
    *   `doneCh` は source 終了時に一度だけ close。購読側は `notify` を落としても `doneCh` で必ず起きる。
    *   `isSourceDone()` は現行どおり `sourceDone` を返す（M7 診断用）。
    *   公開 API は増やさない（`With*` 不要）。

### agentservice — 先行実装の回帰固定（M7 / M8）

#### [MODIFY] [shared/libs/go/agentservice/handler_retry_test.go](file://shared/libs/go/agentservice/handler_retry_test.go)

*   **Description**: 既存 `TestHandleSendMessage_PostResultDrain*` を維持。必要なら stall WARN の単体を追加（drain を長くし stall を短くオーバーライドできる場合のみ。現状 stall は定数 5s のため、**本番定数のままでログ文字列の存在をドキュメント化し、追加テストは任意**）。
*   **Logic**: drain ハング防止・trailing イベントは既存テストでカバー済み → 本ステップでは失敗したら修正するだけ。

### claudecode — Windows env 回帰（M4-A）

#### [MODIFY] [shared/libs/go/codingagent/claudecode/process_test.go](file://shared/libs/go/codingagent/claudecode/process_test.go)（なければ NEW）

*   **Description**: `BuildEnv` が Windows で `MSYS_NO_PATHCONV=1`、`SHELL`/`COMSPEC` が cmd.exe を指すことを固定。
*   **Logic**: `runtime.GOOS == "windows"` のときのみアサート。非 Windows では Skip。Codex の `BuildEnv` は変更しない。

### tests — 共通 E2E 契約ヘルパ（M2 / M4-B）

#### [NEW] [tests/e2e_agent_parity_helpers_test.go](file://tests/e2e_agent_parity_helpers_test.go)

*   **Description**: Claude / Codex 共通のアサーション（パッケージ `llm_test`）。
*   **Technical Design**:

```go
// assertSSEDoneRequires [DONE] in parsed SSE (gotDone bool).
func assertParitySSEDone(t *testing.T, gotDone bool)

// assertParitySessionCompleted GETs session and requires status == "completed".
func assertParitySessionCompleted(t *testing.T, baseURL, sessionID string)

// fileCreatePrompt returns a prompt that names an absolute path under workDir.
// Example: Create file at "<abs>/hello.txt" with exact contents '...'. Prefer Write/file tools; do not use /tmp.
func fileCreatePrompt(workDir, fileName, contents string) string

// assertParityWorkFileExists checks workDir/fileName OR any path recorded in tool_use ToolInput / tool_result
// that resolves to the same base name under workDir (shared rule for both agents).
func assertParityWorkFileExists(t *testing.T, workDir, fileName string, events []codingagent.StreamEvent)
```

*   **Logic**:
    *   プロンプトに `filepath.Abs(workDir)` を埋め込み、`/tmp` 禁止を明示（M4-B）。
    *   ファイル検証: (1) `filepath.Join(workDir, fileName)` が存在すれば PASS。(2) なければ events から `file_path`/`path` を集め、`filepath.Base` が一致し、かつその絶対パスが読める、または workDir 配下に同名があれば PASS。(3) どちらも無ければ FAIL（Claude だけ Skip 禁止）。

#### [MODIFY] [tests/agentservice_e2e_test.go](file://tests/agentservice_e2e_test.go)

*   **Description**: `TestE2E_CodingAgentStreaming` / `DefaultModel` / ternctl 系をヘルパ利用に寄せる。`e2eDefaultModel` は既に `claude-sonnet-4-6`。
*   **Logic**:
    *   プロンプトを `fileCreatePrompt` に置換。
    *   `[DONE]` / ファイル存在を共通ヘルパで検証。
    *   ternctl: 出力に `Session created:` と完了 JSON、または共通成功判定。exit 1 時は上流エラー以外 Fail（既存 Skip 条件は Codex 側と同型の文言に限る）。

#### [MODIFY] [tests/codex_e2e_test.go](file://tests/codex_e2e_test.go)

*   **Description**: ファイル作成系 Codex E2E で同じ `fileCreatePrompt` / `assertParity*` を使う（C-Stream / C-Artifact / C-Status）。
*   **Logic**: Codex 専用の assertion を削除せず、共通ヘルパを追加呼び出し。挙動変更はプロンプトの絶対パス明示程度に留める。

### 設定・ドキュメント（M6）

#### [MODIFY] 確認のみ

*   `tests/testdata/model_profiles.yaml` — `default_profile.model: claude-sonnet-4-6`
*   `settings/demo/model_profiles.yaml` / `settings/example/model_profiles.yaml` / `README.md`
*   差分が無ければ計画チェックを `[x]` にしてコミット対象から外す。

## Step-by-Step Implementation Guide

1. [x] **TDD relay**: `exec_registry_test.go` に lost-wakeup / drain-after-close テストを追加し、現行実装で Fail またはフレークすることを確認する。
2. [x] **Fix relay**: `exec_registry.go` に `doneCh` + `sync.Once` を導入し、stream 待機を `notify`/`doneCh` の select にする。テストを PASS させる。
3. [x] **Verify M7/M8**: 既存 PostResultDrain テストが PASS のままであること。
4. [x] **BuildEnv テスト**: claudecode Windows env 回帰を追加。
5. [x] **Parity helpers**: `tests/e2e_agent_parity_helpers_test.go` を追加。
6. [x] **Wire Claude E2E**: `agentservice_e2e_test.go` のファイル作成・ternctl をヘルパ化。
7. [x] **Wire Codex E2E**: `codex_e2e_test.go` の該当テストをヘルパ化。
8. [x] **M6 確認**: モデル ID 同期ファイルを確認。
9. [x] **Verification Plan 実行**: 下記コマンドを順に実行し、Claude / Codex ライブ対が両方 PASS（または対称 Skip）であること。

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**: `./scripts/process/build.sh`
2. **Integration — AgentService / SSE 周辺**: `./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify 'TestHandleSendMessage_PostResultDrain|TestEventRelay|TestSSE_'`
3. **Integration — Claude ライブ対**: `./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify 'TestE2E_CodingAgentStreaming$|TestE2E_CodingAgentDefaultModel$|TestE2E_SessionContinuation$|TestE2E_ClaudeCode_TernctlRealCommand$'`
4. **Integration — Codex ライブ非回帰**: `./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify 'TestCodexE2E_'`

（Linux / Remote-SSH Linux では `build.sh --skip-etc`、統合は `xvfb-run -a` ラップ。本リポジトリに `--categories` は無い。）

### E2E コード化

*   新規ヘルパ + 既存 `tests/agentservice_e2e_test.go` / `tests/codex_e2e_test.go` の改修でカバー。手動確認のみは禁止。

## Documentation

*   仕様 001 の VS-5 チェックリストを実装完了時に更新（任意）。
*   Reference Manual の SSE `[DONE]` 説明に「EventResult 後 drain（既定 2s）および relay done シグナル」を 1 段落追記する場合: `docs/ReferenceManual-WebAPIs.md`（実装時に差分があれば）。
