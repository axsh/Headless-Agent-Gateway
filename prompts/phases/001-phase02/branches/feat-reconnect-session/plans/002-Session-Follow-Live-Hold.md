# 002-Session-Follow-Live-Hold

> **Source Specification**: [ideas/002-Session-Follow-Live-Hold.md](file://prompts/phases/001-phase02/branches/feat-reconnect-session/ideas/002-Session-Follow-Live-Hold.md)
>
> **関連 Issue**: [axsh/arctic-tern#46](https://github.com/axsh/arctic-tern/issues/46)
>
> **前提**: `examples/session-follow` は計画 001 で追加済み。本計画は example と httptest と README のみ。Follow サーバと `client/v1.Stream.LastEventID` は変更しない。

## Goal Description

session-follow の成功条件を「プロセスが 0 で終わること」から「切断後に Follow し `EventResult` を見ること」に変える。drop 時点の論理 id を drain 後の `LastEventID()` から切り離す。ライブ既定プロンプトはツールで `sleep N`（既定 60 秒）してから一文返す形にし、切断後もターンが生きる窓を作る。

## User Review Required

1. **任意 R6 は本計画に含める**（`-hold-seconds 0` で旧説明文プロンプト）。任意 R7（`tests/` 実 Claude E2E）は含めない。
2. **本リポジトリの `build.sh` / `integration_test.sh` に `--categories` も `--skip-etc` も無い。** 検証は `./scripts/process/build.sh` と `./scripts/process/integration_test.sh --specify "TestSessionFollow_"`。
3. **409 `no active turn` のあと `GetSession` が `completed` で Follow 未成功の場合も失敗**とする（R2: Follow 成功と `SawResult` が必須。現行の `session completed during follow retry` 成功 return をやめる）。

反対がなければこのまま実装する。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: drop 時点の id 固定。drain 後の `LastEventID()` を使わない | Proposed Changes > main.go `consumeUntilDrop` |
| R2: Follow + `SawResult` 必須。drop 後 `completed` skip は失敗。drop 前 result は従来どおり失敗 | Proposed Changes > main.go `runFollowDemo`、follow_test.go completed スタブ |
| R3: `-hold-seconds` 既定 60、sleep プロンプト、`-respond` 既定 `yes` | Proposed Changes > main.go `runFlags` / `holdPrompt` |
| R4: Drop last id frozen、completed なら error。既存 3 テスト維持 | Proposed Changes > follow_test.go（テスト先行） |
| R5: README 成功定義、hold、ポート | Proposed Changes > examples/session-follow/README.md |
| R6: `-hold-seconds 0` で旧プロンプト | Proposed Changes > `holdPrompt` |
| R7 | 本計画では実装しない |
| ユーザー提案（試験になっていない / sleep 擬似ロングラン） | R2–R4 と既定プロンプト |
| Follow API 回帰 | Verification: `--specify "TestSessionFollow_"` |

## Proposed Changes

`Proposed Changes` は TDD のため **`_test.go` を先**に書く。

### Example tests

#### [MODIFY] [examples/session-follow/follow_test.go](file://examples/session-follow/follow_test.go)

*   **Description**: Failed First。R4 の 2 ケースを追加する。既存 `TestRunFollowDemo_DropThenFollowFrom` / `FollowWithoutFrom` / `NoSendOnFollow` は残し、引き続き通す。
*   **Technical Design**:

`newFollowStub` をオプション化する（既存 3 テストの呼び出しは互換）。

```go
type stubOpts struct {
    writeMessageID0 bool
    burstAfterID0   bool // after id 0, also write id 1 text + result on the same /messages body
    sessionStatus   string // empty => "active"
    sessionFollowable bool // used when sessionStatus is set; default true for active stub
}

func newFollowStub(t *testing.T, c *stubCounters, opts stubOpts) *httptest.Server
```

既存呼び出し:

```go
newFollowStub(t, counters, stubOpts{writeMessageID0: true})
newFollowStub(t, counters, stubOpts{writeMessageID0: false})
```

GET session JSON:

```go
status := opts.sessionStatus
if status == "" {
    status = "active"
}
followable := true
if opts.sessionStatus == "completed" {
    followable = opts.sessionFollowable // test sets false or true; R2 fails on completed regardless
}
```

`Active after drop required` は status `completed` なら error。`followable` が true でも completed を成功にしない。

`burstAfterID0`: `/messages` が turn context + `id: 0` text one のあと、続けて:

```
id: 1
data: {"type":"text","content":"two"}

id: 2
data: {"type":"result"}

```

を同じ接続に書き、その後 `<-r.Context().Done()`（または短時間 hang）。クライアント `-drop-after 1`。

新規テスト:

1. **`TestRunFollowDemo_DropLastIDFrozen`**
    - `stubOpts{writeMessageID0: true, burstAfterID0: true}`
    - `DropAfter: 1`
    - `err == nil`（GET session は active のまま。Follow は `/events`）
    - `out.DropLastID == "0"`（`"1"` や `"2"` 禁止）
    - `out.FollowMode == "FollowFrom"`
    - スタブ `eventFrom` に `from=0`（`from=1` だけの Follow は失敗）

2. **`TestRunFollowDemo_CompletedAfterDropIsError`**
    - `stubOpts{writeMessageID0: true, sessionStatus: "completed"}`
    - `DropAfter: 1`
    - `err != nil`
    - メッセージに `completed` を含む（`skip follow` 成功経路は禁止）
    - `out.FollowMode == ""`
    - `out.SawResult == false`

`testFlags` は `Respond` をテストで空のままでよい（スタブは `user_input_required` を出さない）。

*   **Logic**: 実 LLM 禁止。`t.Skip` 禁止。

### Example logic

#### [MODIFY] [examples/session-follow/main.go](file://examples/session-follow/main.go)

*   **Description**: R1–R3、R6。英語コメントのみ。
*   **Technical Design**:

```go
const defaultEssayPrompt = "Write a long, multi-paragraph explanation of TCP congestion control, then list three takeaways."

const holdPromptTemplate = "Run a shell command that sleeps for %d seconds (python -c with time.sleep(%d) or POSIX sleep %d). Do not answer before the sleep finishes. After it finishes, reply with exactly one short sentence. Do not ask questions. Do not write a long essay before the tool call."

type runFlags struct {
    Server      string
    Agent       string
    Model       string
    WorkDir     string
    SessionDir  string
    Prompt      string
    DropAfter   int
    Respond     string
    HoldSeconds int
}

func holdPrompt(n int) string {
    return fmt.Sprintf(holdPromptTemplate, n, n, n)
}

func applyPromptDefaults(f *runFlags) {
    if f.Prompt != "" {
        return
    }
    if f.HoldSeconds <= 0 {
        f.Prompt = defaultEssayPrompt
        return
    }
    f.Prompt = holdPrompt(f.HoldSeconds)
}
```

`parseFlags`:

| フラグ | 既定 |
| :--- | :--- |
| `-prompt` | 空（空なら `applyPromptDefaults`） |
| `-hold-seconds` | `60` |
| `-respond` | `yes` |
| `-drop-after` | `1`（変更なし） |

`main`: `parseFlags` のあと `applyPromptDefaults(f)`。テストは `runFlags.Prompt` を直接埋めるので defaults を通さない。

`consumeUntilDrop`:

```go
func consumeUntilDrop(cancel context.CancelFunc, stream *client.Stream, dropAfter int) (lastID string, finishedBeforeDrop bool, err error) {
    dropped := false
    recordedID := ""
    if dropAfter <= 0 {
        cancel()
        dropped = true
    }
    logical := 0
    for ev := range stream.Events() {
        switch ev.Type {
        case client.EventResult:
            if !dropped {
                finishedBeforeDrop = true
                cancel()
                dropped = true
            }
        case client.EventError:
            if dropped || isIntentionalDropError(ev.Error) {
                continue
            }
            return "", false, fmt.Errorf("%s", ev.Error)
        }
        if ev.ID != "" && !dropped {
            logical++
            if logical >= dropAfter {
                recordedID = ev.ID
                cancel()
                dropped = true
            }
        }
    }
    return recordedID, finishedBeforeDrop, nil
}
```

drain 後に `stream.LastEventID()` を **return しない**。`-drop-after 0` の `recordedID` は空のまま。

`runFollowDemo` の GetSession 以降（現行の completed 成功を削除）:

```go
if info.Status == "completed" {
    return out, fmt.Errorf("session completed after drop; follow not attempted")
}
if info.Status == "error" {
    return out, fmt.Errorf("session error, skip follow: %s", info.Error)
}
canFollow := info.Followable || info.Status == "active" || info.Status == "suspended"
if !canFollow {
    return out, fmt.Errorf("session not followable: status=%s followable=%v", info.Status, info.Followable)
}
```

Follow ループ内、409 後に `completed`:

```go
if info.Status == "completed" {
    return out, fmt.Errorf("session completed during follow retry without result")
}
```

Follow HTTP 成功後:

```go
saw, err := consumeThroughResult(...)
if err != nil {
    return out, err
}
if !saw {
    return out, fmt.Errorf("follow finished without result")
}
out.SawResult = true
logf("follow saw result=true")
return out, nil
```

*   **Logic**: 主経路で 2 通目の `SendText` は呼ばない。`client/v1` の `LastEventID()` は触らない。

### Docs

#### [MODIFY] [examples/session-follow/README.md](file://examples/session-follow/README.md)

*   **Description**: 英語。R5。
*   **Logic** 含めること（日本語禁止）:
    - 成功: ログに `follow mode=Follow` または `follow mode=FollowFrom` と `follow saw result=true`。`session completed after drop; skip follow` は失敗（その文言はコードから消える。新しい error 文を README に書く）
    - `-hold-seconds`（既定 60）: 最初のツール呼び出しのあとエージェントが N 秒ブロックする。クライアントが切ってもターンが `active` / `followable` で残るための窓。N が drop より前に尽きると Follow 試験にならない
    - `-respond` 既定 `yes`
    - 既定サーバ `http://localhost:3100`。占有時は `--server http://localhost:<port>`
    - Reattach procedure の API 規則「`completed` → do not Follow」は残す。**この example はその状態を成功にしない**（hold 窓が足りなかった）
    - LIVE billing 注意は残す

ルート [README.md](file://README.md) の Client Examples の session-follow 文は、失敗を成功と読める表現が無ければ変更しない。

## Step-by-Step Implementation Guide

1. [x] **Failed First (R4)**: `follow_test.go` に `stubOpts`、`TestRunFollowDemo_DropLastIDFrozen`、`TestRunFollowDemo_CompletedAfterDropIsError` を追加する。既存 3 テストの stub 呼び出しを `stubOpts` に直す。
2. [x] **Verify fail**: `./scripts/process/build.sh` が session-follow の新規テスト失敗で落ちることを確認する（現行 `consumeUntilDrop` は burst で `DropLastID` が `"0"` 以外になり得る。completed スタブは現行が成功 return するため error 期待が失敗する）。
3. [x] **Implement**: `main.go` の `consumeUntilDrop` recordedID、`runFollowDemo` R2、フラグ `-hold-seconds` / `-respond yes` / `holdPrompt` / `applyPromptDefaults`。
4. [x] **Verify example**: `./scripts/process/build.sh` で既存 3 + 新規 2 と `bin/session-follow` が通る。
5. [x] **Docs**: `examples/session-follow/README.md` を R5 どおり更新する。
6. [x] **Regression**: `./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestSessionFollow_"`。

## Verification Plan

### Automated Verification

1. **Build & example tests（R1, R2, R4, R3 のフラグコンパイル）**:

```bash
./scripts/process/build.sh
```

成功条件:

- `TestRunFollowDemo_DropThenFollowFrom`
- `TestRunFollowDemo_FollowWithoutFrom`
- `TestRunFollowDemo_NoSendOnFollow`
- `TestRunFollowDemo_DropLastIDFrozen`
- `TestRunFollowDemo_CompletedAfterDropIsError`

2. **Integration（公開 Follow API 回帰）**:

```bash
./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestSessionFollow_"
```

Linux / Remote-SSH（Linux）で `xvfb-run` があるとき:

```bash
./scripts/process/build.sh && xvfb-run -a ./scripts/process/integration_test.sh --specify "TestSessionFollow_"
```

3. **E2E / `tests/`**: 本変更の契約は example モジュールの httptest。`tests/` に実 LLM テストは追加しない。公開 API の回帰は既存 `tests/common_session_follow_test.go` の `TestSessionFollow_*`。

ライブ `session-follow` 対実 Claude は CI 必須ゲートにしない（仕様どおり）。機能の正は httptest と `TestSessionFollow_`。

## Documentation

| ファイル | 内容 |
| :--- | :--- |
| `examples/session-follow/README.md` | R5。成功定義、hold-seconds、respond 既定、ポート |
| ルート `README.md` | 原則変更なし |
| `docs/ReferenceManual-WebAPIs.md` | 変更しない |
