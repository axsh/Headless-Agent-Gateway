# 045-Add-ClaudeCode-TernCtl-RealCommand-E2E-Test

## 背景 (Background)

Codex CLI の E2E テスト (`TestCodexE2E_TernctlRealCommand`) では、`startE2EServer` で tern サーバを Go API で起動し、`exec.Command` で `ternctl` を実コマンドとして起動して、stdout 出力に `[Tool: ...]` や `[Tool Result]` が正しく含まれることを検証している。

しかし、Claude Code (`claudecode`) エージェントに対する同等の実コマンド E2E テストは存在しない。現在の Claude Code E2E テスト (`TestE2E_CodingAgentStreaming` 等) は Go の HTTP クライアントから直接 SSE を受信して `StreamEvent` を解析するが、**ternctl を経由した stdout 表示の検証は行っていない**。

ternctl は `stream.Output()` メソッドでイベントをレンダリングしており、Codex と Claude Code で異なるイベント構造が SSE で返されるため、Claude Code 側でも ternctl 経由の表示が正しく動作することを確認する必要がある。

## 要件 (Requirements)

### 必須要件

- **R1**: `TestE2E_ClaudeCode_TernctlRealCommand` テスト関数を追加する
- **R2**: `startE2EServer` (Go API) で tern サーバを起動する (Codex の E2E テストと同じパターン)
- **R3**: `exec.Command` で `bin/ternctl` を実コマンドとして起動する (`--agent claudecode`)
- **R4**: ternctl の stdout に以下が含まれることを検証する:
  - `Session created:` -- セッション作成の確認
  - `[Tool:` -- ツール使用イベント (tool_use)
  - `[Tool Result]` -- ツール結果イベント (tool_result)
  - `"status": "completed"` -- セッション完了ステータス
- **R5**: ternctl の exit code が 0 であることを検証する
- **R6**: Windows 環境での `.exe` 拡張子解決ロジックを含める (`runtime.GOOS == "windows"`)
- **R7**: Claude Code は `claude` CLI を使用するため、`exec.LookPath("claude")` で前提条件を検証する

### 任意要件

- **O1**: テストのプロンプトは `echo hello` コマンド実行を依頼するシンプルなものとする (Codex のテストと同様)
- **O2**: 既存の Claude Code E2E テスト (`TestE2E_CodingAgentStreaming` 等) との重複を避け、ternctl 経由の stdout 表示に焦点を当てる

## 実現方針 (Implementation Approach)

### テスト構造

`TestCodexE2E_TernctlRealCommand` と同じパターンで実装する:

1. **Phase 1**: `startE2EServer(t)` で tern サーバを起動
2. **Phase 2**: `exec.CommandContext(ctx, ternctlBin, "--server", baseURL, "run", "--agent", "claudecode", "--prompt", "...", "--work-dir", workDir)` で ternctl を実行
3. **Phase 3**: stdout 出力の検証

### 配置ファイル

テストは [agentservice_e2e_test.go](file://tests/agentservice_e2e_test.go) に追加する (Claude Code 関連テストの所在)。

### ternctl 出力の仕組み

ternctl はサーバの SSE ストリームを受信し、`stream.Output()` で以下のようにレンダリングする:

| SSE EventType | ternctl stdout |
| :--- | :--- |
| `text` | そのまま出力 |
| `tool_use` | `\n[Tool: {tool_name}]\n` |
| `tool_result` | `[Tool Result] {content}\n` |
| `system` | `[System] {text}\n` |
| `error` | (lastErr に記録) |
| `result` | (出力なし) |

### Claude Code のイベントフロー

Claude Code の `ParseJSONLinesEvent` は以下のようにイベントをマッピングする:

- `type: "system"` + `subtype: "init"` -> `EventSystem` (session_id 含む)
- `type: "assistant"` + `content[].type: "tool_use"` -> `EventToolUse`
- `type: "user"` + `content[].type: "tool_result"` -> `EventToolResult`
- `type: "stream_event"` + `delta.text` -> `EventText`
- `type: "result"` -> `EventResult`

### 事前条件

- `claude` CLI が PATH 上にあること
- `bin/ternctl` (または `bin/ternctl.exe`) がビルド済みであること
- API キーが vault に登録済みであること

## 検証シナリオ (Verification Scenarios)

### シナリオ 1: ternctl 経由の Claude Code E2E テスト

1. `startE2EServer(t)` で tern サーバを起動する
2. 一時ディレクトリを作成し、`git init` を実行する (Claude Code は git リポジトリを必要とする)
3. `ternctl run --agent claudecode --prompt "please run 'echo hello' command and report the result." --work-dir <work_dir>` を `exec.Command` で実行する
4. ternctl の stdout に `Session created:` が含まれることを確認する
5. ternctl の stdout に `[Tool:` が含まれることを確認する
6. ternctl の stdout に `[Tool Result]` が含まれることを確認する
7. ternctl の stdout に `"status": "completed"` が含まれることを確認する
8. ternctl の exit code が 0 であることを確認する

### シナリオ 2: リグレッション確認

1. 既存の Claude Code E2E テスト (`TestE2E_CodingAgentStreaming` 等) が引き続き PASS すること
2. Codex E2E テスト (`TestCodexE2E_TernctlRealCommand` 等) が引き続き PASS すること

## テスト項目 (Testing for the Requirements)

### 自動検証コマンド

```bash
# 1. 全体ビルド + 単体テスト
./scripts/process/build.sh

# 2. Claude Code ternctl E2E テストのみ実行
./scripts/process/integration_test.sh --specify "TestE2E_ClaudeCode_TernctlRealCommand"

# 3. Claude Code E2E リグレッション確認
./scripts/process/integration_test.sh --specify "TestE2E_CodingAgent"

# 4. Codex E2E リグレッション確認
./scripts/process/integration_test.sh --specify "TestCodexE2E"
```

### 注意事項

- Claude Code は `git init` 済みのワークディレクトリを必要とする場合がある (`initGitRepo` ヘルパーを使用)
- Claude Code のテストは Codex と比較して実行時間が長い場合がある (タイムアウトは 120 秒に設定)
- Windows 環境では `ternctl.exe` バイナリの存在確認が必要
