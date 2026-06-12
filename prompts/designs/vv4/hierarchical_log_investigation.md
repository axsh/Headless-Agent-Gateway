# 階層化ログ (Hierarchical Log) 調査レポート

## 調査概要

**目的**: vv4の階層化ログ (parentLogIdによるログの親子関係) について、仕様・実装・現状の動作状況を調査し、HAGでの実装に向けた仕様をドキュメント化する。

**背景**: feat-vibe-codingブランチで「ログに親子関係を持たせて子要素を畳めるようにし、ユーザが読むべきログを減らす機構」を検討中。vv4のコードにはこの機構のインフラが存在するが、動作していない。

**調査日**: 2026-06-03

---

## 1. vv4における階層化ログの設計

### 1.1 仕様書の定義

仕様書 [025-AgentStreamingLog.md](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/reference_repo/vv4/prompts/phases/000-foundation/branches/feat-coding-agents/ideas/025-AgentStreamingLog.md) のR1-1に以下が定義されている:

> `BeginLog` は新しいログID（タスクIDとは別の一意ID）を発行し、コンテキスト（`kind`, `location` 等）を引数に持つ。入れ子をサポート: `BeginLog` 中にさらに `BeginLog` ができる（親子関係のスタック構造）。

```
logId1 = BeginLog(ctx={kind:"text", location:"agents/ClaudeAgent.ts:42"})
                                      ← ルートログ開始、ログID発行
  SendLog(logId1, "推論中...")          ← ルートにテキスト追記
  logId2 = BeginLog(ctx={kind:"tool_use", parentLogId:logId1, location:"agents/ClaudeAgent.ts:55"})
                                      ← 子ログ開始（親のログIDを指定して入れ子）
    SendLog(logId2, "ツール実行: grep")
    SendLog(logId2, "結果: 3件ヒット")
  EndLog(logId2)                      ← 子ログ終了、スタックが1段戻る
  SendLog(logId1, "結論は...")          ← ルートへの追記再開
EndLog(logId1)                        ← ルートログ終了
```

### 1.2 仕様で定義されたGUI表示要件

- GUIでは入れ子ログをインデント表示または折りたたみで視覚的に階層構造を示す
- `kind: "thinking"` はデフォルトで折りたたみ表示。クリックで展開可能
- `kind: "error"` は赤色表示、常時展開
- 未知の `kind` を受信した場合は `text` と同等に表示（フォールバック）
- フロントエンドは `depth` をフロントエンド側で計算（仕様書 L199: `depth?: number; // Nesting depth (frontend-computed)`）

### 1.3 仕様で定義されたストリーミングプロトコル

AgentLogEntryは3つのPhaseで構成される:
- `begin`: ストリーム開始。新しい行の生成。`kind`, `location`, `parentLogId` 等のメタ情報を含む
- `send`: テキストチャンクの送信。同一IDの行に追記
- `end`: ストリーム完了。`isComplete: true` をセット

---

## 2. vv4における実装状況

### 2.1 バックエンド: 実装済みだが未使用

**実装済みの部分**:

| コンポーネント | ファイル | 状態 |
|---|---|---|
| `AgentLogEntry` 構造体 | [agent_log_entry.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/reference_repo/vv4/shared/libs/go/taskengine/tasklog/agent_log_entry.go) | 完全に実装済み |
| `ParentLogID` フィールド | agent_log_entry.go L34 | フィールド定義済み |
| `WithParentLogID` オプション | agent_log_entry.go L72-76 | functional option実装済み |
| `NewAgentLogEntry` (begin) | agent_log_entry.go L88-103 | 実装済み |
| `NewAgentLogSendEntry` (send) | agent_log_entry.go L107-118 | 実装済み |
| `NewAgentLogEndEntry` (end) | agent_log_entry.go L123-134 | 実装済み |
| `toLogViewEntry` でのWS送信 | [service.go L1297-1299](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/reference_repo/vv4/shared/libs/go/taskengine/editor/service/service.go#L1297-L1299) | `parentLogId` をWebSocketペイロードに含める処理が実装済み |
| 単体テスト | [agent_log_entry_test.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/reference_repo/vv4/shared/libs/go/taskengine/tasklog/agent_log_entry_test.go) | `WithParentLogID` のテスト済み |

**未使用の部分**:

`WithParentLogID` は**テストコード以外で一切呼び出されていない**。プロダクションコード (Coding Agent driver、ロジック実行コード等) で `NewAgentLogEntry` を呼ぶ際に `WithParentLogID` オプションを渡している箇所は存在しない。

つまり、**バックエンドインフラは完成しているが、実際にログの親子関係を設定するコードが書かれていない**。

### 2.2 フロントエンド: 型定義のみ

**実装済み**:
- [types.ts L36](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/reference_repo/vv4/features/frontend/webview/src/types.ts#L36): `parentLogId?: string` フィールド定義済み
- 仕様書では `depth?: number` (フロントエンド側で計算するネスト深度) も定義されている

**未実装**:
- `parentLogId` を使った階層ツリーの構築ロジック
- 折りたたみ/展開UIの `parentLogId` 連動
- インデント表示
- `depth` の計算ロジック

`AgentsPanel.tsx` のLogTableコンポーネントは全てのログエントリをフラットなテーブル行として表示しており、親子関係によるグルーピングや折りたたみは行っていない。

### 2.3 DBモデル

[service.go L1100-1110](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/reference_repo/vv4/shared/libs/go/taskengine/editor/service/service.go#L1100-L1110) のDB永続化では、`AgentLogEntry` を `json.Marshal` でシリアライズしてそのまま `EntryData` に保存しているため、`ParentLogID` を含むJSONが保存される。ただし、DBスキーマに `parent_log_id` カラムは存在しない (JSONの中に埋め込まれている)。

---

## 3. 動作していない理由の分析

階層化ログが機能しない理由は以下の3点:

### 3.1 ログ生成側がparentLogIdを設定していない

Coding Agent driverやロジック実行コードが `AgentLogEntry` を生成する際に、`WithParentLogID` オプションを使っていない。全てのログが `ParentLogID: ""` (ルートレベル) として生成される。

### 3.2 フロントエンドにツリー構築ロジックがない

`parentLogId` フィールドは型定義に存在するが、それを使ってログを階層化する処理がない。`AgentContext.tsx` の `mergeStreamingLog` はIDベースのストリーミングマージ (同一IDの `begin`/`send`/`end` を結合) のみ行っており、親子関係は無視される。

### 3.3 折りたたみUIが接続されていない

`CollapsibleSection.tsx` コンポーネントが存在するが、これはLogTableのログ行に使用されていない。LogTableは単純な `<table>` ベースのフラットリストで、折りたたみ機構を持たない。

---

## 4. 有るべき姿: 階層化ログの完全な仕様

仕様書 (025) とコードの分析から、完全に動作する階層化ログの設計は以下の通り:

### 4.1 データモデル

```go
// AgentLogEntry - vv4の設計をそのまま活用
type AgentLogEntry struct {
    BaseEntry

    Body        string  // 可読テキスト
    Kind        string  // "text", "thinking", "tool_use", "tool_result", "system", "error"
    Location    string  // ソースコード場所 (例: "agents/ClaudeAgent.ts:42")
    ParentLogID string  // 親ログのID (空=ルートレベル)
    TaskNodeID  string  // タスクノードID
    AgentID     string  // エージェントID
    RunID       string  // 実行ストリームID
    IsComplete  bool    // EndLogが呼ばれたか
    Phase       string  // "begin", "send", "end"
}
```

### 4.2 ログ生成側の責務

Coding Agent (または DriverAdapter) がLLM応答をストリーミングで受信する際に、以下のパターンでログを生成する:

```
[ルートログ] kind:"text" - エージェントの全体的な推論プロセス
  [子ログ1] kind:"thinking" parentLogId:ルートID - 思考過程 (デフォルト折りたたみ)
  [子ログ2] kind:"tool_use" parentLogId:ルートID - ツール呼び出し
    [孫ログ] kind:"tool_result" parentLogId:子ログ2のID - ツール結果
  [子ログ3] kind:"text" parentLogId:ルートID - 最終出力
```

重要なのは、**ログの生成者が親ログIDを意識的に設定する必要がある**という点。自動的に階層化されるわけではなく、ログ生成側がストリームの文脈に応じてparentLogIdを設定する。

### 4.3 フロントエンドの責務

1. **ツリー構築**: WebSocketで受信したフラットなログ配列を `parentLogId` に基づいてツリー構造に変換する
2. **depth計算**: 各ログのネスト深度をフロントエンド側で計算する
3. **折りたたみ/展開**: 
   - `kind: "thinking"` はデフォルト折りたたみ
   - `kind: "error"` は常時展開
   - その他はデフォルト展開
   - ユーザがクリックでトグル可能
4. **インデント表示**: `depth * 16px` のパディングでインデント
5. **子ログの表示/非表示**: 親ログが折りたたまれている場合、全ての子孫ログを非表示にする

### 4.4 異常終了時の処理

- セッション終了時 (`TERMINATED` エントリ受信時) に、未クローズのログストリーム (`isComplete: false`, `phase != "end"`) を検出
- 自動的に `isComplete: true` にセットし、ストリーミングカーソルを消す
- 必要に応じて `"[auto-closed: abnormal termination]"` メッセージを付与

### 4.5 WebSocket ペイロード

バックエンド → フロントエンドへのWebSocketメッセージ:

```json
{
  "type": "log",
  "payload": {
    "deployment_id": "deploy-1",
    "agent_id": "agent-1",
    "process_id": "proc-1",
    "alias": "Agent-1",
    "entry": {
      "id": "log-uuid-1",
      "time": "2026-06-03T12:00:00Z",
      "entryType": "AGENT_LOG",
      "body": "推論中...",
      "phase": "begin",
      "kind": "text",
      "isComplete": false,
      "parentLogId": "",
      "taskNodeId": "task-1",
      "agentId": "agent-1"
    }
  }
}
```

子ログの場合:

```json
{
  "type": "log",
  "payload": {
    "entry": {
      "id": "log-uuid-2",
      "parentLogId": "log-uuid-1",
      "kind": "thinking",
      "phase": "begin",
      ...
    }
  }
}
```

---

## 5. HAGへの実装に向けた考慮事項

### 5.1 vv4との差異

| 項目 | vv4 | HAG |
|------|-----|-----|
| ログ生成元 | Task Engine内のLogic実行 | Coding Agent Driver |
| ストリーミング経路 | Agent Container SSE → Backend WS | Agent CLI stdout → Driver → Extension WS |
| フロントエンド | VSCode WebView (React) | VSCode WebView (React) |
| DB永続化 | SQLite (EntryData JSON) | TBD |

### 5.2 実装すべきレイヤー

```
[Coding Agent CLI]
    |
    | stdout/stderr (LLM応答ストリーム)
    v
[Agent Driver (Go)]
    |
    | AgentLogEntry (begin/send/end + parentLogId)
    |
    v
[Agent Service / WebSocket]
    |
    | JSON payload (parentLogId含む)
    v
[Frontend (React)]
    |
    | ツリー構築 + 折りたたみUI
    v
[ユーザが見るログ]
```

### 5.3 ログ階層化の具体的なユースケース

1. **Claude Code のツール呼び出し**: Claude Codeが思考 → ツール使用 → 結果取得 → 次の思考、というサイクルを繰り返す。各サイクルをルートログの子として階層化し、思考過程はデフォルト折りたたみにすることで、ユーザは「何のツールを使ったか」のみを素早く把握できる

2. **エラーの階層化**: エラーが発生した場合、エラーの原因となったツール呼び出しや思考過程を子ログとして保持し、エラーログを展開すると詳細が見える

3. **長大なログの圧縮**: Coding Agentのセッションは数百行のログを生成することがある。階層化により、ルートレベルのログ数を大幅に削減し、ユーザは興味のある部分のみを展開して確認できる

### 5.4 kindの拡張案

vv4の初期定義に加え、HAGで有用と思われるkind:

| kind | 表示制御 | 説明 |
|------|---------|------|
| `text` | デフォルト展開 | 通常テキスト出力 |
| `thinking` | デフォルト折りたたみ | LLMの思考過程 (Extended Thinking) |
| `tool_use` | デフォルト展開 | ツール呼び出し情報 |
| `tool_result` | デフォルト折りたたみ | ツール実行結果 (長いことが多い) |
| `system` | デフォルト展開 | システムメッセージ |
| `error` | 常時展開・赤色 | エラーメッセージ |
| `diff` | デフォルト折りたたみ | コード差分 (将来) |
| `file_edit` | デフォルト展開 | ファイル編集操作 |

---

## 6. 結論

### 現状のまとめ

- vv4の階層化ログは**仕様として設計済み、バックエンドインフラとして実装済み、しかし実際には使われていない**
- 理由は、ログ生成側が `WithParentLogID` を呼ばず、フロントエンドにツリー構築ロジックがないため
- 型定義・データモデル・WebSocketペイロード形式は全て準備されており、「配線」が欠けている状態

### HAGでの実装方針

1. **データモデル**: vv4の `AgentLogEntry` をそのまま採用 (ParentLogID, Kind, Phase等)
2. **ログ生成**: Agent DriverがCoding Agent CLIの出力をパースし、適切にparentLogIdを設定してAgentLogEntryを生成する
3. **バックエンド中継**: vv4の `BroadcastLog` + `toLogViewEntry` パターンを踏襲。`parentLogId` をWebSocketペイロードに含める
4. **フロントエンド**: parentLogIdベースのツリー構築、折りたたみ/展開UI、depth計算、kindベースのデフォルト表示制御をスクラッチ実装

---

## 変更履歴

| 日付 | 変更内容 |
|------|---------|
| 2026-06-03 | 初版作成。vv4調査とHAG向け仕様整理 |
