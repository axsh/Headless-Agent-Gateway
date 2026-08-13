# 004-SystemArtifact-FullListFixes-Part3-ClientListAll

> **Source Specification**: `prompts/phases/000-foundation/branches/check-atrifact-limitation/ideas/001-SystemArtifact-FullListFixes.md`  
> **Depends on**: `002-SystemArtifact-FullListFixes-Part1-StorePagination.md`（API デフォルト100）  
> **Recommended after**: `003-SystemArtifact-FullListFixes-Part2-ReconcileArchive.md`

## Goal Description

1. **R3**: `client/v1` に System / User Artifact の `ListAll` ヘルパーを追加
2. 70 update・3ページおよび `ListAll` の統合テストを `tests/` に恒久化
3. README / 先行仕様コメントのページネーション記述をレビュー決定に合わせて更新

## User Review Required

- `client/` ルート（非 v1）に同名 API がある場合は v1 のみ必須とし、非 v1 は存在すれば追従。現状 System Artifact は `client/v1` が正。

## Requirement Traceability

| Requirement (from Spec 001) | Implementation Point (Section/File) |
| :--- | :--- |
| R3 ListAll ヘルパー | Proposed Changes > client/v1/artifacts.go |
| R4/R5 70件3ページの恒久化 | Proposed Changes > tests/artifact_pagination_test.go |
| R7 ドキュメント整合 | Documentation |
| O2 進捗コールバック | **対象外**（任意のまま） |

## Proposed Changes

### Client unit tests (TDD)

#### [MODIFY] [client/v1/artifacts_test.go](file://client/v1/artifacts_test.go)

*   **Description**: httptest スタブで複数ページを返し、`ListAll` が結合することを検証。
*   **Logic**:

```go
func TestSystemArtifactClient_ListAll_WalksPages(t *testing.T) {
    // Stub GET /api/v1/artifacts/system:
    //   page=1 per_page default → 100 items, total_count=170
    //   page=2 → 70 items
    // ListAll → len==170, Page field ignored even if set on filter
}

func TestSystemArtifactClient_ListAll_ContextCancel(t *testing.T) {
    // cancel before second page → error
}

func TestUserArtifactClient_ListAll_WalksPages(t *testing.T) {
    // symmetric for /api/v1/artifacts/user
}
```

### Client implementation

#### [MODIFY] [client/v1/artifacts.go](file://client/v1/artifacts.go)

*   **Description**: ListAll 追加。
*   **Technical Design**:

```go
// ListAll collects all system artifact events matching f by walking pages.
// f.Page is ignored. If f.PerPage <= 0, each request omits per_page so the
// server default (100) applies. Stops when a page returns fewer than PerPage
// items (or empty), or when len(collected) >= total_count from the first page.
func (sc *SystemArtifactClient) ListAll(ctx context.Context, f SystemArtifactFilter) ([]SystemArtifactItem, error) {
    var all []SystemArtifactItem
    pageNum := 1
    for {
        if err := ctx.Err(); err != nil {
            return nil, err
        }
        cf := f
        cf.Page = pageNum
        // leave PerPage as caller set (0 → server default)
        page, err := sc.List(ctx, cf)
        if err != nil {
            return nil, err
        }
        all = append(all, page.Items...)
        if len(page.Items) == 0 || len(all) >= page.TotalCount {
            break
        }
        pageNum++
    }
    return all, nil
}

func (uc *UserArtifactClient) ListAll(ctx context.Context, f UserArtifactFilter) ([]UserArtifactItem, error) {
    // same pattern
}
```

*   **Logic**: 無限ループ防止として `pageNum` 上限（例: TotalCount/perPage+2、または 10000 ページ）を設けてよい。

### Integration / E2E style tests under tests/

#### [NEW] [tests/artifact_pagination_test.go](file://tests/artifact_pagination_test.go)

*   **Description**: 実 Store + HTTP（agentservice または api handler）で 70 update 3ページと ListAll を検証。
*   **Logic**:

```go
func TestSystemArtifact_SeventyUpdatePages(t *testing.T) {
    // NewSQLiteStore + SystemArtifactHandler mux
    // seed 70 create+update
    // GET page=1,2,3 with per_page=30&operation=update&sort=key&order=asc
    // assert lengths 30/30/10 and unique 70
}

func TestSystemArtifact_ListAll_Client(t *testing.T) {
    // httptest with handler + v1.New(ts.URL)
    // seed 120 events
    // SystemArtifacts().ListAll(ctx, filter) → 120
}
```

パッケージ名は既存 `tests` の `llm_test` に合わせる。

### Optional: examples

#### [MODIFY] [examples/artifact-pipeline/main.go](file://examples/artifact-pipeline/main.go)（任意）

*   **Description**: List 後に不足がある場合のコメントを `ListAll` 推奨に更新。必須ではない。

## Step-by-Step Implementation Guide

1. **Add failing client tests** for `ListAll`.
2. **Implement `ListAll`** on System and User clients.
3. **Add `tests/artifact_pagination_test.go`** with SeventyUpdatePages + ListAll.
4. **Update README** Artifact section: default 100, no hard max, show ListAll example.
5. **Update idea 000 note** if not done in Baseline plan.
6. **Full verification**.

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**: `./scripts/process/build.sh`
2. **Integration Tests**: `./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify 'TestSystemArtifact_SeventyUpdatePages|TestSystemArtifact_ListAll_Client|TestReconcile_SessionEndGitSupplement|TestE2E_.*Artifact|TestCodexE2E_SystemArtifact'`

## Documentation

#### [MODIFY] [README.md](file://README.md)

*   **Description**: Artifact API Examples の List 例を更新。

```go
// Default page size is 100 when PerPage is omitted (safety limit).
// Explicit PerPage is honored without a hard maximum.
page, _ := c.SystemArtifacts().List(ctx, client.SystemArtifactFilter{
    SessionIDs: []string{"sess-abc123"},
    PerPage:    200,
})

// Or collect every page:
items, _ := c.SystemArtifacts().ListAll(ctx, client.SystemArtifactFilter{
    SessionIDs: []string{"sess-abc123"},
})
```

`prompts/phases/.../ideas/000-SystemArtifact-ListLimits.md` に 001 契約への参照注記（Baseline で未実施ならここで実施）。
