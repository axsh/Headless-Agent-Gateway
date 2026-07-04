# [FIX] Codex CLI 401 Unauthorized (dummy-key)

## 現状の分析
ユーザーから報告された `Incorrect API key provided: dummy-key` というエラーは、Codex CLI が LLM Gateway (Bifrost) をバイパスして OpenAI 本家に直接アクセスしようとし、さらに有効な API キーが設定されていないために発生しています。

`dummy-key` という文字列は Codex CLI 内部のデフォルト値である可能性が高く、以下のいずれかが原因と考えられます：
1. `-c model_providers.gateway.base_url` などの設定オーバーライドが、Windows の引数クォーティングの問題により正しく認識されていない。
2. `GatewayURL` が空になっており、デフォルトの OpenAI 接続設定が使用されている。

## 提案する修正
1. **診断用ログの強化**: `OPENAI_API_KEY` の先頭数文字をログに出力し、実際に何が渡されているか確認します（実施済み）。
2. **プレースホルダーの変更**: `not-needed` を `tern-internal-key-placeholder` に変更し、エラーメッセージの変化を確認します（実施済み）。
3. **設定の確実な反映**: Windows において引数オーバーライド (`-c`) は不安定な場合があるため、`config.toml` を作成して `--config` フラグで明示的に指定するように変更します。

## ユーザーへの確認事項
- サーバー起動時のログに `starting codex CLI process` という行が出力されているはずです。その中の `cmd="..."` と `env=[...]` の内容を共有いただけますでしょうか。
- 特に `OPENAI_API_KEY` の値が `tern-internal-key-placeholder` になっているか確認したいです。
