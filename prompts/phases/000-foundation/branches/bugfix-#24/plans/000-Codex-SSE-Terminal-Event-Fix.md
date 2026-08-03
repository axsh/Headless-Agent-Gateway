# 000-Codex-SSE-Terminal-Event-Fix

> **Source Specification**: `prompts/phases/000-foundation/branches/bugfix-#24/ideas/000-Codex-SSE-Terminal-Event-Fix.md`

## Goal Description

Codex stdout の JSONL 1 行が 65536 バイト (~64 KiB) を超えると、`bufio.Scanner` のデフォルト上限により読み取りがサイレント停止し、SSE 上の `EventResult` が欠落する ([Issue #24](https://github.com/axsh/arctic-tern/issues/24))。

本計画は **Repro-first (Phase 1 RED → Phase 2 修正 → Phase 3 GREEN)** で以下を実装する:

1. Scanner バッファ拡張 + `scanner.Err()` 明示 (R1)
2. exit 0 時の `EventResult` 合成フォールバック (R2)
3. 大出力 `EventToolResult` の切り詰め (R3)
4. claudecode への同型修正 (R5)
5. `model_profiles.yaml` からの上限設定 (R6)
6. 単体 / 統合 / E2E 再現・回帰テスト (R4, R7)

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: Scanner バッファ拡張 (4MB) + `scanner.Err()` + EventError | `stream_io.go`, `codex/process.go`, `claudecode/process.go` |
| R2: exit 0 かつ EventResult 未送出時に合成 EventResult | `codex/process.go`, `claudecode/process.go` |
| R3: EventToolResult 256KB 切り詰め + マーカー | `stream_io.go`, process 送出前ロジック |
| R4: Minimal local repro 単体テスト (Phase 1 RED) | `codex/scanner_test.go`, `claudecode/scanner_test.go` |
| R5: claudecode 同型修正 + テスト | `claudecode/process.go`, `claudecode/scanner_test.go`, `claudecode/process_internal_test.go` |
| R6: YAML 設定可能化 + 伝播 | `config/model_profiles.go`, `codingagent/options.go`, `adapter_config.go`, `agentservice/handler.go` |
| R7: E2E / 統合 repro (Phase 1 RED → GREEN) | `tests/codex_scanner_integration_test.go`, `tests/codex_large_output_e2e_test.go`, `tests/testutil/fake_codex.go` |

## Proposed Changes

### Phase 1 — 再現テスト (修正前、RED 確認)

#### [NEW] [shared/libs/go/codingagent/codex/scanner_test.go](file://shared/libs/go/codingagent/codex/scanner_test.go)

* **Description**: Issue #24 Minimal local repro の単体テスト。デフォルト Scanner の 64KB 停止を文書化し、修正後 Scanner の 3 行読み取りを検証する。
* **Technical Design**: テストヘルパ `buildThreeLineReproReader()` が以下 3 行を返す `io.Reader` を生成する:
  1. `{"type":"item.started"}\n`
  2. `{"type":"item.completed","item":{"type":"command_execution","aggregated_output":"<65537+ bytes>"}}\n` (padding で 65537 バイト以上)
  3. `{"type":"item.completed"}\n`
* **Logic** — テストケース:

| テスト名 | 現行コード (Phase 1) | 修正後 (Phase 3) |
|----------|---------------------|------------------|
| `TestScanner_DefaultLimitStopsAt64KB` | **PASS** — 1 行のみ読め `scanner.Err() == bufio.ErrTooLong` | **PASS** (回帰防止) |
| `TestScanner_LargeLineReadsAllThreeLines` | **FAIL** — 3 行目未到達 | **PASS** — 3 行すべて読める |

```go
// Same pattern as codex/process.go v0.1.4: default Scanner, no Err check.
scanner := bufio.NewScanner(reader)
for scanner.Scan() {
    // process line
}
// If reader contains: small line, then >64KiB line, then small line —
// only the first small line is read; scanner.Err() == bufio.ErrTooLong
```

Assert (DefaultLimit): ループは 1 行目の small line で停止する。`scanner.Err()` は non-nil。

#### [NEW] [shared/libs/go/codingagent/claudecode/scanner_test.go](file://shared/libs/go/codingagent/claudecode/scanner_test.go)

* **Description**: R5 向け。codex と同一 3 行パターンで claudecode も同バグを再現する。
* **Logic**: `TestClaudeScanner_DefaultLimitStopsAt64KB`, `TestClaudeScanner_LargeLineReadsAllThreeLines` (Phase 1: 後者 FAIL)

#### [NEW] [tests/testutil/fake_codex.go](file://tests/testutil/fake_codex.go)

* **Description**: 統合 / E2E テスト用の fake `codex` 実行ファイル生成ヘルパ。
* **Technical Design**:

```go
// FakeCodexOptions configures stdout JSONL emitted by the fake codex binary.
type FakeCodexOptions struct {
    Lines      []string // JSONL lines (no trailing newline in each element)
    ExitCode   int      // process exit code (default 0)
    DelayMS    int      // optional delay before exit
}

// InstallFakeCodex creates a fake "codex" executable in dir and returns its path.
// The fake binary ignores args and prints Lines to stdout, then exits with ExitCode.
func InstallFakeCodex(t *testing.T, dir string, opts FakeCodexOptions) string
```

* **Logic**: `buildThreeLineReproLines()` が oversized `aggregated_output` 行を含む 3 行を生成。`turn.completed` は **意図的に省略** し exit 0 のみ返す (R2 フォールバック検証用)。

#### [NEW] [tests/codex_scanner_integration_test.go](file://tests/codex_scanner_integration_test.go)

* **Description**: `codex.StartProcess` 経路 + fake codex + 実 SSE 相当のチャネル読み取りで process 層バグを再現 (R4, R7)。
* **Technical Design**:

```go
func TestCodexScannerIntegration_LargeOutputMissingEventResult(t *testing.T) {
    // 1. t.TempDir() に InstallFakeCodex (3-line repro, exit 0, no turn.completed)
    // 2. PATH を fake codex dir に prepend
    // 3. StartProcess(...) を呼び、ch から全 StreamEvent を drain
    // 4. EventResult が 1 件含まれることを assert
}
```

* **Logic**:
  - **Phase 1 (現行コード)**: **FAIL** — oversized 行以降未読 + exit 0 フォールバックなし → `EventResult` 欠落
  - **Phase 3 (修正後)**: **PASS** — 3 行目または合成 `EventResult` が届く

#### [NEW] [tests/codex_large_output_e2e_test.go](file://tests/codex_large_output_e2e_test.go)

* **Description**: AgentService 実 HTTP SSE 経路の E2E (R7)。LLM / 実 Codex 非依存。
* **Technical Design**:

```go
func TestCodexE2E_LargeToolOutputTerminalEvent(t *testing.T) {
    // 1. t.TempDir() + InstallFakeCodex (3-line repro)
    // 2. agentservice.New + codex.New(AdapterConfig{...}) を登録
    //    PATH に fake codex dir を設定 (t.Setenv("PATH", ...))
    // 3. POST /api/v1/sessions → POST /api/v1/sessions/{id}/messages (Accept: text/event-stream)
    // 4. parseE2ESSEEvents (tests/codex_e2e_test.go の既存ヘルパを再利用)
    // 5. EventResult が 1 件、session status == "completed" を assert
}
```

* **Logic**:
  - **Phase 1**: **FAIL** — SSE に `EventResult` なし (または session が completed にならない)
  - **Phase 3**: **PASS**
  - `-short` でもスキップしない

---

### Phase 2 — 共通ユーティリティと設定 (R1, R3, R6)

#### [NEW] [shared/libs/go/codingagent/stream_io.go](file://shared/libs/go/codingagent/stream_io.go)

* **Description**: Scanner / Truncate 共通関数 (codex + claudecode 共用)。
* **Technical Design**:

```go
const (
    DefaultScannerMaxTokenSize = 4 * 1024 * 1024  // 4MB
    DefaultMaxToolResultBytes  = 256 * 1024       // 256KB
)

// NewLargeLineScanner returns a Scanner with configurable max token size.
// maxTokenSize <= 0 uses DefaultScannerMaxTokenSize.
func NewLargeLineScanner(r io.Reader, maxTokenSize int) *bufio.Scanner {
    s := bufio.NewScanner(r)
    limit := maxTokenSize
    if limit <= 0 {
        limit = DefaultScannerMaxTokenSize
    }
    buf := make([]byte, 0, 64*1024)
    s.Buffer(buf, limit)
    return s
}

// TruncateToolResult truncates content to maxBytes for SSE/TaskLog relay.
// Appends "\n... [truncated, N bytes total]" when truncated.
// maxBytes <= 0 uses DefaultMaxToolResultBytes.
func TruncateToolResult(content string, maxBytes int) string { ... }
```

* **Logic** (`TruncateToolResult`):
  - `len(content) <= maxBytes` → そのまま返す
  - 超過時: マーカー `"\n... [truncated, %d bytes total]"` のバイト長を考慮し、content を切り詰めてからマーカーを付与
  - UTF-8 安全: バイト境界で切る (マルチバイト文字の途中分割は許容 — ツール出力は ASCII 主体)

#### [NEW] [shared/libs/go/codingagent/stream_io_test.go](file://shared/libs/go/codingagent/stream_io_test.go)

* **Description**: R3 切り詰め + R1 Scanner の単体テスト。
* **Logic** — テーブル駆動:

| テスト名 | 入力 | 期待 |
|----------|------|------|
| `TestTruncateToolResult_UnderLimit` | 100B content, max 256KB | 変更なし |
| `TestTruncateToolResult_OverLimit` | 300KB content, max 256KB | len ≤ 256KB + マーカー含む |
| `TestTruncateToolResult_MarkerFormat` | 300KB | 末尾 `\n... [truncated, 307200 bytes total]` |
| `TestNewLargeLineScanner_ReadsOversizedLine` | 100KB 1 行 | 読める |

#### [MODIFY] [shared/libs/go/config/model_profiles.go](file://shared/libs/go/config/model_profiles.go)

* **Description**: R6 — AgentConfig に Scanner / ToolResult 上限フィールド追加。
* **Technical Design**:

```go
const (
    DefaultScannerMaxTokenBytes = 4 * 1024 * 1024  // 4194304
    DefaultMaxToolResultBytes   = 256 * 1024       // 262144
)

type AgentConfig struct {
    MaxPromptBytes         int `yaml:"max_prompt_bytes"`
    MaxExecutionSeconds    int `yaml:"max_execution_seconds"`
    IdleTimeoutSeconds     int `yaml:"idle_timeout_seconds"`
    ExecutionMode          string `yaml:"execution_mode"`
    ScannerMaxTokenBytes   int `yaml:"scanner_max_token_bytes"`
    MaxToolResultBytes     int `yaml:"max_tool_result_bytes"`
}

func (c AgentConfig) WithDefaults() AgentConfig {
    // existing defaults ...
    if out.ScannerMaxTokenBytes == 0 {
        out.ScannerMaxTokenBytes = DefaultScannerMaxTokenBytes
    }
    if out.MaxToolResultBytes == 0 {
        out.MaxToolResultBytes = DefaultMaxToolResultBytes
    }
    return out
}
```

* **Logic**: YAML 例 (仕様書より):

```yaml
coding_agents:
  codex:
    scanner_max_token_bytes: 4194304
    max_tool_result_bytes: 262144
  claudecode:
    scanner_max_token_bytes: 4194304
    max_tool_result_bytes: 262144
```

#### [MODIFY] [shared/libs/go/config/model_profiles_test.go](file://shared/libs/go/config/model_profiles_test.go)

* **Logic**:
  - `TestAgentConfig_ScannerAndToolResultDefaults` — ゼロ値 → 4194304 / 262144
  - `TestAgentConfig_ScannerAndToolResultOverride` — YAML 明示値が保持される

#### [MODIFY] [shared/libs/go/codingagent/adapter_config.go](file://shared/libs/go/codingagent/adapter_config.go)

* **Technical Design**:

```go
type AdapterConfig struct {
    // ... existing fields ...
    ScannerMaxTokenBytes int
    MaxToolResultBytes   int
}
```

#### [MODIFY] [shared/libs/go/codingagent/options.go](file://shared/libs/go/codingagent/options.go)

* **Technical Design**:

```go
type SessionConfig struct {
    // ... existing fields ...
    ScannerMaxTokenBytes int
    MaxToolResultBytes   int
}

func WithScannerMaxTokenBytes(n int) SessionOption { ... }
func WithMaxToolResultBytes(n int) SessionOption { ... }
```

* **Logic** (`ApplyDefaults`): `AdapterConfig.ScannerMaxTokenBytes` / `MaxToolResultBytes` を SessionConfig へ伝播 (SessionOption 明示値優先)。

---

### Phase 2 — codex process 修正 (R1, R2, R3)

#### [MODIFY] [shared/libs/go/codingagent/codex/process.go](file://shared/libs/go/codingagent/codex/process.go)

* **Description**: stdout / stderr Scanner 改修、EventResult フォールバック、ToolResult 切り詰め。
* **Technical Design**: goroutine 内ロジック (仕様書より):

```
読み取りループ:
  scanner := NewLargeLineScanner(stdout, cfg.ScannerMaxTokenBytes)
  resultEmitted := false
  for scanner.Scan() {
      line := scanner.Text()
      touchActivity()
      // ... DetectUserInputFromExecEvent ...
      ev := ParseExecEvent(line)
      if ev != nil {
          if ev.Type == EventToolResult {
              ev.Content = TruncateToolResult(ev.Content, cfg.MaxToolResultBytes)
          }
          if ev.Type == EventResult { resultEmitted = true }
          ch <- *ev
      }
  }
  if err := scanner.Err(); err != nil {
      log.Warn("codex stdout scanner error", "error", err)
      ch <- EventError{Content: "stdout read error: " + err.Error()}
  }
  <-stderrDone
  if err := cmd.Wait(); err != nil {
      // existing EventError on failure
  } else {
      if !resultEmitted {
          ch <- EventResult{}
      }
  }
```

* **Logic**:
  - stderr goroutine も `NewLargeLineScanner(stderrPipe, cfg.ScannerMaxTokenBytes)` を使用
  - **サイレント停止禁止** (R2): exit 0 + `!resultEmitted` → 必ず合成 `EventResult`
  - exit != 0 → 既存 `EventError` 維持
  - `resultEmitted` は `EventResult` 型イベント送出時のみ true

#### [MODIFY] [shared/libs/go/codingagent/codex/process_internal_test.go](file://shared/libs/go/codingagent/codex/process_internal_test.go)

* **Description**: R2 フォールバック + R1 Err 処理の process 層テスト (fake stdout、StartProcess 非依存の抽出関数テスト可)。
* **Technical Design**: 可能なら `readStdoutEvents` を package-private 関数として抽出し io.Pipe + fake process wait でテスト。抽出が過大な場合は fake codex 統合テストに委譲。
* **Logic**:

| テスト名 | シナリオ | 期待 |
|----------|----------|------|
| `TestStartProcess_EmitsEventResultOnExitZero` | oversized 行 + exit 0、turn.completed なし | 末尾 EventResult 1 件 |
| `TestStartProcess_NoDuplicateEventResult` | turn.completed あり + exit 0 | EventResult 1 件のみ |
| `TestStartProcess_ScannerErrorEmitsEventError` | Scanner 上限超過 (小さい maxTokenSize) | EventError 送出 |

---

### Phase 2 — claudecode 横展開 (R5)

#### [MODIFY] [shared/libs/go/codingagent/claudecode/process.go](file://shared/libs/go/codingagent/claudecode/process.go)

* **Description**: codex と同型の Scanner / Err / EventResult フォールバック / Truncate 適用。
* **Logic**: stdout / stderr goroutine を codex/process.go と同一パターンに揃える。`ParseJSONLinesEvent` 経路で `EventToolResult` / `EventResult` を同様に処理。

#### [MODIFY] [shared/libs/go/codingagent/claudecode/process_internal_test.go](file://shared/libs/go/codingagent/claudecode/process_internal_test.go)

* **Logic**: `TestClaudeStartProcess_EmitsEventResultOnExitZero`, `TestClaudeStartProcess_NoDuplicateEventResult`

---

### Phase 2 — AgentService 設定伝播 (R6)

#### [MODIFY] [shared/libs/go/agentservice/handler.go](file://shared/libs/go/agentservice/handler.go)

* **Description**: `handleSendMessage` / `handleRespond` の `opts` 構築時に AgentConfig から上限を SessionOption へ渡す。
* **Logic**:

```go
agentCfg := s.resolveAgentConfig(record.AgentName)
opts := []codingagent.SessionOption{
    // ... existing opts ...
}
if agentCfg.ScannerMaxTokenBytes > 0 {
    opts = append(opts, codingagent.WithScannerMaxTokenBytes(agentCfg.ScannerMaxTokenBytes))
}
if agentCfg.MaxToolResultBytes > 0 {
    opts = append(opts, codingagent.WithMaxToolResultBytes(agentCfg.MaxToolResultBytes))
}
```

* **Note**: process 層切り詰めが主防御。handler の `streamSSERelay` への追加チェックは本計画では **行わない** (スコープ最小化)。

#### [MODIFY] server 起動時の AdapterConfig 構築箇所

* **Description**: `registerCodingAgents` 相当で `ScannerMaxTokenBytes` / `MaxToolResultBytes` を AgentConfig から AdapterConfig へコピー。
* **File**: `server/` 内の agent 登録コード (grep `RegisterAgent` / `AdapterConfig{` で特定)

---

## Step-by-Step Implementation Guide

### Phase 1: 再現テスト (RED 確認)

- [x] **Step 1.1**: `tests/testutil/fake_codex.go` を新規作成 — `InstallFakeCodex`, `buildThreeLineReproLines` 実装
- [x] **Step 1.2**: `shared/libs/go/codingagent/codex/scanner_test.go` を新規作成 — `TestScanner_DefaultLimitStopsAt64KB` (PASS), `TestScanner_LargeLineReadsAllThreeLines` (FAIL 期待)
- [x] **Step 1.3**: `shared/libs/go/codingagent/claudecode/scanner_test.go` を新規作成 — 同上 claudecode 版
- [x] **Step 1.4**: `tests/codex_scanner_integration_test.go` を新規作成 — `TestCodexScannerIntegration_LargeOutputMissingEventResult` (FAIL 期待)
- [x] **Step 1.5**: `tests/codex_large_output_e2e_test.go` を新規作成 — `TestCodexE2E_LargeToolOutputTerminalEvent` (FAIL 期待)
- [x] **Step 1.6**: `./scripts/process/build.sh` 実行 — 再現テストが期待どおり RED であることを確認 (DefaultLimit PASS、他 FAIL)
- [x] **Step 1.7**: Phase 1 完了をコミット (`test: add repro tests for codex scanner 64KB limit`)

### Phase 2: 本修正

- [x] **Step 2.1**: `shared/libs/go/codingagent/stream_io.go` + `stream_io_test.go` 新規作成 (R1, R3 ユーティリティ)
- [x] **Step 2.2**: `config/model_profiles.go` + `model_profiles_test.go` 修正 (R6)
- [x] **Step 2.3**: `codingagent/adapter_config.go`, `codingagent/options.go` にフィールド + Option 追加、`ApplyDefaults` 更新 (R6)
- [x] **Step 2.4**: `codex/process.go` 修正 — Scanner / Err / Truncate / EventResult フォールバック (R1–R3)
- [x] **Step 2.5**: `codex/process_internal_test.go` に R2 テスト追加 (`process_repro_test.go` に実装)
- [x] **Step 2.6**: `claudecode/process.go` 同型修正 (R5)
- [x] **Step 2.7**: `claudecode/process_internal_test.go` に R5 テスト追加 (`process_repro_test.go` に実装)
- [x] **Step 2.8**: `agentservice/handler.go` + server agent 登録で R6 伝播 (server/ 不在のため handler 経由のみ)
- [x] **Step 2.9**: `./scripts/process/build.sh` — Phase 1 repro テスト含め全 PASS 確認

### Phase 3: 統合検証 (GREEN)

- [x] **Step 3.1**: `./scripts/process/integration_test.sh --specify "CodexScanner"`
- [x] **Step 3.2**: `./scripts/process/integration_test.sh --specify "LargeToolOutput"`
- [x] **Step 3.3**: `./scripts/process/integration_test.sh --specify "Codex"`
- [x] **Step 3.4**: `./scripts/process/integration_test.sh --specify "AgentService"`
- [x] **Step 3.5**: 全 Step 完了後コミット + push (build + integration 全 PASS 後)

## Verification Plan

### Automated Verification

#### Phase 1 (RED 確認 — 修正前)

1. **Build & Unit Tests**:
   ```bash
   ./scripts/process/build.sh
   ```
   期待:
   - `TestScanner_DefaultLimitStopsAt64KB` → PASS
   - `TestScanner_LargeLineReadsAllThreeLines` → **FAIL**
   - `TestCodexScannerIntegration_LargeOutputMissingEventResult` → **FAIL** (integration_test.sh 経由)
   - `TestCodexE2E_LargeToolOutputTerminalEvent` → **FAIL**

#### Phase 3 (GREEN 確認 — 修正後)

1. **Build & Unit Tests**:
   ```bash
   ./scripts/process/build.sh
   ```

2. **Integration Tests — Codex Scanner repro / fix**:
   ```bash
   ./scripts/process/integration_test.sh --specify "CodexScanner"
   ```

3. **Integration Tests — Large output E2E**:
   ```bash
   ./scripts/process/integration_test.sh --specify "LargeToolOutput"
   ```

4. **Integration Tests — Codex E2E 非退行**:
   ```bash
   ./scripts/process/integration_test.sh --specify "Codex"
   ```

5. **Integration Tests — AgentService SSE 非退行**:
   ```bash
   ./scripts/process/integration_test.sh --specify "AgentService"
   ```

### E2E Tests (tests/ 配下)

| ファイル | テスト | Phase 1 | Phase 3 |
|----------|--------|---------|---------|
| `tests/codex_scanner_integration_test.go` | `TestCodexScannerIntegration_LargeOutputMissingEventResult` | FAIL | PASS |
| `tests/codex_large_output_e2e_test.go` | `TestCodexE2E_LargeToolOutputTerminalEvent` | FAIL | PASS |
| `tests/codex_e2e_test.go` | 既存 `TestCodexE2E_*` | PASS | PASS |

## Documentation

影響を受ける既存ドキュメントの更新は不要 (内部バグ修正 + 設定フィールド追加のみ)。`model_profiles.yaml` のサンプルがリポジトリ内にあれば `scanner_max_token_bytes` / `max_tool_result_bytes` のコメント例を追記する (該当ファイル grep で確認後、存在すれば Step 2.2 に含める)。
