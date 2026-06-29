# Goクライアントのマルチモーダルサンプル追加仕様

Goのクライアントライブラリ（`client/v1`）を用いたマルチモーダル送信の動作サンプルを `examples/` ディレクトリに追加し、`README.md` に正しい使用例を提示します。

## 背景

現在、`README.md` におけるマルチモーダル機能の使用例は Web API（`curl` コマンド）のみが有効な例として記載されています。Goクライアントライブラリのサンプルコードはコメントアウトされており、また `examples/` ディレクトリ内にもマルチモーダル送信を実際に行うサンプルクライアントが存在しないため、ライブラリ経由でのマルチモーダル送信の動作や実装方法がわかりにくい状態になっています。

## 要件

1. **`examples/multimodal-client` の新規作成**
   - 新しいサンプルクライアント `examples/multimodal-client/main.go` を作成します。
   - `"github.com/axsh/arctic-tern/client/v1"` をインポートして使用します。
   - コマンドライン引数から接続先サーバーURLと画像ファイルのパスを受け取れるようにします。
   - 指定された画像ファイルを読み込み、Base64エンコードを行って、テキストプロンプト（例：「この画像について説明してください」）と一緒に `session.SendMessage` で送信します。
   - 画像ファイルが指定されていない、あるいは存在しない場合に備え、埋め込みのダミー画像データ（Base64形式）またはわかりやすいメッセージでエラー終了もしくはフォールバックする仕組みを設けます。
   - レスポンスは標準出力（`os.Stdout`）にストリーミング出力します。

2. **`README.md` の更新**
   - クライアントの紹介セクションに `examples/multimodal-client` へのリンクと、コメントアウトされていない Goクライアントのマルチモーダル呼び出し例を追加します。

3. **ビルドスクリプトと検証テストの更新**
   - `scripts/process/build.sh` で `examples/multimodal-client` がビルドされるように対応します。
   - `tests/examples_build_test.go` に新しいクライアントのビルド検証を追加します。

4. **`examples/minimal-client/main.go` の整理**
   - 新規にマルチモーダルクライアントを作成するため、既存の `minimal-client` に残されているコメントアウトされたマルチモーダルのサンプルコードを削除し、コードをクリーンにします。

## 実現方針

### examples/minimal-client/main.go [MODIFY]
- 末尾のコメントアウトされたマルチモーダルサンプル部分を削除します。

### examples/multimodal-client/go.mod [NEW]
```go
module github.com/axsh/arctic-tern/examples/multimodal-client

go 1.26.3
```

### examples/multimodal-client/main.go [NEW]
- サーバーURL、画像パスを引数で制御。
- `os.ReadFile` で画像を読み込み、`base64.StdEncoding.EncodeToString` でエンコード。
- `client.ContentPart` を構築し、`SendMessage` を呼び出す。

### tests/examples_build_test.go [MODIFY]
- ビルド対象のサンプルリストに `multimodal-client` を追加。

## 検証シナリオ

1. `./scripts/process/build.sh` を実行し、`bin/multimodal-client` が正常にビルドされること。
2. `tests/examples_build_test.go` を含む結合テストが正常に通過すること。

## テスト項目

以下のコマンドを実行してビルドおよびテストを検証します。
```bash
./scripts/process/build.sh
go test -v -run "TestExamplesBuild" ./tests
```
