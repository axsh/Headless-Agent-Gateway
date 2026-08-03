# 000-Codex-SystemArtifact-Tracking-Part1

> **Source Specification**: `prompts/phases/000-foundation/branches/bugfix-#28/ideas/000-Codex-SystemArtifact-Tracking.md`

## Goal Description

[Issue #28](https://github.com/axsh/arctic-tern/issues/28) にて Codex 利用時に `GET /api/v1/artifacts/system` が常に空を返す問題を修正する。Codex CLI が emit する **`file_change` 構造化イベント** を Primary として protocol → TaskLog → Analyzer 経路に載せ、Secondary として **`command_execution`（Codex）と `Bash`（Claude Code）を共通シェルパーサー** で処理する。Claude Code の **`NotebookEdit`** ギャップも同時解消し、既存 Cursor / Claude Code の `Write` / `Edit` 等に退行を与えない。

本 Part は仕様書の **必須要件 R1〜R11（Tier 1/2）** のみを対象とする。Tier 3 リコンシリエーション（O1〜O7）は Part 2 を参照。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: `file_change` item をパースし TaskLog に流す | `codex/protocol.go` — `parseItemEvent` |
| R2: Analyzer が `file_change` から System Artifact を記録 | `artifact/analyzer/analyzer.go` |
| R3: Codex `command_execution` + Claude Code `Bash` を共通パーサーで処理 | `artifact/analyzer/command_parser.go`, `analyzer.go` |
| R4: シェルパーサー対象パターン（redirect, tee, heredoc, cp, mv, touch, rm, Windows 一部） | `command_parser.go`, `command_parser_test.go` |
| R5: Claude Code `NotebookEdit` mapping 追加 | `analyzer.go` |
| R6: 相対パスを WorkDir 基準で絶対化し key 正規化 | 既存 `resolvePath` / `toRelativePath`（変更なし、新経路からも呼ぶ） |
| R7: パス不明コマンドはイベント生成しない | `command_parser.go` — 空スライス返却 |
| R8: 既存 Write/Edit/MultiEdit/StrReplace/Delete mapping 退行なし | `analyzer_test.go` 既存テスト維持 |
| R9: 単体テスト（CLI なし） | `protocol_test.go`, `command_parser_test.go`, `analyzer_test.go` |
| R10: Codex E2E — System Artifacts API | `tests/codex_e2e_test.go` — `TestCodexE2E_SystemArtifact_FileCreation` |
| R11: Claude Code E2E 回帰 | `tests/artifact_e2e_test.go` — `TestE2E_ArtifactPipeline_FullLifecycle` |

## Proposed Changes

### Phase 1 — Codex protocol: `file_change`（R1, R2 の前提）

#### [MODIFY] [shared/libs/go/codingagent/codex/protocol_test.go](file://shared/libs/go/codingagent/codex/protocol_test.go)

* **Description**: `file_change` item.completed のパースを TDD で先に追加。現行コードでは `default` 分支で nil が返るため **FAIL** する。
* **Technical Design**: 既存 `TestParseExecEvent_ItemCompleted_*` パターンに倣う。
* **Logic** — 追加テスト:

| テスト名 | 入力 JSONL | 期待 |
|----------|-----------|------|
| `TestParseExecEvent_ItemCompleted_FileChange_Single` | `item.completed` + `changes:[{path:"docs/a.md",kind:"add"}]` | `EventToolUse`, `ToolName=="file_change"`, `ToolInput["path"]=="docs/a.md"`, `ToolInput["kind"]=="add"` |
| `TestParseExecEvent_ItemCompleted_FileChange_Multiple` | `changes` に add + update 2 件 | `EventToolUse`, `ToolInput["changes"]` が 2 要素の slice |
| `TestParseExecEvent_ItemStarted_FileChange_Ignored` | `item.started` + `file_change` | `nil`（completed のみ処理） |
| `TestParseExecEvent_ItemCompleted_FileChange_UpdateDelete` | kind=`update` / `delete` | 各 kind が ToolInput に保持される |

入力例（仕様書より）:

```json
{
  "type": "item.completed",
  "item": {
    "id": "item_4",
    "type": "file_change",
    "changes": [
      {"path": "docs/a.md", "kind": "add"},
      {"path": "docs/b.md", "kind": "update"}
    ],
    "status": "completed"
  }
}
```

#### [MODIFY] [shared/libs/go/codingagent/codex/protocol.go](file://shared/libs/go/codingagent/codex/protocol.go)

* **Description**: `parseItemEvent` に `file_change` 分支を追加。`ParseExecEvent` の戻り値シグネチャは変更しない（1 JSONL 行 → 1 `StreamEvent`）。
* **Technical Design**:

```go
// fileChangeEntry mirrors Codex CLI file_change.changes[] element.
type fileChangeEntry struct {
    Path string `json:"path"`
    Kind string `json:"kind"` // add | update | delete
}

// Inside parseItemEvent header struct, add:
Changes []fileChangeEntry `json:"changes,omitempty"`
```

* **Logic**:
  1. `header.Type == "file_change"` かつ `completed == true` のときのみ処理
  2. `len(header.Changes) == 0` → `nil`
  3. **単一 change**: `ToolInput: map[string]any{"path": c.Path, "kind": c.Kind}`
  4. **複数 changes**: `ToolInput: map[string]any{"changes": []map[string]any{{"path", "kind"}, ...}}`
  5. `ToolName: "file_change"`
  6. `EventToolUse` を返す
  7. kind マッピングは protocol 層では行わず **生の kind を保持**（analyzer で `add→create` 等に変換）

---

### Phase 2 — 共通シェルコマンドパーサー（R3, R4, R7）

#### [NEW] [shared/libs/go/artifact/analyzer/command_parser_test.go](file://shared/libs/go/artifact/analyzer/command_parser_test.go)

* **Description**: シェルパーサーのテーブル駆動テスト。R7（誤検知より未記録）を検証。
* **Technical Design**:

```go
func TestParseShellCommand(t *testing.T) {
    tests := []struct {
        name  string
        cmd   string
        want  []analyzer.ParsedFileOp
    }{ ... }
}
```

* **Logic** — 最低限ケース:

| テスト名 | command | 期待 |
|----------|---------|------|
| `TestCommandParser_EchoRedirect` | `echo hello > output.txt` | `[{Path:"output.txt", Operation: create}]` |
| `TestCommandParser_AppendRedirect` | `echo x >> log.txt` | `[{Path:"log.txt", Operation: update}]` |
| `TestCommandParser_Tee` | `echo hi \| tee out.txt` | `[{Path:"out.txt", Operation: create}]` |
| `TestCommandParser_Heredoc` | `cat <<EOF > f.txt\nhi\nEOF` | `[{Path:"f.txt", Operation: create}]` |
| `TestCommandParser_Cp` | `cp src.txt dst.txt` | `[{Path:"dst.txt", Operation: create}]` |
| `TestCommandParser_Mv` | `mv old.txt new.txt` | `[{Path:"new.txt", Operation: update}]`（旧 path は記録しない） |
| `TestCommandParser_Touch` | `touch newfile.txt` | `[{Path:"newfile.txt", Operation: create}]` |
| `TestCommandParser_Rm` | `rm obsolete.txt` | `[{Path:"obsolete.txt", Operation: delete}]` |
| `TestCommandParser_NoFileOp` | `ls -la` | `nil` または空スライス |
| `TestCommandParser_GitStatus` | `git status` | 空スライス |
| `TestCommandParser_SetContent_Windows` | `Set-Content -Path out.txt -Value hi` | `[{Path:"out.txt", Operation: create}]`（runtime.GOOS==windows 時のみ、または全 OS でパース） |
| `TestCommandParser_OutFile_Windows` | `'hi' \| Out-File out.txt` | 同上 |

#### [NEW] [shared/libs/go/artifact/analyzer/command_parser.go](file://shared/libs/go/artifact/analyzer/command_parser.go)

* **Description**: Codex `command_execution` と Claude Code `Bash` の共通パーサー。
* **Technical Design**:

```go
package analyzer

import "github.com/axsh/arctic-tern/shared/libs/go/artifact/store"

// ParsedFileOp is a file operation extracted from a shell command string.
type ParsedFileOp struct {
    Path      string
    Operation string // store.OperationCreate | store.OperationUpdate | store.OperationDelete
}

// ParseShellCommand extracts file operations from a one-line shell command.
// Returns empty slice when no file operation can be determined (conservative).
func ParseShellCommand(command string) []ParsedFileOp
```

* **Logic**:
  1. 入力を trim。空 → 空スライス
  2. パターン検出（順不同で複数マッチ可、同一 path は dedup）:
     - **リダイレクト**: 正規表現 `>\s*([^\s|;&]+)` → create、`>>` → update
     - **tee**: `\btee\b.*?([^\s|;&]+)` → create
     - **heredoc**: `<<-?\w*\s*>?\s*([^\s]+)` または `cat\s+<<.*>\s*(\S+)`
     - **cp**: `\bcp\b\s+\S+\s+(\S+)`
     - **mv**: `\bmv\b\s+\S+\s+(\S+)`
     - **touch**: `\btouch\b\s+(\S+)`
     - **rm**: `\brm\b(?:\s+-[^\s]+)*\s+(\S+)`
     - **Set-Content / Out-File** (Windows PowerShell): `-Path\s+(\S+)` / `Out-File\s+(\S+)`
  3. path から引用符（`'`, `"`）を strip
  4. パスが特定できない場合は **何も返さない**（R7）
  5. 1 コマンドから複数 path 抽出時は **複数 `ParsedFileOp`** を返す

---

### Phase 3 — ToolCallAnalyzer 拡張（R2, R3, R5, R6, R8）

#### [MODIFY] [shared/libs/go/artifact/analyzer/analyzer_test.go](file://shared/libs/go/artifact/analyzer/analyzer_test.go)

* **Description**: 新 tool 名向けテストを追加。既存 `TestAnalyzer_CursorWrite` 等は **変更しない**（R8 回帰）。
* **Logic** — 追加テスト（`injectToolUseEvent` ヘルパ再利用）:

| テスト名 | inject | 期待 |
|----------|--------|------|
| `TestAnalyzer_Codex_FileChange_Create` | `file_change`, `{path:"docs/a.md", kind:"add"}` | `Key=="docs/a.md"`, `Operation==create`, `ToolName==file_change` |
| `TestAnalyzer_Codex_FileChange_Multiple` | `file_change`, `{changes:[{path:"a.md",kind:"add"},{path:"b.md",kind:"update"}]}` | 2 events |
| `TestAnalyzer_Codex_FileChange_Delete` | kind=`delete` | `Operation==delete` |
| `TestAnalyzer_Codex_CommandExecution_Create` | `command_execution`, `{command:"echo hi > out.txt"}` | `Key` ends with `out.txt`, `Operation==create` |
| `TestAnalyzer_ClaudeCode_Bash_Create` | `Bash`, `{command:"echo hi > out.txt"}` | 同上、`ToolName==Bash` |
| `TestAnalyzer_ClaudeCode_NotebookEdit` | `NotebookEdit`, `{notebook_path:"nb.ipynb"}` または `{file_path:"nb.ipynb"}` | `Operation==update` |
| `TestAnalyzer_CommandExecution_NoFileOp` | `command_execution`, `{command:"ls -la"}` | 0 events |
| `TestAnalyzer_WorkDirRelativePath` | workDirResolver 設定 + 相対 path | key が workDir 相対になる（R6） |

#### [MODIFY] [shared/libs/go/artifact/analyzer/analyzer.go](file://shared/libs/go/artifact/analyzer/analyzer.go)

* **Description**: `analyzeEvent` を拡張し、mapping 外 tool を個別処理。複数イベント保存対応。
* **Technical Design**:

```go
// kindToOperation maps Codex file_change kind to store operation.
func kindToOperation(kind string) string {
    switch kind {
    case "add":
        return store.OperationCreate
    case "update":
        return store.OperationUpdate
    case "delete":
        return store.OperationDelete
    default:
        return ""
    }
}

// analyzeFileChange handles ToolName=="file_change".
func (a *ToolCallAnalyzer) analyzeFileChange(ev codingagent.StreamEvent, sessionID string) []*store.SystemArtifactEvent

// analyzeShellTool handles command_execution and Bash via ParseShellCommand.
func (a *ToolCallAnalyzer) analyzeShellTool(ev codingagent.StreamEvent, sessionID string) []*store.SystemArtifactEvent

// buildEvent creates a single SystemArtifactEvent from path + operation.
func (a *ToolCallAnalyzer) buildEvent(sessionID, toolName, filePath, operation string) *store.SystemArtifactEvent
```

* **Logic** — `analyzeEvent` フロー:

| ToolName | 処理 |
|----------|------|
| `Write`, `Edit`, `MultiEdit`, `StrReplace`, `Delete` | 既存 `defaultToolMappings`（**変更なし**） |
| `file_change` | `ToolInput["path"]+["kind"]` または `ToolInput["changes"]` を展開 → `kindToOperation` |
| `NotebookEdit` | `defaultToolMappings` に追加: `{update, "notebook_path"}`, `{update, "file_path"}` |
| `command_execution`, `Bash` | `ParseShellCommand(ToolInput["command"])` → 各 op で `buildEvent` |
| `Read`, `Glob`, `Grep` 等 | `nil`（既存どおり） |

* **Logic** — `onEntry` 変更:
  - `analyzeEvent` が単一イベント返却から **スライス返却** に変更
  - 各 event を `SaveSystemArtifactEvent` で保存

```go
events := a.analyzeEvents(ev, agentLog.AgentID)
for _, event := range events {
    if event != nil {
        _ = a.st.SaveSystemArtifactEvent(context.Background(), *event)
    }
}
```

* **Logic** — `defaultToolMappings` への追加:

```go
"NotebookEdit": {
    {store.OperationUpdate, "notebook_path"},
    {store.OperationUpdate, "file_path"},
},
```

---

### Phase 4 — Codex System Artifact E2E（R10）

#### [MODIFY] [tests/codex_e2e_test.go](file://tests/codex_e2e_test.go)

* **Description**: 既存 `TestCodexE2E_FileCreation` の disk 確認に加え、System Artifacts API を検証する E2E を追加。
* **Technical Design**:

```go
// TestCodexE2E_SystemArtifact_FileCreation verifies that after a Codex session
// creates hello.txt, GET /api/v1/artifacts/system returns TotalCount >= 1
// with a create event for hello.txt, and Download works.
func TestCodexE2E_SystemArtifact_FileCreation(t *testing.T)
```

* **Logic**（仕様シナリオ F）:
  1. `startCodexE2EServer(t)` → cleanup defer
  2. `workDir := t.TempDir()`
  3. `sessionID := createE2ESessionWithModel(t, baseURL, "codex", "gpt-4o", workDir)`
  4. prompt: `Create a file named hello.txt in the current directory containing exactly the text 'Hello Codex'. Do nothing else.`
  5. `sendE2EMessage` + SSE parse（既存ヘルパ）
  6. disk 上 `hello.txt` 存在確認（既存 TestCodexE2E_FileCreation と同様）
  7. `v1.New(baseURL).SystemArtifacts().List(ctx, v1.SystemArtifactFilter{SessionIDs: []string{sessionID}, Operation: "create"})`
  8. `require.GreaterOrEqual(t, page.TotalCount, 1)`
  9. `page.Items` から basename `hello.txt` の item を特定
  10. `c.SystemArtifacts().Download(ctx, item.Key)` で内容取得可能

* **Note**: `artifact_e2e_test.go` の Step 4〜5 パターン（`TestE2E_ArtifactPipeline_FullLifecycle`）を参考にする。`client/v1` import を追加。

---

## Step-by-Step Implementation Guide

TDD 順（Failed First）:

1. **Protocol RED**: Edit `shared/libs/go/codingagent/codex/protocol_test.go` to add `TestParseExecEvent_ItemCompleted_FileChange_*`. Run `./scripts/process/build.sh` — tests **FAIL**.
2. **Protocol GREEN**: Edit `shared/libs/go/codingagent/codex/protocol.go` — add `file_change` branch in `parseItemEvent`. Build — protocol tests **PASS**.
3. **Command Parser RED**: Create `command_parser_test.go` with table cases. Build — **FAIL**.
4. **Command Parser GREEN**: Create `command_parser.go` implementing `ParseShellCommand`. Build — parser tests **PASS**.
5. **Analyzer RED**: Add analyzer tests for `file_change`, `command_execution`, `Bash`, `NotebookEdit`. Build — **FAIL**.
6. **Analyzer GREEN**: Refactor `analyzer.go` — `analyzeEvents`, `analyzeFileChange`, `analyzeShellTool`, `NotebookEdit` mapping. Build — all unit tests **PASS**.
7. **E2E RED/GREEN**: Add `TestCodexE2E_SystemArtifact_FileCreation` in `tests/codex_e2e_test.go`. Run integration test — verify **PASS** with real codex CLI.
8. **Regression**: Run `./scripts/process/integration_test.sh --categories llm --specify "TestE2E_ArtifactPipeline_FullLifecycle"` — **PASS** (R11).

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**:

```bash
./scripts/process/build.sh
```

2. **Codex System Artifact E2E** (R10):

```bash
./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories llm --specify "TestCodexE2E_SystemArtifact_FileCreation"
```

3. **Claude Code Artifact Pipeline Regression** (R11):

```bash
./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories llm --specify "TestE2E_ArtifactPipeline_FullLifecycle"
```

### Acceptance Criteria（仕様書より）

- [x] `./scripts/process/build.sh` が成功する
- [x] `file_change` protocol テストおよび analyzer / command_parser テストがすべて PASS
- [x] `TestCodexE2E_SystemArtifact_FileCreation` が PASS
- [x] `TestE2E_ArtifactPipeline_FullLifecycle` が PASS
- [x] Issue #28 再現手順（Codex → `GET /api/v1/artifacts/system`）で空でなくなる

## Documentation

- コード変更のみ。`agentservice` TaskLog 配線は変更不要のため API ドキュメント更新なし。
- Tier 3（git diff 補助等）は Part 2 で扱う。
