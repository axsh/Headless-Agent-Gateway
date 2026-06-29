# 004-multimodal-api-helpers

> **Source Specification**: [prompts/phases/000-foundation/branches/feat-multi-modal-api/ideas/004-multimodal-api-helpers.md](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-multi-modal-api/prompts/phases/000-foundation/branches/feat-multi-modal-api/ideas/004-multimodal-api-helpers.md)

## Goal Description

Goのクライアントライブラリ（`client/v1`）において、マルチモーダルメッセージを安全かつ簡単に構築・送信できる Messageビルダー構造体（`client.Message`）および目的特化のショートカットメソッド（`SendImageFile`）を追加します。

## User Review Required

None.

## Requirement Traceability

> **Traceability Check**:
> 仕様書(Specification)の要件・決定事項をリストアップし、この計画書のどこで対応するかをマッピングしてください。

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| `NewMessage()` / `Message` ビルダーの提供（`Text`, `ImageBase64`, `ImageBytes`, `ImageFile`, `ImageReader`, `Build` のメソッド提供） | Proposed Changes > `client/v1/content.go` |
| 自動MIMEタイプ判別（`http.DetectContentType`を用いた画像データの正確な判別、画像以外はエラー） | Proposed Changes > `client/v1/content.go` |
| `Session.SendImageFile` メソッドの追加（自動MIMEタイプ検出と送信の統合ショートカット） | Proposed Changes > `client/v1/session.go` |
| 単体テストの追加（`client/v1/content_test.go` にビルダーの正常系・異常系テストの追加） | Proposed Changes > `client/v1/content_test.go` |
| サンプルクライアントの更新（`examples/multimodal-client/main.go` を新API（`SendImageFile`, `NewMessage`）を使用する形に修正） | Proposed Changes > `examples/multimodal-client/main.go` |
| `README.md` のサンプルコード更新 | Proposed Changes > `README.md` |

## Proposed Changes

### client/v1/

#### [NEW] [content_test.go](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-multi-modal-api/client/v1/content_test.go)
*   **Description**: 新しい `Message` ビルダーの正常系および異常系（画像以外のデータが渡された場合や、ファイルが存在しない場合のエラーチェック）を検証する単体テストを定義します。
*   **Technical Design**:
    - `validPNG` データとして、1x1ピクセルの有効なPNG画像のバイトスライスを用意します。
    - `TestMessageBuilder_Success`: `Text`, `ImageBase64`, `ImageBytes` の正常なチェーン構築と `Build` 出力の型・値アサーション。
    - `TestMessageBuilder_Errors`: 不正な画像バイト、存在しないファイルパス、不正なReaderを渡した際のエラーアサーション。
*   **Logic**:
    仕様書から継承した `validPNG` 定義、および `TestMessageBuilder_Success` / `TestMessageBuilder_Errors` テストケースを漏れなく実装します。

#### [MODIFY] [content.go](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-multi-modal-api/client/v1/content.go)
*   **Description**: `Message` 構造体と、それに対するビルダーメソッド群を追加します。
*   **Technical Design**:
    ```go
    type Message struct {
        parts []ContentPart
        err   error
    }

    func NewMessage() *Message
    func (m *Message) Text(text string) *Message
    func (m *Message) ImageBase64(mediaType string, base64Data string) *Message
    func (m *Message) ImageBytes(data []byte) *Message
    func (m *Message) ImageFile(path string) *Message
    func (m *Message) ImageReader(r io.Reader) *Message
    func (m *Message) Build() ([]ContentPart, error)
    ```
*   **Logic**:
    - `ImageBytes` 内で `http.DetectContentType(data)` を呼び出し、得られた MIME タイプが `"image/"` で始まらない場合は `err` フィールドにエラーをセットします。エラーが既に発生している場合は以降のメソッドチェーン呼び出しをスキップします。
    - `ImageFile` 内で `os.ReadFile(path)` を実行し、読み込んだバイトスライスを `ImageBytes` に引き渡します。
    - `ImageReader` 内で `io.ReadAll(r)` を実行し、得られたバイトスライスを `ImageBytes` に引き渡します。
    - `Build` 時に `err` が nil でない場合はエラーを返し、nil の場合は `parts` スライスを返します。

#### [MODIFY] [session.go](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-multi-modal-api/client/v1/session.go)
*   **Description**: `Session` 構造体に `SendImageFile` ショートカットメソッドを追加します。
*   **Technical Design**:
    ```go
    func (s *Session) SendImageFile(ctx context.Context, path string, prompt string) (*Stream, error)
    ```
*   **Logic**:
    - `NewMessage()` を生成し、`Text(prompt)` および `ImageFile(path)` メソッドを呼び出して `Build()` を実行します。
    - `Build()` がエラーを返した場合はそのままエラーを返却し、成功した場合は `s.SendMessage(ctx, parts)` を呼び出します。

### examples/

#### [MODIFY] [main.go](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-multi-modal-api/examples/multimodal-client/main.go)
*   **Description**: 新しい `SendImageFile` メソッドおよび `Message` ビルダーを使用するようにサンプルクライアントのメイン処理を書き換え、ボイラープレートを排除します。
*   **Technical Design**:
    - `*imagePath` が空でない場合は `session.SendImageFile(ctx, *imagePath, *prompt)` を直接呼び出します。
    - `*imagePath` が空の場合は、フォールバックとして `client.NewMessage().Text(*prompt).ImageBase64("image/png", dummyPNG).Build()` を用いてパーツスライスを構築し、`session.SendMessage` に引き渡します。

### project root/

#### [MODIFY] [README.md](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-multi-modal-api/README.md)
*   **Description**: Goクライアントのマルチモーダル送信例として、`SendImageFile` メソッドによる1行送信の例と、`client.NewMessage` ビルダーを使用した複数画像・複数テキスト構築の例をドキュメントに追加します。

## Step-by-Step Implementation Guide

1.  **Add content_test.go**:
    `client/v1/content_test.go` を作成し、単体テストコードを記述します。

2.  **Modify content.go**:
    `client/v1/content.go` を修正し、`Message` ビルダーロジックを実装します。

3.  **Modify session.go**:
    `client/v1/session.go` を修正し、`SendImageFile` メソッドを実装します。

4.  **Verify Unit Tests**:
    `go test -v ./client/v1` を実行し、単体テストが正常に通過することを確認します。

5.  **Modify examples/multimodal-client/main.go**:
    サンプルクライアントの実装を、新しい API に更新します。

6.  **Modify README.md**:
    ドキュメントのサンプル記述を最新化します。

7.  **Run Full Verification**:
    ビルドおよび統合テストを実行して全体を検証します。

## Verification Plan

### Automated Verification

1.  **Unit Tests**:
    `client/v1` パッケージの単体テストを実行します。
    ```bash
    go test -v ./client/v1
    ```

2.  **Build Pipeline**:
    ビルドパイプラインを実行し、サンプルクライアントを含む全バイナリのビルドとユニットテストが通ることを検証します。
    ```bash
    ./scripts/process/build.sh
    ```

3.  **Integration Tests**:
    `examples_build_test` を実行し、新しいクライアントが正常にビルドできるか検証します。
    ```bash
    go test -v -tags=integration -run "TestExamples_MultimodalClient_Builds" ./tests
    ```

4.  **総合判定プロセスの実行**:
    上記の全ビルド・テストが正常に完了したことを確認し、リグレッションが生じていないことをテストスイート全体の結果から最終確認します。

## Documentation

なし
