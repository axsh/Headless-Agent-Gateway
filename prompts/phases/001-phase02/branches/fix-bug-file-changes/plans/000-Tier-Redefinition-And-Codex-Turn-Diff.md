# 000-Tier-Redefinition-And-Codex-Turn-Diff

> **Source Specification**: `prompts/phases/001-phase02/branches/fix-bug-file-changes/ideas/000-Tier-Redefinition-And-Codex-Turn-Diff.md`

## Goal Description

Tier1/2/3 の意味を「エージェントネイティブ / 類推 / 外部補完」に再定義し、Codex の Tier1 ソースとして App Server 形式の `turn/diff/updated` を消費できるようにする。本 PR は **案 B（exec 維持 + turn/diff 通知パーサ／注入 + SSE TaskLog 同期）** を採る。実 Codex セッションの App Server 全面移行は次 PR。

## User Review Required

- **方式固定**: 案 B。受け入れは仕様どおり「fake App Server / フィクスチャで turn/diff → System Artifact」。本番の同一ターンで常に `turn_diff` が出ることは本 PR では保証しない（exec 継続のため）。`file_change` 互換は維持。
- **重複排除**: 同一 session+turn+key は **先勝ち（first-wins）**。テストで固定。
- **`tool_name`**: `"turn_diff"` に固定。
- Optional O1/O2/O3 は本計画の対象外。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1 Tier 意味定義 | Documentation + `file_change_collectors.go` コメント |
| R2 turn/diff → Tier1 / tool_name=turn_diff / unified diff 解析 | Proposed Changes > codex/turn_diff.go, analyzer.go |
| R3 file_change 互換維持 | 既存 protocol/analyzer を変更破壊しないこと + 退行テスト |
| R4 Claude/Cursor Tier1 | 変更なし（退行テスト） |
| R5 Tier2/3 維持 | 変更なし |
| R6 collectors 互換・structured_tool が turn_diff もゲート | analyzer `case "turn_diff"` + docs |
| R7 App Server 経路で受け取れる / fake 検証 | appserver_notify.go, notify_reader.go, 統合テスト |
| R8 SSE 直後 List 欠落なし | handler_retry.go attachSSE 同期 Add |
| R9 非 LLM テスト | turn_diff_*_test.go, tests/turn_diff_tier1_e2e_test.go |
| O1–O3 | 対象外 |

## Proposed Changes

### codingagent/codex — turn/diff パーサ

#### [NEW] [shared/libs/go/codingagent/codex/turn_diff_test.go](file://shared/libs/go/codingagent/codex/turn_diff_test.go)

*   **Description**: U1 — unified diff → path/kind、および params → StreamEvent。
*   **Logic**（テーブル駆動）:
    *   add: `--- /dev/null\n+++ b/hello.txt\n` → `{path:"hello.txt", kind:"add"}`
    *   update: `--- a/a.go\n+++ b/a.go\n` → kind `"update"`
    *   delete: `--- a/gone.txt\n+++ /dev/null\n` → kind `"delete"`
    *   複数ファイル hunk → 複数 op
    *   空 / 解析不能 → 空 slice（エラーは返さず、呼び出し側がログ）
    *   `ParseTurnDiffUpdatedParams` が `ToolName=="turn_diff"`, `Type==EventToolUse`, `ToolInput["changes"]` に配列

#### [NEW] [shared/libs/go/codingagent/codex/turn_diff.go](file://shared/libs/go/codingagent/codex/turn_diff.go)

*   **Description**: ターン集約 unified diff から path/operation を抽出し StreamEvent 化する。
*   **Technical Design**:

```go
const ToolNameTurnDiff = "turn_diff"

type DiffPathOp struct {
    Path string // slash-separated relative or as-in-diff
    Kind string // "add" | "update" | "delete"
}

// ExtractPathsFromUnifiedDiff parses a unified diff string into path ops.
// Unparseable hunks are skipped (caller may log if result empty while diff non-empty).
func ExtractPathsFromUnifiedDiff(diff string) []DiffPathOp

// ParseTurnDiffUpdatedParams builds EventToolUse turn_diff from App Server params JSON:
// {"threadId":"...","turnId":"...","diff":"..."}.
// Returns nil if diff empty or no paths extracted.
func ParseTurnDiffUpdatedParams(params json.RawMessage) *codingagent.StreamEvent

// TurnDiffStreamEvent builds ToolUse with ToolNameTurnDiff.
// One op → ToolInput{"path","kind"}; multiple → ToolInput{"changes":[{path,kind},...]}.
func TurnDiffStreamEvent(ops []DiffPathOp) *codingagent.StreamEvent
```

*   **Logic**:
    *   `--- ` / `+++ ` 行を走査。`a/` `b/` プレフィックスを剥がす。`/dev/null` を見て add/delete を決定。両方実パスなら update。
    *   Windows パス混在時は `filepath.ToSlash`。

#### [NEW] [shared/libs/go/codingagent/codex/appserver_notify_test.go](file://shared/libs/go/codingagent/codex/appserver_notify_test.go)

*   **Description**: App Server JSON-RPC notification 行の振り分け。
*   **Logic**:
    *   `{"method":"turn/diff/updated","params":{...}}` → non-nil turn_diff StreamEvent
    *   未知 method → nil
    *   不正 JSON → nil

#### [NEW] [shared/libs/go/codingagent/codex/appserver_notify.go](file://shared/libs/go/codingagent/codex/appserver_notify.go)

*   **Description**: R7 — 通知 1 行を StreamEvent に変換。
*   **Technical Design**:

```go
// ParseAppServerNotification parses one JSON-RPC notification line.
// Supports method "turn/diff/updated" (params → ParseTurnDiffUpdatedParams).
// Returns nil for unrelated methods or parse failures.
func ParseAppServerNotification(line string) *codingagent.StreamEvent
```

*   **Logic**: `json.Unmarshal` で `method` / `params` を読む。`method == "turn/diff/updated"` のみ処理。

#### [NEW] [shared/libs/go/codingagent/codex/notify_reader.go](file://shared/libs/go/codingagent/codex/notify_reader.go)

*   **Description**: R7 — io.Reader から通知 JSONL を読み同一チャネルへ fan-in（テスト／将来の app-server stdout 用）。
*   **Technical Design**:

```go
// FanInAppServerNotifications reads lines from r, parses via ParseAppServerNotification,
// and sends non-nil events to out until ctx done or r EOF. Does not close out.
func FanInAppServerNotifications(ctx context.Context, r io.Reader, out chan<- codingagent.StreamEvent)
```

#### [NEW] [shared/libs/go/codingagent/codex/testdata/turn_diff_updated.jsonl](file://shared/libs/go/codingagent/codex/testdata/turn_diff_updated.jsonl)

*   **Description**: フィクスチャ 1 行以上（hello.txt add を含む unified diff）。

#### [MODIFY] [shared/libs/go/codingagent/codex/process.go](file://shared/libs/go/codingagent/codex/process.go)（任意フック）

*   **Description**: `ProcessConfig` に任意の `AppServerNotifyReader io.Reader` を追加。非 nil なら `FanInAppServerNotifications` を goroutine で起動し、exec の `ch` へ fan-in。
*   **Logic**: 本番デフォルトは nil（挙動不変）。統合テストで pipe を渡して R7 を満たす。

### artifact/analyzer — Tier1 として turn_diff を記録

#### [MODIFY] [shared/libs/go/artifact/analyzer/analyzer_test.go](file://shared/libs/go/artifact/analyzer/analyzer_test.go)

*   **Description**: U2/U3 — turn_diff 記録、structured_tool OFF、file_change との先勝ち。
*   **Logic**:
    *   `TestAnalyzer_TurnDiff_Create` — ToolName turn_diff, path hello.txt, kind add → OperationCreate, ToolName turn_diff
    *   `TestAnalyzer_TurnDiff_StructuredToolOff` — collectors StructuredTool false → 0 events
    *   `TestAnalyzer_TurnDiff_FirstWinsOverFileChange` — 同一 path で file_change を先に注入後 turn_diff → イベントは 1 件（先勝ちで turn_diff は追加されない）。逆順（turn_diff 先）でも 1 件。

#### [MODIFY] [shared/libs/go/artifact/analyzer/analyzer.go](file://shared/libs/go/artifact/analyzer/analyzer.go)

*   **Description**: R2/R6 — `turn_diff` を structured_tool ゲート下で処理。先勝ち用の in-memory set。
*   **Technical Design**:
    *   `ToolCallAnalyzer` に `seenMu sync.Mutex` と `seenTier1 map[string]struct{}`（key = `sessionID+"\x00"+turnID+"\x00"+normalizedKey`）を追加。
    *   `analyzeEvents` の switch に:

```go
case "turn_diff":
    if !cfg.StructuredTool {
        return nil
    }
    return a.analyzeFileChange(ev, sessionID, turnID, correlationID) // kind/path 同形
```

    *   ただし `analyzeFileChange` / `buildEvent` 経路で保存前に `tryClaimTier1Key(session, turn, key) bool` を呼び、false ならその path をスキップ。
    *   `file_change` および mapped tools の build 時も同じ `tryClaimTier1Key` を通す（Tier1 同士 first-wins）。
    *   Tier2 shell: **Tier1 済み key は shell でもスキップ**。

*   **Logic**: `analyzeFileChange` は ToolName をイベントにそのまま載せるため、turn_diff も `ev.ToolName` が `turn_diff` のまま保存される。

#### [MODIFY] [shared/libs/go/artifact/analyzer/reconcile.go](file://shared/libs/go/artifact/analyzer/reconcile.go)

*   **Description**: `sourceForToolName` に `"turn_diff"` → `SourceStructuredTool` を追加。

### codingagent — Tier コメント

#### [MODIFY] [shared/libs/go/codingagent/file_change_collectors.go](file://shared/libs/go/codingagent/file_change_collectors.go)

*   **Description**: R1/R6 — パッケージ／型コメントを新 Tier 定義に更新。
*   **Logic**: `structured_tool` = Tier1 エージェントネイティブ（Codex: turn_diff + file_change、他: Write/Edit…）。`shell_parser` = Tier2。`workdir_reconcile` = Tier3。キー名は変更しない。

### agentservice — SSE TaskLog 同期（R8）

#### [MODIFY] [shared/libs/go/agentservice/handler_retry.go](file://shared/libs/go/agentservice/handler_retry.go) および既存 SSE テスト

*   **Description**: attachSSE のクライアント配送と同時に `taskLog.Add`。pump との二重登録を防ぐ。
*   **Logic**:
    1. `attachSSE` の `writeEvent` 内、SSE 書き込み成功後:

```go
if s.taskLog != nil {
    s.taskLog.Add(toAgentLogEntry(ev, sessionID, exec.turnID, exec.correlationID))
}
```

    2. `pumpExecSideEffects` / `handleRelaySideEffects`: **subscriber がいる間は TaskLog Add をスキップ**（例: `exec.hasSubscriber()` が true なら Add しない）。subscriber 無し（detach 後の drain）のみ pump が Add する。
    3. 既存 JSON 経路は現状維持。

#### [NEW または MODIFY] agentservice 側単体テスト

*   **Description**: U5 — subscriber ありで attach 相当の Add が同期的に起きること、または pump スキップの単体。既存 `handler_*_test.go` に追加してよい。

### tests — 非 LLM 統合 / E2E

#### [NEW] [tests/turn_diff_tier1_e2e_test.go](file://tests/turn_diff_tier1_e2e_test.go)

*   **Description**: I1/I2/S1/S2/S5 — `agentservice.New`（Analyzer 配線あり）+ ArtifactStore。
*   **Logic**:
    *   `TestTurnDiff_Tier1_FromAppServerNotifyFixture`: TaskLog に `ParseAppServerNotification` 結果を Add、または fake agent が turn_diff を返す。List で `tool_name=turn_diff`, key に hello.txt。
    *   `TestTurnDiff_StructuredToolOff`: collectors structured_tool false → 0 件。
    *   `TestSSE_Tier1_AvailableImmediatelyAfterDone`: fake agent が file_change または turn_diff を返し、SSE `[DONE]` 直後に List で key が存在する（R8）。`agentservice.New` + `WithArtifactStore` + `WithTaskLog` を使う（`NewWithStore` は Analyzer 未配線のため使わない）。

### Documentation

#### [MODIFY] [docs/ReferenceManual-WebAPIs.md](file://docs/ReferenceManual-WebAPIs.md)

*   **Description**: `file_change_collectors` の Tier 説明を R1 定義に更新。Codex Tier1 = `turn/diff`（tool_name `turn_diff`）+ 互換 `file_change`。exec だけでは turn/diff 通知は出ず、App Server 通知形式を消費する旨を注記。

## Step-by-Step Implementation Guide

1. **[x] TurnDiff RED**: `turn_diff_test.go` を追加し失敗を確認。
2. **[x] TurnDiff GREEN**: `turn_diff.go` を実装。
3. **[x] AppServerNotify RED/GREEN**: `appserver_notify_test.go` → `appserver_notify.go` + testdata + `notify_reader.go`。
4. **[x] Analyzer RED/GREEN**: analyzer テスト追加 → `analyzer.go` / `reconcile.go`（turn_diff + first-wins + shell skip if Tier1 seen）。
5. **[x] Collectors comments**: `file_change_collectors.go`。
6. **[x] Process optional fan-in**: スキップ（`FanInAppServerNotifications` を公開し、統合はフィクスチャ注入で R7 を満たす。StartProcess 配線は次 PR）。
7. **[x] SSE sync Add**: `handler_retry.go`（attachSSE Add + pump skip when subscriber）。
8. **[x] Integration E2E**: `tests/turn_diff_tier1_e2e_test.go`。
9. **[x] Docs**: ReferenceManual 更新。
10. **[/] Build & tests**: Verification Plan を実行。

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**:

```bash
./scripts/process/build.sh --skip-frontend --skip-etc
```

2. **Integration Tests**（本機能）:

```bash
./scripts/process/build.sh --skip-frontend --skip-etc && ./scripts/process/integration_test.sh --specify 'TestTurnDiff|TestFileChangeCollector|TestReconcile|TestSSE_Tier1'
```

3. **E2E Tests**: `tests/turn_diff_tier1_e2e_test.go` に上記ケースをコード化（手動確認禁止）。

4. **Full regression**（ユーザー要求のフルテスト）:

```bash
./scripts/process/build.sh --skip-frontend --skip-etc && ./scripts/process/integration_test.sh
```

（Windows ローカル。Linux/Remote-SSH 時は build に `--skip-etc`、integration は `xvfb-run -a` ラップ。）

## Documentation

- `docs/ReferenceManual-WebAPIs.md` の collectors / Tier 説明を本仕様 R1・R6 に合わせて更新。
- Codex: Tier1 は `turn_diff`（App Server `turn/diff/updated` 形式）および互換 `file_change`；Tier2 は shell；Tier3 は workdir_reconcile。
