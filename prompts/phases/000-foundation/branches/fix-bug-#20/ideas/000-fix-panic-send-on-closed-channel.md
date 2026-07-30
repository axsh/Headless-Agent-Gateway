# 000: panic: send on closed channel の修正

## 背景 (Background)

[Issue #20](https://github.com/axsh/arctic-tern/issues/20) にて、`codex.ProcessManager.emitTimeout()` 内で `panic: send on closed channel` が発生し、ホストプロセス (agent-runner) がクラッシュする問題が報告された。

本パニックはジョブ正常完了の**約60分後**（`defaultMaxExecutionSec = 3600` の設定値と一致）に発生し、以下の影響をもたらす:

- `agent-runner` プロセスがクラッシュする
- systemd による再起動後、クラッシュ直前に進行中だったジョブが `failed` として記録される
- クラッシュ後に投入されたジョブが永続的に "running" 状態のまま stuck する

問題は `codex` パッケージだけでなく、同一構造を持つ `claudecode` パッケージにも存在する。

## 要件 (Requirements)

### 必須要件

- **R1**: プロセスが正常終了した後、タイムアウト goroutine がクローズ済みチャネルへ送信することを防止する
- **R2**: `codex/process.go` の `StartProcess` を修正する
- **R3**: `claudecode/process.go` の `StartProcess` を修正する
- **R4**: 既存の動作（タイムアウト検出・通知）は維持する
- **R5**: 修正は既存の単体テスト・統合テストをすべてパスすること

### 任意要件

- **O1**: `emitTimeout` に多層防御として `recover()` によるパニック抑制を追加する

## 実現方針 (Implementation Approach)

### 根本原因

`StartProcess` 内の **stdout goroutine** は `defer close(ch)` によりプロセス終了時にイベントチャネルをクローズするが、`cancel()` (= `procCtx` のキャンセル関数) を呼ばない。

```
プロセス正常終了
  → stdout goroutine: cmd.Wait() → close(ch)  ← ch がクローズされる
  → procCtx は未キャンセルのまま
  → タイムアウト goroutine: 生き続ける
  → idleTimeout (デフォルト300秒) または maxTimeout (デフォルト3600秒) 発火
  → emitTimeout(ch, procCtx, msg)
  → ch は closed → panic: send on closed channel
```

`pm.Stop()` は `cancel()` を呼ぶが、Unix パスでは graceful shutdown のタイムアウト後にしか呼ばれない。プロセスが先に終了している場合は `cancel()` が実行されずに抜ける。

### 修正方針

#### 対策A: stdout goroutine に `defer cancel()` を追加 (根本修正)

stdout goroutine がプロセスの終了を検知したタイミングで `procCtx` をキャンセルし、タイムアウト goroutine を確実に停止させる。

```go
// 変更対象: codex/process.go および claudecode/process.go
go func() {
    defer close(ch)
    defer cancel() // ← 追加: プロセス終了時に procCtx をキャンセル
    // ... 既存のコード ...
}()
```

`cancel()` はべき等（複数回呼び出しても安全）なため、`pm.Stop()` 側との競合は問題にならない。

#### 対策B: `emitTimeout` に `recover()` による多層防御を追加

万が一チャネルがクローズされた状態で送信が試みられた場合でも、パニックを吸収してプロセスのクラッシュを防ぐ。

```go
// codex/process.go
func (pm *ProcessManager) emitTimeout(ch chan<- codingagent.StreamEvent, ctx context.Context, msg string) {
    defer func() { recover() }() // ← 追加
    select {
    case ch <- codingagent.StreamEvent{Type: codingagent.EventError, Content: msg}:
    case <-ctx.Done():
    }
}

// claudecode/process.go
func emitTimeout(ch chan<- codingagent.StreamEvent, ctx context.Context, msg string) {
    defer func() { recover() }() // ← 追加
    select {
    case ch <- codingagent.StreamEvent{Type: codingagent.EventError, Content: msg}:
    case <-ctx.Done():
    }
}
```

### 変更ファイル一覧

| ファイル | 変更内容 |
|---|---|
| `shared/libs/go/codingagent/codex/process.go` | stdout goroutine に `defer cancel()` 追加 (対策A) + `emitTimeout` に `recover()` 追加 (対策B) |
| `shared/libs/go/codingagent/claudecode/process.go` | 同上 |

## 検証シナリオ (Verification Scenarios)

### シナリオ 1: プロセス正常終了後にタイムアウト goroutine が停止すること

1. `StartProcess` を呼び出してセッションを開始する
2. プロセスが正常完了し、イベントチャネルが閉じられる
3. `procCtx` がキャンセルされていることを確認する（タイムアウト goroutine が終了すること）
4. タイムアウト時間 (300秒/3600秒) を待っても、パニックが発生しないことを確認する

### シナリオ 2: タイムアウトが依然として正常に機能すること

1. `StartProcess` を呼び出してセッションを開始する
2. タイムアウト条件（アイドル超過または最大実行時間超過）を意図的に発生させる
3. `EventError` タイプのタイムアウトイベントがチャネルに送信されることを確認する
4. プロセスが停止することを確認する

### シナリオ 3: `emitTimeout` の `recover()` がパニックを抑制すること

1. クローズ済みチャネルを用意する
2. `emitTimeout` を呼び出す
3. パニックが発生せずに関数が正常に返ることを確認する

## テスト項目 (Testing)

### 単体テスト

- `codex/process.go` の修正に対するユニットテスト:
  - プロセス終了後に `procCtx` がキャンセルされることを検証するテスト
  - タイムアウト機能が正常に動作することを検証するテスト
- `claudecode/process.go` の修正に対するユニットテスト:
  - 同上

### ビルド・テスト実行

```bash
# 全体ビルド + 単体テスト
./scripts/process/build.sh

# 統合テスト (common カテゴリ)
./scripts/process/integration_test.sh --categories common
```
