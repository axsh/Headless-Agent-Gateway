# 005-ShellParser-DevNull-FalsePositive

> **Source Specification**: `prompts/phases/000-foundation/branches/check-atrifact-limitation/ideas/002-ShellParser-DevNull-FalsePositive.md`  
> **Depends on**: None（他 Part と独立。ExecutionOrder 上は最初に実施推奨）

## Goal Description

シェルコマンド解析が `/dev/null`・`NUL` 等へのリダイレクト / tee をファイル作成・更新と誤認し、System Artifact 一覧に `null` キーが載る問題を修正する。

## User Review Required

None. 除外リストは仕様 R2 のとおり（`/dev/fd/*` 等の O1 は本 Part 対象外）。

## Requirement Traceability

| Requirement (from Spec 002) | Implementation Point (Section/File) |
| :--- | :--- |
| R1 特殊デバイスを記録しない | Proposed Changes > command_parser.go `isIgnoredArtifactPath` |
| R2 除外対象リスト | Proposed Changes > `isIgnoredArtifactPath` の case 群 |
| R3 `2>/dev/null` 等 | 同一フィルタ（`>` キャプチャ後に無視） |
| R4 混在コマンドは実ファイルのみ | Proposed Changes > テストケース |
| R5 単体テスト固定 | Proposed Changes > command_parser_test.go |
| O1/O2 | 対象外 |

## Proposed Changes

### Unit tests first (TDD)

#### [MODIFY] [shared/libs/go/artifact/analyzer/command_parser_test.go](file://shared/libs/go/artifact/analyzer/command_parser_test.go)

*   **Description**: 仕様の表をテーブル駆動ケースとして追加。既存 `echo hello > output.txt` は回帰として維持。
*   **Logic**:

```go
// additional cases inside TestParseShellCommand or new TestParseShellCommand_IgnoresDevNull:
{name: "dev null create", cmd: "echo hi > /dev/null", want: nil},
{name: "dev null append", cmd: "echo hi >> /dev/null", want: nil},
{name: "stderr to null", cmd: "cmd 2>/dev/null", want: nil},
{name: "stdout and stderr null", cmd: "cmd >/dev/null 2>&1", want: nil},
{name: "mixed null and file", cmd: "echo hi > /dev/null && echo x > real.txt",
 want: []analyzer.ParsedFileOp{{Path: "real.txt", Operation: store.OperationCreate}}},
{name: "tee null", cmd: "cat foo | tee /dev/null", want: nil},
{name: "windows NUL", cmd: "echo hi >NUL", want: nil},
{name: "windows nul lower", cmd: "echo hi > nul", want: nil},
{name: "dev stdout", cmd: "echo hi > /dev/stdout", want: nil},
{name: "dev stderr", cmd: "echo hi > /dev/stderr", want: nil},
// regression:
{name: "echo redirect create", cmd: "echo hello > output.txt",
 want: []analyzer.ParsedFileOp{{Path: "output.txt", Operation: store.OperationCreate}}},
```

比較時は順序非依存（map 由来）の既存ヘルパーがあればそれを使う。

#### [MODIFY] [shared/libs/go/artifact/analyzer/analyzer_test.go](file://shared/libs/go/artifact/analyzer/analyzer_test.go)（任意だが推奨）

*   **Description**: Bash tool_use で `> /dev/null` を流し、memStore にイベントが載らないことを確認（キー `null` が出ない）。
*   **Logic**:

```go
func TestToolCallAnalyzer_ShellIgnoresDevNull(t *testing.T) {
    // onEntry with ToolName Bash, command "echo hi > /dev/null"
    // store events empty
    // command "echo hi > /dev/null && echo x > real.txt" → only real.txt
}
```

### Parser implementation

#### [MODIFY] [shared/libs/go/artifact/analyzer/command_parser.go](file://shared/libs/go/artifact/analyzer/command_parser.go)

*   **Description**: `add` 内で無視パスをスキップ。
*   **Technical Design / Logic**（仕様スニペットを継承）:

```go
func isIgnoredArtifactPath(path string) bool {
    p := strings.TrimSpace(stripQuotes(path))
    if p == "" {
        return true
    }
    // Normalize slashes for comparison; trim trailing colon (NUL:)
    norm := strings.ToLower(filepath.ToSlash(p))
    norm = strings.TrimSuffix(norm, ":")
    switch norm {
    case "/dev/null", "/dev/stdout", "/dev/stderr", "nul":
        return true
    }
    if strings.EqualFold(p, "NUL") {
        return true
    }
    return false
}

// in add:
add := func(path, op string) {
    path = stripQuotes(path)
    if path == "" || isIgnoredArtifactPath(path) {
        return
    }
    // ... existing dedup by priority ...
}
```

`filepath` / `strings` import を追加。

**一次フィルタは parser のみ**（仕様どおり）。`buildEvent` への二重防御は必須としない。

### Integration test

#### [NEW] [tests/shell_devnull_artifact_test.go](file://tests/shell_devnull_artifact_test.go)

*   **Description**: TaskLog + ToolCallAnalyzer + SQLiteStore でシェルイベントを流し、List に `null` が無いことを確認。
*   **Logic**:

```go
func TestShellParser_IgnoresDevNull(t *testing.T) {
    // NewSQLiteStore, UpsertSession
    // analyzer.New(taskLog, store, projectRoot, resolver)
    // Inject AgentLogEntry with Bash command "echo hi > /dev/null && echo x > out.txt"
    // ListSystemArtifacts → keys contain "out.txt", do not contain "null" or "/dev/null"
}
```

（agentservice フル起動は不要。analyzer+store で十分ならパッケージテスト側に置き、本ファイルは省略可。少なくとも analyzer パッケージテストは必須。）

## Step-by-Step Implementation Guide

1. **Add failing table cases** to `command_parser_test.go`; confirm `./scripts/process/build.sh` fails on those cases.
2. **Implement `isIgnoredArtifactPath`** and gate inside `add`.
3. **Add analyzer-level test** for tool_use path (recommended).
4. **Add tests/ integration** if not covered by package tests alone.
5. **Re-run verification**.

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**: `./scripts/process/build.sh`
2. **Integration Tests**: `./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify 'TestShellParser_IgnoresDevNull|TestReconcile_SessionEndGitSupplement|TestE2E_.*Artifact'`

`TestShellParser_IgnoresDevNull` を `tests/` に置かない場合は、build.sh 内の analyzer 単体で受け入れ、specify から当該名を外す。

## Documentation

変更はコードコメント（`isIgnoredArtifactPath` の GoDoc 1行）で足りる。README 更新は不要。
