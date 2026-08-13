# 003-SystemArtifact-FullListFixes-Part2-ReconcileArchive

> **Source Specification**: `prompts/phases/000-foundation/branches/check-atrifact-limitation/ideas/001-SystemArtifact-FullListFixes.md`  
> **Depends on**: `002-SystemArtifact-FullListFixes-Part1-StorePagination.md`（`ListAllSystemArtifacts` / `ListAllUserArtifacts`）

## Goal Description

1. **R1 / R6**: `RunSessionReconciliation` が ExistingEvents をページネーションに依存せず全件読み、70件超でも既知キーを supplemental しない
2. **R2**: System Archive の glob 展開を全件収集に変更
3. **O1（任意だが本 Part で実施）**: User Archive の glob も同様に全件化（現状 `PerPage: 100` 固定）

## User Review Required

User Archive（O1）を本 Part に含める方針。外したい場合は指示。

## Requirement Traceability

| Requirement (from Spec 001) | Implementation Point (Section/File) |
| :--- | :--- |
| R1 ExistingEvents 全件 | Proposed Changes > reconcile.go |
| R6 70超でも二重登録なし | Proposed Changes > reconcile_test.go + tests/ |
| R2 Archive glob 全件 | Proposed Changes > api/system.go |
| O1 User Archive 全件 | Proposed Changes > api/user.go |

## Proposed Changes

### Unit tests first (TDD)

#### [MODIFY] [shared/libs/go/artifact/analyzer/reconcile_test.go](file://shared/libs/go/artifact/analyzer/reconcile_test.go)

*   **Description**: ExistingEvents がデフォルトページを超えても既知キーが supplemental に出ないことを検証。
*   **Logic**:

```go
func TestRunSessionReconciliation_ExistingEventsBeyondDefaultPage_NoDup(t *testing.T) {
    // git workDir: commit baseline, then create 70 tracked files and modify them
    // OR: pre-seed ArtifactStore with 70 Write events for keys bulk/f_000.txt.., then
    //     make same paths appear in DetectGitChanges as update
    // After RunSessionReconciliation:
    //   ListAllSystemArtifacts(session) should NOT gain extra reconcile:git rows for those 70 keys
    // Control: 1 untracked-only file with no prior event → exactly one reconcile:git create
}
```

実装が難しい場合の代替（単体で Reconcile 関数を直接叩く）:

```go
func TestReconcile_SkipsRealtimeKeysEvenWhenManyExist(t *testing.T) {
    // Build ReconcileInput with 70 ExistingEvents (tool Write) + GitChanges for same 70 paths
    // + 1 extra git-only path
    // Expect supplements length == 1 (the extra only)
}
```

**推奨**: まず `TestReconcile_*`（純関数）でロジック固定し、`RunSessionReconciliation` 結合は `ListAll` 呼び出しに差し替えたことのスモークを追加。

#### [MODIFY] [shared/libs/go/artifact/api/system_test.go](file://shared/libs/go/artifact/api/system_test.go)

*   **Description**: Archive glob が 120 キーを欠落なく ZIP に含める。
*   **Logic**:

```go
func TestSystemAPI_Archive_GlobMoreThan100(t *testing.T) {
    // seed 120 create events with keys arch/file_%03d.go and real temp files on disk
    // POST /archive {"q":"arch/**"}
    // unzip response → 120 files
}
```

#### [MODIFY] [shared/libs/go/artifact/api/user_test.go](file://shared/libs/go/artifact/api/user_test.go)

*   **Description**: User archive glob >100 も同様（O1）。

### Reconcile

#### [MODIFY] [shared/libs/go/artifact/analyzer/reconcile.go](file://shared/libs/go/artifact/analyzer/reconcile.go)

*   **Description**: ExistingEvents 取得を全件 API に切替。
*   **Technical Design / Logic**:

現状:

```go
page, err := st.ListSystemArtifacts(context.Background(), store.SystemArtifactFilter{SessionIDs: []string{sessionID}})
// ...
input := ReconcileInput{
    SessionID:      sessionID,
    ExistingEvents: page.Items,
}
```

改訂後（仕様どおり全イベント）:

```go
existing, err := st.ListAllSystemArtifacts(context.Background(), store.SystemArtifactFilter{
    SessionIDs:     []string{sessionID},
    IncludeDeleted: true, // 削除済みキーも dedup 対象に含める
})
if err != nil {
    return err
}
input := ReconcileInput{
    SessionID:      sessionID,
    ExistingEvents: existing,
}
```

`Reconcile` 本体の優先度ロジック（SourceStructuredTool < Shell < Git < Snapshot）は変更しない。

### System Archive

#### [MODIFY] [shared/libs/go/artifact/api/system.go](file://shared/libs/go/artifact/api/system.go)

*   **Description**: glob 分岐の `PerPage: 100` を撤廃。
*   **Logic**:

現状:

```go
page, _ := h.store.ListSystemArtifacts(r.Context(), store.SystemArtifactFilter{
    Q: req.Q, SessionIDs: req.SessionID, PerPage: 100,
})
for _, e := range page.Items { ... }
```

改訂後:

```go
events, _ := h.store.ListAllSystemArtifacts(r.Context(), store.SystemArtifactFilter{
    Q:          req.Q,
    SessionIDs: req.SessionID,
})
for _, e := range events {
    if _, exists := keys[e.Key]; !exists {
        keys[e.Key] = e.ActualPath
    }
}
```

同一キーが複数イベントの場合は **後勝ち（スライス順）** または OccurredAt 最新を採用。`ListAll` のソートが `occurred_at ASC` 既定ならループ後勝ちで最新 ActualPath になるよう、Sort を明示:

```go
Sort: "occurred_at", Order: "asc"
```

### User Archive (O1)

#### [MODIFY] [shared/libs/go/artifact/api/user.go](file://shared/libs/go/artifact/api/user.go)

*   **Logic**:

```go
artsList, _ := h.store.ListAllUserArtifacts(r.Context(), store.UserArtifactFilter{Q: req.Q})
for _, a := range artsList {
    if _, exists := arts[a.Key]; !exists {
        arts[a.Key] = a
    }
}
```

### Integration test

#### [MODIFY] [tests/reconcile_integration_test.go](file://tests/reconcile_integration_test.go)

*   **Description**: 既存1ファイル補完に加え、多数既存イベント時の非二重化を追加。
*   **Logic**:

```go
func TestReconcile_ManyExistingEvents_NoDuplicateSupplement(t *testing.T) {
    // create session via agentservice test server
    // pre-insert 70 system events for workdir files that also become git updates
    // terminate → reconcile
    // ListAll: count of reconcile:git for those keys == 0
}
```

## Step-by-Step Implementation Guide

1. **Add `TestReconcile_SkipsRealtimeKeysEvenWhenManyExist`** (fail if using paged ExistingEvents simulation).
2. **Switch `RunSessionReconciliation` to `ListAllSystemArtifacts`** with `IncludeDeleted: true`.
3. **Add Archive tests** (120 keys) — fail on current PerPage:100.
4. **Switch system.go / user.go archive glob** to ListAll.
5. **Extend integration test** in `tests/reconcile_integration_test.go`.
6. **Verify** with build + specify.

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**: `./scripts/process/build.sh`
2. **Integration Tests**: `./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify 'TestReconcile_SessionEndGitSupplement|TestReconcile_ManyExistingEvents_NoDuplicateSupplement|TestE2E_.*Artifact'`

## Documentation

コードコメントで「ExistingEvents は ListAll（ページ非依存）」と1行注記。
