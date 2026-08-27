# 000-File-Change-Collector-Algorithms

> **Source Specification**: `prompts/phases/001-phase02/branches/feat-updated-files-detection/ideas/000-File-Change-Collector-Algorithms.md`

## Goal Description

セッション作成時に `file_change_collectors` で System Artifact 収集アルゴリズム（`structured_tool` / `shell_parser` / `workdir_reconcile`）を個別 ON/OFF できるようにする。省略時既定は Tier1–2 ON / Tier3 OFF。Client SDK・README・Reference Manual・実行可能 example まで含める。

## User Review Required

None.（仕様レビュー済み。既定値・examples/README 反映済み）

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1 アルゴリズム ID | Proposed Changes > codingagent/file_change_collectors.go |
| R2 CreateSession ON/OFF・既定・部分上書き・未知キー 400 | Proposed Changes > agentservice/handler.go + codingagent Resolve |
| R3 GET で解決済み 3 キー明示 | Proposed Changes > SessionRecord + handler_session.go |
| R4 実行時ゲート Tier1/2/3 | Proposed Changes > analyzer.go + artifact_reconcile.go |
| R5 client/v1 | Proposed Changes > client/v1/session.go |
| R6 PATCH 非対応 | 変更なし（UpdateSessionRequest にフィールドを追加しない） |
| R7 README + ReferenceManual | Proposed Changes > Documentation files |
| R8 reconcile IT に明示 ON / 破壊的変更 | Proposed Changes > tests/reconcile_integration_test.go |
| R9 examples | Proposed Changes > examples/file-change-collectors/ + artifact-pipeline |

## Proposed Changes

### codingagent — types & resolve

#### [NEW] [shared/libs/go/codingagent/file_change_collectors_test.go](file://shared/libs/go/codingagent/file_change_collectors_test.go)

*   **Description**: Resolve / Effective / Defaults のテーブル駆動テスト（TDD 先書き）。
*   **Cases**:
    *   `nil` / 空 raw → `{true,true,false}`
    *   `{"workdir_reconcile":true}` → `{true,true,true}`
    *   `{"structured_tool":false}` → `{false,true,false}`
    *   全 false → 全 false
    *   未知キー → error（メッセージに許可キー名）
    *   非 object / 非 boolean → error
    *   `EffectiveFileChangeCollectors` ゼロ値 → 既定と同じ

#### [NEW] [shared/libs/go/codingagent/file_change_collectors.go](file://shared/libs/go/codingagent/file_change_collectors.go)

*   **Description**: アルゴリズム定数・解決済み構造体・JSON 解決。
*   **Technical Design**:

```go
const (
    CollectorStructuredTool   = "structured_tool"
    CollectorShellParser      = "shell_parser"
    CollectorWorkdirReconcile = "workdir_reconcile"
)

// FileChangeCollectors is the resolved per-session collector config.
// Always serialize all three keys (no omitempty on bools).
type FileChangeCollectors struct {
    StructuredTool   bool `json:"structured_tool"`
    ShellParser      bool `json:"shell_parser"`
    WorkdirReconcile bool `json:"workdir_reconcile"`
}

func DefaultFileChangeCollectors() FileChangeCollectors {
    return FileChangeCollectors{
        StructuredTool:   true,
        ShellParser:      true,
        WorkdirReconcile: false,
    }
}

// ResolveFileChangeCollectors applies per-key defaults (partial override).
// raw == nil or len==0 or JSON null → defaults.
// Object: only known keys; missing keys keep DefaultFileChangeCollectors values;
// unknown keys → error; non-bool values → error.
func ResolveFileChangeCollectors(raw json.RawMessage) (FileChangeCollectors, error)

// EffectiveFileChangeCollectors returns cfg, or defaults when all-false-looking
// zero from legacy records that omit the field entirely.
// Legacy: empty/missing JSON field unmarshals to zero value {false,false,false}.
// Distinction: we store resolved values at Create time always, so Effective is for
// old record.json without the field → treat as DefaultFileChangeCollectors().
// Use a presence flag OR detect "never set": prefer storing always at Create.
// Effective: if record was created before this feature, JSON omit means zero.
// Spec R8: Tier1/2 should stay ON for legacy. So Effective maps zero-value
// (all false AND we need a sentinel) — use pointer on SessionRecord OR
// treat {false,false,false} from missing field same as default? Spec says
// explicit all-false is valid. Therefore SessionRecord must use a pointer
// *FileChangeCollectors OR a bool FileChangeCollectorsSet.
//
// Decision (fixed): SessionRecord holds FileChangeCollectors value always set
// at Create via Resolve. For legacy records where field is absent (all false
// zero), EffectiveFileChangeCollectors(c, present bool) — simpler approach:
// SessionRecord uses `FileChangeCollectors *FileChangeCollectors` with
// json omitempty; Effective returns Default when nil; when non-nil use *c
// (including all-false).
```

*   **Logic（Resolve）**:
    1. `raw` 空 / `null` → `DefaultFileChangeCollectors()`
    2. `json.Unmarshal` into `map[string]json.RawMessage`
    3. 各キーが `structured_tool` / `shell_parser` / `workdir_reconcile` 以外なら error: `unknown file_change_collectors key: %q (allowed: structured_tool, shell_parser, workdir_reconcile)`
    4. out := Default…; 各キーが存在すれば `json.Unmarshal` を bool に（失敗なら error）
    5. return out

*   **Effective**:

```go
func EffectiveFileChangeCollectors(c *FileChangeCollectors) FileChangeCollectors {
    if c == nil {
        return DefaultFileChangeCollectors()
    }
    return *c
}
```

#### [MODIFY] [shared/libs/go/codingagent/session_store.go](file://shared/libs/go/codingagent/session_store.go)

*   **Description**: `SessionRecord` にコレクタ設定を追加。
*   **Technical Design**:

```go
// FileChangeCollectors is nil for legacy records (treat as Default via Effective).
FileChangeCollectors *FileChangeCollectors `json:"file_change_collectors,omitempty"`
```

### agentservice — Create / GET / reconcile gate

#### [NEW] [shared/libs/go/agentservice/file_change_collectors_test.go](file://shared/libs/go/agentservice/file_change_collectors_test.go)

*   **Description**: HTTP CreateSession のパース・400・GET 解決値（httptest）。
*   **Cases (U1/S5/S6)**:
    *   フィールド省略 → GET で `{true,true,false}`
    *   `workdir_reconcile: true` のみ → GET で `{true,true,true}`
    *   未知キー → 400
    *   非 boolean → 400
    *   全 false → GET で全 false

#### [MODIFY] [shared/libs/go/agentservice/handler.go](file://shared/libs/go/agentservice/handler.go)

*   **Description**: CreateSession で `file_change_collectors` を受け取り解決して Record に保存。
*   **Technical Design**: リクエスト匿名 struct に追加:

```go
FileChangeCollectors json.RawMessage `json:"file_change_collectors"`
```

*   **Logic**:
    1. `resolved, err := codingagent.ResolveFileChangeCollectors(req.FileChangeCollectors)`
    2. err → `http.Error(w, err.Error(), 400)`
    3. `record.FileChangeCollectors = &resolved`（常にポインタで保存）
    4. Debug log に 3 フラグを出す

#### [MODIFY] [shared/libs/go/agentservice/handler_session.go](file://shared/libs/go/agentservice/handler_session.go)

*   **Description**: GET/List で常に解決済み 3 キーを返す。
*   **Logic** in `sessionResponse`:

```go
eff := codingagent.EffectiveFileChangeCollectors(resp.FileChangeCollectors)
resp.FileChangeCollectors = &eff
```

#### [MODIFY] [shared/libs/go/agentservice/artifact_reconcile.go](file://shared/libs/go/agentservice/artifact_reconcile.go)

*   **Description**: Tier3 ゲート（R4）。
*   **Logic**:
    *   `captureTurnSnapshot`: sessions.Get → `Effective…().WorkdirReconcile == false` なら return
    *   `reconcileSessionArtifacts`: 同様に false なら return（既存の artifactStore nil チェックの直後）

#### [MODIFY] [shared/libs/go/agentservice/service.go](file://shared/libs/go/agentservice/service.go)

*   **Description**: Analyzer に `CollectorConfigResolver` を注入（案 A）。
*   **Logic**:

```go
analyzer.New(s.taskLog, s.artifactStore, s.artifactWorkDir,
    func(sessionID string) string { ... WorkDir ... },
    func(sessionID string) codingagent.FileChangeCollectors {
        if rec, err := s.sessions.Get(sessionID); err == nil {
            return codingagent.EffectiveFileChangeCollectors(rec.FileChangeCollectors)
        }
        return codingagent.DefaultFileChangeCollectors()
    },
)
```

### artifact/analyzer — Tier1/2 gate

#### [MODIFY] [shared/libs/go/artifact/analyzer/analyzer_test.go](file://shared/libs/go/artifact/analyzer/analyzer_test.go)

*   **Description**: U2/U3 — `structured_tool` OFF で Write/file_change が保存されない；`shell_parser` OFF で Bash が保存されない；両方 ON は従来どおり。
*   **Logic**: New に collector resolver を渡し、memStore の件数を assert。

#### [MODIFY] [shared/libs/go/artifact/analyzer/analyzer.go](file://shared/libs/go/artifact/analyzer/analyzer.go)

*   **Description**: Collector resolver 追加と analyzeEvents ゲート。
*   **Technical Design**:

```go
type CollectorConfigResolver func(sessionID string) codingagent.FileChangeCollectors

type ToolCallAnalyzer struct {
    ...
    collectorResolver CollectorConfigResolver // may be nil → Default
}

func New(tl *tasklog.TaskLog, s store.ArtifactStore, projectRoot string,
    workDirResolver WorkDirResolver,
    collectorResolver CollectorConfigResolver) *ToolCallAnalyzer

func (a *ToolCallAnalyzer) collectorsFor(sessionID string) codingagent.FileChangeCollectors {
    if a.collectorResolver != nil {
        return a.collectorResolver(sessionID)
    }
    return codingagent.DefaultFileChangeCollectors()
}
```

*   **Logic in analyzeEvents**:
    *   `cfg := a.collectorsFor(sessionID)`
    *   `file_change` / `analyzeMappedTool`: `if !cfg.StructuredTool { return nil }`
    *   shell tools: `if !cfg.ShellParser { return nil }`
*   **Call sites of New**: `service.go` と既存テストをすべて更新（第5引数。テストで不要なら `nil`）。

### client/v1

#### [MODIFY] [client/v1/session_test.go](file://client/v1/session_test.go)

*   **Description**: U5 — Create で `file_change_collectors` がマージャルされる／省略時キーなし；GET でデコード。

#### [MODIFY] [client/v1/session.go](file://client/v1/session.go)

*   **Description**: 型追加。

```go
// FileChangeCollectors configures System Artifact collection algorithms.
// Nil pointer fields mean "omit from JSON" (server applies per-key defaults).
type FileChangeCollectors struct {
    StructuredTool   *bool `json:"structured_tool,omitempty"`
    ShellParser      *bool `json:"shell_parser,omitempty"`
    WorkdirReconcile *bool `json:"workdir_reconcile,omitempty"`
}

// Helpers (optional):
func BoolPtr(v bool) *bool { return &v }

// On SessionRequest:
FileChangeCollectors *FileChangeCollectors `json:"file_change_collectors,omitempty"`

// On SessionInfo — resolved form always present when server sends it.
// Use same struct with *bool OR a ResolvedFileChangeCollectors with plain bools.
// Prefer plain bools for SessionInfo so all keys always decode:
type FileChangeCollectorsInfo struct {
    StructuredTool   bool `json:"structured_tool"`
    ShellParser      bool `json:"shell_parser"`
    WorkdirReconcile bool `json:"workdir_reconcile"`
}
// SessionInfo.FileChangeCollectors *FileChangeCollectorsInfo `json:"file_change_collectors,omitempty"`
```

定数: `CollectorStructuredTool` 等を client にも再エクスポート（文字列定数）。

### tests — integration / E2E

#### [MODIFY] [tests/reconcile_integration_test.go](file://tests/reconcile_integration_test.go)

*   **Description**: R8/I2 — Create ボディに `"file_change_collectors":{"workdir_reconcile":true}` を追加。
*   **Logic**: 両方のテスト（SessionEndGitSupplement 等）で明示 ON。

#### [NEW] [tests/file_change_collectors_e2e_test.go](file://tests/file_change_collectors_e2e_test.go)

*   **Description**: I1/I3 — 省略時は reconcile 補完なし；全 OFF でイベント 0；省略時 GET 既定値。
*   **Logic**（`package llm_test` または common 配置に合わせる）:
    *   NewWithStore + ArtifactStore + git workDir
    *   Case A: collectors 省略 → terminate 後 `reconcile:git` 件数 0（ディスクに未追跡ファイルあり）
    *   Case B: 全 false → 手動 Save なし・Analyzer なしでも terminate 後新規 0（NewWithStore は Analyzer 無しなので、全 OFF は主に Create/GET + reconcile スキップ検証。Tier1 ゲートは unit で担保）
    *   Case C: GET で既定値

> Note: `NewWithStore` は Analyzer 未配線のため、Tier1 リアルタイムの E2E は既存 `TestE2E_.*Artifact` / llm 系に委ね、本ファイルは主に Tier3 既定 OFF と GET/Create を検証する。

### examples

#### [NEW] [examples/file-change-collectors/](file://examples/file-change-collectors/)

*   **Files**: `main.go`, `go.mod`, `go.sum`（sandbox-mode を雛形に）
*   **Logic**: `go run . [url] [mode]`  
    *   `default` / 省略: FileChangeCollectors なし  
    *   `full`: WorkdirReconcile true  
    *   `off`: 3 つとも false  
    *   Create → GetSession → 解決済みを log

#### [MODIFY] [examples/artifact-pipeline/main.go](file://examples/artifact-pipeline/main.go)

*   **Description**: CreateSession 付近にコメントで既定（Tier3 OFF）と `workdir_reconcile: true` の違いを記載。必要なら明示はしない（既定で十分）。

### Documentation

#### [MODIFY] [docs/ReferenceManual-WebAPIs.md](file://docs/ReferenceManual-WebAPIs.md)

*   Create Session: `file_change_collectors` 説明・既定表・JSON 例
*   Get Session: レスポンス例に解決済みオブジェクト

#### [MODIFY] [README.md](file://README.md)

*   Artifact API Examples に「File change collectors」節（概要・既定・Go 例・`examples/file-change-collectors/` リンク）
*   path+operation であり unified diff ではない旨

## Step-by-Step Implementation Guide

1. **[x] Types & Resolve (TDD)**: `file_change_collectors_test.go` → `file_change_collectors.go` → `SessionRecord` フィールド。
2. **[x] Analyzer gate (TDD)**: analyzer_test 更新 → New シグネチャ + analyzeEvents ゲート → 全 New 呼び出し更新。
3. **[x] AgentService**: Create/GET/reconcile ゲート + service 配線 + HTTP テスト。
4. **[x] Client SDK**: session.go + session_test.go。
5. **[x] Integration**: reconcile_integration_test 更新 + file_change_collectors_e2e_test.go。
6. **[x] Examples**: `examples/file-change-collectors/` + artifact-pipeline コメント。
7. **[x] Docs**: README + ReferenceManual。
8. **[x] Verify**: build.sh → integration_test.sh（下記）。
9. **[x] Commit/Push**: 意味単位でコミット後、全成功で push。

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**:

```bash
./scripts/process/build.sh --skip-frontend --skip-etc
```

（Windows ローカルの場合はプロジェクト慣例に合わせ `--skip-etc` 省略可。本環境は win32 → `--skip-frontend` のみでも可。plans では上記を標準とする。）

2. **Integration Tests**:

```bash
./scripts/process/build.sh --skip-frontend --skip-etc && ./scripts/process/integration_test.sh --categories common --specify 'TestReconcile|TestFileChangeCollector|TestE2E_.*Artifact'
```

3. **E2E Tests**: `tests/file_change_collectors_e2e_test.go`（上記 specify に含む）

4. **Example build**（build.sh が examples を含めない場合の補助確認は実装時に scripts 経由を優先。最低限 example の `go.mod` が client を参照しコンパイル可能であること — build pipeline 内でカバーされなければ integration 前に example ディレクトリで module tidy）。

### Documentation

*   [ ] `docs/ReferenceManual-WebAPIs.md` Create/Get Session
*   [ ] `README.md` Artifact API Examples
*   [ ] `examples/file-change-collectors/`
