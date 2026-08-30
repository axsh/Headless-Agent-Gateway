# 000-Shell-Parser-Existence-Guard

> **Source Specification**: `prompts/phases/001-phase02/branches/bug-fix-enhanced-shell-parser/ideas/000-Shell-Parser-Existence-Guard.md`

## Goal Description

Tier2（`shell_parser`）で正規表現が拾った path のうち、解決先がディスク上に存在しないものを System Artifact から除外する。create/update は存在必須、delete は D-A（ゲート対象外）。Claude Bash の実行前 `tool_use` と Codex `item.started` では確定保存せず、実行完了後に Stat → 保存する。

## User Review Required

None.（仕様レビュー済み前提で、次を実装計画で固定）

- **delete**: D-A（存在確認ゲート対象外）
- **タイミング**: Codex は `execution_status=completed` のみ記録。Claude Bash / 遅延が必要なシェルは tool_use で保留し、対応する `tool_result` 後に Stat → 保存
- **Stat 失敗**（NotExist 以外の権限エラー等）: 記録しない

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1 Tier2 記録前に存在確認 | Proposed Changes > analyzer `pathExists` / `emitShellOps` |
| R2 create/update のみ存在必須、delete=D-A | Proposed Changes > `shouldEmitShellOp` |
| R3 実行後の FS 状態で確認 | Proposed Changes > Codex `execution_status`、Claude `ToolCallID` + pending flush |
| R4 resolvePath と同一 path で Stat | Proposed Changes > `emitShellOps` 内で `resolvePath` 後に Stat |
| R5 collectors API 互換 | 変更なし（退行テスト） |
| R6 沈黙 + 任意 DEBUG ログ | Proposed Changes > logger Debug on drop |
| U1–U5 | Proposed Changes > analyzer_test / protocol_test |
| 検証シナリオ 1–5 | Verification Plan + E2E |

## Proposed Changes

### Analyzer — 単体テスト（先に RED）

#### [MODIFY] [shared/libs/go/artifact/analyzer/analyzer_test.go](file://shared/libs/go/artifact/analyzer/analyzer_test.go)

*   **Description**: 存在ゲートと遅延確定のテーブル／シナリオを追加。既存 Bash / command_execution / shell テストを「ファイル作成 → 完了イベント」に更新。
*   **Logic**:
    *   **U1** `TestAnalyzer_Shell_Create_Exists`: workDir に `real.txt` を作成後、`command_execution` + `execution_status=completed` + `echo hi > real.txt` → 1 event、Key=`real.txt`、Operation=create
    *   **U2** `TestAnalyzer_Shell_Create_Missing_Dropped`: `execution_status=completed` + `echo hi > /definitely/not/exist_xyz_12345.txt`（ファイル未作成）→ events empty
    *   **U2b** `TestAnalyzer_Shell_Update_Missing_Dropped`: `echo x >> missing.log` completed、ファイル無し → empty
    *   **U2c** `TestAnalyzer_Shell_Update_Exists`: 既存 `log.txt` に対する `>>` completed → update
    *   **U3** `TestAnalyzer_Shell_Delete_WithoutFile`: `rm gone.txt` completed、ファイル無し → delete 1 event（D-A）
    *   **U4** `TestAnalyzer_Tier1_Write_NoExistenceGate`: Write で存在しない path でも従来どおり event（ゲート非適用）
    *   **U5** `TestAnalyzer_Bash_DefersUntilToolResult`: Bash tool_use（ファイル未作成）→ events empty。ファイル作成後に同 `ToolCallID` の tool_result → create 1 event
    *   **U5b** `TestAnalyzer_CommandExecution_Started_Ignored`: `execution_status=started` のみ → empty（ファイルがあっても無視）
    *   既存 `TestAnalyzer_Codex_CommandExecution_Create` / `TestAnalyzer_ClaudeCode_Bash_Create` / `TestAnalyzer_LegacyShell_Create` / `TestAnalyzer_ShellParserOff`: 完了条件を満たすよう修正（ファイル touch + completed / tool_result）
    *   helper: `injectStreamEvent(t, tl, sessionID, ev codingagent.StreamEvent)`、`injectToolResult(t, tl, sessionID, toolCallID string)`

### codingagent — StreamEvent / Claude / Codex テスト

#### [MODIFY] [shared/libs/go/codingagent/event_test.go](file://shared/libs/go/codingagent/event_test.go)（該当ケースがあれば）

*   **Description**: `ToolCallID` フィールドの JSON 丸め込みを確認（既存テーブルに 1 ケース追加で可）。

#### [MODIFY] [shared/libs/go/codingagent/claudecode/protocol_test.go](file://shared/libs/go/codingagent/claudecode/protocol_test.go)

*   **Description**: tool_use の `id` → `StreamEvent.ToolCallID`、tool_result の `tool_use_id` → `ToolCallID`。
*   **Logic**:
    *   input: `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu_1","name":"Bash","input":{"command":"echo hi > a.txt"}}]}}` → `ToolCallID=="tu_1"`
    *   input: tool_result with `tool_use_id":"tu_1"` → `Type==EventToolResult`, `ToolCallID=="tu_1"`

#### [MODIFY] [shared/libs/go/codingagent/codex/protocol_test.go](file://shared/libs/go/codingagent/codex/protocol_test.go)

*   **Description**: command_execution started/completed の `execution_status`。
*   **Logic**:
    *   item.started → ToolInput[`execution_status`]==`"started"`
    *   item.completed with command → ToolInput[`execution_status`]==`"completed"`

### codingagent — イベント拡張とプロトコル

#### [MODIFY] [shared/libs/go/codingagent/event.go](file://shared/libs/go/codingagent/event.go)

*   **Description**: ツール呼び出し相関用フィールドを追加。
*   **Technical Design**:

```go
type StreamEvent struct {
	Type          EventType              `json:"type"`
	Content       string                 `json:"content,omitempty"`
	PromptID      string                 `json:"prompt_id,omitempty"`
	Choices       []string               `json:"choices,omitempty"`
	ToolName      string                 `json:"tool_name,omitempty"`
	ToolInput     map[string]interface{} `json:"tool_input,omitempty"`
	ToolCallID    string                 `json:"tool_call_id,omitempty"`
	SessionID     string                 `json:"session_id,omitempty"`
	TurnID        string                 `json:"turn_id,omitempty"`
	CorrelationID string                 `json:"correlation_id,omitempty"`
	ChunkID       string                 `json:"chunk_id,omitempty"`
	ChunkIndex    int                    `json:"index,omitempty"`
	ChunkTotal    int                    `json:"total,omitempty"`
	Error         error                  `json:"-"`
	Retryable     bool                   `json:"-"`
}
```

*   **Logic**: 破壊的変更なし（omitempty）。既存クライアントは無視可能。

#### [MODIFY] [shared/libs/go/codingagent/claudecode/protocol.go](file://shared/libs/go/codingagent/claudecode/protocol.go)

*   **Description**: tool_use / tool_result から ToolCallID を伝播。
*   **Technical Design**:

```go
type contentBlock struct {
	Type      string         `json:"type"`
	ID        string         `json:"id,omitempty"`
	Text      string         `json:"text,omitempty"`
	Name      string         `json:"name,omitempty"`
	Input     map[string]any `json:"input,omitempty"`
	ToolUseID string         `json:"tool_use_id,omitempty"`
	Content   string         `json:"content,omitempty"`
}
```

*   **Logic**:
    *   tool_use: `StreamEvent.ToolCallID = block.ID`
    *   tool_result: `StreamEvent.ToolCallID = block.ToolUseID`

#### [MODIFY] [shared/libs/go/codingagent/codex/protocol.go](file://shared/libs/go/codingagent/codex/protocol.go)

*   **Description**: command_execution の ToolInput に実行フェーズを付与。
*   **Logic**:
    *   `item.started` → `ToolInput["execution_status"] = "started"`
    *   `item.completed` かつ Command あり → `ToolInput["execution_status"] = "completed"`
    *   定数推奨: `const ShellExecutionStatusKey = "execution_status"` は analyzer 側でも同じ文字列 `"execution_status"` / `"started"` / `"completed"` を使う（analyzer パッケージに重複定数を置いてよい）

### Analyzer — 存在ゲートと遅延確定

#### [NEW] [shared/libs/go/artifact/analyzer/shell_emit.go](file://shared/libs/go/artifact/analyzer/shell_emit.go)

*   **Description**: Tier2 専用の存在確認と pending flush。
*   **Technical Design**:

```go
const (
	shellExecStatusKey       = "execution_status"
	shellExecStatusStarted   = "started"
	shellExecStatusCompleted = "completed"
)

type pendingShellOps struct {
	toolName      string
	turnID        string
	correlationID string
	ops           []ParsedFileOp
}

// pathExists reports whether resolved path exists (os.Stat). Any error → false.
func pathExists(absPath string) bool

// shouldEmitShellOp: delete → true; create/update → pathExists(resolved).
func (a *ToolCallAnalyzer) shouldEmitShellOp(sessionID, path, operation string) bool

// emitShellOps applies shouldEmitShellOp and buildEvent for each op.
func (a *ToolCallAnalyzer) emitShellOps(sessionID, turnID, correlationID, toolName string, ops []ParsedFileOp) []*store.SystemArtifactEvent
```

*   **Logic**:
    *   `pathExists`: `_, err := os.Stat(absPath); return err == nil`
    *   `shouldEmitShellOp`: `resolved := a.resolvePath(path, sessionID)`。`operation == store.OperationDelete` なら true。それ以外は `pathExists(resolved)`。false のとき Debug ログ（logger が取れる場合。Analyzer に logger が無ければ標準の package 利用を避け、ログ無しでも可。既存に logger 依存が無ければ **省略して沈黙**でも R6 充足）
    *   `emitShellOps`: 各 op で shouldEmit → buildEvent

#### [MODIFY] [shared/libs/go/artifact/analyzer/analyzer.go](file://shared/libs/go/artifact/analyzer/analyzer.go)

*   **Description**: pending マップ、tool_result 処理、shell の開始/完了分岐。
*   **Technical Design**:

```go
type ToolCallAnalyzer struct {
	st                store.ArtifactStore
	projectRoot       string
	workDirResolver   WorkDirResolver
	collectorResolver CollectorConfigResolver
	seenMu            sync.Mutex
	seenTier1Keys     map[string]struct{}
	pendingMu         sync.Mutex
	// pendingShellByCall: sessionID + "\x00" + toolCallID → ops to flush on tool_result
	pendingShellByCall map[string]pendingShellOps
	// pendingShellFIFO: sessionID → queue when ToolCallID empty (legacy Bash without id)
	pendingShellFIFO map[string][]pendingShellOps
}
```

*   **Logic** (`onEntry` / `analyzeEvents`):
    1. `EventToolResult`: `flushPendingShell(sessionID, ev.ToolCallID)` → SaveSystemArtifactEvent。FIFO: ToolCallID 空なら session の先頭を pop。
    2. shell `EventToolUse`（`analyzeShellTool`）:
       - `execution_status == "started"` → return nil
       - `execution_status == "completed"` → ParseShellCommand → emitShellOps（即時）
       - `execution_status` 欠落:
         - tool が `command_execution` → **互換のため completed 扱い**（即時 emitShellOps）。単体テストの移行を簡単にする。ただし Codex 本番は常に status 付与。
         - tool が `Bash` / `shell` / `shell_command` → pending に積み、return nil（tool_result 待ち）
    3. `shell_parser` OFF なら従来どおり早期 return（pending も積まない）

*   **analyzeShellTool 擬似コード**:

```go
func (a *ToolCallAnalyzer) analyzeShellTool(ev codingagent.StreamEvent, sessionID, turnID, correlationID string) []*store.SystemArtifactEvent {
	cmd := ExtractShellCommand(ev.ToolName, ev.ToolInput)
	if cmd == "" {
		return nil
	}
	status, _ := ev.ToolInput[shellExecStatusKey].(string)
	if status == shellExecStatusStarted {
		return nil
	}
	ops := ParseShellCommand(cmd)
	if len(ops) == 0 {
		return nil
	}
	ready := status == shellExecStatusCompleted ||
		(status == "" && ev.ToolName == "command_execution")
	if ready {
		return a.emitShellOps(sessionID, turnID, correlationID, ev.ToolName, ops)
	}
	// Defer Bash / legacy shell until tool_result
	a.stashPendingShell(sessionID, ev.ToolCallID, pendingShellOps{
		toolName: ev.ToolName, turnID: turnID, correlationID: correlationID, ops: ops,
	})
	return nil
}
```

### 統合 / E2E

#### [MODIFY] [tests/artifact_pagination_test.go](file://tests/artifact_pagination_test.go)

*   **Description**: `TestShellParser_IgnoresDevNull` を存在ゲートに適合。
*   **Logic**: workDir に `out.txt` を事前作成するか、Bash + tool_result 後に検証。`/dev/null` が key に出ないことは維持。`out.txt` はファイル存在時のみ出る。

#### [NEW] [tests/shell_parser_existence_guard_e2e_test.go](file://tests/shell_parser_existence_guard_e2e_test.go)

*   **Description**: TaskLog 経由で Analyzer + SQLite ArtifactStore の E2E。
*   **Logic**:
    *   `TestE2E_ShellParser_ExistenceGuard_KeepsExistingCreate`: workDir に `actual.txt` 作成 → `command_execution` completed `echo hi > actual.txt` → List に `actual.txt`
    *   `TestE2E_ShellParser_ExistenceGuard_DropsMissingCreate`: completed で存在しない絶対 path → List にその base/key 無し
    *   `TestE2E_ShellParser_ExistenceGuard_BashDefer`: Bash tool_use（未作成）→ empty。ファイル作成 + tool_result → `new.txt` あり
    *   `TestE2E_ShellParser_ExistenceGuard_DeleteWithoutFile`: `rm gone.txt` completed → delete 記録

## Step-by-Step Implementation Guide

1. **[x] RED: Analyzer shell existence tests**: Edit `analyzer_test.go` — U1–U5 と既存 shell テスト更新。`./scripts/process/build.sh --skip-frontend --skip-etc` で失敗を確認。
2. **[x] RED: Protocol tests**: Edit `claudecode/protocol_test.go`, `codex/protocol_test.go` for ToolCallID / execution_status.
3. **[x] GREEN: event.go + protocols**: Add `ToolCallID`; Claude id 伝播; Codex `execution_status`.
4. **[x] GREEN: shell_emit.go + analyzer.go**: pathExists, emitShellOps, pending, tool_result flush.
5. **[x] Fix integration TestShellParser_IgnoresDevNull**: Update `artifact_pagination_test.go`.
6. **[x] E2E**: Add `tests/shell_parser_existence_guard_e2e_test.go`.
7. **[x] Verify**: `./scripts/process/build.sh` then `./scripts/process/integration_test.sh --specify "ShellParser|FileChangeCollectors|ExistenceGuard"`.
8. **[/] Push**: 全成功後 `git push`.

各ステップ完了時に commit（英語メッセージ）。

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**: `./scripts/process/build.sh`
2. **Integration Tests**: `./scripts/process/integration_test.sh --specify "ShellParser|ExistenceGuard|FileChangeCollectors"`
3. **E2E Tests**: `tests/shell_parser_existence_guard_e2e_test.go`（上記 `--specify "ExistenceGuard"` に含まれる）

## Documentation

- `docs/ReferenceManual-WebAPIs.md` の Tier2（`shell_parser`）説明に 1 文追加: create/update は解決 path が存在するときのみ記録し、delete は parser 結果を維持。Bash は tool_result 後に確定。
- README の file_change_collectors 表付近に同様の短い注記（既に shell_parser 説明がある場合は追記のみ）。
