# 011-Fix-CodingAgent-Streaming

## 背景 (Background)

cawa-client (`./bin/cawa-client run --agent claudecode --prompt "..." --work-dir ./tmp/`) を実行すると、
セッション作成と完了は成功するが、**SSE ストリーミングイベント (text, tool_use 等) が一切表示されない**。

```
Session created: 30c5db0aeb058d6e1a97f0f03e81ba41
                          <-- ここにイベントが表示されるべき
--- Stream completed ---
```

調査の結果、以下の複合的な問題が判明した:

1. **`ANTHROPIC_API_KEY = "not-needed"` が常に設定される**: `BuildEnv()` が `GatewayURL` の有無に関わらず `ANTHROPIC_API_KEY = "not-needed"` を設定。Claude CLI はこの環境変数を優先するため認証エラーで即終了する。
2. **standalone の `registerCodingAgents` で `GatewayURL` が未設定**: `AdapterConfig{}` が空で渡されるため、Claude CLI が LLM Gateway プロキシを経由しない。
3. **stderr が完全に無視**: `StartProcess()` が `cmd.Stderr` を設定していないため、Claude CLI のエラーメッセージが消失し、問題の診断が困難。
4. **プロセス終了コード未確認**: goroutine 内で `cmd.Wait()` が呼ばれず、エラー終了と正常終了を区別できない。

## 要件 (Requirements)

### R1: BuildEnv の API キー設定ロジック修正

- `GatewayURL` が設定されている場合のみ `ANTHROPIC_BASE_URL` と `ANTHROPIC_API_KEY = "not-needed"` を設定する。
- `GatewayURL` が空の場合は CLI 自身の API キー管理に委任し、これらの環境変数を設定しない。

### R2: standalone の GatewayURL 設定

- `registerCodingAgents` で `AdapterConfig.GatewayURL` に LLM Gateway のポートを反映する。
- `hag.Server` から Gateway ポートを取得する API が必要。

### R3: stderr のキャプチャとログ出力

- `StartProcess()` で `cmd.Stderr` をキャプチャし、ログに出力する。
- エラー出力はサーバの logger 経由で `Error` レベルで記録する。
- logger が渡せない場合は、stderr バッファを保持し、プロセス終了時にエラーイベントとしてチャンネルに送信する。

### R4: プロセス終了コードの確認

- goroutine 内で `cmd.Wait()` を呼び、exit code != 0 の場合は `EventError` をチャンネルに送信する。
- stderr の内容をエラーメッセージに含める。

## 実現方針 (Implementation Approach)

### R1: `BuildEnv` の修正

`shared/libs/go/codingagent/claudecode/process.go` の `BuildEnv` を修正:

```go
func BuildEnv(ac *codingagent.AdapterConfig, cfg *codingagent.SessionConfig) []string {
    env := make(map[string]string)
    // Only set gateway-related env vars when GatewayURL is configured.
    if ac.GatewayURL != "" {
        env["ANTHROPIC_BASE_URL"] = ac.GatewayURL
        env["ANTHROPIC_API_KEY"] = "not-needed" // Gateway handles auth
    }
    // ...
}
```

### R2: standalone の `registerCodingAgents` 修正

`examples/standalone/main.go`:

```go
func registerCodingAgents(srv *hag.Server) {
    if _, err := exec.LookPath("claude"); err == nil {
        gwPort := srv.Gateway().Port()
        gwURL := ""
        if gwPort > 0 {
            gwURL = fmt.Sprintf("http://localhost:%d", gwPort)
        }
        adapter := claudecode.New(&codingagent.AdapterConfig{
            GatewayURL: gwURL,
        })
        srv.AgentService().RegisterAgent(adapter)
    }
}
```

`Gateway().Port()` メソッドが必要。現在 `llmgateway.LLMGatewayBackend` インターフェースに `Port()` がなければ追加する。

### R3: stderr キャプチャ

`StartProcess` で `bytes.Buffer` を `cmd.Stderr` に設定し、goroutine 内で参照する:

```go
var stderrBuf bytes.Buffer
cmd.Stderr = &stderrBuf
```

### R4: プロセス終了コード確認

goroutine 末尾で `cmd.Wait()` を呼び、エラー終了時にイベントを送信:

```go
go func() {
    defer close(ch)
    scanner := bufio.NewScanner(stdout)
    for scanner.Scan() { ... }
    if err := cmd.Wait(); err != nil {
        errMsg := stderrBuf.String()
        ch <- codingagent.StreamEvent{
            Type:  codingagent.EventError,
            Error: fmt.Errorf("claude exited: %w: %s", err, errMsg),
        }
    }
}()
```

## 検証シナリオ (Verification Scenarios)

### シナリオ 1: GatewayURL 未設定時に API キー環境変数が設定されない

1. `AdapterConfig{GatewayURL: ""}` で `BuildEnv` を呼び出す
2. 結果に `ANTHROPIC_API_KEY` が含まれないことを確認
3. 結果に `ANTHROPIC_BASE_URL` が含まれないことを確認

### シナリオ 2: GatewayURL 設定時に API キー環境変数が設定される

1. `AdapterConfig{GatewayURL: "http://localhost:14000"}` で `BuildEnv` を呼び出す
2. 結果に `ANTHROPIC_BASE_URL=http://localhost:14000` が含まれることを確認
3. 結果に `ANTHROPIC_API_KEY=not-needed` が含まれることを確認

### シナリオ 3: stderr キャプチャとエラーイベント

1. 存在しないコマンドまたは不正な引数で `StartProcess` に相当する処理を実行
2. stderr の内容がエラーイベントに含まれることを確認
3. チャンネルに `EventError` が送信されることを確認

### シナリオ 4: 正常な Claude CLI 実行で SSE イベントが流れる

1. standalone サーバを起動
2. `cawa-client run` でメッセージを送信
3. SSE イベント (少なくとも system, text のいずれか) がストリーミングされること
4. セッション状態が `completed` に遷移すること

### シナリオ 5: standalone の GatewayURL 伝播

1. standalone サーバ起動後、health エンドポイントを確認
2. `gateway.url` が `http://localhost:14000` であること
3. cawa-client run でエージェントが Gateway 経由で LLM にアクセスすること

## テスト項目 (Testing for the Requirements)

### 単体テスト

| テスト | 対象要件 | 配置先 |
|--------|----------|--------|
| `TestBuildEnv_NoGateway` | R1 | `shared/libs/go/codingagent/claudecode/process_test.go` |
| `TestBuildEnv_WithGateway` | R1 | 同上 |
| `TestBuildEnv_SessionEnvVars` | R1 | 同上 |

### 統合テスト

| テスト | 対象要件 | 配置先 |
|--------|----------|--------|
| `TestProcessStderrCapture` | R3, R4 | `tests/agentservice_process_test.go` |
| `TestCodingAgentE2EStreaming` | R1-R4 | `tests/agentservice_integration_test.go` |

### ビルド・全体検証

1. ビルド + 単体テスト:
   ```
   scripts/process/build.sh
   ```

2. 統合テスト (AgentService 関連):
   ```
   scripts/process/integration_test.sh --specify "AgentService"
   ```
