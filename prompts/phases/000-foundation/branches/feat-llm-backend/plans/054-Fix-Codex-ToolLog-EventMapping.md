# 054-Fix-Codex-ToolLog-EventMapping

> **Source Specification**: [043-Fix-Codex-ToolLog-EventMapping.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/043-Fix-Codex-ToolLog-EventMapping.md)

## Goal Description

Codex CLI (`codex exec --json`) の `function_call_output` イベントが誤って `EventResult`(何も表示しない型) にマッピングされている問題を修正する。`EventToolResult` に正しくマッピングし、ツール実行結果のコンテンツも `Content` フィールドに設定することで、`ternctl` 出力に `[Tool: ...]` / `[Tool Result] ...` が表示されるようにする。また、未知のイベントタイプを TRACE ログに記録する機能も追加する。

## User Review Required

> [!IMPORTANT]
> **R1 (Codex CLI 実地確認) について**: 実装計画の Step 1 で `codex exec --json` の実際の出力を確認する。もし想定と異なるイベント構造が判明した場合は、Step 2 以降の実装を R1 の結果に合わせて調整する。この時点で計画からの逸脱が大きい場合はユーザーに報告する。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: Codex CLI の exec --json JSONL出力フォーマットの実地確認 | Step-by-Step > Step 1 |
| R2: function_call_output のイベントマッピング修正 | Proposed Changes > protocol.go |
| R3: 未知のイベントタイプへの対応強化 | Proposed Changes > process.go |
| R4: 単体テストの追加・修正 | Proposed Changes > protocol_test.go |
| O1: message タイプ内のツール情報パース拡充 | Step-by-Step > Step 2 (R1結果に依存) |

## Proposed Changes

### codex パッケージ (shared/libs/go/codingagent/codex)

#### [MODIFY] [protocol_test.go](file://shared/libs/go/codingagent/codex/protocol_test.go)
*   **Description**: `function_call_output` の既存テスト期待値を修正し、Content 検証テストを追加する。未知イベントのテストも追加。
*   **Technical Design**:
    *   既存テストはないため、新規テスト関数を追加する。
    *   テーブル駆動テストは使用せず、既存テストの記法に合わせた個別テスト関数を追加する。
*   **Logic**:

    **新規テスト1: `TestParseExecEvent_FunctionCallOutput`**
    ```go
    func TestParseExecEvent_FunctionCallOutput(t *testing.T) {
        line := `{"type":"function_call_output","output":"drwxr-xr-x 5 user group 160 Jun 12 test\n"}`
        ev := codex.ParseExecEvent(line)
        if ev == nil {
            t.Fatal("expected non-nil event")
        }
        if ev.Type != codingagent.EventToolResult {
            t.Errorf("type = %q, want %q", ev.Type, codingagent.EventToolResult)
        }
        if ev.Content != "drwxr-xr-x 5 user group 160 Jun 12 test\n" {
            t.Errorf("content = %q, want directory listing", ev.Content)
        }
    }
    ```

    **新規テスト2: `TestParseExecEvent_FunctionCallOutput_EmptyOutput`**
    ```go
    func TestParseExecEvent_FunctionCallOutput_EmptyOutput(t *testing.T) {
        line := `{"type":"function_call_output"}`
        ev := codex.ParseExecEvent(line)
        if ev == nil {
            t.Fatal("expected non-nil event")
        }
        if ev.Type != codingagent.EventToolResult {
            t.Errorf("type = %q, want %q", ev.Type, codingagent.EventToolResult)
        }
        // Content should be empty but event type should still be EventToolResult
        if ev.Content != "" {
            t.Errorf("content = %q, want empty", ev.Content)
        }
    }
    ```

    **新規テスト3: `TestParseExecEvent_UnknownType`**
    ```go
    func TestParseExecEvent_UnknownType(t *testing.T) {
        line := `{"type":"some.future.event","data":"hello"}`
        ev := codex.ParseExecEvent(line)
        if ev != nil {
            t.Errorf("expected nil for unknown event type, got %+v", ev)
        }
    }
    ```

---

#### [MODIFY] [protocol.go](file://shared/libs/go/codingagent/codex/protocol.go)
*   **Description**: `function_call_output` ケースのマッピングを `EventResult` から `EventToolResult` に修正し、`output` フィールドを `Content` に設定する。
*   **Technical Design**:
    *   L99-101 の `function_call_output` ケースを変更
    *   `output` フィールドをパースするための匿名構造体を追加
*   **Logic**:

    修正前 (L99-101):
    ```go
    case "function_call_output":
        // Tool result
        return &codingagent.StreamEvent{Type: codingagent.EventResult}
    ```

    修正後:
    ```go
    case "function_call_output":
        // Tool result - parse the output content
        var out struct {
            Output string `json:"output"`
        }
        json.Unmarshal([]byte(line), &out)
        return &codingagent.StreamEvent{
            Type:    codingagent.EventToolResult,
            Content: out.Output,
        }
    ```

    変更の根拠:
    - `EventToolResult` を使うことで、クライアント側 `stream.go` の `Output()` メソッドが `[Tool Result] ...` を表示する
    - `Content` フィールドにツール実行結果を設定することで、TaskLog にも結果が記録される
    - `json.Unmarshal` のエラーは無視する (既存の `function_call` ケースと同じ方針)

---

#### [MODIFY] [process.go](file://shared/libs/go/codingagent/codex/process.go)
*   **Description**: JSONL 読み取り goroutine で、`ParseExecEvent` が `nil` を返した場合に TRACE ログを出力する。
*   **Technical Design**:
    *   L182-200 の goroutine 内で、`ev == nil` の場合に `log.Trace` を呼び出す
    *   `ParseExecEvent` 関数のシグネチャは変更しない (純粋関数のまま維持)
*   **Logic**:

    修正前 (L192-199):
    ```go
    ev := ParseExecEvent(line)
    if ev != nil {
        select {
        case ch <- *ev:
        case <-procCtx.Done():
            return
        }
    }
    ```

    修正後:
    ```go
    ev := ParseExecEvent(line)
    if ev != nil {
        select {
        case ch <- *ev:
        case <-procCtx.Done():
            return
        }
    } else {
        log.Trace("unhandled codex event type (ignored)", "line", line)
    }
    ```

    変更の根拠:
    - `ParseExecEvent` は `thread.started`, `turn.started` などの既知ライフサイクルイベントでも `nil` を返すが、それらは TRACE レベルでログに残すことで、将来の問題分析に役立つ
    - 既に L190 で `CLI stdout line` として全行を TRACE ログ出力しているため、これは補助的な情報として、`ParseExecEvent` で処理されなかったイベントを明示するもの

## Step-by-Step Implementation Guide

### Step 1: R1 - Codex CLI の実際の JSONL 出力を確認する [x]

*   `codex exec --json --dangerously-bypass-approvals-and-sandbox --ignore-user-config "please run 'pwd' command and report the result." 2>/dev/null` を実行する。
*   出力された JSONL の各行の `type` フィールドとフィールド構造を記録する。
*   特に `function_call` と `function_call_output` のイベントが存在するか、`output` フィールドの名前と型を確認する。
*   結果が想定と異なる場合は、Step 2 の実装内容を調整する。
*   結果を `tmp/codex_jsonl_output.txt` に保存する。

### Step 2: R4 - 単体テストの追加 (TDD: テスト先行) [x]

*   `shared/libs/go/codingagent/codex/protocol_test.go` に以下のテスト関数を追加:
    *   `TestParseExecEvent_FunctionCallOutput` - `function_call_output` が `EventToolResult` に変換され、`Content` に出力結果が設定されること
    *   `TestParseExecEvent_FunctionCallOutput_EmptyOutput` - `output` フィールドが無い場合でも `EventToolResult` が返ること
    *   `TestParseExecEvent_UnknownType` - 未知のイベントタイプで `nil` が返ること
*   ビルドスクリプトで単体テストを実行し、新規テストが失敗することを確認:
    ```bash
    ./scripts/process/build.sh --skip-frontend --skip-etc
    ```

### Step 3: R2 - function_call_output のマッピング修正 [x]

*   `shared/libs/go/codingagent/codex/protocol.go` の L99-101 を修正:
    *   `EventResult` -> `EventToolResult`
    *   `output` フィールドをパースして `Content` に設定
*   ビルドスクリプトで単体テストを実行し、全テストが成功することを確認:
    ```bash
    ./scripts/process/build.sh --skip-frontend --skip-etc
    ```

### Step 4: R3 - 未知イベントの TRACE ログ出力追加 [x]

*   `shared/libs/go/codingagent/codex/process.go` の L192-199 の `if ev != nil` ブロックに `else` 節を追加:
    *   `log.Trace("unhandled codex event type (ignored)", "line", line)` を出力
*   ビルドスクリプトで再度単体テストを確認:
    ```bash
    ./scripts/process/build.sh --skip-frontend --skip-etc
    ```

### Step 5: git commit [x]

*   変更をコミットする:
    ```bash
    git add shared/libs/go/codingagent/codex/protocol.go \
            shared/libs/go/codingagent/codex/protocol_test.go \
            shared/libs/go/codingagent/codex/process.go
    git commit -m "fix: map codex function_call_output to EventToolResult instead of EventResult"
    ```

### Step 6: 全体ビルド + 検証 [x]

*   全体ビルドを実行:
    ```bash
    ./scripts/process/build.sh
    ```
*   Codex E2E テスト (tool_use / tool_result イベント検証) を実行:
    ```bash
    ./scripts/process/integration_test.sh --specify "TestCodexE2E_FileCreation"
    ```
*   全テスト成功後、git push する。

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```
    *   `codex/protocol_test.go` の全テスト (既存 + 新規3件) が成功すること。

2.  **Integration Tests (Codex E2E)**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestCodexE2E_FileCreation"
    ```
    *   **Log Verification**: テスト出力の `event[N]: type=...` ログで `type=tool_use` および `type=tool_result` イベントが含まれていること。
    *   ファイルが作成され、セッションステータスが `completed` であること。

3.  **E2E Tests - 既存テストで十分な理由**:
    E2Eテストの新規追加は不要。理由: 既存の `TestCodexE2E_FileCreation` (`tests/codex_e2e_test.go`) がすでにSSEイベント (`EventToolUse`, `EventResult`) の存在を検証している。今回の修正により `function_call_output` が `EventToolResult` に変換されるようになるため、既存テストのイベントログで `tool_result` の出現を確認できる。ただし、既存テストの検証ロジックは `EventResult || EventText || EventToolUse` の存在のみをチェックしており、`EventToolResult` の明示的な検証はしていない。仕様書の検証シナリオ1 (ternctl でのツールログ表示) は手動確認で補完する。

    > [!NOTE]
    > 既存の `TestCodexE2E_FileCreation` は `hasResult` チェックで `EventResult` の存在を確認している。修正後は `function_call_output` が `EventToolResult` に変換されるため、`turn.completed` から生成される `EventResult` のみが `hasResult` を満たす。テストの成否には影響しないが、テストログに `tool_result` イベントが新たに出現するようになる。

### テスト項目のセルフレビュー

#### 11.3 観点チェックリスト

| # | 観点 | 確認状況 |
|---|------|----------|
| 1 | 正常系の動作確認 | `TestParseExecEvent_FunctionCallOutput`: output フィールドありの正常ケース |
| 2 | 異常系・境界値 | `TestParseExecEvent_FunctionCallOutput_EmptyOutput`: output フィールドなしの境界ケース |
| 3 | 外部連携の実動作 | `TestCodexE2E_FileCreation` (既存): 実際のCodex CLIとの連携 |
| 4 | データの一貫性 | `TestParseExecEvent_FunctionCallOutput`: Content フィールドの値の正確性 |
| 5 | 状態遷移の検証 | N/A (状態遷移なし、純粋関数の修正) |
| 6 | 設定・構成の反映 | N/A (設定変更なし) |
| 7 | 副作用の確認 | `TestParseExecEvent_UnknownType`: 未知イベントが nil を返すことの確認 (副作用なし) |

#### 11.4 セルフレビュー

1. **網羅性**: 正常系(出力あり)、境界値(出力なし)、未知イベントの3パターンをカバー。`function_call` (ToolUse) は既存テストで検証済み。全テスト成功でパース機能が正しく動作していると言える。
2. **証拠の十分性**: `EventToolResult` 型の検証と `Content` フィールドの値の一致検証を行っている。「型が変わった」だけでなく「正しい値が入っている」ことを確認している。
3. **迂回・抜け道の排除**: `ParseExecEvent` は純粋関数であり、入力に対して決定論的な出力を返す。テストは直接入力を与えて出力を検証するため、迂回の余地がない。
4. **依存関係の整合性**: `ParseExecEvent` (末端) -> `process.go` goroutine (呼び出し元) -> E2E の順でボトムアップ検証している。

### 総合判定プロセス

全テスト完了後、testing-rules.md 12.2 のチェック項目に従って総合判定を実施する。

## Documentation

本修正は内部実装のバグフィックスであり、外部仕様の変更はない。既存のドキュメント更新は不要。
