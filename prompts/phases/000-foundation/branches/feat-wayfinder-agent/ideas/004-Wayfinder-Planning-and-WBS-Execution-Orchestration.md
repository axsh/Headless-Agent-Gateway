# Wayfinder Agent - 計画作成およびWBS実行オーケストレーション仕様書

## 1. 背景 (Background)

ユーザーの要求が大規模または複雑である場合、無計画に直接ツール実行を繰り返すと、不要なファイル操作やコマンド実行が多発し、コンテキスト窓の浪費や誤動作を引き起こします。
この課題を解決するため、タスクの難易度や複雑度に応じて「計画作成」を挟み、タスクを作業分割構成（WBS: Work Breakdown Structure）として定義して順次実行する「計画・実行オーケストレーション」機能を設計します。

## 2. 要件 (Requirements)

### 必須要件 (Mandatory Requirements)
- **要求に応じた自動分岐**: エージェント起動時、ユーザーの指示の難易度（例: 「〜を調査してファイルを修正し、テストを実行せよ」など複数ファイル変更やコマンド実行を含むもの）を判断し、計画を立てるべきタスクであれば自動的に計画ルートを選択すること。
- **WBSツリー計画の自動生成**: LLMのStructured Output（構造化出力機能）等を用いて、計画を階層的なツリー構造を持つWBS（JSONフォーマット）として生成できること。各WBSノードは以下の情報を持つこと。
  - `ID`: 階層型ID（例: `"1"`, `"1.1"`, `"1.2"`, `"2"`）
  - `Name`: 短い作業ステップ名
  - `Description`: 詳細な作業指示
  - `Status`: ステータス（`pending` (未着手), `running` (実行中), `completed` (完了), `failed` (失敗)）
  - `Dependencies`: 依存先ノードIDの配列
  - `SubSteps`: 子WBSノードの配列（ネストされたツリー構造）
  - `ResultSummary`: 実行後の結果要約テキスト
- **WBSに沿ったサブエージェント実行のオーケストレーション**:
  - 生成されたWBSツリーをトラバースし、未実行（`pending`）かつ依存関係が解消されている（すべての依存先ノードが `completed` である）ノードを選択して順次実行すること。
  - 各WBSノードの実行にあたっては、新規の子セッションを生成してサブエージェント（`AgentCore.Run`）を再帰起動して担当させること。その際、親の `WorkDir` および `SessionDir` をそのまま引き継ぐこと。
  - サブエージェントの完了時、その実行サマリーをWBSノードの `ResultSummary` に記録し、ステータスを `completed` に更新すること。
- **シングルショットに完全対応したレジューム**:
  - WBSノードのステータス更新の都度、`SessionDir` 内のセッション状態ファイル（`[SessionDir]/[SessionID].json`）にWBS全体のツリー状態と実行進捗を即時永続化すること。
  - プロセスが途中で終了した場合でも、次回起動時に同一の `session_id` および同じ `SessionDir`, `WorkDir` を指定すれば、完了済みのステップをスキップして、未完了かつ依存解消されたステップから自動的に処理を再開（レジューム）できること。
- **実行失敗時のエラーリカバリーと一時停止**:
  - 特定のWBSノードでエラーが発生（ステータスが `failed` に遷移）した場合、依存する後続ノードの実行を即座にブロックすること。
  - ユーザーに対してエラーの状況を提示し、一時停止状態としてセッションを保存すること。

## 3. 実現方針 (Implementation Approach)

### WBSノードのGo構造体設計

```go
package planning

// WBSNode WBSの1ステップを表現する構造体
type WBSNode struct {
	ID            string     `json:"id"`             // 例: "1", "1.1"
	Name          string     `json:"name"`
	Description   string     `json:"description"`
	Status        string     `json:"status"`         // "pending", "running", "completed", "failed"
	Dependencies  []string   `json:"dependencies"`   // 依存するノードのID一覧
	SubSteps      []WBSNode  `json:"sub_steps,omitempty"` // 子ステップ（ツリー構造）
	ResultSummary string     `json:"result_summary,omitempty"` // 実行結果の要約
}

// WBSTree WBS全体を管理する構造体
type WBSTree struct {
	RootNodes []WBSNode `json:"root_nodes"`
}
```

### オーケストレーション制御ループ

1.  **判定フェーズ**:
    - `AgentCore` は、最初のプロンプトを投げて「計画が必要か（IsPlanningRequired）」をLLMに判定させます。
    - 計画が必要と判定された場合、LLMに対してStructured Output（または厳格なJSONスキーマ）を用いて `WBSTree` 構造の計画を出力させます。
2.  **実行フェーズ**:
    - `WBSOrchestrator` を呼び出して、生成された `WBSTree` をトラバースします。
    - ループ処理:
      - 次に実行可能なノード（Status: `pending`、かつすべての `Dependencies` のStatusが `completed`）を決定。
      - 実行可能なノードが存在しない場合：
        - 全てのノードが `completed` であれば処理終了。
        - 一部が `failed` であればエラー終了。
        - 依存関係のデッドロックがある場合はエラー終了。
      - 実行可能ノードに対して、`SubagentExecutor` を用いて再帰的に `AgentCore.Run` を子セッションで駆動。
      - 子セッションから返ってきた要約を `ResultSummary` に記録し、ノードステータスを `completed` にしてセッションファイルをアトミックに即時保存。
      - ループを継続。

## 4. 検証シナリオ (Verification Scenarios)

1.  **WBS計画生成検証**:
    - エージェントに「新しいモデル定義を追加し、マイグレーションファイルを作成し、テストを走らせて」と大規模な指示を送信。
    - ログまたはセッションファイル上で、要求を分割したWBS（階層構造を持つJSON）が自動生成されることを検証。
2.  **順次実行と親子セッション検証**:
    - 生成されたWBSに沿って、各ステップが順次開始され、子セッションが新規に生成されながらツールを実行していくことを確認。
    - ステップ1の完了結果（要約）が、ステップ2のサブエージェント起動時の「ヒント」として正しく伝達されることを確認。
3.  **WBS実行の中断とレジュームの検証**:
    - WBS実行中のステップ3の処理の途中で、CLIプロセスを強制終了（Ctrl+Cなど）。
    - セッションファイル `[SessionID].json` 内で、ステップ1, 2が `completed`、ステップ3が `pending` または `running` で保存されていることを確認。
    - 再度同一の `session_id` で起動し、ステップ1, 2が再実行されず、スキップされてステップ3から処理が再開されることを検証。
4.  **エラー時の一時停止検証**:
    - テスト実行ステップで意図的にテストを失敗させる。
    - 当該ノードのステータスが `failed` になり、依存している後続の「ドキュメント更新」ステップの実行がスキップされ、エージェントが一時停止状態で終了することを確認。

## 5. テスト項目 (Testing for the Requirements)

### 5.1 単体テスト (Unit Tests)
- `TestWBSDependencyResolution`:
  様々な依存関係を持つ `WBSTree` をモックで構築し、次に実行可能なノードを正しくリストアップできるアルゴリズムを検証。
- `TestWBSTreeSerialization`:
  ネストされたツリー状態がセッションJSONファイルに正しくシリアライズ・デシリアライズされることを検証。

### 5.2 統合テスト (Integration Tests)
`integration_test.sh` にてテストを実行：
```bash
./scripts/process/integration_test.sh --categories taskengine,llm --specify tests/integration/agent/wbs_orchestration_test.go
```
- **WBSレジューム結合テスト**:
  WBS実行中にエラーを挟み、セッションをロードし直してエラーを解消した後に再実行すると、残りのステップのみが実行されて全体が正常終了することをシミュレーション検証します。
