# 001-Session-Follow-Example

> **Source Specification**: [ideas/001-Session-Follow-Example.md](file://prompts/phases/001-phase02/branches/feat-reconnect-session/ideas/001-Session-Follow-Example.md)
>
> **関連 Issue**: [axsh/arctic-tern#46](https://github.com/axsh/arctic-tern/issues/46)
>
> **前提**: Follow Web API と `client/v1` の `Follow` / `FollowFrom` / `LastEventID` は計画 000 で実装済み。本計画は **Client API example** と **ルート `package client` の削除**。サーバの Follow 実装は変更しない。

## Goal Description

`examples/session-follow/` を `minimal-client` と同系統の HTTP Client API 例として追加する。別プロセス Tern（既定 `http://localhost:3100`）へ `client/v1` だけでセッション操作し、`SendText` の SSE を論理イベント N 件で切ったあと `GetSession` → `Follow` / `FollowFrom` で同じターンに再購読する。httptest で実 LLM なしにそのコアを検証する。あわせて Deprecated のルートパッケージ `github.com/axsh/arctic-tern/client`（`client/*.go`）を削除し、公開クライアントは `client/v1` と `client/internal` のみとする。

## User Review Required

1. **任意 R6 / R7 は本計画に含めない。** busy 409 hint デモ（`-demo-busy`）と steal 用第 2 プロセスは実装しない。必須は Follow 成功経路とレガシーパッケージ削除。
2. **本リポジトリの `scripts/process/build.sh` は `--skip-etc` / `--backend-only` を持たない**（未知フラグは失敗する）。検証コマンドは `./scripts/process/build.sh` のみ。Linux 用 `--skip-etc` はフラグが追加されるまで書かない。
3. **`Stream.Events()` は完了前切断で `EventError`（`stream terminated unexpectedly without completion marker`）を出す。** 意図的 drop のあとこのエラーはデモ失敗にしない（後述 Logic）。
4. **`user_input_required`:** フラグ `-respond`（既定空）。空ならエラー終了し README に書く。非空ならその文字列で `Respond`。httptest 必須ケースではこの経路を使わない。

反対がなければこのまま実装する。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: `examples/session-follow/`、独立 `go.mod`、v1 のみ、`agentservice`/`server` を実行パスから import しない、build.sh の examples ループ | Proposed Changes > go.mod, main.go |
| R2: フラグ表、`WithNoTimeout`、手続き 1–11、ログ項目 | Proposed Changes > main.go `runFlags` / `runFollowDemo` |
| R3: example README（英語、Client API、手続き 1–8、Web API リンク） | Proposed Changes > examples/session-follow/README.md |
| R4: ルート README Client Examples | Proposed Changes > README.md |
| R5: httptest、Drop then FollowFrom / Follow without from / No Send on Follow、`t.Skip` 禁止、コア関数切り出し | Proposed Changes > follow_test.go（テスト先行） |
| R8: ルート `package client` 削除、`client/v1` と `client/internal` 残す、レガシー E2E 削除または v1 へ寄せ、docs のレガシー記述削除 | Proposed Changes > 削除ファイル、codex_legacy テスト、docs/client-sse-tool-input.md |
| R6 / R7 | 本計画では実装しない |
| 仕様 000 再接続手続き 1–8（example にコード化） | `runFollowDemo` の固定ステップ |
| Follow API 回帰 | Verification: `integration_test.sh --categories common --specify "TestSessionFollow"` |
| 対象外（サーバ変更、in-process server.New、v1 破壊的変更、ternctl 機能追加、完了後リプレイ、GUI） | 変更しない |

## Proposed Changes

`Proposed Changes` は TDD のため **`_test.go` を先**に書く。example のテストは失敗する（`runFollowDemo` 未実装）状態で追加し、その後 `main.go` を書く。R8 の削除は example が v1 のみになったあと、ルートモジュールがレガシー import 無しで通る順で行う。

HTTP 対応（仕様の表、example は `client/v1` 経由のみ。example の業務ロジックで `http.NewRequest` を組まない）:

| Client API | HTTP |
| :--- | :--- |
| `CreateSession` | `POST /api/v1/sessions` |
| `SendText` / `SendMessage` | `POST /api/v1/sessions/{id}/messages`（`Accept: text/event-stream`） |
| `GetSession` | `GET /api/v1/sessions/{id}` |
| `Follow` / `FollowFrom` | `GET /api/v1/sessions/{id}/events`（任意 `?from=`） |
| `Respond` | `POST /api/v1/sessions/{id}/respond` |
| `Terminate` | `POST /api/v1/sessions/{id}/terminate` |

既存 v1 シグネチャ（変更しない。example が呼ぶ）:

```go
func New(baseURL string, opts ...ClientOption) *Client
func WithNoTimeout() ClientOption

type SessionRequest struct {
    Agent      string `json:"agent"`
    Model      string `json:"model,omitempty"`
    WorkDir    string `json:"work_dir"`
    SessionDir string `json:"session_dir,omitempty"`
    ConfigDir  string `json:"config_dir,omitempty"`
}
func (c *Client) CreateSession(ctx context.Context, req SessionRequest) (*Session, error)
func (c *Client) GetSession(ctx context.Context, sessionID string) (*SessionInfo, error)

type SessionInfo struct {
    ID         string `json:"id"`
    Status     string `json:"status"`
    Error      string `json:"error,omitempty"`
    Followable bool   `json:"followable,omitempty"`
    TurnID     string `json:"turn_id,omitempty"`
    // その他既存フィールドは Create/Get の JSON にあれば埋まる
}

func (s *Session) SendText(ctx context.Context, message string) (*Stream, error)
func (s *Session) Follow(ctx context.Context) (*Stream, error)
func (s *Session) FollowFrom(ctx context.Context, lastEventID string) (*Stream, error)
func (s *Session) Respond(ctx context.Context, content string) (*Stream, error)
func (s *Session) Terminate(ctx context.Context) error

func (s *Stream) Events() <-chan Event
func (s *Stream) LastEventID() string

const EventResult EventType = "result"
const EventError EventType = "error"
const EventUserInputRequired EventType = "user_input_required"
```

`Follow` は `GET .../events`（`from` なし）。`FollowFrom` は `GET .../events?from=` + `url.QueryEscape`。`Terminate` は HTTP 200 を成功とする（スタブも 200。204 にしない）。

### Example tests

#### [NEW] [examples/session-follow/follow_test.go](file://examples/session-follow/follow_test.go)

*   **Description**: Failed First。httptest が CAWA v1 と同じパスを模倣し、`runFollowDemo` を検証する。`t.Skip` 禁止。実 LLM・`agentservice`・`server` パッケージをテストから import しない。
*   **Technical Design**:

スタブが扱う経路（仕様 R5）:

- `POST /api/v1/sessions` → `201`、`{"session_id":"sess-stub-001"}`
- `POST .../messages` SSE: turn context（`id:` なし）→ `id: 0` text → （接続はクライアントが切るまで結果を送らない。ハング）
- `GET .../sessions/{id}` → 初回以降 Follow 完了前は `followable: true`、`status: "active"`、`turn_id: "turn-stub"`。Follow の SSE が `[DONE]` を書き終わったあとでも `completed` にしてよい
- `GET .../events` および `GET .../events?from=` → SSE。`from` があるときは **その論理 id の次から**（`from=0` なら `id: 1` 以降）。`Accept` 欠落は 406 にしなくてよい
- `POST .../terminate` → `200`

SSE ワイヤ（`client/v1` の `TestStream_LastEventID_SkipsEventsWithoutID` と同じ形）:

```
data: {"type":"system","content":"turn context"}

id: 0
data: {"type":"text","content":"one"}

id: 1
data: {"type":"text","content":"two"}

id: 2
data: {"type":"result"}

data: [DONE]
```

`writeSSE(w, id, jsonPayload string)`: `id != ""` なら `id: {id}\n` を先に書く。毎回 `Flusher.Flush()`。

スタブ状態:

```go
type stubCounters struct {
    mu           sync.Mutex
    messagePOSTs int
    eventGETs    int
    eventFrom    []string // 各 GET /events の RawQuery（空文字 = from なし）
}

func newFollowStub(t *testing.T, c *stubCounters) *httptest.Server
```

`POST /messages`: context + `id: 0` を書いたあと `<-r.Context().Done()` でブロック。`id: 1` / result / `[DONE]` は **messages では送らない**（drop 後も messages が増えないことと FollowFrom を分離するため）。

`GET /events`: バッファを `[]struct{ id, payload string }` で `{"0", text one}`, `{"1", text two}`, `{"2", result}` とする。`from` が空なら 0 から全部。`from` が `"0"` なら id 1 と 2。数値パース失敗は 400 でなくてよい（example は v1 が付けたクエリだけ送る）。最後に `[DONE]`。

テストケース:

1. **`TestRunFollowDemo_DropThenFollowFrom`**
    - `DropAfter: 1`
    - `runFollowDemo` が error なし
    - `outcome.DropLastID == "0"`
    - `outcome.FollowMode == "FollowFrom"`
    - `outcome.SawResult == true`
    - スタブの `eventFrom` に `from=0` を含むクエリが 1 件以上
    - `GetSession` 相当のログ用に `outcome.Followable` / `Status` / `TurnID` がスタブ値と一致（初回 GET 時点）

2. **`TestRunFollowDemo_FollowWithoutFrom`**
    - `DropAfter: 0`（論理イベント 0 件で SendText ストリームを切る。`LastEventID` は空のまま）
    - `outcome.FollowMode == "Follow"`
    - `outcome.DropLastID == ""`
    - スタブの `eventFrom` に空クエリ（`from` キー無し）の GET がある。`from=` 付きがあってはならない

3. **`TestRunFollowDemo_NoSendOnFollow`**
    - `DropAfter: 1` でデモ完走
    - `messagePOSTs == 1`（Follow 経路で `POST .../messages` が増えない）

共通セットアップ:

```go
c := client.New(srv.URL, client.WithNoTimeout())
f := &runFlags{
    Server:    srv.URL, // runFollowDemo は Client を引数に取るため未使用でもよい
    Agent:     "claudecode",
    WorkDir:   t.TempDir(),
    Prompt:    "test prompt",
    DropAfter: 1,
}
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
out, err := runFollowDemo(ctx, f, c, t.Logf)
```

*   **Logic**: テストは `main()` を呼ばない。フラグパースは必須ケースで直接 `runFlags` を埋める。

### Example module

#### [NEW] [examples/session-follow/go.mod](file://examples/session-follow/go.mod)

*   **Description**: `scripts/process/build.sh` の `examples/*/` ループ（`go.mod` があるディレクトリだけ `go test` と `go build -o bin/{dirname}`）に載せる。
*   **Technical Design**: `examples/config-dir-switch/go.mod` と同型。

```
module github.com/axsh/arctic-tern/examples/session-follow

go 1.26.5

require github.com/axsh/arctic-tern v0.0.0

replace github.com/axsh/arctic-tern => ../../
```

実装時に `go 1.26.5` は他 example と揃える。`go.sum` はモジュール解決後に追加。

*   **Logic**: ルートモジュール replace。example 実行パスの import は `github.com/axsh/arctic-tern/client/v1` のみ（標準ライブラリと flag/log を除く）。

#### [NEW] [examples/session-follow/main.go](file://examples/session-follow/main.go)

*   **Description**: 英語パッケージコメントとフラグ。日本語コメント禁止。`runFollowDemo` に手続きを固定実装。
*   **Technical Design**:

```go
package main

// session-follow is a Client API example. It talks HTTP to a separate Tern
// process (default http://localhost:3100) using github.com/axsh/arctic-tern/client/v1.
// It does not embed server.New. After SendText it drops SSE and reattaches with
// Follow / FollowFrom (GET /api/v1/sessions/{id}/events), without a second message.

import client "github.com/axsh/arctic-tern/client/v1"

const defaultPrompt = "Write a long, multi-paragraph explanation of TCP congestion control, then list three takeaways."

const (
    followMaxAttempts = 10
    followRetryWait   = 200 * time.Millisecond
)

type runFlags struct {
    Server     string
    Agent      string
    Model      string
    WorkDir    string
    SessionDir string
    Prompt     string
    DropAfter  int
    Respond    string // empty: user_input_required is fatal
}

type demoOutcome struct {
    SessionID  string
    DropLastID string
    FollowMode string // "Follow", "FollowFrom", or "" if Follow skipped
    Status     string
    Followable bool
    TurnID     string
    SawResult  bool
}

func parseFlags(args []string) (*runFlags, error)
func runFollowDemo(ctx context.Context, f *runFlags, c *client.Client, logf func(string, ...any)) (demoOutcome, error)
func consumeUntilDrop(ctx context.Context, stream *client.Stream, dropAfter int) (lastID string, finishedBeforeDrop bool, err error)
func consumeThroughResult(ctx context.Context, sess *client.Session, stream *client.Stream, respond string, logf func(string, ...any)) (sawResult bool, err error)
func isFollowNoActiveTurn(err error) bool
```

フラグ既定（仕様 R2）:

| フラグ | 既定 | 意味 |
| :--- | :--- | :--- |
| `-server` | `http://localhost:3100` | Tern サーバ URL |
| `-agent` | `claudecode` | エージェント名 |
| `-model` | 空 | 任意モデル |
| `-work-dir` | `.` | 作業ディレクトリ |
| `-session-dir` | 空 | 空なら一時ディレクトリを作る |
| `-prompt` | `defaultPrompt` | 最初の `SendText` |
| `-drop-after` | `1` | `id:` 付き論理イベント何件で SSE を切るか。`0` なら件数待ちせず切る |
| `-respond` | 空 | Follow 中の `user_input_required` への固定回答。空ならエラー |

`main`:

```go
func main() {
    f, err := parseFlags(os.Args[1:])
    if err != nil {
        os.Exit(2)
    }
    c := client.New(f.Server, client.WithNoTimeout())
    if _, err := runFollowDemo(context.Background(), f, c, log.Printf); err != nil {
        log.Fatalf("%v", err)
    }
}
```

*   **Logic**（仕様の実行フローをこの順で固定。要約しない）:

`runFollowDemo`:

1. `sessionDir`: `f.SessionDir != ""` ならそれを使う。空なら `os.MkdirTemp`（または `t.TempDir` 相当の `os.MkdirTemp("", "session-follow-")`）。
2. `CreateSession`（`SessionRequest{Agent, Model, WorkDir, SessionDir}`）。`logf("session_id=%s", session.ID)`。`defer session.Terminate(ctx)`（ターン中に呼ばない。関数 return 時のみ。失敗は `logf` して握りつぶしてよい）。
3. SendText 用に **子 context** `sendCtx, cancelSend := context.WithCancel(ctx)`。`stream, err := session.SendText(sendCtx, f.Prompt)`。example は `http.NewRequest` を書かない。
4. `consumeUntilDrop(sendCtx, stream, f.DropAfter)`:
    - `dropAfter <= 0`: 直ちに `cancelSend()`。`Events()` を最後まで drain する（キャンセルで HTTP が切れ、内部 goroutine が終了する）。`LastEventID()` を返す。`finishedBeforeDrop=false`。
    - `dropAfter > 0`: `logical := 0`。`for ev := range stream.Events()`:
        - `ev.Type == EventResult`: ストリームが drop 前に終わった → `finishedBeforeDrop=true`、ループを抜ける（cancel して drain）
        - `ev.Type == EventError`: drop 前なら return error。ただし cancel 後の `stream terminated unexpectedly without completion marker` は無視
        - `ev.ID != ""`: `logical++`。`logical >= dropAfter` なら `cancelSend()` して break
    - break 後も `Events()` がブロックしないよう **必ず cancel 済み**にし、チャネルを drain
    - 戻り値 `lastID := stream.LastEventID()`（`ev.ID` 手元変数と一致させる）
5. `finishedBeforeDrop == true` → `fmt.Errorf("turn finished before drop")`（非ゼロ。仕様: ターンが短すぎた場合はログして非ゼロ）
6. `logf("drop last_event_id=%q", lastID)`
7. `info, err := c.GetSession(ctx, session.ID)`。`logf("GetSession status=%s followable=%v turn_id=%s", info.Status, info.Followable, info.TurnID)`。outcome にコピー。
8. `info.Status == "completed"` → Follow しない。`logf` して **成功 return**（drop 後の完了競合）。`FollowMode=""`、`SawResult` は drop 前に result を見ていなければ false のまま成功でよい
9. `info.Status == "error"` → Follow しない。非ゼロ `fmt.Errorf("session error, skip follow: %s", info.Error)`（drain timeout 後にサーバがターンを落とした場合を含む。Error 文字列の完全一致は要求しない）
10. `info.Followable || info.Status == "active" || info.Status == "suspended"` → Follow へ。どれでもない未知 status は非ゼロ
11. Follow ループ（最大 `followMaxAttempts`、無限禁止）:
    - `lastID != ""` → `session.FollowFrom(ctx, lastID)`、`FollowMode="FollowFrom"`、`logf("follow mode=FollowFrom from=%s", lastID)`
    - `lastID == ""` → `session.Follow(ctx)`、`FollowMode="Follow"`、`logf("follow mode=Follow")`
    - 成功したら `consumeThroughResult`
    - 失敗が `isFollowNoActiveTurn`（`err.Error()` に `HTTP 409` と `no active turn` を含む）→ `time.Sleep(followRetryWait)` のあと `GetSession` 再取得。`completed` なら成功 return。`Followable` または `active`/`suspended` なら Follow やり直し。それ以外は非ゼロ
    - その他の Follow エラーは即座に非ゼロ
    - 回数超過は非ゼロ `fmt.Errorf("follow retries exhausted")`
12. `consumeThroughResult`: `for ev := range stream.Events()`（Follow は通常 `[DONE]` まで読む。ここは意図的 drop しない）
    - `EventText` 等: ログして続行してよい
    - `EventUserInputRequired`: `respond==""` なら `fmt.Errorf("user input required; pass -respond")`。非空なら `session.Respond(ctx, respond)` で stream を差し替え、外側ループ再開（`RunWithHandlers` の `goto nextStream` と同じ。example が `http.NewRequest` で respond を組まない）
    - `EventResult`: `SawResult=true`
    - `EventError`: return error
13. result まで読めたら `logf("follow saw result=true")` して return
14. 主経路で 2 通目の `SendText` / `SendMessage` を呼ばない

`isFollowNoActiveTurn`: v1 の Follow 失敗は `fmt.Errorf("follow failed (HTTP %d): %s", ...)`。本文に `no active turn`。

切断: SendText は **子 context の Cancel** で HTTP を切る。`drop-after` 後に新しいメッセージを送らない。

### Example docs

#### [NEW] [examples/session-follow/README.md](file://examples/session-follow/README.md)

*   **Description**: 英語。`config-dir-switch/README.md` と同水準。日本語禁止。
*   **Logic** 含めること:
    - 冒頭: **Client API example**。別プロセスの Tern へ HTTP。`minimal-client` の延長。このプロセスは `server.New` しない
    - Prerequisites: 稼働中 Tern（例: `minimal-server`）、エージェント CLI / vault。example 自身はサーバを起動しない
    - 実行例: `go run ./examples/session-follow` と `./bin/session-follow --help`（検証の主手段は httptest + `build.sh`。README の run は利用手順）
    - What this demonstrates 表:
        - Follow は新しいユーザーメッセージを積まない（`GET .../events`）
        - `from` は論理 SSE `id`（turn context には `id:` が無い）
        - 再購読は猶予（サーバ既定 90 秒）内
        - `GET .../logs` はターン SSE の代替ではない
    - 再接続手続き（仕様 000 と同じ順序、省略しない）:
        1. `SendMessage` / `SendText` で SSE を読む。完全に組み立てた論理イベントごとに last id を保存する
        2. 切断後 `GetSession` する
        3. `completed` → Follow しない
        4. `error` かつ drain timeout → ターンはサーバが落としている。Follow しない。必要なら新しい `SendMessage`
        5. `followable` または status が `active` / `suspended` → `FollowFrom(lastID)`。last id が無ければ `Follow()`（先頭再生）
        6. Follow が 409 `no active turn` → 短時間後に `GetSession` を再取得（完了との競合）
        7. Follow 中の `user_input_required` → 既存の `Respond`（本 example は `-respond`）
        8. 猶予（既定 90 秒）内に付け直す
    - リンク: [docs/ReferenceManual-WebAPIs.md](../../docs/ReferenceManual-WebAPIs.md) の Follow、`GET /api/v1/sessions/:id/events`
    - `-respond` が空のとき `user_input_required` で終了すること
    - LIVE billing は任意・呼び出し側負担（config-dir-switch と同様の短い注意）

#### [MODIFY] [README.md](file://README.md)

*   **Description**: **Client Examples** 節（`### Client Examples`、現行は minimal / multimodal の一文のあと config-dir-switch 段落）に session-follow を英語で追加。日本語禁止。`### Vault API Examples` や in-process `server.New` の節に混ぜない。
*   **Logic**: config-dir-switch 段落の直後に 1 段落 + 必要なら短いコード。内容:
    - 飛行中ターンの SSE を落としたあと、新しい `SendText` ではなく `GetSession` の `followable` を見て `Follow` / `FollowFrom`（`GET /api/v1/sessions/:id/events`）で再購読する
    - リンク: [examples/session-follow/README.md](examples/session-follow/README.md)、[examples/session-follow/main.go](examples/session-follow/main.go)
    - 既存の `WithNoTimeout()` コードブロックはそのまま（長寿命 SSE）

```markdown
If an in-flight turn SSE drops, do not send another user message. Call `GetSession`, and when `followable` (or status `active` / `suspended`) reattach with `Follow` / `FollowFrom`. Walkthrough: [examples/session-follow/README.md](examples/session-follow/README.md) and [examples/session-follow/main.go](examples/session-follow/main.go). HTTP: [docs/ReferenceManual-WebAPIs.md](docs/ReferenceManual-WebAPIs.md) (`GET /api/v1/sessions/:id/events`).
```

### R8: remove root `package client`

ルート `package client` の残 Go import は `tests/codex_legacy_client_large_output_e2e_test.go` のみ。v1 側に `TestCodexE2E_ClientV1_*`（大きな tool_result と `completed`）がある。レガシーファイルは **削除**する（アサーションの移植はしない。v1 テストが truncation 長と `EventResult` を既に見ている）。

残すディレクトリ:

- `client/v1/`（Follow / FollowFrom 含む。変更しない）
- `client/internal/ssechunk/`（v1 が import）

削除するファイル（ルート `package client`）:

- [client/agents.go](file://client/agents.go)
- [client/client.go](file://client/client.go)
- [client/client_test.go](file://client/client_test.go)
- [client/health.go](file://client/health.go)
- [client/models.go](file://client/models.go)
- [client/session.go](file://client/session.go)
- [client/session_test.go](file://client/session_test.go)
- [client/stream.go](file://client/stream.go)
- [client/stream_test.go](file://client/stream_test.go)

削除後 `github.com/axsh/arctic-tern/client` というパッケージは存在しない。`go list ./client/v1` と `./client/internal/...` は成功する。ルート `go list ./...` に `./client`（v1 無し）が出ない。

#### [DELETE] [tests/codex_legacy_client_large_output_e2e_test.go](file://tests/codex_legacy_client_large_output_e2e_test.go)

*   **Description**: 唯一のルート `client` import。削除しないと R8 後に統合テストがコンパイルできない。
*   **Logic**: `TestCodexE2E_LegacyClient_MaxTruncatedToolOutputTerminalEvent` は廃止。カバレッジは `tests/codex_client_v1_large_output_e2e_test.go`。

#### [MODIFY] [docs/client-sse-tool-input.md](file://docs/client-sse-tool-input.md)

*   **Description**: Official Go client 節からレガシーパッケージの一文を削除する。
*   **Logic**: 現行

```
- Legacy package `github.com/axsh/arctic-tern/client` behaves the same.
```

を削除。残す:

```
- `client/v1.Event.ToolInput` (`map[string]any`) is populated from JSON `tool_input`.
```

Deprecated 案内を「v1 を使え」に書き換えてルートパッケージを残すことは禁止（仕様: パッケージ削除後の記述）。

`prompts/phases/000-foundation/...` の過去仕様・計画に残る `import "github.com/axsh/arctic-tern/client"` は歴史資料として **変更しない**。

ルート `README.md` の import 例は既に `client/v1`。追加のレガシー推奨が無ければ R4 の段落追加以外は触らない。

## Step-by-Step Implementation Guide

1. [x] **Failed First (R5)**: `examples/session-follow/go.mod` と `examples/session-follow/follow_test.go` を追加する。`main.go` が無い、または `runFollowDemo` が stub で `t.Fatalf` 相当に失敗する状態にする。
2. [x] **Verify fail**: `./scripts/process/build.sh` が example `session-follow` のテスト失敗で落ちることを確認する。
3. [x] **Implement demo**: `examples/session-follow/main.go` に `parseFlags` / `runFollowDemo` / `consumeUntilDrop` / `consumeThroughResult` / `isFollowNoActiveTurn` を仕様フロー 1–14 どおり実装する。`WithNoTimeout`。2 通目の `SendText` 禁止。
4. [x] **Verify example tests**: `./scripts/process/build.sh` が `examples/session-follow` の 3 テストと `bin/session-follow` ビルドまで成功する。
5. [x] **Docs**: `examples/session-follow/README.md` とルート `README.md` の Client Examples を英語で更新する。
6. [x] **R8 tests first**: `tests/codex_legacy_client_large_output_e2e_test.go` を削除する（このファイルがレガシー依存の失敗テスト）。続けてルート `client/*.go` を削除する。
7. [x] **R8 docs**: `docs/client-sse-tool-input.md` の Legacy 行を削除する。
8. [x] **R8 verify**: `./scripts/process/build.sh`。ルートモジュールと `client/v1` が通る。レガシー import が残っていればコンパイル失敗するので直す。
9. [x] **Follow 回帰**: `./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories common --specify "TestSessionFollow"`。`TestSessionFollowLive` は `--specify` に含めない。

## Verification Plan

### Automated Verification

本ワークスペースの `build.sh` に `--skip-etc` は無い。次を使う。

1. **Build & example / unit tests（R1, R5, R8）**:

```bash
./scripts/process/build.sh
```

これで `examples/session-follow` の httptest 3 本、`bin/session-follow`、ルート `client/v1`、共有ライブラリが走る。成功条件:

- `TestRunFollowDemo_DropThenFollowFrom`
- `TestRunFollowDemo_FollowWithoutFrom`
- `TestRunFollowDemo_NoSendOnFollow`
- ルート `package client` の `.go` が無く、`client/v1` の既存テストが通る
- `"github.com/axsh/arctic-tern/client"`（v1 無し）の Go import が 0（残っていればこのビルドまたは次の統合で失敗）

2. **Integration（公開 Follow API 回帰）**:

```bash
./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories common --specify "TestSessionFollow"
```

Linux / Remote-SSH（Linux）で `xvfb-run` が使えるときは:

```bash
./scripts/process/build.sh && xvfb-run -a ./scripts/process/integration_test.sh --categories common --specify "TestSessionFollow"
```

`TestSessionFollowLive` を巻き込まない。

3. **E2E / tests 配下**: 新 example の契約は example モジュールの httptest（R5）。公開 API の E2E 相当は既存 `tests/common_session_follow_test.go` の `TestSessionFollow_*`。レガシー `TestCodexE2E_LegacyClient_*` は削除し、大きな tool 出力は既存 `tests/codex_client_v1_large_output_e2e_test.go` に任せる。`tests/` に session-follow 用の実 LLM テストは追加しない。

### Out of scope for verification

実サーバへの `go run ./examples/session-follow` は README の手順であり、CI の必須ゲートにしない（仕様どおり）。機能の正は httptest と `TestSessionFollow`。

## Documentation

| ファイル | 内容 |
| :--- | :--- |
| `examples/session-follow/README.md` | 新規。R3 |
| `README.md` Client Examples | session-follow 段落。R4 |
| `docs/client-sse-tool-input.md` | レガシー client 行を削除。R8 |
| `docs/ReferenceManual-WebAPIs.md` | Follow は 000 で済み。本計画では変更しない |
| `prompts/` 過去フェーズ | 変更しない |
