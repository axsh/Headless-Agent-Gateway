# 000-Multimodal-Support

> **Source Specification**: [000-multimodal-support.md](file://prompts/phases/000-foundation/branches/feat-multi-modal-api/ideas/000-multimodal-support.md)

## Goal Description

cawa (Coding Agent Web API) と llmgp (LLM Gateway Proxy) にマルチモーダル（画像）入力を導入する。APIバージョニング（v1/v2）により後方互換性を維持しつつ、v2で新しいコンテンツブロック配列ベースのAPIを提供する。エージェント側では、Claude Code/Codex CLI は画像を一時ファイルに保存してプロンプトに参照パスを埋め込む方式でマルチモーダルを処理し、Wayfinderは非対応として 501 Not Implemented を返す。llmgp側では Anthropic互換APIの画像ブロックをBifrostにマッピングする変換ロジックを追加する。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R2.1-1: v1 API 後方互換維持 | Proposed Changes > agentservice > service.go (v2ルート追加、v1維持) |
| R2.1-2: v2 API マルチモーダル新規提供 | Proposed Changes > agentservice > handler_v2.go |
| R2.1-3: ContentPart / ImageSource 型定義 | Proposed Changes > codingagent > content.go |
| R2.2-1: Wayfinder 501 Not Implemented | Proposed Changes > wayfinder > adapter.go |
| R2.2-2: Claude Code / Codex 一時ファイル保存+プロンプト書き換え | Proposed Changes > agentservice > multimodal.go |
| R2.3-1: OpenAI互換 透過マッピング | 追加実装不要 (既存Bifrostパスで対応) |
| R2.3-2: Anthropic互換 画像ブロック変換 | Proposed Changes > llmgateway/anthropic > types.go, convert.go |
| R5.1: 単体テスト | Proposed Changes > *_test.go (各セクション) |
| R5.2: 結合テスト | Proposed Changes > tests/ |
| R5.3: 異常系・限界値テスト | Proposed Changes > handler_v2_test.go, content_test.go |

---

## Proposed Changes

### codingagent (型定義・インターフェース層)

#### [NEW] [content_test.go](file://shared/libs/go/codingagent/content_test.go)
*   **Description**: ContentPart / ImageSource のバリデーション、ヘルパー関数の単体テスト。
*   **テストケース**:
    *   `TestExtractTextFromContent`: テキストのみの `[]ContentPart` からプレーンテキスト文字列を抽出。
    *   `TestExtractTextFromContent_Mixed`: テキスト+画像の混在配列からテキスト部分のみを抽出。
    *   `TestHasNonTextContent`: 画像ブロック存在時に `true`、テキストのみ時に `false`。
    *   `TestContentPartValidation`: 不正な Type 値、空の Source、空の Data に対する検証。

#### [NEW] [content.go](file://shared/libs/go/codingagent/content.go)
*   **Description**: マルチモーダルコンテンツのデータ型定義とユーティリティ関数。
*   **Technical Design**:
    ```go
    package codingagent

    import "errors"

    // ErrMultimodalNotSupported is returned by agents that do not support
    // non-text content (e.g., images).
    var ErrMultimodalNotSupported = errors.New("multimodal inputs are not supported by this agent")

    // ContentPart represents a single block in a multimodal message.
    type ContentPart struct {
        Type   string       `json:"type"`             // "text" or "image"
        Text   string       `json:"text,omitempty"`   // type="text"
        Source *ImageSource `json:"source,omitempty"` // type="image"
    }

    // ImageSource holds image data for a ContentPart of type "image".
    type ImageSource struct {
        Type      string `json:"type"`       // "base64"
        MediaType string `json:"media_type"` // MIME type (e.g., "image/png")
        Data      string `json:"data"`       // Base64-encoded data
    }

    // ExtractText concatenates all text blocks from a []ContentPart.
    // Non-text blocks are ignored. Returns empty string if no text blocks exist.
    func ExtractText(parts []ContentPart) string {
        var sb strings.Builder
        for _, p := range parts {
            if p.Type == "text" && p.Text != "" {
                sb.WriteString(p.Text)
            }
        }
        return sb.String()
    }

    // HasNonTextContent returns true if any ContentPart has a type other than "text".
    func HasNonTextContent(parts []ContentPart) bool {
        for _, p := range parts {
            if p.Type != "text" {
                return true
            }
        }
        return false
    }

    // TextOnlyContent creates a []ContentPart from a plain text string.
    // Used by v1 handler to wrap legacy "message" field.
    func TextOnlyContent(text string) []ContentPart {
        return []ContentPart{{Type: "text", Text: text}}
    }
    ```

---

### agentservice (v2 ハンドラ・マルチモーダル処理)

#### [NEW] [multimodal_test.go](file://shared/libs/go/agentservice/multimodal_test.go)
*   **Description**: 一時ファイルデコード保存と画像参照プロンプト書き換えロジックの単体テスト。
*   **テストケース**:
    *   `TestSaveImageToTempFile_PNG`: 有効なPNG Base64データを保存し、ファイルが `tmp/multimodal/` 配下に作成されることを確認。保存されたファイルのバイト列がデコード結果と一致することを確認。
    *   `TestSaveImageToTempFile_JPEG`: JPEG MIMEタイプで保存し、拡張子が `.jpg` になることを確認。
    *   `TestSaveImageToTempFile_InvalidBase64`: 壊れたBase64に対してエラーが返ることを確認。
    *   `TestSaveImageToTempFile_EmptyData`: 空の Data に対してエラーが返ることを確認。
    *   `TestBuildMultimodalPrompt`: テキスト+画像混在の `[]ContentPart` に対し、テキスト部分はそのまま残り、画像参照が `[Attached image: path]` 形式で埋め込まれたプロンプト文字列が返ることを確認。
    *   `TestCleanupMultimodalFiles`: セッションIDに紐づくファイルが削除されることを確認。

#### [NEW] [multimodal.go](file://shared/libs/go/agentservice/multimodal.go)
*   **Description**: マルチモーダルデータの一時ファイル保存とプロンプト書き換えロジック。
*   **Technical Design**:
    ```go
    package agentservice

    import (
        "crypto/sha256"
        "encoding/base64"
        "encoding/hex"
        "fmt"
        "os"
        "path/filepath"
        "strings"

        "github.com/axsh/arctic-tern/shared/libs/go/codingagent"
    )

    // SaveImageToTempFile decodes Base64 image data and saves it to
    // tmp/multimodal/{sessionID}_{hash}.{ext}.
    // Returns the absolute path to the saved file.
    func SaveImageToTempFile(baseDir string, sessionID string, source *codingagent.ImageSource) (string, error) {
        if source.Data == "" {
            return "", fmt.Errorf("image data is empty")
        }
        decoded, err := base64.StdEncoding.DecodeString(source.Data)
        if err != nil {
            return "", fmt.Errorf("invalid base64 data: %w", err)
        }

        ext := mediaTypeToExt(source.MediaType)
        hash := sha256.Sum256(decoded)
        filename := fmt.Sprintf("%s_%s%s", sessionID, hex.EncodeToString(hash[:8]), ext)

        dir := filepath.Join(baseDir, "tmp", "multimodal")
        if err := os.MkdirAll(dir, 0755); err != nil {
            return "", fmt.Errorf("create multimodal dir: %w", err)
        }

        path := filepath.Join(dir, filename)
        if err := os.WriteFile(path, decoded, 0644); err != nil {
            return "", fmt.Errorf("write image file: %w", err)
        }
        return path, nil
    }

    // BuildMultimodalPrompt processes []ContentPart:
    // - text blocks are concatenated
    // - image blocks are saved to temp files and replaced with references
    func BuildMultimodalPrompt(baseDir, sessionID string, parts []codingagent.ContentPart) (string, []string, error) {
        var sb strings.Builder
        var savedFiles []string
        for _, p := range parts {
            switch p.Type {
            case "text":
                sb.WriteString(p.Text)
            case "image":
                if p.Source == nil {
                    return "", nil, fmt.Errorf("image content part missing source")
                }
                path, err := SaveImageToTempFile(baseDir, sessionID, p.Source)
                if err != nil {
                    return "", nil, fmt.Errorf("save image: %w", err)
                }
                savedFiles = append(savedFiles, path)
                sb.WriteString(fmt.Sprintf("\n[Attached image: %s]\n", path))
            }
        }
        return sb.String(), savedFiles, nil
    }

    // CleanupMultimodalFiles removes all temp files for a session.
    func CleanupMultimodalFiles(paths []string) {
        for _, p := range paths {
            os.Remove(p)
        }
    }

    func mediaTypeToExt(mediaType string) string {
        switch strings.ToLower(mediaType) {
        case "image/png":
            return ".png"
        case "image/jpeg", "image/jpg":
            return ".jpg"
        case "image/gif":
            return ".gif"
        case "image/webp":
            return ".webp"
        default:
            return ".bin"
        }
    }
    ```

#### [NEW] [handler_v2_test.go](file://shared/libs/go/agentservice/handler_v2_test.go)
*   **Description**: v2 エンドポイントのHTTPハンドラ結合テスト（Mock Agent使用）。
*   **テストケース**:
    *   `TestHandleV2SendMessage_TextOnly`: テキストのみの v2 リクエストが正常処理されること。
    *   `TestHandleV2SendMessage_WithImage`: 画像付きリクエストが正常処理され、SSEイベントが返ること。
    *   `TestHandleV2SendMessage_EmptyContent`: `content` が空配列の場合 400 エラーが返ること。
    *   `TestHandleV2SendMessage_InvalidBase64`: 壊れたBase64データで 400 エラーが返ること。
    *   `TestHandleV2SendMessage_SessionNotFound`: 存在しないセッションIDに対し 404 エラーが返ること。
    *   `TestHandleV2SendMessage_WayfinderRejects`: Wayfinderエージェントセッションに画像を送信し 501 エラーが返ること。

#### [NEW] [handler_v2.go](file://shared/libs/go/agentservice/handler_v2.go)
*   **Description**: v2 API エンドポイントのハンドラ実装。
*   **Technical Design**:
    ```go
    package agentservice

    // handleSendMessageV2 handles POST /api/v2/sessions/:id/messages.
    // Accepts {"content": []ContentPart} with multimodal support.
    func (s *Server) handleSendMessageV2(w http.ResponseWriter, r *http.Request) {
        // 1. Extract session ID from path
        // 2. Validate session exists and get agent
        // 3. Parse request body as SendMessageV2Request
        // 4. Validate content is non-empty
        // 5. Check if agent supports multimodal (HasNonTextContent check)
        //    - If non-text content exists and agent returns ErrMultimodalNotSupported,
        //      return 501 Not Implemented
        // 6. For multimodal content:
        //    - Call BuildMultimodalPrompt to save images and build prompt
        //    - Track saved files for cleanup
        // 7. For text-only content:
        //    - Use ExtractText directly
        // 8. Create agent session, send message, stream response
        //    (reuse existing SSE/JSON streaming logic from handler.go)
        // 9. On session close, call CleanupMultimodalFiles
    }
    ```
*   **Logic**:
    *   リクエストボディを `SendMessageV2Request` (Content `[]codingagent.ContentPart`) としてパース。
    *   `codingagent.HasNonTextContent(req.Content)` が `true` の場合:
        *   エージェントの `Send` 呼び出し前に `BuildMultimodalPrompt` で画像を保存しプロンプトを組み立てる。
        *   エージェントが `ErrMultimodalNotSupported` を返した場合、HTTP 501 を返す。
    *   `codingagent.HasNonTextContent(req.Content)` が `false` の場合:
        *   `codingagent.ExtractText(req.Content)` でプレーンテキストを取得。
    *   SSE/JSONストリーミングは既存の `streamSSE` / `respondJSON` を再利用する。
    *   `defer` でセッション終了時に `CleanupMultimodalFiles` を呼び出す。

#### [MODIFY] [service.go](file://shared/libs/go/agentservice/service.go)
*   **Description**: v2 エンドポイントのルーティングを追加。
*   **Technical Design**:
    *   `HTTPHandler()` メソッド内に v2 ルートを追加:
    ```go
    // 既存 v1 ルート（変更なし）
    mux.HandleFunc("/api/v1/sessions/", s.routeSessionByID)

    // v2 ルート追加
    mux.HandleFunc("/api/v2/sessions/", s.routeSessionByIDV2)
    ```
    *   `routeSessionByIDV2` を追加:
    ```go
    func (s *Server) routeSessionByIDV2(w http.ResponseWriter, r *http.Request) {
        path := r.URL.Path
        if strings.HasSuffix(path, "/messages") {
            s.handleSendMessageV2(w, r)
        } else {
            // v2 ではメッセージ送信のみ。それ以外は v1 にフォールバック。
            http.Error(w, "use /api/v1/ for this operation", http.StatusNotFound)
        }
    }
    ```

---

### wayfinder (非サポートエラー)

#### [MODIFY] [adapter_test.go](file://shared/libs/go/wayfinder/adapter_test.go)
*   **Description**: Wayfinder の multimodal 非対応テストを追加。
*   **テストケース**:
    *   `TestWayfinderRejectsMultimodalContent`: `SupportsMultimodal()` が `false` を返すことを確認。

#### [MODIFY] [adapter.go](file://shared/libs/go/wayfinder/adapter.go)
*   **Description**: `SupportsMultimodal()` メソッドを Adapter に追加。
*   **Technical Design**:
    ```go
    // SupportsMultimodal returns false: Wayfinder does not support image inputs.
    func (a *Adapter) SupportsMultimodal() bool {
        return false
    }
    ```
*   **Logic**: v2 ハンドラ側で、エージェントが `SupportsMultimodal()` インターフェースを実装していて `false` を返す場合、マルチモーダルコンテンツを拒否して 501 を返す。

---

### claudecode / codex (マルチモーダルサポート宣言)

#### [MODIFY] [adapter.go (claudecode)](file://shared/libs/go/codingagent/claudecode/adapter.go)
*   **Description**: `SupportsMultimodal()` メソッドを追加。
*   **Technical Design**:
    ```go
    // SupportsMultimodal returns true: Claude Code CLI supports image inputs.
    func (a *ClaudeCodeAdapter) SupportsMultimodal() bool {
        return true
    }
    ```

#### [MODIFY] [adapter.go (codex)](file://shared/libs/go/codingagent/codex/adapter.go)
*   **Description**: `SupportsMultimodal()` メソッドを追加。
*   **Technical Design**:
    ```go
    // SupportsMultimodal returns true: Codex CLI supports image inputs.
    func (a *CodexAdapter) SupportsMultimodal() bool {
        return true
    }
    ```

---

### llmgateway/anthropic (Bifrostマッピング拡張)

#### [MODIFY] [convert_test.go](file://shared/libs/go/llmgateway/anthropic/convert_test.go)
*   **Description**: 画像ブロックの Bifrost 変換テストを追加。
*   **テストケース**:
    *   `TestConvertToBifrost_ImageBlock`: `ContentBlock{Type: "image", Source: &ImageSource{Type: "base64", MediaType: "image/png", Data: "iVBOR..."}}` が `ResponsesInputMessageContentBlockTypeImage` にマッピングされ、`ImageURL` が `data:image/png;base64,iVBOR...` になることを確認。
    *   `TestConvertToBifrost_MixedTextAndImage`: テキストと画像が混在するメッセージが正しく複数の `ResponsesMessage` に変換されることを確認。
    *   `TestConvertToBifrost_ImageMissingSource`: Source が nil の画像ブロックに対してエラーが返ることを確認。
    *   `TestConvertToBifrost_ImageEmptyData`: Data が空の場合にエラーが返ることを確認。

#### [MODIFY] [types.go](file://shared/libs/go/llmgateway/anthropic/types.go)
*   **Description**: `ContentBlock` に `Source` フィールドを追加し、`ImageSource` 型を定義する。
*   **Technical Design**:
    ```go
    // ImageSource represents the source of an image content block.
    type ImageSource struct {
        Type      string `json:"type"`       // "base64"
        MediaType string `json:"media_type"` // e.g., "image/png"
        Data      string `json:"data"`       // Base64-encoded data
    }

    // ContentBlock に追加するフィールド:
    type ContentBlock struct {
        Type      string          `json:"type"`
        Text      string          `json:"text,omitempty"`
        ID        string          `json:"id,omitempty"`
        Name      string          `json:"name,omitempty"`
        Input     json.RawMessage `json:"input,omitempty"`
        ToolUseID string          `json:"tool_use_id,omitempty"`
        Content   string          `json:"content,omitempty"`
        Source    *ImageSource    `json:"source,omitempty"` // 追加
    }
    ```

#### [MODIFY] [convert.go](file://shared/libs/go/llmgateway/anthropic/convert.go)
*   **Description**: `convertMessage` に `image` ブロックの処理を追加。
*   **Technical Design**:
    *   `convertMessage` 関数内の `switch block.Type` に `case "image":` を追加:
    ```go
    case "image":
        if block.Source == nil {
            return nil, fmt.Errorf("image block missing source")
        }
        if block.Source.Data == "" {
            return nil, fmt.Errorf("image block has empty data")
        }
        // Build data URL: data:{mediaType};base64,{data}
        dataURL := fmt.Sprintf("data:%s;base64,%s", block.Source.MediaType, block.Source.Data)
        detail := "auto"
        imgType := bifrostSchemas.ResponsesInputMessageContentBlockTypeImage
        role := toBifrostRole(msg.Role)
        result = append(result, bifrostSchemas.ResponsesMessage{
            Role: &role,
            Content: &bifrostSchemas.ResponsesMessageContent{
                ContentBlocks: []bifrostSchemas.ResponsesMessageContentBlock{
                    {
                        Type: imgType,
                        ResponsesInputMessageContentBlockImage: &bifrostSchemas.ResponsesInputMessageContentBlockImage{
                            ImageURL: &dataURL,
                            Detail:   &detail,
                        },
                    },
                },
            },
        })
    ```

---

### tests/ (結合テスト)

#### [NEW] [multimodal_integration_test.go](file://tests/multimodal_integration_test.go)
*   **Description**: マルチモーダル機能のエンドツーエンド結合テスト。
*   **テストケース**:
    *   `TestV1BackwardCompatibility`: v1 エンドポイントにテキストメッセージを送信し、正常応答が返ることを確認（後方互換性の検証）。
    *   `TestV2TextOnlyMessage`: v2 エンドポイントにテキストのみの `[]ContentPart` を送信し、正常応答が返ることを確認。
    *   `TestV2MultimodalMessage_MockAgent`: v2 エンドポイントに画像付きリクエストを送信し、Mock Agent 経由で処理され SSE イベントが返ることを確認。`tmp/multimodal/` に一時ファイルが作成されていることを確認。セッション終了後に一時ファイルが削除されていることを確認。
    *   `TestV2WayfinderRejection`: Wayfinder セッションに画像付き v2 リクエストを送信し、HTTP 501 が返ることを確認。レスポンスボディに "multimodal inputs are not supported" が含まれることを確認。
    *   `TestV2InvalidBase64`: 壊れた Base64 データを v2 エンドポイントに送信し、HTTP 400 が返ることを確認。
    *   `TestV2EmptyContent`: 空の content 配列を送信し、HTTP 400 が返ることを確認。
    *   `TestV2LargeImageRejection`: 制限を超える大きな画像を送信し、HTTP 413 が返ることを確認（MaxRequestBodyBytes 設定に依存）。

---

## Step-by-Step Implementation Guide

### Step 1: コンテンツ型定義とユーティリティ (TDD)
1. `shared/libs/go/codingagent/content_test.go` を作成し、テストを記述。
2. `shared/libs/go/codingagent/content.go` を作成し、`ContentPart`, `ImageSource`, `ErrMultimodalNotSupported`, `ExtractText`, `HasNonTextContent`, `TextOnlyContent` を実装。
3. `./scripts/process/build.sh` でビルドと単体テスト成功を確認。
4. **git commit**。

### Step 2: 一時ファイル保存とプロンプト書き換え (TDD)
1. `shared/libs/go/agentservice/multimodal_test.go` を作成し、テストを記述。
2. `shared/libs/go/agentservice/multimodal.go` を作成し、`SaveImageToTempFile`, `BuildMultimodalPrompt`, `CleanupMultimodalFiles` を実装。
3. `./scripts/process/build.sh` でビルドと単体テスト成功を確認。
4. **git commit**。

### Step 3: エージェント SupportsMultimodal 宣言
1. `shared/libs/go/wayfinder/adapter.go` に `SupportsMultimodal() bool` を追加（`false` を返す）。
2. `shared/libs/go/codingagent/claudecode/adapter.go` に `SupportsMultimodal() bool` を追加（`true` を返す）。
3. `shared/libs/go/codingagent/codex/adapter.go` に `SupportsMultimodal() bool` を追加（`true` を返す）。
4. `shared/libs/go/wayfinder/adapter_test.go` にテストを追加。
5. `./scripts/process/build.sh` でビルドと単体テスト成功を確認。
6. **git commit**。

### Step 4: v2 ハンドラ実装 (TDD)
1. `shared/libs/go/agentservice/handler_v2_test.go` を作成し、テストを記述。
2. `shared/libs/go/agentservice/handler_v2.go` を作成し、`handleSendMessageV2` を実装。
3. `shared/libs/go/agentservice/service.go` の `HTTPHandler()` に v2 ルートを追加。
4. `./scripts/process/build.sh` でビルドと単体テスト成功を確認。
5. **git commit**。

### Step 5: Anthropic 画像ブロック変換 (TDD)
1. `shared/libs/go/llmgateway/anthropic/convert_test.go` にテストケースを追加。
2. `shared/libs/go/llmgateway/anthropic/types.go` に `ImageSource` と `ContentBlock.Source` フィールドを追加。
3. `shared/libs/go/llmgateway/anthropic/convert.go` の `convertMessage` に `case "image":` ロジックを追加。
4. `./scripts/process/build.sh` でビルドと単体テスト成功を確認。
5. **git commit**。

### Step 6: 結合テスト
1. `tests/multimodal_integration_test.go` を作成。
2. `./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestV1BackwardCompatibility"` で後方互換テスト実行。
3. `./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestV2"` で v2 関連テスト実行。
4. **git commit**。

### Step 7: 検証とプッシュ
1. Verification Plan の全コマンドを実行。
2. 総合判定を実施。
3. **git push**。

---

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```
    *   **Log Verification**: 新規ファイル (`content.go`, `multimodal.go`, `handler_v2.go`) が正常にコンパイルされること。全単体テストが PASS すること。

2.  **Integration Tests (マルチモーダル結合テスト)**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestV1BackwardCompatibility"
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestV2TextOnlyMessage"
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestV2MultimodalMessage"
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestV2WayfinderRejection"
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestV2InvalidBase64"
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestV2EmptyContent"
    ```
    *   **Log Verification**: 各テストが PASS すること。501/400/413 等の期待するHTTPステータスが返ること。一時ファイルのクリーンアップが実行されていること。

3.  **Full Integration Test (リグレッション確認)**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh
    ```
    *   **Log Verification**: 既存テストにリグレッションがないこと。

4.  **E2Eテスト**:
    本計画ではCLI実体を呼び出すE2Eテストは対象外とする。理由: マルチモーダル機能の主要ロジック（一時ファイル保存、プロンプト書き換え、Bifrost変換、501エラー応答）は全て単体テストおよび結合テスト（Mock Agent使用）で十分に検証可能であるため。実際のCLI（claude/codex）を経由したE2Eテストは、マルチモーダル入力をサポートする実環境が必要であり、本フェーズのスコープ外とする。

### テスト項目セルフレビュー (Testing Rules 11.4)

1.  **網羅性の検証**: 本テスト群が全て成功した場合、以下が確認される:
    *   コンテンツ型定義が正しく機能する (content_test.go)
    *   一時ファイル保存/クリーンアップが正しく動作する (multimodal_test.go)
    *   v2 ハンドラが正常/異常パターンを適切に処理する (handler_v2_test.go)
    *   Anthropic画像ブロックがBifrostに正しく変換される (convert_test.go)
    *   結合レベルでv1後方互換・v2マルチモーダル・501エラーが動作する (multimodal_integration_test.go)
    これにより、マルチモーダル機能が実際に正しく動作していると言える。

2.  **証拠の十分性**: 各テストは「エラーが出ない」だけでなく、「保存されたファイルのバイト列一致」「生成されたプロンプト文字列の内容検証」「HTTPステータスコードの一致」「Bifrost構造体のフィールド値確認」等の具体的なアサーションを含んでいる。

3.  **迂回・抜け道の排除**: v2ハンドラテストではMock Agentを通じてリクエストが実際にエージェントまで到達していることを確認する。Wayfinderテストでは501が返ることで、意図したエラーパスが使用されていることを確認する。

4.  **依存関係の整合性**: ボトムアップ順序: content.go (末端) -> multimodal.go (中間) -> handler_v2.go (上位) -> integration_test (結合) の順で検証する。

### 総合判定プロセス (Testing Rules 12)

全テスト完了後、Testing Rules 12.2 のチェック項目（スキップ有無、部分エラー、迂回処理、アダプタ誤適用、テスト間依存、カバレッジ、外部システム状態）を確認し、12.3 のフォーマットで総合判定を記録する。

---

## Documentation

#### [MODIFY] [README.md](file://README.md)
*   **更新内容**: cawa API セクションに v2 エンドポイントの説明を追加。マルチモーダル対応の概要を記載。
