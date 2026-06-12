# 032-SSE-Parser-And-Gateway-Retry

> **Source Specification**: [023-SSE-Parser-And-Gateway-Retry.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/023-SSE-Parser-And-Gateway-Retry.md)

## Goal Description

SSEストリームパーサーの `bufio.Scanner` を `bufio.Reader` に置き換えて行サイズ上限を撤廃し、Gateway層にHTTPリトライ機構（429/5xx対応の指数バックオフ）を追加する。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: SSEパーサーの bufio.Scanner 廃止 | Proposed Changes > convert_a2r.go, stream_converter.go |
| R2: Gateway層のHTTPリトライ機構 | Proposed Changes > provider_forwarder.go |
| R3: リトライ設定の外部化 | Proposed Changes > config.go, config.yaml |
| R4: リトライログの可視化 (任意) | Proposed Changes > provider_forwarder.go (ログ出力のみ。WebSocket通知は先送り) |
| R5: 既存 codingagent.Retry の統合検討 (任意) | 先送り。Gateway層とcodingagent層は責務が異なるため、現時点では独立維持。必要になった時点で統合を検討。|

## Proposed Changes

### llmgateway パッケージ -- テスト

#### [MODIFY] [stream_converter_test.go](file://shared/libs/go/llmgateway/stream_converter_test.go)
*   **Description**: `bufio.Reader` 置き換え後の動作検証テストを追加
*   **Technical Design**:
    ```go
    func TestConvertOpenAIStreamToAnthropic_LargeEvent(t *testing.T)
    ```
*   **Logic**:
    1. 100KBの `data:` 行（64KB超）を含むSSE入力を生成する
       ```go
       // 100KB のテキストコンテンツを含む1行の data: を生成
       largeContent := strings.Repeat("x", 100*1024)
       sseInput := strings.Join([]string{
           fmt.Sprintf(`data: {"id":"lg","choices":[{"delta":{"role":"assistant","content":""},"finish_reason":null}]}`, ),
           ``,
           fmt.Sprintf(`data: {"id":"lg","choices":[{"delta":{"content":"%s"},"finish_reason":null}]}`, largeContent),
           ``,
           `data: {"id":"lg","choices":[{"delta":{},"finish_reason":"stop"}]}`,
           ``,
           `data: [DONE]`,
           ``,
       }, "\n")
       ```
    2. `ConvertOpenAIStreamToAnthropic` がエラーなく処理を完了することを検証
    3. 出力SSEイベントに `message_start`, `content_block_delta`, `message_stop` が含まれることを検証
    4. content_block_delta のテキスト内容に `largeContent` が含まれることを検証

#### [NEW] [convert_a2r_stream_test.go](file://shared/libs/go/llmgateway/convert_a2r_stream_test.go)
*   **Description**: Responses APIストリーム変換の大規模イベントテストを追加
*   **Technical Design**:
    ```go
    func TestConvertResponsesStreamToAnthropic_LargeEvent(t *testing.T)
    ```
*   **Logic**:
    1. 100KBの `data:` 行を含むResponses API SSEイベント（`response.output_text.delta` タイプ）を生成
       ```go
       // 100KB のデルタテキストを含む1行の data: を生成
       largeText := strings.Repeat("y", 100*1024)
       sseInput := strings.Join([]string{
           `event: response.created`,
           fmt.Sprintf(`data: {"type":"response.created","response":{"id":"resp-lg"}}`),
           ``,
           `event: response.content_part.added`,
           `data: {"type":"response.content_part.added"}`,
           ``,
           `event: response.output_text.delta`,
           fmt.Sprintf(`data: {"type":"response.output_text.delta","delta":"%s"}`, largeText),
           ``,
           `event: response.output_text.done`,
           `data: {"type":"response.output_text.done"}`,
           ``,
           `event: response.completed`,
           `data: {"type":"response.completed"}`,
           ``,
       }, "\n")
       ```
    2. `ConvertResponsesStreamToAnthropic` がエラーなく完了することを検証
    3. 出力に `message_start`, `content_block_delta`, `content_block_stop`, `message_delta`, `message_stop` が含まれることを検証

#### [NEW] [provider_forwarder_test.go](file://shared/libs/go/llmgateway/provider_forwarder_test.go)
*   **Description**: リトライ機構の単体テスト
*   **Technical Design**:
    ```go
    func TestForwardWithRetry_429(t *testing.T)
    func TestForwardWithRetry_500(t *testing.T)
    func TestForwardWithRetry_400_NoRetry(t *testing.T)
    func TestForwardWithRetry_MaxAttempts(t *testing.T)
    func TestForwardWithRetry_ContextCancel(t *testing.T)
    func TestForwardWithRetry_Success(t *testing.T)
    func TestForwardWithRetry_RetryAfterHeader(t *testing.T)
    func TestCalculateBackoff(t *testing.T)
    func TestIsRetryableStatusCode(t *testing.T)
    ```
*   **Logic**:
    *   各テストは `httptest.NewServer` でモックサーバーを作成し、特定のステータスコードを返す
    *   `TestForwardWithRetry_429`:
        1. モックサーバーが1回目に429、2回目に200を返す
        2. `forwardWithRetry` が2回リクエストを送信し、最終的に200のレスポンスを返すことを検証
        3. リクエスト回数が2であることを検証
    *   `TestForwardWithRetry_500`:
        1. モックサーバーが1回目に500、2回目に200を返す
        2. 同上
    *   `TestForwardWithRetry_400_NoRetry`:
        1. モックサーバーが400を返す
        2. `forwardWithRetry` が1回でエラーレスポンスを返すことを検証
        3. リクエスト回数が1であることを検証
    *   `TestForwardWithRetry_MaxAttempts`:
        1. モックサーバーが常に500を返す
        2. `forwardWithRetry` が `MaxRetries+1` 回リクエストを送信し、エラーを返すことを検証
    *   `TestForwardWithRetry_ContextCancel`:
        1. キャンセル済みコンテキストを渡す
        2. `context.Canceled` エラーが返ることを検証
    *   `TestForwardWithRetry_RetryAfterHeader`:
        1. モックサーバーが `Retry-After: 1` ヘッダー付きの429を返す
        2. 2回目のリクエストまでの経過時間が1秒以上であることを検証
    *   `TestCalculateBackoff`:
        テーブル駆動テストで各attemptの計算結果を検証:
        | attempt | 期待 (base) |
        | 0 | 1s |
        | 1 | 2s |
        | 2 | 4s |
        | 3 | 8s (capped to MaxDelay) |
    *   `TestIsRetryableStatusCode`:
        テーブル駆動テストで各ステータスコードの判定を検証:
        | status | retryable |
        | 200 | false |
        | 400 | false |
        | 401 | false |
        | 429 | true |
        | 500 | true |
        | 502 | true |
        | 503 | true |
        | 504 | true |

---

### llmgateway パッケージ -- 実装

#### [MODIFY] [convert_a2r.go](file://shared/libs/go/llmgateway/convert_a2r.go)
*   **Description**: `ConvertResponsesStreamToAnthropic` の `bufio.Scanner` を `bufio.Reader` に置き換え
*   **Technical Design**:
    ```go
    func ConvertResponsesStreamToAnthropic(reader io.Reader, writer io.Writer, model string) error {
        br := bufio.NewReader(reader)
        flusher, hasFlusher := writer.(http.Flusher)
        // ... (既存の変数宣言)

        for {
            line, err := br.ReadString('\n')
            if err != nil && err != io.EOF {
                return err
            }
            // EOF with remaining data
            if err == io.EOF && line == "" {
                break
            }

            line = strings.TrimRight(line, "\r\n")

            // 既存のSSEパースロジック（event:, data: の判定）
            // ...

            if err == io.EOF {
                break
            }
        }

        return nil  // bufio.Reader には scanner.Err() 相当がないため nil を返す
    }
    ```
*   **Logic**:
    1. `bufio.NewScanner(reader)` を `bufio.NewReader(reader)` に置き換え
    2. `scanner.Buffer(...)` の行を削除
    3. `for scanner.Scan()` ループを `for { line, err := br.ReadString('\n') ... }` に置き換え
    4. `scanner.Text()` を `strings.TrimRight(line, "\r\n")` に置き換え
    5. `scanner.Err()` を `nil` に置き換え（`ReadString` はエラーを即時返すため蓄積しない）
    6. `io.EOF` 時に残りデータがある場合の処理を追加（最終行に改行がないケース対応）
    7. 既存のSSEパースロジック（`event:`, `data:` の判定、各 `eventType` の `switch` 文）は変更しない

#### [MODIFY] [stream_converter.go](file://shared/libs/go/llmgateway/stream_converter.go)
*   **Description**: `ConvertOpenAIStreamToAnthropic` の `bufio.Scanner` を `bufio.Reader` に置き換え
*   **Technical Design**:
    ```go
    func ConvertOpenAIStreamToAnthropic(
        reader io.Reader,
        w http.ResponseWriter,
        model string,
    ) error {
        flusher, _ := w.(http.Flusher)
        br := bufio.NewReader(reader)

        // ... (既存の変数宣言)

        for {
            line, err := br.ReadString('\n')
            if err != nil && err != io.EOF {
                return err
            }
            if err == io.EOF && line == "" {
                break
            }

            line = strings.TrimRight(line, "\r\n")

            // 既存のSSEパースロジック
            // ...

            if err == io.EOF {
                break
            }
        }

        return nil
    }
    ```
*   **Logic**:
    1. `convert_a2r.go` と同じ置き換えパターンを適用
    2. `scanner.Buffer(...)` の行を削除
    3. `for scanner.Scan()` を `for { line, err := br.ReadString('\n') ... }` に置き換え
    4. `scanner.Text()` を `strings.TrimRight(line, "\r\n")` に置き換え
    5. `return scanner.Err()` を `return nil` に置き換え
    6. import から `bufio` は引き続き使用（`bufio.NewReader`）

#### [MODIFY] [provider_forwarder.go](file://shared/libs/go/llmgateway/provider_forwarder.go)
*   **Description**: リトライ機構を追加
*   **Technical Design**:
    ```go
    // RetryConfig configures retry behavior for upstream provider requests.
    type RetryConfig struct {
        // MaxRetries is the maximum number of retry attempts (0 = no retry).
        MaxRetries int
        // InitialDelay is the base delay for exponential backoff.
        InitialDelay time.Duration
        // MaxDelay is the maximum delay between retries.
        MaxDelay time.Duration
    }

    // DefaultRetryConfig returns the default retry configuration.
    func DefaultRetryConfig() *RetryConfig {
        return &RetryConfig{
            MaxRetries:   3,
            InitialDelay: 1 * time.Second,
            MaxDelay:     30 * time.Second,
        }
    }

    // isRetryableStatusCode returns true for HTTP status codes that warrant a retry.
    func isRetryableStatusCode(status int) bool {
        switch status {
        case http.StatusTooManyRequests,        // 429
            http.StatusInternalServerError,     // 500
            http.StatusBadGateway,              // 502
            http.StatusServiceUnavailable,      // 503
            http.StatusGatewayTimeout:          // 504
            return true
        }
        return false
    }

    // isRetryableNetworkError returns true for transient network errors.
    func isRetryableNetworkError(err error) bool {
        if err == nil {
            return false
        }
        errStr := err.Error()
        return strings.Contains(errStr, "EOF") ||
            strings.Contains(errStr, "connection reset") ||
            strings.Contains(errStr, "broken pipe") ||
            strings.Contains(errStr, "connection refused") ||
            strings.Contains(errStr, "connectex")
    }

    // calculateBackoff computes the backoff delay for a given attempt.
    // If resp has a Retry-After header (for 429), it is used instead.
    func calculateBackoff(attempt int, cfg *RetryConfig, resp *http.Response) time.Duration {
        // Check Retry-After header for 429 responses.
        if resp != nil && resp.StatusCode == http.StatusTooManyRequests {
            if ra := resp.Header.Get("Retry-After"); ra != "" {
                if seconds, err := strconv.Atoi(ra); err == nil && seconds > 0 {
                    return time.Duration(seconds) * time.Second
                }
            }
        }
        // Exponential backoff: InitialDelay * 2^attempt
        delay := cfg.InitialDelay * (1 << uint(attempt))
        if delay > cfg.MaxDelay {
            delay = cfg.MaxDelay
        }
        // Add jitter: +/- 10%
        jitter := time.Duration(rand.Int63n(int64(delay) / 5))
        return delay - time.Duration(int64(delay)/10) + jitter
    }

    // forwardWithRetry sends a request to the upstream provider with retry logic.
    // It retries on retryable status codes (429, 5xx) and network errors.
    // Streaming responses (text/event-stream) are NOT retried after headers are received.
    func (f *providerForwarder) forwardWithRetry(
        ctx context.Context,
        provider, path string,
        body []byte,
        apiKey string,
        headers http.Header,
        cfg *RetryConfig,
        logger *slog.Logger,
    ) (*http.Response, error) {
        if cfg == nil {
            cfg = DefaultRetryConfig()
        }
        var lastErr error
        for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
            // Check context before each attempt.
            if err := ctx.Err(); err != nil {
                return nil, err
            }

            resp, err := f.forwardToProvider(provider, path, body, apiKey, headers)
            if err != nil {
                if !isRetryableNetworkError(err) {
                    return nil, err
                }
                lastErr = err
                if logger != nil {
                    logger.Warn("upstream network error, retrying",
                        "attempt", attempt+1,
                        "max_retries", cfg.MaxRetries,
                        "error", err)
                }
            } else if !isRetryableStatusCode(resp.StatusCode) {
                // Non-retryable status (200, 400, 401, etc.) - return immediately.
                return resp, nil
            } else {
                // Retryable status (429, 5xx) - drain body and retry.
                if logger != nil {
                    logger.Warn("upstream retryable status",
                        "attempt", attempt+1,
                        "max_retries", cfg.MaxRetries,
                        "status", resp.StatusCode)
                }
                io.Copy(io.Discard, resp.Body)
                resp.Body.Close()
                lastErr = &GatewayError{
                    Type:    "api_error",
                    Message: fmt.Sprintf("upstream returned %d", resp.StatusCode),
                    Code:    "upstream_retryable",
                    Status:  resp.StatusCode,
                }
            }

            // Wait before next attempt (except on last attempt).
            if attempt < cfg.MaxRetries {
                delay := calculateBackoff(attempt, cfg, resp)
                if logger != nil {
                    logger.Info("retry backoff",
                        "attempt", attempt+1,
                        "delay", delay)
                }
                select {
                case <-time.After(delay):
                case <-ctx.Done():
                    return nil, ctx.Err()
                }
            }
        }
        return nil, lastErr
    }
    ```
*   **Logic**:
    1. `providerForwarder` 構造体に `retryConfig *RetryConfig` フィールドを追加
    2. `newProviderForwarder` にオプショナルな `RetryConfig` パラメータを追加
    3. `isRetryableStatusCode` で 429, 500, 502, 503, 504 を判定
    4. `isRetryableNetworkError` で EOF, connection reset 等を判定
    5. `calculateBackoff` で指数バックオフ + Retry-After対応 + ジッターを計算
    6. `forwardWithRetry` でリトライループを実装

---

### config パッケージ

#### [MODIFY] [config.go](file://shared/libs/go/config/config.go)
*   **Description**: `LLMGatewayConfig` にリトライ設定を追加
*   **Technical Design**:
    ```go
    // LLMGatewayConfig holds LLM Gateway Proxy settings.
    type LLMGatewayConfig struct {
        Port              int             `yaml:"port"`
        ModelProfilesPath string          `yaml:"model_profiles_path"`
        MetricsEnabled    bool            `yaml:"metrics_enabled"`
        Retry             RetrySettings   `yaml:"retry"`
    }

    // RetrySettings holds retry configuration for upstream provider requests.
    type RetrySettings struct {
        MaxRetries          int `yaml:"max_retries"`
        InitialDelaySeconds int `yaml:"initial_delay_seconds"`
        MaxDelaySeconds     int `yaml:"max_delay_seconds"`
    }
    ```
*   **Logic**:
    1. `RetrySettings` 構造体を新規追加
    2. `LLMGatewayConfig` に `Retry RetrySettings` フィールドを追加
    3. YAML設定がない場合（ゼロ値）は `DefaultRetryConfig()` がデフォルト値を提供する

---

### proxy 統合

#### [MODIFY] [proxy_anthropic.go](file://shared/libs/go/llmgateway/proxy_anthropic.go)
*   **Description**: `forwardToProvider` 呼び出しを `forwardWithRetry` に置き換え
*   **Technical Design**:
    - L154の `fwd.forwardToProvider(...)` を `fwd.forwardWithRetry(r.Context(), ..., retryCfg, p.logger)` に変更
    - `AnthropicProxy` に `retryConfig *RetryConfig` を保持し、コンストラクタで設定から変換
*   **Logic**:
    1. `AnthropicProxy` 構造体に `retryConfig *RetryConfig` フィールドを追加
    2. コンストラクタ（`NewAnthropicProxy` 等）でconfig値を `RetryConfig` に変換:
       ```go
       retryCfg := DefaultRetryConfig()
       if cfg.Retry.MaxRetries > 0 {
           retryCfg.MaxRetries = cfg.Retry.MaxRetries
       }
       if cfg.Retry.InitialDelaySeconds > 0 {
           retryCfg.InitialDelay = time.Duration(cfg.Retry.InitialDelaySeconds) * time.Second
       }
       if cfg.Retry.MaxDelaySeconds > 0 {
           retryCfg.MaxDelay = time.Duration(cfg.Retry.MaxDelaySeconds) * time.Second
       }
       ```
    3. `forwardToProvider` を `forwardWithRetry` に置き換え

#### [MODIFY] [proxy_openai.go](file://shared/libs/go/llmgateway/proxy_openai.go)
*   **Description**: OpenAI proxy でも同様に `forwardWithRetry` に置き換え
*   **Logic**:
    1. `proxy_anthropic.go` と同じパターンで `forwardToProvider` を `forwardWithRetry` に変更

---

### 設定ファイル

#### [MODIFY] [config.yaml](file://examples/standalone/config.yaml)
*   **Description**: リトライ設定のサンプルを追加
*   **Logic**:
    ```yaml
    llm_gateway:
      port: 14000
      model_profiles_path: "./examples/standalone/model_profiles.yaml"
      retry:
        max_retries: 3
        initial_delay_seconds: 1
        max_delay_seconds: 30
    ```

## Step-by-Step Implementation Guide

### Phase 1: SSEパーサー置き換え (R1)

1. **テスト作成 -- 大規模SSEイベント (TDD: Red)**:
   - `shared/libs/go/llmgateway/stream_converter_test.go` に `TestConvertOpenAIStreamToAnthropic_LargeEvent` を追加
   - `shared/libs/go/llmgateway/convert_a2r_stream_test.go` を新規作成し `TestConvertResponsesStreamToAnthropic_LargeEvent` を追加
   - `./scripts/process/build.sh` を実行し、テストが失敗することを確認（現在の1MBバッファで通るため、テスト入力を2MB超にする）
   - **注意**: 実際にはバッファ拡大の応急処置があるため、テスト入力サイズを2MB超にして `scanner.Buffer` の1MB上限を超えさせるか、先に `scanner.Buffer` の行を削除してからテストを実行する

2. **convert_a2r.go の Scanner -> Reader 置き換え (TDD: Green)**:
   - `ConvertResponsesStreamToAnthropic` の `bufio.NewScanner` を `bufio.NewReader` に置き換え
   - `scanner.Buffer(...)` の行を削除
   - `for scanner.Scan()` ループを `for { line, err := br.ReadString('\n') ... }` に変換
   - `return scanner.Err()` を `return nil` に変更
   - `./scripts/process/build.sh` を実行しテストが通ることを確認
   - git commit

3. **stream_converter.go の Scanner -> Reader 置き換え (TDD: Green)**:
   - `ConvertOpenAIStreamToAnthropic` に同じパターンを適用
   - `./scripts/process/build.sh` を実行しテストが通ることを確認
   - git commit

### Phase 2: リトライ機構 (R2, R3)

4. **リトライテスト作成 (TDD: Red)**:
   - `shared/libs/go/llmgateway/provider_forwarder_test.go` を新規作成
   - `TestForwardWithRetry_*`, `TestCalculateBackoff`, `TestIsRetryableStatusCode` を追加
   - `./scripts/process/build.sh` を実行しコンパイルエラーを確認

5. **config.go にリトライ設定を追加**:
   - `RetrySettings` 構造体を追加
   - `LLMGatewayConfig` に `Retry` フィールドを追加
   - git commit

6. **provider_forwarder.go にリトライ実装 (TDD: Green)**:
   - `RetryConfig`, `DefaultRetryConfig`, `isRetryableStatusCode`, `isRetryableNetworkError`, `calculateBackoff`, `forwardWithRetry` を追加
   - `./scripts/process/build.sh` を実行しテストが通ることを確認
   - git commit

7. **proxy_anthropic.go / proxy_openai.go の統合**:
   - `forwardToProvider` を `forwardWithRetry` に置き換え
   - config値の変換ロジックを追加
   - `./scripts/process/build.sh` を実行しビルドが通ることを確認
   - git commit

8. **config.yaml のサンプル更新**:
   - `examples/standalone/config.yaml` にリトライ設定を追加
   - git commit

### Phase 3: 検証

9. **統合テスト実行**:
   - `./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestE2E_CodingAgent"` を実行
   - `./scripts/process/integration_test.sh --specify "TestCrossProvider"` を実行
   - `./scripts/process/integration_test.sh --specify "TestResponsesAPI"` を実行

10. **リグレッションテスト実行**:
    - `./scripts/process/build.sh && ./scripts/process/integration_test.sh` で全テスト通過を確認

11. **git push**

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**:
   ```bash
   ./scripts/process/build.sh
   ```

2. **Integration Tests (関連テストの選択的実行)**:
   ```bash
   ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestE2E_CodingAgent"
   ./scripts/process/integration_test.sh --specify "TestCrossProvider"
   ./scripts/process/integration_test.sh --specify "TestResponsesAPI"
   ```
   *   **Log Verification**:
       - `bufio.Scanner: token too long` エラーが発生しないことを確認
       - `cross-provider conversion` ログが正常に出力されることを確認

3. **Regression Tests (全テスト)**:
   ```bash
   ./scripts/process/build.sh && ./scripts/process/integration_test.sh
   ```

4. **E2E Tests**:

   E2Eテストの新規追加は不要。理由: 既存の `TestE2E_CodingAgentStreaming` (TC-002) および `TestE2E_CodingAgentDefaultModel` (TC-005b) が、`tool_use` イベントの検証を含んでおり、SSEパーサーとリトライ機構の変更がE2E動作に影響しないことを確認できる。R2のリトライ動作は単体テストのモックサーバーで十分に検証可能であり、実LLMプロバイダーの429/5xxを意図的に発生させるE2Eテストは実用的でない。

### テスト項目設計セルフレビュー

#### 観点チェックリスト
- [x] 正常系: Scanner -> Reader 置き換え後の通常SSEパース動作
- [x] 境界値: 64KB超、100KB超、空行、最終行改行なし
- [x] 異常系: 不正JSON、接続切断
- [x] リトライ正常系: 429 -> 200, 500 -> 200
- [x] リトライ異常系: 400 (リトライなし), 最大回数超過, コンテキストキャンセル
- [x] Retry-After ヘッダー対応
- [x] 指数バックオフ計算の正確性

#### 網羅性確認
- R1の2ファイル (convert_a2r.go, stream_converter.go) に対して各1つ以上の大規模イベントテスト
- R2のリトライロジックに対して7つのテストケース
- R3の設定に対してはconfig構造体の追加のみ（YAML読み込みは既存テストでカバー）
- 既存テストの動作が変わらないことはリグレッションテストで確認

### 総合判定プロセス
1. Phase 1 完了後: `./scripts/process/build.sh` で単体テスト全PASS確認
2. Phase 2 完了後: `./scripts/process/build.sh` で単体テスト全PASS確認
3. Phase 3: 統合テスト選択的実行 -> リグレッションテスト全実行
4. 全テストPASS後に `git push`

## Documentation

#### [MODIFY] [023-SSE-Parser-And-Gateway-Retry.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/023-SSE-Parser-And-Gateway-Retry.md)
*   **更新内容**: 実装完了後に検証結果セクションを追加。R4のWebSocket通知部分はスコープ外であることを注記。
