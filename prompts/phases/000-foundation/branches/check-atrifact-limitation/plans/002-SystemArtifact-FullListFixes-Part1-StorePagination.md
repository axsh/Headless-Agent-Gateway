# 002-SystemArtifact-FullListFixes-Part1-StorePagination

> **Source Specification**: `prompts/phases/000-foundation/branches/check-atrifact-limitation/ideas/001-SystemArtifact-FullListFixes.md`  
> **Depends on**: `001-SystemArtifact-ListLimits-Baseline.md`（推奨）  
> **Unblocks**: Part2 Reconcile/Archive, Part3 Client ListAll

## Goal Description

001 仕様の Store / ページネーション基盤を実装する。

1. **R7**: `normalizePerPage` — 未指定時デフォルト **100**、明示正整数はクランプしない
2. **全件読込ヘルパー**: `ListAllSystemArtifacts`（および User 側対称）を Store に追加（R1/R2 の前提）
3. **R4/R5**: 70 update × 明示 `per_page=30` の3ページ（先頭/中間/末尾）自動テスト

## User Review Required

- `limit` クエリエイリアスは **追加しない**（既存 `per_page` のみ。ドキュメントで limit と同義と説明する）。異論があれば Part3 前に指示。
- 全件ヘルパーは「ページ無視で全件スライスを返す」方式を採用（`-1` センチネルは使わない）。

## Requirement Traceability

| Requirement (from Spec 001) | Implementation Point (Section/File) |
| :--- | :--- |
| R7 未指定100・明示尊重 | Proposed Changes > store.go `normalizePerPage` + models コメント |
| R4/R5 70 update 3ページ | Proposed Changes > store_test.go / system_test.go |
| R1/R2 の前提（全件読込手段） | Proposed Changes > `ListAllSystemArtifacts` / `ListAllUserArtifacts` |
| R1–R3 本体 | Part2 / Part3 |

## Proposed Changes

### Models

#### [MODIFY] [shared/libs/go/artifact/store/models.go](file://shared/libs/go/artifact/store/models.go)

*   **Description**: `PerPage` コメントをレビュー決定に合わせて更新。
*   **Technical Design**:

```go
// SystemArtifactFilter ...
PerPage int // 0 or negative → default 100 (safety); positive → honored as-is (no hard max)
```

UserArtifactFilter も同様。

### Store interface + implementation

#### [MODIFY] [shared/libs/go/artifact/store/store.go](file://shared/libs/go/artifact/store/store.go)

*   **Description**: ページネーション正規化の改訂と全件 API 追加。
*   **Technical Design**:

```go
type ArtifactStore interface {
    // ... existing methods ...
    ListSystemArtifacts(ctx context.Context, f SystemArtifactFilter) (*SystemArtifactPage, error)
    // ListAllSystemArtifacts returns all matching events; Page/PerPage are ignored.
    ListAllSystemArtifacts(ctx context.Context, f SystemArtifactFilter) ([]SystemArtifactEvent, error)
    ListUserArtifacts(ctx context.Context, f UserArtifactFilter) (*UserArtifactPage, error)
    ListAllUserArtifacts(ctx context.Context, f UserArtifactFilter) ([]UserArtifact, error)
}

const DefaultPerPage = 100

func normalizePerPage(n int) int {
    if n <= 0 {
        return DefaultPerPage // 100
    }
    return n // no upper clamp
}
```

*   **Logic**:
    - `ListSystemArtifacts` のフィルタ/glob/`excludeDeletedKeys` ロジックは現状維持。変更は `normalizePerPage` のみ。
    - `ListAllSystemArtifacts`: `ListSystemArtifacts` と同じ WHERE/glob/delete 除外を適用したうえで、オフセット分割せず全件を返す。実装方針: 内部で既存の全件スキャン経路を共有し、ページスライスをスキップするプライベート関数 `listSystemArtifactsFiltered(...) ([]SystemArtifactEvent, error)` に抽出し、List はそこから slice、ListAll は全件返却。
    - `ListAllUserArtifacts` も同様。

### Unit tests (TDD — 先に書く)

#### [MODIFY] [shared/libs/go/artifact/store/store_test.go](file://shared/libs/go/artifact/store/store_test.go)

*   **Description**: R7 と R4/R5 の単体テスト。
*   **Logic**（テーブル駆動）:

```go
func TestNormalizePerPage_Default100_NoHardMax(t *testing.T) {
    // via ListSystemArtifacts behavior:
    // PerPage 0 → page.PerPage == 100
    // PerPage 200 with 150 seeded → len(items)==150, PerPage==200
}

func TestListSystemArtifacts_SeventyUpdates_ThreePages(t *testing.T) {
    // For i in 0..69: Save create + update on key updated/file_%03d.go
    // Filter Operation=update, Sort=key, Order=asc, PerPage=30
    // page1: 30, page2: 30, page3: 10, page4: 0
    // merge unique == 70
    // p1[last] < p2[0] < p3[0] (key order)
}

func TestListAllSystemArtifacts_ReturnsMoreThanDefaultPage(t *testing.T) {
    // seed 140 events (70 create + 70 update), ListAll with SessionIDs → len==140
    // List with default PerPage → len==100, TotalCount==140
}
```

#### [MODIFY] [shared/libs/go/artifact/api/system_test.go](file://shared/libs/go/artifact/api/system_test.go)

*   **Description**: HTTP でもデフォルト100・明示200・70件3ページを検証。
*   **Logic**:

```go
func TestSystemAPI_List_DefaultPerPage100(t *testing.T) { /* seed 120; GET without per_page → items 100, total 120 */ }
func TestSystemAPI_List_PerPage200_NoClamp(t *testing.T) { /* seed 150; per_page=200 → items 150 */ }
func TestSystemAPI_List_SeventyUpdates_ThreePages(t *testing.T) { /* same as store, via HTTP */ }
```

#### [MODIFY] [shared/libs/go/artifact/api/user_test.go](file://shared/libs/go/artifact/api/user_test.go)（必要なら）

*   **Description**: User List のデフォルト100回帰（対称性）。

### memStore / fakes

analyzer / mcp テストの `memStore` が `ArtifactStore` を実装している場合、`ListAll*` を追加（スタブで `List` をページ送り、または全件保持スライスを返す）。

#### [MODIFY] 該当テスト用フェイク（`analyzer_test.go` 等で interface 実装しているもの）

*   **Description**: コンパイル通しのため `ListAllSystemArtifacts` / `ListAllUserArtifacts` を追加。

## Step-by-Step Implementation Guide

1. **Write failing tests** for `TestNormalizePerPage_Default100_NoHardMax` and `TestListSystemArtifacts_SeventyUpdates_ThreePages`（現行は default30 / clamp100 のため失敗する）。
2. **Change `normalizePerPage`** and model comments; run `./scripts/process/build.sh` until R7 tests pass.
3. **Extract filtered list helper** and implement `ListAllSystemArtifacts` / `ListAllUserArtifacts`; add interface methods; fix fakes.
4. **Add `TestListAllSystemArtifacts_ReturnsMoreThanDefaultPage`** and HTTP API tests; make green.
5. **Commit** store/api changes as a unit（ユーザー指示時）。

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**: `./scripts/process/build.sh`
2. **Integration Tests**: `./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify 'TestReconcile_SessionEndGitSupplement|TestE2E_.*Artifact'`

（70件3ページの統合恒久化は Part3 の `tests/` 追加でも可。本 Part ではパッケージテスト必須。）

## Documentation

- `models.go` コメント更新のみ。README は Part3。
