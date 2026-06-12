# 041-Factory-Registry-Bifrost-Part2

> **Source Specification**: prompts/phases/000-foundation/branches/feat-llm-backend/ideas/030-Factory-Registry-And-Bifrost-Unification.md

## Goal Description

本 Part2 では、Part1 (R8, R1, R2) で構築した基盤の上に、以下の3要件を実装する:

1. **R3: Bifrost SDK 一本化** (/v1/messages ハンドラの移行 + /v1/chat/completions 削除)
2. **R4: Ollama プロバイダーの機能テスト** (Part1 で Provider 定義済み、本 Part で動作検証)
3. **R5: client ライブラリ** (cawa-client のロジックを shared/libs/go/client/ に抽出)

依存関係: R2 -> R3, R4 / R5 は独立

## User Review Required

> [!IMPORTANT]
> **R3 は段階的アプローチ**: Bifrost SDK パスを primary にし、legacy を fallback として残す。安定動作が確認できてから Part3 でレガシーコードを削除する。

> [!WARNING]
> **/v1/chat/completions 削除**: 仕様の決定事項4に基づき、本 Part で削除する。現在利用中のクライアントがないことは確認済みだが、念のためレビューをお願いします。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R3: Anthropic Messages -> BifrostResponsesRequest 変換レイヤー | Proposed Changes > llmgateway/convert_anthropic_to_bifrost.go |
| R3: BifrostResponsesResponse -> Anthropic Messages 逆変換 | Proposed Changes > llmgateway/convert_bifrost_to_anthropic.go |
| R3: handleAnthropicMessages の Bifrost SDK パス追加 | Proposed Changes > llmgateway/proxy_anthropic.go |
| R3: ストリーミング対応 (SSE 変換) | Proposed Changes > llmgateway/convert_anthropic_to_bifrost.go |
| R3: /v1/chat/completions 削除 | Proposed Changes > llmgateway/proxy_openai.go, proxy.go |
| R4: Ollama 動作テスト | Proposed Changes > tests/llm_ollama_test.go |
| R5: client パッケージ作成 | Proposed Changes > client/*.go |
| R5: Session オブジェクト中心 API | Proposed Changes > client/session.go |
| R5: Stream 二層 API (Output/OnXxx/Events) | Proposed Changes > client/stream.go |
| R5: cawa-client リファクタリング | Proposed Changes > examples/cawa-client/main.go |
| 決定事項3: Anthropic -> BifrostResponsesRequest 変換 | Proposed Changes > llmgateway/convert_anthropic_to_bifrost.go |
| 決定事項4: /v1/chat/completions 削除 | Proposed Changes > llmgateway/proxy_openai.go |

## Proposed Changes

### R3: Bifrost SDK 一本化

#### [NEW] [convert_anthropic_to_bifrost_test.go](file://shared/libs/go/llmgateway/convert_anthropic_to_bifrost_test.go)
*   **Description**: Anthropic Messages -> BifrostResponsesRequest 変換のテスト (TDD)
*   **Technical Design**:
    *   テーブル駆動テスト: system, messages (user/assistant), tools, parameters のマッピングを検証
    *   tool_use / tool_result content blocks の変換を検証
    *   extended thinking content blocks の変換を検証
*   **テストケース**:
    *   `TestConvertAnthropicToBifrost_BasicMessage`: system + user message
    *   `TestConvertAnthropicToBifrost_ToolUse`: tool_use / tool_result blocks
    *   `TestConvertAnthropicToBifrost_Parameters`: max_tokens, temperature, top_p, top_k
    *   `TestConvertAnthropicToBifrost_Streaming`: stream flag の処理

#### [NEW] [convert_anthropic_to_bifrost.go](file://shared/libs/go/llmgateway/convert_anthropic_to_bifrost.go)
*   **Description**: Anthropic Messages Request -> BifrostResponsesRequest 変換レイヤー (推定 300-500行)
*   **Technical Design**:
    ```go
    package llmgateway

    // ConvertAnthropicToBifrost converts an Anthropic Messages API request
    // to a BifrostResponsesRequest for use with the Bifrost SDK.
    func ConvertAnthropicToBifrost(anthropicReq *AnthropicMessagesRequest) (*bifrostSchemas.BifrostResponsesRequest, error) {
        // 1. Model -> Model
        // 2. System (string or content blocks) -> Params.Instructions
        // 3. Messages (user/assistant array) -> Input (ResponsesMessage array)
        // 4. Tools (Anthropic tool format) -> Params.Tools (ResponsesTool array)
        // 5. ToolChoice -> Params.ToolChoice
        // 6. MaxTokens -> Params.MaxOutputTokens
        // 7. Temperature -> Params.Temperature
        // 8. TopP -> Params.TopP
        // 9. TopK -> ExtraParams
        // 10. Metadata -> Params.Metadata
    }
    ```
*   **Logic**: 仕様書 決定事項3 の入出力マッピングテーブルに従い、各フィールドを1対1で変換。content blocks の種別 (text, image, tool_use, tool_result) ごとに変換ロジックを実装。

#### [NEW] [convert_bifrost_to_anthropic_test.go](file://shared/libs/go/llmgateway/convert_bifrost_to_anthropic_test.go)
*   **Description**: BifrostResponsesResponse -> Anthropic Messages Response 逆変換のテスト (TDD)

#### [NEW] [convert_bifrost_to_anthropic.go](file://shared/libs/go/llmgateway/convert_bifrost_to_anthropic.go)
*   **Description**: BifrostResponsesResponse -> Anthropic Messages API Response 逆変換
*   **Technical Design**:
    ```go
    // ConvertBifrostToAnthropic converts a BifrostResponsesResponse
    // to an Anthropic Messages API response.
    func ConvertBifrostToAnthropic(bifrostResp *bifrostSchemas.BifrostResponsesResponse) (*AnthropicMessagesResponse, error) {
        // 1. Output -> content blocks
        // 2. Usage -> usage
        // 3. Model -> model
        // 4. Status "completed" -> stop_reason "end_turn"
    }
    ```

#### [MODIFY] [proxy_anthropic.go](file://shared/libs/go/llmgateway/proxy_anthropic.go)
*   **Description**: handleAnthropicMessages に Bifrost SDK パスを追加
*   **Logic**:
    1. 受信した Anthropic Messages JSON をパース
    2. `ConvertAnthropicToBifrost()` で `BifrostResponsesRequest` に変換
    3. `Provider.BifrostProvider()` でターゲットプロバイダーを取得
    4. Bifrost SDK の `ResponsesRequest()` (非ストリーム) または `ResponsesStreamRequest()` (ストリーム) を呼び出し
    5. `ConvertBifrostToAnthropic()` でレスポンスを逆変換
    6. ストリームの場合は SSE イベントとして逆変換しながら送出
    7. エラー時は既存のレガシーパス (provider_forwarder) にフォールバック

#### [MODIFY] [proxy_openai.go](file://shared/libs/go/llmgateway/proxy_openai.go)
*   **Description**: `/v1/chat/completions` ハンドラを削除
*   **Logic**: `handleOpenAIChatCompletions()` 関数を削除

#### [MODIFY] [proxy.go](file://shared/libs/go/llmgateway/proxy.go)
*   **Description**: `/v1/chat/completions` ルーティングを削除
*   **Logic**: ルーティングテーブルから `/v1/chat/completions` エントリを削除

---

### R5: client ライブラリ

#### [NEW] [client.go](file://shared/libs/go/client/client.go)
*   **Description**: Client 構造体とコンストラクタ
*   **Technical Design**:
    ```go
    package client

    import "net/http"

    // Client is a tern API client.
    type Client struct {
        baseURL    string
        httpClient *http.Client
    }

    // New creates a new Client for the given server URL.
    func New(baseURL string, opts ...ClientOption) *Client {
        c := &Client{
            baseURL:    baseURL,
            httpClient: http.DefaultClient,
        }
        for _, opt := range opts {
            opt(c)
        }
        return c
    }

    // ClientOption configures the Client.
    type ClientOption func(*Client)

    // WithHTTPClient sets a custom HTTP client.
    func WithHTTPClient(hc *http.Client) ClientOption {
        return func(c *Client) { c.httpClient = hc }
    }
    ```

#### [NEW] [session.go](file://shared/libs/go/client/session.go)
*   **Description**: Session 型 (セッションオブジェクト中心 API)
*   **Technical Design**:
    ```go
    package client

    import "context"

    // Session represents an active coding agent session.
    type Session struct {
        ID     string
        client *Client
    }

    // SessionRequest is the request to create a session.
    type SessionRequest struct {
        Agent      string `json:"agent"`
        Model      string `json:"model,omitempty"`
        WorkDir    string `json:"work_dir"`
        SessionDir string `json:"session_dir,omitempty"`
    }

    // CreateSession creates a new session and returns a Session object.
    func (c *Client) CreateSession(ctx context.Context, req SessionRequest) (*Session, error) { ... }

    // SendMessage sends a message to the session and returns a Stream.
    func (s *Session) SendMessage(ctx context.Context, message string) (*Stream, error) { ... }

    // Terminate terminates the session.
    func (s *Session) Terminate(ctx context.Context) error { ... }
    ```

#### [NEW] [stream.go](file://shared/libs/go/client/stream.go)
*   **Description**: Stream 型 (二層 API: Output / OnXxx+Run / Events)
*   **Technical Design**:
    ```go
    package client

    import "io"

    // EventType identifies the type of streaming event.
    type EventType string

    const (
        EventAssistantText EventType = "assistant_text"
        EventResult        EventType = "result"
        EventError         EventType = "error"
        EventToolUse       EventType = "tool_use"
    )

    // Event is a single streaming event from the server.
    type Event struct {
        Type   EventType
        Text   string
        Result *ResultData
        Error  string
    }

    // ResultData contains the result of a completed operation.
    type ResultData struct {
        Cost string
    }

    // Stream processes SSE events from a session message.
    type Stream struct { ... }

    // Output writes all text events to the given writer and blocks until completion.
    // Returns error if an error event is received.
    func (s *Stream) Output(w io.Writer) error { ... }

    // OnText sets a custom handler for assistant text events.
    func (s *Stream) OnText(fn func(text string)) *Stream { ... }

    // OnResult sets a custom handler for result events.
    func (s *Stream) OnResult(fn func(result *ResultData)) *Stream { ... }

    // Run executes the stream with the configured handlers.
    func (s *Stream) Run() error { ... }

    // Events returns a channel of raw events for full control.
    func (s *Stream) Events() <-chan Event { ... }
    ```

#### [NEW] [health.go](file://shared/libs/go/client/health.go), [agents.go](file://shared/libs/go/client/agents.go), [models.go](file://shared/libs/go/client/models.go)
*   **Description**: Health(), ListAgents(), ListModels() メソッド

#### [MODIFY] [cawa-client/main.go](file://examples/cawa-client/main.go)
*   **Description**: client ライブラリを使用する薄い CLI ラッパーにリファクタリング (442行 -> 約100行)

---

## Step-by-Step Implementation Guide

### Phase A: R5 client ライブラリ (R3 と独立して並行可能)

1.  **client パッケージ作成 (TDD)**:
    *   `client/client_test.go`, `client/session_test.go`, `client/stream_test.go` を作成
    *   `./scripts/process/build.sh` でテストが失敗することを確認

2.  **client 実装**:
    *   `client/client.go`, `client/session.go`, `client/stream.go` を作成
    *   `client/health.go`, `client/agents.go`, `client/models.go` を作成
    *   `./scripts/process/build.sh` でテストが成功することを確認
    *   コミット: `feat: add client library package`

3.  **cawa-client リファクタリング**:
    *   `examples/cawa-client/main.go` を client ライブラリを使用するように書き換え
    *   コミット: `refactor: simplify cawa-client using client library`

### Phase B: R3 Bifrost SDK 一本化

4.  **変換レイヤー作成 (TDD)**:
    *   `convert_anthropic_to_bifrost_test.go`, `convert_bifrost_to_anthropic_test.go` を作成
    *   `./scripts/process/build.sh` でテストが失敗することを確認

5.  **変換レイヤー実装**:
    *   `convert_anthropic_to_bifrost.go`, `convert_bifrost_to_anthropic.go` を作成
    *   `./scripts/process/build.sh` でテストが成功することを確認
    *   コミット: `feat: add anthropic-to-bifrost conversion layer`

6.  **proxy_anthropic.go に Bifrost パス追加**:
    *   handleAnthropicMessages に Bifrost SDK primary パスを追加
    *   エラー時は legacy fallback
    *   コミット: `feat: add bifrost primary path for /v1/messages`

7.  **/v1/chat/completions 削除**:
    *   `proxy_openai.go` から `handleOpenAIChatCompletions()` を削除
    *   `proxy.go` からルーティングを削除
    *   コミット: `feat: remove /v1/chat/completions endpoint`

### Phase C: R4 Ollama テスト

8.  **Ollama 統合テスト**:
    *   `tests/llm_ollama_test.go` を作成 (Ollama が利用可能な環境でのみ実行)
    *   Provider Registry 経由で正しく解決されることを確認

### Phase D: 統合テスト + プッシュ

9.  **統合テスト実行**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories llm
    ```

10. **全テスト実行**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh
    ```

11. **プッシュ**: `git push`

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```

2.  **Integration Tests (LLM)**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories llm
    ```
    *   R3: Bifrost SDK パスで /v1/messages が正常動作すること
    *   R3: legacy fallback が機能すること

3.  **Integration Tests (全体)**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh
    ```

4.  **E2E Tests**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestClaudeCodeE2E"
    ```
    *   R3: Claude Code CLI 経由の /v1/messages フローが正常動作すること

    **新規 E2E テストの追加**: R5 (client ライブラリ) は内部ライブラリであり、cawa-client リファクタリング後に既存 E2E テストが通ることで動作を検証する。R3 (Bifrost 一本化) は既存の E2E テストで /v1/messages フローをカバー済み。

### テスト項目セルフレビュー (testing-rules 11.4)

1.  **網羅性**: R3 は Anthropic -> Bifrost 変換の正常系・異常系 + 統合テストでカバー。R5 は単体テスト + cawa-client 動作確認でカバー。
2.  **証拠の十分性**: 変換テストは入出力の値レベルで検証。Stream テストはイベントの型・内容を検証。
3.  **迂回排除**: proxy_anthropic のログで Bifrost SDK パスが使用されたことを確認。
4.  **依存関係**: convert -> proxy_anthropic -> 統合テストの順で検証。

## 継続計画について

- **Part3** (002-Factory-Registry-Bifrost-Part3): R6 (Example 簡素化/Viper/Cobra) + R7 (レガシーコード削除)
