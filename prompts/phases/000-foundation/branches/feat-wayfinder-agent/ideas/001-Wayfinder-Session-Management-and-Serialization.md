# Wayfinder Agent - セッション管理および永続化仕様書

## 1. 背景 (Background)

Wayfinder Agentは、サーバープロセスを常駐させず、ユーザーがCLIから指示を投げるたびに起動・終了する「シングルショット（単発）実行」を基本とします。
そのため、過去の対話履歴（Context）、生成されたファイル、および実行中のバックグラウンドプロセスをプロセスの終了を越えて追跡できるように、状態をファイルへ保存（シリアライズ）および復元（デシリアライズ）する仕組みが不可欠です。

## 2. 要件 (Requirements)

### 必須要件 (Mandatory Requirements)
- **ファイルベースの状態保存**: セッションID（UUID等）をキーとして、状態全体を指定された `SessionDir` 配下のファイル（`[SessionDir]/[SessionID].json`）にJSON形式で保存できること。
- **レジューム機能**: CLI起動時またはAPI経由で `--session-id` (あるいは `session_id`) が渡された場合、対応するセッションファイルを `SessionDir` から読み込んで前回の会話履歴、トラッカー情報を復元し、対話を再開できること。存在しない場合は新規セッションを生成すること。
- **親子セッションでの設定の伝播**: 親セッションから子セッション（サブエージェント）を起動する際、子セッションは親の `WorkDir` および `SessionDir` をそのまま引き継いで動作し、子セッションファイルも同じ `SessionDir` 配下に `[ChildSessionID].json` として保存されること。
- **シリアライズ対象データの網羅**: 以下の情報が完全にシリアライズ・デシリアライズされること。
  - セッションID (`SessionID`)
  - 親セッションID (`ParentID` - 子セッションの場合のみ)
  - セッションステータス (`Status` - "active", "completed" など)
  - 会話履歴 (`Messages`): 役割 (`Role` - "user", "assistant", "tool")、内容 (`Content`)、タイムスタンプ
  - **削除許可リスト / 生成ファイルメタデータ (`FileCreationTracker`)**: エージェントが実行中に生成したファイル・ディレクトリの絶対パスと作成日時。このデータはガードレールの削除許可判定に使用されるため、セッション間で確実に引き継がれる必要がある。
  - **バックグラウンドプロセス情報 (`CommandExecutionContext`)**: バックグラウンド起動したプロセスのPID、コマンド名、引数、起動日時。このデータもガードレールの`kill`操作許可判定に使用される。
- **セッション復旧時のトラッカー整合性検証**: セッションファイルをデシリアライズしてトラッカー情報を復元する際、実際のシステム状態との整合性を検証すること（詳細は003ガードレール仕様書を参照）。具体的には:
  - `FileCreationTracker` の各エントリについてファイル/ディレクトリの存在を確認し、存在しない場合はリストから除外する。
  - `CommandExecutionContext` の各PIDについてプロセスの存在とコマンド名の一致を確認し、不一致の場合はリストから除外する。
- **アトミックな書き込み**: ファイル保存時の書き込み失敗による既存データの破損を防ぐため、一時ファイルに書き込んでから `os.Rename` で置き換えるアトミック書き込みを行うこと。
- **自動クリーンアップ**: 最終更新日時から指定期間（デフォルト24時間）が経過した古いセッションファイルを削除するクリーンアップAPIを提供すること。
- **コンテキストのコンパクション (圧縮・最適化)**: 会話履歴（Messages）が一定トークン数またはターン数を超えて肥大化した場合に、必要な文脈を喪失することなくトークン量を削減する最適化機能（コンパクション）を備えること。具体的には以下の制御を満たすこと。
  - **ピン留め＆スライディングウィンドウ**: システム指示や最初の指示（ユーザー要求）などの重要メッセージを「ピン留め（最前部に固定）」したまま、古い対話履歴からウィンドウ外へ破棄する制御ができること。
  - **要約コンパクション**: 破棄対象となる古い会話ログ（例: 最初の10メッセージ等）を自動的に要約メッセージに置換してコンテキストの文脈喪失を防ぐこと。
  - **出力・ファイルトリミング**: 過去の長いコマンド実行結果や読み込まれたファイル内容を、最大閾値（例: 5000文字）に基づいてトリミングまたは空白行・コメントの圧縮処理をして保持できること。

## 3. 実現方針 (Implementation Approach)

### データ構造 (Go Structs)

セッション状態を表現する構造体は、以下のように設計します。

```go
package session

import (
	"time"
)

// Message 代表的な会話メッセージ
type Message struct {
	Role      string    `json:"role"`      // "user", "assistant", "tool"
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
	Pinned    bool      `json:"pinned"`    // コンパクション除外対象とするピン留めフラグ
}

// TrackedFile エージェントが作成したファイルのメタデータ
type TrackedFile struct {
	Path      string    `json:"path"`
	CreatedAt time.Time `json:"created_at"`
	IsDir     bool      `json:"is_dir"`
}

// TrackedProcess エージェントが起動したバックグラウンドプロセスのメタデータ
type TrackedProcess struct {
	PID       int       `json:"pid"`
	Command   string    `json:"command"`
	Args      []string  `json:"args"`
	StartedAt time.Time `json:"started_at"`
}

// SessionState シリアライズ対象のセッション全体状態
type SessionState struct {
	SessionID         string           `json:"session_id"`
	ParentID          *string          `json:"parent_id,omitempty"`
	Status            string           `json:"status"` // "active", "completed", "failed"
	Messages          []Message        `json:"messages"`
	CreatedFiles      []TrackedFile    `json:"created_files"`
	RunningProcesses  []TrackedProcess `json:"running_processes"`
	CreatedAt         time.Time        `json:"created_at"`
	LastActivityAt    time.Time        `json:"last_activity_at"`
}
```

### シリアライズ・復元の制御フロー

1.  **起動時**:
    - 指定された `session_id` に対応するファイル `[SessionDir]/[session_id].json` の存在を確認。
    - 存在する場合、ファイルをデシリアライズして `SessionState` をメモリ上に復元。
    - **トラッカー整合性検証の実行**: 復元直後に `ValidateTrackerState` を呼び出し、`CreatedFiles` の各パスの存在確認および `RunningProcesses` の各PID/コマンド名の整合性を検証する。不整合なエントリは自動的に削除許可リストから除外される。
    - 存在しない場合、新規の `session_id` で空の `SessionState` を初期化。
2.  **実行ループ中 (ツール実行やLLM応答の都度)**:
    - ファイル書き込みやプロセス起動が発生した際、`SessionState` 内の `CreatedFiles` や `RunningProcesses` に動的に追記。
    - 会話の往復（User/Assistant/Tool）のたびに、状態をファイルへ即時シリアライズして保存。
3.  **コンパクション (LLMリクエスト前の実行判定)**:
    - LLMGPへリクエストを送信する直前に、`Messages` のターン数やトークン総数を評価。
    - 閾値（例: 15ターンまたは特定トークン数）を超えている場合、古いメッセージから要約を作成し、`Pinned: true` なメッセージと要約されたメッセージのみを残して履歴を圧縮し、セッションファイルへ保存。
4.  **終了時**:
    - 最終的な状態をアトミックに書き込み、リソースを安全に閉じて終了。

## 4. 検証シナリオ (Verification Scenarios)

1.  **新規セッションファイル作成検証**:
    - エージェントを起動し、初期メッセージを送信。
    - 指定したフォルダ配下に `[session_id].json` が生成され、`messages` 配列にユーザーの指示が記録されていることを確認。
2.  **状態の復元（レジューム）検証**:
    - 生成された `[session_id].json` を開き、会話履歴があることを確認。
    - 再度同じ `session_id` を指定してエージェントを起動し、「前回の内容に基づいて」という指示を送信。
    - 新しいプロンプトに対して、前回の会話コンテキストを考慮した回答が得られることを確認。
3.  **作成ファイルの引き継ぎ検証**:
    - セッション1で `write_file` ツールを用いて `test_output.txt` を生成。
    - エージェントを一度終了。
    - 同じセッションIDを指定してエージェントを再起動し、`rm test_output.txt` に相当するツール呼び出し（またはコマンド実行）を指示。
    - ガードレールにより「自己生成ファイル」として認識され、削除処理が許可されることを確認。
4.  **コンテキスト・コンパクション検証**:
    - 多数のターンの会話を意図的に行い、`max_turns` の閾値を超える状況を発生させる。
    - ロードされたメッセージの中から、ピン留めされた最初の指示や過去の要約メッセージが残り、中間の古いメッセージ群が破棄・統合されていることを確認。

## 5. テスト項目 (Testing for the Requirements)

### 5.1 単体テスト (Unit Tests)
- `TestSessionStateSerialization`:
  モックの `SessionState` を作成し、JSONへのシリアライズとデシリアライズを実行。フィールド（特に対話履歴、親IDのポインタ、作成ファイル一覧、バックグラウンドプロセス情報）が劣化なく復元できることをアサート。
- `TestAtomicWrite`:
  書き込み中にエラーが発生する状況（ディスクフル、アクセス権限エラー等）をシミュレートし、元のセッションファイルが破壊されずに維持されることを検証。
- `TestContextCompaction`:
  長尺の対話履歴（メッセージ配列）を構築してコンパクション関数を呼び出し、ピン留めされたメッセージが必ず残り、古いメッセージが１つの要約メッセージに圧縮されるか、または正しく切り捨てられることをアサート。
- `TestTrackerStateSerialization`:
  `CreatedFiles` と `RunningProcesses` を含む `SessionState` をシリアライズし、デシリアライズ後に削除許可リストとプロセス情報が完全に復元されることを検証。
- `TestTrackerValidationOnRestore`:
  存在しないファイルパスや終了済みPIDを含む `SessionState` を構築し、復元時の整合性検証 (`ValidateTrackerState`) により不整合エントリが正しく除外されることを検証。

### 5.2 統合テスト (Integration Tests)
`integration_test.sh` にてテストを実行：
```bash
./scripts/process/integration_test.sh --categories taskengine --specify tests/integration/agent/session_test.go
```
- **CLIセッションレジューム検証**:
  CLIツールを模したインテグレーションテストを書き、2回の連続した起動で同じセッションファイルが共有・更新されることを確認します。
- **コンパクション動作結合テスト**:
  長尺のログや複数回の対話を連続で実行した際に、LLMGPへの総送信トークン数が閾値以内に維持され、かつ最初のタスク要求がLLMに正しく伝わり続けることを確認します。

