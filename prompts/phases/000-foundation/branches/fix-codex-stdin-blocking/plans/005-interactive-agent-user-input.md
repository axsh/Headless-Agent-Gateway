# 005-interactive-agent-user-input

> **Source Specification**: `prompts/phases/000-foundation/branches/fix-codex-stdin-blocking/ideas/003-interactive-agent-user-input.md`

## Goal Description

Coding Agent（Codex / Claude Code / Wayfinder）の実行中に発生するユーザー入力要求を、Tern の第一級機能として扱えるようにする。`user_input_required` イベント、`suspended` セッションステータス、`POST /respond` API、双方向 stdin、タイムアウトウォッチドッグ、クライアント SDK コールバックを実装し、「非対話・単発実行」前提のアーキテクチャをインタラクティブ実行モデルへ拡張する。

## User Review Required

- **SSE 継続方式**: 初期実装は方式 B（`respond` が新 SSE ストリームを返す）。クライアント SDK がループで吸収する。
- **デフォルト `execution_mode`**: 本番設定は `interactive`、E2E テスト config は `single_shot` を明示してリグレッション防止。
- **README**: 仕様レビュー時点で既に更新済み。実装完了後に API 実装との整合を再確認する。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: `user_input_required` イベント | Proposed Changes > `event.go`, `client/v1/stream.go` |
| R2: `suspended` ステータス | Proposed Changes > `session_store.go`, `handler.go` |
| R3: `POST /respond` API | Proposed Changes > `handler.go`, `service.go` |
| R4: 双方向 stdin（interactive / single_shot） | Proposed Changes > `codex/process.go`, `claudecode/process.go`, `interface.go` |
| R5: ユーザー入力要求の検出 | Proposed Changes > `codex/process.go`, `codex/input_detect.go`, Wayfinder tools |
| R6: idle タイムアウト | Proposed Changes > `codex/process.go`, `claudecode/process.go` |
| R7: max execution タイムアウト | Proposed Changes > `codex/process.go`, `claudecode/process.go` |
| R8: 並行 SendMessage 拒否 | Proposed Changes > `handler.go`, `exec_registry.go` |
| R9: クライアント SDK コールバック | Proposed Changes > `client/v1/stream.go`, `session.go` |
| R10: Wayfinder `ask_user` 統合 | Proposed Changes > `wayfinder/tools/*`, `wayfinder/agent_core.go` |
| R11: `execution_mode` 設定 | Proposed Changes > `config/model_profiles.go`, `handler.go`, adapters |
| R12: `choices` 構造化（ヒューリスティック禁止） | Proposed Changes > `wayfinder/tools/register.go`, `tool_ask_user.go` |
| 方式 A（SSE 再接続） | 非スコープ（仕様書どおり先送り） |
| `ternctl --auto-answer` | 非スコープ（任意要件。本計画では実装しない） |

---

## Proposed Changes

### codingagent パッケージ（共通基盤）

#### [MODIFY] `shared/libs/go/codingagent/event.go`
*   **Description**: 新イベント型と `StreamEvent` フィールド拡張。
*   **Technical Design**:
    ```go
    const (
        // EventUserInputRequired indicates the agent is waiting for user input.
        EventUserInputRequired EventType = "user_input_required"
    )

    type StreamEvent struct {
        Type      EventType              `json:"type"`
        Content   string                 `json:"content,omitempty"`
        PromptID  string                 `json:"prompt_id,omitempty"`
        Choices   []string               `json:"choices,omitempty"`
        ToolName  string                 `json:"tool_name,omitempty"`
        ToolInput map[string]interface{} `json:"tool_input,omitempty"`
        SessionID string                 `json:"session_id,omitempty"`
        Error     error                  `json:"-"`
    }
    ```
*   **Logic**:
    *   `EventUserInputRequired` 定数を追加する。
    *   `StreamEvent` に `PromptID string` と `Choices []string` フィールドを追加する。

#### [MODIFY] `shared/libs/go/codingagent/event_test.go`
*   **Description**: `user_input_required` の JSON シリアライズ/デシリアライズテスト。
*   **Logic**:
    *   `choices` あり/なし、`prompt_id` ありのケースをテーブル駆動で検証する。

#### [MODIFY] `shared/libs/go/codingagent/session_store.go`
*   **Description**: `suspended` ステータス定数追加。
*   **Technical Design**:
    ```go
    const (
        StatusActive    = "active"
        StatusSuspended = "suspended"
        StatusCompleted = "completed"
        StatusError     = "error"
        StatusClosed    = "closed"
    )
    ```
*   **Logic**:
    *   `StatusSuspended` を追加する。`active` と同様、非終端ステータスとして扱う。

#### [MODIFY] `shared/libs/go/codingagent/interface.go`
*   **Description**: インタラクティブセッション用のオプションインターフェース追加。
*   **Technical Design**:
    ```go
    // StdinWriter allows writing additional input to a running CLI session.
    type StdinWriter interface {
        WriteStdin(text string) error
    }
    ```
*   **Logic**:
    *   既存 `Session` インターフェースは変更しない。
    *   Codex / Claude Code の `codexSession` / `claudeSession` が `StdinWriter` を実装する。
    *   agentservice は type assertion で `StdinWriter` を取得する。

#### [NEW] `shared/libs/go/codingagent/execution_mode.go`
*   **Description**: `execution_mode` 定数とバリデーション。
*   **Technical Design**:
    ```go
    const (
        ExecutionModeInteractive = "interactive"
        ExecutionModeSingleShot  = "single_shot"
    )

    func NormalizeExecutionMode(mode string) string {
        switch mode {
        case ExecutionModeSingleShot:
            return ExecutionModeSingleShot
        default:
            return ExecutionModeInteractive
        }
    }
    ```
*   **Logic**:
    *   未設定・不正値は `interactive` にフォールバックする（R11）。

---

### config パッケージ

#### [MODIFY] `shared/libs/go/config/model_profiles.go`
*   **Description**: `AgentConfig` 拡張とデフォルト解決ヘルパー。
*   **Technical Design**:
    ```go
    type AgentConfig struct {
        MaxPromptBytes      int    `yaml:"max_prompt_bytes"`
        MaxExecutionSeconds int    `yaml:"max_execution_seconds"`
        IdleTimeoutSeconds  int    `yaml:"idle_timeout_seconds"`
        ExecutionMode       string `yaml:"execution_mode"`
    }

    const (
        DefaultMaxPromptBytes      = 1048576
        DefaultMaxExecutionSeconds = 3600
        DefaultIdleTimeoutSeconds  = 300
    )

    func (c AgentConfig) WithDefaults() AgentConfig { ... }
    func ResolveAgentConfig(profiles *ModelProfilesConfig, agentName string) AgentConfig { ... }
    ```
*   **Logic**:
    *   `MaxPromptBytes == 0` → `1048576`
    *   `MaxExecutionSeconds == 0` → `3600`
    *   `IdleTimeoutSeconds == 0` → `300`
    *   `ExecutionMode` → `codingagent.NormalizeExecutionMode(c.ExecutionMode)`
    *   `ResolveAgentConfig`: `profiles.CodingAgents[agentName]` を取得し `WithDefaults()` を返す。未定義エージェントもデフォルト値を返す。

#### [MODIFY] `shared/libs/go/config/model_profiles_test.go`
*   **Description**: 新フィールドの YAML アンマーシャルとデフォルト解決テスト。

#### [MODIFY] `settings/example/model_profiles.yaml`
*   **Description**: `coding_agents` に `max_execution_seconds`, `idle_timeout_seconds`, `execution_mode` を追加（README と整合）。

#### [MODIFY] `tests/testdata/model_profiles.yaml`
*   **Description**: E2E 用に `single_shot` を明示。
    ```yaml
    coding_agents:
      codex:
        max_prompt_bytes: 1048576
        execution_mode: single_shot
      claudecode:
        execution_mode: single_shot
    ```

---

### codex パッケージ

#### [NEW] `shared/libs/go/codingagent/codex/input_detect.go`
*   **Description**: stderr / プロトコルからのユーザー入力待ち検出（ヒューリスティック禁止）。
*   **Technical Design**:
    ```go
    var stdinWaitPatterns = []string{
        "Reading additional input from stdin",
    }

    func DetectStdinWaitFromStderr(line string) (bool, string)
    func DetectUserInputFromExecEvent(line string) *codingagent.StreamEvent
    ```
*   **Logic**:
    *   `DetectStdinWaitFromStderr`: `stdinWaitPatterns` のいずれかを含む場合 `(true, line)` を返す。
    *   `DetectUserInputFromExecEvent`: JSONL 行をパースし、明示的な `choices []string` フィールドを持つイベント型のみ `EventUserInputRequired` に変換する。該当なしは `nil`。
    *   自由テキストからの `choices` 抽出は**実装しない**（R12）。

#### [MODIFY] `shared/libs/go/codingagent/codex/input_detect_test.go` (NEW)
*   **Description**: stderr パターン検出、JSON `choices` フィールド抽出のテーブル駆動テスト。

#### [MODIFY] `shared/libs/go/codingagent/codex/process.go`
*   **Description**: interactive / single_shot 分岐、双方向 stdin、ウォッチドッグ。
*   **Technical Design**:
    ```go
    type ProcessOptions struct {
        ExecutionMode       string
        IdleTimeoutSeconds  int
        MaxExecutionSeconds int
    }

    type ProcessManager struct {
        cmd         *exec.Cmd
        cancel      context.CancelFunc
        codexHome   string
        logger      logger.Logger
        stdinWriter io.WriteCloser
        stdinMu     sync.Mutex
    }

    func (pm *ProcessManager) WriteStdin(text string) error {
        pm.stdinMu.Lock()
        defer pm.stdinMu.Unlock()
        if pm.stdinWriter == nil {
            return fmt.Errorf("stdin not available (single_shot or closed)")
        }
        if _, err := io.WriteString(pm.stdinWriter, text); err != nil {
            return err
        }
        if _, err := io.WriteString(pm.stdinWriter, "\n"); err != nil {
            return err
        }
        return nil
    }

    func StartProcess(
        ctx context.Context,
        ac *codingagent.AdapterConfig,
        cfg *codingagent.SessionConfig,
        configOverrides []string,
        codexHome string,
        opts ProcessOptions,
    ) (<-chan codingagent.StreamEvent, *ProcessManager, error)
    ```
*   **Logic**:
    1. `stdinReader, stdinWriter := io.Pipe()` を作成し `cmd.Stdin = stdinReader` を設定する。
    2. **`single_shot`**: プロンプト書き込み goroutine 内で `defer stdinWriter.Close()` し、現行動作を維持する。
    3. **`interactive`**: プロンプト書き込み後も `stdinWriter` を `ProcessManager` に保持し、Close しない。
    4. stderr スキャン goroutine で各行を `DetectStdinWaitFromStderr` に渡し、マッチ時に `EventUserInputRequired` を ch に送信する。`content` は stderr 行、判別不能時は `"Agent is waiting for user input"`。
    5. stdout スキャンで `DetectUserInputFromExecEvent` が非 nil を返した場合も `EventUserInputRequired` を送信する。
    6. **ウォッチドッグ goroutine**（interactive / single_shot 共通）:
        * 起動時刻を記録し、`MaxExecutionSeconds` 経過で `EventError{"agent max execution timeout after Ns"}` を送信して `Stop()` する（R7）。
        * 最後の stdout/stderr 出力時刻を更新し、`IdleTimeoutSeconds` 無出力で `EventError{"agent idle timeout after Ns"}` を送信して `Stop()` する（R6）。
    7. `Stop()` 時に `stdinWriter.Close()` を呼び、リソースを解放する。

#### [MODIFY] `shared/libs/go/codingagent/codex/process_test.go`
*   **Description**: `single_shot` で stdin が Close されること、`interactive` で Close されないこと、`WriteStdin` のテスト。

#### [MODIFY] `shared/libs/go/codingagent/codex/adapter.go`
*   **Description**: `ProcessOptions` を `AdapterConfig` / セッションオプションから構築して `StartProcess` に渡す。
*   **Logic**:
    *   `AdapterConfig` に `ExecutionMode`, `IdleTimeoutSeconds`, `MaxExecutionSeconds` フィールドを追加する。
    *   `codexSession` が `WriteStdin` を `pm.WriteStdin` に委譲する。

#### [MODIFY] `shared/libs/go/codingagent/adapter_config.go`
*   **Description**: `AdapterConfig` にエージェント実行設定フィールド追加。

---

### claudecode パッケージ

#### [MODIFY] `shared/libs/go/codingagent/claudecode/process.go`
*   **Description**: codex と同様の interactive stdin パイプ、ウォッチドッグ、`WriteStdin`。
*   **Logic**:
    1. `bytes.NewReader(nil)` を `io.Pipe()` に置き換える。
    2. **`single_shot`**: 現行どおり即 EOF（`-p` フラグでプロンプト渡し）。`stdinWriter` は即 Close。
    3. **`interactive`**: `-p` フラグは維持しつつ、追加入力は `stdinWriter` 経由で書き込む（CLI ヘルプで stdin 追記が有効か起動前に `claude --help` で確認し、無効なら `interactive` モードで stderr に警告ログを出す）。
    4. ウォッチドッグは codex と同じロジック（R6/R7）。
    5. ユーザー入力検出: stderr に既知パターンがあれば `EventUserInputRequired` を送出。JSONL に明示 `choices` があれば同様（R12: テキスト解析禁止）。

#### [MODIFY] `shared/libs/go/codingagent/claudecode/process_test.go`
*   **Description**: execution mode 分岐と `WriteStdin` テスト。

#### [MODIFY] `shared/libs/go/codingagent/claudecode/adapter.go`
*   **Description**: codex adapter と同様に `ProcessOptions` 連携、`WriteStdin` 委譲。

---

### agentservice パッケージ

#### [NEW] `shared/libs/go/agentservice/exec_registry.go`
*   **Description**: 実行中セッションの登録・参照・並行ガード。
*   **Technical Design**:
    ```go
    type activeExecution struct {
        sessionID  string
        agentSess  codingagent.Session
        stdin      codingagent.StdinWriter
        status     string // "active" | "suspended"
    }

    type execRegistry struct {
        mu   sync.Mutex
        exec map[string]*activeExecution
    }

    func (r *execRegistry) Register(id string, exec *activeExecution) error
    func (r *execRegistry) Get(id string) (*activeExecution, bool)
    func (r *execRegistry) SetStatus(id, status string)
    func (r *execRegistry) Unregister(id string)
    ```
*   **Logic**:
    *   `Register`: 既に同一 `sessionID` が存在すれば `ErrSessionBusy` を返す（R8）。
    *   `handleSendMessage` 開始時に `Register`、`defer` で `Unregister`。

#### [NEW] `shared/libs/go/agentservice/exec_registry_test.go`
*   **Description**: 並行登録拒否、ステータス遷移テスト。

#### [MODIFY] `shared/libs/go/agentservice/session_store.go`
*   **Description**: `suspended` の遷移ルール更新。
*   **Logic**:
    *   `isTerminalStatus` は変更なし（`suspended` は非終端）。
    *   `completed` / `error` / `closed` → `suspended` は `ErrInvalidTransition`（既存ルール維持）。
    *   `active` → `suspended` → `active` は許可。

#### [MODIFY] `shared/libs/go/agentservice/session_store_test.go`
*   **Description**: `active` ↔ `suspended` 遷移テストケース追加。

#### [MODIFY] `shared/libs/go/agentservice/handler.go`
*   **Description**: `handleSendMessage` 拡張、`handleRespond` 新設、SSE 状態遷移。
*   **Technical Design**:
    ```go
    type RespondRequest struct {
        Content string `json:"content"`
    }

    // POST /api/v1/sessions/:id/respond
    func (s *Server) handleRespond(w http.ResponseWriter, r *http.Request)

    func (s *Server) resolveAgentConfig(agentName string) config.AgentConfig
    func (s *Server) applyAgentConfig(agent codingagent.CodingAgent, cfg config.AgentConfig)
    ```
*   **Logic — `handleSendMessage` 変更**:
    1. `execRegistry.Get(sessionID)` で実行中チェック。存在すれば HTTP 409 + JSON `{"error":"session busy","status":"...","hint":"respond or terminate"}`（R8）。
    2. `resolveAgentConfig(record.AgentName)` で設定取得（R11）。
    3. `applyAgentConfig` でアダプターに `ExecutionMode` / タイムアウトを注入（`SetAgentConfig` メソッドまたは `AdapterConfig` 更新）。
    4. セッション作成後 `execRegistry.Register`。
    5. `streamSSE` 内で `EventUserInputRequired` 受信時: `record.Status = suspended`、`sessions.Update`、`execRegistry.SetStatus(suspended)`（R2）。
    6. 完了時: `record.Status = completed/error`、既存ロジック維持。

*   **Logic — `handleRespond` 新設（方式 B）**:
    1. `sessionID` をパスから抽出。
    2. `execRegistry.Get(sessionID)` で実行中エントリ取得。なければ 409。
    3. `record.Status != suspended` なら 409。
    4. `stdin.WriteStdin(req.Content)` を呼ぶ（R3）。
    5. `record.Status = active`、`sessions.Update`、`execRegistry.SetStatus(active)`。
    6. `Accept: text/event-stream` の場合、既存 `agentSess.Send` のチャネルから継続イベントを `streamSSE` で返す（新 SSE 接続）。
    7. JSON 応答モードは非対応（SSE のみ）とし、クライアント SDK は常に SSE を要求する。

#### [MODIFY] `shared/libs/go/agentservice/handler_test.go`
*   **Description**: 以下のテストを追加（モック `StdinWriter` 付き MockAgent）:
    *   `TestHandleSendMessage_ConcurrentRejected` — 409
    *   `TestHandleRespond_Success` — suspended → active、stdin 書き込み
    *   `TestHandleRespond_NotSuspended` — 409
    *   `TestStreamSSE_UserInputRequiredSetsSuspended`

#### [MODIFY] `shared/libs/go/agentservice/service.go`
*   **Description**: ルーティング追加、`execRegistry` フィールド追加。
*   **Logic**:
    ```go
    // routeSessionByID 内:
    } else if strings.HasSuffix(path, "/respond") {
        s.handleRespond(w, r)
    ```
    *   `Server` 構造体に `execRegistry *execRegistry` を追加し `New` で初期化。

---

### client/v1 パッケージ

#### [MODIFY] `client/v1/stream.go`
*   **Description**: イベント型拡張、ハンドラ、インタラクティブループ。
*   **Technical Design**:
    ```go
    const EventUserInputRequired EventType = "user_input_required"

    type UserInputRequiredEvent struct {
        Content  string
        PromptID string
        Choices  []string
    }

    type StreamHandlers struct {
        OnText              func(text string)
        OnToolUse           func(toolName string)
        OnToolResult        func(content string)
        OnUserInputRequired func(ev UserInputRequiredEvent) (response string, err error)
        OnError             func(err string) error
        OnResult            func()
    }

    func (s *Stream) RunWithHandlers(ctx context.Context, session *Session, h StreamHandlers) error
    ```
*   **Logic — イベントパース**:
    *   `events()` の raw struct に `prompt_id`, `choices` フィールドを追加。
    *   `type == "user_input_required"` を `EventUserInputRequired` にマッピング。

*   **Logic — `RunWithHandlers`（仕様書のループを具体化）**:
    ```go
    func (s *Stream) RunWithHandlers(ctx context.Context, session *Session, h StreamHandlers) error {
        stream := s
        for {
            for ev := range stream.events() {
                switch ev.Type {
                case EventText:
                    if h.OnText != nil { h.OnText(ev.Text) }
                case EventToolUse:
                    if h.OnToolUse != nil { h.OnToolUse(ev.ToolName) }
                case EventToolResult:
                    if h.OnToolResult != nil { h.OnToolResult(ev.Text) }
                case EventUserInputRequired:
                    if h.OnUserInputRequired == nil {
                        return fmt.Errorf("user input required but no handler configured")
                    }
                    answer, err := h.OnUserInputRequired(ev.UserInputRequired)
                    if err != nil { return err }
                    var err2 error
                    stream, err2 = session.Respond(ctx, answer)
                    if err2 != nil { return err2 }
                    goto nextStream
                case EventError:
                    if h.OnError != nil {
                        if err := h.OnError(ev.Error); err != nil { return err }
                    }
                    return fmt.Errorf("%s", ev.Error)
                case EventResult:
                    if h.OnResult != nil { h.OnResult() }
                }
            }
            return nil
        nextStream:
        }
    }
    ```
*   **Logic — デフォルトハンドラ未設定時**:
    *   `OnUserInputRequired == nil` → 即エラー（R9）。
    *   `OnText == nil` → 無出力（`Output` 相当のデフォルトは `SendTextWithHandlers` 側で提供）。

#### [NEW] `client/v1/stream_test.go`
*   **Description**: `RunWithHandlers` の user_input_required ループ、ハンドラ未設定時フェイルファスト。

#### [MODIFY] `client/v1/session.go`
*   **Description**: `Respond`, `SendTextWithHandlers` 追加。
*   **Technical Design**:
    ```go
    func (s *Session) Respond(ctx context.Context, content string) (*Stream, error) {
        body, _ := json.Marshal(map[string]string{"content": content})
        req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
            s.client.baseURL+"/api/v1/sessions/"+s.ID+"/respond",
            bytes.NewReader(body))
        req.Header.Set("Content-Type", "application/json")
        req.Header.Set("Accept", "text/event-stream")
        // ... error handling, return newStream(resp.Body)
    }

    func (s *Session) SendTextWithHandlers(ctx context.Context, message string, h StreamHandlers) error {
        stream, err := s.SendText(ctx, message)
        if err != nil { return err }
        if h.OnText == nil {
            h.OnText = func(text string) { fmt.Print(text) }
        }
        return stream.RunWithHandlers(ctx, s, h)
    }
    ```

---

### wayfinder パッケージ

#### [MODIFY] `shared/libs/go/wayfinder/tools/register.go`
*   **Description**: `ask_user` スキーマに `choices` 追加（R12）。
*   **Technical Design**:
    ```go
    "choices": map[string]any{
        "type": "array",
        "items": map[string]any{"type": "string"},
        "description": "Optional list of choices for the user",
    },
    ```
*   **Logic**: `choices` は required に含めない（オプション）。

#### [MODIFY] `shared/libs/go/wayfinder/tools/tool_ask_user.go`
*   **Description**: `choices` 入力の受け取りと構造化保持。
*   **Technical Design**:
    ```go
    type askUserPayload struct {
        Prompt  string
        Choices []string
    }
    ```
*   **Logic**:
    *   `input["choices"]` を `[]string` に型アサーション（`[]any` から変換）。
    *   `ErrFeedbackRequired` と共に payload を返すため、ハンドラが `choices` を参照できるよう `askUserResult` 構造体を導入する。

#### [MODIFY] `shared/libs/go/wayfinder/agent_core.go`
*   **Description**: `ask_user` 発火時に `EventUserInputRequired` を emit（R10）。
*   **Logic**:
    ```go
    if errors.Is(toolErr, tools.ErrFeedbackRequired) {
        ac.emitter.Emit(codingagent.StreamEvent{
            Type:    codingagent.EventUserInputRequired,
            Content: prompt,
            Choices: choices, // 構造化入力のみ。未指定時は nil/省略
        })
        ac.saveSession(session.StatusSuspended)
        return result, tools.ErrFeedbackRequired
    }
    ```

#### [MODIFY] `shared/libs/go/wayfinder/tools/tools_test.go`
*   **Description**: `ask_user` の `choices` 付き/なしテスト。

---

### 統合テスト

#### [NEW] `tests/interactive_agent_test.go`
*   **Description**: モックアダプターによるインタラクティブフロー検証（CLI 非依存）。
*   **Technical Design**:
    ```go
    type interactiveMockSession struct {
        phase int
        stdin []string
    }

    func (s *interactiveMockSession) WriteStdin(text string) error {
        s.stdin = append(s.stdin, text)
        return nil
    }
    ```
*   **Logic — テストケース**:
    *   `TestInteractive_UserInputRequired_MockAdapter`: SendMessage → `user_input_required` → suspended → Respond → completed
    *   `TestInteractive_ConcurrentMessageRejected`: suspended 中の SendMessage → 409
    *   `TestInteractive_IdleTimeout_MockAdapter`: 短い `idle_timeout_seconds` で `EventError`
    *   `TestInteractive_MaxExecutionTimeout_MockAdapter`: 短い `max_execution_seconds` で `EventError`
    *   `TestInteractive_ClientRunWithHandlers`: SDK コールバックループ
    *   `TestInteractive_AskUserChoices`: Wayfinder `ask_user` + choices
    *   `TestInteractive_NoHeuristicChoices`: テキストのみイベントで `choices` が空
    *   `TestInteractive_SingleShotRegression`: `single_shot` モックで stdin Close 相当の動作確認

---

## Step-by-Step Implementation Guide

1.  **イベント型とステータス（TDD: Red）**:
    *   Edit `shared/libs/go/codingagent/event_test.go` — `EventUserInputRequired` の JSON テスト追加（FAIL 確認）。
    *   Edit `shared/libs/go/agentservice/session_store_test.go` — `suspended` 遷移テスト追加（FAIL 確認）。
    *   Run `./scripts/process/build.sh --scope backend`

2.  **イベント型とステータス（TDD: Green）**:
    *   Edit `shared/libs/go/codingagent/event.go` — 定数・フィールド追加。
    *   Edit `shared/libs/go/codingagent/session_store.go` — `StatusSuspended` 追加。
    *   Edit `shared/libs/go/codingagent/execution_mode.go` — 新規作成。
    *   Run `./scripts/process/build.sh --scope backend`
    *   `git commit`

3.  **Config 拡張（TDD）**:
    *   Edit `shared/libs/go/config/model_profiles_test.go` — 新フィールドテスト。
    *   Edit `shared/libs/go/config/model_profiles.go` — `AgentConfig` 拡張、`WithDefaults`, `ResolveAgentConfig`。
    *   Edit `settings/example/model_profiles.yaml`, `tests/testdata/model_profiles.yaml`。
    *   Run `./scripts/process/build.sh --scope backend`
    *   `git commit`

4.  **StdinWriter インターフェースと codex interactive（TDD）**:
    *   Edit `shared/libs/go/codingagent/interface.go` — `StdinWriter` 追加。
    *   Edit `shared/libs/go/codingagent/codex/input_detect_test.go` — 検出テスト（Red）。
    *   Edit `shared/libs/go/codingagent/codex/input_detect.go` — 新規。
    *   Edit `shared/libs/go/codingagent/codex/process_test.go` — interactive/single_shot テスト（Red）。
    *   Edit `shared/libs/go/codingagent/codex/process.go` — `ProcessOptions`, `WriteStdin`, ウォッチドッグ。
    *   Edit `shared/libs/go/codingagent/codex/adapter.go`, `adapter_config.go`。
    *   Run `./scripts/process/build.sh --scope backend`
    *   `git commit`

5.  **claudecode interactive（TDD）**:
    *   Edit `shared/libs/go/codingagent/claudecode/process_test.go` — Red。
    *   Edit `shared/libs/go/codingagent/claudecode/process.go`, `adapter.go`。
    *   Run `./scripts/process/build.sh --scope backend`
    *   `git commit`

6.  **agentservice exec registry と respond API（TDD）**:
    *   Edit `shared/libs/go/agentservice/exec_registry_test.go` — Red。
    *   Edit `shared/libs/go/agentservice/exec_registry.go` — 新規。
    *   Edit `shared/libs/go/agentservice/handler_test.go` — respond/409 テスト Red。
    *   Edit `shared/libs/go/agentservice/handler.go` — `handleRespond`, `handleSendMessage` 拡張。
    *   Edit `shared/libs/go/agentservice/service.go` — ルーティング。
    *   Run `./scripts/process/build.sh --scope backend`
    *   `git commit`

7.  **クライアント SDK（TDD）**:
    *   Edit `client/v1/stream_test.go` — Red。
    *   Edit `client/v1/stream.go` — パース、`StreamHandlers`, `RunWithHandlers`。
    *   Edit `client/v1/session.go` — `Respond`, `SendTextWithHandlers`。
    *   Run `./scripts/process/build.sh --scope backend`
    *   `git commit`

8.  **Wayfinder ask_user 統合（TDD）**:
    *   Edit `shared/libs/go/wayfinder/tools/tools_test.go` — choices テスト Red。
    *   Edit `shared/libs/go/wayfinder/tools/register.go`, `tool_ask_user.go`, `agent_core.go`。
    *   Run `./scripts/process/build.sh --scope backend`
    *   `git commit`

9.  **統合テスト**:
    *   Edit `tests/interactive_agent_test.go` — 新規（モックアダプター）。
    *   Run `./scripts/process/build.sh --scope backend`
    *   Run `./scripts/process/integration_test.sh --specify "TestInteractive"`
    *   `git commit`

10. **E2E リグレッション**:
    *   Run `./scripts/process/build.sh`
    *   Run `./scripts/process/integration_test.sh --specify "TestCodexE2E|TestE2E_CodingAgent"`
    *   Run `./scripts/process/integration_test.sh --specify "TestE2E_Wayfinder"`
    *   `git commit`（必要に応じて E2E config 修正のみ）

11. **Verification Plan を実行**（下記参照）。

---

## Verification Plan

### Build Scope

**本計画の Build Scope**: `backend`

| 実装対象 | 中間ビルド (開発中) | 最終検証 |
| :--- | :--- | :--- |
| Go (Backend) のみ | `./scripts/process/build.sh --scope backend` | `./scripts/process/build.sh` |

### Automated Verification

1.  **Build & Unit Tests（最終）**:
    ```bash
    ./scripts/process/build.sh
    ```

2.  **Integration Tests（新規インタラクティブ）**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestInteractive"
    ```
    *   **Log Verification**:
        *   `user input required detected` がサーバーログに出力されること
        *   `suspended` → `active` ステータス遷移が Debug ログに記録されること
        *   idle / max execution タイムアウト時に PID と経過秒数がログに残ること

3.  **Integration Tests（既存 E2E リグレッション）**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestCodexE2E|TestE2E_CodingAgent"
    ```
    *   `tests/testdata/model_profiles.yaml` の `execution_mode: single_shot` により既存テストが通ること

4.  **Integration Tests（Wayfinder）**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestE2E_Wayfinder"
    ```

### テスト項目設計のセルフレビュー (Section 11)

テスト項目は以下のボトムアップ順序で設計:

1. **末端 (event/config)**: `EventUserInputRequired` JSON、`AgentConfig.WithDefaults`、`NormalizeExecutionMode`
2. **中間 (process/registry)**: codex/claudecode stdin 分岐、`execRegistry` 409、`WriteStdin`
3. **ハンドラ (handler)**: `handleRespond`、`streamSSE` suspended 遷移
4. **クライアント (SDK)**: `RunWithHandlers` ループ、フェイルファスト
5. **統合 (TestInteractive_*)**: モックアダプターによる end-to-end API フロー
6. **E2E リグレッション**: 既存 Codex/Claude Code E2E（`single_shot` config）

**観点チェックリスト:**

| # | 観点 | 対応状況 |
|---|------|----------|
| 1 | 正常系 | `TestInteractive_UserInputRequired_MockAdapter`, `TestInteractive_ClientRunWithHandlers` |
| 2 | 異常系/境界値 | 409 並行拒否、respond 非 suspended 409、ハンドラ未設定フェイルファスト |
| 3 | 外部連携 | E2E リグレッション（実 CLI、`single_shot`） |
| 4 | データ一貫性 | `choices` は Wayfinder 構造化入力のみ。`TestInteractive_NoHeuristicChoices` |
| 5 | 状態遷移 | active → suspended → active → completed |
| 6 | 設定反映 | `execution_mode` 分岐テスト、`tests/testdata` の `single_shot` |
| 7 | 副作用 | stdin Close（single_shot）、stdin 保持（interactive）、タイムアウト時 Stop |

**セルフレビュー結果 (Section 11.4):**

1. **網羅性**: R1〜R12 をユニット・統合・E2E の 3 層でカバー。方式 A は先送り理由を Traceability に明記。
2. **証拠の十分性**: モック統合テストが API フロー全体を検証。実 CLI はリグレッション E2E で担保。
3. **迂回排除**: `single_shot` では stdin 即 Close のテストで迂回を防止。
4. **依存関係**: event → config → process → handler → client → integration の順序を Step-by-Step で強制。

### 総合判定プロセス (Section 12)

全テスト完了後、以下を確認する:

1. スキップされたテストがないか（特に Codex/Claude CLI 未検出による Skip）
2. `TestInteractive_*` がすべて PASS しているか
3. 既存 `TestCodexE2E_*` / `TestE2E_*` が `single_shot` config で PASS しているか
4. サーバーログに `user input required detected` とステータス遷移ログが記録されるか
5. `choices` がテキスト解析なしで Wayfinder 構造化入力からのみ設定されるか（`TestInteractive_NoHeuristicChoices`）

---

## Documentation

#### [MODIFY] `README.md`
*   **更新内容**: 仕様レビュー時点でインタラクティブ実行の概要は追記済み。実装完了後、以下を再確認する:
    *   `Respond` / `SendTextWithHandlers` の API シグネチャが実装と一致しているか
    *   Roadmap の `Interactive agent execution` を `[x]` に更新するか判断
