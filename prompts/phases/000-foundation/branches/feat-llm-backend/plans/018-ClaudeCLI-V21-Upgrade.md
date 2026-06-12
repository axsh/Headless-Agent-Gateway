# 018-ClaudeCLI-V21-Upgrade

> **Source Specification**: [012-ClaudeCLI-V21-Upgrade.md](file:///prompts/phases/000-foundation/branches/feat-llm-backend/ideas/012-ClaudeCLI-V21-Upgrade.md)

## Goal Description

Claude Code CLI v2.0.14 から v2.1.x への移行に伴い、`claudecode` パッケージのプロセス管理 (process.go)、プロトコルパーサー (protocol.go)、セッション設定 (options.go) を更新する。加えて、サーバ起動時の CLI バージョンチェック機能と README.md のバージョン要件記載を実施する。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: `--bare` フラグの追加 | Proposed Changes > process.go |
| R2: v2.1 JSON Lines プロトコル対応 | Proposed Changes > protocol.go |
| R3: `text` イベント抽出方法の修正 | Proposed Changes > protocol.go |
| R4: `--verbose` 必須 (変更不要) | 既に設定済み。テストで確認。 |
| R5: README.md にバージョン要件記載 | Proposed Changes > README.md |
| R6: `--max-turns` の設定 | Proposed Changes > options.go, process.go |
| R7: stdin 警告の抑制 | Proposed Changes > process.go |
| R8: サーバ起動時の CLI バージョンチェック | Proposed Changes > service.go |

## Proposed Changes

### codingagent パッケージ (共有ライブラリ)

---

#### [MODIFY] [process_test.go](file:///shared/libs/go/codingagent/claudecode/process_test.go)
*   **Description**: R1, R4, R6, R7 の新しいテストケースを追加
*   **Technical Design**:
    *   既存の `TestBuildArgs` テーブル駆動テストに新しいケースを追加
    *   TDD: テストを先に書いて失敗を確認してから実装する
*   **Logic**:
    *   `TestBuildArgs` の `tests` テーブルに以下を追加:

    ```go
    // R1: --bare フラグが必ず含まれる
    {
        name: "includes --bare flag",
        cfg:  &codingagent.SessionConfig{Prompt: "test"},
        contains: []string{"--bare"},
    },
    // R4: --verbose フラグが必ず含まれる
    {
        name: "includes --verbose flag",
        cfg:  &codingagent.SessionConfig{Prompt: "test"},
        contains: []string{"--verbose"},
    },
    // R6: MaxTurns が設定されている場合 --max-turns N が含まれる
    {
        name: "with max turns",
        cfg:  &codingagent.SessionConfig{Prompt: "test", MaxTurns: 200},
        contains: []string{"--max-turns", "200"},
    },
    // R6: MaxTurns が 0 の場合 --max-turns は含まれない
    {
        name:       "zero max turns omits flag",
        cfg:        &codingagent.SessionConfig{Prompt: "test"},
        notContain: "--max-turns",
    },
    ```

    *   テストケースに `notContain` フィールドを追加 (R6 の MaxTurns=0 ケースで必要):

    ```go
    tests := []struct {
        name       string
        cfg        *codingagent.SessionConfig
        contains   []string
        notContain string  // 新規追加
    }{...}

    // 検証ロジックに追加:
    if tt.notContain != "" {
        if strings.Contains(argsStr, tt.notContain) {
            t.Errorf("args %q should NOT contain %q", argsStr, tt.notContain)
        }
    }
    ```

---

#### [MODIFY] [protocol_test.go](file:///shared/libs/go/codingagent/claudecode/protocol_test.go)
*   **Description**: R2, R3 の v2.1 プロトコル対応テストを追加
*   **Technical Design**:
    *   v2.1 の実際の出力から取得した JSON サンプルを使用
    *   既存の v2.0 テスト (`TestParseJSONLinesEvent_StreamEvent`) は後方互換のため維持
*   **Logic**:

    ```go
    // R3: v2.1 形式の assistant/text ブロック
    func TestParseJSONLinesEvent_V21_TextBlock(t *testing.T) {
        input := `{"type":"assistant","message":{"content":[{"type":"text","text":"Hello! I'm here."}]}}`
        ev := claudecode.ParseJSONLinesEvent(input)
        if ev == nil {
            t.Fatal("expected non-nil event")
        }
        if ev.Type != codingagent.EventText {
            t.Errorf("Type = %v, want EventText", ev.Type)
        }
        if ev.Content != "Hello! I'm here." {
            t.Errorf("Content = %q, want %q", ev.Content, "Hello! I'm here.")
        }
    }

    // R2: system/thinking_tokens は無視される
    func TestParseJSONLinesEvent_V21_ThinkingTokens(t *testing.T) {
        input := `{"type":"system","subtype":"thinking_tokens","estimated_tokens":200,"estimated_tokens_delta":24}`
        ev := claudecode.ParseJSONLinesEvent(input)
        if ev != nil {
            t.Errorf("expected nil for thinking_tokens, got %+v", ev)
        }
    }

    // R2: assistant/thinking ブロックはエラーなく処理される
    func TestParseJSONLinesEvent_V21_ThinkingBlock(t *testing.T) {
        input := `{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"reasoning...","signature":"sig123"}]}}`
        ev := claudecode.ParseJSONLinesEvent(input)
        // thinking ブロックのみの場合は nil を返す (text も tool_use もない)
        if ev != nil {
            t.Errorf("expected nil for thinking-only message, got %+v", ev)
        }
    }

    // R2: v2.1 拡張フィールド付き result
    func TestParseJSONLinesEvent_V21_Result(t *testing.T) {
        input := `{"type":"result","subtype":"success","is_error":false,"duration_ms":6354,"num_turns":1,"result":"Hello!","stop_reason":"end_turn","total_cost_usd":0.01,"terminal_reason":"completed"}`
        ev := claudecode.ParseJSONLinesEvent(input)
        if ev == nil {
            t.Fatal("expected non-nil event")
        }
        if ev.Type != codingagent.EventResult {
            t.Errorf("Type = %v, want EventResult", ev.Type)
        }
    }

    // R3: v2.1 で text と tool_use が混在する assistant メッセージ
    func TestParseJSONLinesEvent_V21_TextAndToolUse(t *testing.T) {
        // tool_use が text より先にある場合: tool_use を返す (既存動作と一致)
        input := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"path":"a.go"}},{"type":"text","text":"ok"}]}}`
        ev := claudecode.ParseJSONLinesEvent(input)
        if ev == nil {
            t.Fatal("expected non-nil event")
        }
        if ev.Type != codingagent.EventToolUse {
            t.Errorf("Type = %v, want EventToolUse", ev.Type)
        }
    }
    ```

---

#### [MODIFY] [options.go](file:///shared/libs/go/codingagent/options.go)
*   **Description**: R6 の `MaxTurns` フィールドと `WithMaxTurns` オプションを追加
*   **Technical Design**:
    *   `SessionConfig` に `MaxTurns int` フィールドを追加
    *   `WithMaxTurns(n int) SessionOption` 関数を追加
*   **Logic**:

    ```go
    // SessionConfig に追加:
    type SessionConfig struct {
        // ... 既存フィールド (Model, Prompt, AllowedTools, WorkDir, EnvVars, SDKSessionID, VFSMounts)

        // MaxTurns limits the number of agent turns. 0 means CLI default.
        MaxTurns int
    }

    // WithMaxTurns sets the maximum number of agent turns.
    func WithMaxTurns(n int) SessionOption {
        return func(c *SessionConfig) { c.MaxTurns = n }
    }
    ```

---

#### [MODIFY] [process.go](file:///shared/libs/go/codingagent/claudecode/process.go)
*   **Description**: R1, R6, R7 の実装
*   **Technical Design**:
    *   `BuildArgs()`: `--bare` フラグを追加、`MaxTurns > 0` の場合 `--max-turns` を追加
    *   `StartProcess()`: `cmd.Stdin` に空の Reader を設定して stdin 警告を抑制
*   **Logic**:

    **`BuildArgs()` の変更**:
    ```go
    func BuildArgs(cfg *codingagent.SessionConfig) []string {
        args := []string{
            "--bare",                          // R1: OAuth/Keychain スキップ
            "--output-format", "stream-json",
            "--verbose",                       // R4: stream-json に必須
            "--permission-mode", "bypassPermissions",
        }
        if cfg.Prompt != "" {
            args = append(args, "-p", cfg.Prompt)
        }
        if cfg.Model != "" {
            args = append(args, "--model", cfg.Model)
        }
        if len(cfg.AllowedTools) > 0 {
            args = append(args, "--allowedTools", strings.Join(cfg.AllowedTools, ","))
        }
        if cfg.SDKSessionID != "" {
            args = append(args, "--session-id", cfg.SDKSessionID)
        }
        // R6: MaxTurns が設定されている場合のみフラグ追加
        if cfg.MaxTurns > 0 {
            args = append(args, "--max-turns", strconv.Itoa(cfg.MaxTurns))
        }
        return args
    }
    ```

    **`StartProcess()` の変更**:
    ```go
    // import に "bytes" を追加

    func StartProcess(
        ctx context.Context,
        ac *codingagent.AdapterConfig,
        cfg *codingagent.SessionConfig,
    ) (<-chan codingagent.StreamEvent, *ProcessManager, error) {
        // ... 既存のコマンド構築

        // R7: stdin 警告の抑制 (EOF を即座に返す空の Reader を設定)
        cmd.Stdin = bytes.NewReader(nil)

        // ... 残りは既存通り
    }
    ```

    **import 追加**: `"strconv"`, `"bytes"`

---

#### [MODIFY] [protocol.go](file:///shared/libs/go/codingagent/claudecode/protocol.go)
*   **Description**: R2, R3 の v2.1 プロトコル対応
*   **Technical Design**:
    *   `contentBlock` に `Text` フィールドを追加
    *   `ParseJSONLinesEvent()` の `assistant` ケースで `type:"text"` ブロックを処理
    *   `system/thinking_tokens` は既存の `system` ケースで自然に nil を返す (subtype != "init")
*   **Logic**:

    **`contentBlock` 構造体の変更**:
    ```go
    type contentBlock struct {
        Type      string         `json:"type"`
        Text      string         `json:"text,omitempty"`      // R3: v2.1 テキストブロック
        Name      string         `json:"name,omitempty"`
        Input     map[string]any `json:"input,omitempty"`
        ToolUseID string         `json:"tool_use_id,omitempty"`
        Content   string         `json:"content,omitempty"`
    }
    ```

    **`ParseJSONLinesEvent()` の `assistant` ケース変更**:
    ```go
    case "assistant":
        var msg messagePayload
        if err := json.Unmarshal(raw.Message, &msg); err != nil {
            return nil
        }
        for _, block := range msg.Content {
            switch block.Type {
            case "tool_use":
                return &codingagent.StreamEvent{
                    Type:      codingagent.EventToolUse,
                    ToolName:  block.Name,
                    ToolInput: block.Input,
                }
            case "text":
                // R3: v2.1 形式のテキストブロック
                if block.Text != "" {
                    return &codingagent.StreamEvent{
                        Type:    codingagent.EventText,
                        Content: block.Text,
                    }
                }
            // case "thinking": 無視 (何もしない)
            }
        }
        return nil
    ```

    **注**: `system/thinking_tokens` は subtype が "init" でないため、既存の `system` ケースで自然に nil が返される。追加の変更は不要。

---

### agentservice パッケージ

---

#### [NEW] [version.go](file:///shared/libs/go/agentservice/version.go)
*   **Description**: R8 のバージョンパース・比較ロジックを独立ファイルに切り出す
*   **Technical Design**:
    *   `parseCLIVersion(raw string) (major, minor, patch int, err error)` 関数
    *   `checkCLIVersion(raw string, minVersion string) error` 関数
    *   `minClaudeCLIVersion` 定数
*   **Logic**:

    ```go
    package agentservice

    import (
        "fmt"
        "strconv"
        "strings"
    )

    const minClaudeCLIVersion = "2.1.0"

    // parseCLIVersion extracts major.minor.patch from a version string like "2.1.169 (Claude Code)".
    // Returns (0,0,0, err) if parsing fails.
    func parseCLIVersion(raw string) (major, minor, patch int, err error) {
        // "2.1.169 (Claude Code)" -> "2.1.169"
        raw = strings.TrimSpace(raw)
        parts := strings.Fields(raw)
        if len(parts) == 0 {
            return 0, 0, 0, fmt.Errorf("empty version string")
        }
        versionStr := parts[0]

        segments := strings.SplitN(versionStr, ".", 3)
        if len(segments) < 2 {
            return 0, 0, 0, fmt.Errorf("invalid version format: %q", versionStr)
        }

        major, err = strconv.Atoi(segments[0])
        if err != nil {
            return 0, 0, 0, fmt.Errorf("invalid major version: %w", err)
        }
        minor, err = strconv.Atoi(segments[1])
        if err != nil {
            return 0, 0, 0, fmt.Errorf("invalid minor version: %w", err)
        }
        if len(segments) >= 3 {
            patch, err = strconv.Atoi(segments[2])
            if err != nil {
                return major, minor, 0, nil // patch なしでも OK
            }
        }
        return major, minor, patch, nil
    }

    // checkCLIVersion validates that the given version meets the minimum requirement.
    // Returns nil if valid, or an error with a user-friendly message.
    func checkCLIVersion(raw string, minVersion string) error {
        if raw == "" || raw == "unavailable" {
            return nil // CLI not found; handled separately
        }

        major, minor, _, err := parseCLIVersion(raw)
        if err != nil {
            return fmt.Errorf("failed to parse CLI version %q: %w", raw, err)
        }

        minMajor, minMinor, _, _ := parseCLIVersion(minVersion)

        if major < minMajor || (major == minMajor && minor < minMinor) {
            return fmt.Errorf(
                "Claude Code CLI version %s is not supported. Minimum required: %s. Run \"claude update\" to upgrade",
                raw, minVersion,
            )
        }
        return nil
    }
    ```

---

#### [NEW] [version_test.go](file:///shared/libs/go/agentservice/version_test.go)
*   **Description**: R8 のバージョンパース・比較テスト
*   **Technical Design**:
    *   テーブル駆動テストで複数のバージョン文字列をテスト
    *   TDD: テストを先に書く
*   **Logic**:

    ```go
    package agentservice

    import "testing"

    func TestParseCLIVersion(t *testing.T) {
        tests := []struct {
            name      string
            input     string
            wantMajor int
            wantMinor int
            wantPatch int
            wantErr   bool
        }{
            {
                name: "v2.1.169 with suffix",
                input: "2.1.169 (Claude Code)",
                wantMajor: 2, wantMinor: 1, wantPatch: 169,
            },
            {
                name: "v2.0.14 old version",
                input: "2.0.14 (Claude Code)",
                wantMajor: 2, wantMinor: 0, wantPatch: 14,
            },
            {
                name: "version only",
                input: "2.1.169",
                wantMajor: 2, wantMinor: 1, wantPatch: 169,
            },
            {
                name: "empty string",
                input: "",
                wantErr: true,
            },
        }

        for _, tt := range tests {
            t.Run(tt.name, func(t *testing.T) {
                major, minor, patch, err := parseCLIVersion(tt.input)
                if tt.wantErr {
                    if err == nil {
                        t.Error("expected error, got nil")
                    }
                    return
                }
                if err != nil {
                    t.Fatalf("unexpected error: %v", err)
                }
                if major != tt.wantMajor || minor != tt.wantMinor || patch != tt.wantPatch {
                    t.Errorf("got %d.%d.%d, want %d.%d.%d",
                        major, minor, patch,
                        tt.wantMajor, tt.wantMinor, tt.wantPatch)
                }
            })
        }
    }

    func TestCheckCLIVersion(t *testing.T) {
        tests := []struct {
            name    string
            raw     string
            wantErr bool
        }{
            {name: "v2.1.169 OK", raw: "2.1.169 (Claude Code)", wantErr: false},
            {name: "v2.1.0 exact minimum", raw: "2.1.0", wantErr: false},
            {name: "v2.0.14 too old", raw: "2.0.14 (Claude Code)", wantErr: true},
            {name: "v1.99.0 too old", raw: "1.99.0", wantErr: true},
            {name: "v3.0.0 future OK", raw: "3.0.0", wantErr: false},
            {name: "unavailable skipped", raw: "unavailable", wantErr: false},
            {name: "empty skipped", raw: "", wantErr: false},
        }

        for _, tt := range tests {
            t.Run(tt.name, func(t *testing.T) {
                err := checkCLIVersion(tt.raw, minClaudeCLIVersion)
                if tt.wantErr && err == nil {
                    t.Error("expected error, got nil")
                }
                if !tt.wantErr && err != nil {
                    t.Errorf("unexpected error: %v", err)
                }
            })
        }
    }
    ```

---

#### [MODIFY] [service.go](file:///shared/libs/go/agentservice/service.go)
*   **Description**: R8 の `detectCLIVersions()` にバージョン検証ロジックを追加
*   **Technical Design**:
    *   `detectCLIVersions()` のシグネチャに `logger.Logger` を追加
    *   バージョン取得後に `checkCLIVersion()` を呼び出す
    *   バージョン不足の場合はエラーログを出力
*   **Logic**:

    ```go
    // detectCLIVersions のシグネチャ変更:
    func detectCLIVersions(agents map[string]codingagent.CodingAgent, log logger.Logger) map[string]string {
        versions := make(map[string]string)
        cliNames := map[string]string{
            "claudecode": "claude",
            "codex":      "codex",
        }
        for agentName := range agents {
            cliName, ok := cliNames[agentName]
            if !ok {
                versions[agentName] = "unavailable"
                continue
            }
            out, err := exec.Command(cliName, "--version").Output()
            if err != nil {
                versions[agentName] = "unavailable"
                continue
            }
            versionStr := strings.TrimSpace(string(out))
            versions[agentName] = versionStr

            // R8: バージョンチェック
            if err := checkCLIVersion(versionStr, minClaudeCLIVersion); err != nil {
                if log != nil {
                    log.Error(err.Error(), "agent", agentName)
                }
            }
        }
        return versions
    }
    ```

    **呼び出し元 `HTTPHandler()` の変更** (L134):
    ```go
    func (s *Server) HTTPHandler() http.Handler {
        if s.cliVersions == nil {
            s.cliVersions = detectCLIVersions(s.agents, s.logger)  // logger を渡す
        }
        // ...
    }
    ```

---

### ドキュメント

---

#### [MODIFY] [README.md](file:///README.md)
*   **Description**: R5 の前提条件テーブルとヘルスチェック出力例の更新
*   **Technical Design**:
    *   前提条件テーブル (L8-14) に Claude Code CLI の行を追加
    *   ヘルスチェック出力例 (L315) のバージョンを更新
    *   移行注意事項を追記
*   **Logic**:

    **前提条件テーブル** (L12 の後に行を追加):
    ```markdown
    | Claude Code CLI | 2.1.x 以上 | `claude update` でアップデート。v2.0.x は非対応 |
    ```

    **ヘルスチェック出力例** (L315):
    ```diff
    -    "claudecode": "2.0.14 (Claude Code)"
    +    "claudecode": "2.1.x (Claude Code)"
    ```

    **前提条件セクションの後に注意事項を追加** (L15 の後):
    ```markdown
    > **Important**: Claude Code CLI v2.0.x は `ANTHROPIC_BASE_URL` 環境変数を無視するため、
    > Gateway 経由のリクエストが機能しません。必ず v2.1.x 以上にアップデートしてください。
    > アップデート: `claude update` を実行するか、
    > [公式インストーラー](https://claude.ai) を使用してください。
    ```

---

## Step-by-Step Implementation Guide

### Phase 1: テスト作成 (TDD - Red)

1. **Step 1: protocol_test.go にv2.1テストを追加**
    *   Edit `shared/libs/go/codingagent/claudecode/protocol_test.go`
    *   `TestParseJSONLinesEvent_V21_TextBlock`, `TestParseJSONLinesEvent_V21_ThinkingTokens`, `TestParseJSONLinesEvent_V21_ThinkingBlock`, `TestParseJSONLinesEvent_V21_Result`, `TestParseJSONLinesEvent_V21_TextAndToolUse` の5テストを追加
    *   `scripts/process/build.sh` で失敗を確認 (R2, R3 のテストが FAIL)

2. **Step 2: process_test.go にフラグテストを追加**
    *   Edit `shared/libs/go/codingagent/claudecode/process_test.go`
    *   テーブルに `--bare`, `--verbose`, `--max-turns` のケースを追加
    *   `notContain` フィールドを追加
    *   `scripts/process/build.sh` で失敗を確認 (R1, R6 のテストが FAIL)

3. **Step 3: version_test.go を作成**
    *   Create `shared/libs/go/agentservice/version_test.go`
    *   `TestParseCLIVersion`, `TestCheckCLIVersion` テストを追加
    *   `scripts/process/build.sh` で失敗を確認 (R8 のテストが FAIL - version.go 未作成)

### Phase 2: 実装 (TDD - Green)

4. **Step 4: options.go に MaxTurns を追加**
    *   Edit `shared/libs/go/codingagent/options.go`
    *   `SessionConfig` に `MaxTurns int` フィールドを追加
    *   `WithMaxTurns(n int) SessionOption` 関数を追加
    *   git commit: `feat: add MaxTurns field to SessionConfig (R6)`

5. **Step 5: process.go を更新**
    *   Edit `shared/libs/go/codingagent/claudecode/process.go`
    *   `BuildArgs()`: `--bare` フラグを追加 (args スライスの先頭)
    *   `BuildArgs()`: `cfg.MaxTurns > 0` の場合 `--max-turns` を追加
    *   `StartProcess()`: `cmd.Stdin = bytes.NewReader(nil)` を追加
    *   import に `"strconv"`, `"bytes"` を追加
    *   git commit: `feat: add --bare flag and --max-turns to BuildArgs (R1, R6, R7)`

6. **Step 6: protocol.go を更新**
    *   Edit `shared/libs/go/codingagent/claudecode/protocol.go`
    *   `contentBlock` に `Text string` フィールドを追加
    *   `ParseJSONLinesEvent()` の `assistant` ケースで `case "text":` を追加
    *   git commit: `feat: support v2.1 text blocks in protocol parser (R2, R3)`

7. **Step 7: version.go を作成**
    *   Create `shared/libs/go/agentservice/version.go`
    *   `parseCLIVersion()`, `checkCLIVersion()`, `minClaudeCLIVersion` 定数を実装
    *   git commit: `feat: add CLI version parsing and validation (R8)`

8. **Step 8: service.go を更新**
    *   Edit `shared/libs/go/agentservice/service.go`
    *   `detectCLIVersions()` に `logger.Logger` パラメータを追加
    *   バージョンチェックロジックを追加
    *   `HTTPHandler()` の呼び出しを更新
    *   git commit: `feat: add CLI version check at server init (R8)`

### Phase 3: ビルド検証

9. **Step 9: ビルド + 単体テスト**
    *   `scripts/process/build.sh` を実行
    *   全テスト (既存 + 新規) が PASS することを確認

### Phase 4: ドキュメント更新

10. **Step 10: README.md を更新**
    *   Edit `README.md`
    *   前提条件テーブルに Claude Code CLI の行を追加
    *   ヘルスチェック出力例のバージョンを更新
    *   移行注意事項を追記
    *   git commit: `docs: add Claude Code CLI version requirement to README (R5)`

### Phase 5: 統合テスト + git push

11. **Step 11: 統合テスト**
    *   `scripts/process/integration_test.sh --categories "common"` を実行
    *   リグレッションがないことを確認

12. **Step 12: git push**
    *   全テスト成功後に `git push`

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```
    *   **検証項目**:
        *   `TestBuildArgs` の新ケース (--bare, --verbose, --max-turns) が PASS
        *   `TestParseJSONLinesEvent_V21_*` の全ケースが PASS
        *   `TestParseCLIVersion` が PASS
        *   `TestCheckCLIVersion` が PASS
        *   既存テストにリグレッションなし

2. **Integration Tests (common)**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories "common"
    ```
    *   **Log Verification**: 共通機能のリグレッションがないこと

3. **Integration Tests (llm)**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories "llm"
    ```
    *   **Log Verification**: LLM 関連のリグレッションがないこと

### テスト項目のセルフレビュー

**ボトムアップ順序**:
1. `version_test.go`: 純粋関数テスト (依存なし、最下層)
2. `protocol_test.go`: パーサーテスト (JSON 入力 -> StreamEvent 出力)
3. `process_test.go`: CLI 引数構築テスト (SessionConfig -> []string)
4. 統合テスト: 既存テストでリグレッション確認

**観点チェックリスト**:
- 正常系: v2.1 テキストブロック抽出、バージョンパース正常、--bare/--max-turns 出力
- 異常系: 無効なバージョン文字列、空文字列
- 境界値: MaxTurns=0 (フラグ省略)、最低バージョン境界 (2.1.0 ぴったり)
- 後方互換: v2.0 の stream_event テストが引き続き動作

**網羅性**: 全 8 要件 (R1-R8) に対応するテストケースが存在する。
**迂回排除**: 全テストが `scripts/process/build.sh` 経由で実行される。

## Documentation

#### [MODIFY] [README.md](file:///README.md)
*   **更新内容**: 前提条件テーブルに Claude Code CLI v2.1.x 要件を追加、ヘルスチェック出力例のバージョン更新、移行注意事項の追記
