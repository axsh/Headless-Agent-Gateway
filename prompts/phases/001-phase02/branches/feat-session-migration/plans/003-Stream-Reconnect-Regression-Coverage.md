# 003-Stream-Reconnect-Regression-Coverage

> **Source Specification**: [ideas/002-Stream-Reconnect-Regression-Coverage.md](file://prompts/phases/001-phase02/branches/feat-session-migration/ideas/002-Stream-Reconnect-Regression-Coverage.md)
>
> **関連 Issue**: [axsh/arctic-tern#41](https://github.com/axsh/arctic-tern/issues/41)
>
> **方針**: 仕様レビュー時の判断「1」— 002 はテスト厚みのまま進める。分類されない `exit status 1` / kanban-gui real-tern 残件は本計画に含めない。

## Goal Description

001 で入れたストリーム復旧の不変条件を、見逃していた層（同一プロセス JSONL、AgentService 実 Codex アダプタ、Gateway **ハンドラ**配線、3 回以上の resume+send、fake プロセス寿命、LIVE の overload 偽陽性）で自動的に壊せるようにする。復旧アルゴリズムの再設計はしない。本番変更は fake CLI 共有ヘルパー、Gateway の試験用 open フック、矛盾する既存テストと LIVE 判定の修正に限る。

## User Review Required

1. **Issue #41 再報告の残件は対象外。** `exit status 1` を retryable にする、drain 上限、kanban-gui の `TestSummarizerRealTern_*` は別仕様。本計画は回帰ネットのみ。
2. **`--categories` は付けない。** `integration_test.sh` は未知オプションで失敗する。位置づけは `llm`。実行は `--specify` のみ（仕様 002 URR4）。
3. **任意 R9 / R10 / R11 は実装しない。** 歴史的 E2E の `t.Skip` 一掃と `--categories` 実装はしない。
4. **LIVE の CLI 欠如。** 仕様 R7 は Fail。`TestStreamReconnectLive*` は `t.Skip` せず、PATH / vault 欠落は `t.Fatal`。フル `integration_test.sh` は Codex 無しでこのテストが落ちる。必須ゲートコマンドには LIVE を含めない。反対なら CLI 欠如だけ従来の Skip に戻す（その場合は仕様 R7/R8 からの逸脱として明記が必要）。

反対がなければこの 4 点で進める。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: 同一プロセス stdout retryable `error`/`turn.failed` のあと `turn.completed`、EventError 0 / EventResult 1 / 起動 1 回。既存 stderr 成功・retryable exit・unauthorized は残す | Proposed Changes > testfake、codex/process_repro_test.go `TestStartProcess_InProcessRetryableThenResult` |
| R2: fake `codex` を AgentService HTTP/SSE まで通す。SSE に `"type":"error"` 無し `"type":"result"` あり。プロセス再実行 0。`t.Skip` 禁止 | Proposed Changes > tests/llm_stream_reconnect_regression_test.go `TestStreamReconnectRegression_FakeCLIInProcessJSONL` |
| R3: OpenAI / Anthropic ハンドラで open 1 回目 retryable 失敗→2 回目成功、leading error chunk 再 open、非 retryable は 1 回、ゼロ値 `RetrySettings` でも既定リトライ | Proposed Changes > openai/handler_stream_retry_test.go、anthropic/handler_stream_retry_test.go、open フック |
| R4: ハンドラ試験が本番 `openResponsesStream` 経由。`handler.go` が `NewRetryBudget` / `RetryLeadingChunk` / `openResponsesStream` を参照することをソース検査 | Proposed Changes > 上記ハンドラ試験 + `TestHandlerSource_StreamRetryWiring` |
| R5: 同一 `session_id` で Send 3 回。2 回目だけ retryable exit 1、同一 resume id で再実行、3 回目成功、busy が残らない | Proposed Changes > `TestStreamReconnectRegression_ThreeResumeSends` |
| R6: SSE cancel 後も fake が `turn.completed` するまで Stop/taskkill 相当が走らない。終端後 Close。`TestStreamSSERelay_DisconnectUpdatesStatus` を drain 後 completed に合わせて更新 | Proposed Changes > `TestStreamReconnectRegression_DisconnectDoesNotKillFake`、handler_test.go |
| R7: LIVE 成功は期待テキスト + `EventResult`。`[upstream_overloaded]` 単独 PASS を別テスト名へ。必須 `--specify` に混ぜない。前提欠落は Fatal | Proposed Changes > tests/llm_stream_reconnect_live_test.go |
| R8: 本計画の新規・改修テストは `t.Skip`/`t.Skipf` 禁止（LIVE Fatal 含む） | 各新規テスト Logic |
| R9 デッドコンフィグ汎用化 | 対象外 |
| R10 歴史的 E2E Skip 削減 | 対象外 |
| R11 `--categories` | 対象外 |
| シナリオ A | R1 単体 + R2 統合 |
| シナリオ B | R3 単体 |
| シナリオ C | R5 統合 |
| シナリオ D | R6 統合 |
| シナリオ E | 既存 `TestStreamReconnect_NonRetryableNoRetry` と `TestStartProcess_NonRetryableExitNoRetryableFlag` を残す。新規必須ゲートには混ぜない |
| シナリオ F | R7 任意 LIVE。必須コマンドに含めない |

## Proposed Changes

依存順: 共有 fake → プロセス単体テスト → Gateway フックとハンドラ試験 → AgentService 切断テスト更新 → 統合 `tests/` → LIVE。各コンポーネントは `_test.go` を先に書く（Failed First）。

### codingagent/codex（共有 fake CLI）

#### [NEW] [shared/libs/go/codingagent/codex/testfake/install.go](file://shared/libs/go/codingagent/codex/testfake/install.go)

*   **Description**: 単体と `tests/` が同じ fake `codex` をビルドするためのヘルパー。本番アダプタからは import しない。
*   **Technical Design**:
    ```go
    package testfake

    type Options struct {
        Lines          []string
        Stderr         string
        ExitCode       int           // default 0。FailLaunches に当たった起動では無視し 1
        LineDelay      time.Duration // 空でない stdout 行のあいだ（最初の行の前は待たない）
        LaunchLogPath  string        // `codex exec` のたびに1行 append
        PIDFile        string        // exec 時に pid を書く
        FailLaunches   []int         // 1-origin。この番号の exec は Lines を出さず Stderr+exit 1
        FailStderr     string        // FailLaunches 用。空なら "Reconnecting... 1/5 (We're currently experiencing high demand, which may cause temporary errors.)"
    }

    func Install(t *testing.T, dir string, opts Options)
    func LaunchCount(t *testing.T, launchLogPath string) int
    func PID(t *testing.T, pidFile string) int
    ```
*   **Logic**:
    - `Install` は `dir/codex`（Windows は `dir/codex.exe`）をテスト内でビルドし、`t.Setenv("PATH", dir+sep+既存PATH)` する。`t.Skip` は使わない。ビルド失敗は `t.Fatal`。
    - fake の `main` は `--version` / `-V` なら `fake-codex 0.0.0` を出して 0。`exec` を含まない起動は 0（LookPath / バージョン確認用）。
    - `exec` のとき: `LaunchLogPath` があれば lock してカウント行を append。`PIDFile` に `os.Getpid()`。
    - 現在の launch 番号が `FailLaunches` に含まれる: stderr に `FailStderr`（既定は仕様の reconnect 文言 `Reconnecting... 1/5 (We're currently experiencing high demand, which may cause temporary errors.)`）、exit 1。stdout JSONL は出さない。
    - それ以外: `FAKE_CODEX_STDERR` / `opts.Stderr` を1行、`Lines` を順に print。行と行の間だけ `LineDelay`。最後に `opts.ExitCode`。
    - 既存 `process_repro_test.go` のインライン fake はこのパッケージへ置換する（挙動互換: Lines / Stderr / Exit 環境変数相当）。

#### [MODIFY] [shared/libs/go/codingagent/codex/process_repro_test.go](file://shared/libs/go/codingagent/codex/process_repro_test.go)

*   **Description**: Failed First で R1 を追加し、既存 3 本（stderr 成功 / retryable exit / unauthorized）は `testfake` 利用に差し替えて残す。
*   **Technical Design**:
    ```go
    const retryableReconnect = "Reconnecting... 1/5 (We're currently experiencing high demand, which may cause temporary errors.)"

    func TestStartProcess_InProcessRetryableThenResult(t *testing.T)
    // サブテスト errorJSONL と turnFailed
    // Lines:
    //   {"type":"thread.started","thread_id":"thr-1"}
    //   {"type":"error","message":"<retryableReconnect>"}  または
    //   {"type":"turn.failed","error":{"message":"We're currently experiencing high demand"}}
    //   {"type":"turn.completed"}
    // LineDelay: 50ms 以上（プロセス生存中に error が先に届くこと）
    // ExitCode: 0
    // LaunchLogPath を渡す

    // 収集: EventError 0、EventResult 1、LaunchCount==1
    ```
*   **Logic**:
    - `ParseExecEvent` は retryable な `error` / `turn.failed` で `nil` を返す（既存）。本テストは **StartProcess のチャネル**で終端 EventError が無いことを固定する。stderr+exit0 の `TestStartProcess_ReconnectStderrDoesNotEmitEventErrorOnSuccess` は残し、本テストの代替にしない。
    - 既存 `TestStartProcess_RetryableExitSetsRetryableFlag` / `NonRetryableExitNoRetryableFlag` / `ReconnectStderrDoesNotEmitEventErrorOnSuccess` は `testfake.Install` に移行するだけ。

### llmgateway（ハンドラ配線・R3/R4）

`config.RetrySettings`:

```go
type RetrySettings struct {
    MaxRetries          int `yaml:"max_retries"`
    InitialDelaySeconds int `yaml:"initial_delay_seconds"`
    MaxDelaySeconds     int `yaml:"max_delay_seconds"`
}
```

ゼロ値の正規化（現行 `normalizeRetrySettings` / `LLMGatewayConfig.ApplyDefaults`）:

- `MaxRetries == 0` → `2`（初回のあと最大 2 回。`OpenWithBudget` は `b.fails >= MaxRetries` で打ち切り）
- 3 フィールドとも 0 のとき `InitialDelaySeconds=1`、`MaxDelaySeconds=8`

`RetryLeadingChunk` は `IsRetryableUpstream(msg)` かつ `!dataWritten` かつ `fails < MaxRetries` のとき true。

試験用に Bifrost SDK を差し替える。`HandlerContext` は変えない（実装面を増やさない）。openai / anthropic それぞれに本番デフォルトの関数変数を置く。

#### [MODIFY] [shared/libs/go/llmgateway/openai/handler.go](file://shared/libs/go/llmgateway/openai/handler.go)

*   **Description**: `openResponsesStream` 内の `hctx.BifrostSDK().ResponsesStreamRequest` を変数経由にする。SSE 転送形式は変えない。
*   **Technical Design**:
    ```go
    type responsesStreamFn func(
        hctx handlerctx.HandlerContext,
        bCtx *bifrostSchemas.BifrostContext,
        req *bifrostSchemas.BifrostResponsesRequest,
    ) (chan *bifrostSchemas.BifrostStreamChunk, *bifrostSchemas.BifrostError)

    var openBifrostResponsesStream responsesStreamFn = func(hctx handlerctx.HandlerContext, bCtx *bifrostSchemas.BifrostContext, req *bifrostSchemas.BifrostResponsesRequest) (chan *bifrostSchemas.BifrostStreamChunk, *bifrostSchemas.BifrostError) {
        return hctx.BifrostSDK().ResponsesStreamRequest(bCtx, req)
    }
    ```
    `openResponsesStream` の `open` クロージャは `openBifrostResponsesStream(hctx, bCtx, req)` を呼ぶ。`berr != nil` なら現行どおり `llmgateway.StreamErr(bifrostErrorMessage(berr, "upstream stream request failed"))`。
*   **Logic**: 本番は SDK 直呼びと同一。テストだけ変数を差し替える。`handleResponsesStream` は引き続き `budget := llmgateway.NewRetryBudget(ctx.Config().LLMGateway.Retry)`、`openResponsesStream`、`budget.RetryLeadingChunk(reqCtx, log, msg, chunkCount > 0)`、再 open、データ後は `event: error` を書いて return。

#### [MODIFY] [shared/libs/go/llmgateway/anthropic/handler.go](file://shared/libs/go/llmgateway/anthropic/handler.go)

*   **Description**: openai と同じ `openBifrostResponsesStream` 変数。`handleMessagesBifrostStream` の leading chunk 分岐は現行維持。
*   **Technical Design**: openai と同シグネチャのパッケージ変数。anthropic は既に `message_start` を open 成功後・チャンクループ前に出す。R3 は leading `event: error` を出さないこと。`message_start` は許可。
*   **Logic**: 透過プロキシのチャンク変換は変えない。

テスト用共通スタブ（各 `_test.go` に複製してよい。小さく保つ）:

```go
type streamTestCtx struct {
    cfg *config.AppConfig
    log logger.Logger
}

func (c *streamTestCtx) Config() *config.AppConfig { return c.cfg }
func (c *streamTestCtx) Logger() logger.Logger     { return c.log }
func (c *streamTestCtx) Vault() vault.VaultStore   { return nil }
func (c *streamTestCtx) Router() handlerctx.ModelRouter { return nil }
func (c *streamTestCtx) BifrostSDK() *bifrost.Bifrost { return nil }
func (c *streamTestCtx) ToBifrostProvider(string) bifrostSchemas.ModelProvider { return "" }
func (c *streamTestCtx) SanitizeTools(*bifrostSchemas.BifrostResponsesRequest, bifrostSchemas.ModelProvider) {}
func (c *streamTestCtx) TryFallbackAnthropicResponse([]byte) ([]byte, bool) { return nil, false }
func (c *streamTestCtx) ExtractSessionID(string) string { return "" }
func (c *streamTestCtx) ExtractFallbackFlag(string) bool { return false }
func (c *streamTestCtx) MaskSecret(string) string { return "" }
```

Flush 可能な recorder:

```go
type flushRecorder struct{ httptest.ResponseRecorder }
func (flushRecorder) Flush() {}
```

retryable メッセージは分類器と揃える: `We're currently experiencing high demand`。非 retryable: `invalid api key`。

成功チャンク: `BifrostError == nil` かつ `BifrostResponsesStreamResponse != nil`。Anthropic 側は変換が失敗しても `event: error` にならなければよい。openai は Type 文字列をそのまま SSE にする。

#### [NEW] [shared/libs/go/llmgateway/openai/handler_stream_retry_test.go](file://shared/libs/go/llmgateway/openai/handler_stream_retry_test.go)

*   **Description**: Failed First。本番 `handleResponsesStream` を呼ぶ。
*   **Technical Design**:
    ```go
    func TestHandleResponsesStream_RetryOpenThenSuccess(t *testing.T)
    // RetrySettings{MaxRetries:2, InitialDelaySeconds:0, MaxDelaySeconds:8}
    // 1 回目 openBifrostResponsesStream は BifrostError Message=high demand（ch nil）
    // 2 回目は成功チャンクを送って close
    // 呼び出し回数 == 2
    // レスポンスに "event: error" が無く、成功イベントがある
    // HTTP 200（ヘッダは最初の成功 open のあと）

    func TestHandleResponsesStream_RetryLeadingErrorChunk(t *testing.T)
    // 1 回目の channel: 先頭チャンクのみ BifrostError high demand、close
    // RetryLeadingChunk が true → 2 回目 open が成功チャンク
    // body に leading の event: error が無い

    func TestHandleResponsesStream_NonRetryableNoRetry(t *testing.T)
    // open が invalid api key を 1 回 → 呼び出し回数 1、エラー応答（502 / event: error いずれか現行の開始失敗経路）

    func TestHandleResponsesStream_ZeroRetryConfigStillRetries(t *testing.T)
    // AppConfig.LLMGateway.Retry ゼロ値。ApplyDefaults も NewRetryBudget も MaxRetries=2
    // 1 回目 high demand、2 回目成功。open 回数 2
    // InitialDelaySeconds 既定 1 のため最大数秒待つ。タイムアウト 10s

    func TestHandlerSource_StreamRetryWiring(t *testing.T)
    // os.ReadFile("handler.go")
    // bytes.Contains NewRetryBudget, RetryLeadingChunk, openResponsesStream, openBifrostResponsesStream
    ```
*   **Logic**: テスト専用に `OpenStreamWithRetry` を再実装して緑にしない。必ず `handleResponsesStream(ctx, recorder, context.Background(), nil, &BifrostResponsesRequest{Model: "gpt-4o"})`。`t.Cleanup` で `openBifrostResponsesStream` を戻す。`t.Skip` 禁止。

#### [NEW] [shared/libs/go/llmgateway/anthropic/handler_stream_retry_test.go](file://shared/libs/go/llmgateway/anthropic/handler_stream_retry_test.go)

*   **Description**: `handleMessagesBifrostStream` に対して openai と同型 4 ケース + `TestHandlerSource_StreamRetryWiring`。
*   **Technical Design**: 関数名 `TestHandleMessagesBifrostStream_RetryOpenThenSuccess` 等。`event: error` が最終成功パスに無いこと。`message_start` はあってよい。
*   **Logic**: openai と同じ分類文字列・ゼロ値 `RetrySettings`・ソース検査。

### agentservice（切断テストの一本化・R6）

#### [MODIFY] [shared/libs/go/agentservice/handler_test.go](file://shared/libs/go/agentservice/handler_test.go)

*   **Description**: `TestStreamSSERelay_DisconnectUpdatesStatus` が「切断即 completed」になっていないか確認し、001 R4（切断はログ、終端まで drain）に合わせる。
*   **Technical Design**: 現行は cancel 後 200ms で `status == completed` を要求。`mockSlowLargeToolAgent` が cancel 後も `EventResult` を出せば、drain 後の completed は正しい。次を明示する。
    ```go
    // cancel 直後（sleep 無し）の GET は completed を要求しない。
    // drain 完了後（結果イベントまたはタイムアウト 2s）に completed。
    // 切断だけで StatusError / "client disconnected before completion" になっていたら失敗。
    ```
*   **Logic**: 矛盾した緑（殺して completed にした）を残さない。モックが `Close` で即終了するなら、切断後も result を出すようモック側を直す。R6 の fake プロセス生存は統合テスト側。

### tests/（必須ゲート統合・R2/R5/R6）

既存モックの `TestStreamReconnect_*` は残す。R1–R3 の代替に数えない。

ヘルパ（同ファイル）:

```go
func newFakeCodexHTTP(t *testing.T, retry config.ProcessRetryConfig) (*httptest.Server, *codex.CodexAdapter)
// agentservice.New(WithProcessRetry(retry), WithSandboxDisabled(true))
// RegisterAgent(codex.New(&AdapterConfig{Logger: ...}))
// SetGatewayModels は既存 reconnect テストと同様
```

`ProcessRetryConfig{MaxAttempts: 3, IntervalSeconds: 0}`（カスタムなので間隔 0。`WithProcessRetry` は `processRetryCustom` を立てる）。

SSE 投稿は既存 `postReconnectSSE` 相当を回帰ファイルに持つ。`sseErrorCount` は `"type":"error"` の data 行。

期待 reconnect 文言:

`Reconnecting... 1/5 (We're currently experiencing high demand, which may cause temporary errors.)`

#### [NEW] [tests/llm_stream_reconnect_regression_test.go](file://tests/llm_stream_reconnect_regression_test.go)

*   **Description**: Failed First。プレフィックス `TestStreamReconnectRegression`（`TestStreamReconnectLive` を `--specify` 正規表現で巻き込まない）。
*   **Technical Design**:
    ```go
    func TestStreamReconnectRegression_FakeCLIInProcessJSONL(t *testing.T)
    // シナリオ A HTTP（R2）
    // testfake: thread.started + retryable error JSONL + LineDelay 80ms + turn.completed, Exit 0
    // LaunchLogPath で LaunchCount==1
    // POST SSE: HTTP 200、sseErrorCount==0、body に "type":"result"
    // t.Skip 禁止。fake が PATH 先頭

    func TestStreamReconnectRegression_ThreeResumeSends(t *testing.T)
    // シナリオ C（R5）
    // 同一 session_id で Send 3 回
    // FailLaunches: {2} のみ（2 回目の最初の exec が retryable exit 1）
    // 成功 Lines は毎回 thread.started thread_id="thr-regress" + turn.completed
    // 1 回目 result、2 回目はプロセス再実行後 result（sseErrorCount==0、creates/exec は 2 回目ターンで +2）
    // 3 回目 result
    // 終了後 GET session が 409 busy 相当でない（messages 再 POST が 409 なら失敗）
    // resume: 2 回目以降の exec 引数に native id が含まれることは、thread.started の id が record に入ることと LaunchLog で足りなければ args を launch log に書く

    func TestStreamReconnectRegression_DisconnectDoesNotKillFake(t *testing.T)
    // シナリオ D（R6）
    // LineDelay 400ms、error 行のあと completed
    // SSE 開始 80ms で cancel
    // cancel 直後 PIDFile の pid がまだ生きている（Windows も os.FindProcess + signal 0 相当、または heartbeat ファイルの mtime）
    // 完了待ち後（1s 超）Close され、プロセスは終了してよい
    // completed 前に pid が消えたら失敗（taskkill / Stop が早すぎる）
    ```
*   **Logic**: モック `codingagent.Agent` は使わない。`CodexAdapter` が PATH の fake を exec する。`TestStreamReconnect_InProcessReconnectSucceeds` はモック初期成功なので代替にしない。`TestStreamReconnect_ClientDisconnectDoesNotKillCLI` はモック Close 回数なので残しつつ、本テストを R6 の正とする。

heartbeat が必要なら fake に `FAKE_CODEX_HEARTBEAT` を追加し、sleep 中 50ms ごとにファイルを touch。pid 消滅より確実。計画では **heartbeat ファイルを必須**にする。

```go
// testfake.Options.HeartbeatPath
// exec 中、50ms ごとにファイルを書き換え
```

cancel 後 150ms で heartbeat の mtime が更新されていたら生存。completed 後は更新が止まる。

### LIVE（R7・任意）

#### [MODIFY] [tests/llm_stream_reconnect_live_test.go](file://tests/llm_stream_reconnect_live_test.go)

*   **Description**: overload 単独 PASS を必須成功判定から外す。
*   **Technical Design**:
    ```go
    func liveReconnectTurnMustSucceed(t *testing.T, baseURL, sessionID, prompt, wantSubstr string)
    // EventResult 必須
    // EventError があれば Fatal（分類タグがあってもこの関数では成功にしない）
    // wantSubstr が空でなければ EventText または結合 Content に含む

    func TestStreamReconnectLiveResumeSend(t *testing.T)
    // mustCLI: exec.LookPath("codex") 失敗は t.Fatalf
    // ターン1: wantSubstr "reconnect-live-ok"
    // ターン2: EventResult（ack テキストは空でなく結果があること）

    func TestStreamReconnectLiveClaudeResumeSend(t *testing.T)
    // 同上。LookPath("claude") Fatal。必須 --specify に混ぜない

    func TestStreamReconnectLiveOverloadClassified(t *testing.T)
    // 任意。EventResult または Content に "[upstream_overloaded]"
    // 名前に Overload。TestStreamReconnectLiveResumeSend$ にマッチしない
    ```
*   **Logic**: 現行 `liveReconnectTurn` の

    ```go
    if sawResult { return }
    if strings.Contains(lastErr, "["+codingagent.ErrorCodeUpstreamOverloaded+"]") { return }
    ```

    を成功パスから削除する。`ErrorCodeUpstreamOverloaded` は `upstream_overloaded`。分類許容は `TestStreamReconnectLiveOverloadClassified` のみ。`requireCLI` の Skip は LIVE ファイルでは使わない。`startCodexE2EServer` が内部で Skip するなら、LIVE から呼ばない新しい起動ヘルパを同ファイルか回帰ファイルに置き、Skip しない。`startCodexE2EServer` の Skip は本計画の対象外（R10）だが、LIVE がそれを呼ぶと R7 違反なので **LIVE は Skip しない起動経路を使う**。

`startCodexE2EServer` の `t.Skipf("codex CLI not found")` を LIVE から避けるため、`tests/llm_stream_reconnect_live_test.go` で `mustStartCodexE2EServer` を作り、LookPath 失敗は Fatal。共通部分は既存関数から Skip 行だけ分岐してよい。既存 `codex_e2e_test.go` の Skip は R10 対象外のため残す。

### 残さないもの

- モック `TestStreamReconnect_*` の削除
- `stream_retry_test.go` ヘルパー単体の削除
- 分類語彙の拡大（`exit status 1` 単体）
- Claude プロセス再実行

## Step-by-Step Implementation Guide

1.  **Failed First / fake 共有**: 空の `testfake.Install` でもよいのでパッケージを作り、`TestStartProcess_InProcessRetryableThenResult` を先に書く。まだ行間 delay が無い fake だと error と completed が同時になり得る。失敗を確認してから `Install` の delay / launch log を実装する。
2.  **process_repro を testfake へ**: 既存 3 本を移行し、`./scripts/process/build.sh`（Linux は `--skip-etc`）で単体が通ることを確認する。
3.  **Gateway フック Failed First**: `openai/handler_stream_retry_test.go` を先に書き、フック未導入で失敗させる。`openBifrostResponsesStream` を入れて `handleResponsesStream` を通す。Anthropic も同様。ソース検査テストを追加する。
4.  **切断テスト更新**: `TestStreamSSERelay_DisconnectUpdatesStatus` を drain 後 completed に直す。
5.  **統合 Failed First**: `tests/llm_stream_reconnect_regression_test.go` の 3 テストを書く。heartbeat 付き fake で R6 を通す。
6.  **LIVE 判定**: `liveReconnectTurnMustSucceed` に切り替え、`TestStreamReconnectLiveOverloadClassified` を分離。LIVE 起動は Fatal 経路。
7.  **Verification Plan のコマンドを実行**: 必須ゲートが緑。LIVE は必須コマンドに入れない。

各ステップ完了ごとに、意味のある単位でコミットする（実装フェーズの Git ルール）。本計画作成時点ではコミットしない。

## Verification Plan

### Automated Verification

Windows:

1.  **Build & Unit Tests**: `./scripts/process/build.sh`
2.  **Integration Tests（必須ゲート、カテゴリ位置づけ llm）**: `./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestStreamReconnectRegression"`
3.  **E2E Tests**: 必須分は `tests/llm_stream_reconnect_regression_test.go`（上記 `--specify`）。任意 LIVE:

    `./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestStreamReconnectLiveResumeSend$"`

Linux / Remote-SSH（Linux）:

1.  `./scripts/process/build.sh --skip-etc`
2.  `./scripts/process/build.sh --skip-etc && xvfb-run -a ./scripts/process/integration_test.sh --specify "TestStreamReconnectRegression"`
3.  任意 LIVE: `./scripts/process/build.sh --skip-etc && xvfb-run -a ./scripts/process/integration_test.sh --specify "TestStreamReconnectLiveResumeSend$"`

`--specify` 正規表現が `TestStreamReconnectLive` を飲み込まないよう、必須は `TestStreamReconnectRegression` のみ。Claude LIVE と Overload は別指定。

単体で必ず走る名前:

- `TestStartProcess_InProcessRetryableThenResult`
- `TestHandleResponsesStream_RetryOpenThenSuccess`
- `TestHandleResponsesStream_ZeroRetryConfigStillRetries`
- `TestHandleMessagesBifrostStream_RetryOpenThenSuccess`
- `TestHandlerSource_StreamRetryWiring`（openai と anthropic）
- `TestStreamSSERelay_DisconnectUpdatesStatus`（更新後）

`cd`、素の `go test`、`go build`、`integration_test.sh` 単独（build.sh なし）は検証コマンドに使わない。fake バイナリのビルドはテストヘルパー内部のみ。

## Documentation

API 面は変わらない。[file://docs/ReferenceManual-WebAPIs.md](file://docs/ReferenceManual-WebAPIs.md) は更新しない。README に reconnect 節は無いので必須更新なし。テストの期待（overload 単独は LIVE 成功にしない）はテストコードと本計画で足りる。

## 対象外（再掲）

- 001 の分類・リトライ回数・Claude プロセス再実行の必須化
- Issue #41 コメントの `exit status 1` / `context deadline exceeded` / kanban-gui ゲート
- 全 E2E の `t.Skip` 削除
- `integration_test.sh --categories`
