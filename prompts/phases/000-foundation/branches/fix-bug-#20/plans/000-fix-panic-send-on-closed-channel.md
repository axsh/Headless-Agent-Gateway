# 000-fix-panic-send-on-closed-channel

> **Source Specification**: prompts/phases/000-foundation/branches/fix-bug-#20/ideas/000-fix-panic-send-on-closed-channel.md

## Goal Description

`codex/process.go` および `claudecode/process.go` の `StartProcess` 内にある stdout goroutine が、プロセス正常終了時に `cancel()` を呼ばないため、タイムアウト goroutine が生き残り、クローズ済みチャネルへ送信して `panic: send on closed channel` を引き起こす問題を修正する。

対策A (`defer cancel()` 追加) と対策B (`emitTimeout` に `recover()` 追加) の両方を実施する。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: プロセス正常終了後にタイムアウト goroutine がクローズ済みチャネルへ送信しないこと | Proposed Changes > codex/process.go (対策A) および claudecode/process.go (対策A) |
| R2: `codex/process.go` の `StartProcess` を修正する | Proposed Changes > codex/process.go |
| R3: `claudecode/process.go` の `StartProcess` を修正する | Proposed Changes > claudecode/process.go |
| R4: 既存の動作（タイムアウト検出・通知）は維持する | 変更は goroutine 起動後のフックのみ。タイムアウトロジック自体には触れない |
| R5: 修正は既存の単体テスト・統合テストをすべてパスすること | Verification Plan |
| O1: `emitTimeout` に `recover()` 追加 | Proposed Changes > codex/process.go (対策B) および claudecode/process.go (対策B) |

## Proposed Changes

> **注意**: 依存関係順 (Test → Implementation) で記述する。テストファイルを先に記述すること。

---

### 1. codex パッケージ

#### [MODIFY] `shared/libs/go/codingagent/codex/process_test.go`

*   **Description**: 対策A・Bを検証する単体テストを追加する。既存のテストは `package codex_test`（外部テストパッケージ）であるため、`emitTimeout`（非公開メソッド）に直接アクセスできない。既存ファイルに **内部パッケージテスト** 用の新規テスト関数を `package codex_test` 内で追加できる範囲で記述し、公開APIを通じて検証する。
*   **Technical Design**: テーブル駆動テスト `tests := []struct{...}` 形式で追加するテスト関数:
    - `TestEmitTimeout_ClosedChannel_NoPanic` – `recover()` によるパニック抑制の検証
    - `TestProcessContextCanceledAfterExit` – stdout goroutine 終了後に `procCtx` がキャンセルされる検証（モックプロセス使用）
*   **Logic**:

    ```go
    // TestEmitTimeout_ClosedChannel_NoPanic
    // emitTimeout を内部パッケージからアクセスするため、同パッケージの internal_test.go として作成する
    // → 以下は package codex (internal) のテストファイルに記述する
    func TestEmitTimeout_ClosedChannel_NoPanic(t *testing.T) {
        pm := &ProcessManager{logger: logger.NewDefault(logger.LevelInfo)}
        ch := make(chan codingagent.StreamEvent, 1)
        close(ch)
        ctx := context.Background()
        // recover() が機能していれば panic しない
        require.NotPanics(t, func() {
            pm.emitTimeout(ch, ctx, "timeout test")
        })
    }

    // TestEmitTimeout_OpenChannel_SendsEvent
    func TestEmitTimeout_OpenChannel_SendsEvent(t *testing.T) {
        pm := &ProcessManager{logger: logger.NewDefault(logger.LevelInfo)}
        ch := make(chan codingagent.StreamEvent, 1)
        ctx := context.Background()
        pm.emitTimeout(ch, ctx, "idle timeout after 300s")
        ev := <-ch
        if ev.Type != codingagent.EventError {
            t.Errorf("expected EventError, got %v", ev.Type)
        }
        if ev.Content != "idle timeout after 300s" {
            t.Errorf("unexpected content: %q", ev.Content)
        }
    }

    // TestProcessContextCanceled_AfterStdoutClose
    // モックプロセスとして os/exec で "echo" (Unix) または "cmd /c echo" (Windows) を使い、
    // 即座に終了するプロセスを起動する。
    // StartProcess は実際の codex バイナリを前提とするため、内部構造を直接テストする代わりに
    // goroutine ライフサイクル自体のロジックをテストするためのヘルパー関数を導入する。
    ```

    > **設計方針**: `emitTimeout` は非公開メソッドのため、内部テスト (`package codex`、`process_internal_test.go`) として追加する。`ProcessManager` は exported 構造体であり、フィールドは unexported だが、テスト内でゼロ値を利用できる範囲でテストを組む。

#### [NEW] `shared/libs/go/codingagent/codex/process_internal_test.go`

*   **Description**: `emitTimeout` (非公開メソッド) を直接テストするための内部テストファイルを新規作成する。
*   **Technical Design**:
    - `package codex`（外部ではなく内部パッケージ）
    - `require.NotPanics` の代わりに `defer func() { if r := recover(); r != nil { t.Fatal(...) } }()` を使用（標準ライブラリのみ）
*   **Logic**:

    ```go
    package codex

    import (
        "context"
        "testing"

        "github.com/axsh/arctic-tern/shared/libs/go/codingagent"
        "github.com/axsh/arctic-tern/shared/libs/go/logger"
    )

    func TestEmitTimeout_ClosedChannel_NoPanic(t *testing.T) {
        pm := &ProcessManager{logger: logger.NewDefault(logger.LevelInfo)}
        ch := make(chan codingagent.StreamEvent, 1)
        close(ch)
        ctx := context.Background()

        defer func() {
            if r := recover(); r != nil {
                t.Fatalf("emitTimeout panicked on closed channel: %v", r)
            }
        }()
        pm.emitTimeout(ch, ctx, "timeout test")
    }

    func TestEmitTimeout_OpenChannel_SendsEvent(t *testing.T) {
        pm := &ProcessManager{logger: logger.NewDefault(logger.LevelInfo)}
        ch := make(chan codingagent.StreamEvent, 1)
        ctx := context.Background()

        pm.emitTimeout(ch, ctx, "idle timeout after 300s")

        select {
        case ev := <-ch:
            if ev.Type != codingagent.EventError {
                t.Errorf("expected EventError, got %v", ev.Type)
            }
            if ev.Content != "idle timeout after 300s" {
                t.Errorf("unexpected content: %q", ev.Content)
            }
        default:
            t.Fatal("expected event to be sent, but channel is empty")
        }
    }

    func TestEmitTimeout_CanceledContext_NoSend(t *testing.T) {
        pm := &ProcessManager{logger: logger.NewDefault(logger.LevelInfo)}
        ch := make(chan codingagent.StreamEvent, 1)
        ctx, cancel := context.WithCancel(context.Background())
        cancel() // 事前にキャンセル

        pm.emitTimeout(ch, ctx, "should not send")

        select {
        case ev := <-ch:
            t.Fatalf("unexpected event sent: %v", ev)
        default:
            // 期待動作: キャンセル済みコンテキストのためイベントが送信されない
        }
    }
    ```

#### [MODIFY] `shared/libs/go/codingagent/codex/process.go`

*   **Description**: 対策A・B を実装する。
*   **Technical Design**:
    - **対策A**: stdout goroutine (L329) の先頭 defer に `defer cancel()` を追加する
    - **対策B**: `emitTimeout` メソッド (L386) の先頭に `defer func() { recover() }()` を追加する
*   **Logic**:

    **対策A の変更箇所** (現在の L329–L381):
    ```go
    // 変更前
    go func() {
        defer close(ch)
        scanner := bufio.NewScanner(stdout)
        // ...

    // 変更後
    go func() {
        defer close(ch)
        defer cancel() // 対策A: プロセス終了時に procCtx をキャンセルし、タイムアウト goroutine を停止させる
        scanner := bufio.NewScanner(stdout)
        // ...
    ```

    **対策B の変更箇所** (現在の L386–L391):
    ```go
    // 変更前
    func (pm *ProcessManager) emitTimeout(ch chan<- codingagent.StreamEvent, ctx context.Context, msg string) {
        select {
        case ch <- codingagent.StreamEvent{Type: codingagent.EventError, Content: msg}:
        case <-ctx.Done():
        }
    }

    // 変更後
    func (pm *ProcessManager) emitTimeout(ch chan<- codingagent.StreamEvent, ctx context.Context, msg string) {
        defer func() { recover() }() // 対策B: closed channel への送信によるパニックを多層防御として抑制
        select {
        case ch <- codingagent.StreamEvent{Type: codingagent.EventError, Content: msg}:
        case <-ctx.Done():
        }
    }
    ```

---

### 2. claudecode パッケージ

#### [NEW] `shared/libs/go/codingagent/claudecode/process_internal_test.go`

*   **Description**: `emitTimeout` 関数 (非公開、package-level関数) を直接テストするための内部テストファイルを新規作成する。
*   **Technical Design**:
    - `package claudecode`（内部パッケージ）
    - `codex` パッケージのテストと同等のテストケースを実装する
    - `claudecode` パッケージの `emitTimeout` は **スタンドアロン関数** (`*ProcessManager` のメソッドではない) であることに注意
*   **Logic**:

    ```go
    package claudecode

    import (
        "context"
        "testing"

        "github.com/axsh/arctic-tern/shared/libs/go/codingagent"
    )

    func TestEmitTimeout_ClosedChannel_NoPanic(t *testing.T) {
        ch := make(chan codingagent.StreamEvent, 1)
        close(ch)
        ctx := context.Background()

        defer func() {
            if r := recover(); r != nil {
                t.Fatalf("emitTimeout panicked on closed channel: %v", r)
            }
        }()
        emitTimeout(ch, ctx, "timeout test")
    }

    func TestEmitTimeout_OpenChannel_SendsEvent(t *testing.T) {
        ch := make(chan codingagent.StreamEvent, 1)
        ctx := context.Background()

        emitTimeout(ch, ctx, "idle timeout after 300s")

        select {
        case ev := <-ch:
            if ev.Type != codingagent.EventError {
                t.Errorf("expected EventError, got %v", ev.Type)
            }
            if ev.Content != "idle timeout after 300s" {
                t.Errorf("unexpected content: %q", ev.Content)
            }
        default:
            t.Fatal("expected event to be sent, but channel is empty")
        }
    }

    func TestEmitTimeout_CanceledContext_NoSend(t *testing.T) {
        ch := make(chan codingagent.StreamEvent, 1)
        ctx, cancel := context.WithCancel(context.Background())
        cancel()

        emitTimeout(ch, ctx, "should not send")

        select {
        case ev := <-ch:
            t.Fatalf("unexpected event sent: %v", ev)
        default:
            // 期待動作
        }
    }
    ```

#### [MODIFY] `shared/libs/go/codingagent/claudecode/process.go`

*   **Description**: codex と同様に対策A・B を実装する。
*   **Technical Design**:
    - **対策A**: stdout goroutine (L292) の先頭 defer に `defer cancel()` を追加する
    - **対策B**: `emitTimeout` 関数 (L335) の先頭に `defer func() { recover() }()` を追加する
*   **Logic**:

    **対策A の変更箇所** (現在の L292–L330):
    ```go
    // 変更前
    go func() {
        defer close(ch)
        scanner := bufio.NewScanner(stdout)
        // ...

    // 変更後
    go func() {
        defer close(ch)
        defer cancel() // 対策A: プロセス終了時に procCtx をキャンセルし、タイムアウト goroutine を停止させる
        scanner := bufio.NewScanner(stdout)
        // ...
    ```

    **対策B の変更箇所** (現在の L335–L340):
    ```go
    // 変更前
    func emitTimeout(ch chan<- codingagent.StreamEvent, ctx context.Context, msg string) {
        select {
        case ch <- codingagent.StreamEvent{Type: codingagent.EventError, Content: msg}:
        case <-ctx.Done():
        }
    }

    // 変更後
    func emitTimeout(ch chan<- codingagent.StreamEvent, ctx context.Context, msg string) {
        defer func() { recover() }() // 対策B: closed channel への送信によるパニックを多層防御として抑制
        select {
        case ch <- codingagent.StreamEvent{Type: codingagent.EventError, Content: msg}:
        case <-ctx.Done():
        }
    }
    ```

---

## Step-by-Step Implementation Guide

1. **[TDD: テスト作成 - codex]**: `shared/libs/go/codingagent/codex/process_internal_test.go` を新規作成し、`TestEmitTimeout_ClosedChannel_NoPanic`・`TestEmitTimeout_OpenChannel_SendsEvent`・`TestEmitTimeout_CanceledContext_NoSend` を実装する。この時点では `recover()` が未追加のため `TestEmitTimeout_ClosedChannel_NoPanic` は失敗することを確認する (`./scripts/process/build.sh` 実行)。

2. **[TDD: テスト作成 - claudecode]**: `shared/libs/go/codingagent/claudecode/process_internal_test.go` を新規作成し、同等のテストを実装する。

3. **[実装: 対策B - codex]**: `shared/libs/go/codingagent/codex/process.go` の `emitTimeout` メソッドに `defer func() { recover() }()` を追加する。ビルドしてテストがパスすることを確認する。

4. **[実装: 対策B - claudecode]**: `shared/libs/go/codingagent/claudecode/process.go` の `emitTimeout` 関数に同様の変更を加える。

5. **[実装: 対策A - codex]**: `shared/libs/go/codingagent/codex/process.go` の stdout goroutine 先頭に `defer cancel()` を追加する (L329 付近)。

6. **[実装: 対策A - claudecode]**: `shared/libs/go/codingagent/claudecode/process.go` の stdout goroutine 先頭に `defer cancel()` を追加する (L292 付近)。

7. **[ビルド・単体テスト]**: `./scripts/process/build.sh` を実行して全単体テストがパスすることを確認する。

8. **[git commit]**: 変更をコミットする。

    ```
    fix: prevent send on closed channel in emitTimeout goroutine
    ```

9. **[統合テスト]**: `./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories common` を実行してリグレッションがないことを確認する。

10. **[git push]**: 全テストパス確認後にプッシュする。

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**:
    ```
    ./scripts/process/build.sh
    ```
    確認内容:
    - `TestEmitTimeout_ClosedChannel_NoPanic` が PASS すること (対策B の検証)
    - `TestEmitTimeout_OpenChannel_SendsEvent` が PASS すること (タイムアウト機能が維持されていることの確認)
    - `TestEmitTimeout_CanceledContext_NoSend` が PASS すること (procCtx キャンセル時の動作確認)

2. **Integration Tests**:
    ```
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories common
    ```
    確認内容: 既存の統合テストがリグレッションしていないこと

## Documentation

変更内容はバグ修正のみであり、APIやインターフェースの変更はない。既存ドキュメントの更新は不要。
