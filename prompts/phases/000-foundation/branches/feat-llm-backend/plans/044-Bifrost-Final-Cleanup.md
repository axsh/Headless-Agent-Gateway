# 044-Bifrost-Final-Cleanup

> **Source Specification**: prompts/phases/000-foundation/branches/feat-llm-backend/ideas/032-Bifrost-Final-Cleanup.md

## Goal Description

Bifrost SDK 統合の最終クリーンアップ。proxy_openai.go の legacy fallback パスを削除し、provider_forwarder.go を完全に除去する。さらに Ollama プロバイダーの統合テストを作成して、全プロバイダーの動作検証を完了させる。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R-OAI: proxy_openai.go の `handleOpenAIResponsesLegacy` 削除 | Proposed Changes > llmgateway > proxy_openai.go |
| R-OAI: `bifrostSDK == nil` 時に 503 エラーを返す | Proposed Changes > llmgateway > proxy_openai.go |
| R-PF: provider_forwarder.go + test 削除 (626行) | Proposed Changes > llmgateway > provider_forwarder.go |
| R-OLL: `tests/llm_ollama_test.go` 作成 | Proposed Changes > tests > llm_ollama_test.go |
| R-OLL: model_profiles.yaml に Ollama プロファイル追加 | Proposed Changes > tests > testdata/model_profiles.yaml |
| R-OLL: non-streaming テスト | Proposed Changes > tests > llm_ollama_test.go |
| R-OLL: streaming テスト | Proposed Changes > tests > llm_ollama_test.go |
| R-OLL: Ollama 未起動時のハンドリング | Proposed Changes > tests > llm_ollama_test.go (接続チェック + t.Fatalf) |

## Proposed Changes

### llmgateway パッケージ

#### [MODIFY] [proxy_openai.go](file:///shared/libs/go/llmgateway/proxy_openai.go)
*   **Description**: legacy fallback 分岐を削除し、Bifrost SDK 必須化する
*   **Technical Design**:
    *   L86-L91 の `bifrostSDK == nil` 分岐を、503 エラーレスポンスに変更する (`proxy_anthropic.go` L103-L112 と同じパターン)
    *   L260-L324 の `handleOpenAIResponsesLegacy` 関数を完全に削除する
    *   コメント L26 の "Falls back to legacy passthrough when Bifrost SDK is not initialized." を更新する
*   **Logic**:
    *   変更前 (L86-L91):
        ```go
        // Fallback to legacy passthrough when Bifrost SDK is not available.
        if p.driver.bifrostSDK == nil {
            p.logger.Debug("bifrost SDK not available, using legacy forwarder")
            p.handleOpenAIResponsesLegacy(w, r, body, req, routed)
            return
        }
        ```
    *   変更後:
        ```go
        // Bifrost SDK path (required)
        if p.driver.bifrostSDK == nil {
            WriteErrorResponse(w, &GatewayError{
                Type:    "api_error",
                Message: "Bifrost SDK not initialized",
                Code:    "not_configured",
                Status:  http.StatusServiceUnavailable,
            })
            return
        }
        ```
    *   `handleOpenAIResponsesLegacy` 関数 (L260-L324) を削除
    *   L26 のコメントを `// It resolves the model via ModelRouter, and delegates to Bifrost SDK.` に更新
    *   未使用になる import がないか確認 (import ブロック内の `vault` パッケージは Bifrost SDK パスの L93-L95 で使用されるため残存)

#### [DELETE] [provider_forwarder.go](file:///shared/libs/go/llmgateway/provider_forwarder.go)
*   **Description**: legacy HTTP forwarder を完全削除
*   **Technical Design**:
    *   `git rm shared/libs/go/llmgateway/provider_forwarder.go`
    *   削除対象: `providerForwarder` struct, `newProviderForwarder`, `forwardToProvider`, `proxyResponse`, `forwardWithRetry`, `overrideProviderBaseURL`, `RetryConfig`
    *   325行削除

#### [DELETE] [provider_forwarder_test.go](file:///shared/libs/go/llmgateway/provider_forwarder_test.go)
*   **Description**: legacy forwarder のテストを削除
*   **Technical Design**:
    *   `git rm shared/libs/go/llmgateway/provider_forwarder_test.go`
    *   301行削除

---

### tests パッケージ

#### [MODIFY] [model_profiles.yaml](file:///tests/testdata/model_profiles.yaml)
*   **Description**: Ollama プロバイダーのプロファイルを追加
*   **Technical Design**:
    *   `providers` セクションに `ollama` エントリを追加
    *   Ollama は認証不要のため `value` は空文字列 `""` でよい
*   **Logic**:
    ```yaml
    ollama:
      keys:
        - name: default
          value: ""
          models:
            - name: llama3.2:1b
    ```

#### [NEW] [llm_ollama_test.go](file:///tests/llm_ollama_test.go)
*   **Description**: Ollama プロバイダーの統合テスト
*   **Technical Design**:
    *   パッケージ: `llm_test` (既存の `llm_gateway_test.go` と同一パッケージ)
    *   `testServer(t)` ヘルパーを再利用 (llm_gateway_test.go で定義済み)
    *   Ollama サーバーの起動確認: `http://localhost:11434` への接続チェック
    *   テストルール 6.2 に従い、Ollama 未起動時は `t.Fatalf` で明確にエラーにする
*   **Logic**:
    *   **Ollama 接続チェック関数**:
        ```go
        func checkOllamaAvailable(t *testing.T) {
            t.Helper()
            client := &http.Client{Timeout: 2 * time.Second}
            resp, err := client.Get("http://localhost:11434")
            if err != nil {
                t.Fatalf("Ollama server not available at localhost:11434: %v (run: ollama serve)", err)
            }
            resp.Body.Close()
        }
        ```
    *   **TestOllama_NonStream**:
        1. `checkOllamaAvailable(t)` で Ollama の起動を確認
        2. `testServer(t)` でゲートウェイサーバーを起動
        3. `/v1/messages` エンドポイントに Anthropic 形式のリクエスト (`model: "llama3.2:1b"`) を送信
        4. 200 OK レスポンスを確認
        5. レスポンスが Anthropic 形式 (`type: "message"`, `content` 配列) であることを検証
        ```go
        func TestOllama_NonStream(t *testing.T) {
            checkOllamaAvailable(t)
            baseURL, cleanup := testServer(t)
            defer cleanup()

            body := map[string]any{
                "model":      "llama3.2:1b",
                "max_tokens": 50,
                "messages": []map[string]string{
                    {"role": "user", "content": "Say exactly: hello ollama test"},
                },
            }
            bodyBytes, _ := json.Marshal(body)

            client := &http.Client{Timeout: 60 * time.Second}
            resp, err := client.Post(baseURL+"/v1/messages", "application/json", bytes.NewReader(bodyBytes))
            if err != nil {
                t.Fatalf("POST /v1/messages (Ollama) failed: %v", err)
            }
            defer resp.Body.Close()

            respBody, _ := io.ReadAll(resp.Body)
            if resp.StatusCode != http.StatusOK {
                t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(respBody))
            }

            var result map[string]any
            if err := json.Unmarshal(respBody, &result); err != nil {
                t.Fatalf("JSON decode failed: %v\nbody: %s", err, string(respBody))
            }

            // Anthropic 形式の検証
            if result["type"] != "message" {
                t.Errorf("expected type=message, got %v", result["type"])
            }
            content, ok := result["content"].([]any)
            if !ok || len(content) == 0 {
                t.Fatalf("expected non-empty content array, got: %s", string(respBody))
            }

            t.Logf("Ollama (non-stream) response: %s", string(respBody))
        }
        ```
    *   **TestOllama_Stream**:
        1. `checkOllamaAvailable(t)` で Ollama の起動を確認
        2. `testServer(t)` でゲートウェイサーバーを起動
        3. `/v1/messages` に `stream: true` で送信
        4. SSE イベント (`message_start`, `content_block_delta`, `message_stop`) の存在を確認
        ```go
        func TestOllama_Stream(t *testing.T) {
            checkOllamaAvailable(t)
            baseURL, cleanup := testServer(t)
            defer cleanup()

            body := map[string]any{
                "model":      "llama3.2:1b",
                "max_tokens": 50,
                "stream":     true,
                "messages": []map[string]string{
                    {"role": "user", "content": "Say exactly: hello ollama streaming"},
                },
            }
            bodyBytes, _ := json.Marshal(body)

            client := &http.Client{Timeout: 60 * time.Second}
            resp, err := client.Post(baseURL+"/v1/messages", "application/json", bytes.NewReader(bodyBytes))
            if err != nil {
                t.Fatalf("POST /v1/messages (Ollama stream) failed: %v", err)
            }
            defer resp.Body.Close()

            if resp.StatusCode != http.StatusOK {
                respBody, _ := io.ReadAll(resp.Body)
                t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(respBody))
            }

            respBody, _ := io.ReadAll(resp.Body)
            events := string(respBody)

            if !strings.Contains(events, "event: message_start") {
                t.Error("missing message_start event")
            }
            if !strings.Contains(events, "event: content_block_delta") {
                t.Error("missing content_block_delta event")
            }
            if !strings.Contains(events, "event: message_stop") {
                t.Error("missing message_stop event")
            }

            t.Logf("Ollama (stream) response length: %d bytes", len(events))
        }
        ```

## Step-by-Step Implementation Guide

### Phase 1: R-OAI - proxy_openai.go legacy fallback 削除

1.  **proxy_openai.go の legacy 分岐を書き換え**:
    *   L86-L91 の `bifrostSDK == nil` 分岐を 503 エラーレスポンスに変更
    *   L26 のコメントを更新 (legacy fallback への言及を削除)
2.  **handleOpenAIResponsesLegacy を削除**:
    *   L260-L324 の `handleOpenAIResponsesLegacy` 関数を削除
3.  **未使用 import の確認・修正**:
    *   `go build ./llmgateway/` でビルドエラーを確認
    *   未使用 import があれば削除
4.  **コミット**: `refactor: remove legacy fallback from openai responses handler`

### Phase 2: R-PF - provider_forwarder 削除

5.  **provider_forwarder.go 内の関数参照を確認**:
    *   `grep -rn 'newProviderForwarder\|forwardWithRetry\|proxyResponse\|overrideProviderBaseURL\|forwardToProvider' shared/libs/go/llmgateway/*.go | grep -v _test.go | grep -v provider_forwarder.go`
    *   参照が 0 件であることを確認してから削除
6.  **provider_forwarder.go と provider_forwarder_test.go を削除**:
    *   `git rm shared/libs/go/llmgateway/provider_forwarder.go shared/libs/go/llmgateway/provider_forwarder_test.go`
7.  **ビルド確認**:
    *   `./scripts/process/build.sh` で全体ビルドが通ることを確認
8.  **コミット**: `refactor: remove legacy provider forwarder`

### Phase 3: R-OLL - Ollama 統合テスト

9.  **model_profiles.yaml に Ollama エントリを追加**:
    *   `tests/testdata/model_profiles.yaml` に `ollama` プロバイダーセクションを追加
10. **llm_ollama_test.go を作成**:
    *   `checkOllamaAvailable` ヘルパーを作成
    *   `TestOllama_NonStream` を作成
    *   `TestOllama_Stream` を作成
11. **Ollama テストを実行**:
    *   Ollama サーバーが起動していることを確認 (`ollama serve`)
    *   対象モデルが pull 済みであることを確認 (`ollama pull llama3.2:1b`)
    *   テスト実行
12. **コミット**: `feat: add Ollama integration tests`

### Phase 4: 最終検証

13. **全体ビルド + テスト**:
    *   `./scripts/process/build.sh` を実行
14. **LLM 統合テスト**:
    *   `./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories llm`
15. **Ollama テスト**:
    *   `./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestOllama"`
16. **プッシュ**:
    *   全テスト成功を確認後 `git push`

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```

2.  **LLM Integration Tests** (既存テストのリグレッション確認):
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories llm
    ```

3.  **Ollama Integration Tests** (新規テスト):
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestOllama"
    ```
    *   **Log Verification**:
        *   `Ollama (non-stream) response:` ログに Anthropic 形式のレスポンスが出力されること
        *   `Ollama (stream) response length:` ログに 0 より大きいバイト数が出力されること

4.  **E2E Tests**:
    E2E テストは不要。理由: 本計画は内部リファクタリング (legacy コード削除) と Ollama 統合テスト追加であり、ユーザー向けの機能変更はない。既存の E2E テスト (`TestE2E_CodingAgentStreaming` 等) は LLM 統合テスト (`--categories llm`) の一部として実行され、リグレッション検出をカバーする。

### テスト項目設計セルフレビュー (testing-rules 11)

**ボトムアップ順序**: 単体 (build.sh) -> 統合 (LLM categories) -> 新規統合 (Ollama) の順で設計済み。

**観点チェックリスト**:
- 正常系: non-streaming/streaming の両方をカバー
- 異常系: Ollama 未起動時の明確なエラー (`t.Fatalf`)
- 境界値: 該当なし (LLM 応答のテスト)
- 回帰: 既存 LLM テストの再実行で確認

**セルフレビュー結果**:
- 網羅性: R-OAI, R-PF, R-OLL の全要件がカバーされている
- 証拠の十分性: build.sh + LLM integration + Ollama specify の3段階で確認
- 迂回排除: `t.Skip` 不使用 (testing-rules 6.2 準拠)
- 依存関係: R-OAI -> R-PF の順序が Step-by-Step Guide に反映済み

### 総合判定プロセス (testing-rules 12)

全テスト完了後に以下を確認:
1. `./scripts/process/build.sh` が全コンポーネントで PASS
2. `./scripts/process/integration_test.sh --categories llm` が全テスト PASS
3. `./scripts/process/integration_test.sh --specify "TestOllama"` が全テスト PASS
4. 上記全て PASS を確認後 `git push`

## Documentation

#### [MODIFY] [031-Remaining-Work-Summary.md](file:///prompts/phases/000-foundation/branches/feat-llm-backend/ideas/031-Remaining-Work-Summary.md)
*   **更新内容**: R3, R7 の状態を「実装済み」に更新。R4 の状態を本計画で対応中に更新。
