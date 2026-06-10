# 仕様書: Google Gemini (google プロバイダ) 対応

## 1. 背景 (Background)

現在、Headless Agent Gateway (HAG) サーバーおよび LLM Gateway Proxy (LLMGP) は Anthropic プロバイダおよび OpenAI プロバイダへの相互変換をサポートしていますが、Google Gemini モデル（`gemini-3.5-flash` など）を指定した際に、Google プロバイダ向けの翻訳・変換層（Translation Layer）が存在しないため、以下のエラーが発生して動作しません。
```
API Error: 400 cross-provider translation not supported for: google
```
HAG から Google Gemini のモデルに対話およびツール実行要求を正しく中継できるようにするため、Gateway に Google プロバイダへの翻訳機能を追加します。

## 2. 要件 (Requirements)

### R1: リクエストの変換 (Anthropic -> Google Gemini)
* Anthropic Messages API のリクエストを受け取り、Google Gemini API (`/v1beta/models/{model}:generateContent` または `streamGenerateContent`) の JSON 形式に変換する。
* **モデルのマッピング**: リクエストの `model` フィールドに指定されたモデル名をそのまま Gemini のモデル名として引き渡す。
* **システムプロンプトの変換**: Anthropic の `system` フィールドを Gemini の `systemInstruction` フィールドへマッピングする。
* **対話履歴の変換**: 
  * `user` ロールおよび `assistant` ロールを、Gemini の `role` フィールド (`user` / `model`) に正しくマッピングする。
  * `tool_use` (Anthropic) は、Gemini の `functionCall` を含むパートに変換する。
  * `tool_result` (Anthropic) は、Gemini の `functionResponse` を含むパートに変換する。
* **ツール定義の変換**:
  * Anthropic の `tools` 定義（JSONスキーマ）を、Gemini の `tools` -> `functionDeclarations` のスキーマ構造に変換する。
  * JSON スキーマタイプ（`string`, `object` など）について、Google Gemini API の要件に従い大文字（`STRING`, `OBJECT` など）に変換する処理を組み込む。
* **パラメータのマッピング**:
  * `temperature`, `max_tokens` (Gemini では `maxOutputTokens`) をマッピングする。

### R2: レスンポス（レスポンス）の変換 (Google Gemini -> Anthropic)
* **非ストリームレスポンス**:
  * Gemini API の `candidates[0].content` からテキストまたはツール呼び出し（`functionCall`）を抽出し、Anthropic Messages API のレスポンス形式に変換する。
  * `usageMetadata` から `input_tokens` と `output_tokens` を取得し、Anthropic の `usage` に変換する。
* **ストリームレスポンス (SSE)**:
  * `:streamGenerateContent` から送信される Google のチャンク（JSON形式）を監視し、Anthropic Messages API のストリームイベント（`message_start`, `content_block_start`, `content_block_delta`, `content_block_stop`, `message_delta`, `message_stop`）に変換して順次出力する。
  * ツール呼び出し（`functionCall`）が検知された場合、ストリームの stop_reason を `"tool_use"` として終了イベントを組み立てる。

### R3: Gateway フォワード処理の追加
* `proxy_anthropic.go` 内のプロバイダ選択スイッチに `google` のケースを追加する。
* リクエストが `google` プロバイダにルーティングされた場合、Google Gemini API のエンドポイント（例: `https://generativelanguage.googleapis.com`）にリクエストをフォワードする。
* 認証方法として、ヘッダーに `x-goog-api-key: [API_KEY]` を設定して送信する。

---

## 3. 実現方針 (Implementation Approach)

### 新規追加コンポーネント
- **[convert_a2g.go](file:///shared/libs/go/llmgateway/convert_a2g.go)**:
  Anthropic <=> Google Gemini API 間の JSON 構造体定義、およびリクエスト・レスポンスの変換ロジックを実装。
  * `ConvertAnthropicRequestToGemini(body []byte, logs ...logger.Logger) ([]byte, error)`
  * `ConvertGeminiResponseToAnthropic(body []byte, model string, logs ...logger.Logger) ([]byte, error)`
  * `ConvertGeminiStreamToAnthropic(reader io.Reader, writer io.Writer, model string, logs ...logger.Logger) error`

### 変更対象コンポーネント
- **[proxy_anthropic.go](file:///shared/libs/go/llmgateway/proxy_anthropic.go)**:
  `handleAnthropicMessages` 関数のプロバイダ処理スイッチに `case "google"` を追加し、リクエストを Gemini API エンドポイント宛てに変換・送信する処理を統合。

```go
	case "google":
		forwardPath = fmt.Sprintf("/v1beta/models/%s:generateContent", routed.Model)
		if routed.Stream {
			forwardPath = fmt.Sprintf("/v1beta/models/%s:streamGenerateContent?alt=sse", routed.Model)
		}
		// Convert request...
		converted, convErr := ConvertAnthropicRequestToGemini(body, p.logger)
```

- **Google API エンドポイントおよびヘッダーの適用**:
  Gemini 宛てに送信する際は、API Key を `x-goog-api-key` ヘッダーに設定してフォワードする。

---

## 4. 検証シナリオ (Verification Scenarios)

Google Gemini 用の API Key (GEMINI_API_KEY など) が設定された状態で以下の手順を検証します。

### シナリオ1: cawa-client による Gemini への単一メッセージ対話
1. HAG サーバーを起動する。
2. 以下のコマンドで、`gemini-2.5-flash` または `gemini-3.5-flash` モデル宛てにプロンプトを送信する。
   ```bash
   ./bin/cawa-client run --agent claudecode --model gemini-3.5-flash --prompt "Hello Gemini"
   ```
3. エラーなしで正常なテキスト返答が取得できることを確認する。

### シナリオ2: マルチターンおよびツール実行（ファイル作成）
1. 以下のコマンドで、ファイル作成の指示を Gemini へ送信する。
   ```bash
   ./bin/cawa-client run --agent claudecode --model gemini-3.5-flash --prompt "Create a test_gemini.py file that prints hello" --work-dir ./tmp/
   ```
2. Gemini がツール（`Write` など）の呼び出しを要求し、クライアントがそれに応答して `test_gemini.py` を作成し、完了することを確認する。

---

## 5. テスト項目 (Testing for the Requirements)

### 自動テスト (単体テスト)
* `convert_a2g_test.go` を作成し、以下の変換が正しく行われることをテストする。
  * `TestConvertAnthropicRequestToGemini_BasicText`: 通常テキストリクエストの変換。
  * `TestConvertAnthropicRequestToGemini_WithTools`: ツール定義およびパラメータの大文字化変換。
  * `TestConvertGeminiResponseToAnthropic_Text`: レスポンスのテキストマッピング。
  * `TestConvertGeminiResponseToAnthropic_ToolCall`: `functionCall` からのツール呼び出し変換。
  * `TestConvertGeminiStreamToAnthropic_TextStream`: SSE ストリーム（テキスト）の変換。
  * `TestConvertGeminiStreamToAnthropic_ToolCallStream`: SSE ストリーム（ツール呼び出し）の変換。

### 自動テスト (統合テスト)
* `scripts/process/integration_test.sh` を用いて、Gemini モデルの統合検証を実施する。
  ```bash
  ./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories llm
  ```
