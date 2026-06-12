# 035-Gemini-Support-TestPlan

> **Source Specification**: [025-Gemini-Support.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/025-Gemini-Support.md)

## Goal Description
Google Gemini プロバイダ (`google`) 対応における機能要件（リクエスト/レスポンス変換、ストリーム中継、認証ヘッダー付加）に加え、Gemini API 特有の JSON スキーマ非サポート属性（`$schema`、`additionalProperties` など）によるエラー（`Cannot find field`）を回避するためのスキーマクレンジング要件（R1.1）が確実に実現されていることを検証するテスト計画を設計します。また、モック環境を用いて `cawa-client` からの実際のコマンド実行とファイル生成を模したエンドツーエンドの結合検証を行い、対話およびツール実行の実現性を担保します。

## User Review Required
E2Eテストでは本物の Gemini API キーに依存しないようにするため、ローカルにモックの Gemini API サーバーを立てて検証を行います。これにより、環境依存を排除して安定した自動テスト実行を可能にしています。この検証設計アプローチについてレビューをお願いします。

---

## 1. 要件一覧 (Extracted Requirements)

| ID | 要件 | 分類 |
| :--- | :--- | :--- |
| REQ-001 | リクエストの変換 (Anthropic -> Gemini、大文字タイプ変換) | 機能 |
| REQ-002 | JSONスキーマのクレンジング (R1.1: 非サポートプロパティの除去) | 機能 |
| REQ-003 | レスポンスおよび SSE ストリームの変換 (Gemini -> Anthropic) | 機能 |
| REQ-004 | Gateway 中継と認証ヘッダーの付加 (`x-goog-api-key`) | 統合 |
| REQ-005 | cawa-client を用いた実際の対話およびツール実行による結合検証 | 統合 |

---

## 2. 要件別 実現根拠と検証設計

### REQ-002: JSONスキーマのクレンジング

#### 2.1 実現根拠 (Evidence of Fulfillment)
1. **[E-002-1]**: Gemini に送信されるリクエストの JSON スキーマ内から、`$schema`、`additionalProperties`、`const`、`exclusiveMinimum`、`propertyNames` などの非サポートキーが完全に除去されていること。
2. **[E-002-2]**: クレンジング後も、ツール名や本来必要なパラメータ定義（`properties` や `required` など）が損なわれず保持されていること。

#### 2.2 確認手段 (Verification Methods)

| 根拠ID | 確認の視点 | 確認手段 |
| :--- | :--- | :--- |
| E-002-1 | 構造データ確認 | 単体テストにて変換後の JSON スキーマを unmarshal し、対象キーが存在しないことをアサートする。 |
| E-002-2 | 構造データ確認 | 変換後も必要なフィールド値が正しく格納されているか確認する。 |

#### 2.3 確認手順 (Detailed Procedures)

##### E-002-1, E-002-2: スキーマクレンジングの検証
1. **前提条件**: なし。
2. **入力**: `$schema`, `additionalProperties` を含む複雑な JSON スキーマ。
3. **操作手順**: `convertSchemaTypesToUppercase` と組み合わせて、非サポート属性を除去するクレンジング関数を実行する。
4. **期待結果**: 非サポート属性が削除され、かつ `type` が大文字化された JSON になること。
5. **判定基準**: `$schema` などの文字列が変換後の JSON に含まれず、かつ `type: "OBJECT"` などの大文字化が成功していること。

#### 2.4 テストシナリオ (Test Scenarios)

##### TC-001: 単体テストによるスキーマクレンジングの検証
* **対応要件**: REQ-002
* **対応根拠**: E-002-1, E-002-2
* **テスト種別**: 単体テスト
* **配置先**: [convert_a2g_test.go](file:///shared/libs/go/llmgateway/convert_a2g_test.go)
* **テスト関数名**: `TestConvertAnthropicRequestToGemini_SchemaCleansing`
* **テストシナリオ**:
  1. [Arrange] `$schema` や `additionalProperties` を含んだツール定義を持つ Anthropic リクエスト JSON を用意する。
  2. [Act] `ConvertAnthropicRequestToGemini` を呼び出す。
  3. [Assert] 生成された JSON から `$schema` などのキーが削除され、`type` が大文字化されていることをアサートする。

---

### REQ-005: 実際のコマンド実行でのモック結合検証

#### 2.1 実現根拠 (Evidence of Fulfillment)
1. **[E-005-1]**: `cawa-client` から `gemini-3.5-flash` モデルを指定してコマンドを実行した際、エラーを吐かずに正常終了コード（`0`）を返すこと。
2. **[E-005-2]**: ツール実行指示を行った際、モックの Gemini 側から返される `functionCall` が `cawa-client` に渡り、実際にローカルでファイルが作成されること。

#### 2.2 確認手段 (Verification Methods)

| 根拠ID | 確認の視点 | 確認手段 |
| :--- | :--- | :--- |
| E-005-1 | プロセス結果確認 | テストスクリプトにて `cawa-client` を実行し、終了コードおよび標準出力を検証する。 |
| E-005-2 | ファイル出力確認 | ツール実行プロンプト送信後、ワークスペース上の `./tmp/` 内に指定ファイルが生成されることを検証する。 |

#### 2.3 確認手順 (Detailed Procedures)

##### E-005-1, E-005-2: cawa-client 結合検証
1. **前提条件**:
   - `cawa-client` および `standalone` がビルドされていること。
   - `model_profiles.yaml` に `gemini-3.5-flash` (google) が定義されていること。
2. **入力**: `"Create a game.py file to play CUI based reversi."` などのファイル作成プロンプト。
3. **操作手順**:
   - ローカルにモック Gemini API サーバーを立ち上げる。
   - ポート 3100 にて `standalone` HAG ゲートウェイサーバーをモック Vault（APIキー登録済み）を指定して起動する。
   - `cawa-client` プロセスを起動し、上記プロンプトとワークディレクトリ `./tmp/` を指定して実行する。
4. **期待結果**:
   - コマンドが正常に終了し、`./tmp/` 配下に `game.py` が生成されること。
5. **判定基準**: 終了コードが `0` であり、かつ `./tmp/game.py` が存在すること。

#### 2.4 テストシナリオ (Test Scenarios)

##### TC-002: モック結合による cawa-client 対話テスト
* **対応要件**: REQ-005
* **対応根拠**: E-005-1, E-005-2
* **テスト種別**: 結合/E2Eテスト
* **配置先**: [gemini_e2e_test.go](file:///tests/gemini_e2e_test.go)
* **テスト関数名**: `TestGeminiE2E_CawaClient_FileCreation`
* **テストシナリオ**:
  1. [Arrange] モック Gemini サーバー（ツール実行 `Write` を返すモック）を起動し、HAG サーバーをモック API キーで立ち上げる。
  2. [Act] `cawa-client` バイナリを `exec.Command` で呼び出し、プロンプト `"Create test.txt"` を投げる。
  3. [Assert] コマンドの正常終了を確認し、`./tmp/test.txt` が実際に作成されたか検証する。

---

## 3. テスト実装サマリー

### テストケース一覧

| TC-ID | テストケース名 | 対応要件 | テスト種別 | 配置先 |
| :--- | :--- | :--- | :--- | :--- |
| TC-001 | スキーマクレンジングの検証 | REQ-002 | 単体テスト | `shared/libs/go/llmgateway/convert_a2g_test.go` |
| TC-002 | cawa-client 結合検証 | REQ-005 | 結合/E2E | `tests/gemini_e2e_test.go` |

### 要件カバレッジマトリクス

| 要件 | 単体テスト | 統合テスト | E2Eテスト | カバー状態 |
| :--- | :--- | :--- | :--- | :--- |
| REQ-001 | PASS済 | - | - | 完全 |
| REQ-002 | TC-001 | - | - | 完全 |
| REQ-003 | PASS済 | - | - | 完全 |
| REQ-004 | PASS済 | - | - | 完全 |
| REQ-005 | - | - | TC-002 | 完全 |

---

## 4. Step-by-Step Implementation Guide

1.  **JSONスキーマクレンジング関数の追加**:
    *   [ ] `shared/libs/go/llmgateway/convert_a2g.go` 内の `convertSchemaTypesToUppercase` 処理を拡張、または別途クレンジング処理を実装し、非サポートキーを除去する。
2.  **スキーマクレンジングの単体テスト実装**:
    *   [ ] `shared/libs/go/llmgateway/convert_a2g_test.go` に `TestConvertAnthropicRequestToGemini_SchemaCleansing` を追加。
3.  **cawa-client 結合検証テストの実装**:
    *   [ ] `tests/gemini_e2e_test.go` に `TestGeminiE2E_CawaClient_FileCreation` を追加。
    *   [ ] `exec.Command` を用いて `./bin/cawa-client` を実際に起動し、ファイル生成が行われるか検証する。

---

## 5. Verification Plan

1.  **Build & Unit Tests**: `./scripts/process/build.sh`
2.  **統合・E2Eテスト**: `./scripts/process/integration_test.sh --specify "TestGeminiE2E"`
