# 052: LLM 応答安定性改善 -- max_output_tokens 設定化、空レスポンス対策、ログ改善

## 背景 (Background)

Wayfinder でブラウザゲーム作成を依頼したところ、Gemini API が約4分後に空レスポンス (テキストなし、ツール呼び出しなし) を返し、処理が未完了のまま「正常完了」として終了する事象が発生した。

### 直接的な問題

1. **max_output_tokens=4096 のハードコード**: [bifrost_client.go:77](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-llm-backend/shared/libs/go/wayfinder/bifrost_client.go#L77) で固定値。大規模コード生成タスクでは不足し、LLM が出力を打ち切る原因になる。
2. **空レスポンスの無言完了**: AgentCore は `len(resp.ToolCalls) == 0` の場合、`resp.Content` が空でも正常完了として処理する ([agent_core.go:175-189](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-llm-backend/shared/libs/go/wayfinder/agent_core.go#L175-L189))。
3. **ログの情報不足**: stop_reason、トークン使用量、LLM リクエスト所要時間がログに含まれない。
4. **Debug/Trace 分離不足**: リクエスト/レスポンスの全 JSON ペイロードが Debug レベルで出力され、ターミナルが埋め尽くされる。

---

## 要件 (Requirements)

### R1: max_output_tokens の設定化 (必須)

- `model_profiles.yaml` の `ModelConfig.behavior` に `max_output_tokens` フィールドを追加する
- 設定例:

```yaml
providers:
  google:
    api_keys:
      - name: default
        models:
          - name: gemini-2.5-flash
            behavior:
              max_output_tokens: 65536
              structured_output: true
```

- 設定値は LLM Gateway の routing 経路を通じて BifrostClient まで伝達される
- 未設定の場合のデフォルト値は `16384` (現在の 4096 から引き上げ)
- BifrostClient の `buildRequestBody` でハードコードされた `4096` を、設定値で置換する

#### 伝達経路の設計

```
model_profiles.yaml
  -> ModelConfig.Behavior.MaxOutputTokens
  -> routing.ResolveModel() -> RoutedModel.MaxOutputTokens
  -> anthropic/handler.go -> FullRequest.MaxTokens をオーバーライド
  -> Bifrost SDK -> upstream API
```

### R2: LLMResponse に StopReason を追加 (必須)

- `LLMResponse` 構造体に `StopReason string` フィールドを追加する
- BifrostClient の `parseResponse` および `parseSSEStream` で Anthropic レスポンスの `stop_reason` を取得・伝達する
- AgentCore はこの値をログに記録する

### R3: 空レスポンスの検出とリトライ (必須)

- AgentCore の `runSimple` ループで、`resp.Content == "" && len(resp.ToolCalls) == 0` の場合:
  1. Warn ログを出力: `"LLM returned empty response"` (stop_reason, iteration, model を含む)
  2. 最大1回のリトライを試みる (同一 iteration 内)
  3. リトライも空の場合、エラーとしてセッションを `StatusFailed` にする
- `stop_reason == "max_tokens"` の場合は特別な Warn ログを出力し、max_output_tokens の設定見直しを促す

### R4: ログレベルの適正化 (必須)

以下のログを Debug から Trace に降格する:

| ファイル | 現在のログ | 変更 |
|:---------|:-----------|:-----|
| [anthropic/handler.go:170](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-llm-backend/shared/libs/go/llmgateway/anthropic/handler.go#L170) | `Debug("raw anthropic request messages", "json", ...)` | -> **Trace** |
| [anthropic/handler.go:190](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-llm-backend/shared/libs/go/llmgateway/anthropic/handler.go#L190) | `Debug("converted bifrost request", "json", ...)` | -> **Trace** |

### R5: LLM リクエスト/レスポンスの概要ログ追加 (必須)

AgentCore の `runSimple` ループに以下のログを追加:

| タイミング | レベル | 内容 |
|:-----------|:-------|:-----|
| LLM リクエスト送信時 | Info | `"LLM request" iteration, model, messages_count` |
| LLM レスポンス受信時 | Info | `"LLM response" iteration, duration_ms, stop_reason, content_len, tool_calls_count` |
| 空レスポンス時 | Warn | `"LLM empty response" iteration, stop_reason, model` |

### R6: LLM Gateway ストリーム完了ログの改善 (任意)

[anthropic/handler.go:462](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-llm-backend/shared/libs/go/llmgateway/anthropic/handler.go#L462) のストリーム完了ログに所要時間を追加する:

```go
// 現在
log.Debug("bifrost anthropic stream completed", "model", model, "chunks", chunkCount)
// 改善後
log.Info("bifrost anthropic stream completed", "model", model, "chunks", chunkCount, "duration_ms", elapsed.Milliseconds(), "output_tokens", totalOutputTokens)
```

---

## 実現方針 (Implementation Approach)

### Phase 1: max_output_tokens 設定化 (R1)

#### 1-1. config パッケージの変更

[model_profiles.go](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-llm-backend/shared/libs/go/config/model_profiles.go) の `ModelBehavior` に追加:

```go
type ModelBehavior struct {
    ToolCallFallback bool `yaml:"tool_call_fallback"`
    StructuredOutput bool `yaml:"structured_output"`
    MaxOutputTokens  int  `yaml:"max_output_tokens,omitempty"` // 新規追加
}
```

#### 1-2. RoutedModel への伝達

[handlerctx/context.go](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-llm-backend/shared/libs/go/llmgateway/handlerctx/context.go) の `RoutedModel` に追加:

```go
type RoutedModel struct {
    // ...existing fields...
    MaxOutputTokens int `json:"max_output_tokens,omitempty"` // 新規追加
}
```

#### 1-3. routing.go での伝達

`ResolveModel` で `ModelBehavior.MaxOutputTokens` を `RoutedModel.MaxOutputTokens` にコピーする。

#### 1-4. anthropic/handler.go での適用

`handleMessagesViaBifrost` で、RoutedModel の MaxOutputTokens が設定されていれば `FullRequest.MaxTokens` をオーバーライドする。

#### 1-5. BifrostClient のデフォルト値変更

[bifrost_client.go:77](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-llm-backend/shared/libs/go/wayfinder/bifrost_client.go#L77) のハードコードを `16384` に変更する (model_profiles.yaml 経由で LLM Gateway 側でオーバーライドされるため、BifrostClient 側のデフォルトは安全な値に)。

### Phase 2: StopReason 伝達と空レスポンス対策 (R2, R3)

#### 2-1. LLMResponse の拡張

```go
type LLMResponse struct {
    Content    string     `json:"content"`
    ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
    StopReason string     `json:"stop_reason,omitempty"` // 新規追加
}
```

#### 2-2. BifrostClient での StopReason 取得

- `parseResponse`: Anthropic レスポンスの `stop_reason` フィールドを読み取る
- `parseSSEStream`: ストリーム中の `message_delta` イベントから `stop_reason` を取得する

#### 2-3. AgentCore の空レスポンスハンドリング

```go
if len(resp.ToolCalls) == 0 {
    if resp.Content == "" {
        ac.logger.Warn("LLM returned empty response",
            "iteration", iteration,
            "stop_reason", resp.StopReason,
            "model", ac.config.LogicalModel)
        if resp.StopReason == "max_tokens" {
            ac.logger.Warn("consider increasing max_output_tokens in model_profiles.yaml")
        }
        // 1回だけリトライ
        if !retried {
            retried = true
            continue // ループの先頭に戻る
        }
        ac.saveSession(session.StatusFailed)
        return "", fmt.Errorf("agent core: LLM returned empty response at iteration %d (stop_reason=%s)", iteration, resp.StopReason)
    }
    // ...existing completion logic...
}
```

### Phase 3: ログ改善 (R4, R5, R6)

#### 3-1. Debug -> Trace 降格

`anthropic/handler.go` の2箇所で `log.Debug` を `log.Trace` に変更するだけ。

#### 3-2. AgentCore ログ追加

LLM 呼び出しの前後で `time.Now()` を記録し、所要時間を計算してログに出力。

#### 3-3. ストリーム完了ログの改善

`handleMessagesBifrostStream` の冒頭で `startTime := time.Now()` を記録し、完了時に elapsed を計算。

---

## 変更対象ファイル一覧

| コンポーネント | ファイル | 変更内容 |
|:--------------|:---------|:---------|
| config | `model_profiles.go` | `ModelBehavior.MaxOutputTokens` 追加 |
| config | `model_profiles_test.go` | テスト追加 |
| handlerctx | `context.go` | `RoutedModel.MaxOutputTokens` 追加 |
| llmgateway | `routing.go` | MaxOutputTokens の伝達 |
| llmgateway/anthropic | `handler.go` | MaxTokens オーバーライド、ログレベル変更、所要時間ログ |
| wayfinder | `llm_client.go` | `LLMResponse.StopReason` 追加 |
| wayfinder | `bifrost_client.go` | StopReason パース、デフォルト値変更 |
| wayfinder | `bifrost_client_test.go` | テスト更新 |
| wayfinder | `agent_core.go` | 空レスポンス対策、ログ追加 |
| wayfinder | `agent_core_test.go` | テスト追加 |
| settings | `demo/model_profiles.yaml` | max_output_tokens 設定例追加 |
| settings | `example/model_profiles.yaml` | スキーマコメント追加 |

---

## 検証シナリオ (Verification Scenarios)

### シナリオ 1: max_output_tokens の設定反映

1. `model_profiles.yaml` に `max_output_tokens: 65536` を設定する
2. ternctl でプロンプトを送信する
3. サーバーログで LLM Gateway が適切な max_tokens でリクエストを送信していることを確認する

### シナリオ 2: 空レスポンスのリトライ

1. LLM が空レスポンスを返す状況を模擬する (テスト)
2. AgentCore が Warn ログを出力し、1回リトライすることを確認する
3. リトライも失敗した場合、`StatusFailed` でセッションが保存されることを確認する

### シナリオ 3: ログレベル確認

1. `config.yaml` で `log.level: "debug"` に設定する
2. リクエスト/レスポンスの全 JSON ペイロードが表示されないことを確認する
3. `log.level: "trace"` にした場合のみ、全ペイロードが表示されることを確認する

### シナリオ 4: LLM リクエスト/レスポンスの概要ログ

1. ternctl でプロンプトを送信する
2. サーバーログに `"LLM request"` と `"LLM response"` の Info ログが出力されることを確認する
3. `"LLM response"` ログに `duration_ms`, `stop_reason`, `content_len`, `tool_calls_count` が含まれることを確認する

---

## テスト項目 (Testing for the Requirements)

### 単体テスト

```bash
./scripts/process/build.sh
```

- `config/model_profiles_test.go`: MaxOutputTokens のパース・バリデーション
- `wayfinder/bifrost_client_test.go`: StopReason のパース、デフォルト max_tokens の確認
- `wayfinder/agent_core_test.go`: 空レスポンスのリトライ・失敗ハンドリング

### 統合テスト

```bash
./scripts/process/integration_test.sh --specify "TestE2E_Wayfinder"
```

- `TestE2E_Wayfinder_FullScenario_Gemini`: 正常系リグレッション
- `TestE2E_Wayfinder_CompactionToolPairProtection`: compaction リグレッション
