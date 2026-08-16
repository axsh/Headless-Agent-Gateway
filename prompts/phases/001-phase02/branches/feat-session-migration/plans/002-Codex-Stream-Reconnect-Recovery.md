# 002-Codex-Stream-Reconnect-Recovery

> **Source Specification**: [ideas/001-Codex-Stream-Reconnect-Recovery.md](file://prompts/phases/001-phase02/branches/feat-session-migration/ideas/001-Codex-Stream-Reconnect-Recovery.md)
>
> **関連 Issue**: [axsh/arctic-tern#41](https://github.com/axsh/arctic-tern/issues/41)

## Goal Description

一時的な上流ストリーム障害（`Reconnecting... 1/5` / high demand）を終端失敗にせず、Gateway と Codex プロセスの有界リトライで `resume+send` を回復する。SSE 切断でもターン終了まで CLI を kill しない。尽きても `[upstream_overloaded]` を 1 回だけ返し、同じ Tern `session_id` で後続 Send できるようにする。

## User Review Required

仕様の 4 点は計画に落とした。追加の実装決定は次のとおり。反対がなければこのまま進める。

1. **YAML `max_retries: 0`（ゼロ値）**: 区別できないので「リトライ無し」にしない。`ApplyDefaults` で `MaxRetries=2`（初回 + 再試行 2 = 最大 3 試行）、`InitialDelaySeconds=1`、`MaxDelaySeconds=8`。明示的にリトライを止めたい場合は将来フィールドが必要だが、本計画では設けない（Issue 再現をゼロ既定で残さない）。
2. **Codex プロセス再実行**: `agent_service.process_retry.max_attempts` のゼロ値は **3**（初回含む）。`interval_seconds` ゼロ値は **3**（`DefaultRetryInterval` と同じ 3s）。対象は `record.AgentName == "codex"` のみ。
3. **`StreamEvent.Retryable`**: `json:"-"` の内部フラグ。SSE には出さない。retryable 終了は AgentService が飲み、尽きたときだけ `Content` に `[upstream_overloaded]` を付けた `EventError` を 1 回出す。
4. **client/v1**: R2 により途中 reconnect を SSE に出さないため変更しない。
5. **R6 プロセス再実行（Claude/Wayfinder）/ R7 / R8**: 本計画では実装しない。R1 分類と R4 非 kill と Gateway retry は共通経路なので Claude にも効く。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: retryable 判定・安定コード `[upstream_overloaded]` / `[upstream_error]` | Proposed Changes > codingagent/retry.go |
| R2: 生存中の reconnect / retryable stdout error を終端 EventError にしない | Proposed Changes > codex/protocol.go, codex/process.go |
| R3 Gateway: `llm_gateway.retry` 配線、ゼロ値は安全既定 | Proposed Changes > config.go, llmgateway/stream_retry.go, openai/handler.go, anthropic/handler.go |
| R3 Codex プロセス: 同一 resume id / 同一プロンプトで有界再実行、尽きれば分類エラー 1 回 | Proposed Changes > agentservice/handler.go, config AgentServiceConfig |
| R4: SSE 切断で即 `finishActiveExecution` / `taskkill` しない。切断だけでは `StatusError` にしない | Proposed Changes > agentservice/handler.go streamSSERelay |
| R5: 分類失敗後も同じ session_id で Send 可。ingest は成功ターンのみ（現行維持） | Proposed Changes > handler.go execRegistry 解除、ingest.go は変更しない |
| R6 Claude/Wayfinder プロセス再実行 | 対象外（分類・非 kill・Gateway は共通で適用） |
| R7 native 自動再作成 | 対象外 |
| R8 上流容量・モデル切替 | 対象外 |
| シナリオ A | Verification / `TestStreamReconnect_InProcessReconnectSucceeds` |
| シナリオ B | Verification / `TestStreamReconnect_ProcessRetrySameResume` |
| シナリオ C | Verification / `TestStreamReconnect_ExhaustedReturnsClassifiedError` |
| シナリオ D | Verification / `TestStreamReconnect_ClientDisconnectDoesNotKillCLI` |
| シナリオ E | Verification / `TestStreamReconnect_NonRetryableNoRetry` |
| シナリオ F（LIVE） | Verification / `TestStreamReconnectLiveResumeSend`（任意） |

## Proposed Changes

### codingagent（分類器）

#### [MODIFY] [shared/libs/go/codingagent/retry_test.go](file://shared/libs/go/codingagent/retry_test.go)

*   **Description**: Failed First。`IsRetryableUpstream` と `ClassifiedErrorContent` のテーブル駆動。
*   **Technical Design**:
    ```go
    func TestIsRetryableUpstream(t *testing.T) {
        tests := []struct {
            in   string
            want bool
        }{
            {"Reconnecting... 1/5 (We're currently experiencing high demand, which may cause temporary errors.)", true},
            {"RECONNECTING... 2/5", true},
            {"We're currently experiencing high demand", true},
            {"HTTP 429 too many requests", true},
            {"upstream overloaded", true},
            {"EOF", true},
            {"read: connection reset by peer", true},
            {"write: broken pipe", true},
            {"dial tcp: connection refused", true},
            {"connectex: no connection", true},
            {"unauthorized", false},
            {"invalid api key", false},
            {"model not found", false},
            {"prompt size exceeds the limit", false},
            {"", false},
        }
    }

    func TestClassifiedErrorContent(t *testing.T) {
        // retryable true  → 末尾 " [upstream_overloaded]"。既に含まれていれば二重に付けない
        // retryable false → 末尾 " [upstream_error]"
        // msg 空 → "[upstream_overloaded]" のみ
    }

    func TestIsRetryableError_StillMatchesConnectionOnly(t *testing.T)
    // 既存 EOF 等は維持。high demand 文字列は IsRetryableError(err) 単体では false のままでよい
    // （接続エラー専用）。IsRetryableUpstream が拡張判定。
    ```
*   **Logic**: 仕様 R1 の部分一致（大文字小文字無視）をそのままケースにする。

#### [MODIFY] [shared/libs/go/codingagent/retry.go](file://shared/libs/go/codingagent/retry.go)

*   **Description**: 上流ストリーム用の判定と安定コード付き Content を追加する。`Retry()` の本番呼び出しは本計画でも増やさない（Gateway / Codex は独自ループ。間隔は `DefaultRetryInterval` を流用してよい）。
*   **Technical Design**:
    ```go
    const (
        ErrorCodeUpstreamOverloaded = "upstream_overloaded"
        ErrorCodeUpstreamError      = "upstream_error"
    )

    // IsRetryableUpstream reports whether a log/stderr/API message is a transient
    // upstream stream failure.
    func IsRetryableUpstream(msg string) bool {
        if msg == "" {
            return false
        }
        if IsRetryableError(errors.New(msg)) {
            return true
        }
        lower := strings.ToLower(msg)
        return strings.Contains(lower, "reconnecting...") ||
            strings.Contains(lower, "we're currently experiencing high demand") ||
            strings.Contains(lower, "too many requests") ||
            strings.Contains(lower, "overloaded") ||
            strings.Contains(lower, "429")
    }

    func ClassifiedErrorContent(msg string, retryable bool) string {
        code := ErrorCodeUpstreamError
        if retryable {
            code = ErrorCodeUpstreamOverloaded
        }
        tag := "[" + code + "]"
        msg = strings.TrimSpace(msg)
        if msg == "" {
            return tag
        }
        if strings.Contains(msg, tag) {
            return msg
        }
        return msg + " " + tag
    }
    ```
    `IsRetryableError` の既存本体は変えない:
    ```
    EOF || connection reset || broken pipe || connection refused || connectex
    ```
*   **Logic**: 判定入力は Codex stderr、stdout `error`/`turn.failed` の message、Gateway 上流エラー文字列。認証・不明モデル・プロンプト検証は false。

#### [MODIFY] [shared/libs/go/codingagent/event.go](file://shared/libs/go/codingagent/event.go)

*   **Description**: 内部フラグを追加する。SSE JSON には出さない。
*   **Technical Design**:
    ```go
    type StreamEvent struct {
        Type          EventType              `json:"type"`
        Content       string                 `json:"content,omitempty"`
        // ...既存フィールドそのまま...
        Error         error                  `json:"-"`
        Retryable     bool                   `json:"-"`
    }
    ```
*   **Logic**: AgentService がプロセス再実行の可否を見るため。クライアントへは尽きたときだけ `Content` の `[upstream_overloaded]` で伝える。

### config

#### [MODIFY] [shared/libs/go/config/config_test.go](file://shared/libs/go/config/config_test.go)

*   **Description**: Failed First。`ApplyDefaults` 後の retry 既定と `process_retry` 既定。
*   **Technical Design**:
    ```go
    func TestLLMGatewayRetry_ZeroBecomesBoundedDefault(t *testing.T)
    // YAML 相当のゼロ値 AppConfig に ApplyDefaults
    // Retry.MaxRetries == 2
    // Retry.InitialDelaySeconds == 1
    // Retry.MaxDelaySeconds == 8

    func TestAgentServiceProcessRetry_ZeroBecomesThree(t *testing.T)
    // ProcessRetry.MaxAttempts == 3
    // ProcessRetry.IntervalSeconds == 3
    ```
*   **Logic**: ゼロのまま放置して Issue を再現させない。

#### [MODIFY] [shared/libs/go/config/config.go](file://shared/libs/go/config/config.go)

*   **Description**: Gateway retry 既定と Codex プロセス再実行設定。
*   **Technical Design**:
    ```go
    // AgentServiceConfig に追加
    ProcessRetry ProcessRetryConfig `yaml:"process_retry"`

    type ProcessRetryConfig struct {
        MaxAttempts     int `yaml:"max_attempts"`
        IntervalSeconds int `yaml:"interval_seconds"`
    }
    ```
    `LLMGatewayConfig.ApplyDefaults` に追加:
    ```go
    if c.Retry.MaxRetries == 0 {
        c.Retry.MaxRetries = 2
    }
    if c.Retry.InitialDelaySeconds == 0 {
        c.Retry.InitialDelaySeconds = 1
    }
    if c.Retry.MaxDelaySeconds == 0 {
        c.Retry.MaxDelaySeconds = 8
    }
    ```
    AgentService 側は `New` 時または handler で:
    ```go
    if cfg.ProcessRetry.MaxAttempts == 0 {
        cfg.ProcessRetry.MaxAttempts = 3
    }
    if cfg.ProcessRetry.IntervalSeconds == 0 {
        cfg.ProcessRetry.IntervalSeconds = 3
    }
    ```
    `RetrySettings` のコメントを「0 = 未設定 → ApplyDefaults で 2」に直す（現行「0 = no retry」を消す）。
*   **Logic**: 仕様「ゼロ値なら実装側の安全な既定。ゼロのまま放置して Issue を再現させたままにしない」。

### Codex アダプタ

#### [MODIFY] [shared/libs/go/codingagent/codex/protocol_test.go](file://shared/libs/go/codingagent/codex/protocol_test.go)

*   **Description**: Failed First。retryable な `error` / `turn.failed` は nil（終端にしない）。非 retryable は従来どおり EventError。
*   **Technical Design**:
    ```go
    func TestParseExecEvent_RetryableErrorIgnored(t *testing.T)
    // {"type":"error","message":"Reconnecting... 1/5 (We're currently experiencing high demand, which may cause temporary errors.)"}
    // ParseExecEvent == nil

    func TestParseExecEvent_RetryableTurnFailedIgnored(t *testing.T)
    // {"type":"turn.failed","error":{"message":"We're currently experiencing high demand"}}
    // ParseExecEvent == nil

    func TestParseExecEvent_NonRetryableErrorStillEventError(t *testing.T)
    // {"type":"error","message":"unauthorized"}
    // Type==EventError, Content に unauthorized
    ```
*   **Logic**: プロセスが生きている間は内部 5 回再接続を待つ。成功すれば通常 `EventResult`。

#### [MODIFY] [shared/libs/go/codingagent/codex/protocol.go](file://shared/libs/go/codingagent/codex/protocol.go)

*   **Description**: `error` / `turn.failed` で retryable なら nil。
*   **Technical Design**:
    ```go
    case "error":
        if codingagent.IsRetryableUpstream(ev.Message) {
            return nil
        }
        return &codingagent.StreamEvent{Type: codingagent.EventError, Content: ev.Message}

    case "turn.failed":
        // 既存どおり nested error.message を msg に取る。空なら "codex turn failed"
        if codingagent.IsRetryableUpstream(msg) {
            return nil
        }
        return &codingagent.StreamEvent{Type: codingagent.EventError, Content: msg}
    ```
*   **Logic**: R2。stderr Debug は process.go の現行 `CLI stderr line` のまま。

#### [MODIFY] [shared/libs/go/codingagent/codex/process_repro_test.go](file://shared/libs/go/codingagent/codex/process_repro_test.go) または同梱の process テスト

*   **Description**: Failed First。fake CLI が stderr に reconnect を書いてから JSONL 成功 exit 0 → チャネルに EventError が無い。retryable stderr で exit 1 → EventError は `Retryable==true` かつ Content に `[upstream_overloaded]`。非 retryable exit 1 は `Retryable==false`。
*   **Technical Design**:
    既存 `installFakeCodexForProcessTest` を拡張し、stderr 書き出しと exit code を制御できるようにする（環境変数 `FAKE_CODEX_STDERR` / `FAKE_CODEX_EXIT`）。
    ```go
    func TestStartProcess_ReconnectStderrDoesNotEmitEventErrorOnSuccess(t *testing.T)
    // stderr: Reconnecting... 1/5 (...high demand...)
    // stdout: turn.completed 相当 JSONL
    // exit 0
    // 受信イベントに Type==EventError が無い。EventResult がある。

    func TestStartProcess_RetryableExitSetsRetryableFlag(t *testing.T)
    // stderr high demand, exit 1, stdout 空
    // 最後の EventError.Retryable==true
    // Content は codingagent.ClassifiedErrorContent(stderr, true)

    func TestStartProcess_NonRetryableExitNoRetryableFlag(t *testing.T)
    // stderr "unauthorized", exit 1
    // Retryable==false
    ```
*   **Logic**: 生存中 reconnect は終端にしない。死んで retryable なら AgentService が再実行するためにフラグを立てる。生 stderr を SSE 終端として出さないのは AgentService 側。

#### [MODIFY] [shared/libs/go/codingagent/codex/process.go](file://shared/libs/go/codingagent/codex/process.go)

*   **Description**: `cmd.Wait()` 失敗時に分類する。
*   **Technical Design**: 現行:
    ```go
    errMsg := strings.TrimSpace(stderrBuf.String())
    if errMsg == "" { errMsg = err.Error() }
    log.Warn("codex CLI process exited with error", ...)
    ch <- StreamEvent{Type: EventError, Content: errMsg}
    ```
    変更後:
    ```go
    retryable := codingagent.IsRetryableUpstream(errMsg) || codingagent.IsRetryableError(err)
    content := errMsg
    if retryable {
        content = codingagent.ClassifiedErrorContent(errMsg, true)
    }
    ch <- codingagent.StreamEvent{
        Type:      codingagent.EventError,
        Content:   content,
        Retryable: retryable,
    }
    ```
*   **Logic**: 非 retryable は現行どおり終端。retryable は AgentService が SSE に出す前に飲み込む。`Stop()` の `taskkill` は terminate / ターン終了の Close からのみ。本ファイルの Stop 実装は変えなくてよい。

### LLM Gateway

#### [NEW] [shared/libs/go/llmgateway/stream_retry_test.go](file://shared/libs/go/llmgateway/stream_retry_test.go)

*   **Description**: Failed First。開始失敗の有界リトライ、先頭エラーチャンクの再接続、非 retryable は 1 回、成功データを書いたあとのエラーは再試行しない。
*   **Technical Design**:
    ```go
    func TestOpenStreamWithRetry_RetriesRetryableStartError(t *testing.T)
    // opener が 2 回目まで IsRetryableUpstream な error、3 回目成功
    // 呼び出し 3 回、戻り値成功、MaxRetries=2

    func TestOpenStreamWithRetry_NonRetryableNoRetry(t *testing.T)
    // "unauthorized" → 1 回で返す

    func TestOpenStreamWithRetry_ZeroConfigUsesDefaults(t *testing.T)
    // RetrySettings{} に ApplyDefaults 相当を通すと 2 回リトライする

    func TestShouldRetryStreamChunk_RetryableErrorBeforeAnyData(t *testing.T)
    func TestShouldRetryStreamChunk_AfterDataWrittenFalse(t *testing.T)
    ```
*   **Logic**: ストリーム開始失敗と、クライアント（Codex）へまだ成功チャンクを書いていない先頭 BifrostError のみ再接続する。成功トークンを出したあとに retry すると重複するため禁止。壊れたストリームを continue で引きずらな。

#### [NEW] [shared/libs/go/llmgateway/stream_retry.go](file://shared/libs/go/llmgateway/stream_retry.go)

*   **Description**: `llm_gateway.retry` を使う共通ループ。
*   **Technical Design**:
    ```go
    func retryBackoff(cfg config.RetrySettings, attempt int) time.Duration
    // delay = InitialDelaySeconds * 2^attempt 秒、MaxDelaySeconds でキャップ
    // attempt は 0 始まりの失敗回数

    func OpenStreamWithRetry[T any](
        ctx context.Context,
        cfg config.RetrySettings,
        log logger.Logger,
        open func() (T, error),
    ) (T, error)
    // 最大 1+MaxRetries 回。error が nil なら返す。
    // IsRetryableUpstream(err.Error()) または IsRetryableError(err) のときだけ待機して再 open。
    // 非 retryable または回数尽きは最後の error。

    func IsRetryableStreamErr(err error, msg string) bool
    // err != nil なら IsRetryableError(err) || IsRetryableUpstream(err.Error())
    // 加えて IsRetryableUpstream(msg)
    ```
*   **Logic**: Bifrost が設定を受け取らなくても Tern 側で有界再試行する。

#### [MODIFY] [shared/libs/go/llmgateway/openai/handler.go](file://shared/libs/go/llmgateway/openai/handler.go)

*   **Description**: `ResponsesStreamRequest` と非ストリーム失敗を retry で包む。エラーチャンクは成功データ前ならストリームを捨てて再 open。書いたあとはエラーを 1 回送って終了（`continue` 無限禁止）。
*   **Technical Design**:
    `handleResponsesStream` 内:
    1. `cfg := ctx.Config().LLMGateway`（ApplyDefaults 済み前提）
    2. `ch, err := llmgateway.OpenStreamWithRetry(r.Context(), cfg.Retry, log, func() (chan *Chunk, error) { return sdk.ResponsesStreamRequest(...) })`
    3. ヘッダ WriteHeader の **前** に open 成功させる（開始失敗は HTTP エラーのまま）。
    4. 読み取りループ:
       - `BifrostError` かつまだ `chunkCount==0` かつ retryable → チャネルを破棄し `OpenStreamWithRetry` 相当の残り回数で再 open。ヘッダ未送信ならまだ 502 にできる。ヘッダ送信後は再 open したチャネルを同じ SSE に書く。
       - 成功レスポンスを 1 つでも書いたあとの `BifrostError` → 現行どおり `event: error` を **1 回** 書いて **return**（continue しない）。
       - 非 retryable 先頭エラー → 現行 WriteErrorResponse / event error で終了、リトライ無し。
    非ストリーム `ChatCompletionsRequest` / `ResponsesRequest` も開始エラーが retryable なら同じ `OpenStreamWithRetry` パターン（関数名は `DoWithRetry` でも可）で包む。
*   **Logic**: Gateway は上流 high demand を吸収してから Codex に渡す。Codex の 1/5 より手前で減らせる。

#### [MODIFY] [shared/libs/go/llmgateway/anthropic/handler.go](file://shared/libs/go/llmgateway/anthropic/handler.go)

*   **Description**: Claude 経路にも同じストリーム retry を適用する（仕様: R1/R3 Gateway は共通。Claude プロセス再実行はしない）。
*   **Technical Design**: `handleMessagesBifrostStream` を openai と同じ「開始 retry / 先頭エラー再 open / データ後は 1 回で終了」にする。
*   **Logic**: Claude 確認結果どおり、上流 Anthropic ストリームも吸収する。

### AgentService

#### [MODIFY] [shared/libs/go/agentservice/handler_session_test.go](file://shared/libs/go/agentservice/handler_session_test.go) および必要なら [handler_test.go](file://shared/libs/go/agentservice/handler_test.go)

*   **Description**: Failed First。recordingAgent を拡張し、CreateSession 回数・Retryable EventError・切断後 Close が遅延完了まで呼ばれないことを見る。
*   **Technical Design**:
    ```go
    type retryAgent struct {
        name        string
        failTimes   int
        creates     int
        closes      int
        nativeID    string
        delay       time.Duration
        closedEarly chan struct{} // Close() が Send 完了前に来たら close
    }
    // 1..failTimes 回目の Send は Retryable EventError を出してチャネル close
    // その後は EventSystem(nativeID)+EventResult

    func TestHandleSendMessage_CodexRetryableProcessRetriesSameResume(t *testing.T)
    // agent Name()=="codex", AgentSessionID 既存, failTimes=1
    // SSE に EventError 無し、EventResult あり
    // creates==2, 2 回目の WithAgentSessionID が同じ native id

    func TestHandleSendMessage_CodexRetryExhaustedOneClassifiedError(t *testing.T)
    // failTimes=99, process_retry.max_attempts=2（オプション注入）
    // SSE の EventError は 1 件, Content に [upstream_overloaded]
    // creates==2（上限）
    // 直後に同じ session へ POST messages が 409 でない（busy 解除）

    func TestHandleSendMessage_NonRetryableNoProcessRetry(t *testing.T)
    // EventError Retryable=false, "unauthorized"
    // creates==1, 直ちに EventError（分類 [upstream_error] は任意だが Content は失わない）

    func TestHandleSendMessage_ClientDisconnectDoesNotFinishUntilTerminal(t *testing.T)
    // delay 付き成功。httptest で SSE 開始後にリクエストキャンセル。
    // delay 完了まで Close 回数 0。完了後に Close>=1、execRegistry に残らない。
    ```
    `WithProcessRetry(config.ProcessRetryConfig)` または `WithSupplementConfig` と同様の ServerOption を追加してテストから上限を 2 にする。
*   **Logic**: シナリオ B/C/D/E の単体相当。Name が `claudecode` のときは failTimes=1 でも creates==1（R3 は Codex のみ）。

#### [MODIFY] [shared/libs/go/agentservice/service.go](file://shared/libs/go/agentservice/service.go)

*   **Description**: `processRetry config.ProcessRetryConfig` と `WithProcessRetry`。`New` でゼロ値なら MaxAttempts=3, IntervalSeconds=3。
*   **Technical Design**:
    ```go
    type Server struct {
        // 既存...
        processRetry config.ProcessRetryConfig
    }
    func WithProcessRetry(cfg config.ProcessRetryConfig) ServerOption
    ```
    `server/server.go` の `resolveAgentService` で `cfg.AgentService.ProcessRetry` を渡す。
*   **Logic**: 回数は設定可能、無制限禁止。

#### [MODIFY] [shared/libs/go/agentservice/handler.go](file://shared/libs/go/agentservice/handler.go)

*   **Description**: Codex のみプロセス再実行。SSE 切断は drain して終端待ち。retryable EventError は attempts 残なら SSE に出さず再 CreateSession。尽きたら分類 Content を 1 回出す。切断だけでは `finalizeSessionStatusOnDisconnect` の `StatusError` を走らせない。
*   **Technical Design**:

    定数的ループ（Send の SSE 分岐）:
    ```go
    maxAttempts := 1
    interval := time.Duration(0)
    if record.AgentName == "codex" {
        maxAttempts = s.processRetry.MaxAttempts
        if maxAttempts <= 0 { maxAttempts = 3 }
        sec := s.processRetry.IntervalSeconds
        if sec <= 0 { sec = 3 }
        interval = time.Duration(sec) * time.Second
    }

    var lastRetryableErr string
    for attempt := 1; attempt <= maxAttempts; attempt++ {
        agentSess, err := agent.CreateSession(execCtx, opts...)
        // RegisterActiveSession / execRegistry は既存どおり。再試行時は前プロセス Close 済みであること。
        ch, err := agentSess.Send(execCtx, promptText)
        relay := newEventRelay(ch)
        active.agentSess = agentSess
        active.relay = relay
        terminal, suspended, classified := s.streamSSERelay(r.Context(), w, active, true)
        if suspended {
            return
        }
        if terminal == codingagent.EventResult {
            s.finishActiveExecution(sessionID, agentSess, savedFiles)
            // 既存: unregister 後に [DONE]
            fmt.Fprintf(w, "data: [DONE]\n\n")
            return
        }
        if terminal == codingagent.EventError && classified.retryable && attempt < maxAttempts {
            lastRetryableErr = classified.content
            _ = agentSess.Close()
            s.UnregisterActiveSession(sessionID)
            // execRegistry は同じ session で次ループのため維持するか、Unregister して即 Register。
            // busy を他クライアントに開けないよう Register を保ったまま agentSess だけ差し替える。
            select {
            case <-time.After(interval):
            case <-execCtx.Done():
            }
            continue
        }
        // 非 retryable、または尽き
        if terminal == codingagent.EventError && classified.retryable {
            ev := codingagent.StreamEvent{
                Type:    codingagent.EventError,
                Content: codingagent.ClassifiedErrorContent(classified.content, true),
            }
            _ = s.writeSSEWireEvents(...)
            s.updateSessionStatusOnTerminal(sessionID, ev, true, ev.Content)
        }
        s.finishActiveExecution(sessionID, agentSess, savedFiles)
        fmt.Fprintf(w, "data: [DONE]\n\n")
        return
    }
    ```

    `streamSSERelay` シグネチャ変更:
    ```go
    type streamTerminal struct {
        kind      codingagent.EventType // EventResult or EventError or 空
        retryable bool
        content   string
    }

    func (s *Server) streamSSERelay(...) (term streamTerminal, suspended bool)
    ```

    クライアント切断:
    ```go
    clientGone := false
    for {
        select {
        case <-ctx.Done():
            if !clientGone {
                clientGone = true
                s.logger.Warn("client disconnected during SSE stream", "session_id", sessionID, "events_sent", eventCount)
                // finalizeSessionStatusOnDisconnect は呼ばない（R4: 即 StatusError 禁止）
            }
            // ch の終端待ちへ。書き込みだけ止める。keepalive も止めてよい。
        case ev, ok := <-ch:
            if !ok { /* term が空なら結果なし。Codex 再実行判定は Process Wait 由来 EventError を待つ */ goto done }
            if ev.Type == codingagent.EventError && ev.Retryable {
                // SSE に書かない。term に保持。チャネル close 待ち。
                term = streamTerminal{kind: EventError, retryable: true, content: ev.Content}
                continue
            }
            if !clientGone {
                writeSSEWireEvents(...)
            }
            // EventSystem の AgentSessionID 保存は切断後も行う（resume id を落とさない）
            s.updateSessionStatusOnTerminal(...) // Retryable error ではまだ StatusError にしない
            if ev.Type == EventResult {
                term = streamTerminal{kind: EventResult}
            }
            if ev.Type == EventError && !ev.Retryable {
                term = streamTerminal{kind: EventError, retryable: false, content: ev.Content}
            }
        }
    }
    ```
    `updateSessionStatusOnTerminal`: `Retryable==true` の EventError では Status を Error にしない。最終分類を出すときだけ Error にする。

    JSON 応答経路（非 SSE）も同じ再実行ループにする。テストの主は SSE。

*   **Logic**:
    - 同一 `session_id` / 同一 `resumeID`（既存 `WithAgentSessionID`）/ 同一 `promptText`。
    - wrapPrompt / ingest は触らない。成功ターンだけ `finishActiveExecution` → 現行 `ingestActiveTurn`。retryable 失敗の再実行前 Close では ingest しない（失敗ターンを history に書かない）。再実行前に `ingestActiveTurn` を呼ぶと reconnect ログが正本に入るため、**中間 Close では ingest せず UnregisterActiveSession と process Stop のみ**。`finishActiveExecution` は最終回だけ。
    - 中間リトライで `finishActiveExecution` を使うと ingest してしまうので、`closeAttempt(sessionID, agentSess)` を分ける:
      ```go
      func (s *Server) closeAttempt(sessionID string, agentSess codingagent.Session) {
          if agentSess != nil { _ = agentSess.Close() }
          s.UnregisterActiveSession(sessionID)
          s.UnregisterExecCancel(sessionID) // 付け直すなら次ループで Register
      }
      ```
      `execRegistry` はターン中 busy 維持のため Unregister しない。最終 `finishActiveExecution` で解除。
    - 他エージェント native id は既存 wrap / resumeID ロジックのまま。本計画で触らない。

#### [MODIFY] [server/server.go](file://server/server.go)

*   **Description**: `resolveAgentService` に `WithProcessRetry(cfg.AgentService.ProcessRetry)` を追加する。
*   **Logic**: 本番 Server が YAML を使う。

### 統合 / E2E

#### [NEW] [tests/llm_stream_reconnect_test.go](file://tests/llm_stream_reconnect_test.go)

*   **Description**: 仕様の必須梯子。`t.Skip` 禁止。モック / スタブエージェント（実 Codex 不要）。名前は `TestStreamReconnectLive` にマッチしないこと。
*   **Technical Design**:
    `tests/llm_session_portability_test.go` と同様に `agentservice.New` + httptest + スタブエージェント。
    ```go
    func TestStreamReconnect_InProcessReconnectSucceeds(t *testing.T)
    // シナリオ A: スタブが EventError を出さず、内部相当として Text+Result のみ。
    // （プロセス内 reconnect は Codex unit の fake CLI が主検証。ここでは SSE に Error が無いこと）

    func TestStreamReconnect_ProcessRetrySameResume(t *testing.T)
    // シナリオ B: Name=codex, 1 回目 Retryable EventError, 2 回目 System(session)+Result
    // 収集 SSE に終端 Error 無し。2 回目 CreateSession の AgentSessionID が 1 回目と同じ

    func TestStreamReconnect_ExhaustedReturnsClassifiedError(t *testing.T)
    // シナリオ C: 常に Retryable 失敗。EventError ちょうど 1。Content に [upstream_overloaded]
    // ストリーム終了後に同じ session_id へ POST /messages → 409 以外（200/201 系の開始）

    func TestStreamReconnect_ClientDisconnectDoesNotKillCLI(t *testing.T)
    // シナリオ D: Send が 400ms 後に Result。クライアントは 50ms で切断。
    // Close は Result 後。テスト終了時 exec なし。

    func TestStreamReconnect_NonRetryableNoRetry(t *testing.T)
    // シナリオ E: unauthorized 非 Retryable。CreateSession 1 回。直ちに EventError。
    ```
    スタブは `handler_session_test` の retryAgent と同等を tests パッケージに置く（重複可。tests から internal は呼べない）。
*   **Logic**: Issue 再現の「繰り返し hard fail / churn」をモックで固定する。

#### [NEW] [tests/llm_stream_reconnect_live_test.go](file://tests/llm_stream_reconnect_live_test.go)

*   **Description**: シナリオ F。任意回帰。名前は `TestStreamReconnectLive` で始める（必須ゲートの `TestStreamReconnect_` にマッチしない）。
*   **Technical Design**:
    ```go
    func TestStreamReconnectLiveResumeSend(t *testing.T)
    ```
    既存 `startE2EServer` / `requireCLI(t, "codex")` パターン。`codex` 欠落は Fail。Create → Send → 同じ session でもう一度 Send（resume）。失敗してもプロセスを無限に起こさず、最終エラーに `[upstream_overloaded]` または成功のいずれか。
*   **Logic**: 必須ゲートではない。実 high demand は再現不定なので「無限 churn しない / 終端が分類か成功」を見る。

### ドキュメント

#### [MODIFY] [docs/ReferenceManual-WebAPIs.md](file://docs/ReferenceManual-WebAPIs.md)

*   **Description**: SSE `error` の Content に `[upstream_overloaded]` / `[upstream_error]` が付くこと。一時障害はサーバが有界リトライすること。クライアントは最終 error または result まで待つこと。
*   **Logic**: 仕様 R1 / 呼び出し側の復旧判断。

YAML 例（README または ReferenceManual の config 節）:

```yaml
llm_gateway:
  retry:
    max_retries: 2
    initial_delay_seconds: 1
    max_delay_seconds: 8
agent_service:
  process_retry:
    max_attempts: 3
    interval_seconds: 3
```

`examples/` の新規追加は必須としない。

## Step-by-Step Implementation Guide

1.  [x] **TDD 分類器**: `retry_test.go` を追加し Failed を確認する。`IsRetryableUpstream` / `ClassifiedErrorContent` / `StreamEvent.Retryable` を実装する。
2.  [x] **TDD Codex protocol/process**: retryable JSON が nil、fake CLI 成功で EventError 無し、retryable exit で Retryable=true。
3.  [x] **TDD config defaults**: ApplyDefaults と ProcessRetry。
4.  [x] **TDD Gateway stream_retry**: 開始失敗・先頭チャンク・非 retryable・データ後無 retry。openai / anthropic handler に接続する。
5.  [x] **TDD AgentService**: Codex 再実行、1 回の分類エラー、非 retryable、切断後も終端まで Close しない。`streamSSERelay` を drain 仕様に変える。中間 ingest 禁止。
6.  [x] **server.go 配線**: `WithProcessRetry`。
7.  [x] **E2E モック**: `tests/llm_stream_reconnect_test.go` をシナリオ A→B→C→D→E の順で追加する。
8.  [x] **LIVE**: `tests/llm_stream_reconnect_live_test.go`。
9.  [x] **ドキュメント**: ReferenceManual と YAML 既定。
10. [x] **検証**: Verification Plan のコマンドを実行する。

進行中は `[/]`、完了は `[x]` で本ガイドの項目を更新する。

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**: `./scripts/process/build.sh`（Linux / Remote-SSH は `./scripts/process/build.sh --skip-etc`）
2.  **Integration Tests（モック、必須）**:
    `./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories common --specify "TestStreamReconnect_"`
    Linux: `./scripts/process/build.sh --skip-etc && xvfb-run -a ./scripts/process/integration_test.sh --categories common --specify "TestStreamReconnect_"`
    末尾 `_` により `TestStreamReconnectLive*` を必須ゲートから外す。
3.  **E2E Tests（実 CLI、任意）**:
    `./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories llm --specify "TestStreamReconnectLive"`
    Linux: `./scripts/process/build.sh --skip-etc && xvfb-run -a ./scripts/process/integration_test.sh --categories llm --specify "TestStreamReconnectLive"`
4.  **リグレッション（Codex / セッション）**:
    `./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories llm --specify "TestSessionPortability_|TestCodexE2E"`

必須ゲートは 1 と 2。3 は実 Codex 環境の追加確認。`t.Skip` 禁止。

Issue [#41](https://github.com/axsh/arctic-tern/issues/41) 原文の再現は LIVE（手順 1〜4）に対応し、モック梯子で決定論化する。

## Documentation

- [docs/ReferenceManual-WebAPIs.md](file://docs/ReferenceManual-WebAPIs.md): SSE error の安定コードとサーバ側有界リトライ。
- 設定 YAML 既定（`llm_gateway.retry` / `agent_service.process_retry`）。
- セッション可搬性の docs は変更しない。
