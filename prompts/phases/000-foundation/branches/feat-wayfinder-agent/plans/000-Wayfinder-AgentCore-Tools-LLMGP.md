# 000-Wayfinder-AgentCore-Tools-LLMGP

> **Source Specification**:
> - [000-Wayfinder-Agent-Overview.md](file://prompts/phases/000-foundation/branches/feat-wayfinder-agent/ideas/000-Wayfinder-Agent-Overview.md)
> - [003-Wayfinder-Guardrails-and-LLMGP-Integration.md](file://prompts/phases/000-foundation/branches/feat-wayfinder-agent/ideas/003-Wayfinder-Guardrails-and-LLMGP-Integration.md)

## Goal Description

Wayfinder Agentの基盤層を構築する。具体的には以下を実装する:

1. **エージェントコア (`AgentCore`)**: ユーザー指示を受けてLLMとの思考ループを駆動し、Tool Callingを繰り返して最終回答を生成するRunループ。`WorkDir` / `SessionDir` のフォールバック・絶対パス正規化を含む初期化ロジック。
2. **ツールレジストリおよびツール実装**: `read_file`, `write_file`, `list_directory`, `create_directory`, `edit_file`, `search_files`, `grep_files`, `execute_command`, `kill_process` の各ツールハンドラ。
3. **ガードレール**: `ValidatePath` によるパス境界検証、危険コマンドブロック、所有権/自己生成オブジェクト制限。シェル演算子はブロックしない。
4. **LLMGP/Bifrost統合**: エージェントコアからLLMへの接続を `LLMClient` インターフェース経由で行い、Bifrostクライアントをアダプタとして実装。

> **注**: セッション永続化(Part 2)、サブエージェント(Part 3)、WBS計画(Part 4)は後続の計画で実装する。本計画のAgentCoreは、これらの拡張ポイントを考慮したインターフェース設計を行うが、スタブ実装に留める。

## User Review Required

> [!IMPORTANT]
> **パッケージ配置**: Wayfinder Agentのコードを `shared/libs/go/wayfinder/` に配置する方針としています。既存の `codingagent` パッケージとは独立したパッケージとして新設します。Wayfinder AgentはGoライブラリとして設計され、Ternのコードから直接Goパッケージとしてimportして使用します。スタンドアロンCLIは作成しません。

## Requirement Traceability

> **Traceability Check**:
> 仕様書000および003の要件を本計画にマッピングする。

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| ライブラリモジュール化 (000) | Proposed Changes > wayfinder パッケージ (Goライブラリとして提供、CLIは作成しない) |
| Tool Callingの実装 (000) | Proposed Changes > tools パッケージ (全9ツール) |
| WorkDir/SessionDirの初期化・フォールバック・絶対パス化 (000) | Proposed Changes > config.go |
| シェル演算子ブラックリストの廃止 (003) | Proposed Changes > guardrail.go (演算子チェックなし) |
| 危険コマンドのブロック (003) | Proposed Changes > guardrail.go > `isBlockedCommand` |
| パス境界検証 ValidatePath (003) | Proposed Changes > guardrail.go > `ValidatePath` |
| コマンドの実行ディレクトリ (003) | Proposed Changes > tool_execute_command.go > `Cmd.Dir = WorkDir` |
| 所有権・自己生成オブジェクト制限 (003) | Proposed Changes > guardrail.go > `CheckFileOwnership` |
| 削除許可リストの永続化 (003) | **Part 2で実装** (本計画ではインメモリ FileTracker のみ) |
| 適合パスパターン (003) | Proposed Changes > guardrail.go > `MatchesAllowedPattern` + config.go > `AllowedPathPatterns` |
| セッション復旧時のトラッカー整合性検証 (003) | **Part 2で実装** (本計画では FileTracker の構造体定義のみ) |
| 直接API/SDK依存の排除 (003) | Proposed Changes > llm_client.go (interface) |
| LLMGP/Bifrostクライアント呼び出し (003) | Proposed Changes > llm_bifrost.go (adapter) |
| 論理モデル名によるモデル指定 (003) | Proposed Changes > llm_client.go > `GenerateMessage` の `logicalModel` 引数 |
| 実行ブランチの分岐 (000) | **Part 4で実装** (本計画では単純実行ルートのみ) |
| サブエージェント連携 (000) | **Part 3で実装** (本計画ではインターフェースのみ) |
| セッション永続化 (000) | **Part 2で実装** (本計画ではインメモリのみ) |

## Proposed Changes

### wayfinder コアパッケージ

#### [NEW] [config.go](file://shared/libs/go/wayfinder/config.go)
*   **Description**: エージェント設定値の定義と初期化ロジック
*   **Technical Design**:
    ```go
    package wayfinder

    import (
        "os"
        "path/filepath"
    )

    // AgentConfig holds the runtime configuration for Wayfinder Agent.
    type AgentConfig struct {
        WorkDir             string   // Working directory (absolute path after init)
        SessionDir          string   // Session data directory (absolute path after init)
        SessionID           string   // Session ID for resume
        Model               string   // Logical model name (e.g. "claude", "gemini")
        AllowedPathPatterns []string // Regex patterns for deletion permission (default: WorkDir subtree)
    }

    // InitConfig resolves defaults and normalizes paths.
    // WorkDir default: current working directory
    // SessionDir default: WorkDir/.claudecode
    // Both are resolved to absolute paths.
    func InitConfig(cfg *AgentConfig) error {
        if cfg.WorkDir == "" {
            cwd, err := os.Getwd()
            if err != nil {
                return fmt.Errorf("failed to get working directory: %w", err)
            }
            cfg.WorkDir = cwd
        }
        abs, err := filepath.Abs(cfg.WorkDir)
        if err != nil {
            return fmt.Errorf("failed to resolve WorkDir: %w", err)
        }
        cfg.WorkDir = abs

        if cfg.SessionDir == "" {
            cfg.SessionDir = filepath.Join(cfg.WorkDir, ".claudecode")
        }
        abs, err = filepath.Abs(cfg.SessionDir)
        if err != nil {
            return fmt.Errorf("failed to resolve SessionDir: %w", err)
        }
        cfg.SessionDir = abs
        return nil
    }
    ```

#### [NEW] [agent_core.go](file://shared/libs/go/wayfinder/agent_core.go)
*   **Description**: エージェントの思考ループ (Run loop) を制御するコア構造体
*   **Technical Design**:
    ```go
    package wayfinder

    import (
        "context"
        "fmt"
    )

    // AgentCore drives the thinking loop: LLM -> Tool Call -> LLM -> ... -> Final Answer.
    type AgentCore struct {
        config   *AgentConfig
        llm      LLMClient
        tools    *ToolRegistry
        tracker  *FileTracker
        logger   Logger
    }

    // NewAgentCore creates a new AgentCore.
    func NewAgentCore(cfg *AgentConfig, llm LLMClient, tools *ToolRegistry, logger Logger) *AgentCore {
        return &AgentCore{
            config:  cfg,
            llm:     llm,
            tools:   tools,
            tracker: NewFileTracker(),
            logger:  logger,
        }
    }

    // Run executes the agent loop for a single user prompt.
    // It maintains an in-memory message history for this invocation.
    // Returns the final assistant text response.
    func (a *AgentCore) Run(ctx context.Context, prompt string) (string, error) {
        messages := []ChatMessage{
            {Role: "system", Content: systemPrompt(a.tools)},
            {Role: "user", Content: prompt},
        }

        for {
            resp, err := a.llm.GenerateMessage(ctx, a.config.Model, messages, a.tools.Definitions())
            if err != nil {
                return "", fmt.Errorf("llm generate failed: %w", err)
            }

            // If response is final text (no tool calls), return it
            if len(resp.ToolCalls) == 0 {
                return resp.Content, nil
            }

            // Append assistant message with tool calls
            messages = append(messages, ChatMessage{
                Role:      "assistant",
                Content:   resp.Content,
                ToolCalls: resp.ToolCalls,
            })

            // Execute each tool call
            for _, tc := range resp.ToolCalls {
                result, toolErr := a.executeTool(ctx, tc)
                resultContent := result
                if toolErr != nil {
                    resultContent = fmt.Sprintf("Error: %v", toolErr)
                }
                messages = append(messages, ChatMessage{
                    Role:       "tool",
                    Content:    resultContent,
                    ToolCallID: tc.ID,
                })
            }
        }
    }

    // executeTool dispatches a tool call to the registry.
    func (a *AgentCore) executeTool(ctx context.Context, tc ToolCall) (string, error) {
        tool, ok := a.tools.Get(tc.Name)
        if !ok {
            return "", fmt.Errorf("unknown tool: %s", tc.Name)
        }
        return tool.Handler(ctx, a.config.WorkDir, tc.Input, a.tracker)
    }
    ```
*   **Logic**:
    *   `Run` は無限ループで LLM に問い合わせ、ToolCall がなくなるまで繰り返す。
    *   各ToolCallの実行結果は `role: "tool"` メッセージとして履歴に追加し、次のLLMリクエストに含める。
    *   `systemPrompt(tools)` はツール定義一覧からシステムプロンプトを自動生成する関数。

#### [NEW] [llm_client.go](file://shared/libs/go/wayfinder/llm_client.go)
*   **Description**: LLM接続の抽象インターフェース定義
*   **Technical Design**:
    ```go
    package wayfinder

    import "context"

    // ChatMessage represents a single message in the conversation.
    type ChatMessage struct {
        Role       string     `json:"role"`         // "system", "user", "assistant", "tool"
        Content    string     `json:"content"`
        ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
        ToolCallID string     `json:"tool_call_id,omitempty"`
    }

    // ToolCall represents a tool invocation requested by the LLM.
    type ToolCall struct {
        ID    string         `json:"id"`
        Name  string         `json:"name"`
        Input map[string]any `json:"input"`
    }

    // ToolDefinition describes a tool for the LLM.
    type ToolDefinition struct {
        Name        string         `json:"name"`
        Description string         `json:"description"`
        InputSchema map[string]any `json:"input_schema"`
    }

    // LLMResponse is the response from the LLM.
    type LLMResponse struct {
        Content   string     `json:"content"`
        ToolCalls []ToolCall `json:"tool_calls,omitempty"`
    }

    // LLMClient is the abstract interface for LLM communication.
    // Implementations connect to Bifrost/LLMGP or mock for testing.
    type LLMClient interface {
        // GenerateMessage sends messages to the specified logical model
        // and returns a response that may contain text and/or tool calls.
        GenerateMessage(ctx context.Context, logicalModel string, messages []ChatMessage, tools []ToolDefinition) (*LLMResponse, error)
    }
    ```

#### [NEW] [llm_bifrost.go](file://shared/libs/go/wayfinder/llm_bifrost.go)
*   **Description**: LLMGP/Bifrost経由のLLMClient実装
*   **Technical Design**:
    ```go
    package wayfinder

    import (
        "context"
        "fmt"
    )

    // BifrostClient implements LLMClient using LLMGP/Bifrost proxy.
    type BifrostClient struct {
        gatewayURL   string
        gatewayToken string
        logger       Logger
    }

    // NewBifrostClient creates a Bifrost-backed LLM client.
    func NewBifrostClient(gatewayURL, gatewayToken string, logger Logger) *BifrostClient {
        return &BifrostClient{
            gatewayURL:   gatewayURL,
            gatewayToken: gatewayToken,
            logger:       logger,
        }
    }

    func (b *BifrostClient) GenerateMessage(ctx context.Context, logicalModel string, messages []ChatMessage, tools []ToolDefinition) (*LLMResponse, error) {
        // 1. Convert ChatMessage/ToolDefinition to Anthropic Messages API format
        // 2. POST to b.gatewayURL + "/v1/messages" with Authorization header
        // 3. Parse response, extract text content and tool_use blocks
        // 4. Convert to LLMResponse
        // Implementation details: use net/http, JSON marshal/unmarshal
        // logicalModel is passed as the "model" field in the API request body
        return nil, fmt.Errorf("not implemented") // placeholder
    }
    ```
*   **Logic**:
    *   `logicalModel` はリクエストボディの `model` フィールドとしてBifrostに送信される。
    *   Bifrost側がモデル名を実際のエンドポイントにルーティングする。
    *   認証は `Authorization: Bearer <gatewayToken>` ヘッダで行う。

---

### wayfinder/tools ツールパッケージ

#### [NEW] [tool_registry.go](file://shared/libs/go/wayfinder/tools/tool_registry.go)
*   **Description**: ツールの登録・取得・一覧を管理するレジストリ
*   **Technical Design**:
    ```go
    package tools

    import (
        "context"
        "sync"
    )

    // ToolHandler executes a tool with the given working directory, input, and file tracker.
    type ToolHandler func(ctx context.Context, workDir string, input map[string]any, tracker *FileTracker) (string, error)

    // Tool represents a registered tool.
    type Tool struct {
        Name        string
        Description string
        InputSchema map[string]any
        Handler     ToolHandler
    }

    // ToolRegistry manages registered tools.
    type ToolRegistry struct {
        tools map[string]*Tool
        mu    sync.RWMutex
    }

    func NewToolRegistry() *ToolRegistry {
        return &ToolRegistry{tools: make(map[string]*Tool)}
    }

    func (r *ToolRegistry) Register(t *Tool) {
        r.mu.Lock()
        defer r.mu.Unlock()
        r.tools[t.Name] = t
    }

    func (r *ToolRegistry) Get(name string) (*Tool, bool) {
        r.mu.RLock()
        defer r.mu.RUnlock()
        t, ok := r.tools[name]
        return t, ok
    }

    // Definitions returns tool definitions for LLM.
    func (r *ToolRegistry) Definitions() []ToolDefinition {
        r.mu.RLock()
        defer r.mu.RUnlock()
        defs := make([]ToolDefinition, 0, len(r.tools))
        for _, t := range r.tools {
            defs = append(defs, ToolDefinition{
                Name:        t.Name,
                Description: t.Description,
                InputSchema: t.InputSchema,
            })
        }
        return defs
    }
    ```

#### [NEW] [guardrail.go](file://shared/libs/go/wayfinder/tools/guardrail.go)
*   **Description**: パス検証、コマンドブロック、所有権チェックのガードレール関数群
*   **Technical Design**:
    ```go
    package tools

    import (
        "fmt"
        "os"
        "path/filepath"
        "runtime"
        "strings"
    )

    // blockedCommands is the set of dangerous system commands to block.
    var blockedCommands = map[string]bool{
        "su": true, "sudo": true, "format": true, "mkfs": true,
        "shutdown": true, "reboot": true, "passwd": true,
        "useradd": true, "userdel": true, "init": true,
        "systemctl": true, "dd": true,
    }

    // ValidatePath validates that the resolved path is within workDir boundary.
    // Returns the absolute resolved path or error if boundary is violated.
    func ValidatePath(workDir, requestedPath string) (string, error) {
        cleanWorkDir, err := filepath.Abs(filepath.Clean(workDir))
        if err != nil {
            return "", fmt.Errorf("failed to resolve working directory: %w", err)
        }

        var targetPath string
        if filepath.IsAbs(requestedPath) {
            targetPath = filepath.Clean(requestedPath)
        } else {
            targetPath = filepath.Join(cleanWorkDir, requestedPath)
        }

        absTarget, err := filepath.Abs(targetPath)
        if err != nil {
            return "", fmt.Errorf("failed to resolve target path: %w", err)
        }

        normTarget := normalizeForComparison(absTarget)
        normWorkDir := normalizeForComparison(cleanWorkDir)

        if !strings.HasPrefix(normTarget, normWorkDir) {
            return "", fmt.Errorf("path traversal detected: %s is outside working directory %s", absTarget, cleanWorkDir)
        }

        // Symlink escape check
        evalPath, err := filepath.EvalSymlinks(absTarget)
        if err != nil {
            parentDir := filepath.Dir(absTarget)
            if _, statErr := os.Stat(parentDir); statErr == nil {
                evalParent, evalErr := filepath.EvalSymlinks(parentDir)
                if evalErr != nil {
                    return "", fmt.Errorf("failed to evaluate parent directory: %w", evalErr)
                }
                if !strings.HasPrefix(normalizeForComparison(evalParent), normWorkDir) {
                    return "", fmt.Errorf("symlink escape: parent %s outside working directory", evalParent)
                }
            }
            return absTarget, nil
        }

        if !strings.HasPrefix(normalizeForComparison(evalPath), normWorkDir) {
            return "", fmt.Errorf("symlink escape: %s resolves to %s outside working directory", absTarget, evalPath)
        }
        return absTarget, nil
    }

    func normalizeForComparison(p string) string {
        if runtime.GOOS == "darwin" {
            s := filepath.ToSlash(p)
            if s == "/private/var" || strings.HasPrefix(s, "/private/var/") {
                return "/var" + strings.TrimPrefix(s, "/private/var")
            }
            if s == "/private/tmp" || strings.HasPrefix(s, "/private/tmp/") {
                return "/tmp" + strings.TrimPrefix(s, "/private/tmp")
            }
            return s
        }
        return p
    }

    // IsBlockedCommand checks if a command is in the blocked list.
    // Shell operators (|, &&, ||, >, <, ;) are NOT blocked.
    func IsBlockedCommand(command string) bool {
        cmd := strings.ToLower(strings.TrimSpace(command))
        return blockedCommands[cmd]
    }

    // CheckFileOwnership verifies rm/chmod operations are allowed.
    // Allowed if: (a) tracked by FileTracker, (b) matches allowed path pattern, or (c) owned by current user.
    func CheckFileOwnership(path string, tracker *FileTracker, allowedPatterns []*regexp.Regexp) error {
        if tracker.IsTracked(path) {
            return nil // agent created this file
        }
        if MatchesAllowedPattern(path, allowedPatterns) {
            return nil // path matches allowed pattern (e.g. WorkDir subtree)
        }
        info, err := os.Stat(path)
        if err != nil {
            return fmt.Errorf("cannot stat path: %w", err)
        }
        if !isOwnedByCurrentUser(info) {
            return fmt.Errorf("permission denied: cannot modify untracked/unowned path: %s", path)
        }
        return nil
    }

    // MatchesAllowedPattern checks if an absolute path matches any allowed regex pattern.
    func MatchesAllowedPattern(absPath string, patterns []*regexp.Regexp) bool {
        for _, p := range patterns {
            if p.MatchString(absPath) {
                return true
            }
        }
        return false
    }

    // CompileAllowedPatterns compiles string patterns to regexp.
    // Invalid patterns are skipped with a warning log.
    func CompileAllowedPatterns(patterns []string) []*regexp.Regexp {
        compiled := make([]*regexp.Regexp, 0, len(patterns))
        for _, p := range patterns {
            re, err := regexp.Compile(p)
            if err != nil {
                continue
            }
            compiled = append(compiled, re)
        }
        return compiled
    }
    ```

#### [NEW] [file_tracker.go](file://shared/libs/go/wayfinder/tools/file_tracker.go)
*   **Description**: エージェントが作成したファイル/ディレクトリを追跡するトラッカー
*   **Technical Design**:
    ```go
    package tools

    import (
        "sync"
        "time"
    )

    // TrackedFile represents a file created by the agent.
    type TrackedFile struct {
        Path      string    `json:"path"`
        CreatedAt time.Time `json:"created_at"`
        IsDir     bool      `json:"is_dir"`
    }

    // FileTracker tracks files and directories created by the agent.
    type FileTracker struct {
        files map[string]*TrackedFile
        mu    sync.RWMutex
    }

    func NewFileTracker() *FileTracker {
        return &FileTracker{files: make(map[string]*TrackedFile)}
    }

    func (ft *FileTracker) Track(path string, isDir bool) {
        ft.mu.Lock()
        defer ft.mu.Unlock()
        ft.files[path] = &TrackedFile{
            Path: path, CreatedAt: time.Now(), IsDir: isDir,
        }
    }

    func (ft *FileTracker) IsTracked(path string) bool {
        ft.mu.RLock()
        defer ft.mu.RUnlock()
        _, ok := ft.files[path]
        return ok
    }

    func (ft *FileTracker) All() []*TrackedFile {
        ft.mu.RLock()
        defer ft.mu.RUnlock()
        result := make([]*TrackedFile, 0, len(ft.files))
        for _, f := range ft.files {
            result = append(result, f)
        }
        return result
    }
    ```

#### [NEW] [tool_read_file.go](file://shared/libs/go/wayfinder/tools/tool_read_file.go)
*   **Description**: `read_file` ツールハンドラ。WorkDir基準でパスを解決し、ValidatePathでチェック後にファイル内容を返す。
*   **Logic**:
    *   入力: `{"path": "relative/or/absolute/path"}`
    *   `ValidatePath(workDir, path)` で検証 -> エラーなら拒否
    *   `os.ReadFile(absPath)` で読み込み、内容を文字列で返却
    *   ファイルサイズ上限チェック (10MB)

#### [NEW] [tool_write_file.go](file://shared/libs/go/wayfinder/tools/tool_write_file.go)
*   **Description**: `write_file` ツールハンドラ。ファイルを作成/上書きし、FileTrackerに登録する。
*   **Logic**:
    *   入力: `{"path": "...", "content": "..."}`
    *   `ValidatePath` + サイズチェック
    *   親ディレクトリの自動作成 (`os.MkdirAll`)
    *   `os.WriteFile` で書き込み
    *   `tracker.Track(absPath, false)`

#### [NEW] [tool_list_directory.go](file://shared/libs/go/wayfinder/tools/tool_list_directory.go)
*   **Description**: `list_directory` ツールハンドラ。ディレクトリ内容をリスト形式で返す。
*   **Logic**:
    *   入力: `{"path": "..."}`
    *   `ValidatePath` でチェック
    *   `os.ReadDir` でエントリ取得、最大1000件
    *   各エントリを `[dir] name` or `[file] name (size)` 形式でフォーマット

#### [NEW] [tool_create_directory.go](file://shared/libs/go/wayfinder/tools/tool_create_directory.go)
*   **Description**: `create_directory` ツールハンドラ。
*   **Logic**: `ValidatePath` -> `os.MkdirAll` -> `tracker.Track(absPath, true)`

#### [NEW] [tool_edit_file.go](file://shared/libs/go/wayfinder/tools/tool_edit_file.go)
*   **Description**: `edit_file` ツールハンドラ。ユニークな文字列を検索して置換する。
*   **Logic**:
    *   入力: `{"path": "...", "old_text": "...", "new_text": "..."}`
    *   `ValidatePath` -> ファイル読み込み
    *   `old_text` の出現回数を確認。0回ならエラー、2回以上ならエラー（ユニーク性の保証）
    *   `strings.Replace(content, oldText, newText, 1)` で置換
    *   ファイルに書き戻し

#### [NEW] [tool_search_files.go](file://shared/libs/go/wayfinder/tools/tool_search_files.go)
*   **Description**: `search_files` ツールハンドラ。ファイル名のパターン検索。
*   **Logic**:
    *   入力: `{"pattern": "*.go", "path": "."}`
    *   `filepath.WalkDir` + `filepath.Match` で検索
    *   結果を最大100件に制限

#### [NEW] [tool_grep_files.go](file://shared/libs/go/wayfinder/tools/tool_grep_files.go)
*   **Description**: `grep_files` ツールハンドラ。ファイル内テキスト検索。
*   **Logic**:
    *   入力: `{"query": "search term", "path": "."}`
    *   `filepath.WalkDir` でファイルを走査
    *   各ファイルを行単位で読み、クエリとマッチする行を収集
    *   結果を最大100件に制限し `file:line: content` 形式で返却

#### [NEW] [tool_execute_command.go](file://shared/libs/go/wayfinder/tools/tool_execute_command.go)
*   **Description**: `execute_command` ツールハンドラ。フォアグラウンド/バックグラウンドでコマンドを実行する。
*   **Technical Design**:
    ```go
    func executeCommandHandler(ctx context.Context, workDir string, input map[string]any, tracker *FileTracker) (string, error) {
        commandLine, _ := input["command_line"].(string)
        background, _ := input["background"].(bool)

        // 1. Parse command name (first token before shell operators)
        command := extractBaseCommand(commandLine)

        // 2. Blocked command check (shell operators are NOT checked)
        if IsBlockedCommand(command) {
            return "", fmt.Errorf("permission denied: blocked command: %s", command)
        }

        // 3. Ownership check for rm/chmod
        if command == "rm" || command == "chmod" {
            paths := extractPathArgs(commandLine)
            for _, p := range paths {
                absP, err := ValidatePath(workDir, p)
                if err != nil { return "", err }
                if err := CheckFileOwnership(absP, tracker); err != nil {
                    return "", err
                }
            }
        }

        // 4. chown check
        if command == "chown" {
            if isTargetingOtherUser(commandLine) {
                return "", fmt.Errorf("permission denied: chown to other users is not allowed")
            }
        }

        // 5. Execute via sh -c with Cmd.Dir = workDir
        cmd := exec.CommandContext(ctx, "sh", "-c", commandLine)
        cmd.Dir = workDir
        // ...
    }
    ```

#### [NEW] [tool_kill_process.go](file://shared/libs/go/wayfinder/tools/tool_kill_process.go)
*   **Description**: `kill_process` ツールハンドラ。PIDを指定してプロセスを終了する。
*   **Logic**:
    *   入力: `{"pid": 12345}`
    *   `os.FindProcess(pid)` -> `process.Kill()`

---

### テストファイル (TDD: テストを先に記述)

#### [NEW] [config_test.go](file://shared/libs/go/wayfinder/config_test.go)
*   **Description**: `InitConfig` のユニットテスト
*   **テストケース**:
    *   `TestInitConfig_DefaultWorkDir`: WorkDir空 -> `os.Getwd()` の絶対パスになる
    *   `TestInitConfig_DefaultSessionDir`: SessionDir空 -> `WorkDir/.claudecode` になる
    *   `TestInitConfig_ExplicitPaths`: 両方指定 -> そのまま絶対パス化
    *   `TestInitConfig_RelativeWorkDir`: 相対パス -> 絶対パスに解決

#### [NEW] [guardrail_test.go](file://shared/libs/go/wayfinder/tools/guardrail_test.go)
*   **Description**: ガードレール関数のユニットテスト
*   **テストケース**:
    *   `TestValidatePath_WithinBoundary`: WorkDir内のパス -> 成功
    *   `TestValidatePath_TraversalAttack`: `../../etc/passwd` -> エラー
    *   `TestValidatePath_AbsoluteOutside`: `/etc/shadow` -> エラー
    *   `TestIsBlockedCommand_Sudo`: `sudo` -> true
    *   `TestIsBlockedCommand_NormalCommand`: `ls` -> false
    *   `TestIsBlockedCommand_ShellOperators`: パイプ含むコマンド -> false (ブロックされない)
    *   `TestCheckFileOwnership_TrackedFile`: tracker登録済み -> 許可
    *   `TestCheckFileOwnership_UntrackedFile`: 未登録かつパターン不一致 -> エラー
    *   `TestMatchesAllowedPattern_WorkDirMatch`: WorkDir配下パス -> マッチ成功
    *   `TestMatchesAllowedPattern_OutsideWorkDir`: WorkDir外パス -> マッチ失敗
    *   `TestMatchesAllowedPattern_CustomPattern`: カスタム正規表現パターン -> マッチ検証
    *   `TestCheckFileOwnership_AllowedPatternMatch`: 未登録だがパターンマッチ -> 許可

#### [NEW] [tool_read_file_test.go](file://shared/libs/go/wayfinder/tools/tool_read_file_test.go)
*   **テストケース**: テーブル駆動テストで正常読み込み、存在しないファイル、サイズ超過、パス境界外を検証

#### [NEW] [tool_write_file_test.go](file://shared/libs/go/wayfinder/tools/tool_write_file_test.go)
*   **テストケース**: 正常書き込み、親ディレクトリ自動作成、パス境界外拒否、FileTracker登録確認

#### [NEW] [tool_execute_command_test.go](file://shared/libs/go/wayfinder/tools/tool_execute_command_test.go)
*   **テストケース**: 正常実行、パイプ許可、危険コマンドブロック、rm/chmod所有権チェック、Cmd.Dir=WorkDir検証

#### [NEW] [tool_edit_file_test.go](file://shared/libs/go/wayfinder/tools/tool_edit_file_test.go)
*   **テストケース**: ユニーク文字列置換成功、非ユニーク(複数出現)エラー、見つからないエラー

#### [NEW] [agent_core_test.go](file://shared/libs/go/wayfinder/agent_core_test.go)
*   **Description**: AgentCoreのRunループテスト。MockLLMClientを使用。
*   **テストケース**:
    *   `TestAgentCore_SimpleResponse`: ToolCallなしの直接回答
    *   `TestAgentCore_SingleToolCall`: 1回のToolCall -> 結果 -> 最終回答
    *   `TestAgentCore_MultipleToolCalls`: 複数回のToolCall -> 最終回答
    *   `TestAgentCore_UnknownTool`: 未知のツール名 -> エラーメッセージがLLMに返る

#### [NEW] [llm_client_test.go](file://shared/libs/go/wayfinder/llm_client_test.go)
*   **Description**: MockLLMClientの実装とインターフェース準拠テスト

## Step-by-Step Implementation Guide

1.  **Go moduleの初期化**:
    *   `shared/libs/go/wayfinder/` ディレクトリを作成
    *   `go.mod` は既存の `shared/libs/go/go.mod` に含まれるため、パッケージパスを `github.com/axsh/arctic-tern/wayfinder` として追加

2.  **LLMClientインターフェースとモック実装** (TDD: テスト先行):
    *   `llm_client.go` に `LLMClient`, `ChatMessage`, `ToolCall`, `ToolDefinition`, `LLMResponse` を定義
    *   `llm_client_test.go` にモックLLMClientを作成しインターフェース準拠テスト
    *   `git commit -m "feat(wayfinder): add LLMClient interface and mock"`

3.  **AgentConfig と初期化ロジック** (TDD: テスト先行):
    *   `config_test.go` を作成し、4つのテストケースを記述 -> 失敗確認
    *   `config.go` に `AgentConfig` 構造体と `InitConfig` 関数を実装
    *   `git commit -m "feat(wayfinder): add AgentConfig with path resolution"`

4.  **ガードレール実装** (TDD: テスト先行):
    *   `guardrail_test.go` を作成し、全テストケースを記述 -> 失敗確認
    *   `guardrail.go` に `ValidatePath`, `IsBlockedCommand`, `CheckFileOwnership` を実装
    *   `file_tracker.go` に `FileTracker` を実装
    *   `git commit -m "feat(wayfinder): add guardrail (ValidatePath, blocked commands, ownership)"`

5.  **ツールレジストリ + 全ツール実装** (TDD: テスト先行):
    *   `tool_registry.go` を実装
    *   各ツールテストファイルを作成 -> 失敗確認
    *   `tool_read_file.go`, `tool_write_file.go`, `tool_list_directory.go`, `tool_create_directory.go`, `tool_edit_file.go`, `tool_search_files.go`, `tool_grep_files.go`, `tool_execute_command.go`, `tool_kill_process.go` を順次実装
    *   `git commit -m "feat(wayfinder): add tool registry and all tool handlers"`

6.  **AgentCore Runループ実装** (TDD: テスト先行):
    *   `agent_core_test.go` を作成し、4つのテストケースを記述 -> 失敗確認
    *   `agent_core.go` にRunループを実装
    *   `git commit -m "feat(wayfinder): add AgentCore run loop"`

7.  **BifrostClient実装**:
    *   `llm_bifrost.go` にHTTPベースのBifrostクライアントを実装
    *   `git commit -m "feat(wayfinder): add BifrostClient for LLMGP integration"`

8.  **ビルド・テスト実行**:
    *   Verification Planに従い全テスト実行

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```

2.  **Integration Tests**:
    ```bash
    ./scripts/process/integration_test.sh --categories llm --specify "TestWayfinder"
    ```
    *   **Log Verification**: `view-syslog.sh --tail 50` で Wayfinder 関連ログにエラーがないことを確認。

3.  **E2E Tests (新規)**:

    #### [NEW] [wayfinder_core_test.go](file://tests/wayfinder_core_test.go)
    *   **テストケース**: `TestWayfinderE2E_BasicToolExecution`
        *   MockLLMClient を使用し、`read_file` ツールを1回呼び出して回答するシナリオ
        *   AgentCoreのRunを直接呼び出し、ToolCallの実行と最終回答の生成を検証
    *   **テストケース**: `TestWayfinderE2E_GuardrailBlock`
        *   MockLLMClientが `sudo rm -rf /` を実行しようとするシナリオ
        *   ガードレールにより拒否され、エラーメッセージがLLMに返されることを検証
    *   **テストケース**: `TestWayfinderE2E_PathBoundaryCheck`
        *   `../../etc/passwd` への `read_file` が拒否されることを検証
    *   **検証ポイント**: AgentCoreのRunループが正しくToolCallを駆動し、ガードレールが適切に動作すること

### テスト項目のセルフレビュー (testing-rules 11.4)

1. **網羅性**: 正常系(ツール実行成功)、異常系(ガードレール拒否、未知ツール)、境界値(パストラバーサル)をカバー。
2. **証拠の十分性**: 各テストは期待値の文字列比較やエラー型チェックで動作を検証。
3. **迂回排除**: MockLLMClientを使用するため、実際のLLM応答に依存せず再現性がある。
4. **依存関係**: config -> guardrail -> tools -> agent_core の順にボトムアップでテスト。

### 総合判定プロセス (testing-rules 12)

全テスト完了後、testing-rules 12.2のチェック項目(スキップ有無、部分エラー見落とし、迂回偽成功等)を確認し、総合判定を記録する。

## Documentation

本計画は新規パッケージの作成のため、既存ドキュメントへの影響はない。

---

## 継続計画について

本計画はWayfinder Agent実装の **Part 1/4** です。

- **Part 2** ([001-Wayfinder-Session-Persistence.md](file://prompts/phases/000-foundation/branches/feat-wayfinder-agent/plans/001-Wayfinder-Session-Persistence.md)): セッション管理、永続化、コンパクション
- **Part 3** ([002-Wayfinder-Subagent-Summarization.md](file://prompts/phases/000-foundation/branches/feat-wayfinder-agent/plans/002-Wayfinder-Subagent-Summarization.md)): サブエージェント連携、要約
- **Part 4** ([003-Wayfinder-WBS-Planning-Orchestration.md](file://prompts/phases/000-foundation/branches/feat-wayfinder-agent/plans/003-Wayfinder-WBS-Planning-Orchestration.md)): WBS計画生成、オーケストレーション、実行分岐
