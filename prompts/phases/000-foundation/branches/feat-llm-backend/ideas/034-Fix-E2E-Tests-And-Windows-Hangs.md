# 034-Fix-E2E-Tests-And-Windows-Hangs

## 背景 (Background)

Windowsローカル環境でのE2Eテスト実行時、以下の2つの根本的な問題によりテストが失敗またはタイムアウト・ハングする。

1. **`TestE2E_CodingAgentDefaultModel` の失敗**:
   Claude CLIのサンドボックスが有効な状態（デフォルト）で動作した際、サンドボックス内の仮想ファイルシステムからホスト側の物理一時ディレクトリ（`C:\Users\yamya\AppData\Local\Temp\...`）へのファイル共有・同期が Windows のセキュリティやマウント制限により動作しない。このため、作成指示した `test.txt` が `work_dir` 内に反映されず、アサーションエラーとなる。
2. **`TestE2E_SessionContinuation` のタイムアウト・ハング**:
   - セッション継続テストにおいて、2回目の起動時に `CLAUDE_CONFIG_DIR` が設定されていないため、以前のセッション情報の解決に失敗する、あるいはサンドボックス環境内からのファイル一覧ツールの実行がWindows特有の制限によりエラーになり、LLMが修復を試みてAPIリクエストのリトライループ（無限ループ）に陥り、2分間のタイムアウト上限に達する。
   - タイムアウト発生時のテストクリーンアップ（`srv.Shutdown()`）において、Windows環境では `os.Process.Kill()` を呼び出しても Node.js などの子プロセスツリー（子孫プロセス）を強制終了しきれず、パイプが開いたままになり `cmd.Wait()` で永久に待機してハングが発生する。

## 要件 (Requirements)

1. **テスト実行時のサンドボックス無効化 (Disable Sandbox in E2E Tests)**:
   - E2Eテストにおいて、ホスト側物理一時ディレクトリへのファイル作成および読み取り確認が確実に機能するよう、テストで構築されるエージェント（`claudecode` 等）のサンドボックスを無効化する。
   - 具体的には、`startE2EServer` 内でエージェントのアダプタを生成する際、`AdapterConfig.DisableSandbox` を `true` に設定する。

2. **E2Eテストでのセッションディレクトリの明示的指定 (Session Directory Config for E2E Tests)**:
   - テストセッション間でセッション継続（`--resume`）が正しく機能するように、各E2Eテストセッションごとに隔離されたセッション保存ディレクトリを割り当てる。
   - 具体的には、`createE2ESession` および `createE2ESessionNoModel` などのテストヘルパー関数において、リクエストパラメータとして `session_dir` (テスト一時ディレクトリの下に作成した `sessions/` サブフォルダ) を指定してセッションを作成し、`CLAUDE_CONFIG_DIR` を確実に伝搬させる。

3. **Windows環境下におけるプロセスツリーの強制クリーンアップ (Robust Process Termination on Windows)**:
   - Windows環境で `ProcessManager.Stop()` が呼び出された際、Node.js などの実行中プロセスとその子孫プロセスツリーを確実に強制終了させる。
   - `runtime.GOOS == "windows"` の場合、標準の `pm.cancel()` に加えて、Windowsのシステムコマンド `taskkill /F /T /PID <pid>` を呼び出して再帰的にプロセスツリーを終了させる。これにより、ハング状態を解消し、テストが確実にクリーンアップされるようにする。

## 実現方針 (Implementation Approach)

### 1. テストコードの修正
- **[agentservice_e2e_test.go](file:///tests/agentservice_e2e_test.go)** などの初期化部を修正：
  - `startE2EServer()` 内で `claudecode.New()` に渡す `AdapterConfig` で `DisableSandbox: true` を明示。
  - `createE2ESession`, `createE2ESessionNoModel` 内のリクエスト生成部で、`session_dir` パラメータとして `filepath.Join(workDir, "sessions")` を追加。

### 2. プロダクションコードの修正
- **[process.go (claudecode)](file:///shared/libs/go/codingagent/claudecode/process.go)** の `Stop()` メソッド：
  - `runtime.GOOS == "windows"` のブロックにおいて、`taskkill /F /T /PID <pid>` の実行を追加し、子プロセスツリーを強制終了した後に `pm.cmd.Wait()` を呼び出す。
  - 必要に応じて `strconv` パッケージのインポートを追加。
- **[process.go (codex)](file:///shared/libs/go/codingagent/codex/process.go)** が存在する場合、同様のWindowsプロセス終了処理の見直しを適用。

## 検証シナリオ (Verification Scenarios)

1. 全体ビルドを実行し、コンパイルエラーが無いことを検証する。
   ```bash
   ./scripts/process/build.sh
   ```
2. 統合・E2Eテストを実行する。
   ```bash
   ./scripts/process/integration_test.sh
   ```
   すべてのE2Eテスト（`TestE2E_CodingAgentDefaultModel`, `TestE2E_SessionContinuation` を含む）が正常に `PASS` し、途中でハングすることなくプロセスが正常にクリーンアップされることを確認する。

## テスト項目 (Testing for the Requirements)

- **自動テスト**:
  ```bash
  ./scripts/process/integration_test.sh
  ```
  すべてのE2Eテストが正常終了することを確認。
