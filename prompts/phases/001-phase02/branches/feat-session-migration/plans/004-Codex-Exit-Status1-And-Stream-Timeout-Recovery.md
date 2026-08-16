# 004-Codex-Exit-Status1-And-Stream-Timeout-Recovery

> **Source Specification**: [ideas/003-Codex-Exit-Status1-And-Stream-Timeout-Recovery.md](file://prompts/phases/001-phase02/branches/feat-session-migration/ideas/003-Codex-Exit-Status1-And-Stream-Timeout-Recovery.md)
>
> **関連 Issue**: [axsh/arctic-tern#41](https://github.com/axsh/arctic-tern/issues/41)
>
> **先行計画**: [002-Codex-Stream-Reconnect-Recovery.md](file://prompts/phases/001-phase02/branches/feat-session-migration/plans/002-Codex-Stream-Reconnect-Recovery.md), [003-Stream-Reconnect-Regression-Coverage.md](file://prompts/phases/001-phase02/branches/feat-session-migration/plans/003-Stream-Reconnect-Regression-Coverage.md)
>
> **方針**: v0.1.14 後に残った live 失敗（分類されない `exit status 1`、壊れた `exec resume`、切断後の無制限ドレイン）を AgentService / Codex プロセス層で有界復旧する。Gateway ストリームリトライと kanban-gui 側テストは対象外。

## Goal Description

Codex CLI が stderr に既知の混雑文言を残さず `exit status 1` で落ちても、認証・引数ミス等の明示的な非リトライを除き有界回数（既定 3）のプロセス再実行対象にする。`codex exec resume` が retryable 失敗したら native `thread_id` を捨て、同一 Tern `session_id` のまま正本履歴をプロンプトへ注入して新規 `codex exec` で自己修復する。SSE 切断後のドレインは最大 15 秒で打ち切り、`ProcessManager.Stop()`（`codexSession.Close()`）と `execRegistry` 解除で後続 Send が 409 にならないようにする。中間リトライエラーは SSE に出さず、成功ストリームか尽きたときの分類エラー 1 回だけを返す。

## User Review Required

仕様の必須 3 点（R1/R2/R3）は計画に落とした。実装決定は次のとおり。反対がなければこの 4 点で進める。

1. **ドレイン上限は定数 15 秒。** 仕様の例「最大 15〜30秒」のうち **15 秒** を `defaultSSEClientDrainTimeout` とする。YAML 新フィールドは設けない。単体/統合で待たないために `WithSSEDrainTimeout(d)` を試験専用に追加する（本番経路は触らない）。30 秒や設定化が必要なら本計画の改訂が必要。
2. **尽きた汎用 `exit status 1` は `[upstream_error]`。** `IsRetryableUpstream` が真の最終エラーだけ `[upstream_overloaded]`。`ClassifiedErrorContent(msg, overloaded bool)` の第 2 引数はこれまでどおりタグ選択に使う。
3. **Self-heal は resume 付き attempt が retryable 失敗した直後。** 同一ターンの次 attempt と、永続化した `record.AgentSessionID` のクリアの両方を行う。001 の「同一 resume id で再実行」は、**プロセスが死んだ retryable 失敗**に限り本計画が上書きする。生存中 JSONL の in-process 復旧（exit 0）は変えない。プロンプト超過など仕様リスト外の終了も R1 により retryable になる。
4. **任意 R5（指数バックオフ）は実装しない。** `ProcessRetryConfig.IntervalSeconds` の現行固定待機のまま。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: 非ゼロ終了は明示的非リトライ以外 `Retryable: true`。空 stderr / `"exit status 1"` のみも対象。既定 3 回超過後は安定タグ付き EventError を 1 回 | Proposed Changes > codingagent/retry.go `IsNonRetryableError`、codex/process.go `cmd.Wait()` 分岐、handler_retry.go 枯渇時分類 |
| R1 非リトライ一覧: `unauthorized`, `invalid api key`, `authentication failed`, `model not found`, `unknown model`, `invalid argument`, `flag provided but not defined` | Proposed Changes > `IsNonRetryableError`（URR どおり `invalid_api_key` も含む） |
| R2: resume の retryable 失敗後、次 attempt は `AgentSessionID` 空の新規 `codex exec`。正本から補完注入。同一 Tern `session_id` | Proposed Changes > handler_retry.go `runTurn`、handler_session.go `wrapPromptForSelfHeal`、handler_retry_test.go `TestHandleSendMessage_BrokenResumeThreadSelfHeals` |
| R3: `<-ctx.Done()` 後のドレイン上限（15 秒）。超過で `ProcessManager.Stop()` と busy 解除。直後の同一セッション Send が 409 にならない | Proposed Changes > streamSSERelay / respondJSONRelay の drain timer、`finishActiveExecution`、回帰 `TestStreamReconnectRegression_DrainTimeoutUnregistersBusy` |
| R4: 中間リトライエラーを SSE に出さない。成功ストリームまたは最終分類エラー 1 回 | 現行 `streamSSERelay` の retryable 飲み込みを維持。汎用 exit 1 でも同じ経路。検証はシナリオ A 統合 |
| R5: ProcessRetryConfig 指数バックオフ | 対象外（URR4） |
| シナリオ A: fake が stderr なし exit 1 → 2 回目 EventResult。SSE に exit status 1 の error イベントなし | Verification > シナリオ A。`TestStartProcess_GenericExit1IsRetryable`、`TestStreamReconnectRegression_GenericExit1RetriesWithoutSSEError` |
| シナリオ B: ターン1 で `AgentSessionID=thr-broken`。ターン2 の `resume thr-broken` が exit 1。同一 session_id で新規 exec 成功。ターン3 も成功 | Verification > シナリオ B。`TestHandleSendMessage_BrokenResumeThreadSelfHeals`、`TestStreamReconnectRegression_BrokenResumeThreadSelfHeals` |
| シナリオ C: 無応答プロセス中にクライアント切断。15 秒相当の上限後に Stop、busy 解除。直後 Send が 409 でない | Verification > シナリオ C。`TestStreamSSERelay_DrainTimeoutStopsProcess`、`TestStreamReconnectRegression_DrainTimeoutUnregistersBusy` |

## Proposed Changes

依存順: 判定関数と単体テスト → プロセス終了分類 → fake CLI 拡張 → AgentService self-heal / ドレイン → 統合回帰。各コンポーネントは `_test.go` を先に書く（Failed First）。

### codingagent（非リトライ判定）

#### [MODIFY] [shared/libs/go/codingagent/retry_test.go](file://shared/libs/go/codingagent/retry_test.go)
*   **Description**: `IsNonRetryableError` のテーブル駆動。既存 `TestIsRetryableUpstream` は変更しない（空文字は引き続き false）。
*   **Technical Design**: 追加するテストは仕様の名前どおり `TestIsNonRetryableError`。
*   **Logic**:
    ```go
    func TestIsNonRetryableError(t *testing.T) {
        tests := []struct {
            in   string
            want bool
        }{
            {"unauthorized", true},
            {"UNAUTHORIZED: token expired", true},
            {"invalid api key", true},
            {"Invalid API Key provided", true},
            {"invalid_api_key", true},
            {"authentication failed", true},
            {"model not found", true},
            {"unknown model gpt-nope", true},
            {"invalid argument", true},
            {"flag provided but not defined: --foo", true},
            {"exit status 1", false},
            {"codex CLI process exited with error (exit status 1)", false},
            {"", false},
            {"Reconnecting... 1/5 (We're currently experiencing high demand, which may cause temporary errors.)", false},
            {"prompt size exceeds the limit", false},
        }
        for _, tt := range tests {
            t.Run(tt.in, func(t *testing.T) {
                got := codingagent.IsNonRetryableError(tt.in)
                if got != tt.want {
                    t.Errorf("IsNonRetryableError(%q) = %v, want %v", tt.in, got, tt.want)
                }
            })
        }
    }
    ```

#### [MODIFY] [shared/libs/go/codingagent/retry.go](file://shared/libs/go/codingagent/retry.go)
*   **Description**: 仕様の非リトライ集合を `IsNonRetryableError` として追加する。`IsRetryableUpstream` / `IsRetryableError` / `ClassifiedErrorContent` の既存シグネチャは変えない。
*   **Technical Design**:
    ```go
    // IsNonRetryableError reports a fatal Codex CLI / auth / argv failure
    // that must not trigger process re-exec.
    func IsNonRetryableError(msg string) bool
    ```
*   **Logic**:
    ```go
    func IsNonRetryableError(msg string) bool {
        if msg == "" {
            return false
        }
        lower := strings.ToLower(msg)
        needles := []string{
            "unauthorized",
            "invalid api key",
            "invalid_api_key",
            "authentication failed",
            "model not found",
            "unknown model",
            "invalid argument",
            "flag provided but not defined",
        }
        for _, n := range needles {
            if strings.Contains(lower, n) {
                return true
            }
        }
        return false
    }
    ```
    仕様 R1 の文言をそのまま `strings.Contains`（小文字化後）で照合する。空文字は非リトライではない（プロセス側で retryable にする）。`IsRetryableUpstream("")` は現行どおり false のまま。

### Codex プロセス終了分類

#### [MODIFY] [shared/libs/go/codingagent/codex/process_repro_test.go](file://shared/libs/go/codingagent/codex/process_repro_test.go)
*   **Description**: 仕様の `TestStartProcess_GenericExit1IsRetryable`。既存 `TestStartProcess_NonRetryableExitNoRetryableFlag`（stderr=`unauthorized`）と `TestStartProcess_RetryableExitSetsRetryableFlag`（high demand stderr）は残す。
*   **Logic**:
    1. `testfake.Install` に `Lines: nil`（または空）、`Stderr: ""`、`ExitCode: 1`。
    2. `codex.StartProcess` のチャネルを読み切る。
    3. `EventError` が 1 件以上あり、最後の `Retryable == true`。
    4. `Content` に `"exit status 1"` を含む（stderr が空のため `cmd.Wait()` の `err.Error()` が使われる現行分岐）。
    5. 非リトライ回帰としてテーブルを 1 件追加してもよいが、必須は既存 unauthorized テストの維持。

#### [MODIFY] [shared/libs/go/codingagent/codex/process.go](file://shared/libs/go/codingagent/codex/process.go)
*   **Description**: `cmd.Wait()` 失敗時の retryable 判定を仕様アプローチ 1 に差し替える。
*   **Technical Design**: `StartProcess` の Wait 失敗ブロックだけ変更。`ProcessManager.Stop` は変更しない（R3 から呼ぶ既存 API）。
*   **Logic**: 現行:
    ```go
    retryable := codingagent.IsRetryableUpstream(errMsg) || codingagent.IsRetryableError(err)
    content := errMsg
    if retryable {
        content = codingagent.ClassifiedErrorContent(errMsg, true)
    }
    ```
    置換後（仕様: `!IsNonRetryableError(errMsg)` なら `Retryable: true`。空 stderr は現行どおり `errMsg = err.Error()`）:
    ```go
    if err := cmd.Wait(); err != nil {
        errMsg := strings.TrimSpace(stderrBuf.String())
        if errMsg == "" {
            errMsg = err.Error()
        }
        log.Warn("codex CLI process exited with error", "error", err.Error(), "stderr", errMsg)
        retryable := !codingagent.IsNonRetryableError(errMsg)
        content := errMsg
        if retryable {
            overloaded := codingagent.IsRetryableUpstream(errMsg)
            content = codingagent.ClassifiedErrorContent(errMsg, overloaded)
        }
        log.Debug("classified process exit error", "retryable", retryable)
        select {
        case ch <- codingagent.StreamEvent{
            Type:      codingagent.EventError,
            Content:   content,
            Retryable: retryable,
        }:
        case <-procCtx.Done():
        }
    }
    ```
    これにより `"exit status 1"` のみ、および接続系（`IsRetryableError` 相当の文言が stderr になくても非リトライ集合に入らない限り）は retryable。`unauthorized` 等は `Retryable: false` のまま。尽きたときのタグは handler 側でも `IsRetryableUpstream` で overloaded / error を分ける（下記）。

### fake CLI（回帰用）

#### [MODIFY] [shared/libs/go/codingagent/codex/testfake/install.go](file://shared/libs/go/codingagent/codex/testfake/install.go)
*   **Description**: シナリオ A（空 stderr の FailLaunches）、B（特定 resume id だけ失敗）、C（終端せずハング）を fake で再現する。既存 `FailLaunches` + 空 `FailStderr` → `DefaultFailStderr` の既定は**維持**（計画 003 の回帰を壊さない）。
*   **Technical Design**: `Options` と埋め込み `config` にフィールド追加。
    ```go
    type Options struct {
        Lines         []string
        Stderr        string
        ExitCode      int
        LineDelay     time.Duration
        LaunchLogPath string
        PIDFile       string
        HeartbeatPath string
        FailLaunches  []int
        FailStderr    string
        SilentFail    bool     // FailLaunches 時に stderr を出さず exit 1
        FailResumeIDs []string // argv に "resume <id>" があるとき SilentFail 相当
        HangForever   bool     // Lines 出力後（または空のまま）殺されるまでブロック
    }
    ```
*   **Logic**（埋め込み main）:
    1. 起動ログ追記のあとに resume 判定: `os.Args` を走査し、`arg == "resume"` の次要素が `FailResumeIDs` に含まれれば `shouldFail = true` かつ silent。
    2. `FailLaunches` ヒット時、`SilentFail == true` なら stderr なし `os.Exit(1)`。`SilentFail == false` なら現行どおり `FailStderr` 空時は `DefaultFailStderr`。
    3. `HangForever == true` なら Lines（あれば）を出したあと `select {}`。`PIDFile` / `HeartbeatPath` は現行どおり先に書く。
    4. 既存 `TestStreamReconnectRegression_ThreeResumeSends` 等は新フィールドゼロ値のため挙動不変。

### AgentService（self-heal・ドレイン）

#### [MODIFY] [shared/libs/go/agentservice/handler_retry_test.go](file://shared/libs/go/agentservice/handler_retry_test.go)
*   **Description**: 仕様の `TestHandleSendMessage_BrokenResumeThreadSelfHeals`。既存 `TestHandleSendMessage_CodexRetryableProcessRetriesSameResume` は URR3 に合わせて「2 回目 CreateSession の `AgentSessionID` が空」に更新する（プロセス死亡後は同一 resume を使わない）。`TestHandleSendMessage_CodexRetryExhaustedOneClassifiedError` は high demand モックのため引き続き `[upstream_overloaded]` 1 回。汎用 exit 1 枯渇用テストを追加。ドレイン上限の単体を追加。
*   **Technical Design**:
    `retryAgent` にフィールド追加:
    ```go
    type retryAgent struct {
        // 既存: name, nativeID, failTimes, nonRetry, delay, creates, closes, cfgs, sendDone, earlyClose
        failResumeID    string // CreateSession 時 cfg.AgentSessionID がこの値なら retryable EventError
        genericFailOnce bool   // failResume 時の Content を "exit status 1"
        nextNativeID    string // self-heal 成功時に EventSystem.SessionID へ出す新 thread
    }
    ```
    `Server` 試験用: `WithSSEDrainTimeout`。
*   **Logic**:
    **`TestHandleSendMessage_BrokenResumeThreadSelfHeals`**
    1. `nativeID = "thr-broken"`、`failResumeID = "thr-broken"`、`nextNativeID = "thr-fresh"`、`ProcessRetryConfig{MaxAttempts: 3, IntervalSeconds: 0}`。
    2. ターン1: `CreateSession` は resume なし。Send は System(`thr-broken`) + Result。HTTP `session_id` を保持。
    3. ターン2: 1 回目 CreateSession の `cfg.AgentSessionID == "thr-broken"`。Send は `EventError{Content: "exit status 1", Retryable: true}` のみ。2 回目 CreateSession の `AgentSessionID == ""`。Send は System(`thr-fresh`) + Result。SSE の `"type":"error"` は 0、`"type":"result"` あり。
    4. GET または後続 Send で HTTP `session_id` がターン1 と同一。
    5. ターン3: CreateSession は `thr-fresh`（成功時に保存された新 ID）または空でも Result 成功。`creates` は 1 + 2 + 1 = 4。

    **`TestHandleSendMessage_GenericExit1ExhaustedUpstreamError`**
    1. モックが毎回 `EventError{Content: "exit status 1", Retryable: true}`、`MaxAttempts: 2`。
    2. SSE `EventError` はちょうど 1、本文に `[upstream_error]`、`[upstream_overloaded]` は無い。
    3. 直後の同一 session Send が 409 でない。

    **`TestStreamSSERelay_DrainTimeoutStopsProcess`**（HTTP 経由、`retryAgent.delay` をドレインより長くする）
    1. `WithSSEDrainTimeout(80 * time.Millisecond)`、`delay: 10 * time.Second`。
    2. SSE POST を 20ms で cancel。
    3. 500ms 以内に `Close()` が呼ばれる（`earlyClose` または `closes >= 1`）。
    4. 直後 `postSSE` の status が 409 でない。
    5. 既存 `TestHandleSendMessage_ClientDisconnectDoesNotFinishUntilTerminal` は delay=400ms < 既定 15s のため、ドレイン完了まで待つ現行期待を維持。

    **`TestHandleSendMessage_SelfHealWrapsCanonicalHistory`**（正本注入）
    1. 実セッションストア + `SessionDir` にユーザー+assistant 履歴を 1 ターン分書き、`AgentSessionID="thr-broken"`。
    2. 壊れた resume のあと 2 回目 CreateSession の `cfg.Prompt`（または Send に渡る文字列）に `portable.TransferHeader`（`Tern session context transfer`）が含まれる。生のユーザー文の重複注入は最終ブロック 1 回だけ。

#### [MODIFY] [shared/libs/go/agentservice/handler_session_test.go](file://shared/libs/go/agentservice/handler_session_test.go)
*   **Description**: `wrapPromptForSelfHeal` の単体。既存 supplement テストは Delta（他 origin）のまま。
*   **Logic**:
    1. Canonical に origin=codex の user/assistant と、末尾の現在 user（TotalSeq）を置く。
    2. `wrapPromptForSelfHeal` の結果は `Tern session context transfer` を含み、assistant 本文を含み、末尾は引数の `userPrompt`。TotalSeq の現在 user 本文が supplement 側に二重に出ない。
    3. `SessionDir == ""` なら入力プロンプトをそのまま返す。

#### [MODIFY] [shared/libs/go/agentservice/service.go](file://shared/libs/go/agentservice/service.go)
*   **Description**: ドレイン上限の試験用オプション。`ProcessRetryConfig` は変更しない（R5 対象外）。
*   **Technical Design**:
    ```go
    type Server struct {
        // 既存フィールド...
        processRetry       config.ProcessRetryConfig
        processRetryCustom bool
        sseDrainTimeout    time.Duration
    }

    // WithSSEDrainTimeout overrides the post-disconnect drain bound for tests.
    // Production zero value uses defaultSSEClientDrainTimeout (15s).
    func WithSSEDrainTimeout(d time.Duration) ServerOption {
        return func(s *Server) { s.sseDrainTimeout = d }
    }
    ```
*   **Logic**: `New` / `NewWithStore` は既存どおり。ゼロ値の解釈は `handler_retry.go` のヘルパに置く。

#### [MODIFY] [shared/libs/go/agentservice/handler_session.go](file://shared/libs/go/agentservice/handler_session.go)
*   **Description**: resume 破棄後に Codex が空スレッドになるため、Wayfinder 正本の**自エージェント履歴を含む**補完を組み立てる。既存 `wrapPromptWithSupplement` の `portable.Delta`（`origin != target` のみ）はエージェント切替用のまま使い、self-heal では使わない。
*   **Technical Design**:
    ```go
    func (s *Server) wrapPromptForSelfHeal(ctx context.Context, record *codingagent.SessionRecord, userPrompt string) (string, error)
    func (s *Server) clearPersistedAgentSessionID(sessionID string)
    ```
*   **Logic** `wrapPromptForSelfHeal`:
    1. `record.SessionDir == ""` なら `return userPrompt, nil`。
    2. `c := session.OpenCanonical(record.SessionDir)`。`LoadMetadata` 失敗なら `userPrompt`。
    3. `msgs, err := c.LoadRange(1, meta.TotalSeq)`。失敗または空なら `userPrompt`。
    4. 現在ターンのユーザー行を除外する: `Seq == meta.TotalSeq && Role == "user"` の要素を落とす（`handleSendMessage` が wrap 前に `AppendSessionMessage` するため）。
    5. 残りが空なら `userPrompt`。
    6. `strat, err := portable.MergeStrategy(s.serverSupplement(), meta.Supplement, portable.Strategy{})`。失敗はエラー返却。`strat = portable.WithDefaults(strat)`。`strat.Model == ""` なら `record.Model`。
    7. `sup, err := portable.BuildSupplement(ctx, record.AgentName, prior, strat, s.summarizer)`。
    8. `return portable.WrapPrompt(sup, userPrompt), nil`。

    `clearPersistedAgentSessionID`:
    ```go
    func (s *Server) clearPersistedAgentSessionID(sessionID string) {
        rec, err := s.sessions.Get(sessionID)
        if err != nil || rec.AgentSessionID == "" {
            return
        }
        rec.AgentSessionID = ""
        _ = s.sessions.Update(rec)
        if s.logger != nil {
            s.logger.Warn("cleared broken native thread id for self-heal",
                "session_id", sessionID)
        }
    }
    ```

#### [MODIFY] [shared/libs/go/agentservice/handler.go](file://shared/libs/go/agentservice/handler.go)
*   **Description**: `runTurn` に self-heal 用の生ユーザープロンプトを渡す。`finishActiveExecution` は現行のまま R3 の掃除経路（`agentSess.Close()` → `ProcessManager.Stop()`、`UnregisterActiveSession`、`UnregisterExecCancel`、`execRegistry.Unregister`）。
*   **Technical Design**: `handleSendMessage` で wrap 前の文字列を保持する。
    ```go
    rawUserPrompt := promptText // ExtractText / multimodal 構築直後
    wrapped, wrapErr := s.wrapPromptWithSupplement(...)
    promptText = wrapped.prompt
    s.runTurn(r, w, execCtx, execCancel, record, sessionID, turnID, req.CorrelationID, promptText, rawUserPrompt, resumeID, opts, savedFiles)
    ```
    `runTurn` の引数に `rawUserPrompt string` を追加（`handler_retry.go`）。

#### [MODIFY] [shared/libs/go/agentservice/handler_retry.go](file://shared/libs/go/agentservice/handler_retry.go)
*   **Description**: 仕様アプローチ 2 と 3。resume 失敗後の fresh exec、正本 wrap、切断ドレイン期限、枯渇時タグ分岐。R4 の「中間 EventError を書かない」は現行 `handleEvent` の `ev.Retryable` 分岐を維持。
*   **Technical Design**:
    ```go
    const defaultSSEClientDrainTimeout = 15 * time.Second

    func (s *Server) clientDrainTimeout() time.Duration {
        if s.sseDrainTimeout > 0 {
            return s.sseDrainTimeout
        }
        return defaultSSEClientDrainTimeout
    }

    func (s *Server) sessionOptsWithResume(base []codingagent.SessionOption, sessionID, fallback string) []codingagent.SessionOption
    // 現行どおり: record.AgentSessionID があればそれを resume、無ければ fallback。

    func (s *Server) runTurn(
        r *http.Request,
        w http.ResponseWriter,
        execCtx context.Context,
        execCancel func(),
        record *codingagent.SessionRecord,
        sessionID, turnID, correlationID, promptText, rawUserPrompt, fallbackResume string,
        baseOpts []codingagent.SessionOption,
        savedFiles []string,
    )
    ```
    `streamSSERelay` / `respondJSONRelay` の戻り `streamTerminal` にドレイン打ち切りを載せる（`retryable: false`）。クライアント切断後は SSE へ書かない（現行 `clientGone`）。
*   **Logic**:
    **`runTurn` ループ（仕様フローチャート）**:
    ```
    A[SendMessage] --> B[codex exec resume / exec]
    B --> C{終了}
    C -- EventResult --> D[finish & DONE]
    C -- 非 retryable EventError --> E[分類 EventError 1 回]
    C -- retryable --> F{attempt < maxAttempts?}
    F -- Yes --> G[AgentSessionID クリア & 正本 wrap した fresh exec]
    G --> B
    F -- No --> H[[upstream_error] または [upstream_overloaded] を 1 回]
    ```
    実装手順:
    1. `healFresh := false`。`attemptPrompt := promptText`。
    2. 各 attempt 冒頭: `healFresh` なら `s.clearPersistedAgentSessionID(sessionID)` 済み前提で `sessionOptsWithResume(baseOpts, sessionID, "")`（fallback も空）。さらに `wrapped, err := s.wrapPromptForSelfHeal(execCtx, record, rawUserPrompt)`。成功時 `attemptPrompt = wrapped`。失敗時は `rawUserPrompt` のまま進める（セッション ID 維持を優先し、wrap 失敗だけで 500 にしない。ログ Warn）。
    3. `CreateSession(execCtx, opts...)`。失敗が初回登録前なら現行どおり 500。登録後なら現行どおり分類エラー。
    4. `sess.Send(execCtx, attemptPrompt)`。
    5. resume を使ったかどうか: `CreateSession` 直前の `record.AgentSessionID != "" || (!healFresh && fallbackResume != "")`。
    6. `streamSSERelay` / `respondJSONRelay` が `term.retryable && attempt < maxAttempts`:
       - 今回 resume を使っていたら `s.clearPersistedAgentSessionID(sessionID)` と `healFresh = true`。
       - `s.closeAttempt(sessionID, sess)`。`agentSess = nil`。
       - `interval` 待機（現行。0 なら即時）。`execCtx.Done()` なら `finishActiveExecution` + SSE なら DONE。
       - `continue`。
    7. `term.retryable` で回数切れ:
       ```go
       overloaded := codingagent.IsRetryableUpstream(term.content)
       content := codingagent.ClassifiedErrorContent(term.content, overloaded)
       s.emitClassifiedSSE(w, active, content, overloaded) // JSON 経路は events に 1 件追加
       ```
       汎用 `exit status 1` は overloaded=false → `[upstream_error]`。high demand 文言は `[upstream_overloaded]`。
    8. 成功時の `thread.started` → `EventSystem.SessionID` による `record.AgentSessionID` 上書きは現行 `handleRelaySideEffects` のまま。
    9. Claude 等 `processRetryLimits` が `maxAttempts=1` のエージェントはループが 1 回のため self-heal も走らない（仕様対象外: Codex フォーカス）。

    **`streamSSERelay` ドレイン（仕様アプローチ 3）**:
    現行の `clientGone` 時 `for { ev, ok := <-ch }` 無制限待ちを、上限付き `select` に置換する。
    ```go
    var drainTimer *time.Timer
    ensureDrainTimer := func() <-chan time.Time {
        if drainTimer == nil {
            drainTimer = time.NewTimer(s.clientDrainTimeout())
        }
        return drainTimer.C
    }
    defer func() {
        if drainTimer != nil {
            drainTimer.Stop()
        }
    }()
    ```
    `clientGone == true` のとき:
    ```go
    select {
    case ev, ok := <-ch:
        if !ok {
            goto done
        }
        if handleEvent(ev) {
            return term, true
        }
    case <-ensureDrainTimer():
        if s.logger != nil {
            s.logger.Warn("SSE drain timed out; stopping agent process",
                "session_id", sessionID,
                "timeout", s.clientDrainTimeout().String())
        }
        if exec.agentSess != nil {
            _ = exec.agentSess.Close() // Codex: ProcessManager.Stop (taskkill / SIGTERM)
        }
        term = streamTerminal{
            kind:      codingagent.EventError,
            retryable: false,
            content:   "client drain timeout",
        }
        goto done
    }
    ```
    `ctx.Done()` で初めて `clientGone=true` にする現行ログ（`client disconnected during SSE stream`）は残す。上限内に `turn.completed` が来れば計画 003 の `TestStreamReconnectRegression_DisconnectDoesNotKillFake` どおり Stop しない。

    **`respondJSONRelay`**: 同じ `clientDrainTimeout` と `exec.agentSess.Close()` を入れる。JSON クライアント切断でも busy が残らないようにする。

    **`runTurn` が drain timeout の non-retryable を受けたとき**: クライアントは既に切断しているので SSE に EventError を新たに書かない。`finishActiveExecution` で unregister。`writeSSEDone` は接続が生きていれば現行どおり（切断後の write 失敗は無視してよい）。

### 統合 / 回帰

#### [MODIFY] [tests/llm_stream_reconnect_regression_test.go](file://tests/llm_stream_reconnect_regression_test.go)
*   **Description**: 仕様の統合コマンド `--specify "TestStreamReconnectRegression"` に乗るよう、既存 prefix で 3 シナリオを追加。`t.Skip` / `t.Skipf` 禁止。
*   **Logic**:
    **`TestStreamReconnectRegression_GenericExit1RetriesWithoutSSEError`（シナリオ A）**
    1. `SilentFail: true`, `FailLaunches: []int{1}`, `Lines` は `thread.started` + `turn.completed`, `LaunchLogPath` 付き, `IntervalSeconds: 0`。
    2. 1 回 SSE Send。status 200。`sseErrorCount == 0`。body に `"type":"result"`。本文に `exit status 1` の error イベントなし。
    3. `LaunchCount == 2`。

    **`TestStreamReconnectRegression_BrokenResumeThreadSelfHeals`（シナリオ B）**
    1. `FailResumeIDs: []string{"thr-broken"}`, `Lines` 1 回目（resume なし）は `thread.started thr-broken` + `turn.completed`。resume 失敗後の新規 exec は同じ Lines だと再び `thr-broken` になるため、**resume 無しの 2 回目以降は `thread.started thr-healed`** が必要。
    2. fake 拡張: `SuccessThreadID` を 1 つに固定すると B が壊れる。代わりに埋め込み main で「`resume` が argv に無ければ launch 回数に応じた thread_id」にするか、`Lines` を使わず thread_id を `thr-broken`（launch 1）/ `thr-healed`（resume なしの launch >=2）とハードコードする試験専用オプション:
       ```go
       ThreadIDByLaunch map[int]string `json:"thread_id_by_launch,omitempty"`
       ```
       未設定時は `Lines` をそのまま出力（既存テスト不変）。
    3. ターン1 Send 成功。ターン2: launch ログに `resume thr-broken` が 1 行あり、その直後の launch 行に `resume` が無い。SSE に error イベントなし、result あり。HTTP `session_id` 不変。
    4. ターン3 Send 200、error 0、result あり。

    **`TestStreamReconnectRegression_DrainTimeoutUnregistersBusy`（シナリオ C）**
    1. `HangForever: true`, `PIDFile` あり, `Lines` は `thread.started` のみ（`turn.completed` なし）。
    2. `agentservice.New(..., WithSSEDrainTimeout(80*time.Millisecond), WithProcessRetry(...))` — `newFakeCodexHTTP` に timeout 引数を足すか、このテストだけ Server を直接組む。
    3. SSE を 20ms で cancel。
    4. 1 秒以内に同一 `session_id` へ `postReconnectSSE`。`code != 409`。
    5. PID が生存していれば失敗（Windows は `taskkill /T` 後にプロセス消失を `os.FindProcess` + 短い poll で確認。確認不能な環境では 409 否定を主断言、PID は best-effort）。

    既存 `TestStreamReconnectRegression_DisconnectDoesNotKillFake`（ドレイン中は殺さない）は既定 15s > ターン完了時間のため残す。本テストは明示的に短い timeout + HangForever で対比する。

#### [MODIFY] [docs/ReferenceManual-WebAPIs.md](file://docs/ReferenceManual-WebAPIs.md)
*   **Description**: `POST /api/v1/sessions/:id/messages` のエラー説明に、Codex プロセス再実行と切断ドレインの観測仕様を追記する。新エンドポイントは無い。
*   **Logic**:
    - クライアントに見えるのは成功 SSE、または枯渇時の `"type":"error"` 1 件（`[upstream_overloaded]` または `[upstream_error]`）。中間の `exit status 1` は出さない。
    - SSE 切断後、サーバは最大 15 秒プロセス完了を待ち、その後強制停止して busy を解除する。同一 `session_id` の後続 POST は 409 にしない（まだ実行中かつ上限未経過のときは現行どおり 409）。

## Step-by-Step Implementation Guide

1. [x] **Failed First: IsNonRetryableError**: `retry_test.go` に `TestIsNonRetryableError` を追加し、`retry.go` に実装。
2. [x] **Failed First: GenericExit1**: `TestStartProcess_GenericExit1IsRetryable` と `process.go` の Wait 分岐置換。
3. [x] **testfake 拡張**: `SilentFail`, `FailResumeIDs`, `HangForever`, `ThreadIDByLaunch`。
4. [x] **Failed First: wrapPromptForSelfHeal**: `handler_session_selfheal_test.go` と `handler_session.go`。
5. [x] **Failed First: handler_retry**: self-heal / 枯渇 `[upstream_error]` / ドレイン Stop / SameResume 期待更新。
6. [x] **service.go / handler.go / handler_retry.go**: `WithSSEDrainTimeout`、`rawUserPrompt`、`healFresh`、ドレイン timer、枯渇タグ分岐。
7. [x] **統合 3 本**: `tests/llm_stream_reconnect_regression_test.go` にシナリオ A/B/C。
8. [x] **ドキュメント**: `docs/ReferenceManual-WebAPIs.md`。
9. [x] **検証**: `build.sh` と `integration_test.sh --specify TestStreamReconnectRegression` を成功。

## Verification Plan

`--categories` は `integration_test.sh` に未実装のため付けない（未知フラグで失敗する）。位置づけは llm。実行は `--specify` のみ。LIVE（実 Codex）は本計画の必須ゲートに含めない。

### Automated Verification

Windows:

```bash
./scripts/process/build.sh
```

```bash
./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestStreamReconnectRegression"
```

Linux / Remote-SSH（Linux）:

```bash
./scripts/process/build.sh --skip-etc
```

```bash
./scripts/process/build.sh --skip-etc && xvfb-run -a ./scripts/process/integration_test.sh --specify "TestStreamReconnectRegression"
```

単体は `build.sh` に含まれる次を Failed First の対象とする（生の `go test` は計画に書かない）:

- `TestIsNonRetryableError`
- `TestStartProcess_GenericExit1IsRetryable`
- `TestHandleSendMessage_BrokenResumeThreadSelfHeals`
- `TestHandleSendMessage_GenericExit1ExhaustedUpstreamError`
- `TestStreamSSERelay_DrainTimeoutStopsProcess`

### シナリオ別（仕様の検証シナリオを個別項目にする）

1. **シナリオ A**: `TestStartProcess_GenericExit1IsRetryable` と `TestStreamReconnectRegression_GenericExit1RetriesWithoutSSEError`。stderr 空の exit 1 → 再実行後 Result。SSE に `"type":"error"` なし。
2. **シナリオ B**: `TestHandleSendMessage_BrokenResumeThreadSelfHeals` と `TestStreamReconnectRegression_BrokenResumeThreadSelfHeals`。`thr-broken` resume 失敗 → 同一 HTTP `session_id` で fresh exec → ターン3 成功。
3. **シナリオ C**: `TestStreamSSERelay_DrainTimeoutStopsProcess` と `TestStreamReconnectRegression_DrainTimeoutUnregistersBusy`。切断後上限で Stop、直後 Send が 409 でない。対比として既存 `TestStreamReconnectRegression_DisconnectDoesNotKillFake` が上限内完了なら殺さないこと。

### E2E Tests

本リポジトリの自動ゲートは `tests/llm_stream_reconnect_regression_test.go`（HTTP/SSE + fake CLI）とする。kanban-gui の `TestSummarizerRealTern_*` は別リポジトリのため本計画にコードを追加しない。Issue #41 の実 Codex 再検証は任意の手動/LIVE であり必須コマンドに含めない。

## Documentation

- [docs/ReferenceManual-WebAPIs.md](file://docs/ReferenceManual-WebAPIs.md): Send の分類エラーと 15 秒ドレイン。
- [settings/example/config.yaml](file://settings/example/config.yaml): 変更しない（`process_retry` 既存のまま。ドレインは定数）。
- README: プロセス再実行の意味が変わる場合のみ、`exit status 1` が有界再実行対象である旨を追記。新 API は無い。
