# 003: Codex プロセス異常終了 (Exit Status 1) とタイムアウトからのセッション完全復旧

> **関連 Issue**: [axsh/arctic-tern#41](https://github.com/axsh/arctic-tern/issues/41)
> **先行仕様**: [001-Codex-Stream-Reconnect-Recovery.md](file://prompts/phases/001-phase02/branches/feat-session-migration/ideas/001-Codex-Stream-Reconnect-Recovery.md), [002-Stream-Reconnect-Regression-Coverage.md](file://prompts/phases/001-phase02/branches/feat-session-migration/ideas/002-Stream-Reconnect-Regression-Coverage.md)

## 背景 (Background)

仕様 001 および 002 により、上流混雑時の `Reconnecting... 1/5 (high demand)` 文言を検知してリトライ・復旧する仕組みと回帰テスト層を導入した。
しかし、Tern `v0.1.14` を用いた実環境（kanban-gui / real tern 統合テスト）での再検証において、以下の残課題が報告されている（[Issue #41 追記コメント](https://github.com/axsh/arctic-tern/issues/41#issuecomment-5308229711)）:

1. `Reconnecting... 1/5` の文字列は出なくなったが、依然として `codex CLI process exited with error (exit status 1)` が発生し、呼び出し側に `arctic_tern stream error: exit status 1` が返る。
2. `stream read error: context deadline exceeded` が発生し、タイムアウトでテストが失敗する (`TestSummarizerRealTern_ResumeAfterKanbanRestart` 等)。
3. セッション再利用時 (`TestSummarizerRealTern_ResumeSameSession`) にセッションが再作成されてしまい、同一セッション ID での継続が阻害される。

### 現状の問題と構造的原因

1. **`exit status 1` が非 retryable として即座に終端されている**
   - 現在の `process.go` では、`cmd.Wait()` 失敗時に `IsRetryableUpstream(errMsg)`（`reconnecting...` や `high demand` 等）または `IsRetryableError(err)`（EOF, connection reset 等）のみを retryable と判定している。
   - Codex CLI が stderr に既知の文言を残さずに `exit status 1` で終了した場合、`Retryable: false` となり、AgentService はプロセス再実行を行わずに直ちに `EventError` をクライアントへ返してしまう。

2. **壊れた native thread による `exec resume` の無限失敗ループ**
   - Codex プロセスが異常終了した後に、次回 Send で `codex exec resume <thread_id>` を実行すると、破損したスレッド情報を読み込もうとして即座に `exit status 1` でクラッシュを繰り返す。
   - 現在の Tern には「`exec resume` が失敗した場合に thread_id を破棄して新規 `exec` でフォールバックする」自己修復機構（Self-Healing Resume）が存在しないため、呼び出し側は Tern セッションそのものを破棄・再作成せざるを得ない。

3. **タイムアウトと切断ドレインのアンバランス**
   - クライアント側のタイムアウト（30秒〜数分）に対して、Codex プロセスのデフォルト最大タイムアウト (3600秒) やアイドルタイムアウト (300秒) が長すぎる。
   - クライアントがタイムアウトで切断した後、Tern 側が応答のないプロセスを無制限にドレインし続けることで、後続の同一セッション要求が長時間 busy 競合やデッドロックに巻き込まれる。

---

## User Review Required

1. **`exit status 1` のリトライ対象化**:
   - 認証失敗 (`unauthorized`, `invalid_api_key`) や設定不備などの明確な非リトライエラーを除き、Codex プロセスの非ゼロ終了 (`exit status 1` 等) を有界回数（デフォルト3回）のプロセス再実行対象とする。
2. **破損 resume thread の自動フォールバック (Self-Healing Resume)**:
   - `codex exec resume <thread_id>` でプロセスが異常終了した場合、リトライ試行時に `AgentSessionID` をクリアして新規 `codex exec`（会話履歴は Tern 側の正本からプロンプト補完注入）で再実行する。
   - これにより、外部の呼び出し側が Tern セッション ID を維持したまま復旧できるようにする。
3. **ドレインおよびタイムアウトの健全化**:
   - クライアント切断後のドレイン処理に安全な上限時間（例: 最大 15〜30秒、またはセッションタイムアウト設定）を設け、応答のないプロセスを確実にクリーンアップして busy 状態を解除する。

---

## 要件 (Requirements)

### 必須要件 (Must)

#### R1: Codex プロセス非ゼロ終了 (`exit status 1`) の包括的リトライ

- Codex CLI が非ゼロ終了 (`exit status 1` 等) した場合、以下の明示的な非リトライ可能エラーに該当しない限り、`Retryable: true` として分類する:
  - `unauthorized`, `invalid api key`, `authentication failed`
  - `model not found`, `unknown model`
  - `invalid argument`, `flag provided but not defined`
- `stderr` が空、または `"exit status 1"` のみの場合でも、未特定のプロセス異常終了として有界リトライの対象とする。
- 有界リトライ回数（デフォルト3回）を超過した場合は、`[upstream_error]` または `[upstream_overloaded]` の安定コードタグを付与した `EventError` を1回のみ返し、セッション状態を安全に更新する。

#### R2: 破損 resume スレッドからの自動フォールバック (Self-Healing Resume)

- `codex exec resume <thread_id>` のプロセスが retryable エラー（`exit status 1` や上流エラー）で終了した場合:
  - 次の再試行 attempt では `AgentSessionID` を空（初回起動モード `codex exec`）にフォールバックしてプロセスを起動する。
  - セッションの会話コンテキストは、Tern の正本（Wayfinder 正本または履歴）からの補完注入機構によって維持する。
- 同一 Tern `session_id` は維持され、クライアント側から見てセッション ID の再作成が不要であること。

#### R3: クライアント切断ドレインのタイムアウト制限とクリーンアップ

- SSE クライアントが切断 (`<-ctx.Done()`) された後、プロセスのドレイン完了を待つ時間に安全な上限（例: 15秒）を設定する。
- 上限を超えてもプロセスが終端イベントを出さない場合は、`ProcessManager.Stop()` を呼んでプロセスを強制停止し、セッションの busy 状態（`execRegistry`）を確実に解除する。
- これにより、クライアントがタイムアウトで再接続した際にセッションが busy で拒絶される問題を防止する。

#### R4: リトライ時の安定した SSE イベント出力

- プロセス再試行中の中間エラーイベントは SSE ストリームへ出力せず、最終的に成功した試行のイベント、または全試行失敗時の単一分類エラーイベントのみをクライアントへ届ける。
- クライアントが不要な `stream error: exit status 1` で中断されないことを保証する。

### 任意要件 (Nice to Have)

#### R5: プロセスリトライ間隔の指数バックオフ調整

- `ProcessRetryConfig` において、固定秒数だけでなく最小限のバックオフを設定可能にする（デフォルトは即時〜短時間待機）。

---

## 実現方針 (Implementation Approach)

```mermaid
flowchart TD
    A[SendMessage リクエスト] --> B[codex exec resume / exec 起動]
    B --> C{プロセス終了状態}
    C -- 成功 exit 0 --> D[EventResult 出力 & ターン完了]
    C -- 明示的非リトライエラー --> E[EventError 出力 & 終端]
    C -- exit status 1 / upstreamエラー --> F{試行回数 < maxAttempts ?}
    F -- Yes --> G[thread_id クリア & fresh exec で再起動]
    G --> B
    F -- No --> H[分類エラー [upstream_error] 出力 & 終端]
```

### 1. リトライ判定ロジックの改善 (`codingagent/retry.go` & `codex/process.go`)
- `IsNonRetryableError(msg string) bool` を新設し、認証エラーや不正引数などの既知の致命的エラーを定義。
- `codex/process.go` において、`cmd.Wait()` 失敗時に `!IsNonRetryableError(errMsg)` であれば `Retryable: true` とする。

### 2. セッション再試行時の Self-Healing (`agentservice/handler_retry.go`)
- `runTurn` のループにおいて、前回の attempt が resume 実行で失敗した場合、次回 attempt の `sessionOptsWithResume` で `fallbackResume` を空文字列にして新規スレッド起動を促す。
- 成功時に新しく発行された `thread.started` の ID で `record.AgentSessionID` を上書き更新する。

### 3. ドレイン処理のバウンディング (`agentservice/handler_retry.go`)
- `streamSSERelay` 内の `clientGone` ループに、切断後の最大待機タイマー（`drainDeadline`）を設ける。
- タイムアウト時は `agentSess.Close()` を実行して安全に `finishActiveExecution` する。

---

## 検証シナリオ (Verification Scenarios)

### シナリオ A: 生の `exit status 1` からの同一プロセス/再実行復旧 (必須)
1. fake CLI が stderr なし（または汎用エラー）で `exit status 1` を返す。
2. AgentService がこれを retryable とみなし、2回目の試行で正常に `EventResult` を受信してターンが成功する。
3. クライアントに `exit status 1` のエラーイベントは届かない。

### シナリオ B: 壊れた resume thread からの Self-Healing 復旧 (必須)
1. 1回目のターンが正常完了し、`AgentSessionID` に `thr-broken` が記録される。
2. 2回目のターンで `codex exec resume thr-broken` が `exit status 1` で即座に失敗する。
3. Tern が同一 `session_id` のまま `thr-broken` を破棄し、新規 `codex exec` で再試行して成功する。
4. セッション ID は変わらず、後続の 3 回目のターンも正常に実行できる。

### シナリオ C: クライアントタイムアウト切断後の自動クリーンアップ (必須)
1. プロセスが応答を返さない状態でクライアントが切断する。
2. ドレインタイムアウト（15秒）経過後にプロセスが停止され、セッション busy が解除される。
3. 直後に同じセッションへ新しいメッセージを送信した際、409 Conflict にならず正常に受け付けられる。

---

## テスト項目 (Testing)

### 単体テスト (Unit Tests)

Windows:
```bash
./scripts/process/build.sh
```

Linux / Remote-SSH（Linux）:
```bash
./scripts/process/build.sh --skip-etc
```

- `TestIsNonRetryableError`: 認証失敗・モデル不存在の判定確認
- `TestStartProcess_GenericExit1IsRetryable`: stderr なしの exit status 1 が `Retryable: true` になること
- `TestHandleSendMessage_BrokenResumeThreadSelfHeals`: resume 失敗時の新規 thread へのフォールバック確認

### 統合テスト (Integration Tests)

Windows:
```bash
./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestStreamReconnectRegression"
```

Linux / Remote-SSH（Linux）:
```bash
./scripts/process/build.sh --skip-etc && xvfb-run -a ./scripts/process/integration_test.sh --specify "TestStreamReconnectRegression"
```

---

## 対象外

- Claude Code / Wayfinder への固有プロセス再実行ロジック追加（Codex にフォーカス）
- 上流プロバイダのクォータ増枠やネットワークインフラ自体の改善
