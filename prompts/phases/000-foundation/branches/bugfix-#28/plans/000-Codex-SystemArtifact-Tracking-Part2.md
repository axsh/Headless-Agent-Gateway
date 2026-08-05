# 000-Codex-SystemArtifact-Tracking-Part2

> **Source Specification**: `prompts/phases/000-foundation/branches/bugfix-#28/ideas/000-Codex-SystemArtifact-Tracking.md`
>
> **Prerequisite**: Part 1（`000-Codex-SystemArtifact-Tracking-Part1.md`）の Tier 1/2 実装完了

## Goal Description

Part 1 でリアルタイム追跡（Tier 1/2）を実装した後、**セッション終了時リコンシリエーション（Tier 3）** により複数ソースを統合し、セッション単位のユニークな生成・更新ファイル一覧を確定する。あわせて任意要件 O4〜O6（旧 Codex flat 形式、claudecode 複数 tool_use、仕様書 R12 追記）を実装する。

git diff は **補助ソース** として位置づけ、`.gitignore` 対象外という制約をコードコメントおよび仕様に明記する。

## User Review Required

- **Tier 3 の初回リリース同梱可否**: Part 1 のみで Issue #28 は解消可能。Part 2 は follow-up として別 PR でもよい。同梱する場合は `agentservice` セッション終了 hook の変更範囲をレビューすること。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| O1: セッション終了時 merge + dedup | `artifact/analyzer/reconcile.go`, `agentservice` session close hook |
| O2: git diff 補助（gitignore 制約ドキュメント化） | `artifact/analyzer/git_diff.go`, `reconcile.go` |
| O3: structured output 補助 | `reconcile.go` — 低優先度ソースとして merge |
| O4: Codex 旧 flat 形式 `shell` / `shell_command` | `analyzer.go` — `analyzeShellTool` 拡張 |
| O5: claudecode 複数 tool_use emit | `claudecode/protocol.go` |
| O6: `000-AgentFileListAPI.md` R12 に Codex 追記 | `prompts/phases/000-foundation/branches/feat-file-list/ideas/000-AgentFileListAPI.md` |
| O7: 非 git workDir 向け snapshot diff | `artifact/analyzer/snapshot_diff.go`, `reconcile.go` |

## Proposed Changes

### Phase 1 — git diff 検出（O2）

#### [NEW] [shared/libs/go/artifact/analyzer/git_diff_test.go](file://shared/libs/go/artifact/analyzer/git_diff_test.go)

* **Description**: git リポジトリ内での変更検出を単体テスト。`t.TempDir()` 内で `git init` + ファイル操作。
* **Technical Design**:

```go
// GitDiffResult holds paths detected via git commands.
type GitDiffResult struct {
    Path      string
    Operation string // create | update | delete
    Source    string // always "git"
}

// DetectGitChanges runs git diff --name-status HEAD and
// git ls-files --others --exclude-standard in workDir.
// Returns empty slice if workDir is not a git repo (.git missing).
func DetectGitChanges(workDir string) ([]GitDiffResult, error)
```

* **Logic** — テストケース:

| テスト名 | セットアップ | 期待 |
|----------|-------------|------|
| `TestDetectGitChanges_NotARepo` | 非 git ディレクトリ | 空スライス, error nil |
| `TestDetectGitChanges_NewUntrackedFile` | git init → 新規 file 作成（未 add） | create 1 件 |
| `TestDetectGitChanges_ModifiedTrackedFile` | tracked file 編集 | update 1 件 |
| `TestDetectGitChanges_GitignoreExcluded` | `.gitignore` に `tmp/` → `tmp/x.txt` 作成 | **検出されない**（O2 制約） |
| `TestDetectGitChanges_DeletedFile` | tracked file 削除 | delete 1 件 |

* **Logic** — git コマンド（仕様書 §6.2）:

```bash
git diff --name-status HEAD          # tracked ファイルの変更
git ls-files --others --exclude-standard  # untracked（非 ignore）の新規
```

* **Logic** — `--name-status` パース:

| git status letter | Operation |
|-------------------|-----------|
| `A`, `?` (untracked) | create |
| `M` | update |
| `D` | delete |

#### [NEW] [shared/libs/go/artifact/analyzer/git_diff.go](file://shared/libs/go/artifact/analyzer/git_diff.go)

* **Description**: WorkDir が git リポジトリの場合のみ git diff を実行。
* **Logic**:
  1. `filepath.Join(workDir, ".git")` 存在確認 — なければ空返却
  2. `exec.Command("git", "-C", workDir, "diff", "--name-status", "HEAD")` 実行
  3. `exec.Command("git", "-C", workDir, "ls-files", "--others", "--exclude-standard")` 実行
  4. 結果を `[]GitDiffResult` に正規化
  5. パッケージ doc comment に **`.gitignore` 対象は検出不可** を明記（O2）

---

### Phase 2 — 非 git snapshot diff（O7）

#### [NEW] [shared/libs/go/artifact/analyzer/snapshot_diff_test.go](file://shared/libs/go/artifact/analyzer/snapshot_diff_test.go)

* **Description**: セッション開始時と終了時のディレクトリ walk diff。
* **Technical Design**:

```go
// DirSnapshot captures file paths and sizes at a point in time.
type DirSnapshot struct {
    Files map[string]int64 // relative path → size
}

// TakeSnapshot walks workDir (skipping .git) and records relative paths.
func TakeSnapshot(workDir string) (DirSnapshot, error)

// DiffSnapshots returns create/update/delete ops between before and after.
func DiffSnapshots(before, after DirSnapshot) []ParsedFileOp
```

* **Logic** — テスト:
  - 新規 file → create
  - サイズ変更 → update
  - 削除 → delete
  - セッション外で同時に変更された file の **混入リスク** を doc comment に記載（O7）

#### [NEW] [shared/libs/go/artifact/analyzer/snapshot_diff.go](file://shared/libs/go/artifact/analyzer/snapshot_diff.go)

* **Description**: git 非対応 workDir（`t.TempDir()` 等）向け補助。

---

### Phase 3 — Reconciler（O1, O3）

#### [NEW] [shared/libs/go/artifact/analyzer/reconcile_test.go](file://shared/libs/go/artifact/analyzer/reconcile_test.go)

* **Description**: merge + dedup アルゴリズムの単体テスト（git / LLM 非依存）。
* **Technical Design**:

```go
// ReconcileSource identifies where a file op came from.
type ReconcileSource int

const (
    SourceStructuredTool ReconcileSource = 1 // Tier 1: file_change, Write, Edit, NotebookEdit
    SourceShellParser    ReconcileSource = 2 // Tier 2: command_execution, Bash
    SourceGitDiff        ReconcileSource = 3 // Tier 3: git
    SourceSnapshot       ReconcileSource = 4 // Tier 3: snapshot diff
    SourceStructuredOut  ReconcileSource = 5 // Tier 3: LLM structured output
)

// ReconcileInput aggregates all sources for one session.
type ReconcileInput struct {
    SessionID       string
    ExistingEvents  []store.SystemArtifactEvent // Tier 1/2 realtime
    GitChanges      []GitDiffResult             // optional
    SnapshotChanges []ParsedFileOp              // optional
    StructuredPaths []ParsedFileOp              // optional (O3)
}

// Reconcile merges inputs, deduplicates by key, applies priority, returns events to save.
func Reconcile(in ReconcileInput, workDirResolver WorkDirResolver, projectRoot string) []store.SystemArtifactEvent
```

* **Logic** — 統合アルゴリズム（仕様書 §6.3）:

1. **収集**: `ExistingEvents` + git + snapshot + structured output
2. **正規化**: path を WorkDir 相対 key に統一
3. **ユニーク化**: 同一 `key` に対し **最高優先度ソース** の operation を採用

| 優先度 | ソース |
|--------|--------|
| 1 | Tier 1: `file_change`, `Write`, `Edit`, `MultiEdit`, `NotebookEdit` |
| 2 | Tier 2: `command_execution`, `Bash` |
| 3 | git diff |
| 4 | snapshot diff |
| 5 | structured output |

4. **補完登録**: git / structured output のみに存在し Tier 1/2 に無い key を追加（`ToolName`: `reconcile:git` / `reconcile:structured` / `reconcile:snapshot`）
5. **重複イベント**: 同一 session + key + operation の重複は最新 `occurred_at` を残す

* **Logic** — テストケース:

| テスト名 | 入力 | 期待 |
|----------|------|------|
| `TestReconcile_Tier1WinsOverGit` | Tier1 create + git create 同一 key | Tier1 採用 |
| `TestReconcile_GitFillsGap` | Tier1 なし + git create | `reconcile:git` イベント追加 |
| `TestReconcile_GitignoreNotInGit` | git 空 + Tier1 create for tmp/x | Tier1 のみ |
| `TestReconcile_StructuredOutputLowestPriority` | structured + git 同一 key | git 採用 |
| `TestReconcile_DedupSameSource` | 2 件同一 key+op | 最新 occurred_at 残す |

#### [NEW] [shared/libs/go/artifact/analyzer/reconcile.go](file://shared/libs/go/artifact/analyzer/reconcile.go)

* **Description**: `Reconcile` 実装。Part 1 の `resolvePath` / `toRelativePath` ロジックを Reconciler 内で再利用するか、package-level ヘルパとして抽出。

---

### Phase 4 — agentservice セッション終了 hook（O1）

#### [MODIFY] agentservice セッション Close 処理（配置は実装時に `grep CloseSession` で特定）

* **Description**: セッション `completed` / `CloseSession` 時に Reconciler を呼び出す。
* **Technical Design**:

```go
// On session close:
// 1. existing := store.ListSystemArtifacts(sessionID)
// 2. gitChanges := DetectGitChanges(workDir) // if git repo
// 3. snapshotChanges := DiffSnapshots(startSnapshot, TakeSnapshot(workDir)) // if snapshot taken
// 4. newEvents := Reconcile(ReconcileInput{...})
// 5. for each event not already in store: SaveSystemArtifactEvent
```

* **Logic**:
  - セッション **開始時** に `TakeSnapshot(workDir)` を保持（O7 用、git 非対応時のみ）
  - git リポジトリの場合は snapshot は **スキップ**（git の方が信頼性高い）
  - structured output（O3）は **初回 follow-up では未実装でも可** — hook ポイントのみ用意

---

### Phase 5 — 任意拡張（O4, O5, O6）

#### [MODIFY] [shared/libs/go/artifact/analyzer/analyzer.go](file://shared/libs/go/artifact/analyzer/analyzer.go)

* **Description**: O4 — Codex 旧 flat 形式 `shell` / `shell_command` をシェルパーサー経由で処理。
* **Logic**:
  - `analyzeShellTool` で `ToolName` が `shell` / `shell_command` の場合、`ToolInput["arguments"]` を JSON parse し `command` フィールドを抽出 → `ParseShellCommand`

#### [MODIFY] [shared/libs/go/codingagent/claudecode/protocol.go](file://shared/libs/go/codingagent/claudecode/protocol.go)

* **Description**: O5 — 1 行に複数 `tool_use` ブロックがある場合、すべて emit。
* **Logic**: 現状最初の 1 件のみ返す箇所を特定し、複数 `StreamEvent` を順次 TaskLog に流すよう process 層を調整（`ParseJSONLinesEvent` の戻り値が単一の場合は process.go 側で slice 対応）。

#### [MODIFY] [prompts/phases/000-foundation/branches/feat-file-list/ideas/000-AgentFileListAPI.md](file://prompts/phases/000-foundation/branches/feat-file-list/ideas/000-AgentFileListAPI.md)

* **Description**: O6 — R12 対応 Agent 一覧に Codex を追記。

---

## Step-by-Step Implementation Guide

Part 1 完了後:

1. **Git Diff RED**: Create `git_diff_test.go`. `./scripts/process/build.sh` — **FAIL**.
2. **Git Diff GREEN**: Create `git_diff.go`. Build — **PASS**.
3. **Snapshot RED/GREEN**: Create `snapshot_diff_test.go` + `snapshot_diff.go`.
4. **Reconcile RED**: Create `reconcile_test.go` with merge priority cases. Build — **FAIL**.
5. **Reconcile GREEN**: Create `reconcile.go`. Build — **PASS**.
6. **Session Hook**: Wire Reconciler into agentservice session close. Integration test with fake session.
7. **O4/O5/O6**: Optional extensions per priority.

## Verification Plan

### Automated Verification

1. **Unit Tests**:

```bash
./scripts/process/build.sh
```

2. **Reconcile Integration** (agentservice session close):

```bash
./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories integration --specify "TestReconcile"
```

（統合テスト名は実装時に `tests/` 配下へ追加。git init + session lifecycle で reconcile を検証）

3. **Part 1 Regression**:

```bash
./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories llm --specify "TestCodexE2E_SystemArtifact_FileCreation"
```

### Acceptance Criteria

- [x] `DetectGitChanges` が gitignore 除外を正しく扱う（単体テスト PASS）
- [x] `Reconcile` が優先度付き merge + dedup を正しく行う
- [x] セッション終了後、git のみ捕捉できた path が `reconcile:git` として補完される
- [x] Part 1 の E2E / 単体テストに退行なし

## Documentation

- `git_diff.go` および `reconcile.go` の package doc に **gitignore 盲点** と **セッション外変更混入リスク** を記載（O2, O7）
- O6: `000-AgentFileListAPI.md` R12 更新
