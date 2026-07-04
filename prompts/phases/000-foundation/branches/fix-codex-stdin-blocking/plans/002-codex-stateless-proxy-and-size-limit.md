# 002-codex-stateless-proxy-and-size-limit

> **Source Specification**: [002-codex-multimodal-limit-and-restoration.md](file:///c:/Users/yamya/myprog/arctic-tern/work/fix-codex-stdin-blocking/prompts/phases/000-foundation/branches/fix-codex-stdin-blocking/ideas/002-codex-multimodal-limit-and-restoration.md)

## Goal Description
Codex CLI の 1MB stdin 制限に対応するため、サーバー側をステートレスなプロキシ型に移行します。サーバー側での画像データの永続化と過去コンテキストの自動復元機能を削除し、代わりに送信データのサイズチェック機能を導入します。また、クライアント側でのエラーハンドリング（フェイルファスト）を強化します。

## User Review Required
- **下位互換性**: サーバー側での履歴からの画像自動復元が廃止されるため、クライアント側は必要に応じて画像を毎回送信する必要があります。
- **設定ファイル**: `model_profiles.yaml` に新しいセクション `coding_agents` が追加されます。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| 1MB制限の事前チェック (設定値ベース) | Proposed Changes > agentservice/handler.go |
| 設定ファイル管理 (max_prompt_bytes) | Proposed Changes > config/config.go |
| 画像永続化機能の削除 | Proposed Changes > agentservice/multimodal.go |
| 自動復元ロジックの削除 | Proposed Changes > agentservice/handler.go |
| クライアント側 Fail-fast 実装 | Proposed Changes > client/v1/stream.go |

## Proposed Changes

### 1. Configuration

#### [MODIFY] [config.go](file:///shared/libs/go/config/config.go)
*   **Description**: `ModelProfilesConfig` に `CodingAgents` 設定を追加。
*   **Technical Design**:
    ```go
    type CodingAgentsConfig map[string]AgentConfig

    type AgentConfig struct {
        MaxPromptBytes int `yaml:"max_prompt_bytes"`
    }
    ```

### 2. Agent Service (Server-side)

#### [MODIFY] [multimodal.go](file:///shared/libs/go/agentservice/multimodal.go)
*   **Description**: 画像をディスクに保存する機能を削除し、データ変換のみに絞る。
*   **Action**: `SaveImageToSessionDir`, `SaveImageToTempFile` などの関数を削除し、バイナリデータを取得するユーティリティのみを残す。

#### [MODIFY] [handler.go](file:///shared/libs/go/agentservice/handler.go)
*   **Description**: 画像保存の廃止、サイズバリデーションの追加。
*   **Logic**:
    1.  `handleSendMessage` 内で、`BuildMultimodalContent` を呼び出す際、もはや `SessionDir` を渡して保存させない。
    2.  アダプターの `Send` 呼び出し前に、構築されたデータのサイズを計算。
    3.  `cfg.CodingAgents[agentName].MaxPromptBytes` (デフォルト 1MB) を超える場合、即座に `EventError` をストリームに流して終了。

### 3. Client Library

#### [MODIFY] [stream.go](file:///client/v1/stream.go)
*   **Description**: `Output` メソッドのフェイルファスト化。
*   **Logic**:
    *   `events()` チャネルからイベントを受信するループ内で、`EventError` を受け取った瞬間にループを抜け、エラーを返却するように変更。

## Step-by-Step Implementation Guide

1.  **Config Extension**:
    *   `shared/libs/go/config/config.go` を編集し、`CodingAgentsConfig` 構造体を追加。
2.  **Clean up Multimodal Logic**:
    *   `shared/libs/go/agentservice/multimodal.go` から、`filepath.Join(sessionDir, "multimodal")` などの保存パス構築と `os.WriteFile` を伴う処理を削除。
3.  **Implement Validation in Handler**:
    *   `shared/libs/go/agentservice/handler.go` を編集。
    *   `handleSendMessage` 内で、履歴からの画像読み込み (`LoadRecentImages`) を削除。
    *   アダプター呼び出し前に `len(finalPromptBytes)` をチェックし、超過時にエラーイベントを送信するガードを追加。
4.  **Update Client Stream**:
    *   `client/v1/stream.go` の `Output` メソッドを修正し、エラーイベント受信時に即座に return するようにする。
5.  **Config Default**:
    *   `model_profiles.yaml` に Codex 用のデフォルト値 (1048576) を追加。

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```

2.  **Integration Tests**:
    ```bash
    ./scripts/process/integration_test.sh --specify "TestCodexE2E_Stateless"
    ```
    *   **Log Verification**: サーバーログに出力されるプロンプトサイズが制限内であることを確認。制限超過時に 413 エラーではなく SSE エラーイベントが正しくクライアントに届くことを確認。

3.  **E2E Tests (新規)**:
    #### [NEW] [stateless_multimodal_test.go](file://tests/stateless_multimodal_test.go)
    *   **テストケース**: `TestCodex_1MB_Limit_Enforcement`
    *   **検証ポイント**: 1.1MB の画像を添付して送信した際、Codex が起動される前にエラー（Stream Error）が返ること。
    *   **テストケース**: `TestCodex_Stateless_Behavior`
    *   **検証ポイント**: 1回目に画像、2回目にテキストのみを送った際、2回目の Codex 実行に 1回目の画像が含まれていないこと。

## Documentation

#### [MODIFY] [README.md](file://README.md)
*   **更新内容**: マルチモーダルセッションの永続化に関する記述を、「自動復元」から「クライアント管理」に変更。
