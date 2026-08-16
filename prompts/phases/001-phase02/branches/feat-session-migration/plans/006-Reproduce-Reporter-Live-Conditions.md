# 006-Reproduce-Reporter-Live-Conditions

> **Source Specification**: [ideas/005-Reproduce-Reporter-Live-Conditions.md](file://prompts/phases/001-phase02/branches/feat-session-migration/ideas/005-Reproduce-Reporter-Live-Conditions.md)
>
> **関連 Issue**: [axsh/arctic-tern#41](https://github.com/axsh/arctic-tern/issues/41)
>
> **観測**: [issuecomment-5309016142](https://github.com/axsh/arctic-tern/issues/41#issuecomment-5309016142), [issuecomment-5309034949](https://github.com/axsh/arctic-tern/issues/41#issuecomment-5309034949)

## Goal Description

kanban-gui は移植しない。報告のバグ再現モデル **`gpt-5.6-terra`** で本リポジトリ LIVE を走らせ、3/3 枯渇（`resume_mode=fresh`、stderr が実質 `exit status 1`）と同じ形が出るかを記録する。既存 `TestLiveCodex_*` の `liveCodexModel`（`gpt-4o`）と `process_retry` / 15 秒ドレインは変えない。再現しても `t.Error` しない。`t.Logf("reproduced_reporter_exhaustion=%v", ...)` に残す。

## User Review Required

仕様 URR は次で固定する。追加の判断事項はなし。

1. 再現テストのモデルは **`gpt-5.6-terra` のみ**。`liveCodexModel` は `gpt-4o` のまま。
2. kanban-gui（`kanban_summarizer_tern_live.sh`、busy-recovery）は移植しない。
3. `process_retry` 既定と 15 秒ドレインは変更しない。
4. ロガー注入は **`server.WithLogger` + テスト内 `captureLogger`** に固定する（ログファイル読みは使わない）。
5. R7（example profiles）と R8（20s/60s テーブル）は実装しない。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1 `tests/testdata/model_profiles.yaml` の openai models に `name: gpt-5.6-terra`。mode responses は付けない | Proposed Changes > testdata/model_profiles.yaml |
| R2 `TestLiveCodex_ReporterConditions_SingleCardReady`。model `"gpt-5.6-terra"`。session_dir なし。プロンプト `Reply with exactly: live-terra-ready`。待ち `4 * time.Minute`。再現でも FAIL しない。`t.Logf("reproduced_reporter_exhaustion=true\|false")`。PATH/vault/400 は Fatal | Proposed Changes > tests/llm_live_codex_test.go |
| R2 再現同型: SSE error に `exit status 1` と `[upstream_error]`、result なし。ERROR `codex process retry exhausted`、`attempt=3`、`max_attempts=3`、`resume_mode=fresh`、`exit_status=1`、stderr 実質 exit status 1 | logReporterExhaustion + 同テスト |
| R2 再現しない: result あり error なし、本文に `live-terra-ready`。PASS + reproduced=false | 同テスト |
| R3 `TestLiveCodex_ReporterConditions_ShortSSERead`。model terra。`http.Client{Timeout: 35 * time.Second}`。切断後 `client disconnected during SSE stream`。ドレイン Timeout / Gateway deadline は必須でない。Skip 禁止 | 同ファイル。`sendE2EMessage` は Timeout で Fatal するため使わない |
| R4 capture: `attempt` `max_attempts` `resume_mode` `exit_status` `stderr` `session_id` `agent_session_id_empty` `terminal_content` | `mustStartCodexE2EServerWithLogger` + `captureLogger` |
| R5 実装後に判定表を埋める（修復しない） | Verification Plan > 観測判定 |
| R6 `--specify "TestLiveCodex_ReporterConditions"`。`--categories` なし。`TestLiveCodex_` に混ぜない。gpt-4o は別コマンド | Verification Plan |
| R3 の 3s kanban drainSSE | 対象外 |
| R7 example yaml | 対象外 |
| R8 20s/60s | 対象外 |

## Proposed Changes

依存順（TDD Failed First）: `_test.go` のヘルパーとテスト関数を先に書く。yaml 未追加の初回実行はセッション作成 400 で `t.Fatal` する。そのあと testdata にモデル名を足す。本番修復コード（`handler_retry.go` 等）は変更禁止。

中間ファイルは `tmp/` のみ。

定数（仕様から継承。`liveCodexModel` は既存値のまま変更禁止）:

```go
const (
    liveCodexModel          = "gpt-4o"
    liveCodexReporterModel  = "gpt-5.6-terra"
    reporterShortSSETimeout = 35 * time.Second
    reporterLongSSETimeout  = 4 * time.Minute
)
```

枯渇 / 切断ログ本文（既存 `handler_retry.go`。変更禁止）:

```go
logCodexProcessRetryExhausted = "codex process retry exhausted"
logClientDisconnectedSSE      = "client disconnected during SSE stream"
logSSEDrainTimedOut           = "SSE drain timed out; stopping agent process"
```

分類タグ（既存 `codingagent.ErrorCodeUpstreamError`）: `"upstream_error"`。SSE / ログ stderr は `exit status 1 [upstream_error]` の形（報告コメントと同型）。

`logProcessRetryExhausted` が ERROR に載せる kv（既存。テストはこれを読む。本番は触らない）:

```go
fields := []any{
    "session_id", sessionID,
    "attempt", attempt,
    "max_attempts", maxAttempts,
    "resume_mode", resumeMode, // healFresh または AgentSessionID=="" なら "fresh"、それ以外 "resume"
    "agent_session_id", agentSessionID,
    "stderr", stderr, // truncateStderrTail(term.content, 8KiB)
    "stderr_empty", stderr == "",
    "agent_session_id_empty", agentSessionID == "",
    "terminal_content", drainTimeoutTerminalContentMatches(term.content),
}
// parseExitStatus 成功なら exit_status は数字文字列（報告は "1"）。失敗なら ""
```

報告と同型の stderr: `strings.Contains(stderr, "exit status 1")` かつ `strings.Contains(stderr, "["+codingagent.ErrorCodeUpstreamError+"]")`。CLI の `Reconnecting...` 本文は無い（空 stderr を process.go が `err.Error()` にした結果）。

`captureLogger` は `package agentservice_test` の [handler_retry_test.go](file://shared/libs/go/agentservice/handler_retry_test.go) と同型を `package llm_test` へ複製する（別パッケージのため import できない）。`WithComponent` は **同じ `*captureLogger` を返す**（AgentService が `WithComponent("agentservice")` しても capture が途切れないこと）。

### LIVE テスト

#### [MODIFY] [tests/llm_stream_reconnect_live_test.go](file://tests/llm_stream_reconnect_live_test.go)
*   **Description**: 既存 `mustStartCodexE2EServer` を logger 差し込み対応にする。LookPath Fatal、tmp config、`testdata/model_profiles.yaml`、Launch、cleanup は現行どおり。`t.Skip` を足さない。
*   **Technical Design**:

```go
func mustStartCodexE2EServer(t *testing.T) (string, func()) {
    return mustStartCodexE2EServerWithLogger(t, nil)
}

func mustStartCodexE2EServerWithLogger(t *testing.T, log logger.Logger) (string, func()) {
    t.Helper()
    if _, err := exec.LookPath("codex"); err != nil {
        t.Fatalf("LIVE test requires codex CLI on PATH: %v", err)
    }
    modelProfilesSrc, _ := filepath.Abs(filepath.Join("testdata", "model_profiles.yaml"))
    // 既存と同じ gwPort / wsPort / asPort / tmpConfig 文字列
    opts := []server.Option{server.WithConfigPath(tmpConfig)}
    if log != nil {
        opts = append(opts, server.WithLogger(log))
    }
    srv, err := server.New(opts...)
    if err != nil {
        t.Fatalf("server.New failed: %v", err)
    }
    if err := srv.Launch(context.Background()); err != nil {
        t.Fatalf("Launch failed: %v", err)
    }
    baseURL := fmt.Sprintf("http://localhost:%d", srv.AgentService().Port())
    cleanup := func() {
        shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()
        srv.Shutdown(shutCtx)
    }
    return baseURL, cleanup
}
```

`server.New` は `WithLogger` を `resolveLogger` 経由で AgentService に渡す（[server/server.go](file://server/server.go)）。ログファイル読みは使わない。

#### [MODIFY] [tests/llm_live_codex_test.go](file://tests/llm_live_codex_test.go)
*   **Description**: `captureLogger`、`createLiveCodexSessionWithModel`、`sendE2EMessageAllowErr`、R2/R3 テストを追加する。`liveCodexModel` と `TestLiveCodex_SingleCardReady` / `ResumeSameSession` / `ResumeAfterInProcessRestart` は変更しない。
*   **Technical Design**:

```go
type captureLogEntry struct {
    level string
    msg   string
    kv    []any
}

type captureLogger struct {
    mu      sync.Mutex
    entries []captureLogEntry
}

func (l *captureLogger) append(level, msg string, fields []any) {
    copied := append([]any(nil), fields...)
    l.mu.Lock()
    l.entries = append(l.entries, captureLogEntry{level: level, msg: msg, kv: copied})
    l.mu.Unlock()
}

func (l *captureLogger) Trace(msg string, fields ...any) { l.append("trace", msg, fields) }
func (l *captureLogger) Debug(msg string, fields ...any) { l.append("debug", msg, fields) }
func (l *captureLogger) Info(msg string, fields ...any)  { l.append("info", msg, fields) }
func (l *captureLogger) Warn(msg string, fields ...any)  { l.append("warn", msg, fields) }
func (l *captureLogger) Error(msg string, fields ...any) { l.append("error", msg, fields) }
func (l *captureLogger) WithFields(map[string]any) logger.Logger { return l }
func (l *captureLogger) WithComponent(string) logger.Logger     { return l }

func (l *captureLogger) find(level, msg string) (captureLogEntry, bool) {
    l.mu.Lock()
    defer l.mu.Unlock()
    for _, e := range l.entries {
        if e.level == level && e.msg == msg {
            return e, true
        }
    }
    return captureLogEntry{}, false
}

func kvLookup(kv []any, key string) (any, bool) {
    for i := 0; i+1 < len(kv); i += 2 {
        k, ok := kv[i].(string)
        if ok && k == key {
            return kv[i+1], true
        }
    }
    return nil, false
}

func kvFmt(kv []any, key string) string {
    v, ok := kvLookup(kv, key)
    if !ok {
        return "<missing>"
    }
    return fmt.Sprint(v)
}
```

`handler_retry_test.go` の `kvString` は欠落で `t.Fatal` する。再現観測では欠落を不合格にしないため `kvFmt` を使う。

```go
func createLiveCodexSessionWithModel(t *testing.T, baseURL, workDir, model string) string {
    t.Helper()
    initGitRepo(t, workDir)
    body, err := json.Marshal(map[string]string{
        "agent":    "codex",
        "model":    model,
        "work_dir": workDir,
    })
    if err != nil {
        t.Fatalf("marshal create session: %v", err)
    }
    resp, err := http.Post(baseURL+"/api/v1/sessions", "application/json", bytes.NewReader(body))
    if err != nil {
        t.Fatalf("create session: %v", err)
    }
    defer resp.Body.Close()
    if resp.StatusCode != http.StatusCreated {
        t.Fatalf("create session: expected 201, got %d", resp.StatusCode)
    }
    var result map[string]string
    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        t.Fatalf("decode create session: %v", err)
    }
    sid := result["session_id"]
    if sid == "" {
        t.Fatal("create session: empty session_id")
    }
    return sid
}

func createLiveCodexSession(t *testing.T, baseURL, workDir string) string {
    return createLiveCodexSessionWithModel(t, baseURL, workDir, liveCodexModel)
}

func sendE2EMessageAllowErr(t *testing.T, baseURL, sessionID, message string, timeout time.Duration) (*http.Response, error) {
    t.Helper()
    type contentPart struct {
        Type string `json:"type"`
        Text string `json:"text,omitempty"`
    }
    body, _ := json.Marshal(map[string]any{
        "content": []contentPart{{Type: "text", Text: message}},
    })
    req, _ := http.NewRequest("POST",
        baseURL+"/api/v1/sessions/"+sessionID+"/messages",
        bytes.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Accept", "text/event-stream")
    client := &http.Client{Timeout: timeout}
    return client.Do(req)
}
```

`session_dir` は JSON に含めない（`createLiveCodexSession` と同じ。fallback は `{work_dir}/.tern/{id}`）。

既存 `sendE2EMessage`（[tests/agentservice_e2e_test.go](file://tests/agentservice_e2e_test.go)）は `client.Do` の error で `t.Fatalf` する。R3 の `Timeout: 35 * time.Second` と枯渇後の再 Send では **使わない**。

*   **Logic** — `TestLiveCodex_ReporterConditions_SingleCardReady`:

```go
func TestLiveCodex_ReporterConditions_SingleCardReady(t *testing.T) {
    logs := &captureLogger{}
    baseURL, cleanup := mustStartCodexE2EServerWithLogger(t, logs)
    defer cleanup()
    workDir := t.TempDir()
    sessionID := createLiveCodexSessionWithModel(t, baseURL, workDir, liveCodexReporterModel)
    resp := sendE2EMessage(t, baseURL, sessionID, "Reply with exactly: live-terra-ready", reporterLongSSETimeout)
    defer resp.Body.Close()
    events, _ := parseE2ESSEEvents(t, resp)

    var sawResult, sawErr bool
    var errContent string
    var allText strings.Builder
    for _, ev := range events {
        if ev.Type == codingagent.EventResult {
            sawResult = true
        }
        if ev.Type == codingagent.EventError {
            sawErr = true
            errContent = ev.Content
        }
        if ev.Type == codingagent.EventText {
            allText.WriteString(ev.Content)
        }
    }
    entry, hasExhaust := logs.find("error", "codex process retry exhausted")
    stderr := kvFmt(entry.kv, "stderr")
    reproduced := !sawResult && sawErr &&
        strings.Contains(errContent, "exit status 1") &&
        strings.Contains(errContent, "["+codingagent.ErrorCodeUpstreamError+"]") &&
        hasExhaust &&
        kvFmt(entry.kv, "attempt") == "3" &&
        kvFmt(entry.kv, "max_attempts") == "3" &&
        kvFmt(entry.kv, "resume_mode") == "fresh" &&
        kvFmt(entry.kv, "exit_status") == "1" &&
        strings.Contains(stderr, "exit status 1") &&
        strings.Contains(stderr, "["+codingagent.ErrorCodeUpstreamError+"]")

    if sawResult && !sawErr && strings.Contains(allText.String(), "live-terra-ready") {
        t.Logf("reproduced_reporter_exhaustion=false")
        t.Logf("gpt-5.6-terra returned EventResult in this environment")
        return
    }
    t.Logf("reproduced_reporter_exhaustion=%v", reproduced)
    t.Logf("session_id=%s saw_result=%v saw_error=%v err=%q exhaust=%v", sessionID, sawResult, sawErr, errContent, hasExhaust)
    if hasExhaust {
        t.Logf("attempt=%s max_attempts=%s resume_mode=%s exit_status=%s stderr=%q terminal_content=%s agent_session_id_empty=%s",
            kvFmt(entry.kv, "attempt"),
            kvFmt(entry.kv, "max_attempts"),
            kvFmt(entry.kv, "resume_mode"),
            kvFmt(entry.kv, "exit_status"),
            stderr,
            kvFmt(entry.kv, "terminal_content"),
            kvFmt(entry.kv, "agent_session_id_empty"))
    }
    // t.Error しない。前提欠落（201 以外、PATH、Launch）だけ Fatal。
    if !reproduced {
        return
    }
    resend, resendErr := sendE2EMessageAllowErr(t, baseURL, sessionID, "Reply with exactly: live-terra-resend", 30*time.Second)
    if resend != nil {
        t.Logf("resend_status=%d", resend.StatusCode)
        io.Copy(io.Discard, resend.Body)
        resend.Body.Close()
    } else {
        t.Logf("resend_status=0 resend_err=%v", resendErr)
    }
}
```

*   **Logic** — `TestLiveCodex_ReporterConditions_ShortSSERead`（切断待ちは既存 `TestStreamSSERelay_DisconnectLogsClientGone` と同じ 2s / 20ms。見つからなくても `t.Error` しない）:

```go
func TestLiveCodex_ReporterConditions_ShortSSERead(t *testing.T) {
    logs := &captureLogger{}
    baseURL, cleanup := mustStartCodexE2EServerWithLogger(t, logs)
    defer cleanup()
    workDir := t.TempDir()
    sessionID := createLiveCodexSessionWithModel(t, baseURL, workDir, liveCodexReporterModel)
    resp, err := sendE2EMessageAllowErr(t, baseURL, sessionID, "Reply with exactly: live-terra-short", reporterShortSSETimeout)
    if resp != nil {
        io.Copy(io.Discard, resp.Body)
        resp.Body.Close()
    }
    t.Logf("short_sse_do_err=%v", err)
    deadline := time.Now().Add(2 * time.Second)
    for time.Now().Before(deadline) {
        if _, ok := logs.find("warn", "client disconnected during SSE stream"); ok {
            break
        }
        time.Sleep(20 * time.Millisecond)
    }
    entry, ok := logs.find("warn", "client disconnected during SSE stream")
    if ok {
        t.Logf("disconnect_session_id=%s events_sent=%s", kvFmt(entry.kv, "session_id"), kvFmt(entry.kv, "events_sent"))
    } else {
        t.Logf("client disconnected during SSE stream not observed (stream may have finished before 35s)")
    }
    if _, ok := logs.find("warn", "SSE drain timed out; stopping agent process"); ok {
        t.Logf("observed drain timeout log")
    }
}
```

必要な import 追加: `io`、`sync`、`github.com/axsh/arctic-tern/shared/libs/go/logger`。`mustStartCodexE2EServerWithLogger` 側に `github.com/axsh/arctic-tern/server`（既存）。

### testdata

#### [MODIFY] [tests/testdata/model_profiles.yaml](file://tests/testdata/model_profiles.yaml)
*   **Description**: openai モデル一覧に報告モデルを足し、セッション作成が 400 にならないようにする。TDD ではテスト追加の **あと** に書く。
*   **Technical Design**: `providers.openai.api_keys[0].models` にエントリを 1 つ追加。`mode: responses` は付けない（`gpt-5.3-codex` とは別。Codex の `wire_api=responses` とは無関係）。
*   **Logic**: `gpt-4o` の直後に次を挿入する。

```yaml
          - name: gpt-5.6-terra
```

### 変更しないファイル

*   [shared/libs/go/agentservice/handler_retry.go](file://shared/libs/go/agentservice/handler_retry.go) — `MaxAttempts`、ドレイン、分類、self-heal
*   [tests/llm_live_codex_test.go](file://tests/llm_live_codex_test.go) 内の既存 3 テスト（`TestLiveCodex_SingleCardReady` / `ResumeSameSession` / `ResumeAfterInProcessRestart`）のアサーション。`liveCodexModel` の値 `"gpt-4o"`
*   kanban-gui（リポジトリ外）
*   [settings/example/model_profiles.yaml](file://settings/example/model_profiles.yaml)

## Step-by-Step Implementation Guide

1.  [x] **起動ヘルパー (Failed First)**: [tests/llm_stream_reconnect_live_test.go](file://tests/llm_stream_reconnect_live_test.go) に `mustStartCodexE2EServerWithLogger` を追加し、既存 `mustStartCodexE2EServer` は `nil` logger で委譲する。
2.  [x] **capture とセッション (Failed First)**: [tests/llm_live_codex_test.go](file://tests/llm_live_codex_test.go) に `captureLogger`、`kvLookup`、`kvFmt`、`createLiveCodexSessionWithModel`、`sendE2EMessageAllowErr` を追加する。既存 `createLiveCodexSession` は `liveCodexModel` を渡すラッパにする。
3.  [x] **R2 テスト**: 同ファイルに `TestLiveCodex_ReporterConditions_SingleCardReady` を追加する。この時点では yaml 未追加なので、セッション作成は 400 で `t.Fatal` する（Failed First）。
4.  [x] **R3 テスト**: 同ファイルに `TestLiveCodex_ReporterConditions_ShortSSERead` を追加する。
5.  [x] **プロファイル**: [tests/testdata/model_profiles.yaml](file://tests/testdata/model_profiles.yaml) に `- name: gpt-5.6-terra` を追加し、201 でセッションが作れるようにする。
6.  [x] **検証コマンド**: 下の Automated Verification を実行する。再現しても修復しない。観測判定表を埋める。
7.  [x] **退行**: `TestLiveCodex_SingleCardReady`（`gpt-4o`）を別 `--specify` で実行する。

## Verification Plan

`integration_test.sh` に `--categories` は無い。付けない。kanban 手順は実行しない。

### Automated Verification

1.  **Build & Unit Tests**: `./scripts/process/build.sh`
2.  **Reporter LIVE**: `./scripts/process/integration_test.sh --specify "TestLiveCodex_ReporterConditions"`
3.  **gpt-4o 退行**: `./scripts/process/integration_test.sh --specify "TestLiveCodex_SingleCardReady"`
4.  **E2E**: 上記 LIVE が E2E。kanban-gui は追加しない。

Windows 一連:

```bash
./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestLiveCodex_ReporterConditions" && ./scripts/process/integration_test.sh --specify "TestLiveCodex_SingleCardReady"
```

Linux / Remote-SSH（Linux）:

```bash
./scripts/process/build.sh --skip-etc && xvfb-run -a ./scripts/process/integration_test.sh --specify "TestLiveCodex_ReporterConditions" && xvfb-run -a ./scripts/process/integration_test.sh --specify "TestLiveCodex_SingleCardReady"
```

`TestLiveCodex_ReporterConditions` は `TestLiveCodex_SingleCardReady` にマッチしない。Codex / vault 欠落は Fatal。

### 観測判定（実装実行後に記入。修復しない）

実行日時: 2026-08-17（Windows）。コマンド: `./scripts/process/build.sh` PASS。`./scripts/process/integration_test.sh --specify "TestLiveCodex_ReporterConditions"` PASS（24s）。`./scripts/process/integration_test.sh --specify "TestLiveCodex_SingleCardReady"` PASS（18s）。

| 観察 | 結果（実行後） | 解釈 |
| :--- | :--- | :--- |
| terra で 3/3 枯渇、stderr が exit status 1 のみ | なし。`TestLiveCodex_ReporterConditions_SingleCardReady` は 10.21s で PASS。SSE [DONE]、4 events | この環境では Tern + gpt-5.6-terra の silent exit 1 は再現しなかった |
| terra で EventResult | あり。`reproduced_reporter_exhaustion=false`。ログ: `gpt-5.6-terra returned EventResult in this environment` | 004 の gpt-4o LIVE と同じ成功経路。報告特有（当時の上流、負荷、または kanban クライアント） |
| `reproduced_reporter_exhaustion=` の値 | `false` | 報告と同型の枯渇ではなかった |
| 短縮 Timeout で切断ログ / events_sent | 観測なし。`short_sse_do_err=<nil>`。テスト 7.41s。`client disconnected during SSE stream not observed (stream may have finished before 35s)` | ストリームが 35s より前に完了したためクライアント締切は発火しなかった。Gateway deadline ログも出ていない |
| 枯渇後再 Send の HTTP ステータス | 未実行（`reproduced=false` のため early return） | 作り直し / 409 の確認は kanban 側、または枯渇が再現できた環境 |

本番コード（`process_retry`、15s ドレイン、分類、self-heal）は変更していない。

### 総合判定結果

**判定**: ⚠️ 条件付き確認完了

#### テスト結果サマリ
- 実行した LIVE テスト: 3 件（ReporterConditions 2 件 + gpt-4o SingleCardReady 1 件）
- 成功: 3 件
- 失敗: 0 件
- 事実上スキップ: 0 件（`t.Skip` なし。Codex PATH / vault は満たされていた）

#### チェック項目の結果
| # | チェック項目 | 結果 | 備考 |
|---|------------|------|------|
| 1 | スキップされたテスト | ✅ | Skip なし。前提欠落もなし |
| 2 | 部分的なエラー | ✅ | ReporterConditions は capture logger のため INFO が標準出力に出ないだけ。gpt-4o 側は Gateway / Codex 起動 INFO のみ |
| 3 | 迂回処理による偽成功 | ⚠️ | terra は EventResult で通った。報告の silent exit 1 経路は未通過。テストの PASS は「観測用テストが落ちなかった」であり「報告バグが直った」ではない |
| 4 | アダプタ・コンフィグの誤適用 | ✅ | R2/R3 は `liveCodexReporterModel`（`gpt-5.6-terra`）。退行は `liveCodexModel`（`gpt-4o`）。testdata に terra を登録済み |
| 5 | テスト間の依存・順序 | ✅ | `--specify` を分けて単独実行 |
| 6 | カバレッジ | ✅ | 報告モデル初回 Send と 35s SSE 読みはコード化済み。kanban busy-recovery は対象外 |
| 7 | 外部システム | ⚠️ | この Windows + 実 Codex + この時刻の上流では terra が成功。報告側 v0.1.16 では失敗。環境差が残る |

#### 判定理由
本リポジトリの等価条件（実 Codex、モデル `gpt-5.6-terra`、session_dir なし、リトライ既定 3/3）では初回 Send が約 10 秒で EventResult になった。したがって silent exit 1 の修復は行わない（計画どおり）。報告が赤のままなら、差分は上流の間欠障害、kanban クライアント締切、または busy-recovery による session 作り直し側である。クライアント 35 秒切断は、ストリームが先に終わったためこちらでは再現できなかった。

## Documentation

README の必須ゲート文（`TestLiveCodex_`）は gpt-4o のまま。報告モデル用 `--specify "TestLiveCodex_ReporterConditions"` は本計画の Verification にのみ書く。ReferenceManual の Send 仕様は変えない（挙動変更なし）。
