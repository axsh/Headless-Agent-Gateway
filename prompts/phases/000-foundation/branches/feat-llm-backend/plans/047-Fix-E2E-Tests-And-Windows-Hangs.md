# 047-Fix-E2E-Tests-And-Windows-Hangs

> **Source Specification**: [034-Fix-E2E-Tests-And-Windows-Hangs.md](file:///prompts/phases/000-foundation/branches/feat-llm-backend/ideas/034-Fix-E2E-Tests-And-Windows-Hangs.md)

## Goal Description

Windows環境でのE2Eテスト実行時に発生する2つの根本的問題を修正する:

1. **サンドボックスによるファイル作成失敗**: `TestE2E_CodingAgentDefaultModel` 等でClaude CLIサンドボックスが有効な場合、仮想ファイルシステムからホスト側一時ディレクトリへのファイル反映が失敗する。
2. **プロセスツリー未終了によるハング**: `TestE2E_SessionContinuation` のクリーンアップ時、Windows上で `os.Process.Kill()` が子孫プロセスを終了しきれず `cmd.Wait()` で永久に待機する。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| テスト実行時のサンドボックス無効化 | Proposed Changes > テストコード > agentservice_e2e_test.go |
| セッションディレクトリの明示的指定 | Proposed Changes > テストコード > agentservice_e2e_test.go |
| プロセスツリーの強制クリーンアップ (claudecode) | Proposed Changes > プロダクションコード > claudecode/process.go |
| プロセスツリーの強制クリーンアップ (codex) | Proposed Changes > プロダクションコード > codex/process.go |

## Proposed Changes

### プロダクションコード (claudecode)

#### [MODIFY] [process.go](file:///shared/libs/go/codingagent/claudecode/process.go)

*   **Description**: `Stop()` メソッドのWindows分岐を修正し、`taskkill /F /T /PID <pid>` でプロセスツリーを再帰的に強制終了する。
*   **Technical Design**:
    *   `Stop()` メソッドの `runtime.GOOS == "windows"` ブロックを修正。
    *   現状のコード (L204-208):
        ```go
        if runtime.GOOS == "windows" {
            pm.cancel()
            return pm.cmd.Wait()
        }
        ```
    *   修正後:
        ```go
        if runtime.GOOS == "windows" {
            pid := pm.cmd.Process.Pid
            pm.logger.Debug("killing process tree on Windows", "pid", pid)
            // taskkill /F /T /PID <pid> で子プロセスツリーを再帰的に強制終了
            killCmd := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid))
            if killErr := killCmd.Run(); killErr != nil {
                pm.logger.Debug("taskkill failed (process may have already exited)", "error", killErr)
            }
            pm.cancel()
            // cmd.Wait() は goroutine 内で既に呼び出されているため、
            // ここでは重複呼び出しを避ける。cancel() によるコンテキストキャンセルと
            // taskkill によるプロセス終了で goroutine は自然に完了する。
            return nil
        }
        ```
*   **Logic**:
    *   `pm.cmd.Process.Pid` からプロセスIDを取得。
    *   `taskkill /F /T /PID <pid>` を実行してNode.js子プロセスツリーを含む全プロセスを再帰的に強制終了。
    *   `taskkill` が失敗する場合(既にプロセスが終了している場合等)はログに記録して続行。
    *   `pm.cancel()` でコンテキストをキャンセルし、goroutine内の `cmd.Wait()` を完了させる。
    *   **重要**: `cmd.Wait()` は `StartProcess` 内の goroutine (L171) で既に呼び出されている。`Stop()` で再度 `pm.cmd.Wait()` を呼ぶと、二重呼び出しとなりパニックする可能性がある。`taskkill` + `cancel()` の組み合わせにより、goroutine 内の `cmd.Wait()` が正常終了する形で処理する。
    *   `os/exec` パッケージは既にインポート済み、`strconv` も既にインポート済みのため、追加インポートは不要。

---

### プロダクションコード (codex)

#### [MODIFY] [process.go](file:///shared/libs/go/codingagent/codex/process.go)

*   **Description**: `Stop()` メソッドのWindows分岐をclaudecodeと同様に修正し、`taskkill /F /T /PID <pid>` を使用する。
*   **Technical Design**:
    *   現状のコード (L216-218):
        ```go
        if runtime.GOOS == "windows" {
            pm.cancel()
            err = pm.cmd.Wait()
        }
        ```
    *   修正後:
        ```go
        if runtime.GOOS == "windows" {
            pid := pm.cmd.Process.Pid
            pm.logger.Debug("killing process tree on Windows", "pid", pid)
            killCmd := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid))
            if killErr := killCmd.Run(); killErr != nil {
                pm.logger.Debug("taskkill failed (process may have already exited)", "error", killErr)
            }
            pm.cancel()
            err = nil
        }
        ```
*   **Logic**: claudecodeと同一のロジック。`taskkill /F /T` でプロセスツリーを強制終了し、`cancel()` でgoroutineのクリーンアップを行う。codexのStop()はその後に `pm.codexHome` のクリーンアップ処理が続くため、`err = nil` として後続処理に影響を与えないようにする。

---

### テストコード

#### [MODIFY] [agentservice_e2e_test.go](file:///tests/agentservice_e2e_test.go)

*   **Description**: `startE2EServer` でサンドボックスを無効化し、セッション継続テストでセッションディレクトリを明示的に指定する。
*   **Technical Design - サンドボックス無効化**:
    *   現状のコード (L97-100):
        ```go
        adapter := claudecode.New(&codingagent.AdapterConfig{
            GatewayURL:   gwURL,
            DefaultModel: e2eDefaultModel,
        })
        ```
    *   修正後:
        ```go
        adapter := claudecode.New(&codingagent.AdapterConfig{
            GatewayURL:     gwURL,
            DefaultModel:   e2eDefaultModel,
            DisableSandbox: true,
        })
        ```
*   **Logic**: `DisableSandbox: true` により `BuildEnv` 内で `CLAUDE_CODE_SKIP_SANDBOX=1` 環境変数が設定され、Claude CLIのサンドボックス機能が無効化される。これにより、ホスト側一時ディレクトリへの直接ファイル作成が可能になる。

*   **Technical Design - セッション継続テストのsession_dir指定**:
    *   `TestE2E_SessionContinuation` (L603-658) のセッション作成部分を修正。
    *   現状のコード (L609):
        ```go
        sessionID := createE2ESession(t, baseURL, "claudecode", workDir)
        ```
    *   修正後:
        ```go
        sessionDir := filepath.Join(workDir, "sessions")
        if err := os.MkdirAll(sessionDir, 0755); err != nil {
            t.Fatalf("create session dir: %v", err)
        }
        sessionID := createE2ESessionWithSessionDir(t, baseURL, "claudecode", workDir, sessionDir)
        ```
*   **Logic**: テスト用の隔離されたセッションディレクトリを `workDir/sessions` に作成し、`createE2ESessionWithSessionDir` ヘルパーを使用してリクエストパラメータとして `session_dir` を指定する。これにより `CLAUDE_CONFIG_DIR` が正しく伝搬され、セッション継続時のセッション情報の解決が確実に行われる。`createE2ESessionWithSessionDir` ヘルパー関数は既に L573-596 に定義済み。

*   **Technical Design - TestE2E_CodingAgentError のサンドボックス無効化**:
    *   `TestE2E_CodingAgentError` (L418-509) は独自に `claudecode.New()` を呼び出しているため、同様に修正が必要。
    *   現状のコード (L469-471):
        ```go
        adapter := claudecode.New(&codingagent.AdapterConfig{
            GatewayURL: fmt.Sprintf("http://localhost:%d", bogusPort),
        })
        ```
    *   修正後:
        ```go
        adapter := claudecode.New(&codingagent.AdapterConfig{
            GatewayURL:     fmt.Sprintf("http://localhost:%d", bogusPort),
            DisableSandbox: true,
        })
        ```
*   **Logic**: エラーテストにおいても、サンドボックスが有効だとWindows固有の問題が発生しうるため、一貫して無効化する。

## Step-by-Step Implementation Guide

1.  **Step 1: claudecode ProcessManager.Stop() の修正**:
    *   Edit [process.go](file:///shared/libs/go/codingagent/claudecode/process.go) の `Stop()` メソッド (L197-225)。
    *   `runtime.GOOS == "windows"` ブロック (L205-208) を、`taskkill /F /T /PID` を使用するロジックに置き換える。
    *   `pm.cmd.Wait()` の直接呼び出しを削除し、`return nil` とする。

2.  **Step 2: codex ProcessManager.Stop() の修正**:
    *   Edit [process.go](file:///shared/libs/go/codingagent/codex/process.go) の `Stop()` メソッド (L208-237)。
    *   `runtime.GOOS == "windows"` ブロック (L216-218) を、Step 1と同様のロジックに置き換える。
    *   `err = pm.cmd.Wait()` を `err = nil` に変更。

3.  **Step 3: startE2EServer のサンドボックス無効化**:
    *   Edit [agentservice_e2e_test.go](file:///tests/agentservice_e2e_test.go) の `startE2EServer` 関数内 (L97-100)。
    *   `AdapterConfig` に `DisableSandbox: true` を追加。

4.  **Step 4: TestE2E_CodingAgentError のサンドボックス無効化**:
    *   Edit [agentservice_e2e_test.go](file:///tests/agentservice_e2e_test.go) の `TestE2E_CodingAgentError` 関数内 (L469-471)。
    *   `AdapterConfig` に `DisableSandbox: true` を追加。

5.  **Step 5: TestE2E_SessionContinuation のsession_dir指定**:
    *   Edit [agentservice_e2e_test.go](file:///tests/agentservice_e2e_test.go) の `TestE2E_SessionContinuation` 関数内 (L606-609)。
    *   `workDir` の直下に `sessions` サブディレクトリを作成する処理を追加。
    *   `createE2ESession` を `createE2ESessionWithSessionDir` に置き換え、`sessionDir` を渡す。

6.  **Step 6: Verification Plan を実行する**。

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```

2.  **Integration Tests (E2E全件)**:
    ```bash
    ./scripts/process/integration_test.sh
    ```
    *   **Log Verification**:
        *   `TestE2E_StandaloneHealth`: `PASS` であること。
        *   `TestE2E_CodingAgentStreaming`: `PASS` であること。`tool_use` イベントと `hello.txt` のファイル作成を確認。
        *   `TestE2E_CodingAgentDefaultModel`: `PASS` であること。`test.txt` のファイル作成がサンドボックス無効化により確実に動作。
        *   `TestE2E_CodingAgentError`: `PASS` であること。エラーイベントの伝搬を確認。
        *   `TestE2E_SessionContinuation`: `PASS` であること。ハングせずにタイムアウト内で完了し、`agent_session_id` が維持されること。
        *   `TestE2E_SessionDirFallback`: `PASS` であること。
        *   `SKIP` の出力がないこと。
        *   ハングやタイムアウトが発生していないこと。

3.  **E2E Tests (新規/追加)**:
    本修正は既存E2Eテストの安定化を目的としており、外部から観測可能な新機能は追加しない。既存の6件のE2Eテスト全てが正常に`PASS`することが検証の主目的である。したがって新規E2Eテストの追加は不要。

### テスト項目の設計と検証観点

#### ボトムアップ確認順序

```
依存関係:  E2Eテスト → startE2EServer → claudecode.New → ProcessManager.Stop

テスト順序:
  Step 1: Build (全パッケージのコンパイル確認)
  Step 2: E2Eテスト全件 (修正したStop()とDisableSandbox設定が統合的に動作すること)
```

#### 観点チェックリスト

| # | 観点 | 確認内容 | 対応テスト |
|---|------|----------|-----------|
| 1 | 正常系の動作確認 | サンドボックス無効化状態でファイル作成が成功する | TestE2E_CodingAgentStreaming, TestE2E_CodingAgentDefaultModel |
| 2 | 異常系 | 到達不能なGatewayに対してエラーイベントが正しく伝搬される | TestE2E_CodingAgentError |
| 3 | 状態遷移の検証 | セッション継続で agent_session_id が保持される | TestE2E_SessionContinuation |
| 4 | 副作用の確認 | テスト完了後にプロセスが残存しない (taskkillによるクリーンアップ) | 全テストのcleanup() |
| 5 | 設定の反映 | DisableSandbox=true が CLAUDE_CODE_SKIP_SANDBOX=1 に反映される | TestE2E_CodingAgentDefaultModel |
| 6 | データの一貫性 | session_dir 未指定時に work_dir にフォールバックする | TestE2E_SessionDirFallback |

#### セルフレビュー結果

1. **網羅性の検証**: 仕様書の3要件 (サンドボックス無効化、セッションディレクトリ指定、プロセスツリー強制終了) すべてが既存E2Eテストの正常動作で検証される。新規テストは不要と判断 (既存テストで既にカバーされている機能の修正のため)。
2. **証拠の十分性**: ファイル作成の成功、SSEイベントの受信、agent_session_idの保持、[DONE]の受信など、実際の値・状態で検証している。
3. **迂回・抜け道の排除**: DisableSandbox は BuildEnv() で環境変数に変換され、実際のCLI起動に反映される。taskkill はOSレベルでプロセスを終了するため迂回の余地はない。
4. **依存関係の整合性**: process.goの修正はE2Eテストのクリーンアップ成功が前提。ビルドが先に成功することで、コンパイルレベルの問題を排除してからE2Eで統合検証する。

## Documentation

仕様書およびドキュメントへの更新は不要。本修正はテストインフラとプロセス管理の内部改善であり、APIや設定の仕様に変更はない。
