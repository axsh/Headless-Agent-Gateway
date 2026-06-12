# 043-Remaining-Work-Bifrost-Unification

> **Source Specification**: prompts/phases/000-foundation/branches/feat-llm-backend/ideas/031-Remaining-Work-Summary.md

## Goal Description

Part 1-3 で未完了となった残作業 (R3, R4, R7) を統合的に実装する:

1. **R3**: `handleAnthropicMessages` の Bifrost SDK primary path 化
2. **R7**: レガシーコード削除 (convert_*.go 等 約3,669行)
3. **R4**: Ollama プロバイダー統合テスト

依存関係: R3 -> R7。R4 は独立 (ただし R3 完了後に Bifrost SDK 経由で検証するのが理想)。

## User Review Required

> [!WARNING]
> **R7 レガシーコード削除**: R3 完了後、convert_a2o.go, convert_a2g.go, convert_a2r.go, stream_converter.go, provider_forwarder.go とそれらのテストファイル (合計約3,669行) を削除します。proxy_anthropic.go の legacy fallback パスも除去されます。

> [!IMPORTANT]
> **R4 Ollama テスト**: Ollama サーバーがローカルで起動していることが前提です。CI 環境での実行可否を確認する必要があります。Ollama が利用できない環境では `t.Fatalf` で失敗します (testing-rules に従いスキップ禁止)。テスト環境の整備状況によっては R4 の実装を見送る可能性があります。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R3-1: Anthropic Messages -> BifrostResponsesRequest 変換 | Proposed Changes > llmgateway/convert_anthropic_bifrost.go |
| R3-2: BifrostResponsesResponse -> Anthropic Messages 逆変換 | Proposed Changes > llmgateway/convert_anthropic_bifrost.go |
| R3-3: ストリーミング (SSE) 対応 | Proposed Changes > llmgateway/proxy_anthropic.go |
| R3-4: provider_forwarder.go を経由しない新パス | Proposed Changes > llmgateway/proxy_anthropic.go |
| R3-5: 段階的移行 (primary: Bifrost, fallback: legacy) | Proposed Changes > llmgateway/proxy_anthropic.go |
| R4: Ollama 統合テスト | Proposed Changes > tests/llm_ollama_test.go |
| R7: convert_a2o.go 削除 | Step-by-Step > Phase C |
| R7: convert_a2g.go 削除 | Step-by-Step > Phase C |
| R7: convert_a2r.go 削除 | Step-by-Step > Phase C |
| R7: stream_converter.go 削除 | Step-by-Step > Phase C |
| R7: provider_forwarder.go 削除 | Step-by-Step > Phase C |
| R7: proxy_anthropic.go legacy fallback 削除 | Step-by-Step > Phase C |

## Proposed Changes

### R3: handleAnthropicMessages の Bifrost SDK 一本化

#### [NEW] [convert_anthropic_bifrost_test.go](file://shared/libs/go/llmgateway/convert_anthropic_bifrost_test.go)
*   **Description**: Anthropic <-> Bifrost 変換レイヤーの単体テスト (TDD)
*   **Technical Design**:
    ```go
    package llmgateway

    // --- Forward Conversion Tests: Anthropic -> Bifrost ---

    func TestConvertAnthropicToBifrost_BasicMessage(t *testing.T) {
        // system (string) + user message -> BifrostResponsesRequest
        // Verify: Instructions == system text, Input[0].Role == "user"
    }

    func TestConvertAnthropicToBifrost_SystemContentBlocks(t *testing.T) {
        // system as []ContentBlock -> Instructions concatenation
    }

    func TestConvertAnthropicToBifrost_ToolUse(t *testing.T) {
        // messages with tool_use + tool_result content blocks
        // Verify: function_call + function_call_output in Input
    }

    func TestConvertAnthropicToBifrost_Tools(t *testing.T) {
        // tools array -> Params.Tools (ResponsesToolTypeFunction)
        // Verify: Name, Description, InputSchema -> Parameters mapping
    }

    func TestConvertAnthropicToBifrost_Parameters(t *testing.T) {
        // max_tokens -> MaxOutputTokens, temperature, top_p
        // Verify: Params fields are set correctly
    }

    func TestConvertAnthropicToBifrost_StreamFlag(t *testing.T) {
        // stream: true -> no change to BifrostResponsesRequest
        // (streaming is handled at handler level, not in conversion)
    }

    // --- Reverse Conversion Tests: Bifrost -> Anthropic ---

    func TestConvertBifrostToAnthropic_BasicResponse(t *testing.T) {
        // BifrostResponsesResponse with text output
        // Verify: AnthropicResponse with text content block
    }

    func TestConvertBifrostToAnthropic_ToolUseOutput(t *testing.T) {
        // Output with function_call -> tool_use content block
    }

    func TestConvertBifrostToAnthropic_Usage(t *testing.T) {
        // Usage mapping: input_tokens, output_tokens
    }

    func TestConvertBifrostToAnthropic_StopReason(t *testing.T) {
        // Status "completed" -> stop_reason "end_turn"
        // StopReason "tool_use" -> stop_reason "tool_use"
    }

    // --- Stream Conversion Tests: Bifrost Stream -> Anthropic SSE ---

    func TestConvertBifrostStreamToAnthropicSSE_Text(t *testing.T) {
        // BifrostResponsesStreamResponse text delta
        // -> Anthropic SSE: message_start, content_block_start,
        //    content_block_delta, content_block_stop, message_delta, message_stop
    }

    func TestConvertBifrostStreamToAnthropicSSE_ToolUse(t *testing.T) {
        // function_call stream events -> tool_use SSE events
    }
    ```

#### [NEW] [convert_anthropic_bifrost.go](file://shared/libs/go/llmgateway/convert_anthropic_bifrost.go)
*   **Description**: Anthropic Messages <-> BifrostResponsesRequest 双方向変換レイヤー
*   **Technical Design**:

    **Forward Conversion (Anthropic -> Bifrost)**:
    ```go
    package llmgateway

    import (
        bifrostSchemas "github.com/maximhq/bifrost/core/schemas"
    )

    // ConvertAnthropicToBifrost converts an Anthropic Messages API request
    // to a BifrostResponsesRequest.
    func ConvertAnthropicToBifrost(
        req *AnthropicFullRequest,
        provider bifrostSchemas.ModelProvider,
    ) (*bifrostSchemas.BifrostResponsesRequest, error) {
        bifrostReq := &bifrostSchemas.BifrostResponsesRequest{
            Provider: provider,
            Model:    req.Model,
            Params:   &bifrostSchemas.ResponsesParameters{},
        }

        // 1. System -> Instructions
        if req.System != nil {
            instructions, err := extractSystemInstructions(req.System)
            if err != nil {
                return nil, err
            }
            bifrostReq.Params.Instructions = &instructions
        }

        // 2. Messages -> Input
        for _, msg := range req.Messages {
            converted, err := convertAnthropicMessage(msg)
            if err != nil {
                return nil, err
            }
            bifrostReq.Input = append(bifrostReq.Input, converted...)
        }

        // 3. Tools -> Params.Tools
        for _, tool := range req.Tools {
            bifrostReq.Params.Tools = append(bifrostReq.Params.Tools,
                convertAnthropicTool(tool))
        }

        // 4. Parameters
        if req.MaxTokens > 0 {
            bifrostReq.Params.MaxOutputTokens = &req.MaxTokens
        }
        if req.Temperature != nil {
            bifrostReq.Params.Temperature = req.Temperature
        }

        return bifrostReq, nil
    }
    ```

    **extractSystemInstructions**: `json.RawMessage` を string または `[]ContentBlock` としてパースし、テキストを連結して Instructions 文字列を生成。

    **convertAnthropicMessage**: Anthropic message の content を Bifrost `ResponsesMessage` に変換。
    - `role: "user"` + string content -> `ResponsesMessage{Role: "user", Content: {ContentStr: &text}}`
    - `role: "user"` + `[]ContentBlock` with `type: "text"` -> same
    - `role: "user"` + `[]ContentBlock` with `type: "tool_result"` -> `ResponsesMessage{Type: "function_call_output", ...}`
    - `role: "assistant"` + `[]ContentBlock` with `type: "text"` -> `ResponsesMessage{Role: "assistant", Content: ...}`
    - `role: "assistant"` + `[]ContentBlock` with `type: "tool_use"` -> `ResponsesMessage{Type: "function_call", ...}`

    **convertAnthropicTool**: `AnthropicTool` -> `bifrostSchemas.ResponsesTool{Type: ResponsesToolTypeFunction, ...}`

    **Reverse Conversion (Bifrost -> Anthropic)**:
    ```go
    // ConvertBifrostToAnthropic converts a BifrostResponsesResponse
    // to an Anthropic Messages API response.
    func ConvertBifrostToAnthropic(
        resp *bifrostSchemas.BifrostResponsesResponse,
    ) (*AnthropicResponse, error) {
        anthResp := &AnthropicResponse{
            ID:    generateAnthropicID(),
            Type:  "message",
            Role:  "assistant",
            Model: resp.Model,
        }

        // 1. Output -> Content blocks
        for _, msg := range resp.Output {
            blocks, err := convertBifrostOutputToContentBlocks(msg)
            if err != nil {
                return nil, err
            }
            anthResp.Content = append(anthResp.Content, blocks...)
        }

        // 2. Status/StopReason -> stop_reason
        anthResp.StopReason = mapBifrostStopReason(resp)

        // 3. Usage
        if resp.Usage != nil {
            anthResp.Usage = AnthropicUsage{
                InputTokens:  safeInt(resp.Usage.InputTokens),
                OutputTokens: safeInt(resp.Usage.OutputTokens),
            }
        }

        return anthResp, nil
    }
    ```

    **mapBifrostStopReason**: `resp.StopReason` が `"tool_use"` なら `"tool_use"`、それ以外 (nil, "stop", "") なら `"end_turn"`。

    **Stream Conversion (Bifrost Stream -> Anthropic SSE)**:
    ```go
    // WriteBifrostStreamAsAnthropicSSE reads Bifrost stream chunks
    // and writes them as Anthropic-compatible SSE events.
    func WriteBifrostStreamAsAnthropicSSE(
        w http.ResponseWriter,
        ch <-chan *bifrostSchemas.BifrostResponsesStreamChunk,
        model string,
        logger logger.Logger,
    ) error {
        flusher, ok := w.(http.Flusher)
        if !ok {
            return fmt.Errorf("response writer does not support flushing")
        }

        // Emit message_start
        emitAnthropicSSE(w, flusher, "message_start", ...)

        blockIndex := 0
        for chunk := range ch {
            if chunk.BifrostError != nil {
                emitAnthropicSSE(w, flusher, "error", ...)
                continue
            }
            if chunk.BifrostResponsesStreamResponse != nil {
                // Convert stream event type to Anthropic SSE events
                convertAndEmitStreamEvent(w, flusher, chunk, &blockIndex, model)
            }
        }

        // Emit message_stop
        emitAnthropicSSE(w, flusher, "message_stop", ...)
        return nil
    }
    ```

    **Anthropic SSE イベントフォーマット**:
    - `message_start`: `{"type":"message_start","message":{"id":"...","type":"message","role":"assistant","model":"...","content":[],"stop_reason":null,"usage":{"input_tokens":0,"output_tokens":0}}}`
    - `content_block_start`: `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`
    - `content_block_delta`: `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"..."}}`
    - `content_block_stop`: `{"type":"content_block_stop","index":0}`
    - `message_delta`: `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":N}}`
    - `message_stop`: `{"type":"message_stop"}`

#### [MODIFY] [proxy_anthropic.go](file://shared/libs/go/llmgateway/proxy_anthropic.go)
*   **Description**: `handleAnthropicMessages` に Bifrost SDK primary パスを追加。proxy_openai.go の `handleOpenAIResponses` と同じパターンに統一。
*   **Technical Design**:

    既存の switch-case (L108-L175) + レスポンス変換 (L212-L330) + `providerForwarder` を、Bifrost SDK 委譲に置き換える:

    ```go
    func (p *ProxyServer) handleAnthropicMessages(w http.ResponseWriter, r *http.Request) {
        // ... (既存: body parse, routing, vault resolve は維持)

        // Bifrost SDK primary path (proxy_openai.go と同パターン)
        if p.driver != nil && p.driver.bifrostSDK != nil {
            p.handleAnthropicMessagesViaBifrost(w, r, body, &fullReq, routed, apiKey)
            return
        }

        // Legacy fallback (既存コード維持、R7 で削除)
        p.handleAnthropicMessagesLegacy(w, r, body, &req, routed, apiKey)
    }

    func (p *ProxyServer) handleAnthropicMessagesViaBifrost(
        w http.ResponseWriter, r *http.Request,
        body []byte, fullReq *AnthropicFullRequest,
        routed *RoutedModel, apiKey string,
    ) {
        providerKey := toBifrostProvider(routed.Provider)

        // 1. Convert Anthropic -> Bifrost
        bifrostReq, err := ConvertAnthropicToBifrost(fullReq, providerKey)
        if err != nil { ... }

        // 2. Override model with routing result
        bifrostReq.Model = routed.Model

        // 3. Tool sanitization (proxy_openai.go と同じロジック)
        sanitizeToolsForProvider(bifrostReq, providerKey, p.logger)

        // 4. Create Bifrost context
        bifrostCtx := bifrostSchemas.NewBifrostContext(r.Context(), bifrostSchemas.NoDeadline)

        // 5. Dispatch stream / non-stream
        if fullReq.Stream != nil && *fullReq.Stream {
            p.handleAnthropicMessagesBifrostStream(w, bifrostCtx, bifrostReq, routed.Model)
        } else {
            p.handleAnthropicMessagesBifrostNonStream(w, bifrostCtx, bifrostReq, routed)
        }
    }

    func (p *ProxyServer) handleAnthropicMessagesBifrostNonStream(
        w http.ResponseWriter,
        ctx *bifrostSchemas.BifrostContext,
        req *bifrostSchemas.BifrostResponsesRequest,
        routed *RoutedModel,
    ) {
        resp, bifrostErr := p.driver.bifrostSDK.ResponsesRequest(ctx, req)
        if bifrostErr != nil { ... }

        // Convert Bifrost response -> Anthropic response
        anthResp, err := ConvertBifrostToAnthropic(resp)
        if err != nil { ... }

        // Apply ToolCallFallback if enabled
        if routed.ToolCallFallback { ... }

        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusOK)
        json.NewEncoder(w).Encode(anthResp)
    }

    func (p *ProxyServer) handleAnthropicMessagesBifrostStream(
        w http.ResponseWriter,
        ctx *bifrostSchemas.BifrostContext,
        req *bifrostSchemas.BifrostResponsesRequest,
        model string,
    ) {
        ch, bifrostErr := p.driver.bifrostSDK.ResponsesStreamRequest(ctx, req)
        if bifrostErr != nil { ... }

        w.Header().Set("Content-Type", "text/event-stream")
        w.Header().Set("Cache-Control", "no-cache")
        w.Header().Set("Connection", "keep-alive")
        w.WriteHeader(http.StatusOK)

        WriteBifrostStreamAsAnthropicSSE(w, ch, model, p.logger)
    }
    ```

*   **Logic**:
    - `handleAnthropicMessages` の既存ロジック (body parse, routing, vault resolve, ToolCallFallback フラグ解析) はそのまま維持
    - Bifrost SDK 利用可能なら新パスに分岐、なければ legacy fallback (R7 で削除予定)
    - `sanitizeToolsForProvider` は proxy_openai.go L144-L186 のロジックを共通関数に抽出
    - 全体の `AnthropicFullRequest` を body からパースするよう変更 (現在は最小の `anthropicRequest` のみ)

#### [MODIFY] [proxy_openai.go](file://shared/libs/go/llmgateway/proxy_openai.go)
*   **Description**: ツールサニタイズロジックを共通関数に抽出
*   **Technical Design**:
    - L144-L186 のツールサニタイズを `sanitizeToolsForProvider(bifrostReq, providerKey, logger)` 関数に抽出
    - `handleOpenAIResponses` 内の呼び出しを共通関数に置き換え

---

### R4: Ollama 統合テスト

#### [NEW] [llm_ollama_test.go](file://tests/llm_ollama_test.go)
*   **Description**: Ollama プロバイダーの統合テスト
*   **Technical Design**:
    ```go
    //go:build integration

    package llm_test

    import (
        "net/http"
        "testing"

        "github.com/axsh/arctic-tern/llmgateway"
    )

    // TestOllama_ProviderRegistry verifies Ollama is registered
    // in the Provider Registry with correct attributes.
    func TestOllama_ProviderRegistry(t *testing.T) {
        p, ok := llmgateway.GetProvider("ollama")
        if !ok {
            t.Fatalf("ollama provider not registered")
        }
        if got := p.BaseURL(); got != "http://localhost:11434" {
            t.Errorf("BaseURL = %q, want %q", got, "http://localhost:11434")
        }
        if got := p.Name(); got != "ollama" {
            t.Errorf("Name = %q, want %q", got, "ollama")
        }
    }

    // TestOllama_HealthCheck verifies Ollama server is reachable.
    func TestOllama_HealthCheck(t *testing.T) {
        resp, err := http.Get("http://localhost:11434/")
        if err != nil {
            t.Fatalf("Ollama server not reachable: %v", err)
        }
        defer resp.Body.Close()
        if resp.StatusCode != http.StatusOK {
            t.Fatalf("Ollama health: expected 200, got %d", resp.StatusCode)
        }
    }
    ```
*   **Logic**: Provider Registry のメタデータ検証 + Ollama サーバーの疎通確認。Ollama が起動していない場合は `t.Fatalf` で明示的に失敗。

---

### R7: レガシーコード削除

#### [DELETE] 変換コード (R3 安定動作確認後)
*   `shared/libs/go/llmgateway/convert_a2o.go` + `convert_a2o_test.go`
*   `shared/libs/go/llmgateway/convert_a2g.go` + `convert_a2g_test.go`
*   `shared/libs/go/llmgateway/convert_a2r.go` + `convert_a2r_test.go` + `convert_a2r_stream_test.go`
*   `shared/libs/go/llmgateway/stream_converter.go` + `stream_converter_test.go` (存在する場合)

#### [DELETE] レガシーフォワーダー
*   `shared/libs/go/llmgateway/provider_forwarder.go` + `provider_forwarder_test.go` (存在する場合)

#### [MODIFY] [proxy_anthropic.go](file://shared/libs/go/llmgateway/proxy_anthropic.go)
*   **Description**: `handleAnthropicMessagesLegacy` と legacy fallback 分岐を削除
*   **Logic**: Bifrost SDK パスのみにし、`if p.driver.bifrostSDK == nil` チェックを `bifrostSDK == nil` ならエラー返却に変更

---

## Step-by-Step Implementation Guide

### Phase A: R3 Bifrost SDK 変換レイヤー

1. **変換テスト作成 (TDD)**:
   - `shared/libs/go/llmgateway/convert_anthropic_bifrost_test.go` を作成
   - Forward 変換テスト (BasicMessage, SystemContentBlocks, ToolUse, Tools, Parameters, StreamFlag)
   - Reverse 変換テスト (BasicResponse, ToolUseOutput, Usage, StopReason)
   - Stream 変換テスト (Text, ToolUse)
   - `./scripts/process/build.sh` でテストが**失敗**することを確認
   - コミット: `test: add anthropic-bifrost conversion tests (TDD red)`

2. **変換レイヤー実装**:
   - `shared/libs/go/llmgateway/convert_anthropic_bifrost.go` を作成
   - `ConvertAnthropicToBifrost()`: Forward 変換
   - `ConvertBifrostToAnthropic()`: Reverse 変換
   - `WriteBifrostStreamAsAnthropicSSE()`: Stream 変換
   - `./scripts/process/build.sh` でテストが**成功**することを確認
   - コミット: `feat: add anthropic-bifrost conversion layer`

3. **ツールサニタイズ共通化**:
   - `proxy_openai.go` L144-L186 のロジックを `sanitizeToolsForProvider()` に抽出
   - `handleOpenAIResponses` 内を共通関数呼び出しに置き換え
   - `./scripts/process/build.sh` でビルド成功を確認
   - コミット: `refactor: extract tool sanitization to common function`

4. **proxy_anthropic.go に Bifrost primary パス追加**:
   - `handleAnthropicMessages` を改修: `AnthropicFullRequest` の full parse + Bifrost 分岐
   - `handleAnthropicMessagesViaBifrost()` (新規)
   - `handleAnthropicMessagesBifrostNonStream()` (新規)
   - `handleAnthropicMessagesBifrostStream()` (新規)
   - `handleAnthropicMessagesLegacy()` (既存コードを移動)
   - `./scripts/process/build.sh` でビルド成功を確認
   - コミット: `feat: add bifrost primary path for /v1/messages`

5. **統合テスト実行 (R3 安定性確認)**:
   ```bash
   ./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories llm
   ```
   - 全 LLM テストが成功することを確認
   - E2E テスト:
   ```bash
   ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestE2E_CodingAgentStreaming"
   ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestE2E_CodingAgentDefaultModel"
   ```

### Phase B: R4 Ollama テスト

6. **Ollama 統合テスト作成**:
   - `tests/llm_ollama_test.go` を作成
   - `./scripts/process/build.sh` でビルド成功を確認
   - コミット: `test: add ollama provider integration tests`

### Phase C: R7 レガシーコード削除

7. **legacy fallback 削除**:
   - `proxy_anthropic.go` から `handleAnthropicMessagesLegacy()` と legacy 分岐を削除
   - Bifrost SDK パスのみにする
   - `./scripts/process/build.sh` でビルド成功を確認
   - コミット: `refactor: remove legacy fallback from anthropic handler`

8. **convert_*.go + stream_converter.go 削除**:
   - `git rm` で以下を削除:
     - `convert_a2o.go`, `convert_a2o_test.go`
     - `convert_a2g.go`, `convert_a2g_test.go`
     - `convert_a2r.go`, `convert_a2r_test.go`, `convert_a2r_stream_test.go`
     - `stream_converter.go`, `stream_converter_test.go` (存在する場合)
   - `./scripts/process/build.sh` でビルド成功を確認 (参照エラーなし)
   - コミット: `feat: remove legacy conversion code (~3000 lines)`

9. **provider_forwarder.go 削除**:
   - `git rm` で削除: `provider_forwarder.go`, `provider_forwarder_test.go` (存在する場合)
   - `./scripts/process/build.sh` でビルド成功を確認
   - コミット: `feat: remove legacy provider forwarder`

### Phase D: 最終検証 + プッシュ

10. **全テスト実行**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh
    ```

11. **E2E テスト実行**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestE2E_"
    ```

12. **プッシュ**: `git push`

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**:
   ```bash
   ./scripts/process/build.sh
   ```
   - R3: `convert_anthropic_bifrost_test.go` が全て成功すること
   - R7: 削除後もビルドが通ること (参照エラーなし)

2. **Integration Tests (LLM)**:
   ```bash
   ./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories llm
   ```
   - R3: Bifrost SDK primary path で `/v1/messages` が正常動作すること
   - R7: レガシーコード削除後もリグレッションなし

3. **E2E Tests**:
   ```bash
   ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestE2E_CodingAgentStreaming"
   ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestE2E_CodingAgentDefaultModel"
   ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestE2E_StandaloneHealth"
   ```
   - R3: Claude Code CLI 経由の `/v1/messages` フローがストリーミング含め正常動作すること
   - R7: Bifrost SDK パスのみで E2E が全て通ること

   **E2E テストの方針**: R3/R7 は既存 E2E テスト (`TestE2E_CodingAgentStreaming`, `TestE2E_CodingAgentDefaultModel` 等) が全て通ることで「Bifrost SDK パスのみで全機能が動作している」ことを検証する。新規 E2E テストの追加は不要 (純粋な内部リファクタリング: 外部 API の動作に変更なし)。

4. **Ollama Tests**:
   ```bash
   ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestOllama"
   ```

### テスト項目セルフレビュー (testing-rules 11.4)

1. **網羅性**: R3 は Forward/Reverse/Stream 変換の各パターン (text, tool_use, tool_result, system) をカバー。R7 はレガシー削除後の全テスト成功でカバー。R4 は Provider Registry + 疎通確認。
2. **証拠の十分性**: 変換テストは入出力の値レベルで検証。E2E は実際の CLI 経由でファイル生成まで検証。
3. **迂回排除**: R7 削除後にレガシーコードが一切参照されないことをビルド成功で保証。E2E テスト中のログで Bifrost SDK パスが使用されたことを確認可能。
4. **依存関係**: convert_anthropic_bifrost (末端) -> proxy_anthropic Bifrost path (中間) -> E2E テスト (全体) の順で検証。

### 総合判定プロセス (testing-rules 12)

全テスト完了後、testing-rules 12.2 のチェック項目7点 (スキップ有無、部分エラー、迂回処理、コンフィグ適用、テスト間依存、カバレッジ、外部システム状態) を確認し、総合判定を記述する。

## Documentation

#### [MODIFY] [README.md](file://README.md)
*   **更新内容**: Bifrost SDK 一本化に関する説明を追加 (手動変換コードの廃止、Bifrost SDK 経由のモデルルーティング)
