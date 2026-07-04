# 003-multimodal-client-example

> **Source Specification**: [prompts/phases/000-foundation/branches/feat-multi-modal-api/ideas/003-multimodal-client-example.md](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-multi-modal-api/prompts/phases/000-foundation/branches/feat-multi-modal-api/ideas/003-multimodal-client-example.md)

## Goal Description

Goのクライアントライブラリ（`client/v1`）を用いたマルチモーダル送信（テキストと画像の混在メッセージ）の動作サンプル `examples/multimodal-client` を追加します。さらに `README.md` の記述を更新して、Goクライアントを使用したマルチモーダル送信が機能していることを示す正しいサンプルコードを提示します。

## User Review Required

None.

## Requirement Traceability

> **Traceability Check**:
> 仕様書(Specification)の要件・決定事項をリストアップし、この計画書のどこで対応するかをマッピングしてください。

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| `examples/multimodal-client` の新規作成（v1クライアント使用、引数でサーバーURLと画像パス制御、ダミー画像フォールバック、ストリーム出力） | Proposed Changes > `examples/multimodal-client/main.go` |
| `examples/multimodal-client/go.mod` の新規作成 | Proposed Changes > `examples/multimodal-client/go.mod` |
| `README.md` の更新（Goクライアントのマルチモーダル例のコメントアウト解除およびサンプル追加説明） | Proposed Changes > `README.md` |
| `scripts/process/build.sh` でビルド可能にすること | Proposed Changes > `examples/multimodal-client/go.mod` （ディレクトリ配置により自動認識） |
| `tests/examples_build_test.go` へのビルド検証の追加 | Proposed Changes > `tests/examples_build_test.go` |
| `examples/minimal-client/main.go` からコメントアウトされているマルチモーダルサンプルの削除 | Proposed Changes > `examples/minimal-client/main.go` |

## Proposed Changes

### tests/

#### [MODIFY] [examples_build_test.go](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-multi-modal-api/tests/examples_build_test.go)
*   **Description**: 新しい `multimodal-client` のビルドが成功することを検証する結合テストケースを追加します。
*   **Technical Design**:
    ```go
    // TestExamples_MultimodalClient_Builds verifies that the multimodal-client example
    // compiles without errors.
    func TestExamples_MultimodalClient_Builds(t *testing.T) {
        projectRoot, _ := filepath.Abs("..")
        exampleDir := filepath.Join(projectRoot, "examples", "multimodal-client")

        if _, err := os.Stat(exampleDir); os.IsNotExist(err) {
            t.Fatalf("example directory does not exist: %s", exampleDir)
        }

        cmd := exec.Command("go", "build", "-o", nullOutput(), ".")
        cmd.Dir = exampleDir
        output, err := cmd.CombinedOutput()
        if err != nil {
            t.Fatalf("multimodal-client build failed: %v\n%s", err, output)
        }
    }
    ```

### examples/

#### [MODIFY] [main.go](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-multi-modal-api/examples/minimal-client/main.go)
*   **Description**: 末尾にコメントアウトされて残っているマルチモーダル送信コードのブロックを削除し、テキスト専用のミニマルな例としてクリーンにします。

#### [NEW] [go.mod](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-multi-modal-api/examples/multimodal-client/go.mod)
*   **Description**: 新しいサンプルクライアントのモジュール定義。
*   **Technical Design**:
    ```go
    module github.com/axsh/arctic-tern/examples/multimodal-client

    go 1.26.4

    require github.com/axsh/arctic-tern v0.0.0

    replace github.com/axsh/arctic-tern => ../../
    ```

#### [NEW] [main.go](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-multi-modal-api/examples/multimodal-client/main.go)
*   **Description**: マルチモーダルな Go クライアントのメインプログラム。
*   **Technical Design**:
    - コマンドライン引数をパースし、第1引数を `serverURL`、第2引数を `imagePath`、第3引数を `prompt`、第4引数を `agentName` として受け取れるようにします。
    - デフォルト値:
      - `serverURL`: `"http://localhost:3100"`
      - `imagePath`: `""` (空の場合はダミー画像データにフォールバック)
      - `prompt`: `"Describe what you see in this image."`
      - `agentName`: `"claudecode"`
    - `imagePath` が指定された場合はファイルを読み込んで Base64 にエンコードし、拡張子から `mediaType`（`image/png`, `image/jpeg` 等）を判定します。指定がない場合は、あらかじめ定義された 1x1 のダミー透明PNGデータ（`"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="`）を `image/png` として使用します。
    - `client.New(serverURL)` でクライアントを生成し、`CreateSession` でセッションを作成。
    - `session.SendMessage(ctx, []client.ContentPart{...})` を使ってテキストプロンプトと画像を送信します。
    - レスポンスストリームを `stream.Output(os.Stdout)` で出力します。

*   **Logic**:
    ```go
    package main

    import (
        "context"
        "encoding/base64"
        "flag"
        "fmt"
        "log"
        "os"
        "path/filepath"
        "strings"

        client "github.com/axsh/arctic-tern/client/v1"
    )

    const dummyPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

    func main() {
        serverURL := flag.String("server", "http://localhost:3100", "Tern server URL")
        imagePath := flag.String("image", "", "Path to the image file (optional, uses dummy image if empty)")
        prompt := flag.String("prompt", "Describe what you see in this image.", "Text prompt to send alongside the image")
        agent := flag.String("agent", "claudecode", "Coding agent to use")
        flag.Parse()

        ctx := context.Background()
        c := client.New(*serverURL)

        var mediaType string
        var base64Data string

        if *imagePath != "" {
            data, err := os.ReadFile(*imagePath)
            if err != nil {
                log.Fatalf("failed to read image file: %v", err)
            }
            base64Data = base64.StdEncoding.EncodeToString(data)
            ext := strings.ToLower(filepath.Ext(*imagePath))
            switch ext {
            case ".jpg", ".jpeg":
                mediaType = "image/jpeg"
            case ".gif":
                mediaType = "image/gif"
            case ".webp":
                mediaType = "image/webp"
            default:
                mediaType = "image/png"
            }
        } else {
            mediaType = "image/png"
            base64Data = dummyPNG
            fmt.Println("[INFO] No image file specified. Using a 1x1 dummy PNG as fallback.")
        }

        session, err := c.CreateSession(ctx, client.SessionRequest{
            Agent:   *agent,
            WorkDir: ".",
        })
        if err != nil {
            log.Fatalf("failed to create session: %v", err)
        }
        defer session.Terminate(ctx)
        fmt.Printf("Session created: %s\n", session.ID)

        stream, err := session.SendMessage(ctx, []client.ContentPart{
            {Type: "text", Text: *prompt},
            {Type: "image", Source: &client.ImageSource{
                Type:      "base64",
                MediaType: mediaType,
                Data:      base64Data,
            }},
        })
        if err != nil {
            log.Fatalf("failed to send message: %v", err)
        }

        if err := stream.Output(os.Stdout); err != nil {
            log.Fatalf("stream output error: %v", err)
        }
    }
    ```

### project root/

#### [MODIFY] [README.md](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-multi-modal-api/README.md)
*   **Description**: Goクライアントのサンプルセクションに、今回追加する `multimodal-client` の説明と、正しいマルチモーダルコード例を追加します。
*   **Technical Design**:
    - 「### Client」の後に、「### Multimodal Client ([examples/multimodal-client](examples/multimodal-client/main.go))」セクションを新設。
    - サンプルコードブロックを記載し、コメントアウトされていない `session.SendMessage(ctx, []client.ContentPart{...})` によるマルチモーダルメッセージ送信例を記述します。
    - `### 6. Or use the Go client library` の部分の例も最新の形式に統一します。

## Step-by-Step Implementation Guide

1.  **Add test case**:
    `tests/examples_build_test.go` に新しいテスト `TestExamples_MultimodalClient_Builds` を追加します。

2.  **Create examples/multimodal-client/go.mod**:
    `examples/multimodal-client/go.mod` を新規作成します。

3.  **Create examples/multimodal-client/main.go**:
    `examples/multimodal-client/main.go` を新規作成します。

4.  **Update README.md**:
    `README.md` のサンプルコード箇所とセクションを更新します。

5.  **Run verification**:
    ビルドおよびテストスクリプトを実行し、正常に終了することを確認します。

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ビルドスクリプトを実行し、`examples/multimodal-client` が正常にビルドされて `bin/multimodal-client` が出力されること、および他のテストが正常に通過することを確認します。
    ```bash
    ./scripts/process/build.sh
    ```

2.  **Integration Tests**:
    `tests/examples_build_test.go` のテストが通過することを確認します。
    ```bash
    ./scripts/process/integration_test.sh --specify "TestExamples_MultimodalClient_Builds"
    ```

3.  **総合判定プロセスの実行**:
    上記のビルドおよび特定のテストが正常に完了したことを確認した後、変更したコードについてテスト全体のデグレーションがないか、すべての自動テスト結果を確認します。

## Documentation

なし
