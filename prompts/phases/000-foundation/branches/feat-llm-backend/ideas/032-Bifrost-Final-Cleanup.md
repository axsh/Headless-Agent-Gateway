# 032-Bifrost-Final-Cleanup

> **Source**: 031-Remaining-Work-Summary の R3/R7 実装完了後に判明した残作業の仕様

## 背景 (Background)

仕様 031 (Remaining-Work-Summary) の R3 (Bifrost SDK primary path) と R7 (レガシーコード削除) は完了した。この作業で以下の成果を得た:

- `/v1/messages` ハンドラーの Bifrost SDK primary path 化
- Anthropic <-> Bifrost 双方向変換レイヤー作成
- convert_a2o.go, convert_a2g.go, convert_a2r.go, stream_converter.go 等 約3,900行のレガシーコード削除

しかし、以下の作業が残存している:

| 残作業 | 状態 | 理由 |
|--------|------|------|
| R-OAI: proxy_openai.go の legacy fallback 削除 | 未実装 | `/v1/responses` ハンドラーに `handleOpenAIResponsesLegacy` が残存。provider_forwarder.go がここで使用中 |
| R-PF: provider_forwarder.go + test 削除 | 未実装 (R-OAI に依存) | R-OAI 完了後に削除可能 |
| R-OLL: Ollama 統合テスト | 未実装 | 環境準備が必要だった。今回環境が整った |

## 要件 (Requirements)

### R-OAI: proxy_openai.go の legacy fallback 削除

**目的**: `/v1/responses` ハンドラーの `handleOpenAIResponsesLegacy` (L260-L324) を削除し、Bifrost SDK パスのみにする。

**現状の問題**:
- [proxy_openai.go L86-L91](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/shared/libs/go/llmgateway/proxy_openai.go#L86-L91): `bifrostSDK == nil` 時の legacy fallback 分岐が存在
- [proxy_openai.go L260-L324](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/shared/libs/go/llmgateway/proxy_openai.go#L260-L324): `handleOpenAIResponsesLegacy` 関数が provider_forwarder.go に依存
- `/v1/messages` (proxy_anthropic.go) と同様に、Bifrost SDK 必須に統一すべき

**要件**:
1. `handleOpenAIResponsesLegacy` 関数を削除する
2. `bifrostSDK == nil` 時は、`/v1/messages` と同様に 503 エラーを返す
3. 削除後も既存の Bifrost SDK パスのテストが全て通ること

### R-PF: provider_forwarder.go 削除

**目的**: `handleOpenAIResponsesLegacy` 削除後、provider_forwarder.go を参照するコードがなくなるため削除する。

**前提条件**: R-OAI 完了後

**削除対象**:

| ファイル | 行数 | 内容 |
|----------|------|------|
| [provider_forwarder.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/shared/libs/go/llmgateway/provider_forwarder.go) | 325行 | Legacy HTTP forwarder, proxyResponse, overrideProviderBaseURL |
| [provider_forwarder_test.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/shared/libs/go/llmgateway/provider_forwarder_test.go) | 301行 | そのテスト |

**合計削除**: 626行

**注意**: `proxyResponse` 関数は provider_forwarder.go に定義されている。legacy 削除後にこの関数がどこからも参照されないことを確認してから削除すること。`rewriteModelField` は fallback.go に定義されているため影響なし。

### R-OLL: Ollama 統合テスト

**目的**: Provider Registry に登録済みの Ollama プロバイダーが Bifrost SDK 経由で動作することを検証する。

**前提条件**: Ollama サーバーがローカル環境で起動していること (http://localhost:11434)

**要件**:
1. `tests/llm_ollama_test.go` を作成する
2. model_profiles.yaml に Ollama モデルプロファイルを追加 (テスト用)
3. 以下を検証するテストケースを含める:
   - Provider Registry で Ollama プロバイダーが正しく解決される
   - Bifrost SDK 経由での non-streaming リクエスト/レスポンス
   - Bifrost SDK 経由での streaming リクエスト/レスポンス
4. Ollama サーバーが利用不可の場合はスキップ可能とする (ただしプロジェクトルール上 `t.Skip()` は禁止のため、環境変数 or 接続チェックでテスト自体の実行を制御する)

**Ollama プロバイダー登録状況**:
- [provider_ollama.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/shared/libs/go/llmgateway/provider_ollama.go): `init()` で `RegisterProvider(&ollamaProvider{})` 登録済み
- BaseURL: `http://localhost:11434`
- BifrostProvider: `bifrostSchemas.Ollama`
- 認証: 不要 (デフォルト)

## 実現方針 (Implementation Approach)

### 依存関係

```
R-OAI (proxy_openai legacy 削除)
  |
  v
R-PF (provider_forwarder 削除)

R-OLL (Ollama テスト) -- 独立して実行可能
```

### 実装順序

1. **R-OAI**: proxy_openai.go の legacy fallback 削除
   - `handleOpenAIResponsesLegacy` 関数を削除
   - `bifrostSDK == nil` 時のエラーハンドリング追加
   - テスト更新 (proxy_openai_test.go の legacy テストがある場合は削除/更新)

2. **R-PF**: provider_forwarder.go + test 削除
   - `proxyResponse` 等の参照がなくなったことを確認
   - `git rm` で削除
   - `buildRetryConfig` は proxy.go に定義されているため影響なし

3. **R-OLL**: Ollama 統合テスト作成
   - `tests/llm_ollama_test.go` 作成
   - model_profiles.yaml へのテスト用 Ollama プロファイル追加

### 技術的な検討事項

**provider_forwarder.go 内のユーティリティ関数**:
- `proxyResponse`: legacy passthrough 専用。Bifrost SDK パスでは不要
- `overrideProviderBaseURL`: テスト用ヘルパー。legacy テスト削除とともに不要
- `RetryConfig`, `buildRetryConfig`: proxy.go に定義されている別の関数。provider_forwarder.go の `forwardWithRetry` でのみ使用

## 検証シナリオ (Verification Scenarios)

### R-OAI + R-PF 検証

1. proxy_openai.go から `handleOpenAIResponsesLegacy` を削除
2. `bifrostSDK == nil` 時に 503 エラーが返ることを確認
3. provider_forwarder.go と provider_forwarder_test.go を削除
4. `./scripts/process/build.sh` で全体ビルドが通ること (参照エラーなし)
5. Bifrost SDK 経由の `/v1/responses` が正常動作すること

### R-OLL 検証

1. ローカルで Ollama サーバーを起動 (`ollama serve`)
2. 適当なモデルを pull (`ollama pull llama3.2:1b` 等)
3. model_profiles.yaml に Ollama プロファイルを設定
4. 統合テスト実行:
   - non-streaming: テキスト応答が返ること
   - streaming: SSE イベントが順序通り返ること
5. Ollama サーバー未起動時にテストが適切にハンドリングされること

## テスト項目 (Testing for the Requirements)

### R-OAI + R-PF

```bash
# 削除後の全体ビルド
./scripts/process/build.sh

# LLM 関連統合テスト
./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories llm

# E2E テスト (Codex)
./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestE2E_CodingAgentStreaming"
./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestE2E_CodingAgentDefaultModel"
```

### R-OLL

```bash
# Ollama 統合テスト
./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestOllama"
```
