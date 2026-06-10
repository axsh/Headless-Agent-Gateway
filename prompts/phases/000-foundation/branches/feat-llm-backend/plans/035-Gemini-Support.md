# 035-Gemini-Support

> **Source Specification**: [025-Gemini-Support.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/025-Gemini-Support.md)

## Goal Description

LLM Gateway Proxy (LLMGP) に Google プロバイダ向けの翻訳・変換層（Translation Layer）を追加し、`cawa-client` (Claude Code CLI) から Google Gemini モデル (`gemini-3.5-flash` 等) を使用した際に、対話および Function Calling (ツール実行) がストリーミング・非ストリーミングの両モードで正しく中継・実行されるようにします。
さらに、Gemini API 特有の制約として、JSON スキーマ内の非サポート属性（`$schema`、`additionalProperties` など）を検出すると `Cannot find field` エラー（400 Bad Request）を返す問題に対処するため、リクエスト変換処理内でスキーマのクレンジング処理を実装します。

## User Review Required

E2Eテストでは、ローカルにモックの Gemini API サーバーを起動し、実際に `./bin/cawa-client` コマンドを `exec.Command` で起動させてファイル（`test.txt`）がローカルに作成されることを確認する結合テストを行います。
この検証アプローチ（E2Eで本物のキーに依存せずローカルでモック結合検証を行う手法）についてレビューをお願いします。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: リクエストの変換 (Anthropic -> Google Gemini) | Proposed Changes > convert_a2g.go (ConvertAnthropicRequestToGemini) |
| R1.1: JSONスキーマのクレンジング | Proposed Changes > convert_a2g.go (cleanseGeminiSchema) |
| R2: 非ストリームレスポンスの変換 (Gemini -> Anthropic) | Proposed Changes > convert_a2g.go (ConvertGeminiResponseToAnthropic) |
| R2: ストリームレスポンス (SSE) の変換 (Gemini -> Anthropic) | Proposed Changes > convert_a2g.go (ConvertGeminiStreamToAnthropic) |
| R3: Gateway フォワード処理 of 追加 (`case "google"`) | Proposed Changes > proxy_anthropic.go (handleAnthropicMessages) |
| R3: Gemini 認証ヘッダーの付加 (`x-goog-api-key`) | Proposed Changes > provider_forwarder.go (forwardToProvider) |

---

## Proposed Changes

### llmgateway パッケージ (Google Gemini 変換層)

---

#### [MODIFY] [convert_a2g.go](file://shared/libs/go/llmgateway/convert_a2g.go)
*   **Description**: Anthropic <=> Google Gemini API 間のJSONデータ定義、リクエスト/レスポンス変換処理、SSE ストリーム変換処理、および非サポート属性を除去するスキーマクレンジング処理を実装します。
*   **Technical Design**:
    ```go
    // cleanseGeminiSchema recursively removes unsupported keys such as "$schema",
    // "additionalProperties", "const", "exclusiveMinimum", and "propertyNames"
    // from the schema map.
    func cleanseGeminiSchema(val interface{}) interface{}
    ```
*   **Logic (Schema Cleansing)**:
    - ツールパラメータのスキーマオブジェクトを再帰的に走査し、キーが `"$schema"`, `"additionalProperties"`, `"const"`, `"exclusiveMinimum"`, `"propertyNames"` であるペアを削除（マップから除外）します。
    - `convertSchemaTypesToUppercase` 関数と組み合わせて、またはその前後でこのクレンジングロジックを実行し、大文字化されたきれいなスキーマ定義のみを Gemini に送信します。

---

#### [MODIFY] [convert_a2g_test.go](file://shared/libs/go/llmgateway/convert_a2g_test.go)
*   **Description**: スキーマクレンジングロジックの単体テストを追加します。
*   **Technical Design**:
    ```go
    func TestConvertAnthropicRequestToGemini_SchemaCleansing(t *testing.T)
    ```
*   **Test Case details**:
    - `$schema`, `additionalProperties` 属性を含むツール定義の JSON を用意し、変換後にそれらの属性がすべて削除され、かつ本来必要な `properties` などのキーと大文字化された `type` が正しく残っていることをアサートします。

---

#### [MODIFY] [provider_forwarder.go](file://shared/libs/go/llmgateway/provider_forwarder.go)
*   **Description**: R3 - Google Gemini API への中継フォワード時に、ヘッダーに `x-goog-api-key: [API_KEY]` を設定し、さらに既存のクエリパラメータを保持しながら `key` パラメータをマージする処理を追加します。
*   **Technical Design**:
    すでに実装済みの `forwardToProvider` における `case "google"` のマージ処理が意図通り動作することを確認します。

---

#### [MODIFY] [proxy_anthropic.go](file://shared/libs/go/llmgateway/proxy_anthropic.go)
*   **Description**: R3 - `handleAnthropicMessages` で `case "google"` のルーティングおよびフォワード処理を統合します。
*   **Technical Design**:
    すでに実装済みのリクエスト・レスポンス（ストリーム/非ストリーム両対応）の中継ルートが意図通り動作することを確認します。

---

## Step-by-Step Implementation Guide

1.  **単体テスト作成 (TDD - テスト先行)**:
    *   [ ] [convert_a2g_test.go](file://shared/libs/go/llmgateway/convert_a2g_test.go) に `TestConvertAnthropicRequestToGemini_SchemaCleansing` テストを追加する。
    *   [ ] ビルドを実行して、スキーマクレンジングが未実装のためテストがFAILすることを確認する。

2.  **`convert_a2g.go` でのクレンジング処理の実装 (R1.1)**:
    *   [ ] [convert_a2g.go](file://shared/libs/go/llmgateway/convert_a2g.go) に再帰的に非サポートスキーマ属性を除去する `cleanseGeminiSchema` ヘルパー関数を実装する。
    *   [ ] `ConvertAnthropicRequestToGemini` 処理の中で、ツールスキーマに対して `cleanseGeminiSchema` を適用する。
    *   [ ] 単体テストがすべて PASS することを確認する。

3.  **モック結合 E2E テストの作成**:
    *   [ ] [gemini_e2e_test.go](file://tests/gemini_e2e_test.go) に `TestGeminiE2E_CawaClient_FileCreation` を追加する。
    *   [ ] `exec.Command` を用いて、モック HAG サーバー経由で `./bin/cawa-client` を実際に実行し、ツール実行によってファイルが作成されるかを検証する。

4.  **全体ビルドとテスト検証**:
    *   [ ] `./scripts/process/build.sh` で全ビルドが通ることを確認する。
    *   [ ] `./scripts/process/integration_test.sh --specify "TestGeminiE2E"` ですべてのE2Eテストが PASS することを確認する。

---

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```

2.  **Integration Tests (E2E)**:
    ```bash
    ./scripts/process/integration_test.sh --specify "TestGeminiE2E"
    ```

3.  **E2E Tests (新規追加)**:
    #### [MODIFY] [gemini_e2e_test.go](file://tests/gemini_e2e_test.go)
    *   **テストケース**: `TestGeminiE2E_CawaClient_FileCreation`
    *   **検証ポイント**: 
        - モック Gemini サーバーからツール実行 (`Write`) のレスポンスを返し、`cawa-client` コマンドを実行した結果として、実際にワークスペース上の指定されたディレクトリ（`./tmp/`）配下にファイルが生成され、コマンドが正常終了（exit code 0）すること。

## Documentation

なし。
