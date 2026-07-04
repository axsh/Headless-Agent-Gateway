# 000-Persistent-Multimodal-Sessions

## 背景 (Background)
現在のマルチモーダル（画像）対応実装では、画像データは `tmp/multimodal/` 配下に一時ファイルとして保存され、各リクエスト（1ターン）の終了時に削除される設計になっています。
このため、会話の2ターン目以降で過去の画像について質問しても、履歴に含まれる画像パスが既に無効（ファイル削除済み）であるため、エージェントが画像を認識できず、文脈が途切れる問題が発生しています。
本仕様では、画像をセッションディレクトリ内に永続化し、複数ターンにわたる会話でも画像を継続的に認識できるようにします。

## 要件 (Requirements)
1.  **画像の永続保存**: 画像の保存先を `tmp/` からセッション固有の永続ディレクトリ（`SessionDir`）に変更する。
2.  **履歴における画像保持**: セッション履歴（`session.Message`）において、テキストだけでなく画像データ（または安定した参照）を構造的に保持できるようにする。
3.  **文脈の再現**: 過去の履歴をエージェント（CLIアダプタ）に送信する際、履歴に含まれる画像データが正しくCLIに渡されるようにする。
4.  **クリーンアップの制御**: 画像ファイルの削除は、リクエスト終了時ではなく、セッションが明示的に削除されるタイミングまで延期する。
5.  **後方互換性**: 既存のテキストのみのセッション履歴やAPIとの互換性を維持する。

## 実現方針 (Implementation Approach)

### 1. セッション履歴の拡張
`shared/libs/go/wayfinder/session/session_state.go` の `Message` 構造体を拡張し、マルチモーダルなコンテンツを扱えるようにします。

```go
type Message struct {
    Role       string           `json:"role"`
    Content    string           `json:"content"`       // 互換性のためのテキスト
    ContentParts []ContentPart   `json:"content_parts,omitempty"` // 新設: 構造化コンテンツ
    // ... 既存フィールド ...
}

type ContentPart struct {
    Type string `json:"type"` // "text", "image"
    Text string `json:"text,omitempty"`
    Image *ImageMetadata `json:"image,omitempty"`
}

type ImageMetadata struct {
    Path      string `json:"path"`       // セッションディレクトリ相対パス
    MediaType string `json:"media_type"`
}
```

### 2. 画像保存ロジックの変更
`shared/libs/go/agentservice/multimodal.go` を修正し、`SessionDir` 内に画像を保存するようにします。
- 保存先: `[SessionDir]/multimodal/[hash].[ext]`
- `SessionDir` が指定されていない場合は、現在の `tmp/` 方式をフォールバックとして利用。

### 3. ハンドラの修正
`shared/libs/go/agentservice/handler_v2.go` を修正します。
- メッセージ送信時に画像を `SessionDir` に保存し、`Message` オブジェクトを構築して履歴に保存する。
- 毎ターンの `CleanupMultimodalFiles` 呼び出しを廃止。

### 4. アダプタ（Codex / Claude Code）の対応
履歴をCLIに渡す際、`Message.ContentParts` を走査し、画像が含まれる場合はCLIの引数（`-p` や `exec` 引数）として適切にパスを渡す、または環境変数を通じてコンテキストを注入します。

## 検証シナリオ (Verification Scenarios)

1.  **初回ターン**: 画像（図）を添付して「この図は何ですか？」と質問する。
    - 期待値: エージェントが図の内容を正しく説明する。
2.  **継続ターン**: （画像を添付せずに）「その図の右側にある詳細を教えてください」と質問する。
    - 期待値: エージェントが過去の図を認識し、詳細を回答する。
3.  **再起動後の継続**: 一度セッションを終了（HTTP接続切断）し、同じ `session_id` で「さっきの図について...」と質問する。
    - 期待値: セッションディレクトリから画像がロードされ、エージェントが認識できる。

## テスト項目 (Testing for the Requirements)

### 自動テスト (Automated Tests)

1.  **単体テスト**: `session_state_test.go` にマルチモーダルなメッセージのシリアライズ・デシリアライズテストを追加。
2.  **ハンドラテスト**: `handler_v2_test.go` において、同一セッションで2回連続でリクエストを送り、2回目でも画像が有効であることを確認。
3.  **結合テスト**: `tests/multimodal_integration_test.go` に以下のケースを追加。
    - `TestMultimodalSessionPersistence`: セッションを跨いで画像が保持され、クリーンアップが即座に走らないことを確認。
    - 実行コマンド:
      ```bash
      ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestMultimodalSessionPersistence"
      ```

### 影響範囲確認
- `common`, `llm` カテゴリのテストを実行し、既存のテキストメッセージ処理に影響がないことを確認。
  ```bash
  ./scripts/process/integration_test.sh --categories common,llm
  ```
