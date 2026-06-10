# 022: Tool Call 実行パイプライン修正

## 背景 (Background)

Claude Code CLIを通じてコーディングエージェントにファイル作成を指示した際、LLMが「ファイルを作成した」とテキストで回答するだけで、実際にはWriteツールが呼び出されず、ファイルが作成されない問題が発生している。

VV4 (参照実装: SDKベース) では Claude Code SDK + GPT-Codex (OpenAIモデル) の組み合わせでTool Callが正常動作していた実績がある。従って本問題はモデル互換性の問題ではなく、現在の実装のゲートウェイ変換層に不備がある。

調査 (conversation 786c10c4) により、以下3つの根本原因が特定された:

1. **Responses APIストリーム変換のstop_reason判定が未実装**: function_callイベントがあっても常に`end_turn`が返る
2. **ANTHROPIC_API_KEYにsid/fallbackメタデータが欠如**: Tool Call Fallback機能とセッション固定が無効
3. **stop_reason防御ロジックの欠如**: VV4にあったtool_calls存在時の強制上書きロジックがない

### 問題のセッションログ (証拠)

```json
{
  "content": [{"type": "text", "text": "Got it - I created hello.py..."}],
  "model": "gpt-5.3-codex",
  "stop_reason": "end_turn"
}
```

tool_useブロックが一切含まれず、stop_reasonが`end_turn`のままである。

---

## 要件 (Requirements)

### R1: Responses APIストリーム変換のstop_reason判定 (必須)

`ConvertResponsesStreamToAnthropic()` 関数 (convert_a2r.go) の `response.completed` ケースで、function_callイベントの有無を追跡し、stop_reasonを正しく設定する。

- function_callイベントが1つ以上あった場合、stop_reasonは`tool_use`とする
- function_callイベントがなかった場合、stop_reasonは`end_turn`とする

**対象ファイル**: `shared/libs/go/llmgateway/convert_a2r.go`

**現在のコード** (L403-L413):
```go
case "response.completed":
    stopReason := "end_turn"
    if contentBlockIndex > 0 {
        // Check if last block was a tool call by looking at what we emitted.
        // Simple heuristic: if we got function_call events, use tool_use.
    }
    // TODO: 未実装
```

**期待する動作**: `response.output_item.added` で `function_call` が検出された際にフラグを立て、`response.completed` でそのフラグを参照してstop_reasonを決定する。

### R2: Responses API非ストリーム変換のstop_reason判定 (必須)

`ConvertResponsesResponseToAnthropic()` 関数 (convert_a2r.go) でも同様に、function_call出力がある場合にstop_reasonを`tool_use`に設定する。

**対象ファイル**: `shared/libs/go/llmgateway/convert_a2r.go`

**現在のコード** (L282-L287):
```go
if hasToolUse {
    anthResp.StopReason = "tool_use"
} else {
    anthResp.StopReason = "end_turn"
}
```

この部分は既に実装されている。ただし、実際にfunction_call出力がある場合に`hasToolUse`フラグが正しくtrueになるか検証が必要。

### R3: Chat Completions APIストリーム変換のstop_reason防御 (必須)

`ConvertOpenAIStreamToAnthropic()` 関数 (stream_converter.go) で、tool_callsが検出された場合にfinishReasonを`tool_calls`に強制する防御ロジックを追加する。

**対象ファイル**: `shared/libs/go/llmgateway/stream_converter.go`

**根拠**: VV4のproxy.go (L454-L456) に以下の防御ロジックがあった:
```go
if len(resp.Result.ToolCalls) > 0 && stopReason != "tool_use" {
    stopReason = "tool_use"
}
```

現在のstream_converter.goでは、`toolBlockStarted`フラグがあるが、`finishReason`にOpenAIが返した値をそのまま使用しており、OpenAIが`stop`を返した場合でもtool_callsがある場合はstop_reasonを`tool_use`に強制する必要がある。

### R4: ANTHROPIC_API_KEYへのsid/fallbackメタデータ追加 (必須)

`BuildEnv()` 関数 (claudecode/process.go) で、ANTHROPIC_API_KEYにセッションIDとfallbackフラグを埋め込む。

**対象ファイル**: `shared/libs/go/codingagent/claudecode/process.go`

**現在のコード** (L56-L58):
```go
if ac.GatewayURL != "" {
    env["ANTHROPIC_BASE_URL"] = ac.GatewayURL
    env["ANTHROPIC_API_KEY"] = "not-needed"
}
```

**期待するフォーマット**:
```
not-needed;fallback={true|false};sid={sessionID}
```

- `fallback`フラグ: model_profiles.yaml の `behavior.tool_call_fallback` 設定を参照
- `sid`: SessionConfigから取得可能なセッションIDを使用

VV4の実装 (ClaudeAgent.ts L126):
```typescript
ANTHROPIC_API_KEY: (this.config.apiKey || "not-needed") + ";fallback=" + String(this.fallbackEnabled) + ";sid=" + currentSessionId
```

### R5: ToolCallFallback有効化パラメータの伝達 (必須)

ToolCallFallbackの有効/無効をAdapterConfigまたはSessionConfigに伝達する仕組みを追加する。現在、model_profiles.yamlに`behavior.tool_call_fallback`設定があるが、codingagentアダプターに伝達されていない。

**選択肢**:
- A) AdapterConfigに`ToolCallFallback bool`フィールドを追加し、main.goのregisterCodingAgentsで設定する
- B) SessionConfigに`ToolCallFallback bool`フィールドを追加し、AgentServiceが解決したモデルの設定から注入する

ゲートウェイ側では既にx-api-keyから`fallback`フラグを読み取る`ExtractSessionID()`が存在するため (fallback.go L207-L220)、API Keyにフラグを埋め込めば機能する。ただし、現在のプロキシハンドラでfallbackフラグの読み取り・適用が行われているか確認が必要。

### R6: ストリーミングレスポンスでのToolCallFallback対応 (任意)

現在のToolCallFallback (proxy_anthropic.go L246-L258, proxy_openai.go L116-L131) は非ストリーミングレスポンスにのみ適用される。ストリーミングレスポンスでもテキスト内にtool_call記述があった場合の対策が必要な場合は、将来的に実装する。

> [!NOTE]
> VV4でもストリーミングでのFallbackは非対応だったため、本仕様では任意要件とする。

### R7: maxTurnsデフォルト設定 (任意)

`BuildArgs()` 関数 (claudecode/process.go) で、SessionConfig.MaxTurnsが未設定(0)の場合にデフォルト値(200)を設定する。

**対象ファイル**: `shared/libs/go/codingagent/claudecode/process.go`

VV4ではmaxTurns: 200を明示的に設定していた。CLIのデフォルトが低い場合にエージェントループが早期終了するリスクがある。

### R8: x-api-keyからのfallbackフラグ読み取りとプロキシ適用 (必須)

ゲートウェイのプロキシハンドラ(proxy_anthropic.go)で、x-api-keyからfallbackフラグを抽出し、model_profiles.yamlの設定に加えてfallbackを有効化する。

現在、`RoutedModel.ToolCallFallback`はmodel_profiles.yamlの静的設定から決定されるが、VV4ではx-api-key内のfallbackフラグも考慮していた。両方のソースからOR条件でfallbackを有効化すべき。

**対象ファイル**: `shared/libs/go/llmgateway/proxy_anthropic.go`, `shared/libs/go/llmgateway/proxy_openai.go`

---

## 実現方針 (Implementation Approach)

### 変更ファイル一覧

```
shared/libs/go/llmgateway/
  convert_a2r.go          -- R1, R2: Responses API変換のstop_reason修正
  stream_converter.go     -- R3: Chat Completions変換のstop_reason防御
  proxy_anthropic.go      -- R8: x-api-keyからfallback読み取り
  proxy_openai.go         -- R8: x-api-keyからfallback読み取り
  fallback.go             -- (既存) ExtractFallbackFlag関数の追加

shared/libs/go/codingagent/
  adapter_config.go       -- R5: ToolCallFallbackフィールド追加
  claudecode/process.go   -- R4, R7: BuildEnvとBuildArgsの修正

examples/standalone/
  main.go                 -- R5: ToolCallFallback設定の注入
```

### 処理フロー (修正後)

```mermaid
sequenceDiagram
    participant CLI as Claude CLI
    participant GW as LLM Gateway
    participant OpenAI as gpt-5.3-codex

    Note over CLI: ANTHROPIC_API_KEY に<br/>sid, fallback 埋め込み済み

    CLI->>GW: POST /v1/messages<br/>(tools含む, x-api-key: ...;fallback=true;sid=xxx)
    Note over GW: x-api-keyからsid, fallbackを抽出<br/>RoutedModelにfallback ORで適用

    GW->>OpenAI: POST /v1/responses (変換済み)
    OpenAI-->>GW: function_call応答

    Note over GW: ConvertResponsesStreamToAnthropic()<br/>function_callフラグ追跡<br/>stop_reason = tool_use (R1)

    GW-->>CLI: SSE (tool_use blocks)<br/>stop_reason: tool_use
    Note over CLI: stop_reason=tool_use<br/>→ツール実行→結果をLLMへ<br/>→エージェントループ継続
```

### R1の実装詳細

`ConvertResponsesStreamToAnthropic()` に `hadFunctionCall` フラグ変数を追加:

```go
hadFunctionCall := false  // ストリーム中にfunction_callイベントがあったか

// ...

case "response.output_item.added":
    // 既存コード
    if item.Type == "function_call" {
        hadFunctionCall = true  // フラグを立てる
        // ... 既存の content_block_start 送信
    }

// ...

case "response.completed":
    stopReason := "end_turn"
    if hadFunctionCall {
        stopReason = "tool_use"
    }
    // ... 既存の message_delta 送信
```

### R3の実装詳細

`ConvertOpenAIStreamToAnthropic()` の [DONE] 処理で、`toolBlockStarted`を参照:

```go
// 既存: finishReasonからstopReasonを導出
stopReason := "end_turn"
if finishReason != "" {
    stopReason = mapFinishReason(finishReason)
}

// 追加: tool_callsが検出されていればstop_reasonを強制
if toolBlockStarted && stopReason != "tool_use" {
    stopReason = "tool_use"
}
```

### R4の実装詳細

`BuildEnv()` 関数を拡張:

```go
func BuildEnv(ac *codingagent.AdapterConfig, cfg *codingagent.SessionConfig) []string {
    // ...
    if ac.GatewayURL != "" {
        env["ANTHROPIC_BASE_URL"] = ac.GatewayURL
        // メタデータ付きAPI Key
        apiKey := "not-needed"
        fallbackStr := "false"
        if ac.ToolCallFallback {
            fallbackStr = "true"
        }
        sid := cfg.SessionID  // または適切なセッション識別子
        if sid == "" {
            sid = "default"
        }
        env["ANTHROPIC_API_KEY"] = apiKey + ";fallback=" + fallbackStr + ";sid=" + sid
    }
    // ...
}
```

### R8の実装詳細

`proxy_anthropic.go` の `handleAnthropicMessages()` で:

```go
// 既存: ToolCallFallback は RoutedModel から取得
// 追加: x-api-keyのfallbackフラグも考慮
if ExtractFallbackFlag(r.Header.Get("x-api-key")) {
    routed.ToolCallFallback = true
}
```

`fallback.go` に `ExtractFallbackFlag()` を追加:

```go
func ExtractFallbackFlag(authHeader string) bool {
    for _, part := range strings.Split(authHeader, ";") {
        part = strings.TrimSpace(part)
        if part == "fallback=true" {
            return true
        }
    }
    return false
}
```

---

## 検証シナリオ (Verification Scenarios)

### シナリオ1: Responses API経由のTool Call (R1, R2)

1. HAGサーバーを起動し、model_profiles.yamlでgpt-5.3-codex (mode: responses) を設定
2. Claude Code CLI経由で「Create a hello.py file with print('hello')」を送信
3. LLMがfunction_call (Write) を返した場合:
   - ゲートウェイがstop_reasonを`tool_use`に設定してCLIに返却
   - CLIがWriteツールを実行し、hello.pyが作成される
   - セッションログでtool_useブロックが確認できる
4. LLMがテキストのみで応答した場合:
   - ToolCallFallback有効時: テキスト内のtool_call記述を抽出してtool_useに変換
   - ToolCallFallback無効時: テキストのまま返却 (stop_reason: end_turn)

### シナリオ2: Chat Completions API経由のTool Call (R3)

1. model_profiles.yamlでgpt-4o (mode未設定 = chat) を設定
2. Claude Code CLI経由でファイル作成を指示
3. OpenAIがtool_callsを含むレスポンスを返した場合:
   - finishReasonに関わらず、toolBlockStartedフラグによりstop_reasonが`tool_use`に設定される
   - CLIがツールを実行してファイルが作成される

### シナリオ3: API Keyメタデータの伝達 (R4, R5, R8)

1. model_profiles.yamlで `behavior.tool_call_fallback: true` を設定
2. HAGサーバーを起動し、ClaudeCode adapterのToolCallFallback=trueを確認
3. CLIがANTHROPIC_API_KEY: `not-needed;fallback=true;sid={sid}` でリクエストを送信
4. ゲートウェイのログで以下を確認:
   - sidが正しく抽出されている
   - fallback=trueが読み取られている
   - ToolCallFallbackが有効化されている

---

## テスト項目 (Testing for the Requirements)

### 単体テスト

#### T1: Responses APIストリーム変換 stop_reason (R1)

`convert_a2r_test.go` に追加:
- function_callイベントを含むSSEストリームを入力し、出力のmessage_deltaのstop_reasonが`tool_use`であることを確認
- function_callイベントなし(テキストのみ)のSSEストリームを入力し、stop_reasonが`end_turn`であることを確認

#### T2: Responses API非ストリーム変換 stop_reason (R2)

`convert_a2r_test.go` に追加:
- function_call出力を含むレスポンスを変換し、stop_reasonが`tool_use`であることを確認
- テキストのみの出力を変換し、stop_reasonが`end_turn`であることを確認

#### T3: Chat Completions APIストリーム変換 stop_reason防御 (R3)

`stream_converter_test.go` に追加:
- tool_callsを含むストリームで、finishReasonが`stop`でもstop_reasonが`tool_use`に設定されることを確認
- tool_callsなしで、finishReasonが`stop`ならstop_reasonが`end_turn`であることを確認

#### T4: BuildEnv メタデータ (R4)

`process_test.go` に追加:
- ToolCallFallback=true + SessionID設定時、ANTHROPIC_API_KEYに`;fallback=true;sid=xxx`が含まれることを確認
- ToolCallFallback=false時、ANTHROPIC_API_KEYに`;fallback=false`が含まれることを確認

#### T5: ExtractFallbackFlag (R8)

`fallback_test.go` に追加:
- `not-needed;fallback=true;sid=abc` -> true
- `not-needed;fallback=false;sid=abc` -> false
- `not-needed` -> false (fallbackパートなし)

### 統合テスト

```bash
# LLMカテゴリのテスト (Gateway変換テスト)
./scripts/process/integration_test.sh --categories llm

# 全カテゴリのリグレッション確認
./scripts/process/integration_test.sh --categories common,llm
```

### ビルド検証

```bash
./scripts/process/build.sh
```
