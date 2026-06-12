# 024-LLMGP-CrossProvider-APITranslation-Part2

> **Source Specification**: [016-LLMGP-CrossProvider-APITranslation.md](file:///prompts/phases/000-foundation/branches/feat-llm-backend/ideas/016-LLMGP-CrossProvider-APITranslation.md)

## Goal Description

Part 1 (023) で実装した非ストリーミングテキスト変換を拡張し、以下を追加する:

1. **ストリーミング SSE 変換** (R3): OpenAI SSE チャンク (`data: {...}`) を Anthropic SSE イベント (`event: ...\ndata: {...}`) にリアルタイム変換
2. **ツール呼び出し変換** (R4): Anthropic 形式のツール定義/ツール結果と OpenAI 形式の相互変換

## User Review Required

> [!IMPORTANT]
> ストリーミング変換は、OpenAI のチャンクを1つずつパースして Anthropic イベントに変換するパイプライン方式で実装します。
> バッファリングしてからまとめて変換する方式は遅延が大きくなるため採用しません。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: Anthropic -> OpenAI リクエスト変換 | **Part 1 で実装済み** |
| R2: OpenAI -> Anthropic レスポンス変換 | **Part 1 で実装済み** (テキスト) / 本計画で拡張 (tool_calls) |
| R3: ストリーミングレスポンス変換 | Proposed Changes > `stream_converter.go` |
| R4: ツール呼び出し変換 | Proposed Changes > `convert_a2o.go` (リクエスト拡張) + `convert_a2o.go` (レスポンス拡張) |
| R5: エラーハンドリング | Proposed Changes > `stream_converter.go` (ストリームエラー処理) |
| R6: R4 パススルー統合テスト | **Part 1 で実装済み** |
| R7: 変換対象外フィールドの透過 | **Part 1 で実装済み** |

## Proposed Changes

### llmgateway パッケージ

---

#### [NEW] [stream_converter_test.go](file:///shared/libs/go/llmgateway/stream_converter_test.go)

*   **Description**: OpenAI SSE -> Anthropic SSE ストリーミング変換の単体テスト
*   **Technical Design**:
    ```go
    func TestConvertOpenAIStreamToAnthropic(t *testing.T)
    func TestConvertOpenAIStreamToAnthropic_ToolCalls(t *testing.T)
    func TestConvertOpenAIStreamToAnthropic_EmptyStream(t *testing.T)
    ```
*   **Logic**:
    *   テーブル駆動テストで以下のケースを検証:
        *   **テキストストリーミング**: 複数の `data: {...}` チャンクが `content_block_delta` イベントに変換されること
        *   **message_start / message_stop**: 最初のチャンクで `message_start` が送信され、`data: [DONE]` で `message_stop` が送信されること
        *   **content_block_start / content_block_stop**: テキストブロックの開始/終了イベントが適切に送信されること
        *   **finish_reason マッピング**: `stop` -> `end_turn`, `length` -> `max_tokens`
        *   **ツール呼び出しストリーミング**: `delta.tool_calls` が `content_block_delta` (tool_use) に変換されること
        *   **空ストリーム**: 空入力に対して `message_start` + `message_stop` のみが送信されること

---

#### [NEW] [stream_converter.go](file:///shared/libs/go/llmgateway/stream_converter.go)

*   **Description**: OpenAI Chat Completions SSE ストリームを Anthropic Messages SSE ストリームに変換するパイプライン
*   **Technical Design**:
    ```go
    // OpenAIStreamChunk represents a single chunk in OpenAI's streaming response.
    type OpenAIStreamChunk struct {
        ID      string                `json:"id"`
        Choices []OpenAIStreamChoice  `json:"choices"`
        Usage   *OpenAIUsage          `json:"usage,omitempty"`
    }

    // OpenAIStreamChoice represents a choice in a streaming chunk.
    type OpenAIStreamChoice struct {
        Delta        OpenAIStreamDelta `json:"delta"`
        FinishReason *string           `json:"finish_reason"`
    }

    // OpenAIStreamDelta represents the delta content in a streaming chunk.
    type OpenAIStreamDelta struct {
        Role      string                  `json:"role,omitempty"`
        Content   string                  `json:"content,omitempty"`
        ToolCalls []OpenAIStreamToolCall  `json:"tool_calls,omitempty"`
    }

    // OpenAIStreamToolCall represents a tool call delta in streaming.
    type OpenAIStreamToolCall struct {
        Index    int                     `json:"index"`
        ID       string                  `json:"id,omitempty"`
        Type     string                  `json:"type,omitempty"`
        Function OpenAIStreamToolFunc    `json:"function"`
    }

    // OpenAIStreamToolFunc represents the function part of a streaming tool call.
    type OpenAIStreamToolFunc struct {
        Name      string `json:"name,omitempty"`
        Arguments string `json:"arguments,omitempty"`
    }

    // ConvertOpenAIStreamToAnthropic reads OpenAI SSE chunks from reader,
    // converts them to Anthropic SSE events, and writes them to the ResponseWriter.
    // It flushes after each event for real-time streaming.
    func ConvertOpenAIStreamToAnthropic(
        reader io.Reader,
        w http.ResponseWriter,
        model string,
    ) error
    ```
*   **Logic**:
    1. `bufio.Scanner` で `reader` を行単位で読み取る
    2. `data: ` プレフィックスの行を検出
    3. `data: [DONE]` の場合:
        - 各未クローズの content_block に `content_block_stop` イベント送信
        - `message_delta` (stop_reason, usage) イベント送信
        - `message_stop` イベント送信
        - 終了
    4. JSON パース -> `OpenAIStreamChunk`
    5. **最初のチャンク** (role が存在する場合):
        - `message_start` イベント送信 (id, type, role, model)
    6. **content delta** (`delta.content` が非空):
        - まだ text block が開始されていなければ `content_block_start` (type: text) 送信
        - `content_block_delta` (type: text_delta, text: delta.content) 送信
    7. **tool_calls delta** (`delta.tool_calls` が存在):
        - 新規 tool_call (id が非空) の場合: 直前の block を close し、`content_block_start` (type: tool_use, id, name) 送信
        - arguments の場合: `content_block_delta` (type: input_json_delta, partial_json: arguments) 送信
    8. **finish_reason** が非 nil の場合:
        - `mapFinishReason` でマッピングし、`message_delta` に反映
    9. 各イベント送信後に `Flusher.Flush()` を呼ぶ

    **Anthropic SSE イベント形式**:
    ```
    event: message_start
    data: {"type":"message_start","message":{"id":"...","type":"message","role":"assistant","model":"...","content":[],"usage":{"input_tokens":0,"output_tokens":0}}}

    event: content_block_start
    data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

    event: content_block_delta
    data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

    event: content_block_stop
    data: {"type":"content_block_stop","index":0}

    event: message_delta
    data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":15}}

    event: message_stop
    data: {"type":"message_stop"}
    ```

---

#### [MODIFY] [convert_a2o.go](file:///shared/libs/go/llmgateway/convert_a2o.go)

*   **Description**: ツール定義とツール呼び出しの変換を追加
*   **Technical Design**:
    ```go
    // --- Anthropic Tool Types ---

    // AnthropicTool represents a tool definition in Anthropic format.
    type AnthropicTool struct {
        Name        string          `json:"name"`
        Description string          `json:"description,omitempty"`
        InputSchema json.RawMessage `json:"input_schema"`
    }

    // AnthropicToolUseBlock represents a tool_use content block in Anthropic response.
    type AnthropicToolUseBlock struct {
        Type  string          `json:"type"`  // "tool_use"
        ID    string          `json:"id"`
        Name  string          `json:"name"`
        Input json.RawMessage `json:"input"`
    }

    // AnthropicToolResultMsg represents a tool_result message in Anthropic format.
    type AnthropicToolResultMsg struct {
        Type      string `json:"type"`       // "tool_result"
        ToolUseID string `json:"tool_use_id"`
        Content   string `json:"content"`
    }

    // --- OpenAI Tool Types ---

    // OpenAITool represents a tool definition in OpenAI format.
    type OpenAITool struct {
        Type     string         `json:"type"` // "function"
        Function OpenAIFunction `json:"function"`
    }

    // OpenAIFunction represents a function definition in OpenAI format.
    type OpenAIFunction struct {
        Name        string          `json:"name"`
        Description string          `json:"description,omitempty"`
        Parameters  json.RawMessage `json:"parameters"`
    }

    // OpenAIToolCall represents a tool call in OpenAI response.
    type OpenAIToolCall struct {
        ID       string         `json:"id"`
        Type     string         `json:"type"` // "function"
        Function OpenAIFuncCall `json:"function"`
    }

    // OpenAIFuncCall represents a function call in OpenAI response.
    type OpenAIFuncCall struct {
        Name      string `json:"name"`
        Arguments string `json:"arguments"`
    }
    ```
*   **Logic**:
    *   `AnthropicFullRequest` に `Tools []AnthropicTool` フィールドを追加
    *   `OpenAIRequest` に `Tools []OpenAITool` フィールドを追加
    *   `OpenAIMsg` に `ToolCalls []OpenAIToolCall` フィールドを追加 (レスポンス用)、`ToolCallID string` フィールドを追加 (tool result用)
    *   `ConvertAnthropicRequestToOpenAI` に以下を追加:
        - `req.Tools` が存在する場合、各 `AnthropicTool` を `OpenAITool{Type: "function", Function: {Name, Description, Parameters: InputSchema}}` に変換
        - `messages` 内の `tool_result` ブロックを OpenAI の `role: "tool"` メッセージに変換
    *   `ConvertOpenAIResponseToAnthropic` に以下を追加:
        - `choices[0].message.tool_calls` が存在する場合、各 `OpenAIToolCall` を `AnthropicToolUseBlock{Type: "tool_use", ID, Name, Input}` に変換し `content` に追加
        - `finish_reason: "tool_calls"` を `stop_reason: "tool_use"` にマッピング
    *   `AnthropicMsg` の `content` が配列形式で `tool_result` ブロックを含む場合の処理

---

#### [MODIFY] [convert_a2o_test.go](file:///shared/libs/go/llmgateway/convert_a2o_test.go)

*   **Description**: ツール変換の単体テストを追加
*   **Technical Design**:
    ```go
    func TestConvertAnthropicRequestToOpenAI_WithTools(t *testing.T)
    func TestConvertOpenAIResponseToAnthropic_WithToolCalls(t *testing.T)
    func TestConvertAnthropicRequestToOpenAI_ToolResultMessage(t *testing.T)
    ```
*   **Logic**:
    *   ツール定義の変換: Anthropic `tools[].input_schema` -> OpenAI `tools[].function.parameters`
    *   ツール呼び出しレスポンス: OpenAI `tool_calls` -> Anthropic `content[].type: "tool_use"`
    *   `finish_reason: "tool_calls"` -> `stop_reason: "tool_use"` のマッピング
    *   ツール結果メッセージ: Anthropic `role: "user", content: [{type: "tool_result", ...}]` -> OpenAI `role: "tool"` メッセージ

---

#### [MODIFY] [proxy_anthropic.go](file:///shared/libs/go/llmgateway/proxy_anthropic.go)

*   **Description**: OpenAI プロバイダへのストリーミング転送対応を追加
*   **Technical Design**:
    *   L147-172 の OpenAI レスポンス変換ブロックを修正
    *   ストリーミング判定: `resp.Header.Get("Content-Type")` に `text/event-stream` が含まれるかで判定
*   **Logic**:
    ```go
    // Cross-provider response conversion (OpenAI -> Anthropic).
    if routed.Provider == "openai" && resp.StatusCode == http.StatusOK {
        // Check if streaming response
        if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
            // Streaming: convert OpenAI SSE -> Anthropic SSE
            w.Header().Set("Content-Type", "text/event-stream")
            w.Header().Set("Cache-Control", "no-cache")
            w.Header().Set("Connection", "keep-alive")
            w.WriteHeader(http.StatusOK)
            if err := ConvertOpenAIStreamToAnthropic(resp.Body, w, routed.Model); err != nil {
                p.logger.Error("stream conversion error", "error", err)
            }
            return
        }
        // Non-streaming: existing conversion logic (Part 1)
        // ... (既存コードそのまま)
    }
    ```

---

#### [MODIFY] [proxy_anthropic_test.go](file:///shared/libs/go/llmgateway/proxy_anthropic_test.go)

*   **Description**: ストリーミングクロスプロバイダ変換の統合テストを追加
*   **Technical Design**:
    ```go
    func TestHandleAnthropicMessages_CrossProviderOpenAI_Streaming(t *testing.T)
    ```
*   **Logic**:
    1. モック OpenAI サーバーが `text/event-stream` で SSE チャンクを返す
    2. Anthropic 形式のリクエスト (`stream: true`) を送信
    3. レスポンスが `text/event-stream` で返り、Anthropic SSE 形式のイベントを含むことを検証
    4. `message_start`, `content_block_delta`, `message_stop` の各イベントが存在することを確認

## Step-by-Step Implementation Guide

### Step 1: ツール変換のテスト作成

*   `convert_a2o_test.go` にツール関連テストを追加
*   `TestConvertAnthropicRequestToOpenAI_WithTools`
*   `TestConvertOpenAIResponseToAnthropic_WithToolCalls`
*   `TestConvertAnthropicRequestToOpenAI_ToolResultMessage`

### Step 2: ツール変換の実装

*   `convert_a2o.go` にツール型定義を追加
*   `AnthropicFullRequest`, `OpenAIRequest`, `OpenAIMsg` にフィールド追加
*   `ConvertAnthropicRequestToOpenAI` にツール変換と `tool_result` メッセージ変換を追加
*   `ConvertOpenAIResponseToAnthropic` に `tool_calls` 変換を追加
*   `mapFinishReason` に `"tool_calls"` -> `"tool_use"` マッピングを追加
*   テスト PASS を確認

### Step 3: ストリーミング変換のテスト作成

*   `stream_converter_test.go` を新規作成
*   テキストストリーミング、ツールストリーミング、空ストリームの各テストケース

### Step 4: ストリーミング変換の実装

*   `stream_converter.go` を新規作成
*   `ConvertOpenAIStreamToAnthropic` を実装
*   テスト PASS を確認

### Step 5: proxy_anthropic.go のストリーミング対応

*   `proxy_anthropic.go` の OpenAI レスポンス変換ブロックにストリーミング分岐を追加
*   `proxy_anthropic_test.go` に `TestHandleAnthropicMessages_CrossProviderOpenAI_Streaming` を追加
*   テスト PASS を確認

### Step 6: ビルド・テスト検証

*   `./scripts/process/build.sh` を実行
*   `./scripts/process/integration_test.sh --specify "TestAgentService|TestModelPassthrough"` を実行

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**:
   ```bash
   ./scripts/process/build.sh
   ```

2. **Integration Tests**:
   ```bash
   ./scripts/process/integration_test.sh --specify "TestAgentService|TestModelPassthrough"
   ```

3. **Log Verification**:
   - LLMGP のログに `"cross-provider conversion"` と `"direction": "anthropic->openai"` が出力されること
   - ストリーミング変換エラーが発生した場合、`"stream conversion error"` がログに記録されること

### テスト項目のセルフレビュー (SS11.4)

1. **網羅性**: ツール変換 (R4) はリクエスト/レスポンスの双方向、ストリーミング (R3) はテキスト/ツール/空の各パターンをカバー。proxy レベルのテストでエンドツーエンドの動作確認も実施。
2. **証拠の十分性**: 各テストで変換後の JSON フィールドを個別に検証。ストリーミングテストでは SSE イベントの `event:` タグと `data:` の内容を両方検証。
3. **迂回の排除**: ストリーミング分岐テストで `Content-Type: text/event-stream` の場合のみストリーム変換が呼ばれることを検証。
4. **依存関係**: ツール型定義 (Step 1-2) -> ストリーム変換 (Step 3-4) -> proxy 統合 (Step 5) のボトムアップ順序。

### 総合判定プロセス (SS12)

Step 6 完了後、testing-rules.md SS12 に従い総合判定を実施する。

## Documentation

#### [MODIFY] [016-LLMGP-CrossProvider-APITranslation.md](file:///prompts/phases/000-foundation/branches/feat-llm-backend/ideas/016-LLMGP-CrossProvider-APITranslation.md)
*   **更新内容**: R3 (ストリーミング変換), R4 (ツール変換) の実装状態を「完了」に更新
