# 035-Gemini-Support

> **Source Specification**: [025-Gemini-Support.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/025-Gemini-Support.md)

## Goal Description

LLM Gateway Proxy (LLMGP) に Google プロバイダ向けの翻訳・変換層（Translation Layer）を新規追加し、`cawa-client` (Claude Code CLI) から Google Gemini モデル (`gemini-3.5-flash` 等) を使用した際に、対話および Function Calling (ツール実行) がストリーミング・非ストリーミングの両モードで正しく中継・実行されるようにします。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: リクエストの変換 (Anthropic -> Google Gemini) | Proposed Changes > convert_a2g.go (ConvertAnthropicRequestToGemini) |
| R1: ツール定義の JSON スキーマ大文字化 | Proposed Changes > convert_a2g.go (convertSchemaTypesToUppercase) |
| R2: 非ストリームレスポンスの変換 (Gemini -> Anthropic) | Proposed Changes > convert_a2g.go (ConvertGeminiResponseToAnthropic) |
| R2: ストリームレスポンス (SSE) の変換 (Gemini -> Anthropic) | Proposed Changes > convert_a2g.go (ConvertGeminiStreamToAnthropic) |
| R3: Gateway フォワード処理の追加 (`case "google"`) | Proposed Changes > proxy_anthropic.go (handleAnthropicMessages) |
| R3: Gemini 認証ヘッダーの付加 (`x-goog-api-key`) | Proposed Changes > proxy_anthropic.go (handleAnthropicMessages) |

---

## Proposed Changes

### llmgateway パッケージ (Google Gemini 変換層)

---

#### [NEW] [convert_a2g.go](file://shared/libs/go/llmgateway/convert_a2g.go)
*   **Description**: Anthropic <=> Google Gemini API 間のJSONデータ定義、リクエスト/レスポンス変換処理、および SSE ストリーム変換処理を実装します。
*   **Technical Design**:
    ```go
    package llmgateway

    import (
        "bufio"
        "encoding/json"
        "fmt"
        "io"
        "net/http"
        "strings"

        "github.com/axsh/hag/logger"
    )

    // --- Google Gemini API Types ---

    type GeminiRequest struct {
        Contents          []GeminiContent          `json:"contents"`
        SystemInstruction *GeminiContent           `json:"systemInstruction,omitempty"`
        GenerationConfig  *GeminiGenerationConfig  `json:"generationConfig,omitempty"`
        Tools             []GeminiTool             `json:"tools,omitempty"`
    }

    type GeminiContent struct {
        Role  string       `json:"role,omitempty"` // "user" or "model"
        Parts []GeminiPart `json:"parts"`
    }

    type GeminiPart struct {
        Text             string                  `json:"text,omitempty"`
        FunctionCall     *GeminiFunctionCall     `json:"functionCall,omitempty"`
        FunctionResponse *GeminiFunctionResponse `json:"functionResponse,omitempty"`
    }

    type GeminiFunctionCall struct {
        Name string          `json:"name"`
        Args json.RawMessage `json:"args"`
    }

    type GeminiFunctionResponse struct {
        Name     string          `json:"name"`
        Response json.RawMessage `json:"response"`
    }

    type GeminiTool struct {
        FunctionDeclarations []GeminiFunctionDeclaration `json:"functionDeclarations"`
    }

    type GeminiFunctionDeclaration struct {
        Name        string          `json:"name"`
        Description string          `json:"description,omitempty"`
        Parameters  json.RawMessage `json:"parameters,omitempty"`
    }

    type GeminiGenerationConfig struct {
        Temperature     *float64 `json:"temperature,omitempty"`
        MaxOutputTokens *int     `json:"maxOutputTokens,omitempty"`
    }

    type GeminiResponse struct {
        Candidates    []GeminiCandidate     `json:"candidates"`
        UsageMetadata *GeminiUsageMetadata  `json:"usageMetadata,omitempty"`
    }

    type GeminiCandidate struct {
        Content      GeminiContent `json:"content"`
        FinishReason string        `json:"finishReason,omitempty"` // e.g. "STOP"
    }

    type GeminiUsageMetadata struct {
        PromptTokenCount     int `json:"promptTokenCount"`
        CandidatesTokenCount int `json:"candidatesTokenCount"`
        TotalTokenCount      int `json:"totalTokenCount"`
    }

    // --- Conversion Functions ---

    func ConvertAnthropicRequestToGemini(body []byte, logs ...logger.Logger) ([]byte, error)
    func ConvertGeminiResponseToAnthropic(body []byte, model string, logs ...logger.Logger) ([]byte, error)
    func ConvertGeminiStreamToAnthropic(reader io.Reader, writer io.Writer, model string, logs ...logger.Logger) error
    ```
*   **Logic (Request Conversion)**:
    *   Anthropic の `system` フィールドが与えられている場合は、`systemInstruction` として `parts: [{"text": systemText}]` を作成します。
    *   メッセージの `role` は、`user` -> `"user"`, `assistant` -> `"model"` にマッピングします。
    *   メッセージの `content` が `[]ContentBlock` である場合は、以下のルールで `parts` にマッピングします。
        *   `type == "text"`: `parts: [{"text": Text}]`
        *   `type == "tool_use"`: `parts: [{"functionCall": {"name": Name, "args": Input}}]`
        *   `type == "tool_result"`: `parts: [{"functionResponse": {"name": ToolUseID, "response": {"content": Content}}}]`
    *   Anthropic の `tools` を Gemini の `tools` 定義にマッピングします。
        *   各ツールの `InputSchema` は `Parameters` に格納されます。
        *   Gemini API はスキーマタイプに小文字の `string`, `object` などがあると 400 エラーになるケースがあるため、`convertSchemaTypesToUppercase` という再帰ヘルパー関数を定義し、JSON内の `type` プロパティの文字列値を大文字化（例: `"string"` -> `"STRING"`, `"object"` -> `"OBJECT"`, `"array"` -> `"ARRAY"`, `"boolean"` -> `"BOOLEAN"`, `"number"` -> `"NUMBER"`, `"integer"` -> `"INTEGER"`）した上で `Parameters` に割り当てます。
    *   `max_tokens` が存在する場合は `generationConfig.maxOutputTokens` に、`temperature` が存在する場合は `generationConfig.temperature` にマッピングします。

*   **Logic (Response Conversion)**:
    *   Gemini のレスポンスを Anthropic の `AnthropicResponse` 構造体にマッピングします。
    *   最初の candidate の content 内の parts を走査します。
        *   `Text` がある場合: `ContentBlock{Type: "text", Text: part.Text}` を追加。
        *   `FunctionCall` がある場合: `ContentBlock{Type: "tool_use", ID: "call_gemini_" + name, Name: part.FunctionCall.Name, Input: part.FunctionCall.Args}` を追加。
    *   `stop_reason` の判定:
        *   `functionCall` があった場合: `"tool_use"`。
        *   それ以外: `"end_turn"`。
    *   `usageMetadata` から `usage.input_tokens` および `output_tokens` をマッピング。

*   **Logic (Stream Conversion)**:
    *   `bufio.NewReader` で一行ずつ SSE ストリームを読み取ります。
    *   各行が `data: ` で始まっている場合は、その内容を `GeminiResponse` としてパースします。
    *   パースした内容から、テキストデルタまたは `functionCall` のパーツを抽出し、以下の Anthropic SSE イベントを構築して即座にクライアントに flush 送信します。
        *   `response.candidates[0].content.parts[0].text` がある場合: `content_block_delta` (type="text_delta")。
        *   `functionCall` が開始された場合: `content_block_start` (type="tool_use") および `content_block_delta` (type="input_json_delta")。
    *   `hadFunctionCall` フラグを管理し、ストリーム全体でツール呼び出しが検出されたか追跡します。
    *   ストリーム終端（または `[DONE]` 信号）で、`hadFunctionCall` が true であれば `stop_reason` を `"tool_use"`、それ以外は `"end_turn"` として `message_delta` と `message_stop` イベントを出力します。

---

#### [NEW] [convert_a2g_test.go](file://shared/libs/go/llmgateway/convert_a2g_test.go)
*   **Description**: `convert_a2g.go` 内の各変換ロジックの単体テストを実装します（TDD 用のテスト先行）。
*   **Technical Design**:
    ```go
    package llmgateway

    import (
        "bytes"
        "encoding/json"
        "strings"
        "testing"
    )

    func TestConvertSchemaTypesToUppercase(t *testing.T)
    func TestConvertAnthropicRequestToGemini_BasicText(t *testing.T)
    func TestConvertAnthropicRequestToGemini_WithTools(t *testing.T)
    func TestConvertGeminiResponseToAnthropic_Text(t *testing.T)
    func TestConvertGeminiResponseToAnthropic_ToolCall(t *testing.T)
    func TestConvertGeminiStreamToAnthropic_TextStream(t *testing.T)
    func TestConvertGeminiStreamToAnthropic_ToolCallStream(t *testing.T)
    ```

---

#### [MODIFY] [proxy_anthropic.go](file://shared/libs/go/llmgateway/proxy_anthropic.go)
*   **Description**: R3 - `handleAnthropicMessages` で `case "google"` のルーティングおよびフォワード処理、`x-goog-api-key` ヘッダーによる認証を統合します。
*   **Technical Design & Logic**:
    *   `case "google"` ブロックの追加:
        ```go
        case "google":
            forwardPath = fmt.Sprintf("/v1beta/models/%s:generateContent", routed.Model)
            if routed.Stream != nil && *routed.Stream {
                forwardPath = fmt.Sprintf("/v1beta/models/%s:streamGenerateContent?alt=sse", routed.Model)
            }
            p.logger.Debug("converting anthropic request", "direction", "anthropic->gemini", "target_path", forwardPath)
            converted, convErr := ConvertAnthropicRequestToGemini(body, p.logger)
            if convErr != nil {
                // WriteErrorResponse...
                return
            }
            forwardBody = converted
            p.logger.Info("cross-provider conversion", "direction", "anthropic->gemini", "model", routed.Model)
        ```
    *   Google API へのリクエストフォワード時のヘッダー適用:
        `fwd.forwardWithRetry` に渡すヘッダー、あるいは `forwardWithRetry` 内での Google プロバイダ宛てのリクエスト作成において、`x-goog-api-key` ヘッダーを設定して送信します。
        既存の [provider_forwarder.go](file://shared/libs/go/llmgateway/provider_forwarder.go) 内の `forwardWithRetry` で、`provider == "google"` の場合にヘッダーを上書きするようにします。

---

#### [MODIFY] [provider_forwarder.go](file://shared/libs/go/llmgateway/provider_forwarder.go)
*   **Description**: R3 - Google Gemini API への中継フォワード時に、ヘッダーに `x-goog-api-key: [API_KEY]` を設定する処理を追加します。
*   **Technical Design**:
    `forwardWithRetry` (あるいは `prepareRequest` 等の内部メソッド) 内で、`provider == "google"` の際に送信ヘッダーをカスタマイズします。
    ```go
    // provider == "google" の場合の処理
    if provider == "google" {
        req.Header.Set("x-goog-api-key", apiKey)
        req.Header.Set("Content-Type", "application/json")
        req.Header.Del("Authorization") // Bearer 認証は使用しない
    }
    ```

---

## Step-by-Step Implementation Guide

1.  **単体テスト作成 (TDD - テスト先行)**:
    *   [convert_a2g_test.go](file://shared/libs/go/llmgateway/convert_a2g_test.go) を新規作成し、各テストケース（`TestConvertAnthropicRequestToGemini_BasicText` 等）を実装します。
    *   ビルドを実行して、未実装のためコンパイルエラーまたはテストFAILとなることを確認します。

2.  **`convert_a2g.go` の実装 (R1, R2)**:
    *   [convert_a2g.go](file://shared/libs/go/llmgateway/convert_a2g.go) を新規作成し、リクエスト/レスポンス変換ロジックを記述します。
    *   スキーマタイプの大文字化を行う再帰関数 `convertSchemaTypesToUppercase` を記述します。
    *   単体テストを実行し、テストが PASS することを確認します。

3.  **`provider_forwarder.go` の修正 (R3)**:
    *   [provider_forwarder.go](file://shared/libs/go/llmgateway/provider_forwarder.go) に `provider == "google"` 時の `x-goog-api-key` 設定ロジックを組み込みます。

4.  **`proxy_anthropic.go` の修正 (R3)**:
    *   [proxy_anthropic.go](file://shared/libs/go/llmgateway/proxy_anthropic.go) の `case "google"` 分岐およびレスポンス/ストリームの変換呼び出し（`ConvertGeminiStreamToAnthropic` 等）を組み込みます。

5.  **E2Eテストコードの実装**:
    *   `tests/agentservice_e2e_test.go` または新規のテストファイルで、Gemini 用の API Key とルーティングをモック、もしくは実環境で検証可能な E2E テストを追加します（詳細は Verification Plan 参照）。

6.  **ビルドと全体のテスト実行**:
    *   `./scripts/process/build.sh` で全体をビルドし、単体テストを確認します。
    *   `./scripts/process/integration_test.sh` で統合テストを実行し、正しく動作することを確認します。

---

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```

2.  **Integration Tests**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories llm
    ```
    *   **Log Verification**: 
        - `converting anthropic request (anthropic->gemini)` の変換ログの検知
        - 変換後のリクエストに含まれるツール定義のパラメータ型が大文字（`STRING`、`OBJECT` 等）になっていることの確認

3.  **E2E Tests**:
    Gemini へのリクエスト中継が正常に動作することを検証する E2E テストコードを `tests/` 配下に追加します。

    #### [NEW] [gemini_e2e_test.go](file://tests/gemini_e2e_test.go)
    *   **テストケース**: `TestE2E_GeminiTranslation_Stream`
    *   **検証ポイント**: 
        - Anthropic エンドポイントに送られた `gemini-3.5-flash` 向けのリクエストが Google 形式に変換され、モックされた Google の SSE ストリームレスポンス（`generateContent` または `streamGenerateContent`）が Anthropic SSE 形式に逆変換されて正しくクライアントに返されること。

---

## Documentation

なし。

---

## テスト項目設計のセルフレビュー (S11)

### 11.3 観点チェックリスト

| # | 観点 | 対応状況 |
|---|------|----------|
| 1 | 正常系の動作確認 | `TestConvertAnthropicRequestToGemini_BasicText`, `TestConvertGeminiResponseToAnthropic_Text` |
| 2 | 異常系・境界値 | スキーマ変換処理での予期しない型（ネスト、空フィールド）の検証 |
| 3 | 外部連携の実動作 | 統合テストでのモック/実Gemini APIとの連携検証 |
| 4 | データの一貫性 | リクエスト変換後の JSON スキーマ大文字化が正しく再現されているかの検証（`TestConvertSchemaTypesToUppercase`） |
| 5 | 状態遷移の検証 | ストリーム変換での `hadFunctionCall` フラグ判定による `stop_reason` の適切なマッピング |
| 6 | 設定・構成の反映 | Gateway 宛ての `Authorization` または `x-api-key` から API Key が `x-goog-api-key` に正しく詰め替えられること |

### 11.4 セルフレビュー

1. **網羅性**: 要件 R1〜R3 のすべてに対応する単体テストおよび E2E テストが設計されています。
2. **証拠の十分性**: テスト内でのアサーションにより、JSON の各フィールド（特に入力パラメータの大文字化や stop_reason のマッピング）が正確に期待値と一致することをアサートします。
3. **迂回排除**: テストでは `bufio.Reader` と `httptest.NewRecorder` を使用して SSE ストリームの実出力を直にアサートするため、変換処理をバイパスすることは不可能です。
4. **依存関係**: ボトムアップ順序（`convert_a2g.go` -> `provider_forwarder.go` -> `proxy_anthropic.go` -> E2E）に従って段階的にテスト・検証を実施します。
