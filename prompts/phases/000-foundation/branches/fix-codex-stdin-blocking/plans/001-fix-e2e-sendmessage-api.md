# 001-fix-e2e-sendmessage-api

> **Source Specification**: [001-fix-e2e-sendmessage-api.md](file:///c:/Users/yamya/myprog/arctic-tern/work/fix-codex-stdin-blocking/prompts/phases/000-foundation/branches/fix-codex-stdin-blocking/ideas/001-fix-e2e-sendmessage-api.md)

## Goal Description
`agentservice` の API 仕様変更（v1 message -> v2 content）に追従できていなかった E2E テストコードを修正し、`"content must not be empty"` バリデーションエラーを解消します。これにより、codex/claudecode の正常系・異常系テストが再び正しく実行可能になります。

## User Review Required
None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| sendE2EMessage ヘルパーの修正 (content 形式への変更) | Proposed Changes > agentservice_e2e_test.go |
| wayfinder_e2e_test.go の修正 (L120 直接送信箇所) | Proposed Changes > wayfinder_e2e_test.go |
| agentservice_e2e_test.go L843 の修正 (WSLテスト直接送信) | Proposed Changes > agentservice_e2e_test.go |
| 修正後のバリデーション通過 | Verification Plan |

## Proposed Changes

### tests (E2E Tests)

#### [MODIFY] [agentservice_e2e_test.go](file:///c:/Users/yamya/myprog/arctic-tern/work/fix-codex-stdin-blocking/tests/agentservice_e2e_test.go)
*   **Description**: `sendE2EMessage` ヘルパーおよび WSL テスト内の直接 JSON 構築箇所を v2 API 形式に修正。
*   **Technical Design**:
    *   `sendE2EMessage` 内部で匿名構造体/Map を使用して `{"content": [{"type": "text", "text": "..."}]}` を構築。
*   **Logic**:
    *   `sendE2EMessage` (L261付近): `map[string]string{"message": message}` を `map[string]any{"content": []map[string]string{{"type": "text", "text": message}}}` に変更。
    *   WSL テスト (L843付近): 同様の変換を適用。

#### [MODIFY] [wayfinder_e2e_test.go](file:///c:/Users/yamya/myprog/arctic-tern/work/fix-codex-stdin-blocking/tests/wayfinder_e2e_test.go)
*   **Description**: 直接 JSON を構築している箇所を v2 形式に修正。
*   **Logic**:
    *   L120付近: `map[string]string{"message": message}` を `map[string]any{"content": []map[string]string{{"type": "text", "text": message}}}` に変更。

## Step-by-Step Implementation Guide

1.  **Update `sendE2EMessage` Helper**:
    *   Edit `tests/agentservice_e2e_test.go`.
    *   Modify `sendE2EMessage` function to wrap the message string into a `ContentPart` array within the `content` field.
2.  **Update WSL Test Direct Call**:
    *   Edit `tests/agentservice_e2e_test.go`.
    *   Locate the direct `json.Marshal` call for message sending in the WSL test case and update it to the `content` format.
3.  **Update Wayfinder Test Direct Call**:
    *   Edit `tests/wayfinder_e2e_test.go`.
    *   Locate the direct `json.Marshal` call at L120 and update it to the `content` format.
4.  **Verification**:
    *   Run `integration_test.sh` with specific filters to verify the fix.

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```

2.  **Integration Tests (Targeted)**:
    まず、バリデーションエラーが解消されたことを確認するために、比較的軽量なテストを実行します。
    ```bash
    ./scripts/process/integration_test.sh --specify "TestE2E_StandaloneHealth|TestCodexE2E_HealthWithCodexAgent"
    ```
    *   **Log Verification**: 400 Bad Request ("content must not be empty") が発生せず、200 OK またはエージェント固有のイベントが返ることを確認。

3.  **Full Agent E2E Tests**:
    ```bash
    ./scripts/process/integration_test.sh --specify "TestCodexE2E|TestE2E_CodingAgent"
    ```
    *   **Log Verification**: サーバーログに `content` フィールドが正しくパースされ、エージェント（codex/claudecode）が起動されるログが出ることを確認。

### セルフレビュー
- [x] **網羅性**: 3つのファイルの計4箇所の修正漏れがないか。
- [x] **依存関係**: ヘルパー修正を最初に行う計画になっているか。
- [x] **迂回排除**: `ValidateContentParts` を回避せず、正しくデータを送るようになっているか。

## Documentation
None.
