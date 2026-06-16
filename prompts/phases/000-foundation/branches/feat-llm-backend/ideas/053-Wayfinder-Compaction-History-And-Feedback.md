# 053: Wayfinder のコンパクション、履歴管理、およびユーザー対話メカニズムの改善

## 背景 (Background)

これまでの実行セッション（[wf-1781489054678940400](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-llm-backend/tmp/.wayfinder/wf-1781489054678940400/)）の調査レポートにより、以下の問題点と改善要求が定義された。

1. **ターン数ベースの過剰なコンパクション**: 現在はターン数（デフォルト15）を超えると一律でアグレッシブなコンパクションが走り、詳細なコンテキストが消失していた。モデルのコンテキスト制限に達したエラーを受けたタイミングでコンパクションをトリガーする（リアクティブな制御）とし、圧縮比率を設定可能にする。
2. **履歴（history）のバグとフラットな保存構造**: コンパクション時にシーケンス番号が巻き戻って過去の履歴ファイルを上書きして破損させていた。また、サブセッション（子エージェント）の履歴が整理されておらず不鮮明であったため、親セッションのインデックスをプレフィックスとした階層的かつシーケンシャルな16進数（7桁）命名規則で全メッセージを記録する。
3. **WBS実行中のユーザー待機不足**: WBS実行ループが自動進行するため、アシスタントがユーザー確認を必要とするメッセージを返してもオーケストレーターが停止せず、誤った状態でタスクを完了させていた。Claude Codeのような対話メカニズム（一時停止と再開）を取り入れる。

---

## 要件 (Requirements)

### R1: コンテキスト長超過時のリアクティブ・コンパクション (必須)

- **トリガーの変更**: 事前のターン数判定によるコンパクションを廃止し、LLMクライアントが「コンテキスト長超過エラー」を返した時にのみコンパクションをトリガーする。
- **超過エラーの検知**: 
  - BifrostClientやLLM Gatewayが返すエラーから、コンテキスト制限到達を検知するヘルパーメソッド（例：`isContextLengthExceeded(err error) bool`）を定義する。
  - HTTP 400（BadRequest）等のレスポンスに含まれる `"context_length_exceeded"`, `"max_context_length"`, `"context_limit"`, `"too many tokens"` などのパターンを解析する。
- **圧縮比率の設定化**: 
  - `config.yaml`（`AgentConfig`）に `compaction_ratio` フィールドを追加する（デフォルト値：`0.5`）。
  - 比率が `0.5`（50%）の場合、Pinned（固定）メッセージを除外した全メッセージの「古い方から半分（50%）」を対象にMapReduce要約を行い、1つの `[COMPACTED CONTEXT SUMMARY]` システムメッセージに置き換える。
- **リトライ処理**: コンパクション適用後、同一リクエストを1回自動でリトライする。

---

### R2: 階層的・16進数シーケンシャル履歴保存機能 (必須)

- **16進数エンコード**: 履歴ファイル名について、従来の9桁の10進数（`000000001.json`）を廃止し、**7桁の16進数**（小文字・ゼロ埋め、例：`0000001.json`, `000001a.json`）に変更する。
- **サブセッションの階層化**: 
  - 子セッション（サブエージェント）が実行される際、親セッションにおける該当呼び出し元の16進数シーケンス番号（例：`000000a`）を、子セッションのプレフィックス（`HistoryPrefix`）として引き継ぐ。
  - 子セッションのメッセージは、親セッションと共通の履歴ディレクトリ（`history/`）に、ハイフンで連結したファイル名（例：`000000a-0000001.json`）で保存される。さらに階層が深くなれば `000000a-0000005-0000001.json` と連結を繰り返す。
- **完全なシーケンシャル記録（上書きバグ修正）**:
  - `ChatMessage` 構造体に `Seq int` フィールドを追加し、会話に新規メッセージが追加されるたびにグローバルに一意の連番（シーケンス）を割り当てる。
  - コンパクションが実行されても、残されたメッセージの `Seq` は変更せず元の値を保持する。
  - `Save()` メソッドでは、`Seq` が前回保存時の最大値（`prevLatestSeq`）より大きい新しいメッセージのみを追記するように変更する。
  - ツール呼び出し（`tool_use` / `tool_result`）や、セッション完了時の最終要約を含め、すべてのメッセージを欠落なく記録する。

---

### R3: 一時停止と再開を伴うユーザー対話（`ask_user`）メカニズム (必須)

- **`ask_user` ツールの定義**:
  - Wayfinderの標準ツールに `ask_user` を追加する。
  - **インプットスキーマ**:
    ```json
    {
      "type": "object",
      "properties": {
        "prompt": {
          "type": "string",
          "description": "ユーザーに提示する質問、フィードバック要求、またはプレイテストなどの指示"
        }
      },
      "required": ["prompt"]
    }
    ```
- **実行サスペンド機能**:
  - エージェントのツール実行ループで `ask_user` が呼ばれた場合、ツールは対話入力を待機する特別なエラー `ErrFeedbackRequired` またはシグナルを返す。
  - エージェントおよびWBSオーケストレーターはこのシグナルを検知すると、現在の実行を安全に中断する：
    1. 実行中のWBSノードステータスを `StatusPendingUser` または `StatusSuspended` に更新する。
    2. セッションステータスを `suspended` とし、現在のWBSツリーおよび会話コンテキストを永続化（保存）する。
    3. `ask_user` の `prompt` をターミナルに表示し、CLIプロセスを正常終了する。
- **セッションの再開（レジューム）**:
  - ユーザーが `ternctl run --resume [SessionID] --prompt "[回答内容]"` を実行してセッションを再開する。
  - サーバーはセッションが `suspended` であることを検知し、保留中となっていたノードおよびコンテキストを復元する。
  - ユーザーからの入力値（`--prompt`の値）を `ask_user` 呼び出しの実行結果（`tool_result`）としてコンテキストに挿入し、LLM実行ループを再開する。

---

## 実現方針 (Implementation Approach)

### 1. コンパクションの改善 (R1)
- [config.go](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-llm-backend/shared/libs/go/wayfinder/config.go) の `AgentConfig` に `CompactionRatio float64` を追加し、`settings/demo/config.yaml` などの設定ファイルから読み込めるようにする。
- [agent_core.go](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-llm-backend/shared/libs/go/wayfinder/agent_core.go) の `runSimple` ループにおいて、LLMリクエスト送信前のコンパクション判定を削除。LLM呼び出しエラー時に `isContextLengthExceeded(err)` を評価してコンパクションを適用し、リトライする設計に変更。

### 2. 履歴管理の階層化・16進数シーケンシャル化 (R2)
- [history.go](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-llm-backend/shared/libs/go/wayfinder/session/history.go) でファイル名の生成フォーマットを `%09d.json` から `%07x.json` に変更。
- [session_store.go](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-llm-backend/shared/libs/go/wayfinder/session/session_store.go) において、`Store` 構造体に `Prefix string` と `OverrideHistoryDir string` フィールドを追加。子セッション生成時にこれらを設定し、同一ディレクトリへの上書きされない書き込みを実現。
- コンテキストメッセージへの `Seq` 割り当てと、`Save()` でのフィルタリングの実装。

### 3. `ask_user` ツールの実装とWBS制御 (R3)
- `shared/libs/go/wayfinder/tools/` 下に `tool_ask_user.go` を新設し、ツールとして登録する。
- `WBSOrchestrator` の `Execute` メソッドで `StatusPendingUser` 時の終了処理とレジューム時のハンドリングロジックを実装。

---

## 変更対象ファイル一覧

| コンポーネント | ファイル | 変更内容 |
| :--- | :--- | :--- |
| wayfinder | `config.go` | `CompactionRatio` 設定項目の追加 |
| wayfinder | `agent_core.go` | reactive compactionの実装、`ask_user` レジューム処理 |
| wayfinder/tools | [NEW] `tool_ask_user.go` | `ask_user` ツールの定義 |
| wayfinder/tools | `register.go` | `ask_user` のレジストリ登録 |
| wayfinder/session | `session_state.go` | `ChatMessage` への `Seq` フィールドの追加 |
| wayfinder/session | `session_store.go` | プレフィックス、保存先オーバーライド、`Seq`による重複・上書き回避 |
| wayfinder/session | `history.go` | 16進数名生成、プレフィックス連結処理 |
| wayfinder/planning| `wbs_orchestrator.go` | 対話待機時のステータスハンドリング、サスペンド対応 |

---

## 検証シナリオ (Verification Scenarios)

### シナリオ 1: reactive compaction と比率設定
1. `config.yaml` に `compaction_ratio: 0.6` を設定する。
2. LLM Gateway 側で最大コンテキスト制限を擬似的に極小値に制限する。
3. エージェントを実行し、制限超過エラー発生時のみコンパクションが走り、古い方から60%のメッセージが要約に統合され、処理が継続することを確認する。

### シナリオ 2: 16進数階層化履歴の出力確認
1. WBSを利用したタスク（子セッションを含むもの）を実行する。
2. `history/` ディレクトリを確認し、ファイル名が `0000001.json` のような小文字7桁16進数になっていることを確認する。
3. 子セッションのログが `000000a-0000001.json` の形式で親セッションと同一の `history/` ディレクトリに出力されていることを確認する。
4. ファイルの上書きやタイムスタンプの逆戻りが発生せず、すべてのツール呼び出しが欠落なく保存されていることを検証する。

### シナリオ 3: `ask_user` を用いた一時停止と再開
1. ユーザー確認を必要とするWBSステップを実行する。
2. エージェントが `ask_user` ツールを呼び出し、ターミナルに質問を表示してセッションを `suspended` 状態で一時停止し、プロセスが正常終了することを確認する。
3. `ternctl run --resume [SessionID] --prompt "[回答内容]"` を実行して再開する。
4. 中断されたWBSノードから再開され、回答内容がLLMに渡されてタスクが継続することを確認する。

---

## テスト項目 (Testing for the Requirements)

### 単体テスト
- `wayfinder/session/history_test.go`: 16進数シーケンシャルおよびプレフィックス連結ファイル名のテスト。
- `wayfinder/session/compaction_test.go`: `CompactionRatio` に基づく圧縮対象抽出のテスト。
- `wayfinder/tools/tools_test.go`: `ask_user` ツールの動作検証と中断シグナルのテスト。

### 統合テスト
- `integration_test.sh --specify TestE2E_Wayfinder_ReactiveCompaction`
- `integration_test.sh --specify TestE2E_Wayfinder_WBS_AskUser_Resume`
