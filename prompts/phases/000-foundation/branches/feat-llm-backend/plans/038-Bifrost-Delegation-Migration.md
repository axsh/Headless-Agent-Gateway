# 038-Bifrost-Delegation-Migration

> **Source Specification**: [028-Bifrost-Delegation-Migration.md](../ideas/028-Bifrost-Delegation-Migration.md)

## Goal Description

LLMGP の `handleOpenAIResponses` ハンドラが使用している `providerForwarder.forwardWithRetry()` を
Bifrost SDK の `ResponsesRequest()` / `ResponsesStreamRequest()` に置き換え、Codex CLI から
Gemini, Anthropic モデルを利用可能にする (Phase 1)。

Phase 2 (handleAnthropicMessages の委譲) と Phase 3 (自前変換コードの削除) は別計画とする。

## User Review Required

> [!IMPORTANT]
> **OpenAI Responses API のリクエストボディ直接パススルー戦略**:
> Codex CLI が送信するリクエストボディは OpenAI Responses API 形式である。
> `BifrostResponsesRequest` への Go 構造体への変換ではなく、
> `RawRequestBody` フィールド + `BifrostContextKeyUseRawRequestBody` を使用して
> 生 JSON をそのまま Bifrost に渡す方式を採用する。
> これにより、OpenAI Responses API のフィールドカバレッジに依存しない安定した転送が可能。

> [!IMPORTANT]
> **`BifrostContext` の生成**:
> Bifrost SDK は `*schemas.BifrostContext` を要求する。これは Go 標準の
> `context.Context` のラッパーであり、`schemas.NewBifrostContext(ctx)` で生成する。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: handleOpenAIResponses の Bifrost 委譲 | Proposed Changes > proxy_openai.go |
| R3: Bifrost インスタンスの生成と管理 | Proposed Changes > bifrost_driver.go |
| R4: SSE ストリーミングの Bifrost チャネル変換 | Proposed Changes > proxy_openai.go (writeBifrostResponsesStreamAsSSE) |
| R6: 既存機能のリグレッション防止 | Verification Plan > E2E Tests |
| R2: handleAnthropicMessages の委譲 | 本計画では対象外 (Phase 2) |
| R5: 自前変換コード削除 | 本計画では対象外 (Phase 3) |

## Proposed Changes

### llmgateway パッケージ

---

#### [MODIFY] [bifrost_driver.go](file://shared/libs/go/llmgateway/bifrost_driver.go)

*   **Description**: Bifrost SDK インスタンスの生成・保持・Shutdown 管理を追加。

*   **Technical Design**:

    ```go
    import (
        bifrost "github.com/maximhq/bifrost/core"
        bifrostSchemas "github.com/maximhq/bifrost/core/schemas"
    )

    type BifrostDriver struct {
        // 既存フィールド (変更なし)
        cfg      *config.AppConfig
        profiles *config.ModelProfilesConfig
        vault    vault.VaultStore
        logger   logger.Logger
        proxy    *ProxyServer
        router   *ModelRouter
        account  *BifrostAccount
        // 新規追加
        bifrostSDK *bifrost.Bifrost // Bifrost SDK インスタンス
    }
    ```

*   **Logic**:

    1. `NewBifrostDriver` 内で `bifrost.Init()` を呼び出して `bifrostSDK` を初期化:

        ```go
        func NewBifrostDriver(...) (*BifrostDriver, error) {
            // ... 既存コード ...

            // Bifrost SDK インスタンスの初期化
            bifrostCfg := bifrostSchemas.BifrostConfig{
                Account:         d.account,
                Logger:          nil, // Bifrost default logger を使用
                InitialPoolSize: 10,  // 小規模プール (HAG は同時リクエスト少)
            }
            bi, err := bifrost.Init(context.Background(), bifrostCfg)
            if err != nil {
                return nil, fmt.Errorf("bifrost driver: init bifrost SDK: %w", err)
            }
            d.bifrostSDK = bi

            // ... ProxyServer 作成 (既存コード) ...
            return d, nil
        }
        ```

    2. `Shutdown` 内で `bifrostSDK.Shutdown()` を呼び出す:

        ```go
        func (d *BifrostDriver) Shutdown(ctx context.Context) error {
            d.logger.Info("shutting down bifrost driver")
            if d.bifrostSDK != nil {
                d.bifrostSDK.Shutdown()
            }
            return d.proxy.Shutdown(ctx)
        }
        ```

    3. `ReloadProfiles` 内で `bifrostSDK` を再初期化する:

        ```go
        func (d *BifrostDriver) ReloadProfiles(profiles *config.ModelProfilesConfig) {
            d.profiles = profiles
            d.router = NewModelRouter(profiles, d.logger)
            d.account = NewBifrostAccount(profiles, d.vault, d.logger)

            // Bifrost SDK を再初期化 (新しいアカウントで)
            if d.bifrostSDK != nil {
                d.bifrostSDK.Shutdown()
            }
            bi, err := bifrost.Init(context.Background(), bifrostSchemas.BifrostConfig{
                Account:         d.account,
                Logger:          nil,
                InitialPoolSize: 10,
            })
            if err != nil {
                d.logger.Error("failed to reinit bifrost SDK", "error", err)
            } else {
                d.bifrostSDK = bi
            }

            if d.proxy != nil {
                d.proxy.ReloadProfiles(profiles)
            }
        }
        ```

---

#### [MODIFY] [proxy_openai.go](file://shared/libs/go/llmgateway/proxy_openai.go)

*   **Description**: `handleOpenAIResponses` を providerForwarder から Bifrost SDK に委譲。
    SSE ストリーミング変換関数を追加。

*   **Technical Design**:

    `handleOpenAIResponses` のフロー変更:

    ```
    Before: body -> openaiRequest parse -> router -> vault -> providerForwarder.forwardWithRetry() -> proxyResponse()
    After:  body -> openaiRequest parse -> router -> BifrostResponsesRequest 構築 -> bifrost.ResponsesRequest() or ResponsesStreamRequest() -> JSON/SSE 応答
    ```

*   **Logic**:

    1. **ストリーミング判定ヘルパー**:

        ```go
        // isStreamRequest checks if the request body has "stream": true.
        func isStreamRequest(body []byte) bool {
            var raw struct {
                Stream *bool `json:"stream"`
            }
            if err := json.Unmarshal(body, &raw); err != nil {
                return false
            }
            return raw.Stream != nil && *raw.Stream
        }
        ```

    2. **handleOpenAIResponses の書き換え** (152行目以降を置き換え):

        ```go
        func (p *ProxyServer) handleOpenAIResponses(w http.ResponseWriter, r *http.Request) {
            // --- 1. リクエストボディの読み取りとパース (既存ロジックと同一) ---
            body, err := io.ReadAll(r.Body)
            if err != nil {
                WriteErrorResponse(w, &GatewayError{
                    Type: "invalid_request_error", Message: "failed to read request body",
                    Code: "request_read_error", Status: http.StatusBadRequest,
                })
                return
            }
            defer r.Body.Close()

            var req openaiRequest
            if err := json.Unmarshal(body, &req); err != nil {
                WriteErrorResponse(w, &GatewayError{
                    Type: "invalid_request_error", Message: "invalid JSON in request body",
                    Code: "invalid_json", Status: http.StatusBadRequest,
                })
                return
            }

            p.logger.Debug("openai responses request received",
                "method", r.Method, "path", r.URL.Path, "model", req.Model)

            // --- 2. ルーティングとキー解決 (既存ロジックと同一) ---
            if p.driver == nil || p.driver.router == nil {
                WriteErrorResponse(w, &GatewayError{
                    Type: "api_error", Message: "LLM gateway backend not configured",
                    Code: "not_configured", Status: http.StatusServiceUnavailable,
                })
                return
            }

            sessionID := ExtractSessionID(r.Header.Get("Authorization"))
            routed, err := p.driver.router.ResolveModel(req.Model, sessionID)
            if err != nil {
                WriteErrorResponse(w, &GatewayError{
                    Type: "not_found_error", Message: "model not found: " + req.Model,
                    Code: "model_not_found", Status: http.StatusNotFound,
                })
                return
            }

            p.logger.Debug("responses request routed",
                "model", routed.Model, "provider", routed.Provider, "mode", routed.Mode)

            // --- 3. Bifrost SDK がない場合はフォールバック (互換性) ---
            if p.driver.bifrostSDK == nil {
                p.handleOpenAIResponsesLegacy(w, r, body, req, routed)
                return
            }

            // --- 4. API キー解決 ---
            apiKey := routed.KeyValue
            if vault.IsVaultRef(apiKey) && p.vault != nil {
                resolved, err := p.vault.Resolve(apiKey)
                if err != nil {
                    WriteErrorResponse(w, &GatewayError{
                        Type: "api_error", Message: "failed to resolve API key from vault",
                        Code: "vault_error", Status: http.StatusInternalServerError,
                    })
                    return
                }
                apiKey = resolved
            }

            p.logger.Info("openai responses request via bifrost",
                "model", routed.Model, "provider", routed.Provider,
                "key", MaskSecret(apiKey))

            // --- 5. モデル名のリライト ---
            forwardBody := body
            if routed.Model != req.Model {
                forwardBody = rewriteModelField(body, req.Model, routed.Model)
            }

            // --- 6. BifrostResponsesRequest 構築 ---
            providerKey := toBifrostProvider(routed.Provider)
            bifrostReq := &bifrostSchemas.BifrostResponsesRequest{
                Provider:       providerKey,
                Model:          routed.Model,
                RawRequestBody: forwardBody,
            }

            // --- 7. BifrostContext 構築 ---
            bifrostCtx := bifrostSchemas.NewBifrostContext(r.Context())
            bifrostCtx.SetValue(bifrostSchemas.BifrostContextKeyUseRawRequestBody, true)

            // API キーを extra headers で渡す
            extraHeaders := map[string][]string{}
            switch providerKey {
            case bifrostSchemas.Anthropic:
                extraHeaders["x-api-key"] = []string{apiKey}
            case bifrostSchemas.Gemini:
                // Gemini は URL パラメータでキーを渡すため、Bifrost の Key 管理に任せる
            default:
                extraHeaders["Authorization"] = []string{"Bearer " + apiKey}
            }
            if len(extraHeaders) > 0 {
                bifrostCtx.SetValue(bifrostSchemas.BifrostContextKeyExtraHeaders, extraHeaders)
            }

            bodyStr := string(body)
            if len(bodyStr) > 10240 {
                bodyStr = bodyStr[:10240] + "..."
            }
            p.logger.Trace("openai responses request body", "body", bodyStr)

            // --- 8. ストリーミング判定と実行 ---
            if isStreamRequest(body) {
                p.handleOpenAIResponsesStream(w, bifrostCtx, bifrostReq)
            } else {
                p.handleOpenAIResponsesNonStream(w, bifrostCtx, bifrostReq)
            }
        }
        ```

    3. **非ストリーミング応答ハンドラ**:

        ```go
        func (p *ProxyServer) handleOpenAIResponsesNonStream(
            w http.ResponseWriter,
            ctx *bifrostSchemas.BifrostContext,
            req *bifrostSchemas.BifrostResponsesRequest,
        ) {
            resp, bifrostErr := p.driver.bifrostSDK.ResponsesRequest(ctx, req)
            if bifrostErr != nil {
                status := http.StatusBadGateway
                if bifrostErr.StatusCode != nil {
                    status = *bifrostErr.StatusCode
                }
                msg := "upstream request failed"
                if bifrostErr.Error != nil {
                    msg = bifrostErr.Error.Message
                }
                WriteErrorResponse(w, &GatewayError{
                    Type:    "api_error",
                    Message: msg,
                    Code:    "upstream_error",
                    Status:  status,
                })
                return
            }

            w.Header().Set("Content-Type", "application/json")
            w.WriteHeader(http.StatusOK)
            json.NewEncoder(w).Encode(resp)
        }
        ```

    4. **ストリーミング応答ハンドラ**:

        ```go
        func (p *ProxyServer) handleOpenAIResponsesStream(
            w http.ResponseWriter,
            ctx *bifrostSchemas.BifrostContext,
            req *bifrostSchemas.BifrostResponsesRequest,
        ) {
            ch, bifrostErr := p.driver.bifrostSDK.ResponsesStreamRequest(ctx, req)
            if bifrostErr != nil {
                status := http.StatusBadGateway
                if bifrostErr.StatusCode != nil {
                    status = *bifrostErr.StatusCode
                }
                msg := "upstream stream request failed"
                if bifrostErr.Error != nil {
                    msg = bifrostErr.Error.Message
                }
                WriteErrorResponse(w, &GatewayError{
                    Type:    "api_error",
                    Message: msg,
                    Code:    "upstream_error",
                    Status:  status,
                })
                return
            }

            // SSE ヘッダー設定
            w.Header().Set("Content-Type", "text/event-stream")
            w.Header().Set("Cache-Control", "no-cache")
            w.Header().Set("Connection", "keep-alive")
            w.WriteHeader(http.StatusOK)

            flusher, ok := w.(http.Flusher)
            if !ok {
                p.logger.Error("response writer does not support flushing")
                return
            }

            for chunk := range ch {
                if chunk == nil {
                    continue
                }

                // BifrostError の場合
                if chunk.BifrostError != nil {
                    errJSON, _ := json.Marshal(chunk.BifrostError)
                    fmt.Fprintf(w, "event: error\ndata: %s\n\n", errJSON)
                    flusher.Flush()
                    continue
                }

                // BifrostResponsesStreamResponse の場合
                if chunk.BifrostResponsesStreamResponse != nil {
                    data, err := json.Marshal(chunk.BifrostResponsesStreamResponse)
                    if err != nil {
                        p.logger.Error("failed to marshal stream chunk", "error", err)
                        continue
                    }
                    eventType := string(chunk.BifrostResponsesStreamResponse.Type)
                    fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, data)
                    flusher.Flush()
                }
            }

            // 最終 [DONE] は Codex CLI 側の期待するフォーマットに合わせない
            // Responses API SSE は event type で終了を判定する
        }
        ```

    5. **レガシーハンドラ (フォールバック)**:

        ```go
        // handleOpenAIResponsesLegacy is the original passthrough implementation.
        // Used when Bifrost SDK is not initialized.
        func (p *ProxyServer) handleOpenAIResponsesLegacy(
            w http.ResponseWriter, r *http.Request,
            body []byte, req openaiRequest, routed *RoutedModel,
        ) {
            // Vault 解決
            apiKey := routed.KeyValue
            if vault.IsVaultRef(apiKey) && p.vault != nil {
                resolved, err := p.vault.Resolve(apiKey)
                if err != nil {
                    WriteErrorResponse(w, &GatewayError{
                        Type: "api_error", Message: "failed to resolve API key from vault",
                        Code: "vault_error", Status: http.StatusInternalServerError,
                    })
                    return
                }
                apiKey = resolved
            }

            forwardBody := body
            if routed.Model != req.Model {
                forwardBody = rewriteModelField(body, req.Model, routed.Model)
            }

            fwd := newProviderForwarder()
            retryCfg := p.buildRetryConfig()
            resp, err := fwd.forwardWithRetry(
                r.Context(), routed.Provider, "/v1/responses",
                forwardBody, apiKey, r.Header, retryCfg, p.logger,
            )
            if err != nil {
                if gwErr, ok := err.(*GatewayError); ok {
                    WriteErrorResponse(w, gwErr)
                } else {
                    WriteErrorResponse(w, &GatewayError{
                        Type: "api_error", Message: "upstream request failed: " + err.Error(),
                        Code: "upstream_error", Status: http.StatusBadGateway,
                    })
                }
                return
            }
            defer resp.Body.Close()
            proxyResponse(w, resp)
        }
        ```

    6. **プロバイダ変換ヘルパー**:

        ```go
        // toBifrostProvider converts HAG provider name to Bifrost ModelProvider.
        func toBifrostProvider(provider string) bifrostSchemas.ModelProvider {
            if mp, ok := providerNameMap[provider]; ok {
                return mp
            }
            return bifrostSchemas.ModelProvider(provider)
        }
        ```
        (Note: `providerNameMap` は `bifrost_account.go` に既に定義されている。)

---

#### [MODIFY] [bifrost_driver_test.go](file://shared/libs/go/llmgateway/bifrost_driver_test.go)

*   **Description**: BifrostDriver の Bifrost SDK 初期化と Shutdown をテスト。

*   **Technical Design**:

    ```go
    func TestBifrostDriverInit_WithBifrostSDK(t *testing.T) {
        // 有効なプロファイル設定で NewBifrostDriver を呼び出し、
        // d.bifrostSDK != nil であることを検証。
        // Shutdown が panic しないことを検証。
    }

    func TestBifrostDriverReload_ReinitsBifrostSDK(t *testing.T) {
        // ReloadProfiles を呼び出し、bifrostSDK が再初期化されることを検証。
    }
    ```

---

### tests パッケージ

---

#### [MODIFY] [codex_e2e_test.go](file://tests/codex_e2e_test.go)

*   **Description**: Codex + Gemini テストの Skip 解除、Codex + Anthropic テストの追加、
    Codex + GPT-5.x-codex テストの追加。

*   **Technical Design**:

    1. **TC-Codex-002 の Skip 解除**: `TestCodexE2E_GeminiModel_FileCreation` の
       `t.Skip(...)` 行を削除。

    2. **TC-Codex-005: Codex + Anthropic モデル**:

        ```go
        // TestCodexE2E_AnthropicModel_FileCreation verifies Codex CLI + Anthropic model
        // can create a file through Bifrost cross-provider routing.
        func TestCodexE2E_AnthropicModel_FileCreation(t *testing.T) {
            baseURL, cleanup := startCodexE2EServer(t)
            defer cleanup()
            workDir := t.TempDir()

            sessionID := createE2ESessionWithModel(
                t, baseURL, "codex", "claude-sonnet-4-20250514", workDir)
            t.Logf("Session created: %s", sessionID)

            prompt := "Create a file named test_anthropic.txt in the current directory " +
                "containing exactly the text 'Hello from Anthropic via Codex'. Do nothing else."
            resp := sendE2EMessage(t, baseURL, sessionID, prompt, 120*time.Second)
            defer resp.Body.Close()

            events, gotDone := parseE2ESSEEvents(t, resp)
            if !gotDone {
                t.Fatal("expected [DONE] sentinel in SSE stream")
            }
            for i, ev := range events {
                t.Logf("event[%d]: type=%s content_len=%d", i, ev.Type, len(ev.Content))
            }
            for _, ev := range events {
                if ev.Type == codingagent.EventError {
                    t.Fatalf("received error event: %s", ev.Content)
                }
            }

            filePath := filepath.Join(workDir, "test_anthropic.txt")
            content, err := os.ReadFile(filePath)
            if err != nil {
                entries, _ := os.ReadDir(workDir)
                var names []string
                for _, e := range entries {
                    names = append(names, e.Name())
                }
                t.Fatalf("expected test_anthropic.txt in %s, got: %v, err: %v",
                    workDir, names, err)
            }
            if !strings.Contains(string(content), "Hello from Anthropic via Codex") {
                t.Errorf("content = %q, want 'Hello from Anthropic via Codex'",
                    string(content))
            }
        }
        ```

    3. **TC-Codex-006: Codex + GPT-5.x-codex (既存動作維持)**:

        ```go
        // TestCodexE2E_GPT5Codex_FileCreation verifies Codex CLI + GPT-5.x-codex (OpenAI)
        // continues to work through Bifrost routing.
        func TestCodexE2E_GPT5Codex_FileCreation(t *testing.T) {
            baseURL, cleanup := startCodexE2EServer(t)
            defer cleanup()
            workDir := t.TempDir()

            sessionID := createE2ESessionWithModel(
                t, baseURL, "codex", "gpt-5.x-codex", workDir)
            t.Logf("Session created: %s", sessionID)

            prompt := "Create a file named test_gpt5.txt in the current directory " +
                "containing exactly the text 'Hello from GPT5 Codex'. Do nothing else."
            resp := sendE2EMessage(t, baseURL, sessionID, prompt, 120*time.Second)
            defer resp.Body.Close()

            events, gotDone := parseE2ESSEEvents(t, resp)
            if !gotDone {
                t.Fatal("expected [DONE] sentinel in SSE stream")
            }
            for i, ev := range events {
                t.Logf("event[%d]: type=%s content_len=%d", i, ev.Type, len(ev.Content))
            }
            for _, ev := range events {
                if ev.Type == codingagent.EventError {
                    t.Fatalf("received error event: %s", ev.Content)
                }
            }

            filePath := filepath.Join(workDir, "test_gpt5.txt")
            content, err := os.ReadFile(filePath)
            if err != nil {
                entries, _ := os.ReadDir(workDir)
                var names []string
                for _, e := range entries {
                    names = append(names, e.Name())
                }
                t.Fatalf("expected test_gpt5.txt in %s, got: %v, err: %v",
                    workDir, names, err)
            }
            if !strings.Contains(string(content), "Hello from GPT5 Codex") {
                t.Errorf("content = %q, want 'Hello from GPT5 Codex'",
                    string(content))
            }
        }
        ```

## Step-by-Step Implementation Guide

### Step 1: Bifrost SDK インスタンス管理の追加

1.  Edit `shared/libs/go/llmgateway/bifrost_driver.go`:
    - `bifrostSDK *bifrost.Bifrost` フィールドを `BifrostDriver` に追加
    - `NewBifrostDriver` 内で `bifrost.Init()` を呼び出して初期化
    - `Shutdown` 内で `bifrostSDK.Shutdown()` を呼び出す
    - `ReloadProfiles` 内で `bifrostSDK` を再初期化する
    - import に `bifrost "github.com/maximhq/bifrost/core"` と `bifrostSchemas "github.com/maximhq/bifrost/core/schemas"` を追加

### Step 2: ストリーミング判定ヘルパーとプロバイダ変換ヘルパーの追加

1.  Edit `shared/libs/go/llmgateway/proxy_openai.go`:
    - `isStreamRequest(body []byte) bool` を追加
    - `toBifrostProvider(provider string) bifrostSchemas.ModelProvider` を追加
    - import に `bifrostSchemas "github.com/maximhq/bifrost/core/schemas"` を追加

### Step 3: handleOpenAIResponses の Bifrost 委譲

1.  Edit `shared/libs/go/llmgateway/proxy_openai.go`:
    - 既存の `handleOpenAIResponses` を Bifrost 委譲版に置き換え
    - `handleOpenAIResponsesNonStream` を追加
    - `handleOpenAIResponsesStream` を追加
    - `handleOpenAIResponsesLegacy` を追加 (既存ロジックを退避)

### Step 4: 単体テストの追加

1.  Edit `shared/libs/go/llmgateway/bifrost_driver_test.go`:
    - `TestBifrostDriverInit_WithBifrostSDK` を追加
    - `TestBifrostDriverReload_ReinitsBifrostSDK` を追加

### Step 5: E2E テストの更新

1.  Edit `tests/codex_e2e_test.go`:
    - `TestCodexE2E_GeminiModel_FileCreation` の `t.Skip(...)` 行を削除
    - `TestCodexE2E_AnthropicModel_FileCreation` を追加
    - `TestCodexE2E_GPT5Codex_FileCreation` を追加

### Step 6: ビルドと検証

1.  Verification Plan の手順に従い、ビルドとテストを実行する。

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```

2.  **Codex E2E Tests (Phase 1 の核心)**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestCodexE2E"
    ```
    *   **Log Verification**:
        - `TestCodexE2E_FileCreation` (既存): 成功すること (リグレッションなし)
        - `TestCodexE2E_GeminiModel_FileCreation` (Skip解除): ファイル作成成功
        - `TestCodexE2E_AnthropicModel_FileCreation` (新規): ファイル作成成功
        - `TestCodexE2E_GPT5Codex_FileCreation` (新規): ファイル作成成功
        - `TestCodexE2E_ErrorPropagation` (既存): エラー伝播が正常動作
        - `TestCodexE2E_HealthWithCodexAgent` (既存): ヘルスチェック正常

3.  **E2E Tests (新規/更新)**:

    #### [MODIFY] [codex_e2e_test.go](file://tests/codex_e2e_test.go)
    *   **テストケース**: `TestCodexE2E_GeminiModel_FileCreation` -- Skip 解除、Gemini 経由でファイル作成
    *   **検証ポイント**: SSE ストリームにエラーイベントがなく、ファイルが作成され内容が正しいこと

    #### [MODIFY] [codex_e2e_test.go](file://tests/codex_e2e_test.go)
    *   **テストケース**: `TestCodexE2E_AnthropicModel_FileCreation` -- Anthropic 経由でファイル作成
    *   **検証ポイント**: SSE ストリームにエラーイベントがなく、ファイルが作成され内容が正しいこと

    #### [MODIFY] [codex_e2e_test.go](file://tests/codex_e2e_test.go)
    *   **テストケース**: `TestCodexE2E_GPT5Codex_FileCreation` -- OpenAI GPT-5.x-codex 経由でファイル作成
    *   **検証ポイント**: Bifrost 委譲後も OpenAI モデルのルーティングが正常動作すること

4.  **全体リグレッション**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories llm
    ```

### テスト項目のセルフレビュー (Section 11)

#### ボトムアップ検証順序

```
依存関係: E2E Tests -> proxy_openai.go handlers -> bifrost_driver.go (Bifrost SDK init)

Step 1: bifrost_driver.go の単体テスト -> SDK 初期化・Shutdown が動作
Step 2: proxy_openai.go の E2E テスト (Codex + OpenAI) -> 既存パス維持
Step 3: proxy_openai.go の E2E テスト (Codex + Gemini) -> cross-provider 変換動作
Step 4: proxy_openai.go の E2E テスト (Codex + Anthropic) -> cross-provider 変換動作
```

#### 観点チェックリスト

| # | 観点 | 確認内容 | カバー |
|---|------|----------|--------|
| 1 | 正常系 | OpenAI/Gemini/Anthropic モデルでファイル作成成功 | TC-001, TC-002, TC-005, TC-006 |
| 2 | 異常系 | Gateway 到達不能時のエラー伝播 | TC-003 |
| 3 | 外部連携 | Bifrost SDK 経由で実際の LLM API に接続 | TC-001, TC-002, TC-005, TC-006 |
| 4 | データ一貫性 | SSE イベントの型とフォーマットが正しい | TC-001 で検証 |
| 5 | 状態遷移 | セッション状態が completed になる | TC-001 |
| 6 | 設定反映 | model_profiles.yaml のプロバイダ設定が Bifrost に反映 | 単体テスト |
| 7 | 副作用 | Shutdown 後にリソースリークがない | 単体テスト |

#### セルフレビュー結果

- **網羅性**: 3 プロバイダ (OpenAI, Gemini, Anthropic) をカバーし、正常系・異常系を検証
- **証拠の十分性**: ファイル作成とその内容、SSE イベントの型、セッション状態を検証
- **迂回排除**: Bifrost SDK が初期化されていない場合のレガシーフォールバックも定義
- **依存関係**: Bifrost SDK init -> ハンドラ -> E2E の順序で検証

### 総合判定プロセス (Section 12)

全テスト完了後、Section 12 のチェック項目 (スキップされたテスト、部分的エラー、迂回処理、アダプタ誤適用、テスト間依存、カバレッジ、外部システム状態) を確認し、総合判定を実施する。

## Documentation

#### [MODIFY] [028-Bifrost-Delegation-Migration.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/028-Bifrost-Delegation-Migration.md)
*   **更新内容**: Phase 1 の実装完了に伴い、現在の対応表 (Inbound API vs Provider) を更新
