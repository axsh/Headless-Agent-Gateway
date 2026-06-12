# 006-Foundation-Part4-HierarchicalAgentLog

> **Source Specification**: [prompts/phases/000-foundation/branches/feat-llm-backend/ideas/003-HierarchicalAgentLog.md](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/prompts/phases/000-foundation/branches/feat-llm-backend/ideas/003-HierarchicalAgentLog.md)

## Goal Description

Coding Agentが生成する大量の思考・ツール実行などのログを、親子関係（`parentLogId`）によって階層化して整理・表現する「階層化ログ (Hierarchical Agent Log)」のバックエンド基盤を構築します。
本計画では、Goの `shared/libs/go/tasklog` パッケージを新規実装し、以下のコア機能を開発・検証します。

1. **データモデル (`AgentLogEntry`)**: 親子関係やPhase（begin/send/end）、表示属性（kind）を保持し、`tasklog.Entry` インターフェースを実装する。
2. **ストリーミングプロトコル (Phase)**: ログの生成・更新を3フェーズ（開始、チャンク追記、完了）で管理するためのファクトリ関数とFunctional Option。
3. **親子関係のスタック管理 (`LogStack`)**: エージェント実行中に親子関係のコンテキストを自動管理するための並行安全なスタック。
4. **ログの自動クローズ処理（異常終了時のクローズ）**: 異常終了イベント（`TERMINATED`）受信時に、未完了のログストリームを自動的に正常クローズし補完メッセージを付与する。

> [!NOTE]
> フロントエンド部分（R5-R8: WebSocket中継、ツリー構築、UI折りたたみ等）は、バックエンドの `tasklog` パッケージが完成した後の別フェーズで実装するため、本計画の対象外とします。

## User Review Required

None.

## Requirement Traceability

> **Traceability Check**:
> 仕様書(Specification)の要件・決定事項をリストアップし、この計画書のどこで対応するかをマッピングしています。

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| **R1-1**: `AgentLogEntry` 構造体フィールド定義 | Proposed Changes > `agent_log_entry.go` |
| **R1-2**: `BaseEntry` の定義（ID, Time, EntryType） | Proposed Changes > `entry.go` |
| **R1-3**: `tasklog.Entry` インターフェース実装 | Proposed Changes > `entry.go`, `agent_log_entry.go` |
| **R2-1**: 3つのPhase（`begin`/`send`/`end`） | Proposed Changes > `agent_log_entry.go` |
| **R2-2/2-3/2-4**: 各Phaseのフィールド制約ルール | Proposed Changes > `agent_log_entry.go` |
| **R2-5**: ファクトリ関数 (`NewAgentLogEntry`/`Send`/`End`) | Proposed Changes > `agent_log_entry.go` |
| **R2-6**: Functional Option (`WithKind` 等) | Proposed Changes > `agent_log_entry.go` |
| **R3-1/3-2/3-3**: `kind` 表示属性とデフォルト値 | Proposed Changes > `agent_log_entry.go` |
| **R4-1/4-2**: Agent Driverでのログ生成（CLIパース） | 後続フェーズに先送り（※将来Agent Driver開発時に実装） |
| **R4-3**: `LogStack` の実装とスレッドセーフ化 | Proposed Changes > `log_stack.go` |
| **R5**: WebSocket中継 | 後続フェーズに先送り（※将来WebSocket/AgentService実装時に実装） |
| **R6/R7/R8**: フロントエンドのツリー構築・UI折りたたみ | 後続フェーズに先送り（※フロントエンド開発時に実装） |
| **R9-1/9-2/9-3**: 異常終了時のログ自動クローズ処理 | Proposed Changes > `task_log.go` |

---

## Proposed Changes

### shared/libs/go/tasklog

#### [NEW] [agent_log_entry_test.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/shared/libs/go/tasklog/agent_log_entry_test.go)
*   **Description**: `AgentLogEntry` のフィールド設定、ファクトリ関数、Functional Option、JSONシリアライズの正常性を検証するテスト。
*   **Technical Design**:
    ```go
    package tasklog

    import (
        "encoding/json"
        "testing"
    )

    func TestNewAgentLogEntry(t *testing.T) { ... }
    func TestNewAgentLogSendEntry(t *testing.T) { ... }
    func TestNewAgentLogEndEntry(t *testing.T) { ... }
    func TestAgentLogEntry_JSONSerialization(t *testing.T) { ... }
    ```
*   **Logic**:
    *   `NewAgentLogEntry` でデフォルトの `Kind` が `"text"` であること、`Phase` が `"begin"` であること。
    *   Functional Option を使用して `Kind`, `Location`, `ParentLogID`, `TaskNodeID` が正しく上書きされること。
    *   `NewAgentLogSendEntry` と `NewAgentLogEndEntry` で、指定された `ID`, `AgentID` が適用され、`Phase` がそれぞれ `"send"`, `"end"` に設定されること。
    *   JSONシリアライズした際、`parentLogId` などのキー名がキャメルケース（または仕様書の定義通り `"parentLogId"`）でシリアライズされること。

#### [NEW] [log_stack_test.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/shared/libs/go/tasklog/log_stack_test.go)
*   **Description**: `LogStack` の Push/Pop/CurrentParentID 動作と、並行アクセス時のスレッド安全性を検証するテスト。
*   **Technical Design**:
    ```go
    package tasklog

    import (
        "sync"
        "testing"
    )

    func TestLogStack_BasicOperations(t *testing.T) { ... }
    func TestLogStack_Concurrency(t *testing.T) { ... }
    ```
*   **Logic**:
    *   空スタックから `CurrentParentID` を呼ぶと `""` が返ること。
    *   `Push` した後に `CurrentParentID` がそのIDを返し、`Pop` すると元の状態に戻ること。
    *   複数のゴルーチンから同時に `Push`/`Pop`/`CurrentParentID` を呼び出しても、レースコンディションやデッドロックが発生しないこと。

#### [NEW] [task_log_test.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/shared/libs/go/tasklog/task_log_test.go)
*   **Description**: `TaskLog` の追加履歴管理と、異常終了時の自動クローズロジックの動作検証。
*   **Technical Design**:
    ```go
    package tasklog

    import (
        "testing"
    )

    func TestTaskLog_AddAndClone(t *testing.T) { ... }
    func TestTaskLog_AbnormalTerminationAutoClose(t *testing.T) { ... }
    ```
*   **Logic**:
    *   `Add` したエントリが履歴に保持され、`Clone` で正しく複製されること。
    *   `TaskLog.Add` に `TerminatedEntry` (または異常終了を表すエントリ) が渡された場合、それより前に登録された未クローズの `AgentLogEntry`（`IsComplete: false`）をすべて検出し、`IsComplete = true` へ自動的にクローズ処理し、末尾に `"[auto-closed: abnormal termination]"` メッセージを追加すること。

#### [NEW] [entry.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/shared/libs/go/tasklog/entry.go)
*   **Description**: すべてのログエントリのベースとなるインターフェースおよび共通構造体を定義。
*   **Technical Design**:
    ```go
    package tasklog

    import "time"

    // Entry is a marker interface for task log entries.
    type Entry interface {
        Timestamp() time.Time
        Type() string
    }

    // BaseEntry provides a common base for log entries.
    type BaseEntry struct {
        ID        string    `json:"id"`
        Time      time.Time `json:"time"`
        EntryType string    `json:"entryType"`
    }

    func (b BaseEntry) Timestamp() time.Time {
        return b.Time
    }

    func (b BaseEntry) Type() string {
        return b.EntryType
    }
    ```

#### [NEW] [agent_log_entry.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/shared/libs/go/tasklog/agent_log_entry.go)
*   **Description**: `AgentLogEntry` 構造体、Functional Option群、およびファクトリ関数の実装。
*   **Technical Design**:
    ```go
    package tasklog

    import (
        "time"
        "github.com/google/uuid"
    )

    const AgentLogEntryType = "AGENT_LOG"

    type AgentLogEntry struct {
        BaseEntry

        Body        string `json:"body"`
        Kind        string `json:"kind"`
        Location    string `json:"location,omitempty"`
        ParentLogID string `json:"parentLogId,omitempty"`
        TaskNodeID  string `json:"taskNodeId,omitempty"`
        AgentID     string `json:"agentId"`
        RunID       string `json:"runId,omitempty"`
        IsComplete  bool   `json:"isComplete"`
        Phase       string `json:"phase"`
    }

    type AgentLogOption func(*AgentLogEntry)

    func WithKind(kind string) AgentLogOption { ... }
    func WithLocation(location string) AgentLogOption { ... }
    func WithParentLogID(parentLogID string) AgentLogOption { ... }
    func WithTaskNodeID(taskNodeID string) AgentLogOption { ... }

    func NewAgentLogEntry(agentID string, opts ...AgentLogOption) *AgentLogEntry { ... }
    func NewAgentLogSendEntry(logID, agentID, body string) *AgentLogEntry { ... }
    func NewAgentLogEndEntry(logID, agentID string) *AgentLogEntry { ... }
    ```
*   **Logic**:
    *   `NewAgentLogEntry` で、ランダムなUUIDを発行し、タイムスタンプを現在時刻に設定。デフォルトの `Kind = "text"`, `Phase = "begin"` とする。
    *   `NewAgentLogSendEntry` で、指定された `logID` を `ID` にセット。`Phase = "send"`、タイムスタンプは現在時刻とする。
    *   `NewAgentLogEndEntry` で、`Phase = "end"`, `IsComplete = true` に設定。タイムスタンプは現在時刻とする。

#### [NEW] [entry_types.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/shared/libs/go/tasklog/entry_types.go)
*   **Description**: `MovementEntry`, `TerminatedEntry`, `ErrorEntry` などの共通データモデルの定義。
*   **Technical Design**:
    ```go
    package tasklog

    import (
        "fmt"
        "time"
        "github.com/google/uuid"
    )

    const (
        MovementEntryType   = "MOVEMENT"
        TerminatedEntryType = "TERMINATED"
        ErrorEntryType      = "ERROR"
    )

    type MovementEntry struct {
        BaseEntry
        AgentID    string `json:"agentId"`
        NodeID     string `json:"nodeId"`
        FromNodeID string `json:"fromNodeId,omitempty"`
        Body       string `json:"body"`
    }

    func FormatMovementBody(nodeID string) string {
        return fmt.Sprintf("Agent moved to node '%s'", nodeID)
    }

    func NewMovementEntry(agentID, nodeID string) *MovementEntry { ... }

    type TerminatedEntry struct {
        BaseEntry
        AgentID string `json:"agentId"`
        Reason  string `json:"reason"`
        Body    string `json:"body"`
    }

    func FormatTerminatedBody(reason string) string {
        if reason == "" {
            return "Agent terminated"
        }
        return fmt.Sprintf("Agent terminated: %s", reason)
    }

    func NewTerminatedEntry(agentID, reason string) *TerminatedEntry { ... }

    type ErrorEntry struct {
        BaseEntry
        AgentID string `json:"agentId"`
        Message string `json:"message"`
        Body    string `json:"body"`
    }

    func NewErrorEntry(agentID, message string) *ErrorEntry { ... }
    ```

#### [NEW] [log_stack.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/shared/libs/go/tasklog/log_stack.go)
*   **Description**: 親子関係ログのスタック管理用スレッドセーフ構造体の実装。
*   **Technical Design**:
    ```go
    package tasklog

    import "sync"

    type LogStack struct {
        mu    sync.Mutex
        stack []string
    }

    func (s *LogStack) CurrentParentID() string { ... }
    func (s *LogStack) Push(logID string) { ... }
    func (s *LogStack) Pop() string { ... }
    ```
*   **Logic**:
    *   `mu` で排他制御を行う。
    *   `CurrentParentID` は `stack` が空なら `""` を返し、要素があれば最後の値を参照して返す。
    *   `Pop` は最後の要素を取り除いて返し、空なら `""` を返す。

#### [NEW] [task_log.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/shared/libs/go/tasklog/task_log.go)
*   **Description**: タスク実行セッションログの履歴保持および自動クローズ監視の実装。
*   **Technical Design**:
    ```go
    package tasklog

    import "sync"

    type TaskLog struct {
        mu      sync.RWMutex
        History []Entry
        onEntry func(Entry)
    }

    func New() *TaskLog { ... }
    func (l *TaskLog) SetOnEntry(f func(Entry)) { ... }
    func (l *TaskLog) Add(e Entry) { ... }
    func (l *TaskLog) Entries() []Entry { ... }
    func (l *TaskLog) Clone() *TaskLog { ... }
    ```
*   **Logic**:
    *   `Add` の際、渡された `e` が `TerminatedEntry` である場合、履歴内にあるすべての `AgentLogEntry` を走査し、`IsComplete: false` であるものを検出。
    *   検出された未完了エントリに対し、`IsComplete = true` に設定し、`Body = Body + "\n[auto-closed: abnormal termination]"` に更新する。

---

### tests

#### [NEW] [tasklog_integration_test.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/tests/tasklog_integration_test.go)
*   **Description**: 実際に `tasklog` パッケージを使用し、並行して複数のエージェントがログをストリーミング生成・終了する際の親子関係（LogStackによる `parentLogId` 追跡）と、異常終了検知時の自動クローズ機能が総合的に正しく機能するかを検証する。
*   **Technical Design**:
    ```go
    //go:build integration
    package tests

    import (
        "testing"
        "time"
        "github.com/axsh/hag/tasklog"
    )

    func TestIntegration_HierarchicalLogStreaming(t *testing.T) { ... }
    func TestIntegration_AbnormalTerminationAutoClose(t *testing.T) { ... }
    ```
*   **Logic**:
    *   **TestIntegration_HierarchicalLogStreaming**:
        *   `TaskLog` を作成し、ログを追記する。
        *   ルートログ（"begin", kind: "text"）を作成し `Push`。
        *   子ログ（"begin", kind: "thinking", parent: ルートID）を作成し `Push`。
        *   子ログの中身を `"send"` で送信。
        *   子ログを `Pop` し、`"end"` を送信。
        *   ツリー構造が正しく構築できる構成になっているか（各エントリの `ParentLogID` の連動関係）を履歴からアサート。
    *   **TestIntegration_AbnormalTerminationAutoClose**:
        *   いくつかのログエントリ（一部未完了の `IsComplete: false`）が存在する状態で `TerminatedEntry` を `Add` する。
        *   `TaskLog` の履歴内の未クローズログが自動的に `IsComplete: true` に更新され、補完メッセージが付与されていることを検証する。

---

## Step-by-Step Implementation Guide

1.  **[TDD Pre-check / Test Skeleton Creation]**:
    *   `shared/libs/go` 配下に `tasklog` ディレクトリを作成。
    *   `shared/libs/go/tasklog/agent_log_entry_test.go` を作成し、アサーションを含む空のテスト関数を追加。
    *   `shared/libs/go/tasklog/log_stack_test.go` を作成し、空のテスト関数を追加。
    *   `shared/libs/go/tasklog/task_log_test.go` を作成し、空のテスト関数を追加。
    *   `tests/tasklog_integration_test.go` を作成し、統合テスト用のスケルトンを追加。
    *   `./scripts/process/build.sh` を実行して、ビルドとテストが（空なので）パスするか失敗することを確認。

2.  **[Implement base types and structures]**:
    *   `shared/libs/go/tasklog/entry.go` を作成し、`Entry` インターフェースと `BaseEntry` 構造体を実装。
    *   `shared/libs/go/tasklog/entry_types.go` を作成し、`MovementEntry`, `TerminatedEntry`, `ErrorEntry` を実装。

3.  **[Implement AgentLogEntry & options]**:
    *   `shared/libs/go/tasklog/agent_log_entry.go` を作成し、`AgentLogEntry` 構造体と Functional Option、各種 `New...` ファクトリ関数を実装。
    *   `./scripts/process/build.sh` を実行し、`agent_log_entry_test.go` を修正して実装を検証。

4.  **[Implement LogStack]**:
    *   `shared/libs/go/tasklog/log_stack.go` を作成し、`LogStack` スレッドセーフスタックを実装。
    *   `./scripts/process/build.sh` を実行し、`log_stack_test.go` のテストを追加・パスさせて実装を検証。

5.  **[Implement TaskLog & Auto Close Logic]**:
    *   `shared/libs/go/tasklog/task_log.go` を作成し、`TaskLog` 構造体と、`Add` 時の異常終了自動クローズロジックを実装。
    *   `./scripts/process/build.sh` を実行し、`task_log_test.go` のテストを追加・パスさせて実装を検証。

6.  **[Implement and Run Integration Tests]**:
    *   `tests/tasklog_integration_test.go` のテストケースを具体的に実装。
    *   `./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestIntegration_"` を実行して、統合シナリオが期待通り動作することを確認。

7.  **[Comprehensive Verdict & Verdict Record]**:
    *   すべての単体テストおよび統合テストが成功したことを確認し、本計画の末尾にある総合判定を実施・記録する。

---

## Verification Plan

### Automated Verification

#### 1. Build & Unit Tests
Goのコンパイル確認と単体テスト（`tasklog` パッケージの全テスト）の実行。
```bash
./scripts/process/build.sh
```

#### 2. Integration Tests
階層化ログおよび異常終了時の自動クローズ機能の統合シナリオテストを実行。
```bash
./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestIntegration_"
```
*   **Log Verification**: テスト実行時に `AgentLogEntry` の親子階層 ID と `IsComplete` 属性、および補完テキストの自動マージが期待通りアサートされることを標準出力から確認します。

---

## Documentation

本計画での新規パッケージ `tasklog` 追加に伴い、パッケージ構成や影響度をドキュメントに更新します。

#### [MODIFY] [prompts/phases/README.md](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/prompts/phases/README.md)
*   **更新内容**: `shared/libs/go/tasklog` が追加され、階層化ログのバックエンドモデル層が開発完了したことを示す構成の更新。

---

## テスト設計のセルフレビュー (Testing Self-Review)

### 1. 網羅性の検証
- 階層化ログの必須データモデル、3つのPhase、表示 Kind、親子関係管理（LogStack）、異常終了時処理（TaskLogによる自動クローズ）という、仕様書のバックエンド全要件を対象としてテストケースを網羅しています。

### 2. 証拠の十分性
- 単なるエラーチェックではなく、生成された `AgentLogEntry` のJSONへのシリアライズ結果のフィールド値確認、並行アクセス時のスタック状態の整合性、履歴追加時の異常終了時自動クローズ後の属性値書き換えとメッセージ追加をアサートしており、動作している証拠として十分です。

### 3. 迂回・抜け道の排除
- テストはモックを使わず、実際の `TaskLog` と `AgentLogEntry` を使用してメモリ上でリアルなデータフローを通します。これにより、予期しないロジックパスによる「偽の成功」を防止します。

### 4. 依存関係の整合性
- ボトムアップ順序（`BaseEntry/Entry` -> `AgentLogEntry` -> `LogStack` -> `TaskLog` と統合テスト）に従って、小さな構成単位から順次テストを組み立て、最終的に結合して全体の動作を保証します。

---

## 総合判定プロセス（全テスト完了後に実施）

### 総合判定結果

**判定**: ✅ 動作確認完了

#### テスト結果サマリ
- 全テスト数: 10 件 (単体テスト 8 件, 統合テスト 2 件)
- 成功: 10 件
- 失敗: 0 件
- 事実上スキップ: 0 件

#### チェック項目の結果
| # | チェック項目 | 結果 | 備考 |
|---|------------|------|------|
| 1 | スキップされたテスト | ✅ | スキップされたテストはありません。 |
| 2 | 部分的なエラー | ✅ | テストログ内にエラー・警告はありません。 |
| 3 | 迂回処理による偽成功 | ✅ | 実際に tasklog パッケージの構造体が使用され、正常なアサーションが実行されています。 |
| 4 | アダプタ・コンフィグの誤適用 | ✅ | 意図したデータモデルが期待通り機能しています。 |
| 5 | テスト間の依存・順序問題 | ✅ | すべて独立して実行可能です。 |
| 6 | カバレッジの妥当性 | ✅ | 仕様書にあるすべてのバックエンド要件をカバーしています。 |
| 7 | 外部システムの状態 | ✅ | メモリ上でのログ検証のため、特別な外部依存はなく安定しています。 |

#### 判定理由
設計したすべての単体テストおよび統合テスト（計10ケース）が正常にビルドされ、PASSしました。異常終了時の自動クローズ処理（メッセージ補完）およびLogStackを用いた親子階層追跡も期待通りのロジックで動作しており、動作確認完了と判定します。
