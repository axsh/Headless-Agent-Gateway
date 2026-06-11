# 039-Bifrost-TypedConversion-Migration

> **Source Specification**: [029-Bifrost-TypedConversion-Migration.md](file:///prompts/phases/000-foundation/branches/feat-llm-backend/ideas/029-Bifrost-TypedConversion-Migration.md)

## Goal Description

`handleOpenAIResponses` の `UseRawRequestBody` パススルー方式を廃止し、Bifrost SDK の型付き変換パス（`OpenAIResponsesRequest.ToBifrostResponsesRequest()`）に移行する。これにより、Bifrost SDK 内部の `requestConverter()` が各プロバイダ向けの変換を実行し、Gemini の `instructions` 変換エラーおよび Anthropic の `tool_choice` 形式エラーを解決する。加えて、Bifrost SDK を v1.5.15 から v1.5.18 にアップグレードする。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: UseRawRequestBody の廃止 | Proposed Changes > proxy_openai.go (Step 3) |
| R2: OpenAI Responses API リクエストのフルパース | Proposed Changes > proxy_openai.go (Step 3) |
| R3: Bifrost 正規変換パスの利用 | Proposed Changes > proxy_openai.go (Step 3) |
| R4: ルーティング結果の適用 | Proposed Changes > proxy_openai.go (Step 3) |
| R5: 既存テストのリグレッションなし | Verification Plan > Integration Tests |
| R6: Bifrost SDK v1.5.18 アップグレード | Proposed Changes > go.mod (Step 1) |

## Proposed Changes

### llmgateway パッケージ

#### [MODIFY] [go.mod](file:///shared/libs/go/go.mod)
*   **Description**: Bifrost SDK を v1.5.15 から v1.5.18 にアップグレードする
*   **Technical Design**:
    *   `require` ブロック内の `github.com/maximhq/bifrost/core` のバージョンを変更:
    ```
    - github.com/maximhq/bifrost/core v1.5.15
    + github.com/maximhq/bifrost/core v1.5.18
    ```
    *   `go mod tidy` を実行して `go.sum` を更新

---

#### [MODIFY] [proxy_openai.go](file:///shared/libs/go/llmgateway/proxy_openai.go)
*   **Description**: `handleOpenAIResponses` のリクエスト構築を raw body パススルーから型付き変換パスに移行する
*   **Technical Design**:

    **1. import 追加** (L3-14):

    ```go
    import (
        "bytes"
        "encoding/json"
        "fmt"
        "io"
        "net/http"
        "strings"

        bifrostOpenAI "github.com/maximhq/bifrost/core/providers/openai"
        bifrostSchemas "github.com/maximhq/bifrost/core/schemas"

        "github.com/axsh/hag/vault"
    )
    ```

    追加するのは `bifrostOpenAI "github.com/maximhq/bifrost/core/providers/openai"` の 1行のみ。他の import は既存のまま。

    **2. handleOpenAIResponses 関数のリクエストパースと変換ロジック変更** (L159-278):

    変更前 (L173-182, L241-266):
    ```go
    // 最小パース
    var req openaiRequest
    if err := json.Unmarshal(body, &req); err != nil { ... }

    // ...

    // raw body でモデル名書き換え
    forwardBody := body
    if routed.Model != req.Model {
        forwardBody = rewriteModelField(body, req.Model, routed.Model)
    }

    // raw body で BifrostResponsesRequest 構築
    providerKey := toBifrostProvider(routed.Provider)
    bifrostReq := &bifrostSchemas.BifrostResponsesRequest{
        Provider:       providerKey,
        Model:          routed.Model,
        Input:          []bifrostSchemas.ResponsesMessage{},
        RawRequestBody: forwardBody,
    }

    bifrostCtx := bifrostSchemas.NewBifrostContext(r.Context(), bifrostSchemas.NoDeadline)
    bifrostCtx.SetValue(bifrostSchemas.BifrostContextKeyUseRawRequestBody, true)
    ```

    変更後:
    ```go
    // OpenAI Responses API リクエストをフルパース
    var oaiReq bifrostOpenAI.OpenAIResponsesRequest
    if err := json.Unmarshal(body, &oaiReq); err != nil {
        WriteErrorResponse(w, &GatewayError{
            Type:    "invalid_request_error",
            Message: "invalid JSON in request body",
            Code:    "invalid_json",
            Status:  http.StatusBadRequest,
        })
        return
    }

    // openaiRequest はルーティング用に model を取得するためだけに使う
    req := openaiRequest{Model: oaiReq.Model}

    // ... (L186-239 ルーティング・vault解決ロジックは変更なし) ...

    // Bifrost 正規変換パスで BifrostResponsesRequest を構築
    bifrostCtx := bifrostSchemas.NewBifrostContext(r.Context(), bifrostSchemas.NoDeadline)
    bifrostReq := oaiReq.ToBifrostResponsesRequest(bifrostCtx)

    // ルーティング結果でプロバイダとモデルを上書き
    providerKey := toBifrostProvider(routed.Provider)
    bifrostReq.Provider = providerKey
    bifrostReq.Model = routed.Model

    // UseRawRequestBody は設定しない
    // → Bifrost SDK の requestConverter() でプロバイダ固有の変換が実行される
    ```

*   **Logic (変更点の詳細)**:
    1. **パース変更**: `openaiRequest{Model}` の最小パースを `bifrostOpenAI.OpenAIResponsesRequest` のフルパースに変更する。この型は `schemas.ResponsesParameters`（`Instructions`, `ToolChoice`, `Tools` 等のフィールドを含む）を埋め込んでいるため、OpenAI Responses API の全フィールドがデシリアライズされる。
    2. **openaiRequest の互換維持**: ルーティング用に `req.Model` を参照するコード（L184, L199, L203, L210）との互換性を保つため、`req := openaiRequest{Model: oaiReq.Model}` で `openaiRequest` を構築する。
    3. **forwardBody / rewriteModelField の除去**: `forwardBody` 変数と `rewriteModelField` 呼び出し（L242-245）を除去する。モデル名の書き換えは不要（`bifrostReq.Model = routed.Model` で直接上書きするため）。
    4. **body トレースログの維持**: `bodyStr := string(body)` によるトレースログ（L247-251）は既存のまま維持する。`body`（元のリクエストバイト列）は引き続き利用可能。
    5. **BifrostResponsesRequest 構築方式の変更**: `RawRequestBody` + `UseRawRequestBody=true` の代わりに、`oaiReq.ToBifrostResponsesRequest(bifrostCtx)` を使用する。この関数は内部で:
       - `resp.Input` を `OpenAIResponsesRequestInputArray` または文字列から `[]ResponsesMessage` に変換
       - `resp.ResponsesParameters`（`Instructions`, `ToolChoice`, `Tools`, `Temperature` 等）を `bifrostReq.Params` に設定
       - `resp.Model` から `provider`, `model` を `ParseModelString` でパース（HAG ではルーティング後に上書きするので、この初期パース結果は使わない）
    6. **ルーティング結果の上書き**: `bifrostReq.Provider` と `bifrostReq.Model` を HAG の `ModelRouter` のルーティング結果で上書きする。
    7. **isStreamRequest の維持**: `isStreamRequest(body)` による判定は変更しない。`oaiReq.Stream` を使うこともできるが、下流関数 (`handleOpenAIResponsesStream`, `handleOpenAIResponsesNonStream`) のシグネチャには影響しないため、既存方式を維持する。

## Step-by-Step Implementation Guide

### Step 1: Bifrost SDK アップグレード

1. [go.mod](file:///shared/libs/go/go.mod) の `require` ブロックで `github.com/maximhq/bifrost/core` のバージョンを `v1.5.15` から `v1.5.18` に変更する
2. `go mod tidy` を実行して `go.sum` を更新する
3. コミット: `chore: upgrade bifrost SDK to v1.5.18`

### Step 2: ビルド確認（アップグレード後の既存コード互換性）

1. `./scripts/process/build.sh` を実行し、Bifrost SDK v1.5.18 で既存コードがビルドできることを確認する
2. ビルドエラーがある場合はこのステップで修正する（API 互換性の破壊がある場合）

### Step 3: proxy_openai.go の変更

1. [proxy_openai.go](file:///shared/libs/go/llmgateway/proxy_openai.go) の import に `bifrostOpenAI "github.com/maximhq/bifrost/core/providers/openai"` を追加する
2. `handleOpenAIResponses` 関数内のリクエストパースを `openaiRequest` から `bifrostOpenAI.OpenAIResponsesRequest` に変更する:
   - L173-182 の `var req openaiRequest` + `json.Unmarshal(body, &req)` を `var oaiReq bifrostOpenAI.OpenAIResponsesRequest` + `json.Unmarshal(body, &oaiReq)` に変更
   - 直後に `req := openaiRequest{Model: oaiReq.Model}` を追加（ルーティングコードとの互換性維持）
3. L241-266 を以下に置換:
   - `forwardBody` 変数と `rewriteModelField` 呼び出しを除去
   - `BifrostResponsesRequest` の構築を `oaiReq.ToBifrostResponsesRequest(bifrostCtx)` に変更
   - `bifrostReq.Provider` と `bifrostReq.Model` をルーティング結果で上書き
   - `bifrostCtx.SetValue(bifrostSchemas.BifrostContextKeyUseRawRequestBody, true)` を除去
4. コミット: `feat: migrate handleOpenAIResponses to Bifrost typed conversion path`

### Step 4: ビルド確認

1. `./scripts/process/build.sh` を実行し、変更後のコードがビルドできることを確認する

### Step 5: E2E テスト実行（検証計画の実行）

1. `./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify TestCodexE2E` を実行する
2. 全 6 テストが PASS することを確認する:
   - `TestCodexE2E_FileCreation` (OpenAI gpt-4o) -- 既存 PASS 維持
   - `TestCodexE2E_GPT5Codex_FileCreation` (OpenAI gpt-5.3-codex) -- 既存 PASS 維持
   - `TestCodexE2E_GeminiModel_FileCreation` (Gemini) -- FAIL → PASS
   - `TestCodexE2E_AnthropicModel_FileCreation` (Anthropic) -- FAIL → PASS
   - `TestCodexE2E_ErrorPropagation` -- 既存 PASS 維持
   - `TestCodexE2E_HealthWithCodexAgent` -- 既存 PASS 維持
3. 失敗がある場合は修正して再実行

### Step 6: 全体リグレッション確認

1. `./scripts/process/build.sh && ./scripts/process/integration_test.sh` を実行する
2. 全テストが PASS することを確認する

### Step 7: コミットとプッシュ

1. 全テスト PASS 後、`git push` を実行する

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**:
   ```bash
   ./scripts/process/build.sh
   ```
   *   Bifrost SDK v1.5.18 での既存コードの互換性確認
   *   proxy_openai.go の変更後のビルド成功確認

2. **Integration Tests (Codex E2E)**:
   ```bash
   ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify TestCodexE2E
   ```
   *   **Log Verification**: Gemini テストで `"Unknown name instructions"` エラーが出ないこと、Anthropic テストで `"Input should be an object"` エラーが出ないこと
   *   全 6 テストが PASS すること

3. **Integration Tests (全体リグレッション)**:
   ```bash
   ./scripts/process/build.sh && ./scripts/process/integration_test.sh
   ```
   *   他のテスト（Claude Code E2E、Gemini E2E 等）にリグレッションがないこと

4. **E2E Tests (既存テストの活用)**:

   新規の E2E テストコードの追加は不要。理由:
   - 既存の `TestCodexE2E_GeminiModel_FileCreation` と `TestCodexE2E_AnthropicModel_FileCreation` が、今回修正する機能（クロスプロバイダ変換）の検証を既にカバーしている
   - これらのテストは現在 FAIL であり、本修正により PASS に転じることが検証の成功条件である
   - 新たに検証すべき外部から観測可能な動作変更はない（内部の変換方式の変更のみ）

### テスト項目のセルフレビュー

#### 観点チェックリスト

| # | 観点 | 確認内容 | 結果 |
|---|------|----------|------|
| 1 | 正常系の動作確認 | OpenAI/Gemini/Anthropic の 3 プロバイダでファイル作成が成功するか | TestCodexE2E_FileCreation, GeminiModel, AnthropicModel で検証 |
| 2 | 異常系・境界値 | 無効なリクエスト/エラー伝播が正しく動作するか | TestCodexE2E_ErrorPropagation で検証 |
| 3 | 外部連携の実動作 | 実際の LLM API との連携が機能するか | 全 E2E テストが実 API を使用 |
| 4 | データの一貫性 | 変換後のリクエストがプロバイダ API で受け入れられるか | Gemini/Anthropic テストの PASS で確認 |
| 5 | 状態遷移の検証 | ヘルスチェックが正常か | TestCodexE2E_HealthWithCodexAgent で検証 |
| 6 | 設定・構成の反映 | Bifrost SDK v1.5.18 が正しくロードされるか | ビルド成功で確認 |
| 7 | 副作用の確認 | 他のハンドラ (handleAnthropicMessages, handleOpenAIChatCompletions) に影響がないか | 全体リグレッションテストで確認 |

#### セルフレビュー結果

1. **網羅性**: 全 6 Codex E2E テスト + 全体リグレッションテストにより、正常系・異常系・クロスプロバイダ・ヘルスチェックを網羅。十分。
2. **証拠の十分性**: 各テストはファイル作成の副作用を検証（ファイルの存在と内容を確認）しており、「エラーが出ない」だけでなく「期待する結果が得られる」レベル。十分。
3. **迂回の排除**: Gemini/Anthropic テストは実際のプロバイダ API を呼び出すため、フォールバックパスで成功している可能性はない。十分。
4. **依存関係の整合性**: ビルド成功（SDK 互換性）→ Codex E2E（機能検証）→ 全体リグレッション（副作用確認）の順序で積み上げ式に検証。十分。

### 総合判定プロセス

全テスト完了後、以下の項目を確認して総合判定を実施する:

| # | チェック項目 | 確認方法 |
|---|------------|----------|
| 1 | スキップされたテストの有無 | テストログに `SKIP` がないことを確認 |
| 2 | 部分的なエラーの見落とし | テストログに `ERROR`, `WARN`, `panic` がないことを確認 |
| 3 | 迂回処理による偽成功 | Gemini/Anthropic テストの成功が正規変換パス経由であることをログで確認 |
| 4 | アダプタ・コンフィグの誤適用 | ログの `provider` フィールドが正しいことを確認 |
| 5 | テスト間の依存・順序問題 | `--specify` で個別テストを実行しても同じ結果であることを確認 |
| 6 | カバレッジの妥当性 | 新規変更に対して既存 E2E テストがカバーしていることを確認済み |
| 7 | 外部システムの状態 | API キーが有効であることを確認 |

## Documentation

#### [MODIFY] [028-Bifrost-Delegation-Migration.md](file:///prompts/phases/000-foundation/branches/feat-llm-backend/ideas/028-Bifrost-Delegation-Migration.md)
*   **更新内容**: 仕様書の先頭付近に、後続仕様 029 でクロスプロバイダ変換問題が解決されたことを注記として追記する
