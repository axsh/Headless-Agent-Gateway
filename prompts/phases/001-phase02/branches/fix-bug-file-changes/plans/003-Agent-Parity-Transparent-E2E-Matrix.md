# 003-Agent-Parity-Transparent-E2E-Matrix

> **Source Specification**: `prompts/phases/001-phase02/branches/fix-bug-file-changes/ideas/003-Agent-Parity-Transparent-E2E-Matrix.md`

## Goal Description

Coding Agent（`claudecode` / `codex`）を行として差し替えるだけで、同一の E2E ケース本体（P-FileCreate / P-Ternctl）と非 LLM フィクスチャ行列を実行し、共通契約（C-Stream / C-Artifact / C-Status / C-Ternctl）の透過性をテストコードとして固定する。

## User Review Required

- **既存個別 E2E の削除は行わない**（O1 委譲は任意・本計画では未実施）。マトリクス新規追加のみ。
- **モデル**: Claude 行は `e2eDefaultModel`（`claude-sonnet-4-6`）、Codex 行は `gpt-4o`。ケース本文・プロンプト文字列は同一。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| M1 テーブル駆動ライブ | `tests/agent_parity_matrix_e2e_test.go` parityAgents + t.Run |
| M2 P-FileCreate / P-Ternctl | 同ファイル `runParityFileCreate` / `runParityTernctl` |
| M3 フィクスチャ行列 | 同ファイル `TestAgentParityMatrix_FixtureWriteList` |
| M4 specify 入口 | Verification Plan |
| M5 001/002 整合 | ヘルパ再利用・Write→List |

## Proposed Changes

### tests — マトリクス本体（M1–M3）

#### [NEW] [tests/agent_parity_matrix_e2e_test.go](file://tests/agent_parity_matrix_e2e_test.go)

*   **Description**: エージェント差し替え透過 E2E の単一入口。
*   **Technical Design**:

```go
type parityAgentRow struct {
    Agent   string
    Model   string
    CLIName string // LookPath binary name
}

var parityAgents = []parityAgentRow{
    {Agent: "claudecode", Model: e2eDefaultModel, CLIName: "claude"},
    {Agent: "codex", Model: "gpt-4o", CLIName: "codex"},
}

func requireParityCLI(t *testing.T, cliName string) {
    t.Helper()
    if _, err := exec.LookPath(cliName); err != nil {
        t.Skipf("%s CLI not on PATH: %v", cliName, err)
    }
}

// startParityMatrixServer starts tern with CreateAll agents.
// Does NOT Fatal on missing claude; callers Skip per-row via requireParityCLI.
func startParityMatrixServer(t *testing.T) (baseURL string, cleanup func())

func runParityFileCreate(t *testing.T, row parityAgentRow)
func runParityTernctl(t *testing.T, row parityAgentRow)

func TestAgentParityMatrix_FileCreate(t *testing.T)
func TestAgentParityMatrix_Ternctl(t *testing.T)
func TestAgentParityMatrix_FixtureWriteList(t *testing.T)
```

*   **Logic**:
    *   **P-FileCreate**（各行同一）:
      1. `requireParityCLI(row.CLIName)`
      2. `startParityMatrixServer`（または既存 start の CLI Fatal を避ける新ヘルパ）
      3. `workDir := t.TempDir()`; `createE2ESessionWithModel(..., row.Agent, row.Model, workDir)`
      4. `prompt := fileCreatePrompt(workDir, "parity_hello.txt", "Parity Hello")` — 両行同一
      5. `sendE2EMessage` → `parseE2ESSEEvents` → `assertParitySSEDone`
      6. エラーイベントは Fail（モデル一時障害の Skip は **両行共通条件**に限る。エージェント名分岐の Skip 禁止）
      7. `assertParityWorkFileExists(t, workDir, "parity_hello.txt", events)`
      8. `assertParitySessionCompleted(t, baseURL, sessionID)`
    *   **P-Ternctl**（各行同一）:
      1. CLI + `../bin/ternctl` 解決（既存 Codex/Claude Ternctl と同じ Windows .exe 処理をローカル関数 `resolveTernctlBin` に集約）
      2. `ternctl run --agent <row.Agent> --prompt "please run 'echo hello' ..." --work-dir ...`
      3. 成功判定: `Session created:` かつ `[Tool:` かつ（`[Tool Result]` **または** echo 痕跡）かつ status completed|active — **両行同じ判定**
    *   **FixtureWriteList**:
      1. `for _, name := range []string{"claudecode", "codex"}`
      2. `turnDiffFakeAgent{name, events: Write hello.txt}` + `startTurnDiffArtifactServer`
      3. `postSSEMessage` → `AssertSystemArtifactPathsContain(..., "hello.txt")` + `tool_name=Write`

#### [MODIFY] [tests/e2e_agent_parity_helpers_test.go](file://tests/e2e_agent_parity_helpers_test.go)

*   **Description**: 必要ならコメントでマトリクス利用を明記。ロジック変更は最小（既存 `fileCreatePrompt` / `assertParity*` をそのまま使う）。

### agentservice / 本番コード

*   **変更なし**（試験ハーネスのみ）。

## Step-by-Step Implementation Guide

1. [x] **Scaffold**: `tests/agent_parity_matrix_e2e_test.go` に `parityAgentRow` / `requireParityCLI` / `startParityMatrixServer` を追加。
2. [x] **Fixture first (TDD)**: `TestAgentParityMatrix_FixtureWriteList` を実装し、LLM なしで PASS。
3. [x] **FileCreate matrix**: `runParityFileCreate` + `TestAgentParityMatrix_FileCreate`。
4. [x] **Ternctl matrix**: `resolveTernctlBin` + `runParityTernctl` + `TestAgentParityMatrix_Ternctl`。
5. [x] **Verification Plan 実行**。
6. [/] **Commit / push**。

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**: `./scripts/process/build.sh`
2. **Integration — Matrix**: `./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify 'TestAgentParityMatrix'`
3. **Integration — 既存非回帰**: `./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify 'TestE2E_CodingAgentStreaming$|TestCodexE2E_FileCreation$'`

（Linux / Remote-SSH Linux: `build.sh --skip-etc`、統合は `xvfb-run -a`。）

### E2E コード化

*   `tests/agent_parity_matrix_e2e_test.go`（必須）

## Documentation

*   仕様 001 の VS-5 に「003 マトリクスで再確認」を 1 行追記（任意）。
*   本計画・仕様 003 自体が正本。
