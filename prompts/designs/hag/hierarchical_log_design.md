# 階層化ログ設計 (Hierarchical Log)

## 概要

Coding Agentのログに親子関係を持たせ、子要素を折りたたみ可能にすることで、ユーザが読むべきログ量を削減する。

## 背景と動機

Coding Agentの1セッションは数百行のログを生成する。思考過程、ツール呼び出し、ツール結果、最終出力が全てフラットに並ぶため、ユーザが重要な情報を見つけるのが困難である。

階層化により以下を実現する:
- 思考過程 (thinking) をデフォルト折りたたみにし、必要な時のみ展開
- ツール結果 (tool_result) の長大な出力を折りたたみ
- ルートレベルには「何をしたか」のサマリのみ表示

参考: vv4での調査結果は [hierarchical_log_investigation.md](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/prompts/designs/vv4/hierarchical_log_investigation.md) に記載。vv4ではデータモデルまで実装済みだが、実際の親子設定とフロントエンドUIが欠けており動作していない。

---

## データモデル

### AgentLogEntry

```go
type AgentLogEntry struct {
    ID          string    `json:"id"`
    Time        time.Time `json:"time"`
    EntryType   string    `json:"entryType"`   // 固定値: "AGENT_LOG"

    Body        string    `json:"body"`                    // 可読テキスト
    Kind        string    `json:"kind"`                    // 表示制御ディレクティブ
    Location    string    `json:"location,omitempty"`      // ソースコード場所
    ParentLogID string    `json:"parentLogId,omitempty"`   // 親ログID (空=ルートレベル)
    TaskNodeID  string    `json:"taskNodeId,omitempty"`    // タスクノードID
    AgentID     string    `json:"agentId"`                 // エージェントID
    RunID       string    `json:"runId,omitempty"`         // 実行ストリームID
    IsComplete  bool      `json:"isComplete"`              // EndLogが呼ばれたか
    Phase       string    `json:"phase"`                   // "begin", "send", "end"
}
```

### ストリーミングプロトコル (Phase)

| Phase | 役割 | ParentLogID | Body |
|-------|------|-------------|------|
| `begin` | 新しいログストリーム開始 | 親があれば親のID | 通常は空 |
| `send` | テキストチャンク送信 | 不要 (beginで確定済み) | チャンクテキスト |
| `end` | ストリーム完了 | 不要 | 完全テキスト (任意) |

- `begin` でIDが発行され、`kind`, `location`, `parentLogId` が確定する
- `send` と `end` はIDのみで対象を特定する
- `kind` は `begin` 時に確定し、途中変更はできない。種別を切り替える場合は子ログを新たに `begin` する

### Kind (表示制御ディレクティブ)

拡張可能な文字列型とし、ハードコードの列挙型にしない。

| kind | デフォルト表示 | 説明 |
|------|-------------|------|
| `text` | 展開 | 通常テキスト出力 |
| `thinking` | 折りたたみ | LLMの思考過程 |
| `tool_use` | 展開 | ツール呼び出し情報 |
| `tool_result` | 折りたたみ | ツール実行結果 |
| `system` | 展開 | システムメッセージ |
| `error` | 常時展開・赤色 | エラーメッセージ |
| (未知) | `text` と同等 | フォールバック |

---

## 階層構造の例

### Coding Agentの一般的なログ階層

```
[root] kind:"text" - "ファイルの内容を確認します"
  [child-1] kind:"thinking" parentLogId:root - "まずはディレクトリ構造を..." (折りたたみ)
  [child-2] kind:"tool_use" parentLogId:root - "Read file: main.go"
    [grandchild-1] kind:"tool_result" parentLogId:child-2 - "package main\nimport..." (折りたたみ)
  [child-3] kind:"thinking" parentLogId:root - "このファイルを修正する必要が..." (折りたたみ)
  [child-4] kind:"tool_use" parentLogId:root - "Edit file: main.go"
    [grandchild-2] kind:"tool_result" parentLogId:child-4 - "Applied 3 changes" (折りたたみ)
  [child-5] kind:"text" parentLogId:root - "修正が完了しました"
```

ユーザに見えるデフォルト表示:
```
[-] ファイルの内容を確認します
    [+] thinking (折りたたみ)
    [-] Read file: main.go
        [+] tool_result (折りたたみ)
    [+] thinking (折りたたみ)
    [-] Edit file: main.go
        [+] tool_result (折りたたみ)
    [-] 修正が完了しました
```

---

## アーキテクチャ

### データフロー

```
[Coding Agent CLI]
    |
    | stdout/stderr パース
    v
[Agent Driver (Go)]
    |
    | AgentLogEntry (begin/send/end + parentLogId) 生成
    |
    v
[WebSocket 中継]
    |
    | JSONペイロード
    v
[Frontend (React)]
    |
    | ツリー構築 + 折りたたみUI
    v
[ユーザが見るログ]
```

### Agent Driver の責務

Agent Driverは、Coding Agent CLIの出力ストリームをパースし、以下のルールで `AgentLogEntry` を生成する:

1. **ルートログの生成**: エージェントのメッセージ開始時に `begin` を発行。以降のチャンクは `send` で追記
2. **子ログの生成**: thinking block開始、tool_use検出時に新しい `begin` (parentLogId付き) を発行
3. **ログの終了**: ブロック終了時に `end` を発行
4. **スタック管理**: 現在のログIDスタックを保持し、parentLogIdを自動設定

```go
// Agent Driver内のログスタック管理 (概念)
type LogStack struct {
    mu    sync.Mutex
    stack []string // ログIDのスタック
}

func (s *LogStack) CurrentParentID() string {
    s.mu.Lock()
    defer s.mu.Unlock()
    if len(s.stack) == 0 {
        return ""
    }
    return s.stack[len(s.stack)-1]
}

func (s *LogStack) Push(logID string) {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.stack = append(s.stack, logID)
}

func (s *LogStack) Pop() string {
    s.mu.Lock()
    defer s.mu.Unlock()
    if len(s.stack) == 0 {
        return ""
    }
    id := s.stack[len(s.stack)-1]
    s.stack = s.stack[:len(s.stack)-1]
    return id
}
```

### フロントエンドの責務

1. **ツリー構築**: フラットなログ配列を `parentLogId` でツリー構造に変換する
2. **depth計算**: 各ログのネスト深度をフロントエンド側で計算
3. **折りたたみ/展開制御**:
   - kindに基づくデフォルト展開/折りたたみ状態の決定
   - ユーザのクリックによるトグル
   - 親が折りたたまれている場合、全ての子孫を非表示
4. **インデント表示**: `depth * インデント幅` のパディング
5. **ストリーミングマージ**: 同一IDの `begin`/`send`/`end` を結合

### 異常終了時の処理

- `TERMINATED` エントリ受信時に、未クローズのログストリーム (`isComplete: false`) を検出
- 自動的に `isComplete: true` にセットし、ストリーミングカーソルを消去
- `"[auto-closed: abnormal termination]"` メッセージを付与

---

## WebSocket ペイロード

```json
{
  "type": "log",
  "payload": {
    "agent_id": "agent-1",
    "process_id": "proc-1",
    "alias": "Agent-1",
    "entry": {
      "id": "log-uuid-1",
      "time": "2026-06-03T12:00:00Z",
      "entryType": "AGENT_LOG",
      "body": "",
      "phase": "begin",
      "kind": "text",
      "isComplete": false,
      "agentId": "agent-1"
    }
  }
}
```

子ログの場合、`parentLogId` が追加される:

```json
{
  "entry": {
    "id": "log-uuid-2",
    "parentLogId": "log-uuid-1",
    "kind": "thinking",
    "phase": "begin",
    "body": "",
    "isComplete": false,
    "agentId": "agent-1"
  }
}
```

---

## 変更履歴

| 日付 | 変更内容 |
|------|---------|
| 2026-06-03 | 初版作成 |
