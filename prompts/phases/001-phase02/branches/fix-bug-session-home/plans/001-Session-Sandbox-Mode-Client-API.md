# 001-Session-Sandbox-Mode-Client-API

> **Source Specification**: `prompts/phases/001-phase02/branches/fix-bug-session-home/ideas/001-Session-Sandbox-Mode-Client-API.md`

## Goal Description

Create Session / `client/v1` に `sandbox_mode`（`read-only` | `workspace-write` | `danger-full-access`）を追加し、省略時は現行どおり Codex `-s read-only`。`workspace-write` でワークスペース限定 R/W、`danger-full-access` でフル bypass をセッション単位で指定できるようにする。

## User Review Required

None.（仕様レビュー済み。レスポンスは常に解決済み `sandbox_mode` を返す方針で固定する。）

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1 CreateSession `sandbox_mode` + 許可値 + 400 | Proposed Changes > agentservice `handler.go` / `sandbox_mode.go` |
| R2 既定 read-only（bypass なし） | `ResolveSandboxMode` + Codex `StartProcess` |
| R3 SessionRecord 保持・ターン起動反映・レスポンス明示 | `session_store.go` / `handler.go` opts / GET |
| R4 優先順位（明示 > server disable_sandbox > read-only） | `ResolveSandboxMode` |
| R5 Claude `CLAUDE_CODE_SKIP_SANDBOX` マッピング | `claudecode/process.go` |
| R6 T1–T7 リグレッション | unit + `tests/common_sandbox_mode_test.go` |
| R7 PATCH では変更しない | 変更なし（明示） |
| R8 client/v1 SessionRequest/Info/定数/テスト | `client/v1/session.go` / `session_test.go` |
| R9 examples/sandbox-mode | `examples/sandbox-mode/` |
| Docs Issue #54 契約 | `docs/ReferenceManual-WebAPIs.md` |

## Proposed Changes

### codingagent（共有定数・解決・SessionConfig）

#### [NEW] [shared/libs/go/codingagent/sandbox_mode.go](file://shared/libs/go/codingagent/sandbox_mode.go)

*   **Description**: モード定数と解決関数。
*   **Technical Design**:

```go
const (
	SandboxModeReadOnly         = "read-only"
	SandboxModeWorkspaceWrite   = "workspace-write"
	SandboxModeDangerFullAccess = "danger-full-access"
)

// ResolveSandboxMode applies R4:
// 1. explicit non-empty request mode (must be allowed) -> that mode
// 2. else if serverDisableSandbox -> danger-full-access
// 3. else -> read-only
// Empty explicit is treated as "not set" (fall through to 2/3).
// Unknown explicit returns error (caller maps to HTTP 400).
func ResolveSandboxMode(explicit string, serverDisableSandbox bool) (string, error)

// SandboxModeDisablesSandbox returns true iff mode == SandboxModeDangerFullAccess.
func SandboxModeDisablesSandbox(mode string) bool
```

*   **Logic**:
    - 許可値: `read-only`, `workspace-write`, `danger-full-access`。
    - `explicit == ""` → サーバフラグを見て 2 または 3。
    - `explicit` が未知 → `fmt.Errorf("unsupported sandbox_mode: %q (allowed: read-only, workspace-write, danger-full-access)", explicit)`。
    - Codex `BuildArgs`: `read-only` → `-s read-only`、`workspace-write` → `-s workspace-write`、`danger-full-access` → `--dangerously-bypass-approvals-and-sandbox`。

#### [NEW] [shared/libs/go/codingagent/sandbox_mode_test.go](file://shared/libs/go/codingagent/sandbox_mode_test.go)

*   **Description**: テーブル駆動で R4 / 不正値を固定。
*   **Logic**: ケース例:
    - `("", false) -> read-only`
    - `("", true) -> danger-full-access`
    - `("read-only", true) -> read-only`（明示優先）
    - `("danger-full-access", false) -> danger-full-access`
    - `("full-auto", false) -> error`

#### [MODIFY] [shared/libs/go/codingagent/session_store.go](file://shared/libs/go/codingagent/session_store.go)

*   **Description**: レコードに解決済みモードを永続化。
*   **Technical Design**: `SessionRecord` に追加:

```go
SandboxMode string `json:"sandbox_mode,omitempty"`
```

*   **Logic**: Create 時に必ず解決済み値（空にしない）を格納。旧 `record.json` で欠落時は読取側で `read-only` とみなすヘルパを GET 応答組み立てで適用可。

#### [MODIFY] [shared/libs/go/codingagent/options.go](file://shared/libs/go/codingagent/options.go)

*   **Description**: ターン起動用にセッションモードを SessionConfig へ渡す。
*   **Technical Design**:

```go
// in SessionConfig:
SandboxMode string // resolved: read-only | danger-full-access

func WithSandboxMode(mode string) SessionOption {
	return func(c *SessionConfig) { c.SandboxMode = mode }
}
```

### Codex アダプタ

#### [MODIFY] [shared/libs/go/codingagent/codex/process.go](file://shared/libs/go/codingagent/codex/process.go)

*   **Description**: bypass 判定をセッションモード優先にする。
*   **Logic**:
    - `disableSandbox := codingagent.SandboxModeDisablesSandbox(cfg.SandboxMode)`
    - `cfg.SandboxMode == ""` のときのみ後方互換で `ac.DisableSandbox` を使う（アダプタ単体テスト・直接 StartProcess 呼び出し用）。
    - `BuildArgs(..., disableSandbox)` は既存のまま。

#### [MODIFY] [shared/libs/go/codingagent/codex/process_test.go](file://shared/libs/go/codingagent/codex/process_test.go)

*   **Description**: 既存 `TestCodexBuildArgs_SandboxEnforcedOmitsBypassFlag` を維持。SessionConfig 経由の StartProcess 引数は統合側で確認。必要なら `BuildArgs` の true/false ケースを再確認。

### Claude アダプタ

#### [MODIFY] [shared/libs/go/codingagent/claudecode/process.go](file://shared/libs/go/codingagent/claudecode/process.go)

*   **Description**: R5 マッピング。
*   **Logic** in `BuildEnv`:
    - `skip := codingagent.SandboxModeDisablesSandbox(cfg.SandboxMode); if cfg.SandboxMode == "" { skip = ac.DisableSandbox }`
    - `skip` が true のとき `CLAUDE_CODE_SKIP_SANDBOX=1`

#### [MODIFY] [shared/libs/go/codingagent/claudecode/process_test.go](file://shared/libs/go/codingagent/claudecode/process_test.go)

*   **Description**: `SandboxMode` danger / read-only / 空+AdapterConfig のケースを追加。

### agentservice

#### [NEW] [shared/libs/go/agentservice/sandbox_mode_test.go](file://shared/libs/go/agentservice/sandbox_mode_test.go)（または handler テストに統合）

*   **Description**: CreateSession の 400 / 解決済み GET（T6, T7）を HTTP レベルで検証。モック agent 可。

#### [MODIFY] [shared/libs/go/agentservice/handler.go](file://shared/libs/go/agentservice/handler.go)

*   **Description**: Create で `sandbox_mode` を受け取り解決してレコードへ保存。SendMessage で `WithSandboxMode(record.SandboxMode)` を付与。
*   **Technical Design**: request struct に `SandboxMode string \`json:"sandbox_mode"\``。
*   **Logic**:
    1. `resolved, err := codingagent.ResolveSandboxMode(req.SandboxMode, s.disableSandbox)`
    2. err → `400` + err.Error()
    3. `record.SandboxMode = resolved`
    4. `opts` に `codingagent.WithSandboxMode(record.SandboxMode)`（空なら `read-only` を入れる）
    5. Debug ログ: `sandbox_mode`, `server_disable_sandbox`

#### [MODIFY] [shared/libs/go/agentservice/handler_session.go](file://shared/libs/go/agentservice/handler_session.go)

*   **Description**: GET / list レスポンスで解決済み値を常に返す。
*   **Logic**: `sessionResponse` または encode 前に、`record.SandboxMode == ""` なら `codingagent.SandboxModeReadOnly` を埋めて返す（永続レコードは変更しなくても可）。

### client/v1（R8）

#### [MODIFY] [client/v1/session.go](file://client/v1/session.go)（または隣接の定数ファイル）

*   **Technical Design**:

```go
const (
	SandboxModeReadOnly         = "read-only"
	SandboxModeWorkspaceWrite   = "workspace-write"
	SandboxModeDangerFullAccess = "danger-full-access"
)

// SessionRequest:
SandboxMode string `json:"sandbox_mode,omitempty"`

// SessionInfo:
SandboxMode string `json:"sandbox_mode,omitempty"`
```

#### [MODIFY] [client/v1/session_test.go](file://client/v1/session_test.go)

*   **Logic**: Create で `SandboxModeDangerFullAccess` 指定時 JSON にキーあり。省略時キーなし。GetSession デコードで `SandboxMode` が入ること。

### examples（R9）

#### [NEW] [examples/sandbox-mode/](file://examples/sandbox-mode/)

*   **Description**: `main.go` + `go.mod`（`minimal-client` と同様 `replace => ../../`）。
*   **Logic**:
    - 先頭コメントで Issue #54、書き込みには `danger-full-access` またはサーバ `disable_sandbox: true` が必要と記載。
    - 引数: `[server-url] [mode]`。`mode` 省略または `read-only` / `danger-full-access`。
    - `CreateSession` に `SandboxMode` を渡し、作成後 `GetSession` で表示。
    - エージェントは `codex`（または環境で動くもの）。メッセージ送信は短くてよい。

### 統合 / E2E テスト（R6）

#### [NEW] [tests/common_sandbox_mode_test.go](file://tests/common_sandbox_mode_test.go)

*   **Description**: `TestSandboxMode_*` で T1–T7 をカバー。`codex/testfake` の launch log で bypass 有無を検証。
*   **Logic**（各サブテストまたは独立 Test）:
    - **T1**: `WithSandboxDisabled(false)`、Create で mode 省略 → launch log に `dangerously-bypass` **なし**
    - **T2**: `"sandbox_mode":"read-only"` → bypass なし
    - **T3**: `"danger-full-access"` → bypass **あり**
    - **T4**: `WithSandboxDisabled(true)`、省略 → bypass あり
    - **T5**: server true + 明示 `read-only` → bypass なし
    - **T6**: 不正値 → HTTP 400
    - **T7**: GET で `sandbox_mode` が解決済み

既存ヘルパ（`createE2ESession*` / `freePort` / `sendE2EMessage`）を再利用。Create ボディに `sandbox_mode` を渡すヘルパを同ファイルまたは既存に追加。

### Documentation

#### [MODIFY] [docs/ReferenceManual-WebAPIs.md](file://docs/ReferenceManual-WebAPIs.md)

*   **Logic**: Create Session に `sandbox_mode` 説明、許可値、R4 優先順位、Issue #54 推奨、Get Session レスポンス例に `sandbox_mode`、Claude/Codex 意味の差を短く記載。PATCH では変更不可と明記。

## Step-by-Step Implementation Guide

チェックボックスで進捗管理する。

1. [x] **Unit: ResolveSandboxMode**: `sandbox_mode.go` + `_test.go` を追加し、テーブルが FAIL→PASS。
2. [x] **Models**: `SessionRecord.SandboxMode`、`SessionConfig` + `WithSandboxMode`。
3. [x] **Codex/Claude process**: セッションモード優先の bypass / `CLAUDE_CODE_SKIP_SANDBOX`。単体テスト更新。
4. [x] **agentservice Create/GET/Send**: 解決・400・opts 注入。handler テスト。
5. [x] **client/v1**: 定数・フィールド・`session_test.go`。
6. [x] **examples/sandbox-mode**: 追加。
7. [x] **tests/common_sandbox_mode_test.go**: T1–T7。
8. [x] **docs**: ReferenceManual 更新。
9. [x] **Verify**: build + integration `--specify TestSandboxMode` + `--specify TestSessionRecover`。
10. [/] **Git**: 意味単位で commit → 全成功後 push → PR（Issue #54 リンク）。

## Verification Plan

### Automated Verification

Windows（本環境）:

1. **Build & Unit Tests**: `./scripts/process/build.sh`
2. **Integration Tests (new)**: `./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestSandboxMode"`
3. **Regression**: `./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestSessionRecover"`
4. **Related categories (optional full)**: `./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories common,llm`

Linux / Remote-SSH の場合は `build.sh --skip-etc` と `xvfb-run -a` ラップを用いる。

### E2E Tests

- `tests/common_sandbox_mode_test.go` の `TestSandboxMode_*`（testfake + AgentService HTTP）。手動確認は主検証にしない。

## Documentation

- `docs/ReferenceManual-WebAPIs.md`（Create/Get、優先順位、Issue #54）
- 仕様書 ideas は更新済み。本 plans ファイルが実装の正。
