# 026-LLMGP-ResponsesAPI-CodexSupport-TestPlan

> **Source Specification**: [017-LLMGP-ResponsesAPI-CodexSupport.md](file:///prompts/phases/000-foundation/branches/feat-llm-backend/ideas/017-LLMGP-ResponsesAPI-CodexSupport.md)

## Goal Description

仕様書 017 の要件 (R1-R7) が実際に動作していることを、**実際の OpenAI Responses API を呼び出す E2E テスト**で検証するためのテスト計画。既存の単体テスト (モック) に加えて、実 API への HTTP リクエスト送信とレスポンス検証を通じて「Codex モデルが LLMGP 経由で本当に動作する」ことを証明する。

## User Review Required

None.

---

## 1. 要件一覧 (Extracted Requirements)

| ID | 要件 | 分類 |
| :--- | :--- | :--- |
| REQ-001 | model_profiles.yaml の `mode: responses` が正しくパースされ、RoutedModel に伝播する | 機能 |
| REQ-002 | Anthropic Messages API リクエストが Responses API 形式に正しく変換される | 機能 |
| REQ-003 | Responses API レスポンスが Anthropic Messages API 形式に正しく逆変換される (非ストリーミング) | 機能 |
| REQ-004 | Responses API ストリーミングイベントが Anthropic SSE 形式に正しく変換される | 機能 |
| REQ-005 | mode=responses のモデルが `/v1/responses` に転送される (mode="" は `/v1/chat/completions` に転送) | 機能 |
| REQ-006 | codex-mini-latest を LLMGP 経由で実際に呼び出し、非ストリーミングで応答が返る | 統合 |
| REQ-007 | codex-mini-latest を LLMGP 経由で実際に呼び出し、ストリーミングで応答が返る | 統合 |
| REQ-008 | 既存の Chat Completions API ルート (gpt-4.1-mini 等) にリグレッションがない | 統合 |

---

## 2. 要件別 実現根拠と検証設計

### REQ-001 ~ REQ-005: 単体テストで検証済み

これらの要件は実装計画 025 の Step 1-7 で作成済みの単体テスト (モック HTTP サーバー使用) で検証されている。

- `TestModelRouter_ResolveModel_WithMode` (routing_test.go)
- `TestConvertAnthropicRequestToResponses_*` (convert_a2r_test.go, 6件)
- `TestConvertResponsesResponseToAnthropic_*` (convert_a2r_test.go, 3件)
- `TestConvertResponsesStreamToAnthropic_*` (convert_a2r_test.go, 3件)
- `TestHandleAnthropicMessages_ResponsesMode_*` (proxy_anthropic_test.go, 3件)

**ただし、これらはモックであり実際の API を呼び出していない。** REQ-006, REQ-007 で実 API E2E を検証する。

---

### REQ-006: codex-mini-latest 非ストリーミング E2E

#### 2.1 実現根拠 (Evidence of Fulfillment)

1. **E-006-1**: LLMGP が codex-mini-latest へのリクエストを `/v1/responses` に正しく転送し、OpenAI Responses API から HTTP 200 レスポンスが返る
2. **E-006-2**: レスポンスが Anthropic Messages API 形式 (`type: message`, `content[]`, `stop_reason`) に正しく変換されている
3. **E-006-3**: レスポンスの `content[0].text` に LLM が生成したテキストが含まれている (空でない)
4. **E-006-4**: `usage` フィールドに `input_tokens` と `output_tokens` が含まれている
5. **E-006-5**: サーバーログに `direction=anthropic->responses` が出力されている (正しいアダプタが使用されたことの証拠)

#### 2.2 確認手段 (Verification Methods)

| 根拠ID | 確認の視点 | 確認手段 |
| :--- | :--- | :--- |
| E-006-1 | API応答確認 | HTTP POST `/v1/messages` に codex-mini-latest でリクエストし、HTTP 200 を確認 |
| E-006-2 | データ確認 | レスポンス JSON の構造が Anthropic 形式であることを検証 |
| E-006-3 | データ確認 | `content[0].type == "text"` かつ `content[0].text` が非空 |
| E-006-4 | データ確認 | `usage.input_tokens > 0` かつ `usage.output_tokens > 0` |
| E-006-5 | ログ確認 | テスト実行時のサーバーログに `anthropic->responses` が含まれること |

#### 2.3 確認手順 (Detailed Procedures)

##### E-006-1 ~ E-006-4: 非ストリーミング Codex 呼び出し

1. **前提条件**: OpenAI API キーが OS Keyring に登録されていること (`bin/vault-cli set --provider openai --stdin`)
2. **入力**:
   ```json
   {
     "model": "codex-mini-latest",
     "max_tokens": 128,
     "messages": [{"role": "user", "content": "Say exactly: responses api e2e test ok"}]
   }
   ```
3. **操作手順**:
   ```bash
   # テストサーバー起動 (testServer ヘルパー使用)
   # HTTP POST baseURL/v1/messages
   ```
4. **期待結果**:
   - HTTP ステータス: 200
   - レスポンス JSON:
     ```json
     {
       "id": "resp_...",
       "type": "message",
       "role": "assistant",
       "content": [{"type": "text", "text": "...non-empty..."}],
       "model": "codex-mini-latest",
       "stop_reason": "end_turn",
       "usage": {"input_tokens": N, "output_tokens": M}
     }
     ```
5. **判定基準**:
   - `type == "message"`
   - `role == "assistant"`
   - `content` 配列が非空で、`content[0].type == "text"` かつ `content[0].text` が非空
   - `stop_reason` が存在する
   - `usage.input_tokens > 0`

#### 2.4 テストシナリオ

##### TC-001: Codex 非ストリーミング E2E

* **対応要件**: REQ-006
* **対応根拠**: E-006-1, E-006-2, E-006-3, E-006-4
* **テスト種別**: E2E テスト (統合テスト)
* **配置先**: `tests/llm_gateway_test.go`
* **テスト関数名**: `TestResponsesAPI_Codex_via_AnthropicEndpoint_NonStream`
* **前提条件**: OpenAI API キーが vault に登録済み
* **テストシナリオ**:
    1. [Arrange] `checkKeyringAvailable(t, "openai")` で API キーの存在確認。`testServer(t)` でサーバー起動。
    2. [Act] `/v1/messages` に `model: codex-mini-latest`, `max_tokens: 128` でリクエスト送信。
    3. [Assert]
       - HTTP 200
       - `type == "message"`, `role == "assistant"`
       - `content[]` 非空、`content[0].type == "text"`, `content[0].text` 非空
       - `stop_reason` フィールドが存在
       - `usage.input_tokens > 0`
* **実装メモ**: 既存の `TestCrossProvider_OpenAI_via_AnthropicEndpoint_NonStream` のパターンをベースに、モデル名とレスポンス検証項目を変更。タイムアウトは 60 秒 (Codex モデルは応答が遅い可能性がある)。

---

### REQ-007: codex-mini-latest ストリーミング E2E

#### 2.1 実現根拠 (Evidence of Fulfillment)

1. **E-007-1**: LLMGP がストリーミングリクエストを `/v1/responses` に転送し、SSE レスポンスが返る
2. **E-007-2**: SSE イベントに `event: message_start` が含まれる
3. **E-007-3**: SSE イベントに `event: content_block_delta` + `text_delta` が含まれる
4. **E-007-4**: SSE イベントに `event: message_stop` が含まれる
5. **E-007-5**: イベント順序が `message_start` -> `content_block_delta` -> `message_stop`

#### 2.2 確認手段 (Verification Methods)

| 根拠ID | 確認の視点 | 確認手段 |
| :--- | :--- | :--- |
| E-007-1 | 通信確認 | `Content-Type: text/event-stream` レスポンスを確認 |
| E-007-2 | データ確認 | SSE ストリーム内に `message_start` イベントが存在 |
| E-007-3 | データ確認 | SSE ストリーム内に `text_delta` が存在 |
| E-007-4 | データ確認 | SSE ストリーム内に `message_stop` イベントが存在 |
| E-007-5 | データ確認 | `message_start` の位置 < `text_delta` の位置 < `message_stop` の位置 |

#### 2.3 確認手順 (Detailed Procedures)

##### E-007-1 ~ E-007-5: ストリーミング Codex 呼び出し

1. **前提条件**: OpenAI API キーが OS Keyring に登録されていること
2. **入力**:
   ```json
   {
     "model": "codex-mini-latest",
     "max_tokens": 128,
     "stream": true,
     "messages": [{"role": "user", "content": "Say exactly: responses api streaming test ok"}]
   }
   ```
3. **操作手順**: HTTP POST baseURL/v1/messages (stream=true)
4. **期待結果**:
   - HTTP ステータス: 200
   - Content-Type: text/event-stream
   - SSE イベント列に以下が含まれる:
     - `event: message_start` + `data: {"type":"message_start",...}`
     - `event: content_block_delta` + `data: {...,"delta":{"type":"text_delta","text":"..."}}`
     - `event: message_stop` + `data: {"type":"message_stop"}`
5. **判定基準**:
   - 上記 3 イベントが全て存在
   - 順序: message_start < content_block_delta (text_delta) < message_stop

#### 2.4 テストシナリオ

##### TC-002: Codex ストリーミング E2E

* **対応要件**: REQ-007
* **対応根拠**: E-007-1, E-007-2, E-007-3, E-007-4, E-007-5
* **テスト種別**: E2E テスト (統合テスト)
* **配置先**: `tests/llm_gateway_test.go`
* **テスト関数名**: `TestResponsesAPI_Codex_via_AnthropicEndpoint_Stream`
* **前提条件**: OpenAI API キーが vault に登録済み
* **テストシナリオ**:
    1. [Arrange] `checkKeyringAvailable(t, "openai")` + `testServer(t)`
    2. [Act] `/v1/messages` に `model: codex-mini-latest`, `stream: true` でリクエスト送信
    3. [Assert]
       - HTTP 200
       - `event: message_start` が存在
       - `event: content_block_delta` + `text_delta` が存在
       - `event: message_stop` が存在
       - イベント順序が正しい
* **実装メモ**: 既存の `TestCrossProvider_OpenAI_via_AnthropicEndpoint_Stream` のパターンをベースに。タイムアウトは 60 秒。

---

### REQ-008: 既存ルートのリグレッション確認

#### 2.4 テストシナリオ

##### TC-003: 既存 Cross-Provider テストがリグレッションしないこと

* **対応要件**: REQ-008
* **テスト種別**: リグレッションテスト (既存)
* **配置先**: `tests/llm_gateway_test.go` (既存テスト)
* **テスト関数名**: `TestCrossProvider_OpenAI_via_AnthropicEndpoint_NonStream` (既存), `TestCrossProvider_OpenAI_via_AnthropicEndpoint_Stream` (既存)
* **テストシナリオ**: 新規実装不要。既存テストを `--specify` で実行し PASS を確認。

---

## 3. テスト実装サマリー

### テストケース一覧

| TC-ID | テストケース名 | 対応要件 | テスト種別 | 配置先 |
| :--- | :--- | :--- | :--- | :--- |
| TC-001 | Codex 非ストリーミング E2E | REQ-006 | E2E/統合テスト | tests/llm_gateway_test.go |
| TC-002 | Codex ストリーミング E2E | REQ-007 | E2E/統合テスト | tests/llm_gateway_test.go |
| TC-003 | 既存 Cross-Provider リグレッション | REQ-008 | リグレッション (既存) | tests/llm_gateway_test.go |

### 要件カバレッジマトリクス

| 要件 | 単体テスト (既存) | E2E テスト | カバー状態 |
| :--- | :--- | :--- | :--- |
| REQ-001 (Mode パース) | TestModelRouter_ResolveModel_WithMode | - | 完全 (単体で十分) |
| REQ-002 (リクエスト変換) | TestConvertAnthropicRequestToResponses_* (6件) | TC-001 (暗黙的) | 完全 |
| REQ-003 (レスポンス変換) | TestConvertResponsesResponseToAnthropic_* (3件) | TC-001 (暗黙的) | 完全 |
| REQ-004 (ストリーミング変換) | TestConvertResponsesStreamToAnthropic_* (3件) | TC-002 (暗黙的) | 完全 |
| REQ-005 (ルーティング) | TestHandleAnthropicMessages_ResponsesMode_* (3件) | TC-001, TC-002 | 完全 |
| REQ-006 (非ストリーミング E2E) | - | TC-001 | 完全 |
| REQ-007 (ストリーミング E2E) | - | TC-002 | 完全 |
| REQ-008 (リグレッション) | - | TC-003 (既存) | 完全 |

### セルフレビュー結果

1. **網羅性**: 全 8 要件に対してテストケースが存在。暗黙要件 (usage 検証、イベント順序) もカバー。
2. **証拠の十分性**: 単なる「200 が返る」ではなく、レスポンス構造 (type, role, content, stop_reason, usage) を詳細検証。ストリーミングでは SSE イベントの存在と順序を検証。
3. **迂回・抜け道の排除**: サーバーログの `anthropic->responses` 方向ログにより、正しいアダプタが使用されたことを確認可能。テスト内でもレスポンス構造が Anthropic 形式 (Chat Completions 形式ではなく) であることを検証。
4. **依存関係の整合性**: 単体テスト (REQ-001~005) -> E2E テスト (REQ-006~007) のボトムアップ順序。E2E は単体テストが通過した前提で全体を通して検証。

---

## 4. Step-by-Step Implementation Guide

- [x] **Step 1: E2E テストの実装**
  - Edit `tests/llm_gateway_test.go`
  - `TestResponsesAPI_Codex_via_AnthropicEndpoint_NonStream` を追加
  - `TestResponsesAPI_Codex_via_AnthropicEndpoint_Stream` を追加
  - `git add && git commit -m "test: add Responses API Codex E2E tests"`

- [x] **Step 2: ビルド + 単体テスト**
  - `./scripts/process/build.sh` を実行
  - 全単体テストが PASS することを確認

- [x] **Step 3: E2E テスト実行 (Codex のみ)**
  - `./scripts/process/integration_test.sh --specify "TestResponsesAPI_Codex"` を実行
  - 2 件のテストが PASS することを確認
  - サーバーログに `direction=anthropic->responses` が出力されていることを確認

- [x] **Step 4: リグレッション確認 (既存テスト)**
  - `./scripts/process/integration_test.sh --specify "TestCrossProvider|TestResponsesAPI|TestModelPassthrough"` を実行
  - 全テストが PASS することを確認

- [x] **Step 5: 全 LLM テスト実行**
  - `./scripts/process/integration_test.sh --categories llm` を実行
  - 全テストが PASS することを確認

- [x] **Step 6: 総合判定**
  - testing-rules.md Section 12 に基づく総合判定を実施

---

## 5. Test Execution Plan

### 5.1 ビルドと単体テスト

```bash
./scripts/process/build.sh
```

### 5.2 E2E テスト (選択的実行)

```bash
./scripts/process/integration_test.sh --specify "TestResponsesAPI_Codex"
```

### 5.3 リグレッション確認

```bash
./scripts/process/integration_test.sh --specify "TestCrossProvider|TestResponsesAPI|TestModelPassthrough"
```

### 5.4 全 LLM テスト実行

```bash
./scripts/process/integration_test.sh --categories llm
```
