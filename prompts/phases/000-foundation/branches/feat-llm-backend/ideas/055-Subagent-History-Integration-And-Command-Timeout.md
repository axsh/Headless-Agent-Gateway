# 055: サブエージェント履歴統合とコマンド実行タイムアウト

## 背景 (Background)

### SubagentExecutor 経路での履歴分離問題

054 で実装したサブセッション履歴のディレクトリ階層化は、**WBS 経路** (`agentNodeExecutor` -> `RunChild`) のみを対象としていた。しかし実際のセッション実行では、もう一つの経路である **SubagentExecutor 経路** (`SubagentExecutor.Execute` -> `RunChild`) も子セッションを生成する。

SubagentExecutor は `uuid.New().String()` でセッションIDを生成し、`HistorySubDir` を設定せずに `RunChild` を呼び出すため、子セッションは親とは無関係なトップレベルセッションとして `.wayfinder/` 直下に作成される:

```
.wayfinder/
  wf-1781499600656846200/         # 親セッション (ternctl run)
    history/
      0000001.json - 000000b.json # 親の全メッセージ (フラット)
  3638c5b8-23fa-40a3-a39f.../     # SubagentExecutor が生成した子セッション
    history/
      0000001.json - 0000005.json # 子の全メッセージ (独立)
```

期待される構造は以下:

```
.wayfinder/
  wf-1781499600656846200/
    history/
      0000001.json - 000000b.json
      000000X/                      # 親の Seq=X に対応するサブディレクトリ
        0000001.json - 0000005.json # 子セッションのメッセージ
```

### execute_command ツールのタイムアウト不在

`execute_command` ツール (`tool_command.go`) は `exec.CommandContext` を使用しているが、context に明示的なタイムアウトを設定していない。その結果、Webサーバーなどの常駐プロセスを起動した場合、`cmd.Run()` がプロセス終了まで永久にブロックし、エージェントの実行ループが停止する。

実際に、Go Invaders ゲームサーバー (`invader-game`, port 8080) がフォアグラウンドで起動され、エージェントが無限にブロックされる事象が発生した。

## 要件 (Requirements)

### 必須要件

#### R1: SubagentExecutor 経路での HistorySubDir 設定

`subagent/subagent_executor.go` の `Execute` メソッドで、子セッション作成時に `HistorySubDir` を設定する。

1. `Execute` メソッドに親セッションの現在の `nextSeq` を渡すインターフェースを追加する
2. `childConfig.HistorySubDir` に親の Seq を 7桁 hex で設定する
3. 子セッションの履歴が親セッションの `history/{parentSeqHex}/` 以下に格納されることを保証する
4. UUID ベースのセッションIDは維持する (子セッションのディレクトリ名として引き続き使用可)

#### R2: execute_command ツールのタイムアウト機構

`tools/tool_command.go` の `newExecuteCommand` にタイムアウトを追加する。

1. デフォルトタイムアウトを設定する (推奨: 120秒)
2. ツール入力パラメータ `timeout_seconds` (オプション) でLLMがタイムアウトを指定できるようにする
3. タイムアウト超過時は以下の動作:
   - プロセスを強制終了する
   - それまでに得られた stdout/stderr を返す
   - タイムアウトした旨のメッセージを付与する (例: `"Command timed out after 120 seconds"`)
4. ツール定義 (`register.go`) の `input_schema` に `timeout_seconds` パラメータを追加する

### 任意要件

#### O1: SubagentExecutor の子セッションディレクトリ名の統一

現在 SubagentExecutor は `uuid.New().String()` でセッションIDを生成するが、WBS 経路の `{parentSessionID}-wbs-{nodeID}` 形式と統一性がない。将来的に命名規則を統一することを検討する。ただし本仕様では必須としない。

## 実現方針 (Implementation Approach)

### 1. SubagentExecutor の HistorySubDir 対応

`SubagentExecutor.Execute` に親の `nextSeq` を渡す方法として、`SubagentExecutor` 構造体にコールバックまたはフィールドを追加する:

```go
type SubagentExecutor struct {
    parentConfig   *AgentRunnerConfig
    llm            LLMClient
    runner         AgentRunner
    hints          *HintGenerator
    summarizer     SummaryStrategy
    logger         logger.Logger
    parentSeqFunc  func() int // 追加: 親の現在 nextSeq を取得するコールバック
}
```

`Execute` メソッド内で `HistorySubDir` を設定:

```go
func (e *SubagentExecutor) Execute(...) (string, error) {
    // ...
    childConfig := &AgentRunnerConfig{
        // ... 既存フィールド ...
        HistorySubDir: fmt.Sprintf("%07x", e.parentSeqFunc()),
    }
    // ...
}
```

`AgentCore` が `SubagentExecutor` を作成する際に `parentSeqFunc` を注入:

```go
subExec := subagent.NewSubagentExecutor(cfg, llm, runner, log,
    subagent.WithParentSeqFunc(func() int { return ac.nextSeq }),
)
```

### 2. execute_command タイムアウト

```go
func newExecuteCommand(tc *ToolContext) ToolHandler {
    return func(ctx context.Context, input map[string]any) (string, error) {
        commandLine, _ := input["command"].(string)
        if commandLine == "" {
            return "", fmt.Errorf("execute_command: command is required")
        }

        // Determine timeout.
        timeout := 120 * time.Second // Default: 120 seconds
        if t, ok := input["timeout_seconds"].(float64); ok && t > 0 {
            timeout = time.Duration(t) * time.Second
        }

        // Create timeout context.
        execCtx, cancel := context.WithTimeout(ctx, timeout)
        defer cancel()

        cmd := exec.CommandContext(execCtx, "sh", "-c", commandLine)
        cmd.Dir = tc.WorkDir
        var stdout, stderr bytes.Buffer
        cmd.Stdout = &stdout
        cmd.Stderr = &stderr

        err := cmd.Run()
        result := stdout.String()
        if stderr.Len() > 0 {
            result += "\nSTDERR:\n" + stderr.String()
        }
        if execCtx.Err() == context.DeadlineExceeded {
            result += fmt.Sprintf("\nCommand timed out after %d seconds", int(timeout.Seconds()))
        } else if err != nil {
            result += fmt.Sprintf("\nExit error: %v", err)
        }

        // Truncate output.
        const maxOutputLen = 100000
        if len(result) > maxOutputLen {
            result = result[:maxOutputLen] + "\n... (output truncated)"
        }
        return result, nil
    }
}
```

ツール定義の `input_schema` に `timeout_seconds` を追加:

```go
reg.Register("execute_command", "Execute a shell command (foreground only)",
    map[string]any{
        "type": "object",
        "properties": map[string]any{
            "command": map[string]any{
                "type":        "string",
                "description": "The shell command to execute",
            },
            "timeout_seconds": map[string]any{
                "type":        "integer",
                "description": "Maximum execution time in seconds (default: 120)",
            },
        },
        "required": []string{"command"},
    }, newExecuteCommand(tc))
```

## 検証シナリオ (Verification Scenarios)

### R1 検証: SubagentExecutor 履歴統合

1. `ternctl run --agent wayfinder` でサブエージェント委譲が発生するタスクを実行する
2. `.wayfinder/` ディレクトリを確認:
   - 親セッション (`wf-XXXX/`) のみが存在すること
   - UUID 名の独立したセッションディレクトリが作成されていないこと
3. 親セッションの `history/` 内に:
   - サブエージェント委譲時の Seq に対応するサブディレクトリが存在すること
   - サブディレクトリ内に子セッションのメッセージファイルが存在すること

### R2 検証: execute_command タイムアウト

1. `sleep 300` のような長時間コマンドを execute_command で実行する
2. デフォルトタイムアウト (120秒) 後にプロセスが終了すること
3. 返却結果に `"Command timed out after 120 seconds"` メッセージが含まれること
4. `timeout_seconds: 5` を指定して短いタイムアウトをテストする
5. タイムアウト前に完了するコマンドは正常に結果を返すこと

## テスト項目 (Testing for the Requirements)

### 単体テスト

- `subagent_executor_test.go`: `Execute` で `HistorySubDir` が設定されることを検証
- `tool_command_test.go` (tools_test.go): タイムアウト動作の検証
  - デフォルトタイムアウト超過時のメッセージ検証
  - `timeout_seconds` パラメータによるカスタムタイムアウト検証
  - 正常完了時にタイムアウトメッセージが含まれないことの検証
- `tools_test.go`: ツール登録数 (11 -> 11, 変更なし) の確認

### ビルド検証

```bash
./scripts/process/build.sh
```

### 統合テスト

```bash
./scripts/process/integration_test.sh --categories llm --specify "wayfinder"
```
