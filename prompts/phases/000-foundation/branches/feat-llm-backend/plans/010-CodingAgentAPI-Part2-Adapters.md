# 010-CodingAgentAPI-Part2-Adapters

> **Source Specification**: [007-CodingAgentAPI.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/007-CodingAgentAPI.md)

## Goal Description

Claude Code Adapter (`claudecode/`) と Codex Adapter (`codex/`) を実装する。各AdapterはPart1で定義した `CodingAgent` インターフェースを実装し、CLIサブプロセスのライフサイクル管理、プロトコルパーサー (JSON Lines / JSON-RPC 2.0)、環境変数注入を行う。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R2-1: CodingAgent インターフェース実装 | `claudecode/adapter.go`, `codex/adapter.go` |
| R2-2: コンストラクタ注入 | `claudecode/adapter.go New()`, `codex/adapter.go New()` |
| R2-3: サブプロセスライフサイクル | `claudecode/process.go`, `codex/process.go` |
| R2-4: Gateway URL注入 | `claudecode/adapter.go`, `codex/config.go` |
| R2-5: リトライロジック | Part1で実装済み。Adapterから `codingagent.Retry()` を呼び出す |
| R3-1: Claude CLI起動 (NDJSON) | `claudecode/process.go` |
| R3-2: 環境変数注入 | `claudecode/process.go buildEnv()` |
| R3-3: JSON Linesパーサー | `claudecode/protocol.go` |
| R3-4: --session-id (resume) | `claudecode/process.go buildArgs()` |
| R3-5: CWD制御 | `claudecode/process.go` |
| R3-6: サンドボックス制御 | `claudecode/process.go buildEnv()` |
| R4-1: Codex CLI起動 (JSON-RPC 2.0) | `codex/process.go` |
| R4-2: config.toml生成 | `codex/config.go` |
| R4-3: JSON-RPC 2.0ライフサイクル | `codex/protocol.go` |
| R4-4: 承認フロー自動承認 | `codex/protocol.go` |
| R4-5: JSON-RPC通知イベント変換 | `codex/protocol.go` |
| R6-1〜R6-4: 起動オプション二層構造 | Part1 `ApplyDefaults` + 各Adapter |

## Proposed Changes

### claudecode パッケージ (Claude Code Adapter)

#### [NEW] [protocol_test.go](file://shared/libs/go/codingagent/claudecode/protocol_test.go)
*   **Description**: Claude Code JSON Lines パーサーのテスト
*   **Technical Design**:
    ```go
    package claudecode_test

    func TestParseJSONLinesEvent_System(t *testing.T)
    // テーブル駆動テスト:
    // - {"type":"system","subtype":"init","session_id":"abc"}
    //   -> StreamEvent{Type: EventSystem, SessionID: "abc"}
    // - {"type":"system","subtype":"other"}
    //   -> nil (無視)

    func TestParseJSONLinesEvent_StreamEvent(t *testing.T)
    // テーブル駆動テスト:
    // - {"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"hello"}}}
    //   -> StreamEvent{Type: EventText, Content: "hello"}

    func TestParseJSONLinesEvent_Assistant(t *testing.T)
    // テーブル駆動テスト:
    // - {"type":"assistant","message":{"content":[{"type":"tool_use","name":"Write","input":{...}}]}}
    //   -> StreamEvent{Type: EventToolUse, ToolName: "Write", ToolInput: {...}}

    func TestParseJSONLinesEvent_ToolResult(t *testing.T)
    // - {"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"xxx","content":"ok"}]}}
    //   -> StreamEvent{Type: EventToolResult, Content: "ok"}

    func TestParseJSONLinesEvent_Result(t *testing.T)
    // - {"type":"result","result":"completed"}
    //   -> StreamEvent{Type: EventResult}

    func TestParseJSONLinesEvent_Invalid(t *testing.T)
    // - 空行 -> nil
    // - 非JSON文字列 -> StreamEvent{Type: EventError}
    // - 不正なJSON構造 -> StreamEvent{Type: EventError}
    ```
*   **Logic**: 各テストケースでは `ParseJSONLinesEvent()` に1行分のJSON文字列を渡し、返り値の `StreamEvent` のフィールドを検証する

#### [NEW] [protocol.go](file://shared/libs/go/codingagent/claudecode/protocol.go)
*   **Description**: Claude Code JSON Lines パーサー
*   **Technical Design**:
    ```go
    package claudecode

    import (
        "encoding/json"
        "github.com/axsh/hag/codingagent"
    )

    // rawEvent はClaude CLIのJSON Lines出力の生構造
    type rawEvent struct {
        Type    string          `json:"type"`
        Subtype string          `json:"subtype,omitempty"`
        SessionID string        `json:"session_id,omitempty"`
        Event   json.RawMessage `json:"event,omitempty"`
        Message json.RawMessage `json:"message,omitempty"`
        Result  string          `json:"result,omitempty"`
    }

    // streamEventPayload は stream_event 内の event フィールド
    type streamEventPayload struct {
        Type  string `json:"type"`
        Delta struct {
            Type string `json:"type"`
            Text string `json:"text"`
        } `json:"delta,omitempty"`
    }

    // messagePayload は assistant/user メッセージの content 配列要素
    type messagePayload struct {
        Content []contentBlock `json:"content"`
    }
    type contentBlock struct {
        Type      string         `json:"type"`
        Name      string         `json:"name,omitempty"`
        Input     map[string]any `json:"input,omitempty"`
        ToolUseID string         `json:"tool_use_id,omitempty"`
        Content   string         `json:"content,omitempty"`
    }

    // ParseJSONLinesEvent は1行のJSON Lines出力をStreamEventに変換する。
    // 無視すべきイベントの場合は nil を返す。
    func ParseJSONLinesEvent(line string) *codingagent.StreamEvent {
        if line == "" { return nil }

        var raw rawEvent
        if err := json.Unmarshal([]byte(line), &raw); err != nil {
            return &codingagent.StreamEvent{Type: codingagent.EventError, Error: err}
        }

        switch raw.Type {
        case "system":
            if raw.Subtype == "init" {
                return &codingagent.StreamEvent{
                    Type:      codingagent.EventSystem,
                    SessionID: raw.SessionID,
                }
            }
            return nil // 他の system イベントは無視

        case "stream_event":
            var payload streamEventPayload
            if err := json.Unmarshal(raw.Event, &payload); err != nil {
                return &codingagent.StreamEvent{Type: codingagent.EventError, Error: err}
            }
            if payload.Type == "content_block_delta" && payload.Delta.Type == "text_delta" {
                return &codingagent.StreamEvent{
                    Type:    codingagent.EventText,
                    Content: payload.Delta.Text,
                }
            }
            return nil

        case "assistant":
            var msg messagePayload
            if err := json.Unmarshal(raw.Message, &msg); err != nil { return nil }
            for _, block := range msg.Content {
                if block.Type == "tool_use" {
                    return &codingagent.StreamEvent{
                        Type:      codingagent.EventToolUse,
                        ToolName:  block.Name,
                        ToolInput: block.Input,
                    }
                }
            }
            return nil

        case "user":
            var msg messagePayload
            if err := json.Unmarshal(raw.Message, &msg); err != nil { return nil }
            for _, block := range msg.Content {
                if block.Type == "tool_result" {
                    return &codingagent.StreamEvent{
                        Type:    codingagent.EventToolResult,
                        Content: block.Content,
                    }
                }
            }
            return nil

        case "result":
            return &codingagent.StreamEvent{Type: codingagent.EventResult}

        default:
            return nil
        }
    }
    ```

---

#### [NEW] [process_test.go](file://shared/libs/go/codingagent/claudecode/process_test.go)
*   **Description**: Claude Code プロセス管理のテスト
*   **Technical Design**:
    ```go
    func TestBuildArgs(t *testing.T)
    // テーブル駆動テスト:
    // - 基本ケース: Prompt + Model -> [--output-format, stream-json, -p, "prompt", --model, "model", ...]
    // - AllowedTools指定: -> [--allowedTools, "Read,Edit,Write"]
    // - SDKSessionID指定: -> [--session-id, "sdk-id"]
    // - 全フラグ組み合わせ

    func TestBuildEnv(t *testing.T)
    // テーブル駆動テスト:
    // - GatewayURL指定: ANTHROPIC_BASE_URL が設定される
    // - DisableSandbox=true: CLAUDE_CODE_SKIP_SANDBOX=1 が設定される
    // - DisableSandbox=false: CLAUDE_CODE_SKIP_SANDBOX が設定されない
    // - 追加EnvVars: そのまま追加される
    // - APIキー: ANTHROPIC_API_KEY が "not-needed" で設定される

    func TestBuildCommand(t *testing.T)
    // - exec.Command の実行ファイルが "claude" であること
    // - CWD (Dir) が SessionConfig.WorkDir に設定されること
    ```

#### [NEW] [process.go](file://shared/libs/go/codingagent/claudecode/process.go)
*   **Description**: Claude Code CLIサブプロセス管理
*   **Technical Design**:
    ```go
    package claudecode

    import (
        "bufio"
        "context"
        "os/exec"
        "strings"

        "github.com/axsh/hag/codingagent"
    )

    // ProcessManager はClaude CLIサブプロセスを管理する。
    type ProcessManager struct {
        cmd    *exec.Cmd
        cancel context.CancelFunc
    }

    // buildArgs は SessionConfig から claude CLI の引数を構築する。
    func buildArgs(cfg *codingagent.SessionConfig) []string {
        args := []string{
            "--output-format", "stream-json",
            "--input-format", "stream-json",
            "--verbose",
            "--permission-mode", "bypassPermissions",
        }
        if cfg.Prompt != "" { args = append(args, "-p", cfg.Prompt) }
        if cfg.Model != "" { args = append(args, "--model", cfg.Model) }
        if len(cfg.AllowedTools) > 0 {
            args = append(args, "--allowedTools", strings.Join(cfg.AllowedTools, ","))
        }
        if cfg.SDKSessionID != "" {
            args = append(args, "--session-id", cfg.SDKSessionID)
        }
        return args
    }

    // buildEnv は AdapterConfig と SessionConfig から環境変数を構築する。
    func buildEnv(ac *codingagent.AdapterConfig, cfg *codingagent.SessionConfig) []string {
        env := make(map[string]string)
        // Gateway URL
        if ac.GatewayURL != "" { env["ANTHROPIC_BASE_URL"] = ac.GatewayURL }
        // APIキー (Gateway経由のため不要だが、CLIが要求するので設定)
        env["ANTHROPIC_API_KEY"] = "not-needed"
        // サンドボックス制御
        if ac.DisableSandbox { env["CLAUDE_CODE_SKIP_SANDBOX"] = "1" }
        // 追加環境変数
        for k, v := range cfg.EnvVars { env[k] = v }

        var result []string
        for k, v := range env { result = append(result, k+"="+v) }
        return result
    }

    // StartProcess は claude CLI をサブプロセスとして起動し、
    // stdout から JSON Lines を逐次読み取って StreamEvent チャネルに送信する。
    func StartProcess(
        ctx context.Context,
        ac *codingagent.AdapterConfig,
        cfg *codingagent.SessionConfig,
    ) (<-chan codingagent.StreamEvent, *ProcessManager, error) {
        procCtx, cancel := context.WithCancel(ctx)

        args := buildArgs(cfg)
        cmd := exec.CommandContext(procCtx, "claude", args...)
        cmd.Dir = cfg.WorkDir
        cmd.Env = append(cmd.Environ(), buildEnv(ac, cfg)...)

        stdout, err := cmd.StdoutPipe()
        if err != nil { cancel(); return nil, nil, err }

        if err := cmd.Start(); err != nil { cancel(); return nil, nil, err }

        ch := make(chan codingagent.StreamEvent, 64)
        pm := &ProcessManager{cmd: cmd, cancel: cancel}

        go func() {
            defer close(ch)
            scanner := bufio.NewScanner(stdout)
            for scanner.Scan() {
                line := scanner.Text()
                ev := ParseJSONLinesEvent(line)
                if ev != nil {
                    select {
                    case ch <- *ev:
                    case <-procCtx.Done(): return
                    }
                }
            }
        }()

        return ch, pm, nil
    }

    // Stop はサブプロセスを停止する (SIGTERM -> Wait)。
    func (pm *ProcessManager) Stop() error {
        pm.cancel()
        return pm.cmd.Wait()
    }
    ```

---

#### [NEW] [adapter_test.go](file://shared/libs/go/codingagent/claudecode/adapter_test.go)
*   **Description**: ClaudeCodeAdapter の CodingAgent インターフェース準拠テスト
*   **Technical Design**:
    ```go
    func TestClaudeCodeAdapterImplementsCodingAgent(t *testing.T)
    // var _ codingagent.CodingAgent = (*ClaudeCodeAdapter)(nil)

    func TestClaudeCodeAdapterName(t *testing.T)
    // adapter.Name() == "claudecode"
    ```

#### [NEW] [adapter.go](file://shared/libs/go/codingagent/claudecode/adapter.go)
*   **Description**: ClaudeCodeAdapter (CodingAgent実装)
*   **Technical Design**:
    ```go
    package claudecode

    import (
        "context"
        "github.com/axsh/hag/codingagent"
    )

    // ClaudeCodeAdapter は Claude Code CLI を使用する CodingAgent 実装。
    type ClaudeCodeAdapter struct {
        config *codingagent.AdapterConfig
        procs  []*ProcessManager // アクティブなプロセス
    }

    // compile-time check
    var _ codingagent.CodingAgent = (*ClaudeCodeAdapter)(nil)

    // New は ClaudeCodeAdapter を生成する。
    func New(config *codingagent.AdapterConfig) *ClaudeCodeAdapter {
        return &ClaudeCodeAdapter{config: config}
    }

    func (a *ClaudeCodeAdapter) Name() string { return "claudecode" }

    func (a *ClaudeCodeAdapter) CreateSession(
        ctx context.Context, opts ...codingagent.SessionOption,
    ) (codingagent.Session, error) {
        cfg := codingagent.NewSessionConfig(opts...)
        codingagent.ApplyDefaults(cfg, a.config)

        ch, pm, err := StartProcess(ctx, a.config, cfg)
        if err != nil { return nil, err }

        a.procs = append(a.procs, pm)
        return &claudeSession{id: "session-" + pm.cmd.Process.Pid(), ch: ch, pm: pm}, nil
    }

    func (a *ClaudeCodeAdapter) Close() error {
        for _, pm := range a.procs { pm.Stop() }
        a.procs = nil
        return nil
    }

    // claudeSession は Claude Code の Session 実装。
    type claudeSession struct {
        id string
        ch <-chan codingagent.StreamEvent
        pm *ProcessManager
    }

    func (s *claudeSession) Send(ctx context.Context, message string) (<-chan codingagent.StreamEvent, error) {
        // シングルショット: CreateSession 時に既にプロンプトが渡されているので、
        // ch をそのまま返す。マルチターンは O2 (任意要件) で対応。
        return s.ch, nil
    }
    func (s *claudeSession) ID() string    { return s.id }
    func (s *claudeSession) Close() error  { return s.pm.Stop() }
    ```

---

### codex パッケージ (Codex Adapter)

#### [NEW] [config_test.go](file://shared/libs/go/codingagent/codex/config_test.go)
*   **Description**: config.toml テンプレート生成テスト
*   **Technical Design**:
    ```go
    func TestGenerateConfigTOML(t *testing.T)
    // テーブル駆動テスト:
    // - model="gpt-4o", gatewayURL="http://localhost:14000"
    //   -> TOML文字列に model, base_url, wire_api が正しく埋め込まれること
    // - model="" (空) -> デフォルトモデル名が使用されること

    func TestWriteConfigTOML(t *testing.T)
    // - 一時ディレクトリにファイルが作成されること
    // - ファイル内容が GenerateConfigTOML の結果と一致すること
    // - 後処理で一時ファイルが削除可能であること
    ```

#### [NEW] [config.go](file://shared/libs/go/codingagent/codex/config.go)
*   **Description**: Codex config.toml テンプレート生成
*   **Technical Design**:
    ```go
    package codex

    import (
        "fmt"
        "os"
        "path/filepath"
    )

    const configTemplate = `model = "%s"
model_provider = "gateway"

[model_providers.gateway]
name = "HAG LLM Gateway"
base_url = "%s"
env_key = "OPENAI_API_KEY"
wire_api = "chat"
`

    // GenerateConfigTOML は Codex 用の config.toml 文字列を生成する。
    func GenerateConfigTOML(model, gatewayURL string) string {
        if model == "" { model = "gpt-4o" }
        return fmt.Sprintf(configTemplate, model, gatewayURL)
    }

    // WriteConfigTOML は一時ディレクトリに config.toml を書き出し、パスを返す。
    // 呼び出し側は使用後にファイルを削除する責任を持つ。
    func WriteConfigTOML(model, gatewayURL string) (string, error) {
        dir, err := os.MkdirTemp("", "codex-config-*")
        if err != nil { return "", err }
        path := filepath.Join(dir, "config.toml")
        content := GenerateConfigTOML(model, gatewayURL)
        if err := os.WriteFile(path, []byte(content), 0644); err != nil {
            return "", err
        }
        return path, nil
    }
    ```

---

#### [NEW] [protocol_test.go](file://shared/libs/go/codingagent/codex/protocol_test.go)
*   **Description**: Codex JSON-RPC 2.0 クライアントのテスト
*   **Technical Design**:
    ```go
    func TestBuildInitializeRequest(t *testing.T)
    // - JSON-RPC 2.0 形式の initialize リクエストが正しく構築されること
    // - method: "initialize", id: 1

    func TestBuildStartThreadRequest(t *testing.T)
    // - method: "startThread", params に prompt が含まれること

    func TestParseNotification(t *testing.T)
    // テーブル駆動テスト:
    // - {"jsonrpc":"2.0","method":"text","params":{"content":"hello"}}
    //   -> StreamEvent{Type: EventText, Content: "hello"}
    // - {"jsonrpc":"2.0","method":"tool_use","params":{"name":"Write",...}}
    //   -> StreamEvent{Type: EventToolUse, ToolName: "Write"}

    func TestParseApprovalRequest(t *testing.T)
    // - 承認リクエストの検出と自動承認レスポンス構築
    // - {"jsonrpc":"2.0","method":"approval_request","id":5,...}
    //   -> 自動承認レスポンス {"jsonrpc":"2.0","id":5,"result":{"approved":true}}
    ```

#### [NEW] [protocol.go](file://shared/libs/go/codingagent/codex/protocol.go)
*   **Description**: Codex JSON-RPC 2.0 クライアント
*   **Technical Design**:
    ```go
    package codex

    import (
        "encoding/json"
        "github.com/axsh/hag/codingagent"
    )

    // JSONRPCMessage は JSON-RPC 2.0 メッセージの汎用構造
    type JSONRPCMessage struct {
        JSONRPC string          `json:"jsonrpc"`
        ID      *int            `json:"id,omitempty"`
        Method  string          `json:"method,omitempty"`
        Params  json.RawMessage `json:"params,omitempty"`
        Result  json.RawMessage `json:"result,omitempty"`
    }

    // BuildInitializeRequest は initialize リクエストを構築する。
    func BuildInitializeRequest() ([]byte, error) {
        id := 1
        msg := JSONRPCMessage{
            JSONRPC: "2.0", ID: &id, Method: "initialize",
        }
        return json.Marshal(msg)
    }

    // BuildStartThreadRequest は startThread リクエストを構築する。
    func BuildStartThreadRequest(prompt string) ([]byte, error) {
        id := 2
        params, _ := json.Marshal(map[string]string{"prompt": prompt})
        msg := JSONRPCMessage{
            JSONRPC: "2.0", ID: &id, Method: "startThread",
            Params: params,
        }
        return json.Marshal(msg)
    }

    // ParseNotification は JSON-RPC 2.0 通知を StreamEvent に変換する。
    func ParseNotification(line string) *codingagent.StreamEvent {
        var msg JSONRPCMessage
        if err := json.Unmarshal([]byte(line), &msg); err != nil { return nil }

        switch msg.Method {
        case "text":
            var p struct{ Content string `json:"content"` }
            json.Unmarshal(msg.Params, &p)
            return &codingagent.StreamEvent{Type: codingagent.EventText, Content: p.Content}
        case "tool_use":
            var p struct {
                Name  string         `json:"name"`
                Input map[string]any `json:"input"`
            }
            json.Unmarshal(msg.Params, &p)
            return &codingagent.StreamEvent{
                Type: codingagent.EventToolUse, ToolName: p.Name, ToolInput: p.Input,
            }
        case "result":
            return &codingagent.StreamEvent{Type: codingagent.EventResult}
        default:
            return nil
        }
    }

    // IsApprovalRequest は承認リクエストかどうかを判定する。
    func IsApprovalRequest(msg *JSONRPCMessage) bool {
        return msg.Method == "approval_request" && msg.ID != nil
    }

    // BuildApprovalResponse は自動承認レスポンスを構築する。
    func BuildApprovalResponse(id int) ([]byte, error) {
        result, _ := json.Marshal(map[string]bool{"approved": true})
        msg := JSONRPCMessage{
            JSONRPC: "2.0", ID: &id, Result: result,
        }
        return json.Marshal(msg)
    }
    ```

---

#### [NEW] [process_test.go](file://shared/libs/go/codingagent/codex/process_test.go)
*   **Description**: Codex プロセス管理のテスト
*   **Technical Design**:
    ```go
    func TestCodexBuildArgs(t *testing.T)
    // - config.toml パスが --config 引数に含まれること

    func TestCodexBuildEnv(t *testing.T)
    // - OPENAI_API_KEY が設定されること
    ```

#### [NEW] [process.go](file://shared/libs/go/codingagent/codex/process.go)
*   **Description**: Codex CLIサブプロセス管理 (claudecode/process.go と類似構造)

---

#### [NEW] [adapter_test.go](file://shared/libs/go/codingagent/codex/adapter_test.go)
*   **Description**: CodexAdapter の CodingAgent インターフェース準拠テスト
    ```go
    func TestCodexAdapterImplementsCodingAgent(t *testing.T)
    // var _ codingagent.CodingAgent = (*CodexAdapter)(nil)

    func TestCodexAdapterName(t *testing.T)
    // adapter.Name() == "codex"
    ```

#### [NEW] [adapter.go](file://shared/libs/go/codingagent/codex/adapter.go)
*   **Description**: CodexAdapter (CodingAgent実装)。構造はClaudeCodeAdapterと同様。

## Step-by-Step Implementation Guide

1.  **Step 1: claudecode/ ディレクトリ作成**

2.  **Step 2: claudecode/protocol_test.go + protocol.go (JSON Lines パーサー)**:
    *   テストを先に作成し、全パターンのイベント変換を検証
    *   `ParseJSONLinesEvent()` を実装
    *   テスト Green を確認

3.  **Step 3: claudecode/process_test.go + process.go (サブプロセス管理)**:
    *   `buildArgs()`, `buildEnv()` のテストを作成
    *   `StartProcess()`, `ProcessManager.Stop()` を実装
    *   テスト Green を確認

4.  **Step 4: claudecode/adapter_test.go + adapter.go**:
    *   インターフェース準拠テストを作成
    *   `ClaudeCodeAdapter` を実装
    *   テスト Green を確認

5.  **Step 5: codex/ ディレクトリ作成**

6.  **Step 6: codex/config_test.go + config.go (config.toml生成)**:
    *   テストを先に作成
    *   `GenerateConfigTOML()`, `WriteConfigTOML()` を実装
    *   テスト Green を確認

7.  **Step 7: codex/protocol_test.go + protocol.go (JSON-RPC 2.0)**:
    *   テストを先に作成
    *   JSON-RPC メッセージ構築・解析関数を実装
    *   テスト Green を確認

8.  **Step 8: codex/process_test.go + process.go + adapter_test.go + adapter.go**:
    *   プロセス管理とAdapter本体を実装
    *   テスト Green を確認

9.  **Step 9: ビルド検証**:
    *   Verification Plan を実行

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh --skip-frontend --skip-etc
    ```

### テスト項目のセルフレビュー結果

1.  **網羅性の検証**: Claude Code (protocol, process, adapter) と Codex (config, protocol, process, adapter) の全コンポーネントにテストがある。イベント変換の全パターン (system, stream_event, assistant, user, result) をカバーしている。
2.  **証拠の十分性**: JSON Lines の各イベント型について具体的な入力JSON文字列と期待される StreamEvent フィールドの値を検証。buildArgs/buildEnv は引数とフラグの組み合わせを網羅。
3.  **迂回・抜け道の排除**: compile-time check でインターフェース準拠を保証。
4.  **依存関係の整合性**: Part1 (codingagent) -> Part2 (claudecode/codex) のボトムアップ順。protocol -> process -> adapter の順でテスト。

## 継続計画について

- **Part1 (009)**: `codingagent` パッケージのコア抽象層 -- 完了前提
- **Part2 (本計画)**: `claudecode/` + `codex/` Adapter実装
- **Part3 (011)**: `agentservice/` Web API + hag.Server統合
