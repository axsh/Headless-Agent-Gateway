# 000-Persistent-Multimodal-Sessions

> **Source Specification**: [000-Persistent-Multimodal-Sessions.md](file://prompts/phases/000-foundation/branches/fix-session-in-codex/ideas/000-Persistent-Multimodal-Sessions.md)

## Goal Description
マルチモーダル（画像）対応において、画像データをセッションディレクトリ（`SessionDir`）内に永続保存し、会話の履歴（`session.Message`）に構造的に保持することで、2ターン目以降の継続的な会話や再起動後のセッションでも画像を認識できるようにします。

## User Review Required
None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| 1. 画像の永続保存 (`SessionDir`への保存) | Proposed Changes > agentservice/multimodal.go |
| 2. 履歴における画像保持 (`Message`の拡張) | Proposed Changes > wayfinder/session/session_state.go |
| 3. 文脈の再現 (アダプタでの画像処理) | Proposed Changes > codingagent/{codex,claudecode}/adapter.go |
| 4. クリーンアップの制御 (削除タイミングの変更) | Proposed Changes > agentservice/handler_v2.go |
| 5. 後方互換性の維持 | Proposed Changes > wayfinder/session/session_state.go |

## Proposed Changes

### wayfinder/session (データモデル層)

#### [MODIFY] [session_state.go](file://shared/libs/go/wayfinder/session/session_state.go)
*   **Description**: `Message` 構造体を拡張し、構造化されたコンテンツ（テキスト+画像）を保持可能にする。
*   **Technical Design**:
    *   ```go
        type Message struct {
            Role       string           `json:"role"`
            Content    string           `json:"content"`       // 後方互換性のため維持
            ContentParts []ContentPart   `json:"content_parts,omitempty"` // 新設
            Timestamp  time.Time        `json:"timestamp"`
            Pinned     bool             `json:"pinned"`
            Seq        int              `json:"seq"`
            ToolCalls  []ToolCallRecord `json:"tool_calls,omitempty"`
            ToolCallID string           `json:"tool_call_id,omitempty"`
        }

        type ContentPart struct {
            Type  string         `json:"type"` // "text", "image"
            Text  string         `json:"text,omitempty"`
            Image *ImageMetadata `json:"image,omitempty"`
        }

        type ImageMetadata struct {
            Path      string `json:"path"`       // SessionDir からの相対パス
            MediaType string `json:"media_type"`
        }
        ```

#### [MODIFY] [session_state_test.go](file://shared/libs/go/wayfinder/session/session_state_test.go)
*   **Description**: `ContentParts` を含むメッセージの JSON シリアライズ/デシリアライズのテストを追加。

---

### agentservice (ハンドラ・マルチモーダル処理)

#### [MODIFY] [multimodal.go](file://shared/libs/go/agentservice/multimodal.go)
*   **Description**: 画像を `SessionDir` 配下に保存する `SaveImageToSessionDir` を追加（既存の `SaveImageToTempFile` をリファクタリング）。
*   **Logic**:
    *   引数に `sessionDir` を追加。
    *   `sessionDir` が空でない場合、`filepath.Join(sessionDir, "multimodal")` に保存。
    *   ファイル名は `{hash}.{ext}` とし、既存ファイルがあればスキップ（コンテンツ重複排除）。
    *   `SessionDir` 内の相対パスを返す。

#### [MODIFY] [handler_v2.go](file://shared/libs/go/agentservice/handler_v2.go)
*   **Description**: 画像を永続ディレクトリに保存し、構造化された `Message` をセッション履歴に追加する。
*   **Logic**:
    *   `BuildMultimodalPrompt` を `BuildMultimodalContent` にリプレースし、`[]ContentPart` を生成する。
    *   `CleanupMultimodalFiles` の `defer` 呼び出しを削除。
    *   セッション履歴保存時に `ContentParts` をセット。

---

### codingagent (アダプタ層)

#### [MODIFY] [codex/adapter.go](file://shared/libs/go/codingagent/codex/adapter.go)
#### [MODIFY] [claudecode/adapter.go](file://shared/libs/go/codingagent/claudecode/adapter.go)
*   **Description**: `CreateSession` 時に渡されるオプション（履歴）から画像を復元してCLIに渡す。
*   **Logic**:
    *   履歴内の `ContentParts` を走査。
    *   `Image` パートがある場合、絶対パスを解決。
    *   CLIの引数（プロンプト文字列内）に再度パスを埋め込むか、アダプタ固有の画像入力方式に変換。

---

## Step-by-Step Implementation Guide

1.  **[Preparation]**: `shared/libs/go/wayfinder/session/session_state.go` を修正し、`ContentParts` 関連の型を定義する。
2.  **[Test-First]**: `shared/libs/go/wayfinder/session/session_state_test.go` にシリアライズテストを追加し、ビルドエラーを確認（TDD）。
3.  **[Logic]**: `session_state.go` の `convertToSessionMessages` 等の変換ヘルパーを修正し、`ContentParts` を適切に扱うようにする。
4.  **[Refactor]**: `shared/libs/go/agentservice/multimodal.go` に `SaveImageToSessionDir` を実装。
5.  **[Handler]**: `shared/libs/go/agentservice/handler_v2.go` を修正。
    *   画像保存先を `record.SessionDir` に変更。
    *   履歴保存時に `ContentParts` を含める。
    *   `defer CleanupMultimodalFiles` を削除。
6.  **[Adapter]**: `codex/adapter.go` および `claudecode/adapter.go` を修正し、履歴に含まれる構造化されたコンテンツからCLIプロンプトを再構成するロジックを追加。
7.  **[Verification]**: 結合テスト `tests/multimodal_integration_test.go` に継続会話のケースを追加して実行。

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```

2.  **Integration Tests (マルチモーダル永続化)**:
    #### [NEW] [multimodal_persistence_test.go](file://tests/multimodal_persistence_test.go)
    *   **テストケース**: `TestMultimodalSessionPersistence`
    *   **検証内容**:
        1.  画像付き V2 リクエストを送信。
        2.  `SessionDir/multimodal/` にファイルが存在することを確認。
        3.  2回目のリクエスト（画像なし、テキストのみ）を送信。
        4.  アダプタに渡されるプロンプトに、1回目の画像パスが含まれていることを確認（モックアダプタで検証）。
    *   **実行コマンド**:
        ```bash
        ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestMultimodalSessionPersistence"
        ```

3.  **Regression Testing**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories common,llm
    ```

## Documentation

#### [MODIFY] [README.md](file://README.md)
*   **更新内容**: マルチモーダル画像の保存先がセッションディレクトリに変更されたこと、およびそのライフサイクルについての説明を追記。
