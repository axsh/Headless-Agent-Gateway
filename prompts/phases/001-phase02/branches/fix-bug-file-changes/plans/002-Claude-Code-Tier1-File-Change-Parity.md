# 002-Claude-Code-Tier1-File-Change-Parity

> **Source Specification**: `prompts/phases/001-phase02/branches/fix-bug-file-changes/ideas/002-Claude-Code-Tier1-File-Change-Parity.md`

## Goal Description

Claude Code の Tier1 ファイル変更を、Codex のネイティブ変更面（`file_change` / `turn_diff`）と **SystemArtifacts.List 上で結果透過**にする。個別ツール（Write/Edit/…）の記録を回帰固定し、ターン集約相当として Tern 側合成 `tool_name=turn_files`（仕様案 S1）を Analyzer に追加する。Codex 経路は壊さない。

## User Review Required

- **M3 方式固定**: **S1（Tern 側合成）のみ**。S2 は対象外。`tool_name` は **`turn_files`** に固定（`turn_diff` は Codex unified diff 専用のまま）。
- **重複排除**: 同一 `sessionID+turnID+key` は **先勝ち（first-wins）**。Write が既に key を占有していれば `turn_files` は追加行を作らない。P-Native は「path が List に存在する」ことで充足。`turn_files` 単体の記録は「Write 未経由で `turn_files` イベントだけを注入するテスト」で検証する。
- **依存**: ライブ List の信頼性は計画 `001-Shared-SSE-Terminal-And-Agent-E2E-Parity` に依存。Analyzer 単体・フィクスチャ統合は 001 完了前でも着手可。
- **O1/O3**: 本計画の対象外（O2「Bash を Tier1 にしない」は遵守）。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| M1 P-Native / P-Collector / P-ToolName | analyzer + 共通 List ヘルパ + collectors ゲート |
| M2 Write/Edit/… 信頼性・path 欄 | analyzer_test 拡張、defaultToolMappings 維持 |
| M3 turn_files 合成 S1 | analyzer turn_files + artifact_reconcile / Flush 呼び出し |
| M4 Codex 非回帰 | turn_diff_tier1_e2e / analyzer file_change 退行 |
| M5 000 R4 更新 | ideas/000 への相互リンク 1 段落 |
| M6 非 LLM / OFF / 共通ヘルパ | analyzer_test, tests/claude_tier1_*_test.go |
| O1–O3 | 対象外（O2 は設計制約として明記） |

## Proposed Changes

### artifact/analyzer — turn_files と Tier1（M1–M3）

#### [MODIFY] [shared/libs/go/artifact/analyzer/analyzer_test.go](file://shared/libs/go/artifact/analyzer/analyzer_test.go)

*   **Description**: TDD — Write path 欄・`turn_files`・コレクタ OFF・first-wins を先に書く。
*   **Logic**（テーブル駆動）:
    *   Claude Write `file_path` → List 相当 memStore に `tool_name=Write`, key 相対 path。
    *   Write `path`（Cursor 互換）も可。
    *   Edit / MultiEdit / NotebookEdit（`notebook_path`）が Tier1。
    *   `structured_tool=false` → Save されない。
    *   `turn_files` 単独: ToolInput `changes:[{path,kind}]` または単一 `path`+`kind` → 各 path が `tool_name=turn_files`。
    *   Write で key 占有後に同 path の `turn_files` → 追加イベントなし（first-wins）。
    *   同一 turn で Write a.txt + Write b.txt → 両方の key が存在（集約前の個別信頼性）。

#### [MODIFY] [shared/libs/go/artifact/analyzer/analyzer.go](file://shared/libs/go/artifact/analyzer/analyzer.go)

*   **Description**: `turn_files` を Tier1 として解析。ターン終端で合成するための API を追加。
*   **Technical Design**:

```go
const ToolNameTurnFiles = "turn_files"

// isTier1ToolName: add case "turn_files" alongside file_change, turn_diff, Write, ...

// analyzeEvents / switch: case "turn_files" → same path as file_change / turn_diff
// (reuse analyzeFileChange which already understands path/kind and changes[]).

// TurnFileOp mirrors codex DiffPathOp shape for synthesis input.
type TurnFileOp struct {
    Path string // as reported by tool (resolved later by buildEvent)
    Kind string // "add" | "update" | "delete"  → map to store.Operation*
}

// CollectClaudeTier1OpsFromStream extracts Write/Edit/MultiEdit/NotebookEdit/StrReplace/Delete
// ops from StreamEvents (ToolUse only). Kind from tool name:
//   Write→create, Delete→delete, else→update.
func CollectClaudeTier1OpsFromStream(events []codingagent.StreamEvent) []TurnFileOp

// SynthesizeTurnFilesEvent builds EventToolUse with ToolNameTurnFiles.
// One op → ToolInput{"path","kind"}; multiple → ToolInput{"changes":[{path,kind},...]}.
// Returns nil if ops empty.
func SynthesizeTurnFilesEvent(ops []TurnFileOp) *codingagent.StreamEvent

// FlushTurnFiles analyzes a synthesized turn_files event into the store (respecting collectors + first-wins).
func (a *ToolCallAnalyzer) FlushTurnFiles(ctx context.Context, sessionID, turnID, correlationID string, ops []TurnFileOp) error
```

*   **Logic**:
    *   `FlushTurnFiles`: `ev := SynthesizeTurnFilesEvent(ops)`; nil なら return nil。`analyzeFileChange` / 既存 `handle` 経路で Save（`structured_tool` ゲートは Analyzer 既存設定に従う）。
    *   `kind` → operation: `add`/`create` → `OperationCreate`, `delete` → `OperationDelete`, その他 → `OperationUpdate`。
    *   Bash は Collect 対象外（O2）。

#### [MODIFY] [shared/libs/go/artifact/analyzer/reconcile.go](file://shared/libs/go/artifact/analyzer/reconcile.go)

*   **Description**: `sourceForToolName` に `turn_files` → `structured_tool` を追加（Tier3 マージ表示用、既存 `turn_diff` と同様）。

### agentservice — ターン終端で Flush（M3 呼び出し）

#### [MODIFY] [shared/libs/go/agentservice/artifact_reconcile_test.go](file://shared/libs/go/agentservice/artifact_reconcile_test.go)（なければ NEW）

*   **Description**: reconcile 時に relay 上の Claude Tier1 ツールから `FlushTurnFiles` が呼ばれることを、フェイク Analyzer または mem store で検証。
*   **Logic**: activeExecution.relay に Write×2 + Result 相当のスナップショットを載せ、`reconcileSessionArtifacts` 後に a.txt/b.txt が store にある（Write 解析済みならそのままでも可。Flush がエラーなく呼ばれることをスパイまたは turn_files 単独シナリオで確認）。

#### [MODIFY] [shared/libs/go/agentservice/artifact_reconcile.go](file://shared/libs/go/agentservice/artifact_reconcile.go)

*   **Description**: `reconcileSessionArtifacts` の Tier3 処理の**前**に、当該 session の `exec.relay.EventsSnapshot()` から `CollectClaudeTier1OpsFromStream` → `analyzer.FlushTurnFiles` を実行。
*   **Logic**:
    *   `artifactStore` / analyzer が nil なら no-op。
    *   `EffectiveFileChangeCollectors.StructuredTool == false` なら Flush しない（Analyzer 側でもゲートするが二重に安全）。
    *   Codex セッションでも Collect は Write 系のみ拾うため `file_change`/`turn_diff` には影響しない。空 ops なら Flush は no-op。
    *   エラーは logger.Warn（セッション失敗にしない）。

### tests — フィクスチャ統合と共通 List ヘルパ（M1 / M6）

#### [NEW] [tests/testutil/artifact_list_parity.go](file://tests/testutil/artifact_list_parity.go)（または `tests/artifact_list_parity_helpers_test.go`）

*   **Description**: Codex / Claude 共通「期待 path が List に含まれる」ヘルパ。
*   **Technical Design**:

```go
// AssertSystemArtifactPathsContain fails unless every wantBase or wantRel path
// appears in listed artifacts (match on Key basename or Key).
func AssertSystemArtifactPathsContain(t *testing.T, listed []store.SystemArtifactEvent, wantPaths ...string)
```

#### [NEW] [tests/claude_tier1_turn_files_e2e_test.go](file://tests/claude_tier1_turn_files_e2e_test.go)

*   **Description**: 非 LLM — 実サーバまたは Analyzer+TaskLog フィクスチャで Claude Write → List、および turn_files 注入 → List。
*   **Logic**:
    *   VS-1: Write `file_path=hello.txt` 注入 → List に hello.txt、`tool_name=Write`。
    *   VS-2: 同一 turn で a.txt/b.txt → 両 path が List で説明可能。
    *   VS-3: `structured_tool=false` → 増分なし。
    *   turn_files のみ注入 → `tool_name=turn_files` で path 出現。
    *   既存 `tests/turn_diff_tier1_e2e_test.go` と同じく LLM 不要。サーバ起動パターンは turn_diff E2E を踏襲。

#### [MODIFY] [tests/turn_diff_tier1_e2e_test.go](file://tests/turn_diff_tier1_e2e_test.go)

*   **Description**: 共通ヘルパ `AssertSystemArtifactPathsContain` を使うよう寄せる（Codex 対称 M4/M6）。挙動変更なし。

### ドキュメント（M5）

#### [MODIFY] [prompts/phases/001-phase02/branches/fix-bug-file-changes/ideas/000-Tier-Redefinition-And-Codex-Turn-Diff.md](file://prompts/phases/001-phase02/branches/fix-bug-file-changes/ideas/000-Tier-Redefinition-And-Codex-Turn-Diff.md)

*   **Description**: R4 節に注記を 1 段落追加。
*   **Logic**: 「Claude Tier1 は Write/Edit に加え、仕様 002 のターン集約相当 `turn_files`（Tern 合成）を含む。詳細は `ideas/002-Claude-Code-Tier1-File-Change-Parity.md`。」

#### [MODIFY] [docs/ReferenceManual-WebAPIs.md](file://docs/ReferenceManual-WebAPIs.md)

*   **Description**: System Artifact / collectors の説明に `turn_files`（Claude ターン集約相当）を追記。`turn_diff` と混同しない文言。

## Step-by-Step Implementation Guide

1. [x] **TDD analyzer**: `analyzer_test.go` に Write / turn_files / OFF / first-wins ケースを追加し、Fail を確認。
2. [x] **Implement analyzer**: `ToolNameTurnFiles`、`isTier1ToolName`、`analyzeFileChange` 分岐、`Collect*` / `Synthesize*` / `FlushTurnFiles` を実装し単体 PASS。
3. [x] **reconcile.go source map**: `turn_files` → structured_tool。
4. [x] **Wire reconcile**: `artifact_reconcile.go` で Flush 呼び出し + テスト。
5. [x] **Parity helper**: List パス断言ヘルパを追加。
6. [x] **E2E fixture**: `tests/claude_tier1_turn_files_e2e_test.go` を追加。`turn_diff` E2E をヘルパ利用に更新。
7. [x] **Docs**: 000 R4 注記 + Reference Manual。
8. [x] **Verification Plan 実行**。

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**: `./scripts/process/build.sh`
2. **Integration — Tier1 フィクスチャ**: `./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify 'TestTurnDiff|TestClaudeTier1|TestFileChangeCollector|TestAnalyzer'`
3. **Integration — Codex 非回帰（ライブ可）**: `./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify 'TestCodexE2E_SystemArtifact|TestTurnDiff'`
4. **（001 完了後）Claude ライブ併用**: `./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify 'TestE2E_CodingAgentStreaming$|TestE2E_CodingAgentDefaultModel$'`

（Linux / Remote-SSH Linux: `build.sh --skip-etc`、統合は `xvfb-run -a`。`--categories` なし。）

### E2E コード化

*   `tests/claude_tier1_turn_files_e2e_test.go`（必須・非 LLM）。
*   ライブ List 非空は 001 透過性完了後の best-effort（Skip は Codex と対称条件のみ）。

## Documentation

*   `ideas/000-...md` R4 注記。
*   `docs/ReferenceManual-WebAPIs.md` — `turn_files` / collectors。
*   `file_change_collectors.go` コメントに Tier1 例として `turn_files` を追記（任意だが推奨）。
