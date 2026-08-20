# 000-Restore-Vendor-Homes-Outside-Tern

> **Source Specification**: `prompts/phases/001-phase02/branches/fix-bug-exproding-session-size/ideas/000-Restore-Vendor-Homes-Outside-Tern.md`

## Goal Description

`.tern/{session_id}/native/` を Coding Agent の CLI ホーム（`CODEX_HOME` / `CLAUDE_CONFIG_DIR`）として使う現行配線をやめ、ワークスペース直下の vendor ホームへ戻す。

| 役割 | パス |
|---|---|
| Tern 正本 **かつ** Wayfinder vendor ホーム | `{work_dir}/.tern/{session_id}/`（`record.json` / `metadata.json` / `history/`。`native/` は新規作成しない） |
| Codex vendor ホーム | `{work_dir}/.codex` → `CODEX_HOME` |
| Claude vendor ホーム | `{work_dir}/.claude` → `CLAUDE_CONFIG_DIR`（アダプタ名 `claudecode` でも `.claudecode` にしない） |

正本への `IngestTurn`、エージェント切替時の正本 → プロンプト supplement は維持する。Issue #48 のセッション比例 plugins 肥大化を解消する。

## User Review Required

1. **既存 `.tern/{id}/native/` と `.claudecode/`**  
   仕様どおり自動マイグレーションは行わない（放置・手動削除可）。

（Wayfinder のホームは `{work_dir}/.wayfinder` ではない。**`.tern/{session_id}/` 自体が Wayfinder の vendor ホーム**である。確定事項として本計画に反映済み。）

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: `.tern` は正本のみ。`native/` を新設・維持しない | Proposed Changes > wayfinder/session/canonical.go, workspace_session_store.go |
| R2: `CODEX_HOME={work_dir}/.codex`, `CLAUDE_CONFIG_DIR={work_dir}/.claude`。Codex/Claude に正本配下 `native/` を渡さない。overlay 先は各 vendor ホーム。Wayfinder は `.tern/{id}` をホームにする | Proposed Changes > VendorHomeDir, handler.go |
| R3: 正本・Ingest・切替 supplement 維持 | 変更しない（回帰テストで担保）。Wayfinder は正本ルートを SessionDir として共有 |
| R4: 同一 agent resume は vendor ホーム上 | SessionDir が vendor ホームになることで自動的に満たす |
| R5: `.tern` に plugins がセッション比例で増えない | Vendor ホーム共有 + 統合テスト（フェイクでパス検証） |
| R6: 先行仕様の `native/`=CLI ホーム記述を改訂 | Documentation |
| R7: Claude ホームは `.claude` のみ | VendorHomeDir（`claudecode` → `.claude`） |
| R8–R10 (May) | 本計画では実装しない（ドキュメントに手動削除注記のみ R8 相当） |

## Proposed Changes

### agentservice — Vendor ホーム解決

#### [NEW] [shared/libs/go/agentservice/vendor_home_test.go](file://shared/libs/go/agentservice/vendor_home_test.go)

*   **Description**: TDD。`VendorHomeDir` のテーブル駆動テストを先に追加する。
*   **Technical Design**: `testing` パッケージ。ケース例:

```go
tests := []struct {
    name, workDir, agent, sessionDir, want string
}{
    {"codex", "/ws", "codex", "/ws/.tern/s1", filepath.Join("/ws", ".codex")},
    {"claude", "/ws", "claudecode", "/ws/.tern/s1", filepath.Join("/ws", ".claude")},
    {"claude_not_agentname_dir", "/ws", "claudecode", "/ws/.tern/s1", filepath.Join("/ws", ".claude")}, // .claudecode にしない
    {"wayfinder_uses_tern_session", "/ws", "wayfinder", "/ws/.tern/s1", "/ws/.tern/s1"},
    {"wayfinder_empty_session", "/ws", "wayfinder", "", ""},
    {"empty_work_codex", "", "codex", "/x", ""},
    {"empty_agent", "/ws", "", "/ws/.tern/s1", ""},
}
```

*   **Logic**:
    - `claudecode` → 必ず `filepath.Join(workDir, ".claude")`。`"."+agentName`（`.claudecode`）を使わない。`sessionDir` は使わない。
    - `codex` → `filepath.Join(workDir, ".codex")`。`sessionDir` は使わない。
    - `wayfinder` → **`sessionDir` そのもの**（Tern 正本 = Wayfinder vendor ホーム。通常 `{work_dir}/.tern/{id}`）。`{work_dir}/.wayfinder` や `{sessionDir}/native` にはしない。
    - `workDir == ""` かつ codex/claudecode → `""`。
    - `wayfinder` で `sessionDir == ""` → `""`。
    - 未知の agent → `filepath.Join(workDir, "."+agentName)`（ただし `claudecode` / `wayfinder` / `codex` は上記）。

#### [NEW] [shared/libs/go/agentservice/vendor_home.go](file://shared/libs/go/agentservice/vendor_home.go)

*   **Description**: vendor ホーム解決ヘルパを追加する。
*   **Technical Design**:

```go
package agentservice

// VendorHomeDir returns the Coding Agent home directory for launch/overlay.
// Mapping:
//   codex       → {workDir}/.codex
//   claudecode  → {workDir}/.claude   // never {workDir}/.claudecode
//   wayfinder   → sessionDir          // Tern canonical root (.tern/{id}); NOT .wayfinder, NOT .../native
// Empty inputs that are required for the agent return "".
func VendorHomeDir(workDir, agentName, sessionDir string) string
```

*   **Logic**: 上記マッピングを実装。`agentName` はアダプタ `Name()` と同一（`codex` / `claudecode` / `wayfinder`）。

#### [MODIFY] [shared/libs/go/agentservice/workspace_session_store_test.go](file://shared/libs/go/agentservice/workspace_session_store_test.go)

*   **Description**: `TestNativeSessionDir` を削除または `TestVendorHomeDir` へ置換（本体は `vendor_home_test.go`）。`Create` 後に `{session_dir}/native` が**無い**ことを断言するテストを追加。
*   **Logic**:
    - `Create` 後: `os.Stat(filepath.Join(sessionDir, "native"))` が `os.IsNotExist`。
    - `history/` と `record.json` は存在する（Canonical Init 経由）。

#### [MODIFY] [shared/libs/go/agentservice/workspace_session_store.go](file://shared/libs/go/agentservice/workspace_session_store.go)

*   **Description**: `NativeSessionDir` を廃止し、`persistSessionRecord` が `native/` を作らないようにする。
*   **Technical Design**:
    - `func NativeSessionDir(sessionDir string) string` を **削除**する（全呼び出しを `VendorHomeDir` または正本パスへ置換）。
    - `persistSessionRecord`:

```go
func persistSessionRecord(rec *codingagent.SessionRecord) error {
    if err := os.MkdirAll(rec.SessionDir, 0755); err != nil {
        return fmt.Errorf("mkdir session_dir: %w", err)
    }
    // Do NOT mkdir {session_dir}/native
    // write record.json as today
    ...
}
```

*   **Logic**: 仕様 R1「`.tern/{id}/native/` を新設・維持しない」。

#### [MODIFY] [shared/libs/go/agentservice/handler.go](file://shared/libs/go/agentservice/handler.go)

*   **Description**: SendMessage でアダプタへ渡す `SessionDir` を vendor ホームにする。
*   **Technical Design**: `handleSendMessage` 内の opts 構築を変更。

現行:

```go
if record.SessionDir != "" {
    opts = append(opts, codingagent.WithSessionDir(NativeSessionDir(record.SessionDir)))
}
```

変更後:

```go
if vh := VendorHomeDir(record.WorkDir, record.AgentName, record.SessionDir); vh != "" {
    opts = append(opts, codingagent.WithSessionDir(vh))
}
```

*   **Logic**:
    - `codex` / `claudecode`: アダプタ `SessionDir` = `{work_dir}/.codex` または `{work_dir}/.claude`（`CODEX_HOME` / `CLAUDE_CONFIG_DIR` / overlay 先）。
    - `wayfinder`: アダプタ `SessionDir` = `record.SessionDir`（`.tern/{id}`）。正本ディレクトリが Wayfinder の vendor ホームでもある。`native/` サブディレクトリは使わない。
    - Tern 正本操作用の `record.SessionDir`（`AppendSessionMessage` / `IngestTurn` / Canonical）は変更しない。
    - `config_dir` の `WithConfigDir` は現行どおり。overlay はアダプタ内で `cfg.SessionDir`（＝上記 vendor ホーム）へ適用される。

#### [MODIFY] [shared/libs/go/agentservice/handler_session_test.go](file://shared/libs/go/agentservice/handler_session_test.go) / 関連 handler テスト

*   **Description**: フェイク agent の `lastCfg.SessionDir` が `NativeSessionDir(...)` ではなく `VendorHomeDir(workDir, agent, sessionDir)` であることを断言するよう更新・追加。
*   **Logic**:
    - Codex: `filepath.Join(workDir, ".codex")`
    - Claude: `filepath.Join(workDir, ".claude")`
    - Wayfinder: `record.SessionDir`（`.tern/{id}` または明示 session_dir）と一致。`.wayfinder` や `.../native` ではない。
    - 切替後も同様。supplement / resume クリアの既存ケースは維持。

### wayfinder/session — Canonical から native 作成を除去

#### [MODIFY] [shared/libs/go/wayfinder/session/canonical_test.go](file://shared/libs/go/wayfinder/session/canonical_test.go)

*   **Description**: `Init` 後に `native/` が存在することを期待している断言を反転する（存在しないこと）。
*   **Logic**: `os.Stat(.../native)` → `os.IsNotExist(err)`。`history/` は引き続き存在する。

#### [MODIFY] [shared/libs/go/wayfinder/session/canonical.go](file://shared/libs/go/wayfinder/session/canonical.go)

*   **Description**: `Init` が `native/` を作らない。
*   **Technical Design**:

```go
func (c *Canonical) Init(sessionID, activeAgent string) error {
    ...
    if err := os.MkdirAll(c.HistoryDir(), 0755); err != nil { ... }
    // REMOVE: os.MkdirAll(c.NativeDir(), 0755)
    ...
}
```

*   **Logic**: `NativeDir()` メソッドは後方互換のため残してよい（パス計算のみ）。新規作成はしない。コメントを「legacy path helper; not created by Init」に更新。

### codingagent — コメントと単体テストの期待値

#### [MODIFY] [shared/libs/go/codingagent/options.go](file://shared/libs/go/codingagent/options.go)

*   **Description**: `ApplyDefaults` コメントの「Tern assigns ... NativeSessionDir」を「Tern passes VendorHomeDir(workDir, agent, sessionDir)」に更新。
*   **Logic**: 挙動変更は agentservice 側。ここはコメント整合のみ。

#### [MODIFY] [shared/libs/go/codingagent/codex/process_test.go](file://shared/libs/go/codingagent/codex/process_test.go)

*   **Description**: 既存 `session dir sets CODEX_HOME` は維持。追加ケースとして `SessionDir=filepath.Join(workDir, ".codex")` のとき `CODEX_HOME` がその絶対パスになることを明示。
*   **Logic**: `BuildEnv` は既に `env["CODEX_HOME"] = cfg.SessionDir`。配線が正しければ足りる。回帰防止のドキュメント的ケース。

#### [MODIFY] [shared/libs/go/codingagent/claudecode/process_test.go](file://shared/libs/go/codingagent/claudecode/process_test.go)

*   **Description**: `CLAUDE_CONFIG_DIR` が `{workDir}/.claude` になるケースを追加（値が `.claudecode` を含まないこと）。
*   **Logic**: 同様に `BuildEnv` は `cfg.SessionDir` をそのまま設定。agentservice が `.claude` を渡すことが前提。

### 統合 / E2E テスト（tests/）

#### [MODIFY] [tests/agentservice_integration_test.go](file://tests/agentservice_integration_test.go)

*   **Description**: `config_dir` overlay 検証で `NativeSessionDir(sessionDir)` を参照している箇所を、**work_dir 上の vendor ホーム**へ変更する。
*   **Logic**:
    - agent=`codex` のとき overlay 先 = `filepath.Join(workDir, ".codex", ...)`
    - agent=`claudecode` のとき = `filepath.Join(workDir, ".claude", ...)`
    - Tern `session_dir`（`.tern/{id}` または明示 temp）直下に `skills/` が出来ないことを確認してよい。

#### [MODIFY] [tests/agentservice_e2e_test.go](file://tests/agentservice_e2e_test.go)

*   **Description**: `session_dir == work_dir/.claudecode` を期待する古いコメント・断言があれば、正本は `.tern/{id}`、CLI ホームは `.claude` に更新。
*   **Logic**: API の `session_dir` レスポンスは引き続き Tern 正本パス（`.tern/...`）。混同しない。

#### [MODIFY] [tests/llm_session_portability_test.go](file://tests/llm_session_portability_test.go)

*   **Description**: 切替時に `lastCfg.SessionDir` が各 agent の vendor ホームであることを断言するケースを追加／更新。supplement ヘッダ回帰は維持。
*   **Logic**:
    - Codex ターン: `SessionDir == filepath.Join(workDir, ".codex")`
    - Claude ターン: `SessionDir == filepath.Join(workDir, ".claude")`
    - Wayfinder ターン（あれば）: `SessionDir ==` Tern の `session_dir`（`.tern/{id}`）。`native` サフィックス無し。
    - 正本 `session_dir`（Create 時）は API 上 `.tern/...` のまま。

#### [NEW] [tests/llm_vendor_home_layout_test.go](file://tests/llm_vendor_home_layout_test.go)

*   **Description**: 仕様 V1 / V2 / V3 / V5 の自動化（フェイク CodingAgent）。
*   **Technical Design**: 既存 `portabilityAgent` と同型のフェイクで `lastCfg` を記録。HTTP で Create → SendMessage → terminate。
*   **Logic（テストケース）**:

| テスト名 | 検証 |
|---|---|
| `TestTernSessionDir_NoNativeVendorHome` | Create+1ターン後、`{session_dir}/native` が無い。`history/` / `record.json` はある |
| `TestCodexUsesWorkDirCodexHome` | フェイクに渡った `SessionDir == {work_dir}/.codex`。環境相当は SessionDir で代替 |
| `TestClaudeUsesWorkDirClaudeHome` | `SessionDir == {work_dir}/.claude`（文字列に `.claudecode` を含まない） |
| `TestWayfinderUsesTernSessionDir` | agent=`wayfinder` のとき `SessionDir ==` API の `session_dir`（`.tern/{id}`）。末尾 `/native` や `.wayfinder` ではない |
| `TestRepeatedCodexSessions_TernTreeHasNoNativePluginsTree` | 同一 workDir でセッションを 5 回 Create→短文→（可能なら terminate）。各 `.tern/{id}` に `native/.tmp/plugins` が無い。5 回ともフェイクの `SessionDir` が同一 `{work_dir}/.codex` |
| `TestCodexResumeUsesSameVendorHome` | 同一 session_id で 2 ターン。両方の `SessionDir` が `.codex`。2 ターン目に resume id が付く既存挙動と矛盾しないこと |

LIVE 実 Codex による plugins `du` 計測は必須としない（フェイクでパス契約を固定。R5 の本質は「ホームを共有側へ戻す」こと）。

### Documentation

#### [MODIFY] [docs/ReferenceManual-WebAPIs.md](file://docs/ReferenceManual-WebAPIs.md)

*   **Description**: Create Session の `session_dir` / persistence 説明を改訂。
*   **Logic（掲載文の内容）**:
    - `session_dir` 既定: `work_dir/.tern/{session_id}`。Tern 正本（`record.json` / `metadata.json` / `history/`）のみ。
    - **置かない**: `{session_dir}/native` を CLI ホームとしては使わない。
    - Persistence env:
      - Claude Code: `CLAUDE_CONFIG_DIR={work_dir}/.claude`
      - Codex: `CODEX_HOME={work_dir}/.codex`
      - Wayfinder: Tern `session_dir`（既定 `{work_dir}/.tern/{session_id}`）をエージェント SessionDir として使用（正本と同一ルート。`native/` サブディレクトリは使わない）
    - `config_dir` overlay 先: 上記 vendor ホーム（`{session_dir}/native` ではない）。
    - 任意: 過去に生成された `{session_dir}/native` は手動削除可（正本 `history` を消さない）。

#### [MODIFY] [README.md](file://README.md)（該当節がある場合）

*   **Description**: `native/` = CLI ホームの記述があれば同様に改訂。

#### [MODIFY] [prompts/phases/001-phase02/branches/feat-session-migration/ideas/000-Wayfinder-Format-Session-Portability.md](file://prompts/phases/001-phase02/branches/feat-session-migration/ideas/000-Wayfinder-Format-Session-Portability.md)

*   **Description**: R6。レイアウト図の `native/` = `CODEX_HOME` / `CLAUDE_CONFIG_DIR` と「同一 native に同居」記述の直上に、本仕様への改訂注記を追加する。

```markdown
> **改訂 (fix-bug-exproding-session-size)**: CLI ホームを `{session_dir}/native` に置く方針は撤回。
> 正本は `.tern/{id}/` のみ。Codex は `{work_dir}/.codex`、Claude は `{work_dir}/.claude`。
> 詳細: `.../fix-bug-exproding-session-size/ideas/000-Restore-Vendor-Homes-Outside-Tern.md`
```

歴史的本文は残してよいが、実装の正は新仕様とする。

## Step-by-Step Implementation Guide

1. **[x] TDD VendorHomeDir**: `vendor_home_test.go` / `vendor_home.go`
2. **[x] Canonical Init**: no `native/` mkdir
3. **[x] Workspace store**: remove `NativeSessionDir`, no native mkdir
4. **[x] handler SendMessage**: `VendorHomeDir(workDir, agent, sessionDir)`
5. **[x] codingagent comments**
6. **[x] Integration / layout tests**
7. **[x] Docs**: ReferenceManual + Wayfinder idea 改訂注記
8. **[x] Verify**: build.sh + integration_test.sh (focused)

## Verification Plan

### Automated Verification

本リポジトリの `integration_test.sh` は `--specify` のみ（`--categories` なし）。計画ルールの `build.sh` → `integration_test.sh` 順序を守る。

1. **Build & Unit Tests**:

```bash
./scripts/process/build.sh
```

（Linux / Remote-SSH Linux 環境ではプロジェクト指示どおり `./scripts/process/build.sh --skip-etc` を用いる。）

2. **Integration / E2E（レイアウトと可搬性）**:

```bash
./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "VendorHome|NoNativeVendorHome|SessionPortability|Portability|ConfigDir"
```

3. **Integration（Codex / AgentService 回帰）**:

```bash
./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "Codex|AgentService|Session"
```

4. **追加 E2E ファイル**: `tests/llm_vendor_home_layout_test.go` のテスト名が上記 `--specify` にマッチすること。

### 受け入れとの対応

| 受け入れ条件 | 検証 |
|---|---|
| 新規セッションで `native/` が作られない | `TestTernSessionDir_NoNativeVendorHome` + Canonical/store 単体 |
| `CODEX_HOME` ≡ `{work_dir}/.codex` | `TestCodexUsesWorkDirCodexHome` + BuildEnv |
| `CLAUDE_CONFIG_DIR` ≡ `{work_dir}/.claude` | `TestClaudeUsesWorkDirClaudeHome` |
| 切替 supplement 維持 | 既存 SessionPortability テスト |
| `.tern` に plugins 比例増なし | `TestRepeatedCodexSessions_TernTreeHasNoNativePluginsTree` |
| ドキュメント改訂 | Docs 差分レビュー（ビルド対象外） |

## Documentation

- `docs/ReferenceManual-WebAPIs.md`: persistence / overlay の `native/` 記述を削除し、`.codex` / `.claude` に置換。
- `README.md`: 同様の記述があれば更新。
- `feat-session-migration` の Wayfinder 可搬性 idea: 改訂注記（R6）。
- 実装コミットメッセージ例: `fix: use work_dir .codex/.claude as agent homes outside .tern`
