# 023-LLMGP-CrossProvider-APITranslation

> **Source Specification**: [016-LLMGP-CrossProvider-APITranslation.md](file:///prompts/phases/000-foundation/branches/feat-llm-backend/ideas/016-LLMGP-CrossProvider-APITranslation.md)

## Goal Description

Claude CLI が常に Anthropic Messages API (`POST /v1/messages`) でリクエストを送信する中、
LLMGP 内にリクエスト/レスポンス変換レイヤーを追加し、OpenAI モデル (`gpt-4o` 等) への
クロスプロバイダルーティングを実現する。

## User Review Required

> [!IMPORTANT]
> ストリーミング変換 (R3) はフローが複雑なため、本計画では非ストリーミング応答の変換を先に実装し、
> ストリーミング変換は後続の Part で実装する方針としています。この分割方針について確認をお願いします。

> [!IMPORTANT]
> ツール呼び出し変換 (R4) も同様に Part 2 で扱います。まず Part 1 でテキスト応答の変換を安定させます。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: Anthropic -> OpenAI リクエスト変換 | Proposed Changes > `convert_a2o.go` |
| R2: OpenAI -> Anthropic レスポンス変換 | Proposed Changes > `convert_a2o.go` |
| R3: ストリーミングレスポンス変換 | **Part 2 に先送り**: ストリーム変換は SSE パーサーの実装が必要で複雑なため |
| R4: ツール呼び出し変換 | **Part 2 に先送り**: テキスト応答の変換が安定してから実装する |
| R5: エラーハンドリング | Proposed Changes > `convert_a2o.go` + `proxy_anthropic.go` |
| R6: R4 パススルー統合テスト | Proposed Changes > 統合テスト |
| R7: 変換対象外フィールドの透過 | Proposed Changes > `convert_a2o.go` (unknown fields are ignored) |

## Proposed Changes

### llmgateway パッケージ

#### [NEW] [convert_a2o_test.go](file:///shared/libs/go/llmgateway/convert_a2o_test.go)

*   **Description**: Anthropic <-> OpenAI API 変換の単体テスト
*   **Technical Design**:
    ```go
    func TestConvertAnthropicRequestToOpenAI(t *testing.T)
    func TestConvertOpenAIResponseToAnthropic(t *testing.T)
    func TestConvertAnthropicRequestToOpenAI_SystemMessage(t *testing.T)
    func TestConvertAnthropicRequestToOpenAI_ContentBlocks(t *testing.T)
    func TestConvertOpenAIErrorToAnthropic(t *testing.T)
    ```
*   **Logic**:
    *   テーブル駆動テストで以下のケースを検証:
        *   基本テキストメッセージ (user/assistant の role 変換)
        *   `system` フィールドのトップレベル -> messages[0] への移動
        *   `content` が配列形式 (text ブロック) の場合の変換
        *   `max_tokens`, `temperature`, `stream` の透過
        *   OpenAI レスポンス -> Anthropic レスポンスの変換 (content, usage, stop_reason)
        *   OpenAI エラーレスポンス -> Anthropic エラーレスポンスの変換
        *   空メッセージ、未知フィールドへの耐性

#### [NEW] [convert_a2o.go](file:///shared/libs/go/llmgateway/convert_a2o.go)

*   **Description**: Anthropic Messages API <-> OpenAI Chat Completions API の変換ロジック
*   **Technical Design**:
    ```go
    // AnthropicFullRequest represents the full Anthropic Messages API request body.
    type AnthropicFullRequest struct {
        Model       string          `json:"model"`
        Messages    []AnthropicMsg  `json:"messages"`
        System      json.RawMessage `json:"system,omitempty"`   // string or []ContentBlock
        MaxTokens   int             `json:"max_tokens"`
        Temperature *float64        `json:"temperature,omitempty"`
        Stream      *bool           `json:"stream,omitempty"`
    }

    // AnthropicMsg represents a message in Anthropic format.
    type AnthropicMsg struct {
        Role    string          `json:"role"`
        Content json.RawMessage `json:"content"` // string or []ContentBlock
    }

    // ContentBlock represents a content block in Anthropic format.
    type ContentBlock struct {
        Type string `json:"type"`
        Text string `json:"text,omitempty"`
    }

    // OpenAIRequest represents the OpenAI Chat Completions API request body.
    type OpenAIRequest struct {
        Model       string       `json:"model"`
        Messages    []OpenAIMsg  `json:"messages"`
        MaxTokens   *int         `json:"max_tokens,omitempty"`
        Temperature *float64     `json:"temperature,omitempty"`
        Stream      *bool        `json:"stream,omitempty"`
    }

    // OpenAIMsg represents a message in OpenAI format.
    type OpenAIMsg struct {
        Role    string `json:"role"`
        Content string `json:"content"`
    }

    // OpenAIResponse represents the OpenAI Chat Completions API response.
    type OpenAIResponse struct {
        ID      string         `json:"id"`
        Choices []OpenAIChoice `json:"choices"`
        Usage   OpenAIUsage    `json:"usage"`
    }

    type OpenAIChoice struct {
        Message      OpenAIMsg `json:"message"`
        FinishReason string    `json:"finish_reason"`
    }

    type OpenAIUsage struct {
        PromptTokens     int `json:"prompt_tokens"`
        CompletionTokens int `json:"completion_tokens"`
    }

    // AnthropicResponse represents the Anthropic Messages API response.
    type AnthropicResponse struct {
        ID         string             `json:"id"`
        Type       string             `json:"type"` // always "message"
        Role       string             `json:"role"` // always "assistant"
        Content    []ContentBlock     `json:"content"`
        Model      string             `json:"model"`
        StopReason string             `json:"stop_reason"`
        Usage      AnthropicUsage     `json:"usage"`
    }

    type AnthropicUsage struct {
        InputTokens  int `json:"input_tokens"`
        OutputTokens int `json:"output_tokens"`
    }

    // ConvertAnthropicRequestToOpenAI converts Anthropic Messages API request to
    // OpenAI Chat Completions API request.
    func ConvertAnthropicRequestToOpenAI(body []byte) ([]byte, error)

    // ConvertOpenAIResponseToAnthropic converts OpenAI Chat Completions API response
    // to Anthropic Messages API response format.
    func ConvertOpenAIResponseToAnthropic(body []byte, model string) ([]byte, error)
    ```
*   **Logic**:
    *   `ConvertAnthropicRequestToOpenAI`:
        1. JSON デコード -> `AnthropicFullRequest`
        2. `system` フィールドが存在する場合、`OpenAIMsg{Role: "system", Content: systemText}` を messages の先頭に挿入
        3. 各 `AnthropicMsg` を `OpenAIMsg` に変換:
            - `content` が文字列の場合: そのまま `Content` に代入
            - `content` が配列の場合: 各 `ContentBlock` の `text` を結合
        4. `model`, `max_tokens`, `temperature`, `stream` を透過コピー
        5. JSON エンコードして返す
    *   `ConvertOpenAIResponseToAnthropic`:
        1. JSON デコード -> `OpenAIResponse`
        2. `choices[0].message.content` -> `ContentBlock{Type: "text", Text: ...}`
        3. `choices[0].finish_reason` -> `stop_reason` マッピング:
            - `"stop"` -> `"end_turn"`
            - `"length"` -> `"max_tokens"`
            - その他 -> そのまま
        4. `usage` の変換: `prompt_tokens` -> `input_tokens`, `completion_tokens` -> `output_tokens`
        5. `id`, `model` を設定
        6. JSON エンコードして返す

---

#### [MODIFY] [proxy_anthropic.go](file:///shared/libs/go/llmgateway/proxy_anthropic.go)

*   **Description**: `handleAnthropicMessages` にプロバイダ分岐ロジックを追加
*   **Technical Design**:
    ```go
    // handleAnthropicMessages の L92-100 を修正
    // 既存: 常に routed.Provider + "/v1/messages" に転送
    // 修正後: プロバイダに応じてパスと body を変換

    // routed.Provider が "anthropic" の場合:
    //   従来通り forwardToProvider("anthropic", "/v1/messages", forwardBody, ...)
    // routed.Provider が "openai" の場合:
    //   1. ConvertAnthropicRequestToOpenAI(forwardBody) でリクエスト変換
    //   2. forwardToProvider("openai", "/v1/chat/completions", convertedBody, ...)
    //   3. レスポンスが非ストリーミングの場合:
    //      ConvertOpenAIResponseToAnthropic(respBody) でレスポンス変換
    //   4. レスポンスがストリーミングの場合: (Part 2 で実装)
    //      現時点では stream: true はエラーとして返す
    ```
*   **Logic**:
    *   L92-100 の `forwardToProvider` 呼び出しを以下のロジックに置換:
    ```go
    var (
        forwardPath string
        forwardBody = body // default
    )

    switch routed.Provider {
    case "anthropic":
        forwardPath = "/v1/messages"
        if routed.Model != req.Model {
            forwardBody = rewriteModelField(body, req.Model, routed.Model)
        }
    case "openai":
        forwardPath = "/v1/chat/completions"
        converted, err := ConvertAnthropicRequestToOpenAI(body)
        if err != nil {
            WriteErrorResponse(w, &GatewayError{
                Type:    "api_error",
                Message: "failed to convert request: " + err.Error(),
                Code:    "conversion_error",
                Status:  http.StatusInternalServerError,
            })
            return
        }
        forwardBody = converted
    default:
        WriteErrorResponse(w, &GatewayError{
            Type:    "api_error",
            Message: "cross-provider translation not supported for: " + routed.Provider,
            Code:    "unsupported_translation",
            Status:  http.StatusBadRequest,
        })
        return
    }
    ```
    *   レスポンス処理にもプロバイダ分岐を追加:
    ```go
    // openai プロバイダの場合、レスポンスを Anthropic 形式に変換
    if routed.Provider == "openai" && resp.StatusCode == http.StatusOK {
        respBody, err := io.ReadAll(resp.Body)
        if err != nil {
            WriteErrorResponse(w, &GatewayError{...})
            return
        }
        converted, err := ConvertOpenAIResponseToAnthropic(respBody, routed.Model)
        if err != nil {
            WriteErrorResponse(w, &GatewayError{...})
            return
        }
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusOK)
        w.Write(converted)
        return
    }
    ```

---

### 統合テスト

#### [MODIFY] [agentservice_integration_test.go](file:///tests/agentservice_integration_test.go)

*   **Description**: R6 パススルー統合テストの追加
*   **Technical Design**:
    ```go
    // TestModelPassthroughToLLMGP verifies that the model name specified in
    // session creation is passed through to the LLMGP proxy.
    func TestModelPassthroughToLLMGP(t *testing.T)
    ```
*   **Logic**:
    1. モック LLMGP サーバーを起動し、受信した `POST /v1/messages` のリクエストボディをキャプチャ
    2. `agentservice` + `claudecode` (モック) でセッション作成 (`model: "gpt-4o"`)
    3. モック LLMGP が受信したリクエストの `model` フィールドが `"gpt-4o"` であることを検証

## Step-by-Step Implementation Guide

### Step 1: 変換ロジックのテスト作成

*   Create `shared/libs/go/llmgateway/convert_a2o_test.go`
*   テーブル駆動テストで以下のケースを実装:
    - 基本テキスト変換 (user -> user, assistant -> assistant)
    - system メッセージの挿入
    - content 配列形式の結合
    - max_tokens / temperature の透過
    - OpenAI レスポンス -> Anthropic レスポンスの変換
    - finish_reason -> stop_reason のマッピング
    - usage フィールドの変換

### Step 2: 変換ロジックの実装

*   Create `shared/libs/go/llmgateway/convert_a2o.go`
*   `ConvertAnthropicRequestToOpenAI` と `ConvertOpenAIResponseToAnthropic` を実装
*   テストが全て PASS することを確認

### Step 3: proxy_anthropic.go のプロバイダ分岐テスト更新

*   `shared/libs/go/llmgateway/proxy_anthropic_test.go` にクロスプロバイダ変換テストを追加
*   モックアップストリームサーバーで OpenAI 形式のレスポンスを返し、Anthropic 形式に変換されることを検証

### Step 4: proxy_anthropic.go の本体修正

*   `handleAnthropicMessages` にプロバイダ分岐ロジックを実装
*   `openai` の場合は `ConvertAnthropicRequestToOpenAI` -> `forwardToProvider` -> `ConvertOpenAIResponseToAnthropic` のフローを実装

### Step 5: パススルー統合テストの作成

*   `tests/agentservice_integration_test.go` に `TestModelPassthroughToLLMGP` を追加
*   モック LLMGP サーバーでリクエストのモデル名をキャプチャして検証

### Step 6: ビルド・テスト検証

*   `./scripts/process/build.sh` を実行
*   `./scripts/process/integration_test.sh --specify "TestAgentService"` を実行

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**:
   ```bash
   ./scripts/process/build.sh
   ```

2. **Integration Tests** (AgentService 関連のみ):
   ```bash
   ./scripts/process/integration_test.sh --specify "TestAgentService|TestModelPassthrough"
   ```

3. **Log Verification**:
   - LLMGP のログに `"anthropic request routed"` と `"provider": "openai"` が出力されていること
   - 変換エラーが発生した場合、適切なエラーレスポンスが返されていること

### テスト項目のセルフレビュー (SS11.4)

1. **網羅性**: リクエスト変換 (R1)、レスポンス変換 (R2)、エラー変換 (R5) の単体テストと、
   パススルー検証 (R6) の統合テストで、Part 1 スコープの全要件をカバー。
2. **証拠の十分性**: 各テストで JSON の具体的なフィールド値を比較し、「変換が正しい」ことを検証。
   単に「エラーが出ない」ではなく「期待する JSON が返る」を確認。
3. **迂回の排除**: プロバイダ分岐テストで `routed.Provider == "openai"` の場合のみ変換が呼ばれることを検証。
4. **依存関係**: 変換ロジック (Step 1-2) -> proxy 統合 (Step 3-4) -> パススルー (Step 5) の
   ボトムアップ順序で実装・テスト。

### 総合判定プロセス (SS12)

Step 6 完了後、testing-rules.md SS12 に従い総合判定を実施する。

## 継続計画について

本計画は Part 1 (テキスト応答の非ストリーミング変換) のみを対象とする。
以下は Part 2 として別計画で実装予定:

- R3: ストリーミングレスポンス変換 (OpenAI SSE -> Anthropic SSE)
- R4: ツール呼び出し変換 (tools 定義 + tool_calls レスポンス)
- `stream: true` の場合のエンドツーエンド動作

## Documentation

#### [MODIFY] [015-Gateway-ModelDiscovery-And-LogicalNames.md](file:///prompts/phases/000-foundation/branches/feat-llm-backend/ideas/015-Gateway-ModelDiscovery-And-LogicalNames.md)
*   **更新内容**: R4 (パススルー統合テスト) の実装状態を「完了」に更新
