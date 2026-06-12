# 031-Remaining-Work-Summary

> **Source**: Part 1 (040), Part 2 (041), Part 3 (042) の実装計画で未完了となった残作業の統合仕様

## 背景 (Background)

仕様 030 (Factory/Registry/Bifrost Unification) を Part 1-3 に分割して実装した。Part 1 は全完了、Part 2 は一部完了 (R5 client + /v1/chat/completions 削除)、Part 3 は Phase A (R6 Example 簡素化) のみ完了。

以下の要件が未実装のまま残されている:

| 要件 | 元 Part | 状態 | 概要 |
|------|---------|------|------|
| R3 | Part 2 Step 4-6 | 未実装 | handleAnthropicMessages の Bifrost SDK primary path 化 |
| R4 | Part 2 Step 8 | 未実装 | Ollama プロバイダーの統合テスト |
| R7 | Part 3 Phase B | 未実装 (R3 に依存) | レガシーコード削除 (convert_*.go 等 約3,800行) |

## 要件 (Requirements)

### R3: handleAnthropicMessages の Bifrost SDK 一本化

**目的**: `/v1/messages` ハンドラで使われているレガシー変換パス (convert_a2o, convert_a2g, convert_a2r + provider_forwarder) を、Bifrost SDK 経由のパスに置き換える。

**現状の問題**:
- [proxy_anthropic.go L108-L175](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/shared/libs/go/llmgateway/proxy_anthropic.go#L108-L175): プロバイダーごとの switch-case でリクエスト変換 (Anthropic -> OpenAI, Anthropic -> Gemini, Anthropic -> Responses) を手動実装している
- [proxy_anthropic.go L212-L330](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/shared/libs/go/llmgateway/proxy_anthropic.go#L212-L330): レスポンス逆変換も同様に手動実装

**要件**:
1. Anthropic Messages API リクエストを受信したら、Bifrost SDK の `ResponsesRequest` / `ResponsesStreamRequest` に変換して委譲する
2. Bifrost SDK のレスポンスを Anthropic Messages API 形式に逆変換してクライアントに返す
3. ストリーミング (SSE) 対応を含む
4. `provider_forwarder.go` を経由しない新しいパスとして実装する
5. 段階的移行: まず Bifrost SDK を primary、既存パスを fallback として並存させ、安定後に legacy を削除

**対象プロバイダー**:
- anthropic: native Anthropic API (変換不要、パススルー -> Bifrost SDK 委譲)
- openai: Chat Completions パス (Anthropic -> Bifrost SDK -> OpenAI)
- google: Gemini パス (Anthropic -> Bifrost SDK -> Google)
- ollama: OpenAI 互換パス (Anthropic -> Bifrost SDK -> Ollama)

**作成するファイル**:
- `llmgateway/convert_anthropic_to_bifrost.go`: Anthropic Messages -> BifrostResponsesRequest 変換
- `llmgateway/convert_anthropic_to_bifrost_test.go`: 変換テスト (TDD)
- `llmgateway/convert_bifrost_to_anthropic.go`: BifrostResponsesResponse -> Anthropic Messages 逆変換
- `llmgateway/convert_bifrost_to_anthropic_test.go`: 逆変換テスト (TDD)

**変更するファイル**:
- `llmgateway/proxy_anthropic.go`: Bifrost SDK primary パスの追加

### R4: Ollama プロバイダー統合テスト

**目的**: Part 1 で Provider Registry に登録済みの Ollama プロバイダーが実際に動作することを検証する。

**要件**:
1. `tests/llm_ollama_test.go` に Ollama との接続テストを作成
2. `model_profiles.yaml` に Ollama プロファイルが定義されていることを前提とする
3. Provider Registry 経由で Ollama プロバイダーが正しく解決されることを確認
4. Ollama サーバーが利用可能な環境でのみ実行

**注意**: Ollama テスト環境の前提条件が不明確。事前に環境要件を整理する。

### R7: レガシーコード削除

**目的**: R3 が完了し Bifrost SDK パスが安定動作した後、不要になったレガシー変換コードを削除する。

**前提条件**: R3 の Bifrost SDK primary パスで全プロバイダー (anthropic, openai, google, ollama) の統合テストが成功すること。

**削除対象**:

| ファイル | 行数 | 内容 |
|----------|------|------|
| [convert_a2o.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/shared/libs/go/llmgateway/convert_a2o.go) | 約358行 | Anthropic -> OpenAI Chat Completions 変換 |
| [convert_a2o_test.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/shared/libs/go/llmgateway/convert_a2o_test.go) | 約451行 | そのテスト |
| [convert_a2g.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/shared/libs/go/llmgateway/convert_a2g.go) | 約493行 | Anthropic -> Google Gemini 変換 |
| [convert_a2g_test.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/shared/libs/go/llmgateway/convert_a2g_test.go) | 約377行 | そのテスト |
| [convert_a2r.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/shared/libs/go/llmgateway/convert_a2r.go) | 約517行 | Anthropic -> OpenAI Responses 変換 |
| [convert_a2r_test.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/shared/libs/go/llmgateway/convert_a2r_test.go) | 約477行 | そのテスト |
| [convert_a2r_stream_test.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/shared/libs/go/llmgateway/convert_a2r_stream_test.go) | 約60行 | ストリーム変換テスト |
| [stream_converter.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/shared/libs/go/llmgateway/stream_converter.go) | 約292行 | OpenAI/Gemini SSE -> Anthropic SSE ストリーム変換 |
| [provider_forwarder.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/shared/libs/go/llmgateway/provider_forwarder.go) | 約336行 | Legacy provider forwarder |
| provider_forwarder_test.go (存在する場合) | 約308行 | そのテスト |

**合計削除**: 約 3,669行 (テスト含む)

**変更するファイル**:
- `llmgateway/proxy_anthropic.go`: legacy fallback パスを削除し、Bifrost SDK パスのみにする

## 実現方針 (Implementation Approach)

### 依存関係

```
R3 (Bifrost SDK 一本化)
  |
  v
R7 (レガシーコード削除) -- R3完了後に実行
  
R4 (Ollama テスト) -- 独立して実行可能だが R3 完了後に Bifrost 経由で検証するのが理想
```

### 段階的実装

1. **R3: Bifrost SDK primary path**
   - Anthropic <-> Bifrost 変換レイヤーを TDD で作成
   - proxy_anthropic.go に新パスを追加 (primary: Bifrost SDK, fallback: legacy)
   - 全プロバイダーの統合テスト + E2E テストで安定動作を確認

2. **R4: Ollama 統合テスト**
   - R3 完了後、Bifrost SDK 経由で Ollama との通信を検証
   - 環境依存テストのため、テスト環境の前提条件を明確にする

3. **R7: レガシーコード削除**
   - R3 が全プロバイダーで安定動作後、legacy fallback を削除
   - convert_*.go, stream_converter.go, provider_forwarder.go を `git rm`
   - proxy_anthropic.go から legacy 分岐を除去

### 技術的な検討事項

**Bifrost SDK の Anthropic 対応範囲**: Bifrost SDK が Anthropic Messages API 形式のリクエストをネイティブにサポートしているか、それとも Responses API 形式のみか。ネイティブ対応であれば変換レイヤーが簡素化される。

- `/v1/responses` (Codex) は既に Bifrost SDK 経由で動作しているため、同様のパターンで `/v1/messages` も移行可能
- Bifrost SDK 側の `ResponsesRequest` / `ResponsesStreamRequest` の型定義を確認し、変換マッピングを設計する

## 検証シナリオ (Verification Scenarios)

### R3 検証

1. `model_profiles.yaml` で各プロバイダー (anthropic, openai, google) に対応するモデルを設定
2. Claude Code CLI 経由で `/v1/messages` エンドポイントにリクエスト送信
3. Bifrost SDK primary path が使用されたことをログで確認
4. 正常なレスポンスが Anthropic Messages API 形式で返却されることを確認
5. ストリーミングモードでも同様に動作すること
6. Bifrost SDK パスでエラーが発生した場合、legacy fallback に切り替わること (段階移行中)

### R4 検証

1. ローカルに Ollama サーバーが起動していることを確認
2. model_profiles.yaml に Ollama モデルプロファイルを追加
3. Provider Registry 経由で Ollama が解決されることを確認
4. 実際のリクエスト送信とレスポンス受信を検証

### R7 検証

1. R3 の Bifrost SDK primary path が全プロバイダーで安定動作していること (前提)
2. convert_*.go, stream_converter.go, provider_forwarder.go を削除
3. `./scripts/process/build.sh` でビルドが通ること (参照エラーなし)
4. `./scripts/process/integration_test.sh` で全テストが成功すること
5. E2E テスト (TestClaudeCodeE2E 等) が成功すること

## テスト項目 (Testing for the Requirements)

### R3

```bash
# 単体テスト (変換レイヤー)
./scripts/process/build.sh

# LLM 統合テスト
./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories llm

# E2E テスト
./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestE2E_CodingAgentStreaming"
./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestE2E_CodingAgentDefaultModel"
```

### R4

```bash
# Ollama 統合テスト (Ollama 環境必須)
./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestOllama"
```

### R7

```bash
# 削除後の全体ビルド
./scripts/process/build.sh

# 全統合テスト
./scripts/process/build.sh && ./scripts/process/integration_test.sh

# E2E テスト
./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestE2E_"
```
