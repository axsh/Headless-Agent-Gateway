# 020-SessionE2E-And-FlagCleanup

## 背景 (Background)

### 問題1: E2Eテストの欠如

028-SessionPersistence-DirectoryConfig の実装において、セッション継続 (`--resume`) やセッション保存先 (`--session-dir`) などの機能を追加したが、これらの動作を検証するE2Eテストコードが残されていない。手動コマンド実行による確認のみで完了とされた。

既存の E2E テストインフラ (`tests/agentservice_e2e_test.go`) には、テスト用 HAG サーバーの起動、セッション作成、メッセージ送信、SSE パースなどのヘルパー関数が充実しているが、セッション継続のシナリオをカバーするテストが存在しない。

現在の E2E テストは以下の4つのみ:

- `TestE2E_StandaloneHealth`: ヘルスチェック
- `TestE2E_CodingAgentStreaming`: 新規セッションでのストリーミング + ファイル生成
- `TestE2E_CodingAgentError`: エラー伝播
- `TestE2E_CodingAgentDefaultModel`: デフォルトモデルでのセッション

### 問題2: `--session-id` フラグの不要性

`cawa-client` の `run` サブコマンドには `--session-id` フラグがあるが、これは HAG サーバー側の (内部) セッション ID を指定するものである。しかし、セッション継続に本当に必要なのは「同じ HAG セッションに対して2回目以降のメッセージを送る」ことであり、これはサーバー側で `AgentSessionID` を自動的に保持・再利用する仕組みで実現されている。

ユーザーにとって `--session-id` は以下の理由で紛らわしい:

- HAG のセッション ID と、Claude Code の SDK セッション ID (`agent_session_id`) の区別がわかりにくい
- セッション継続時に必要な情報は HAG セッション ID のみだが、名前が `--session-id` では何の ID かが不明瞭
- `--resume` という名前の方が「既存セッションを継続する」意図が明確

## 要件 (Requirements)

### R1: セッション継続の E2E テスト追加 (必須)

以下のシナリオをカバーする E2E テストを `tests/agentservice_e2e_test.go` に追加する:

1. **セッション継続の基本フロー**: 新規セッション作成 -> 1回目のメッセージ送信 -> 2回目のメッセージ送信（セッション継続） -> `agent_session_id` が保持されていることの確認
2. **SessionDir フォールバックの検証**: `session_dir` 未指定でセッション作成 -> GET でセッション情報を取得 -> `session_dir` が `work_dir` と同じ値になっていることの確認

### R2: `--session-id` から `--resume` へのリネーム (必須)

`cawa-client` の `--session-id` フラグを `--resume` にリネームする。

- 変更前: `cawa-client run --session-id <HAG_SESSION_ID> --prompt "..."`
- 変更後: `cawa-client run --resume <HAG_SESSION_ID> --prompt "..."`
- Usage ヘルプも更新する

## 実現方針 (Implementation Approach)

### R1: E2E テストの実装

既存のヘルパー関数を活用して、以下のテスト関数を追加する:

- `TestE2E_SessionContinuation`: `startE2EServer()` でサーバー起動 -> `createE2ESession()` -> `sendE2EMessage()` を2回呼び出し -> 2回目で `agent_session_id` が維持されていることを `getE2ESession()` で検証
- `TestE2E_SessionDirFallback`: `session_dir` を空でセッション作成 -> `getE2ESession()` で `session_dir == work_dir` を確認

セッション作成時に `session_dir` を指定するヘルパー (`createE2ESessionWithSessionDir`) の追加が必要。

### R2: フラグリネーム

`examples/cawa-client/main.go` の `cmdRun` 関数内:

- `existingSessionID := fs.String("session-id", ...)` を `resumeSessionID := fs.String("resume", ...)` に変更
- `printUsage()` を更新
- 条件分岐の変数名を `resumeSessionID` に統一

## 検証シナリオ (Verification Scenarios)

### シナリオ A: セッション継続 E2E テスト

1. テスト用 HAG サーバーを起動する
2. セッションを作成する (agent=claudecode, work_dir=tmpDir)
3. 1回目のメッセージを送信する ("Create a file named hello.txt containing 'Hello'")
4. SSE ストリームが正常に完了する ([DONE] を受信)
5. `getE2ESession()` で `agent_session_id` が空でないことを確認する
6. 2回目のメッセージを送信する ("What files are in the current directory?")
7. SSE ストリームが正常に完了する
8. `getE2ESession()` で `agent_session_id` が変わっていないことを確認する (同じ SDK セッション上で継続された証拠)

### シナリオ B: SessionDir フォールバック E2E テスト

1. テスト用 HAG サーバーを起動する
2. `session_dir` を指定せずにセッションを作成する (agent=claudecode, work_dir=tmpDir)
3. `getE2ESession()` でセッション情報を取得する
4. `session_dir` フィールドが `work_dir` と同じ値であることを確認する

### シナリオ C: --resume フラグのリネーム確認

1. `cawa-client` のコードで `--session-id` フラグが存在しないことを確認する
2. `--resume` フラグが存在し、同じ動作をすることを確認する
3. Usage 出力に `--resume` が表示されることを確認する

## テスト項目 (Testing for the Requirements)

### ビルド・全体検証

1. ビルド + 単体テスト:
   ```
   scripts/process/build.sh
   ```

2. E2E テストの実行 (新規追加分):
   ```
   scripts/process/integration_test.sh --categories "llm" --specify "TestE2E_Session"
   ```

3. 既存 E2E テストのリグレッション確認:
   ```
   scripts/process/integration_test.sh --categories "llm" --specify "TestE2E"
   ```
