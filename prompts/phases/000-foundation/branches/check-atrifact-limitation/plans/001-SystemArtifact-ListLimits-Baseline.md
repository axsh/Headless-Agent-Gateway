# 001-SystemArtifact-ListLimits-Baseline

> **Source Specification**: `prompts/phases/000-foundation/branches/check-atrifact-limitation/ideas/000-SystemArtifact-ListLimits.md`

## Goal Description

000 仕様で確認済みの挙動のうち、**001 改訂後も有効な事実**を自動テストとして恒久化する。製品コードの大規模変更は行わない（ページネーション契約の変更は Part1=`002-...-Part1` が担当）。

対象となる確認事項:

- Coding Agent リアルタイム検知に固定件数上限がないこと（解析経路のスモーク）
- 明示 `per_page` / ページ送りで 50 件を全件取得できること
- `session_id` フィルタで終了後もイベントが残ること（Store レベル）
- Reconciliation / リアルタイムのデータフローが回帰しないこと（既存統合テストの指定実行）

## User Review Required

None. デフォルト30の固定アサーションは **意図的に含めない**（001 でデフォルト100へ変更するため）。

## Requirement Traceability

| Requirement (from Spec 000) | Implementation Point (Section/File) |
| :--- | :--- |
| R1 ページネーション仕様の明文化 | Documentation + Part1 が契約を更新。本 Part は明示 per_page の回帰のみ |
| R2 Agent 側件数上限なし | Proposed Changes > analyzer 既存テスト維持 + build |
| R3 Reconciliation 経路の明文化 | Verification で既存 `TestReconcile_SessionEndGitSupplement` |
| R4 セッション再開後も List 可能 | Proposed Changes > store テスト（同一 session_id で Close 後も List） |
| R5 50件全件取得 | Proposed Changes > store/api の 50件ページ巡回テスト |
| O1/O2/O3 | **実装対象外**（001 必須化 → Part1〜3） |

## Proposed Changes

### Store tests

#### [MODIFY] [shared/libs/go/artifact/store/store_test.go](file://shared/libs/go/artifact/store/store_test.go)

*   **Description**: 50件 create の明示ページ送り・CloseSession 後の List を追加（TDD: 先にテスト）。
*   **Technical Design**: 既存 `insertSession` / `SaveSystemArtifactEvent` ヘルパーを再利用。
*   **Logic**:

```go
func TestListSystemArtifacts_FiftyItems_ExplicitPagination(t *testing.T) {
    // seed 50 create events for session s50
    // page1 per_page=30 → len=30, total=50
    // page2 per_page=30 → len=20, total=50
    // merge unique keys == 50
}

func TestListSystemArtifacts_AfterCloseSession_StillListed(t *testing.T) {
    // UpsertSession → SaveSystemArtifactEvent → CloseSession
    // ListSystemArtifacts(SessionIDs) still returns the event
}
```

### API tests

#### [MODIFY] [shared/libs/go/artifact/api/system_test.go](file://shared/libs/go/artifact/api/system_test.go)

*   **Description**: HTTP 層でも 50件・明示 `per_page=30` の2ページ結合を検証。
*   **Logic**:

```go
func TestSystemAPI_List_FiftyItems_TwoPages(t *testing.T) {
    // seed 50 events
    // GET ?session_id=&page=1&per_page=30
    // GET ?session_id=&page=2&per_page=30
    // unique keys across pages == 50
}
```

### Product code

本 Part では **製品コード変更なし**（テスト追加とドキュメント注記のみ）。デフォルト値変更は Part1。

## Step-by-Step Implementation Guide

1. **Add store tests first (TDD)**: `TestListSystemArtifacts_FiftyItems_ExplicitPagination` と `TestListSystemArtifacts_AfterCloseSession_StillListed` を追加し、`./scripts/process/build.sh` で緑を確認（現行実装で通る想定）。
2. **Add API test**: `TestSystemAPI_List_FiftyItems_TwoPages` を追加し同ビルドで緑を確認。
3. **Annotate idea 000**: 仕様書末尾または本計画の Documentation に「デフォルト/上限は 001 改訂が正」と明記（任意の短い追記で可）。
4. **Do not implement O1/O2/O3 here**.

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**: `./scripts/process/build.sh`
2. **Integration Tests**: `./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify 'TestReconcile_SessionEndGitSupplement|TestE2E_.*Artifact|TestCodexE2E_SystemArtifact'`

## Documentation

- `ideas/000-SystemArtifact-ListLimits.md` の「デフォルト30/最大100」記述に、**「契約の正は 001 レビュー決定（未指定100・ハード上限なし）へ移行」** の注記を1段落追加する。
- README のページネーション説明の本更新は Part3 に委譲。
