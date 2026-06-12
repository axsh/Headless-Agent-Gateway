# 056-Add-ClaudeCode-TernCtl-RealCommand-E2E-Test

> **Source Specification**: [045-Add-ClaudeCode-TernCtl-RealCommand-E2E-Test.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/045-Add-ClaudeCode-TernCtl-RealCommand-E2E-Test.md)

## Goal Description

`exec.Command` で `ternctl` を実コマンドとして起動し、`--agent claudecode` を指定して Claude Code エージェント経由のツール実行結果が ternctl の stdout に正しく表示されることを自動検証する E2E テストを追加する。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: TestE2E_ClaudeCode_TernctlRealCommand テスト追加 | Proposed Changes > agentservice_e2e_test.go |
| R2: startE2EServer (Go API) でサーバ起動 | Proposed Changes > agentservice_e2e_test.go (既存ヘルパー活用) |
| R3: exec.Command で ternctl 起動 (--agent claudecode) | Proposed Changes > agentservice_e2e_test.go |
| R4: stdout に [Tool:], [Tool Result], Session created:, "status": "completed" を検証 | Proposed Changes > agentservice_e2e_test.go |
| R5: exit code 0 の検証 | Proposed Changes > agentservice_e2e_test.go |
| R6: Windows .exe 拡張子解決 | Proposed Changes > agentservice_e2e_test.go |
| R7: claude CLI 前提条件検証 | Proposed Changes > agentservice_e2e_test.go (既存 startE2EServer が実施) |

## Proposed Changes

### E2E テスト (tests/)

#### [MODIFY] [agentservice_e2e_test.go](file://tests/agentservice_e2e_test.go)

*   **Description**: Claude Code エージェントを使用した ternctl 実コマンド実行 E2E テストを追加
*   **Technical Design**:
    *   テスト関数 `TestE2E_ClaudeCode_TernctlRealCommand` を追加
    *   既存ヘルパー `startE2EServer`, `initGitRepo` を活用
    *   ternctl バイナリは Windows 環境での `.exe` 拡張子解決ロジックを含める
    *   `runtime` パッケージを import に追加 (まだ import されていない場合)
*   **Logic**:

    ```go
    // TestE2E_ClaudeCode_TernctlRealCommand starts a tern server via Go API
    // and runs ternctl as a real subprocess with --agent claudecode,
    // verifying that tool use/result events appear in ternctl's stdout.
    func TestE2E_ClaudeCode_TernctlRealCommand(t *testing.T) {
        // ternctl バイナリパスの解決 (Windows .exe 対応)
        ternctlName := "../bin/ternctl"
        if runtime.GOOS == "windows" {
            if _, err := os.Stat(ternctlName + ".exe"); err == nil {
                ternctlName = ternctlName + ".exe"
            }
        }
        ternctlBin, err := filepath.Abs(ternctlName)
        if err != nil {
            t.Fatalf("resolve ternctl path: %v", err)
        }
        if _, err := os.Stat(ternctlBin); err != nil {
            t.Fatalf("ternctl binary not found at %s: %v", ternctlBin, err)
        }

        // Phase 1: tern サーバを Go API で起動
        // startE2EServer は内部で exec.LookPath("claude") を実行して
        // claude CLI の存在を確認する (R7)
        baseURL, cleanup := startE2EServer(t)
        defer cleanup()

        // Phase 2: ternctl を実コマンドとして起動
        tmpDir := t.TempDir()
        workDir := filepath.Join(tmpDir, "work")
        os.MkdirAll(workDir, 0755)
        // Claude Code は git リポジトリを必要とするため initGitRepo を実行
        initGitRepo(t, workDir)

        ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
        defer cancel()

        ternctlCmd := exec.CommandContext(ctx, ternctlBin,
            "--server", baseURL,
            "run",
            "--agent", "claudecode",      // Claude Code エージェントを指定
            "--prompt", "please run 'echo hello' command and report the result.",
            "--work-dir", workDir,
        )
        output, err := ternctlCmd.CombinedOutput()
        outputStr := string(output)
        t.Logf("ternctl output:\n%s", outputStr)

        // Phase 3: stdout 出力の検証
        if err != nil {
            t.Fatalf("ternctl exited with error: %v\noutput: %s", err, outputStr)
        }
        if !strings.Contains(outputStr, "Session created:") {
            t.Error("expected 'Session created:' in output")
        }
        if !strings.Contains(outputStr, "[Tool:") {
            t.Error("expected '[Tool: ...]' in output (tool use event)")
        }
        if !strings.Contains(outputStr, "[Tool Result]") {
            t.Error("expected '[Tool Result] ...' in output (tool result event)")
        }
        if !strings.Contains(outputStr, `"status": "completed"`) {
            t.Error("expected session status 'completed' in output")
        }
    }
    ```

    **Codex 版との差分**:

    | 項目 | Codex (TestCodexE2E_TernctlRealCommand) | Claude Code (TestE2E_ClaudeCode_TernctlRealCommand) |
    | :--- | :--- | :--- |
    | CLI 前提条件 | `exec.LookPath("codex")` | `startE2EServer` 内で `exec.LookPath("claude")` |
    | `--agent` 値 | `codex` | `claudecode` |
    | git init | 不要 | `initGitRepo(t, workDir)` が必要 |
    | `.codex` ディレクトリ | `os.MkdirAll(workDir+"/.codex", 0755)` | 不要 |

## Step-by-Step Implementation Guide

### Step 1: E2E テストコードの追加

*   `tests/agentservice_e2e_test.go` の末尾に `TestE2E_ClaudeCode_TernctlRealCommand` を追加する
*   `runtime` パッケージが import されていない場合は追加する
*   上記 Logic セクションの Go コードをそのまま実装する

### Step 2: ビルド

*   全体ビルドを実行し、コンパイルエラーがないことを確認する:
    ```bash
    ./scripts/process/build.sh
    ```

### Step 3: Claude Code ternctl E2E テスト実行

*   新規テストのみを実行する:
    ```bash
    ./scripts/process/integration_test.sh --specify "TestE2E_ClaudeCode_TernctlRealCommand"
    ```
*   ternctl の stdout に `[Tool:`, `[Tool Result]`, `Session created:`, `"status": "completed"` が含まれることを確認する
*   失敗時は修正して該当テストのみ再実行する

### Step 4: リグレッション確認

*   Claude Code E2E テスト全体を実行する:
    ```bash
    ./scripts/process/integration_test.sh --specify "TestE2E_CodingAgent"
    ```
*   Codex E2E テスト全体を実行する:
    ```bash
    ./scripts/process/integration_test.sh --specify "TestCodexE2E"
    ```

### Step 5: コミット + プッシュ

*   コミット:
    ```bash
    git add tests/agentservice_e2e_test.go
    git commit -m "test: add Claude Code ternctl real command E2E test"
    ```
*   全テスト成功後にプッシュ:
    ```bash
    git push
    ```

### Step 6: Verification Plan 実行

*   以下の Verification Plan を実行して総合判定を行う

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    全体ビルドと単体テストを実行する。
    ```bash
    ./scripts/process/build.sh
    ```

2.  **Integration Tests (新規 E2E テスト)**:
    ```bash
    ./scripts/process/integration_test.sh --specify "TestE2E_ClaudeCode_TernctlRealCommand"
    ```
    *   **Log Verification**: ternctl の stdout 出力ログに以下が含まれること:
        *   `Session created:` -- セッション作成成功
        *   `[Tool:` -- ツール使用イベント (tool_use) の SSE 受信
        *   `[Tool Result]` -- ツール結果イベント (tool_result) の SSE 受信
        *   `"status": "completed"` -- セッション完了ステータス

3.  **Regression Tests (Claude Code E2E)**:
    ```bash
    ./scripts/process/integration_test.sh --specify "TestE2E_CodingAgent"
    ```
    *   **Log Verification**: 既存の `TestE2E_CodingAgentStreaming`, `TestE2E_CodingAgentError`, `TestE2E_CodingAgentDefaultModel` が PASS すること

4.  **Regression Tests (Codex E2E)**:
    ```bash
    ./scripts/process/integration_test.sh --specify "TestCodexE2E"
    ```
    *   **Log Verification**: `TestCodexE2E_TernctlRealCommand` を含む全 Codex E2E テストが PASS すること

### E2E Tests

#### [MODIFY] [agentservice_e2e_test.go](file://tests/agentservice_e2e_test.go)

*   **テストケース**: `TestE2E_ClaudeCode_TernctlRealCommand`
*   **検証ポイント**:
    1. `startE2EServer` でサーバが正常に起動する
    2. `exec.Command` で `ternctl run --agent claudecode --prompt ...` が正常に実行される
    3. ternctl の stdout に `[Tool: ...]` が含まれる (Claude Code の tool_use イベントが SSE 経由で正しくレンダリングされる)
    4. ternctl の stdout に `[Tool Result] ...` が含まれる (Claude Code の tool_result イベントが SSE 経由で正しくレンダリングされる)
    5. ternctl の stdout に `Session created:` が含まれる
    6. ternctl の stdout に `"status": "completed"` が含まれる
    7. ternctl の exit code が 0 である

### テスト項目のセルフレビュー (S11.4)

1. **網羅性の検証**: E2E テストで `tern server -> claudecode adapter -> claude CLI -> SSE -> ternctl -> stdout` の全パイプラインを検証。ternctl の stdout 出力に `[Tool:]`, `[Tool Result]`, `Session created:`, `"status": "completed"` を確認することで、各段階の動作が確認できる。
2. **証拠の十分性**: ternctl の stdout はテスト内で `CombinedOutput()` でキャプチャし、`strings.Contains` で具体的なパターンを検証する。失敗時は stdout 全体がログに出力される。
3. **迂回・抜け道の排除**: `exec.Command` で実バイナリ `ternctl` を起動するため、Go の HTTP クライアントによる直接 SSE 読み取りでは検出できない問題 (stream.go のレンダリングバグ等) も検出可能。
4. **依存関係の整合性**: ビルド -> E2E テスト -> リグレッション確認の順序で実行。前段が失敗した場合は後段を実行しない。

### 総合判定プロセス (S12)

全テスト完了後、testing-rules.md S12.2 のチェック項目を確認し、S12.3 のフォーマットで総合判定結果を記録する。

## Documentation

#### [MODIFY] [045-Add-ClaudeCode-TernCtl-RealCommand-E2E-Test.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/045-Add-ClaudeCode-TernCtl-RealCommand-E2E-Test.md)

*   **更新内容**: 実装完了後、検証結果を仕様書の検証シナリオセクションに反映する。
