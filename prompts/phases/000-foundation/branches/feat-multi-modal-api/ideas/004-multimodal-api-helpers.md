# GoクライアントのマルチモーダルAPIヘルパー追加仕様 (ビルダー & 特化APIハイブリッド設計)

Goのクライアントライブラリ（`client/v1`）において、マルチモーダルメッセージの構築を簡単かつ安全に行うためのビルダーパターン（Message Builder）および目的特化型のショートカットメソッドを提供し、クライアント実装の利便性と安全性を大幅に向上させます。

## 背景

現在、マルチモーダルメッセージを送信する際、クライアント側で `client.ContentPart` や `client.ImageSource` といった内部構造体を直接記述し、マジック文字列（`Type: "text"`, `Type: "image"`, `Type: "base64"` など）を手動で設定する必要があります。
この設計は以下のような課題を抱えています：
1. 構造体のメンバや構造を正確に把握していないとコードが書けない（学習コストが高い）。
2. `Type` などの文字列に誤植があってもコンパイルエラーにならず、実行時エラーになるまで気づきにくい（静的型付け言語のメリットが活かせない）。
3. 多くのクライアントコードで同様の冗長な初期化コードが重複して書かれる。
4. ファイル読み込み、Base64エンコード、および拡張子からのMIMEタイプ判定といったボイラープレートコードがクライアント側に発生する。

## 要件

1. **ビルダーパターン (`client.Message` / `client.NewMessage()`) の提供 (アプローチC)**
   - 流れるようなインターフェース（Fluent Interface）を用いて、スライスを意識せずに動的にメッセージを組み立てるビルダーを提供します。
   - 内部で `http.DetectContentType` を使用し、コンテンツ（ヘッダ）から正確なメディアタイプ（`image/png`, `image/jpeg` など）を自動検出します。画像ではないデータを追加しようとした場合は、ビルドエラーになります。
   - 提供するビルダーのインターフェース：
     - `NewMessage() *Message`: ビルダーの新規作成。
     - `(m *Message) Text(text string) *Message`: テキストコンテンツの追加。
     - `(m *Message) ImageBase64(mediaType string, base64Data string) *Message`: 明示的なBase64画像データの追加。
     - `(m *Message) ImageBytes(data []byte) *Message`: バイト配列画像データの追加（自動メディアタイプ判定）。
     - `(m *Message) ImageFile(path string) *Message`: ローカル画像ファイルの追加（自動読み込み＋自動メディアタイプ判定）。
     - `(m *Message) ImageReader(r io.Reader) *Message`: ストリーム画像データの追加（自動読み込み＋自動メディアタイプ判定）。
     - `(m *Message) Build() ([]ContentPart, error)`: 構築結果（ContentPartのスライス）とエラーを返却。

2. **ショートカット便利メソッド (`SendImageFile`) の提供 (アプローチB)**
   - 最も頻出する「画像ファイル1枚とテキストプロンプトを送信する」というユースケースに特化した、1行で呼べる高レベルなヘルパーメソッドを `Session` 構造体に追加します。
   - `SendImageFile(ctx context.Context, path string, prompt string) (*Stream, error)`: 指定されたファイルを読み込んでメディアタイプを自動判別し、プロンプトと共に送信します。

3. **既存サンプルクライアントとドキュメントのアップデート**
   - `examples/multimodal-client/main.go` を修正し、`SendImageFile` (または `NewMessage` ビルダー) を使用する形に書き換え、クライアント側での画像読み込みやMIMEタイプ判別のボイラープレートを排除します。
   - `README.md` のサンプルコード記述で `SendImageFile` ヘルパーメソッドを使用する形に更新します。

4. **単体テストの追加**
   - `client/v1` パッケージに `content_test.go` を追加し、ビルダーの各メソッドの挙動とエラーハンドリングが正常に動作することを確認します。

## 実現方針

### client/v1/content.go [MODIFY]
```go
package v1

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// Message builds a list of ContentPart for multimodal messages.
type Message struct {
	parts []ContentPart
	err   error
}

// NewMessage creates a new Message builder.
func NewMessage() *Message {
	return &Message{}
}

// Text adds a text content part to the message.
func (m *Message) Text(text string) *Message {
	if m.err != nil {
		return m
	}
	m.parts = append(m.parts, ContentPart{
		Type: "text",
		Text: text,
	})
	return m
}

// ImageBase64 adds an image content part from a base64 encoded string.
func (m *Message) ImageBase64(mediaType string, base64Data string) *Message {
	if m.err != nil {
		return m
	}
	m.parts = append(m.parts, ContentPart{
		Type: "image",
		Source: &ImageSource{
			Type:      "base64",
			MediaType: mediaType,
			Data:      base64Data,
		},
	})
	return m
}

// ImageBytes adds an image content part from raw binary data, detecting media type automatically.
func (m *Message) ImageBytes(data []byte) *Message {
	if m.err != nil {
		return m
	}
	mediaType := http.DetectContentType(data)
	if !strings.HasPrefix(mediaType, "image/") {
		m.err = fmt.Errorf("inferred content type %q is not a supported image type", mediaType)
		return m
	}
	base64Data := base64.StdEncoding.EncodeToString(data)
	return m.ImageBase64(mediaType, base64Data)
}

// ImageFile reads an image file from path and adds it as an image content part.
// The media type is automatically detected from the file content.
func (m *Message) ImageFile(path string) *Message {
	if m.err != nil {
		return m
	}
	data, err := os.ReadFile(path)
	if err != nil {
		m.err = fmt.Errorf("read image file: %w", err)
		return m
	}
	return m.ImageBytes(data)
}

// ImageReader reads image data from r and adds it as an image content part.
// The media type is automatically detected from the content.
func (m *Message) ImageReader(r io.Reader) *Message {
	if m.err != nil {
		return m
	}
	data, err := io.ReadAll(r)
	if err != nil {
		m.err = fmt.Errorf("read image data: %w", err)
		return m
	}
	return m.ImageBytes(data)
}

// Build returns the compiled list of ContentPart, or an error if any operation failed.
func (m *Message) Build() ([]ContentPart, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.parts, nil
}
```

### client/v1/session.go [MODIFY]
目的特化型の `SendImageFile` メソッドを `Session` に追加します。
```go
// SendImageFile is a convenience method that reads an image file from path,
// automatically detects its media type, and sends it alongside a text prompt.
func (s *Session) SendImageFile(ctx context.Context, path string, prompt string) (*Stream, error) {
	parts, err := NewMessage().
		Text(prompt).
		ImageFile(path).
		Build()
	if err != nil {
		return nil, err
	}
	return s.SendMessage(ctx, parts)
}
```

### client/v1/content_test.go [NEW]
ビルダーのテストを追加。
```go
package v1_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	client "github.com/axsh/arctic-tern/client/v1"
)

// 1x1 red transparent PNG image (valid image data)
var validPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89, 0x00, 0x00, 0x00,
	0x0d, 0x49, 0x44, 0x41, 0x54, 0x78, 0x01, 0x63, 0xfc, 0xcf, 0xc0, 0x50,
	0x0f, 0x00, 0x04, 0x85, 0x01, 0x80, 0x84, 0xa9, 0x8c, 0x21, 0x00, 0x00,
	0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
}

func TestMessageBuilder_Success(t *testing.T) {
	parts, err := client.NewMessage().
		Text("hello").
		ImageBase64("image/png", "base64data").
		ImageBytes(validPNG).
		Build()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(parts) != 3 {
		t.Errorf("expected 3 parts, got %d", len(parts))
	}

	if parts[0].Type != "text" || parts[0].Text != "hello" {
		t.Errorf("invalid text part: %+v", parts[0])
	}

	if parts[1].Type != "image" || parts[1].Source.MediaType != "image/png" || parts[1].Source.Data != "base64data" {
		t.Errorf("invalid base64 image part: %+v", parts[1])
	}

	if parts[2].Type != "image" || parts[2].Source.MediaType != "image/png" {
		t.Errorf("invalid bytes image part: %+v", parts[2])
	}
}

func TestMessageBuilder_Errors(t *testing.T) {
	t.Run("invalid image bytes", func(t *testing.T) {
		_, err := client.NewMessage().
			ImageBytes([]byte("not an image")).
			Build()
		if err == nil {
			t.Error("expected error for invalid image bytes, got nil")
		}
	})

	t.Run("missing file", func(t *testing.T) {
		_, err := client.NewMessage().
			ImageFile("non_existent_file.png").
			Build()
		if err == nil {
			t.Error("expected error for missing file, got nil")
		}
	})

	t.Run("invalid reader data", func(t *testing.T) {
		buf := bytes.NewReader([]byte("not an image"))
		_, err := client.NewMessage().
			ImageReader(buf).
			Build()
		if err == nil {
			t.Error("expected error for invalid reader data, got nil")
		}
	})
}
```

### examples/multimodal-client/main.go [MODIFY]
`session.SendMessage` 呼び出し部分を `SendImageFile` または `NewMessage` ビルダーを使って以下のように簡潔にします。
```go
	var stream *client.Stream
	var err error

	if *imagePath != "" {
		// Use specialized helper SendImageFile (Approach B)
		stream, err = session.SendImageFile(ctx, *imagePath, *prompt)
	} else {
		// Use Message Builder (Approach C) to send a fallback query with base64 data
		parts, buildErr := client.NewMessage().
			Text(*prompt).
			ImageBase64("image/png", dummyPNG).
			Build()
		if buildErr != nil {
			log.Fatalf("failed to build message: %v", buildErr)
		}
		stream, err = session.SendMessage(ctx, parts)
	}

	if err != nil {
		log.Fatalf("failed to send message: %v", err)
	}
```

### README.md [MODIFY]
Goクライアントのマルチモーダル送信例でヘルパー関数を使用するようにコードブロックを更新。
```go
// 1. For simple image + text query:
stream, _ := session.SendImageFile(ctx, "screenshot.png", "Describe this image:")
stream.Output(os.Stdout)

// 2. Or build complex multimodal messages:
parts, _ := client.NewMessage().
    Text("Analyze these images:").
    ImageFile("image1.png").
    ImageFile("image2.png").
    Build()

stream, _ = session.SendMessage(ctx, parts)
stream.Output(os.Stdout)
```

## 検証シナリオ

1. `client/v1` の単体テストを実行し、作成した `TestMessageBuilder_Success` および `TestMessageBuilder_Errors` が正常にパスすること。
2. `examples/multimodal-client` をビルドし、正常にコンパイルできること。

## テスト項目

以下のコマンドを実行してビルドおよびテストを検証します。
```bash
go test -v ./client/v1
./scripts/process/build.sh
```
