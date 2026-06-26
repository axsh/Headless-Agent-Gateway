# 060-Compaction-UserStart-Guarantee

> **Source Specification**: prompts/phases/000-foundation/branches/feat-llm-backend/ideas/049-Compaction-UserStart-Guarantee.md

## Goal Description

コンパクション後の `recentMessages` が必ず `user` ロールのメッセージで始まることを保証する境界調整ロジックを追加する。これにより、Gemini API の「function_call は user ターンまたは function_response ターンの直後でなければならない」制約に違反する HTTP 400 エラーを解消する。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: コンパクション境界の user 先頭保証 | Proposed Changes > compaction.go -- `adjustBoundaryForUserStart` 関数, `Compact` 修正 |
| R1: boundary が 0 に達した場合のスキップ | Proposed Changes > compaction.go -- `Compact` の `boundary == 0` ガード |
| R2: バリデーション強化 (system 後に assistant(tool_calls) 来ない保証) | Proposed Changes > compaction.go -- `validateMessageOrdering` 関数 |
| R2: 違反時のフォールバック | Proposed Changes > compaction.go -- `Compact` のバリデーション呼び出し |

R3 (全プロバイダ安全なメッセージ正規化) は任意要件であり、今回は R1/R2 の実装によってプロバイダ非依存の安全なメッセージ順序が保証されるため、明示的なドキュメント化は先送りとする。

## Proposed Changes

### session パッケージ

#### [MODIFY] [compaction_test.go](file:///shared/libs/go/wayfinder/session/compaction_test.go)
*   **Description**: `adjustBoundaryForUserStart` と `validateMessageOrdering` のテストを TDD で先に追加する。
*   **Technical Design**:
    *   テーブル駆動テストで `adjustBoundaryForUserStart` のエッジケースを網羅
    *   `validateMessageOrdering` のバリデーションテスト
    *   `Compact` 全体での user 先頭保証の統合テスト
*   **Logic**:

    **Test 1: `TestAdjustBoundaryForUserStart`** (テーブル駆動)

    ```go
    func TestAdjustBoundaryForUserStart(t *testing.T) {
        tests := []struct {
            name     string
            msgs     []Message
            boundary int
            want     int
        }{
            {
                name:     "boundary at zero returns zero",
                msgs:     []Message{{Role: "user"}},
                boundary: 0,
                want:     0,
            },
            {
                name:     "negative boundary returns zero",
                msgs:     []Message{{Role: "user"}},
                boundary: -1,
                want:     0,
            },
            {
                name:     "boundary beyond length unchanged",
                msgs:     []Message{{Role: "user"}, {Role: "assistant"}},
                boundary: 5,
                want:     5,
            },
            {
                name: "already user no adjustment",
                msgs: []Message{
                    {Role: "assistant", Content: "old"},
                    {Role: "user", Content: "new"},
                    {Role: "assistant", Content: "resp"},
                },
                boundary: 1,
                want:     1,
            },
            {
                name: "assistant with tool calls shifts to user",
                msgs: []Message{
                    {Role: "user", Content: "prompt"},
                    {Role: "assistant", Content: "do tool", ToolCalls: []ToolCallRecord{{ID: "tc1", Name: "edit"}}},
                    {Role: "tool", Content: "result", ToolCallID: "tc1"},
                    {Role: "assistant", Content: "done"},
                    {Role: "user", Content: "next"},
                },
                boundary: 1, // starts at assistant(tool_calls)
                want:     0, // shifts back to user at index 0... but 0 with user -> returns 0
            },
            {
                name: "assistant without tool calls shifts to previous user",
                msgs: []Message{
                    {Role: "user", Content: "p1"},
                    {Role: "assistant", Content: "r1"},
                    {Role: "user", Content: "p2"},
                    {Role: "assistant", Content: "r2"},
                    {Role: "assistant", Content: "r3"}, // boundary here
                    {Role: "user", Content: "p3"},
                },
                boundary: 4,
                want:     3, // shifts to user at index 3... wait, index 3 is assistant. index 2 is user.
            },
            {
                name: "no user in messages returns zero",
                msgs: []Message{
                    {Role: "assistant", Content: "r1"},
                    {Role: "assistant", Content: "r2"},
                    {Role: "assistant", Content: "r3"},
                },
                boundary: 1,
                want:     0,
            },
        }
        for _, tt := range tests {
            t.Run(tt.name, func(t *testing.T) {
                got := adjustBoundaryForUserStart(tt.msgs, tt.boundary)
                if got != tt.want {
                    t.Errorf("adjustBoundaryForUserStart() = %d, want %d", got, tt.want)
                }
            })
        }
    }
    ```

    **注意**: テストケース "assistant without tool calls shifts to previous user" の `want` は `2` (index 2 の user) となる。境界の計算を事前に検証すること。

    **Test 2: `TestValidateMessageOrdering`** (テーブル駆動)

    ```go
    func TestValidateMessageOrdering(t *testing.T) {
        tests := []struct {
            name string
            msgs []Message
            want bool
        }{
            {
                name: "system then user is valid",
                msgs: []Message{
                    {Role: "system", Content: "summary", Pinned: true},
                    {Role: "user", Content: "hello"},
                    {Role: "assistant", Content: "hi"},
                },
                want: true,
            },
            {
                name: "system then assistant with tool calls is invalid",
                msgs: []Message{
                    {Role: "system", Content: "summary", Pinned: true},
                    {Role: "assistant", Content: "do tool", ToolCalls: []ToolCallRecord{{ID: "tc1", Name: "edit"}}},
                    {Role: "tool", Content: "result", ToolCallID: "tc1"},
                },
                want: false,
            },
            {
                name: "system then plain assistant is invalid",
                msgs: []Message{
                    {Role: "system", Content: "summary", Pinned: true},
                    {Role: "assistant", Content: "response"},
                },
                want: false,
            },
            {
                name: "pinned only is valid (edge case)",
                msgs: []Message{
                    {Role: "system", Content: "prompt", Pinned: true},
                },
                want: true,
            },
            {
                name: "empty messages is valid",
                msgs: []Message{},
                want: true,
            },
            {
                name: "no pinned messages user first is valid",
                msgs: []Message{
                    {Role: "user", Content: "hello"},
                    {Role: "assistant", Content: "hi"},
                },
                want: true,
            },
        }
        for _, tt := range tests {
            t.Run(tt.name, func(t *testing.T) {
                got := validateMessageOrdering(tt.msgs)
                if got != tt.want {
                    t.Errorf("validateMessageOrdering() = %v, want %v", got, tt.want)
                }
            })
        }
    }
    ```

    **Test 3: `TestCompact_RecentMessagesStartWithUser`** (統合テスト)

    ```go
    func TestCompact_RecentMessagesStartWithUser(t *testing.T) {
        // Scenario: boundary would naturally fall on assistant(tool_calls).
        // After adjustment, recentMessages must start with "user".
        cfg := &CompactionConfig{MaxTurns: 8, MaxContentLen: 5000}

        msgs := []Message{
            {Role: "user", Content: "p1"},
            {Role: "assistant", Content: "r1"},
            {Role: "user", Content: "p2"},
            {Role: "assistant", Content: "r2"},
            {Role: "user", Content: "p3"},
            {Role: "assistant", Content: "do tool", ToolCalls: []ToolCallRecord{{ID: "tc1", Name: "cmd"}}},
            {Role: "tool", Content: "output", ToolCallID: "tc1"},
            {Role: "assistant", Content: "done"},
            {Role: "user", Content: "p4"},
            {Role: "assistant", Content: "r4"},
            {Role: "user", Content: "p5"},
            {Role: "assistant", Content: "r5"},
        }

        summarizer := func(oldMsgs []Message) (string, error) {
            return "Summary", nil
        }

        result, err := Compact(msgs, cfg, summarizer)
        if err != nil {
            t.Fatalf("Compact failed: %v", err)
        }

        // Find first non-pinned, non-system message.
        for _, m := range result {
            if m.Pinned || m.Role == "system" {
                continue
            }
            if m.Role != "user" {
                t.Errorf("first non-system message role = %q, want %q", m.Role, "user")
                for i, msg := range result {
                    t.Logf("  [%d] role=%s pinned=%v content=%q", i, msg.Role, msg.Pinned, msg.Content[:min(len(msg.Content), 30)])
                }
            }
            break
        }

        // Also verify tool pair integrity is maintained.
        if !validateToolPairIntegrity(result) {
            t.Error("tool pair integrity broken after compaction")
        }
    }
    ```

    **Test 4: `TestCompact_BoundaryReachesZero_SkipsCompaction`**

    ```go
    func TestCompact_BoundaryReachesZero_SkipsCompaction(t *testing.T) {
        // All messages are assistant/tool with no user -> boundary reaches 0 -> skip compaction.
        cfg := &CompactionConfig{MaxTurns: 2, MaxContentLen: 5000}

        msgs := []Message{
            {Role: "assistant", Content: "r1", ToolCalls: []ToolCallRecord{{ID: "tc1", Name: "cmd"}}},
            {Role: "tool", Content: "output", ToolCallID: "tc1"},
            {Role: "assistant", Content: "r2", ToolCalls: []ToolCallRecord{{ID: "tc2", Name: "cmd"}}},
            {Role: "tool", Content: "output2", ToolCallID: "tc2"},
            {Role: "assistant", Content: "done"},
        }

        summarizer := func(oldMsgs []Message) (string, error) {
            return "Summary", nil
        }

        result, err := Compact(msgs, cfg, summarizer)
        if err != nil {
            t.Fatalf("Compact failed: %v", err)
        }

        // Should return original messages unchanged (compaction skipped).
        if len(result) != len(msgs) {
            t.Errorf("expected compaction to be skipped, got %d messages instead of %d", len(result), len(msgs))
        }
    }
    ```

---

#### [MODIFY] [compaction.go](file:///shared/libs/go/wayfinder/session/compaction.go)
*   **Description**: `adjustBoundaryForUserStart` 関数の追加、`Compact` への統合、`validateMessageOrdering` の追加
*   **Technical Design**:

    **新規関数 1: `adjustBoundaryForUserStart`**

    ```go
    // adjustBoundaryForUserStart adjusts the boundary so that
    // recentMessages starts with a "user" role message.
    // This prevents system(summary) -> assistant(tool_calls) sequences
    // that violate Gemini's function call ordering constraint.
    func adjustBoundaryForUserStart(unpinned []Message, boundary int) int {
        if boundary <= 0 {
            return 0
        }
        if boundary >= len(unpinned) {
            return boundary
        }

        // If the boundary message is already "user", no adjustment needed.
        if unpinned[boundary].Role == "user" {
            return boundary
        }

        // Shift backward until we find a "user" message.
        for boundary > 0 && unpinned[boundary].Role != "user" {
            boundary--
        }

        // If we reached index 0 and it's not "user", compaction should be skipped.
        if boundary == 0 && unpinned[0].Role != "user" {
            return 0
        }

        return boundary
    }
    ```

    **新規関数 2: `validateMessageOrdering`**

    ```go
    // validateMessageOrdering checks that the first non-pinned, non-system
    // message after compaction summary is a "user" role message.
    // This prevents Gemini API errors where function_call follows system message.
    func validateMessageOrdering(messages []Message) bool {
        for _, m := range messages {
            if m.Pinned || m.Role == "system" {
                continue
            }
            // First non-pinned, non-system message must be "user".
            return m.Role == "user"
        }
        return true // No non-pinned messages (edge case).
    }
    ```

    **既存関数修正: `Compact`**

    現在のコード (L56-L84):
    ```go
    boundary := len(unpinned) - windowSize
    boundary = adjustBoundaryForToolPairs(unpinned, boundary)
    ```

    修正後:
    ```go
    boundary := len(unpinned) - windowSize
    boundary = adjustBoundaryForToolPairs(unpinned, boundary)
    boundary = adjustBoundaryForUserStart(unpinned, boundary)

    // If boundary reached 0, all messages would be in recentMessages.
    // This means no compaction is possible, skip.
    if boundary == 0 {
        return messages, nil
    }
    ```

    バリデーション部分 (L80-L84):
    ```go
    // 現在:
    if !validateToolPairIntegrity(result) {
        return messages, nil
    }

    // 修正後:
    if !validateToolPairIntegrity(result) || !validateMessageOrdering(result) {
        return messages, nil
    }
    ```

## Step-by-Step Implementation Guide

> [!IMPORTANT]
> TDD アプローチ: テストを先に書き、失敗を確認してから実装する。

### Step 1: テスト追加 (TDD - Red)

1.  Edit `shared/libs/go/wayfinder/session/compaction_test.go`:
    - `TestAdjustBoundaryForUserStart` を追加 (テーブル駆動、7ケース)
    - `TestValidateMessageOrdering` を追加 (テーブル駆動、6ケース)
    - `TestCompact_RecentMessagesStartWithUser` を追加
    - `TestCompact_BoundaryReachesZero_SkipsCompaction` を追加
2.  テストがコンパイルエラーになることを確認 (`adjustBoundaryForUserStart`, `validateMessageOrdering` が未定義)

### Step 2: adjustBoundaryForUserStart 実装 (TDD - Green)

1.  Edit `shared/libs/go/wayfinder/session/compaction.go`:
    - `adjustBoundaryForUserStart` 関数を追加 (既存の `adjustBoundaryForToolPairs` の直後に配置)
2.  `TestAdjustBoundaryForUserStart` が全ケース成功することを確認

### Step 3: validateMessageOrdering 実装 (TDD - Green)

1.  Edit `shared/libs/go/wayfinder/session/compaction.go`:
    - `validateMessageOrdering` 関数を追加 (既存の `validateToolPairIntegrity` の直後に配置)
2.  `TestValidateMessageOrdering` が全ケース成功することを確認

### Step 4: Compact 統合 (TDD - Green)

1.  Edit `shared/libs/go/wayfinder/session/compaction.go`:
    - `Compact` 関数の boundary 計算後に `adjustBoundaryForUserStart` を呼び出す
    - `boundary == 0` のガードを追加
    - バリデーション部分に `validateMessageOrdering` を追加
2.  `TestCompact_RecentMessagesStartWithUser` と `TestCompact_BoundaryReachesZero_SkipsCompaction` が成功することを確認

### Step 5: 全テスト実行と git commit

1.  `./scripts/process/build.sh` を実行し、全テスト成功を確認
2.  git commit: `fix: ensure compaction recentMessages starts with user message`

### Step 6: Verification Plan 実行

1.  `./scripts/process/build.sh` を実行
2.  `./scripts/process/integration_test.sh` を実行
3.  総合判定を実施

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```

2.  **Integration Tests**:
    ```bash
    ./scripts/process/integration_test.sh
    ```
    *   **Log Verification**: 全テストが PASS であること。環境依存の既存失敗 (model_profiles.yaml 不在等) 以外に新規失敗がないこと。

3.  **E2E Tests**:
    E2E テストの追加は不要。理由:
    - 本修正は純粋な内部ロジックの修正 (session パッケージの `Compact` 関数) であり、外部 API やサーバーインターフェースに変更がない
    - 既存の E2E テスト (`TestAgentServiceSessionLifecycle` 等) が正常にパスすることでリグレッションがないことを確認できる
    - 問題の再現には実際の Gemini API を使った長時間の WBS セッション (コンパクション発動まで) が必要であり、E2E テストでの再現は非現実的

### セルフレビュー結果

1.  **網羅性**: R1 (boundary 調整) と R2 (バリデーション) の両方に対してテストが設計されており、正常系・異常系・エッジケースを網羅している。「これらのテストが全て成功すれば、コンパクション後のメッセージ順序が Gemini 制約を満たす」と言える。
2.  **証拠の十分性**: `TestCompact_RecentMessagesStartWithUser` は最終結果のメッセージ列を走査して先頭が `user` であることを直接検証しており、「エラーが出ない」レベルではなく値の検証を行っている。
3.  **迂回排除**: `boundary == 0` のケースでは、コンパクションがスキップされて元のメッセージが返ることを検証しており、フォールバックパスも検証している。
4.  **依存関係**: `adjustBoundaryForUserStart` -> `Compact` の順でボトムアップに設計されている。

### 総合判定プロセス

全テスト完了後、testing-rules.md 12.2 のチェック項目 (スキップ有無、部分エラー、迂回処理、アダプタ適用、順序依存、カバレッジ、外部システム) を確認し、総合判定を walkthrough に記録する。

## Documentation

本修正は内部ロジックの修正であり、既存の仕様書やドキュメントへの更新は不要。
