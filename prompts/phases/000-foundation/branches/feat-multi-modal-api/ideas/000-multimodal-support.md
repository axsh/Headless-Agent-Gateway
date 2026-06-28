# 要求仕様書: cawa & llmgp マルチモーダル対応

## 1. 背景 (Background)

Coding Agent Web API (cawa) および LLM Gateway Proxy (llmgp) は現在、テキストベースのプロンプトおよびソースコードのみを処理する設計となっています。しかし、現代のコーディングエージェント（Claude Code や Codex）はマルチモーダル（画像やドキュメントなど）をネイティブにサポートしており、UIのバグ修正や画面の仕様書からのコード生成など、画像をインプットとしたタスクの需要が高まっています。

本変更では、cawaおよびllmgpに共通のマルチモーダル対応API設計を導入し、エージェント環境に応じた適切なデータ処理およびエラー伝播を可能とします。

---

## 2. 要件 (Requirements)

### 2.1 APIのバージョニングとデータ構造
1. **v1 API（後方互換）の維持**:
   - `POST /api/v1/sessions/:id/messages` エンドポイントは維持します。
   - リクエストボディは `{"message": "string"}` のままとし、内部で自動的にテキストのみのブロック配列 `[]ContentPart` に変換してエージェントコアに渡します。
2. **v2 API（マルチモーダル新規提供）の追加**:
   - `POST /api/v2/sessions/:id/messages` エンドポイントを新規提供します。
   - リクエストボディは `{"content": []ContentPart}` とし、テキストや画像を明示的に指定した配列を受け取ります。
3. **コンテンツブロックのデータ構造**:
   - コンテンツブロックは `type: "text"` または `type: "image"` をサポートします。
   - `type: "image"` では、`source` (Type: base64, MediaType, Data) をサポートします。

### 2.2 エージェント別のデータ処理・エラーハンドリング
1. **Wayfinder (非サポート)**:
   - マルチモーダル入力（画像ブロック）が `Send` メソッドに渡された場合、即座にエラー `ErrMultimodalNotSupported` を返します。
   - cawaサーバーはこれを検知して、クライアントに `501 Not Implemented` レスポンス（メッセージ "multimodal inputs are not supported by the wayfinder agent"）を返します。
2. **Claude Code CLI & Codex CLI (サポート)**:
   - cawaサーバーは受信した画像Base64データを一時ディレクトリ (`tmp/multimodal/`) に画像ファイルとしてデコード・保存します。
   - 保存後、プロンプトテキストを書き換え、そのローカル一時ファイルへの参照（パス）を埋め込んでCLIに渡します。
     - 例: `[Attached image: tmp/multimodal/image_xyz.png]`
   - エージェントは自身の実行ディレクトリから当該画像ファイルを読み取り、llmgpに対してAPIリクエストとして送信します。

### 2.3 LLM Gateway Proxy (llmgp) の対応
1. **OpenAI互換API (Codex等)**:
   - `POST /v1/responses` に対するリクエストをパースし、Bifrostの `OpenAIResponsesRequest` を介して透過的にマルチモーダルデータをマッピングします。
2. **Anthropic互換API (Claude Code等)**:
   - `POST /v1/messages` に対するリクエストの `content` 内の `image` ブロックをパースできるように `ContentBlock` 構造体を拡張します。
   - `convertMessage` ロジックにおいて、`image` ブロックをBifrostの `ResponsesInputMessageContentBlockTypeImage` (および `image_url` に Base64 Data URLを設定) に適切にマッピングして転送します。

---

## 3. 実装方針 (Implementation Approach)

### 3.1 ディレクトリ構成と影響範囲

#### cawa (Coding Agent Web API)
- [MODIFY] `shared/libs/go/codingagent/interface.go` (Sendメソッドの引数拡張)
- [MODIFY] `shared/libs/go/agentservice/handler.go` (v2エンドポイント追加、v1互換ハンドリング、一時ファイルデコード保存ロジック)
- [MODIFY] `shared/libs/go/wayfinder/adapter.go` (Sendにおけるマルチモーダル非サポート判定エラー)
- [MODIFY] `shared/libs/go/codingagent/claudecode/adapter.go` (Send対応および一時ファイル参照プロンプト埋め込み)
- [MODIFY] `shared/libs/go/codingagent/codex/adapter.go` (同上)

#### llmgp (LLM Gateway Proxy)
- [MODIFY] `shared/libs/go/llmgateway/anthropic/types.go` (`ContentBlock` と `ImageSource` の拡張)
- [MODIFY] `shared/libs/go/llmgateway/anthropic/convert.go` (`convertMessage` の `image` ブロックマッピング実装)

### 3.2 詳細型定義 (Go)

```go
// shared/libs/go/codingagent/types.go (または定義ファイル)
type ContentPart struct {
	Type   string       `json:"type"`             // "text" または "image"
	Text   string       `json:"text,omitempty"`   // Typeが"text"の場合
	Source *ImageSource `json:"source,omitempty"` // Typeが"image"の場合
}

type ImageSource struct {
	Type      string `json:"type"`       // "base64" (将来的に "file" なども可)
	MediaType string `json:"media_type"` // MIMEタイプ (例: "image/png", "image/jpeg")
	Data      string `json:"data"`       // Base64文字列
}
```

---

## 4. 検証シナリオ (Verification Scenarios)

### シナリオ1: 既存クライアントによる v1 プレーンテキスト送信 (正常系)
1. ユーザー（v1クライアント）が `POST /api/v1/sessions/:id/messages` に対し `{"message": "Hello"}` を送信する。
2. cawaは内部的にテキストブロック配列に変換してエージェントに渡し、テキスト返答が返ることを確認する。

### シナリオ2: v2 API による画像付きマルチモーダル送信とエージェント連携 (正常系)
1. ユーザーが `POST /api/v2/sessions/:id/messages` に対し、画像Base64を含むリクエストを送信する。
2. cawaサーバーの `tmp/multimodal/` に画像がデコード保存される。
3. エージェントCLI（Claude Code / Codex）が起動され、引数プロンプト内にその一時ファイル参照が含まれることを確認する。

### シナリオ3: Wayfinder エージェントに対するマルチモーダル送信 (異常系)
1. ユーザーが Wayfinder セッションに対し、画像ブロックを含む `POST /api/v2/sessions/:id/messages` リクエストを送信する。
2. cawaサーバーが `501 Not Implemented` レスポンスとエラーメッセージを返却することを確認する。

---

## 5. テスト項目 (Testing for the Requirements)

要件を満たすことを多角的に確認するため、以下の多層的な自動テスト計画を実施します。

### 5.1 単体テスト (Unit Tests)

1. **`llmgateway/anthropic` コンバージョン検証**:
   - `convert_test.go` にテストケースを追加。
   - `FullRequest` 内の `ContentBlock` に `image` タイプ (MIME: `image/png`, Base64データ) が含まれる場合の正常なマッピングテスト。
   - 生成された `BifrostResponsesRequest` の `ContentBlocks` 内に `Type: input_image` が設定され、かつ `ImageURL` に `data:image/png;base64,` から始まるData URLが正しく組み立てられていることを確認する。
   - データが空、MIMEタイプが不足している等の不正な形式に対して適切なパースエラーを返すことを検証。

2. **エージェント側の入力検証**:
   - Wayfinderの `Send` 実装に対するテスト。`ContentPart` に `image` が渡された際に、専用エラー `ErrMultimodalNotSupported` が返ることを確認。
   - Claude Code / Codex の `Send` 実装に対するテスト。`ContentPart` に `image` が渡された際、画像が正常に一時ファイルにデコード保存され、プロンプト書き換えが発生することを確認。

### 5.2 結合テスト (Integration Tests)

`scripts/process/integration_test.sh` を用いた結合テストを整備します。テストカテゴリには `common` および `llm` を使用します。

1. **cawa Web APIのテスト (`agentservice` 結合検証)**:
   - 疑似的なエージェント（Mock Agent）を登録し、`v1` および `v2` エンドポイントの振る舞いを検証するテストケースを追加。
   - `POST /api/v1/sessions/:id/messages` にテキストを送り、正常なストリーミングイベントが返却されること（後方互換）。
   - `POST /api/v2/sessions/:id/messages` に `type: "image"` を含むリクエストを送り、サーバー側の `tmp/multimodal/` ディレクトリに該当画像が正常にファイル出力されていること。

2. **エラー応答テスト**:
   - `TestWayfinderMultimodalRejection` テストケースを作成。
   - Wayfinderエージェントのセッションに対し、画像付きの v2 リクエストを送信し、APIレスポンスのHTTPステータスが `501 Not Implemented` であり、レスポンスボディにエラー内容が含まれていることを検証する。

3. **llmgp プロキシと Bifrost 連携のテスト (`llmgateway` 結合検証)**:
   - `llmgateway/proxy_test.go` にマルチモーダル対応テストを追加。
   - Anthropic互換ハンドラ (`POST /v1/messages`) に画像を含むペイロードを送信し、Bifrostを通じてモックバックエンドに転送されること。
   - OpenAI互換ハンドラ (`POST /v1/responses`) に画像 `image_url` を含むペイロードを送信し、適切に転送されること。

### 5.3 異常系・限界値テスト (Edge Case / Stress Tests)

1. **データサイズ制限の検証**:
   - llmgp および cawa の `MaxRequestBodyBytes` 設定に基づき、制限値を超える巨大な画像データ（例: 20MBの画像）を送信した際、アップストリームへ流れる前に `413 Request Entity Too Large` エラーを返却することを確認する。
2. **不正なBase64エンコーディング検証**:
   - 壊れたBase64データが送られてきた場合、サーバー側でデコードエラーとなり、クライアントへ `400 Bad Request` を返すことを確認する。
3. **一時ファイル書き込み失敗のハンドリング**:
   - ディスクフルや権限不足などにより `tmp/multimodal/` へのファイル保存が失敗した場合に、内部エラー (500 Internal Server Error) を返し、システムがクラッシュしないことを確認する。
4. **一時ファイルクリーンアップ検証**:
   - セッション終了時 (Session Close / Terminate 時) に、該当セッションに関連して作成された `tmp/multimodal/` 内の一時画像ファイルが漏れなく自動削除されることを確認する。

### 実行コマンド例

```bash
# 全体のビルドと単体テストの実行
./scripts/process/build.sh

# cawa と llmgp 関連の統合テストの実行
xvfb-run -a ./scripts/process/integration_test.sh --specify tests/agentservice_integration_test.go
xvfb-run -a ./scripts/process/integration_test.sh --specify tests/llmgateway_integration_test.go
```
(※ Linux環境での実行を想定し、統合テストスクリプトは `xvfb-run -a` でラップして実行します)
