# 025-LLMGP-ResponsesAPI-CodexSupport

> **Source Specification**: [017-LLMGP-ResponsesAPI-CodexSupport.md](file:///prompts/phases/000-foundation/branches/feat-llm-backend/ideas/017-LLMGP-ResponsesAPI-CodexSupport.md)

## Goal Description

LLMGP に OpenAI Responses API (`/v1/responses`) のサポートを追加し、Codex モデル (`codex-mini-latest`, `gpt-5.2-codex` 等) を Anthropic Messages API エンドポイント (`/v1/messages`) 経由で利用可能にする。`model_profiles.yaml` の `mode` フィールドで API モードを指定し、`proxy_anthropic.go` 内で Chat Completions API と Responses API のルーティングを動的に切り替える。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: model_profiles.yaml に `mode` フィールドを追加 | Proposed Changes > config/model_profiles.go |
| R2: RoutedModel に Mode 情報を伝播 | Proposed Changes > llmgateway/routing.go |
| R3: Anthropic -> Responses API リクエスト変換 | Proposed Changes > llmgateway/convert_a2r.go |
| R4: Responses API レスポンス -> Anthropic 変換 (非ストリーミング) | Proposed Changes > llmgateway/convert_a2r.go |
| R5: Responses API ストリーミング変換 | Proposed Changes > llmgateway/convert_a2r.go |
| R6: proxy_anthropic.go での mode ルーティング | Proposed Changes > llmgateway/proxy_anthropic.go |
| R7: tool_result の変換対応 (任意) | Proposed Changes > llmgateway/convert_a2r.go |

---

## Proposed Changes

### config パッケージ

#### [MODIFY] [model_profiles.go](file:///shared/libs/go/config/model_profiles.go)

*   **Description**: `ModelConfig` に `Mode` フィールドを追加
*   **Technical Design**:
    ```go
    // ModelConfig holds per-model configuration.
    type ModelConfig struct {
        Name         string         `yaml:"name"`
        LogicalName  string         `yaml:"logical_name,omitempty"`
        Mode         string         `yaml:"mode,omitempty"`    // "chat" (default) or "responses"
        Behavior     *ModelBehavior `yaml:"behavior,omitempty"`
    }
    ```
*   **Logic**:
    - `Mode` が空文字の場合、呼び出し側で `"chat"` として扱う
    - `"responses"` が指定された場合、OpenAI Responses API (`/v1/responses`) にルーティング

---

### llmgateway パッケージ

#### [MODIFY] [routing_test.go](file:///shared/libs/go/llmgateway/routing_test.go)

*   **Description**: RoutedModel の Mode フィールド伝播テストを追加 (TDD: テストを先に記述)
*   **Technical Design**:
    ```go
    func TestModelRouter_ResolveModel_WithMode(t *testing.T) {
        tests := []struct {
            name      string
            modelName string
            wantMode  string
        }{
            {"chat mode explicit", "gpt-4o", "chat"},
            {"responses mode", "codex-mini-latest", "responses"},
            {"empty mode defaults", "gpt-4.1-mini", ""},
        }
        // ...
    }
    ```
*   **Logic**:
    - model_profiles に `mode: "responses"` を持つモデルを定義
    - `ResolveModel()` の返り値 `RoutedModel.Mode` が正しいことを検証
    - `mode` 未指定の場合は空文字が返ることを検証

#### [MODIFY] [routing.go](file:///shared/libs/go/llmgateway/routing.go)

*   **Description**: `RoutedModel` に `Mode` フィールドを追加、`ResolveModel()` で伝播
*   **Technical Design**:
    ```go
    // RoutedModel holds the resolved provider, key value, and model name.
    type RoutedModel struct {
        Provider         string
        KeyName          string
        KeyValue         string
        Model            string
        Mode             string // "chat", "responses", or "" (treated as "chat")
        ToolCallFallback bool
    }
    ```
*   **Logic**:
    - `ResolveModel()` 内で `model.Mode` を `RoutedModel.Mode` にコピーする
    - 既存の `ToolCallFallback` と同じパターンで、`config.ModelConfig` からフィールドを読み取る
    - 変更箇所 (L56付近):
    ```go
    resolved = &RoutedModel{
        Provider:         providerName,
        KeyName:          key.Name,
        KeyValue:         key.Value,
        Model:            modelName,
        Mode:             model.Mode,             // 追加
        ToolCallFallback: fallback,
    }
    ```

#### [NEW] [convert_a2r_test.go](file:///shared/libs/go/llmgateway/convert_a2r_test.go)

*   **Description**: Anthropic <-> Responses API 変換のユニットテスト (TDD: テストを先に記述)
*   **Technical Design**:
    ```go
    // --- リクエスト変換テスト ---
    func TestConvertAnthropicRequestToResponses_BasicText(t *testing.T)
    func TestConvertAnthropicRequestToResponses_WithSystem(t *testing.T)
    func TestConvertAnthropicRequestToResponses_WithTools(t *testing.T)
    func TestConvertAnthropicRequestToResponses_MaxTokensClamp(t *testing.T)
    func TestConvertAnthropicRequestToResponses_Stream(t *testing.T)
    func TestConvertAnthropicRequestToResponses_ToolResultMessage(t *testing.T) // R7

    // --- レスポンス変換テスト (非ストリーミング) ---
    func TestConvertResponsesResponseToAnthropic_TextOnly(t *testing.T)
    func TestConvertResponsesResponseToAnthropic_WithToolCalls(t *testing.T)
    func TestConvertResponsesResponseToAnthropic_EmptyOutput(t *testing.T)

    // --- ストリーミング変換テスト ---
    func TestConvertResponsesStreamToAnthropic_TextStream(t *testing.T)
    func TestConvertResponsesStreamToAnthropic_ToolCallStream(t *testing.T)
    func TestConvertResponsesStreamToAnthropic_EventOrdering(t *testing.T)
    ```
*   **Logic** (テストケース概要):
    - **BasicText**: `{"messages": [{"role": "user", "content": "hello"}]}` -> `{"input": [{"role": "user", "content": "hello"}]}`
    - **WithSystem**: `{"system": "You are helpful"}` -> `input` 先頭に `{"role": "developer", "content": "You are helpful"}`
    - **WithTools**: `tools[].input_schema` -> `tools[].parameters` (function型)
    - **MaxTokensClamp**: `max_tokens: 32000` -> `max_output_tokens: 16384`
    - **Stream**: `stream: true` -> `stream: true` が維持される
    - **ToolResultMessage**: `tool_result` ブロック -> `function_call_output` メッセージ
    - **TextOnly(resp)**: `output[type=message].content[type=output_text]` -> `content[type=text]`
    - **WithToolCalls(resp)**: `output[type=function_call]` -> `content[type=tool_use]`
    - **EmptyOutput(resp)**: 空の output -> 空の content
    - **TextStream**: `response.output_text.delta` イベント -> `content_block_delta` (text_delta)
    - **ToolCallStream**: `response.function_call_arguments.delta` -> `content_block_delta` (input_json_delta)
    - **EventOrdering**: `message_start` -> `content_block_start` -> `content_block_delta` -> `content_block_stop` -> `message_delta` -> `message_stop`

#### [NEW] [convert_a2r.go](file:///shared/libs/go/llmgateway/convert_a2r.go)

*   **Description**: Anthropic <-> OpenAI Responses API の双方向変換ロジック
*   **Technical Design**:

    Responses API リクエスト型:
    ```go
    // ResponsesRequest represents an OpenAI Responses API request body.
    type ResponsesRequest struct {
        Model           string             `json:"model"`
        Input           []ResponsesInput   `json:"input"`
        MaxOutputTokens *int               `json:"max_output_tokens,omitempty"`
        Temperature     *float64           `json:"temperature,omitempty"`
        Stream          *bool              `json:"stream,omitempty"`
        Tools           []ResponsesTool    `json:"tools,omitempty"`
    }

    // ResponsesInput represents an input message for Responses API.
    type ResponsesInput struct {
        Role    string `json:"role"`              // "user", "assistant", "developer"
        Content string `json:"content,omitempty"`
        // function_call_output fields
        Type   string `json:"type,omitempty"`     // "function_call_output"
        CallID string `json:"call_id,omitempty"`
        Output string `json:"output,omitempty"`
    }

    // ResponsesTool represents a tool definition for Responses API.
    type ResponsesTool struct {
        Type        string          `json:"type"`       // "function"
        Name        string          `json:"name"`
        Description string          `json:"description,omitempty"`
        Parameters  json.RawMessage `json:"parameters"`
    }
    ```

    Responses API レスポンス型:
    ```go
    // ResponsesResponse represents an OpenAI Responses API response.
    type ResponsesResponse struct {
        ID     string            `json:"id"`
        Status string            `json:"status"`
        Output []ResponsesOutput `json:"output"`
        Usage  *ResponsesUsage   `json:"usage,omitempty"`
    }

    // ResponsesOutput represents an output item in Responses API.
    type ResponsesOutput struct {
        Type      string                  `json:"type"` // "message" or "function_call"
        Content   []ResponsesContentBlock `json:"content,omitempty"`
        // function_call fields
        CallID    string `json:"call_id,omitempty"`
        Name      string `json:"name,omitempty"`
        Arguments string `json:"arguments,omitempty"`
    }

    // ResponsesContentBlock represents a content block in Responses API output.
    type ResponsesContentBlock struct {
        Type string `json:"type"` // "output_text"
        Text string `json:"text"`
    }

    // ResponsesUsage represents usage stats in Responses API.
    type ResponsesUsage struct {
        InputTokens  int `json:"input_tokens"`
        OutputTokens int `json:"output_tokens"`
        TotalTokens  int `json:"total_tokens"`
    }
    ```

*   **Logic**:

    **`ConvertAnthropicRequestToResponses(body []byte) ([]byte, error)`**:
    1. `AnthropicFullRequest` に Unmarshal
    2. `ResponsesRequest` を構築:
       - `Model` <- `req.Model`
       - `Temperature` <- `req.Temperature`
       - `Stream` <- `req.Stream`
    3. `max_tokens` の変換: `req.MaxTokens` -> `MaxOutputTokens` (openAIMaxCompletionTokens=16384 でクランプ)
    4. `system` フィールドの変換:
       - `extractText(req.System)` でテキスト取得
       - `Input` 先頭に `{Role: "developer", Content: systemText}` を追加
       - (注: Responses API では system -> developer ロール)
    5. `messages` の変換: 各メッセージを `ResponsesInput` に変換
       - `role: "user"` -> `role: "user"`
       - `role: "assistant"` -> `role: "assistant"`
       - content が文字列の場合: `Content` にそのまま設定
       - content が配列の場合: `text` ブロックのテキストを結合
       - `tool_result` ブロック (R7): `{Type: "function_call_output", CallID: block.ToolUseID, Output: block.Content}` に変換
    6. `tools` の変換: `AnthropicTool` -> `ResponsesTool`
       - `Type: "function"`
       - `Name` <- `tool.Name`
       - `Description` <- `tool.Description`
       - `Parameters` <- `tool.InputSchema`
    7. JSON Marshal して返却

    **`ConvertResponsesResponseToAnthropic(body []byte, model string) ([]byte, error)`**:
    1. `ResponsesResponse` に Unmarshal
    2. `AnthropicResponse` を構築:
       - `ID` <- `resp.ID`
       - `Type` <- `"message"`
       - `Role` <- `"assistant"`
       - `Model` <- `model`
    3. `output` の変換:
       - `type: "message"` -> `content` 内の `output_text` を `ContentBlock{Type: "text", Text: block.Text}` に変換
       - `type: "function_call"` -> `ContentBlock{Type: "tool_use", ID: out.CallID, Name: out.Name, Input: out.Arguments}` に変換
    4. `stop_reason` の判定:
       - tool_use ブロックがある場合: `"tool_use"`
       - `resp.Status == "completed"`: `"end_turn"`
       - それ以外: `"end_turn"`
    5. `usage` の変換:
       - `InputTokens` <- `resp.Usage.InputTokens`
       - `OutputTokens` <- `resp.Usage.OutputTokens`
    6. JSON Marshal して返却

    **`ConvertResponsesStreamToAnthropic(reader io.Reader, writer io.Writer, model string) error`**:
    1. SSE パーサーで `reader` からイベントを読み取る (bufio.Scanner)
    2. イベントタイプに応じて Anthropic SSE を生成:

    | Responses API Event | Anthropic SSE Event | 出力内容 |
    |---|---|---|
    | (初回呼び出し時) | `message_start` | `{"type":"message_start","message":{"id":"...","type":"message","role":"assistant","model":"MODEL","content":[]}}` |
    | `response.output_text.delta` | `content_block_delta` | `{"type":"content_block_delta","index":IDX,"delta":{"type":"text_delta","text":"DELTA"}}` |
    | `response.function_call_arguments.delta` | `content_block_delta` | `{"type":"content_block_delta","index":IDX,"delta":{"type":"input_json_delta","partial_json":"DELTA"}}` |
    | `response.output_item.added` (function_call) | `content_block_start` | `{"type":"content_block_start","index":IDX,"content_block":{"type":"tool_use","id":"CALL_ID","name":"NAME","input":{}}}` |
    | `response.completed` | `message_delta` + `message_stop` | stop_reason + stop |

    3. 各イベントを `event: EVENT_TYPE\ndata: JSON\n\n` 形式で writer に書き込み、`Flush()`

#### [MODIFY] [proxy_anthropic_test.go](file:///shared/libs/go/llmgateway/proxy_anthropic_test.go)

*   **Description**: Responses API ルーティングの統合テスト (TDD)
*   **Technical Design**:
    ```go
    func TestHandleAnthropicMessages_ResponsesMode_NonStream(t *testing.T)
    func TestHandleAnthropicMessages_ResponsesMode_Stream(t *testing.T)
    func TestHandleAnthropicMessages_ChatMode_Unchanged(t *testing.T)
    ```
*   **Logic**:
    - **ResponsesMode_NonStream**: mode="responses" のモデルでリクエスト -> `/v1/responses` にフォワード -> Anthropic 形式で返却を検証
    - **ResponsesMode_Stream**: mode="responses" + stream=true -> SSE 変換を検証
    - **ChatMode_Unchanged**: mode="" (デフォルト) のモデルが従来通り `/v1/chat/completions` に転送されることを検証
    - モック HTTP サーバーで Responses API レスポンスをシミュレート

#### [MODIFY] [proxy_anthropic.go](file:///shared/libs/go/llmgateway/proxy_anthropic.go)

*   **Description**: `handleAnthropicMessages` に `mode: "responses"` のルーティング分岐を追加
*   **Technical Design**:
    - L98-127 の `switch routed.Provider` の `case "openai"` 内を変更
*   **Logic**:
    - 既存の `case "openai"` ブロックを以下のように拡張:
    ```go
    case "openai":
        if routed.Mode == "responses" {
            // Responses API route
            forwardPath = "/v1/responses"
            converted, convErr := ConvertAnthropicRequestToResponses(body)
            if convErr != nil {
                WriteErrorResponse(w, &GatewayError{...})
                return
            }
            forwardBody = converted
            p.logger.Info("cross-provider conversion",
                "direction", "anthropic->responses",
                "model", routed.Model)
        } else {
            // Chat Completions API route (既存)
            forwardPath = "/v1/chat/completions"
            converted, convErr := ConvertAnthropicRequestToOpenAI(body)
            // ...
        }
    ```
    - L148 付近のレスポンス変換部分も拡張:
    ```go
    if routed.Provider == "openai" && resp.StatusCode == http.StatusOK {
        if routed.Mode == "responses" {
            // Responses API response conversion
            if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
                // Streaming
                w.Header().Set("Content-Type", "text/event-stream")
                w.Header().Set("Cache-Control", "no-cache")
                w.Header().Set("Connection", "keep-alive")
                w.WriteHeader(http.StatusOK)
                if streamErr := ConvertResponsesStreamToAnthropic(resp.Body, w, routed.Model); streamErr != nil {
                    p.logger.Error("responses stream conversion error", "error", streamErr)
                }
                return
            }
            // Non-streaming
            respBody, _ := io.ReadAll(resp.Body)
            converted, convErr := ConvertResponsesResponseToAnthropic(respBody, routed.Model)
            if convErr != nil {
                WriteErrorResponse(w, &GatewayError{...})
                return
            }
            w.Header().Set("Content-Type", "application/json")
            w.WriteHeader(http.StatusOK)
            w.Write(converted)
            return
        }
        // 既存の Chat Completions 変換 (変更なし)
        // ...
    }
    ```

---

### model_profiles.yaml 設定

#### [MODIFY] [model_profiles.yaml](file:///examples/standalone/model_profiles.yaml)

*   **Description**: Codex モデルに `mode: responses` を追加
*   **Logic**:
    ```yaml
    openai:
      keys:
        - name: default
          value: vault://providers/openai/default
          models:
            - name: gpt-4o
            - name: gpt-4o-mini
            - name: gpt-4.1-mini
            - name: gpt-5.4-mini
            - name: gpt-5.5
            - name: codex-mini-latest
              mode: responses
    ```

#### [MODIFY] [model_profiles.yaml (test)](file:///tests/testdata/model_profiles.yaml)

*   **Description**: テスト用設定にも同様に `mode: responses` を追加
*   **Logic**: standalone 版と同じパターンで `codex-mini-latest` に `mode: responses` を設定

---

## Step-by-Step Implementation Guide

- [x] **Step 1: config パッケージに Mode フィールド追加**
  - `shared/libs/go/config/model_profiles.go` の `ModelConfig` 構造体に `Mode string` フィールドを追加

- [x] **Step 2: routing のテストを先に記述 (TDD)**
  - `shared/libs/go/llmgateway/routing_test.go` に `TestModelRouter_ResolveModel_WithMode` を追加
  - テストが失敗することを確認 (Mode フィールドがまだ RoutedModel にないため)

- [x] **Step 3: routing.go に Mode 伝播を実装**
  - `RoutedModel` 構造体に `Mode string` フィールドを追加
  - `ResolveModel()` で `model.Mode` を `RoutedModel.Mode` にコピー
  - Step 2 のテストが成功することを確認

- [x] **Step 4: 変換テストを先に記述 (TDD)**
  - `shared/libs/go/llmgateway/convert_a2r_test.go` を新規作成
  - リクエスト変換、レスポンス変換、ストリーミング変換のテストケースを記述
  - テストが失敗することを確認 (変換関数がまだないため)

- [x] **Step 5: 変換ロジックの実装**
  - `shared/libs/go/llmgateway/convert_a2r.go` を新規作成
  - `ConvertAnthropicRequestToResponses()` を実装
  - `ConvertResponsesResponseToAnthropic()` を実装
  - `ConvertResponsesStreamToAnthropic()` を実装
  - Step 4 のテストが成功することを確認

- [x] **Step 6: proxy 統合テストを先に記述 (TDD)**
  - `shared/libs/go/llmgateway/proxy_anthropic_test.go` に Responses API ルーティングテストを追加
  - テストが失敗することを確認

- [x] **Step 7: proxy_anthropic.go にルーティング分岐を実装**
  - `case "openai"` 内に `routed.Mode == "responses"` の分岐を追加
  - レスポンス変換部分にも同様の分岐を追加
  - Step 6 のテストが成功することを確認

- [x] **Step 8: model_profiles.yaml の更新**
  - `examples/standalone/model_profiles.yaml` に `mode: responses` を追加
  - `tests/testdata/model_profiles.yaml` に `mode: responses` を追加

- [x] **Step 9: ビルド + 単体テスト**
  - `./scripts/process/build.sh` を実行
  - 全テストが PASS することを確認

- [x] **Step 10: 統合テスト (E2E)**
  - `./scripts/process/integration_test.sh --specify "TestCrossProvider|TestResponses|TestModelPassthrough"` を実行
  - 既存テストのリグレッションがないことを確認

---

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**:
   ```bash
   ./scripts/process/build.sh
   ```

2. **LLM Integration Tests (E2E)**:
   ```bash
   ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestCrossProvider|TestResponses|TestModelPassthrough"
   ```
   *   **Log Verification**:
       - `cross-provider conversion` ログに `direction=anthropic->responses` が出力されること
       - Responses API テストで `message_start`, `content_block_delta`, `message_stop` イベントが正しく生成されること
       - 既存の `TestCrossProvider_OpenAI_via_AnthropicEndpoint_NonStream` / `_Stream` がリグレッションしないこと

3. **リグレッション確認 (全 LLM テスト)**:
   ```bash
   ./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories "llm"
   ```

### テスト項目のセルフレビュー (11.4)

1. **網羅性の検証**: ボトムアップで config -> routing -> convert -> proxy の各レイヤーをテストし、最後に E2E で全体を通して検証。全要件 R1-R7 に対応するテストが存在する。
2. **証拠の十分性**: 変換テストではリクエスト/レスポンスの JSON 構造を具体的に検証。ストリーミングテストでは SSE イベントの順序と内容を検証。
3. **迂回・抜け道の排除**: proxy テストで `mode: "responses"` が実際に `/v1/responses` パスに転送されることを検証。`mode: ""` の場合に従来の `/v1/chat/completions` に転送されることも検証。
4. **依存関係の整合性**: config (Step 1) -> routing (Step 3) -> convert (Step 5) -> proxy (Step 7) の順序で、各レイヤーが動作確認済みの上位レイヤーに依存。

### 総合判定プロセス (12)

全テスト完了後、以下のチェック項目を確認:
1. スキップされたテストの有無
2. テストログ内の ERROR/WARN/panic の確認
3. フォールバック処理による偽成功の排除
4. mode ルーティングが正しいアダプタを使用していることの確認
5. テスト実行順序に依存しないことの確認

---

## Documentation

#### [MODIFY] [016-LLMGP-CrossProvider-APITranslation.md](file:///prompts/phases/000-foundation/branches/feat-llm-backend/ideas/016-LLMGP-CrossProvider-APITranslation.md)
*   **更新内容**: Responses API 対応が追加されたことを備考として追記
