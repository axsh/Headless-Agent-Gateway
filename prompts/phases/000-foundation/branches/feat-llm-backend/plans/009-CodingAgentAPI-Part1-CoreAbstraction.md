# 009-CodingAgentAPI-Part1-CoreAbstraction

> **Source Specification**: [007-CodingAgentAPI.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/007-CodingAgentAPI.md)

## Goal Description

Coding Agent APIのコア抽象層 (`codingagent` パッケージ) を実装する。CodingAgent / Session / StreamEvent インターフェース、SessionOption/SessionConfig、VFSMount、リトライロジック、フォールバックツールパーサーを含む。このPartは後続のAdapter実装 (Part2) およびAgentService実装 (Part3) の基盤となる。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1-1: CodingAgent Interface | `codingagent/interface.go` |
| R1-2: Session Interface | `codingagent/interface.go` |
| R1-3: StreamEvent型 | `codingagent/event.go` |
| R1-4: SessionOption / SessionConfig | `codingagent/options.go` |
| R2-1: Adapter共通インターフェース準拠 | `codingagent/interface.go` (compile-time check用) |
| R2-2: AdapterConfig | `codingagent/adapter_config.go` |
| R2-5: リトライロジック | `codingagent/retry.go` |
| R6-7: VFSMount | `codingagent/vfs.go` |
| R7-1: SessionStore Interface | `codingagent/session_store.go` |
| R7-4: SessionRecord (SDKSessionID含む) | `codingagent/session_store.go` |
| R9-2: FallbackToolCall パーサー | `codingagent/fallback.go` |
| R9-3: 全ツールサポート | `codingagent/fallback.go` |

## Proposed Changes

### codingagent パッケージ (コア抽象層)

#### [NEW] [interface_test.go](file://shared/libs/go/codingagent/interface_test.go)
*   **Description**: CodingAgent / Session インターフェースの準拠性テスト
*   **Technical Design**:
    ```go
    package codingagent_test

    // TestCodingAgentInterfaceCompliance は compile-time check で
    // CodingAgent インターフェースが正しく定義されていることを確認する。
    // 実際の Adapter (claudecode, codex) の準拠性テストは Part2 で実施。
    func TestCodingAgentInterfaceDefinition(t *testing.T)
    // - CodingAgent interface のメソッドシグネチャ確認
    // - Session interface のメソッドシグネチャ確認
    ```
*   **Logic**:
    *   モック構造体を定義し、`var _ CodingAgent = (*mockAgent)(nil)` でコンパイル時チェック
    *   モック構造体を定義し、`var _ Session = (*mockSession)(nil)` でコンパイル時チェック

#### [NEW] [interface.go](file://shared/libs/go/codingagent/interface.go)
*   **Description**: CodingAgent / Session インターフェース定義
*   **Technical Design**:
    ```go
    package codingagent

    import "context"

    // CodingAgent はコーディングエージェントバックエンドの共通インターフェース。
    // CLIラッパー型 (Claude Code, Codex) と将来のAPI直接型の両方が実装可能。
    type CodingAgent interface {
        // CreateSession は新しいエージェントセッションを開始する。
        // 内部でCLIサブプロセスを起動する。
        CreateSession(ctx context.Context, opts ...SessionOption) (Session, error)

        // Name はエージェントバックエンド名を返す ("claudecode", "codex")。
        Name() string

        // Close はエージェントリソースを解放する。
        Close() error
    }

    // Session はアクティブなエージェントセッション。
    // CLIサブプロセスのライフサイクルに対応する。
    type Session interface {
        // Send はメッセージを送信し、ストリーミングイベントチャネルを返す。
        // チャネルはエージェントの応答完了時にクローズされる。
        Send(ctx context.Context, message string) (<-chan StreamEvent, error)

        // ID はセッションIDを返す。
        ID() string

        // Close はセッションを終了し、サブプロセスをクリーンアップする。
        Close() error
    }
    ```

---

#### [NEW] [event_test.go](file://shared/libs/go/codingagent/event_test.go)
*   **Description**: StreamEvent / EventType のテスト
*   **Technical Design**:
    ```go
    func TestStreamEventJSONMarshal(t *testing.T)
    // テーブル駆動テスト:
    // - EventText: Content フィールドのみ
    // - EventToolUse: ToolName + ToolInput
    // - EventError: Error フィールドは json:"-" で除外されること
    // - EventSystem: SessionID フィールド

    func TestStreamEventJSONUnmarshal(t *testing.T)
    // テーブル駆動テスト:
    // - 正常な JSON 文字列からの逆シリアライズ
    // - 不明な Type の場合もエラーにならないこと

    func TestEventTypeConstants(t *testing.T)
    // - 6つの EventType 定数が定義されていること
    // - 文字列値の一致確認
    ```
*   **Logic**: JSON marshal/unmarshal のラウンドトリップ検証

#### [NEW] [event.go](file://shared/libs/go/codingagent/event.go)
*   **Description**: StreamEvent 型と EventType 定数
*   **Technical Design**:
    ```go
    package codingagent

    // StreamEvent はエージェントからのストリーミングイベント。
    type StreamEvent struct {
        Type      EventType              `json:"type"`
        Content   string                 `json:"content,omitempty"`
        ToolName  string                 `json:"tool_name,omitempty"`
        ToolInput map[string]interface{} `json:"tool_input,omitempty"`
        SessionID string                 `json:"session_id,omitempty"`
        Error     error                  `json:"-"`
    }

    type EventType string

    const (
        EventText       EventType = "text"
        EventToolUse    EventType = "tool_use"
        EventToolResult EventType = "tool_result"
        EventResult     EventType = "result"
        EventError      EventType = "error"
        EventSystem     EventType = "system"
    )
    ```

---

#### [NEW] [options_test.go](file://shared/libs/go/codingagent/options_test.go)
*   **Description**: SessionOption / SessionConfig のテスト
*   **Technical Design**:
    ```go
    func TestSessionOptionFunctions(t *testing.T)
    // テーブル駆動テスト:
    // - WithModel("anthropic/claude-sonnet-4") -> SessionConfig.Model が設定される
    // - WithPrompt("hello") -> SessionConfig.Prompt が設定される
    // - WithAllowedTools(["Read","Write"]) -> SessionConfig.AllowedTools が設定される
    // - WithWorkDir("/workspace") -> SessionConfig.WorkDir が設定される
    // - WithEnvVars(map) -> SessionConfig.EnvVars が設定される
    // - WithSDKSessionID("sdk-123") -> SessionConfig.SDKSessionID が設定される
    // - WithVFSMounts(mounts) -> SessionConfig.VFSMounts が設定される

    func TestSessionOptionComposition(t *testing.T)
    // - 複数の SessionOption を順番に適用し、全フィールドが設定されること
    // - 後から適用したオプションが前のオプションを上書きすること

    func TestApplyDefaults(t *testing.T)
    // - AdapterConfig のデフォルト値が SessionConfig に適用されること
    // - SessionOption がデフォルト値を上書きすること
    // - 優先順位: SessionOption > AdapterConfig.Default* > ゼロ値
    ```

#### [NEW] [options.go](file://shared/libs/go/codingagent/options.go)
*   **Description**: SessionOption / SessionConfig / ApplyDefaults
*   **Technical Design**:
    ```go
    package codingagent

    // SessionOption はセッション作成時のオプション。
    type SessionOption func(*SessionConfig)

    type SessionConfig struct {
        // Web APIリクエスト由来のオプション (リクエスト毎に変化)
        Model        string   // 使用するモデル名 (例: "anthropic/claude-sonnet-4")
        Prompt       string   // 初期プロンプト (シングルショット用)
        AllowedTools []string // 許可するツール一覧

        // 起動オプション (環境/コンテナ起動時に固定)
        WorkDir      string            // 作業ディレクトリ (CWD)
        EnvVars      map[string]string // 追加環境変数

        // セッション継続 (resume)
        SDKSessionID string     // CLI/SDK側の既存セッションを再開する場合に指定

        // VFSマウント (コンテナ実行時)
        VFSMounts    []VFSMount // ホスト->コンテナのファイルマッピング
    }

    func WithModel(model string) SessionOption {
        return func(c *SessionConfig) { c.Model = model }
    }
    func WithPrompt(prompt string) SessionOption {
        return func(c *SessionConfig) { c.Prompt = prompt }
    }
    func WithAllowedTools(tools []string) SessionOption {
        return func(c *SessionConfig) { c.AllowedTools = tools }
    }
    func WithWorkDir(dir string) SessionOption {
        return func(c *SessionConfig) { c.WorkDir = dir }
    }
    func WithEnvVars(vars map[string]string) SessionOption {
        return func(c *SessionConfig) { c.EnvVars = vars }
    }
    func WithSDKSessionID(id string) SessionOption {
        return func(c *SessionConfig) { c.SDKSessionID = id }
    }
    func WithVFSMounts(mounts []VFSMount) SessionOption {
        return func(c *SessionConfig) { c.VFSMounts = mounts }
    }

    // ApplyDefaults は AdapterConfig のデフォルト値を SessionConfig に適用する。
    // SessionOption で明示的に設定されたフィールドは上書きしない。
    // 優先順位: SessionOption > AdapterConfig.Default* > ゼロ値
    func ApplyDefaults(cfg *SessionConfig, ac *AdapterConfig) {
        if cfg.WorkDir == "" { cfg.WorkDir = ac.DefaultWorkDir }
        if cfg.Model == "" { cfg.Model = ac.DefaultModel }
        if cfg.EnvVars == nil && ac.DefaultEnvVars != nil {
            cfg.EnvVars = make(map[string]string)
            for k, v := range ac.DefaultEnvVars { cfg.EnvVars[k] = v }
        }
    }

    // NewSessionConfig は SessionOption を適用して SessionConfig を生成する。
    func NewSessionConfig(opts ...SessionOption) *SessionConfig {
        cfg := &SessionConfig{}
        for _, opt := range opts { opt(cfg) }
        return cfg
    }
    ```

---

#### [NEW] [adapter_config.go](file://shared/libs/go/codingagent/adapter_config.go)
*   **Description**: AdapterConfig (全Adapter共通設定)
*   **Technical Design**:
    ```go
    package codingagent

    import "github.com/axsh/hag/logger"

    // AdapterConfig は全Adapter共通の設定
    type AdapterConfig struct {
        GatewayURL string        // LLM Gateway Proxy URL
        Logger     logger.Logger // ロガー

        // 起動オプション (デフォルト値、SessionOptionで上書き可能)
        DefaultWorkDir string            // デフォルトCWD
        DefaultModel   string            // デフォルトモデル
        DefaultEnvVars map[string]string // デフォルト追加環境変数

        // サンドボックス制御
        DisableSandbox bool // true: CLIの内部サンドボックスを無効化 (コンテナ実行時)
    }
    ```

---

#### [NEW] [vfs_test.go](file://shared/libs/go/codingagent/vfs_test.go)
*   **Description**: VFSMount / vfsToContainerPath のテスト
*   **Technical Design**:
    ```go
    func TestVfsToContainerPath(t *testing.T)
    // テーブル駆動テスト:
    // | input                        | expected       |
    // | "vfs://workspace/"           | "/workspace"   |
    // | "vfs://workspace/data/"      | "/workspace/data" |
    // | "vfs://workspace/sub/dir"    | "/workspace/sub/dir" |
    // | "vfs:///"                    | "/"            |

    func TestSortVFSMounts(t *testing.T)
    // - 複数マウントを渡し、パス長の昇順 (親ディレクトリ優先) でソートされること
    // - 同一パス長のマウントは順序が保たれること (安定ソート)

    func TestVFSMountsToDockerArgs(t *testing.T)
    // テーブル駆動テスト:
    // - 単一マウント: ["-v", "/host/path:/workspace"]
    // - 複数マウント: 親→子の順序で -v 引数が生成されること
    // - PhysicalPath の "file://" プレフィックスが除去されること
    ```

#### [NEW] [vfs.go](file://shared/libs/go/codingagent/vfs.go)
*   **Description**: VFSMount 型と変換ユーティリティ
*   **Technical Design**:
    ```go
    package codingagent

    import (
        "net/url"
        "sort"
        "strings"
    )

    // VFSMount はホストパス <-> コンテナパスのマッピングを定義する
    type VFSMount struct {
        VFSPath      string // 論理パス (例: "vfs://workspace/")
        PhysicalPath string // ホスト物理パス (例: "file:///home/user/project")
    }

    // VfsToContainerPath はVFS URIをコンテナ内パスに変換する。
    // "vfs://workspace/"      -> "/workspace"
    // "vfs://workspace/data/" -> "/workspace/data"
    func VfsToContainerPath(vfsPath string) string {
        // "vfs://" プレフィックスを除去
        path := strings.TrimPrefix(vfsPath, "vfs://")
        // 末尾スラッシュを除去
        path = strings.TrimRight(path, "/")
        if path == "" { return "/" }
        if !strings.HasPrefix(path, "/") { path = "/" + path }
        return path
    }

    // PhysicalToHostPath は "file://" URI をネイティブファイルパスに変換する。
    // "file:///home/user/project" -> "/home/user/project"
    func PhysicalToHostPath(physical string) string {
        if u, err := url.Parse(physical); err == nil && u.Scheme == "file" {
            return u.Path
        }
        return physical
    }

    // SortVFSMounts はマウントを親ディレクトリ優先 (VFSPath長の昇順) でソートする。
    func SortVFSMounts(mounts []VFSMount) {
        sort.SliceStable(mounts, func(i, j int) bool {
            return len(mounts[i].VFSPath) < len(mounts[j].VFSPath)
        })
    }

    // VFSMountsToDockerArgs は VFSMount 一覧から Docker の -v 引数を生成する。
    // 戻り値: ["-v", "/host/path:/container/path", "-v", ...]
    func VFSMountsToDockerArgs(mounts []VFSMount) []string {
        sorted := make([]VFSMount, len(mounts))
        copy(sorted, mounts)
        SortVFSMounts(sorted)

        var args []string
        for _, m := range sorted {
            hostPath := PhysicalToHostPath(m.PhysicalPath)
            containerPath := VfsToContainerPath(m.VFSPath)
            args = append(args, "-v", hostPath+":"+containerPath)
        }
        return args
    }
    ```

---

#### [NEW] [retry_test.go](file://shared/libs/go/codingagent/retry_test.go)
*   **Description**: リトライロジックのテスト
*   **Technical Design**:
    ```go
    func TestIsRetryableError(t *testing.T)
    // テーブル駆動テスト:
    // | error message               | expected |
    // | "EOF"                       | true     |
    // | "connection reset by peer"  | true     |
    // | "broken pipe"               | true     |
    // | "connection refused"        | true     |
    // | "connectex: ..."            | true     |
    // | "404 not found"             | false    |
    // | "unauthorized"              | false    |
    // | nil                         | false    |

    func TestRetryWithSuccess(t *testing.T)
    // - 最初の2回は connection refused エラーを返し、3回目で成功する関数を渡す
    // - Retry が成功を返すこと
    // - 実行回数が3回であること

    func TestRetryAllFail(t *testing.T)
    // - maxAttempts 回全て失敗する関数を渡す
    // - Retry が最後のエラーを返すこと

    func TestRetryNonRetryableError(t *testing.T)
    // - 非リトライ対象エラー ("unauthorized") を返す関数を渡す
    // - Retry が即座にエラーを返すこと (リトライしない)

    func TestRetryWithContextCancel(t *testing.T)
    // - context をキャンセルした状態で Retry を呼ぶ
    // - context.Canceled エラーが返ること
    ```

#### [NEW] [retry.go](file://shared/libs/go/codingagent/retry.go)
*   **Description**: セッション作成時のリトライロジック
*   **Technical Design**:
    ```go
    package codingagent

    import (
        "context"
        "strings"
        "time"
    )

    const (
        DefaultMaxAttempts  = 10
        DefaultRetryInterval = 3 * time.Second
    )

    // RetryConfig はリトライの設定。
    type RetryConfig struct {
        MaxAttempts   int
        RetryInterval time.Duration
        // ContainerCheck は任意のコンテナ死活チェック関数。
        // nil の場合はチェックを行わない。
        // false を返すとリトライを即座に中断する。
        ContainerCheck func() bool
    }

    // DefaultRetryConfig はデフォルトのリトライ設定を返す。
    func DefaultRetryConfig() *RetryConfig {
        return &RetryConfig{
            MaxAttempts:   DefaultMaxAttempts,
            RetryInterval: DefaultRetryInterval,
        }
    }

    // IsRetryableError はリトライ対象のエラーかどうかを判定する。
    // EOF, connection reset, broken pipe, connection refused, connectex が対象。
    func IsRetryableError(err error) bool {
        if err == nil { return false }
        errStr := err.Error()
        return strings.Contains(errStr, "EOF") ||
            strings.Contains(errStr, "connection reset") ||
            strings.Contains(errStr, "broken pipe") ||
            strings.Contains(errStr, "connection refused") ||
            strings.Contains(errStr, "connectex")
    }

    // Retry は fn を最大 MaxAttempts 回実行する。
    // リトライ対象エラーの場合のみリトライする。
    // ContainerCheck が false を返した場合は即座に中断する。
    func Retry(ctx context.Context, cfg *RetryConfig, fn func() error) error {
        if cfg == nil { cfg = DefaultRetryConfig() }
        var lastErr error
        for attempt := 0; attempt < cfg.MaxAttempts; attempt++ {
            if err := ctx.Err(); err != nil { return err }
            lastErr = fn()
            if lastErr == nil { return nil }
            if !IsRetryableError(lastErr) { return lastErr }
            if cfg.ContainerCheck != nil && !cfg.ContainerCheck() {
                return lastErr // コンテナ停止: リトライ中断
            }
            if attempt < cfg.MaxAttempts-1 {
                select {
                case <-time.After(cfg.RetryInterval):
                case <-ctx.Done(): return ctx.Err()
                }
            }
        }
        return lastErr
    }
    ```

---

#### [NEW] [fallback_test.go](file://shared/libs/go/codingagent/fallback_test.go)
*   **Description**: フォールバックツールパーサーのテスト
*   **Technical Design**:
    ```go
    func TestParseFallbackToolCalls_SingleObject(t *testing.T)
    // 入力: `{"name": "Write", "arguments": {"path": "main.go", "content": "package main"}}`
    // 期待: []FallbackToolCall{{Name: "Write", Arguments: {path, content}}}

    func TestParseFallbackToolCalls_Array(t *testing.T)
    // 入力: `[{"name": "Write", ...}, {"name": "Read", ...}]`
    // 期待: 2要素の FallbackToolCall スライス

    func TestParseFallbackToolCalls_MarkdownFence(t *testing.T)
    // 入力: "```json\n{\"name\": \"Bash\", \"arguments\": {\"command\": \"ls\"}}\n```"
    // 期待: []FallbackToolCall{{Name: "Bash", Arguments: {command}}}

    func TestParseFallbackToolCalls_AllToolTypes(t *testing.T)
    // テーブル駆動テスト:
    // | toolName | arguments                              |
    // | "Write"  | {"path": "a.go", "content": "..."}     |
    // | "Read"   | {"path": "a.go"}                       |
    // | "Edit"   | {"path": "a.go", "old": "x", "new": "y"} |
    // | "Bash"   | {"command": "ls -la"}                  |
    // | "Glob"   | {"pattern": "*.go"}                    |
    // | "Grep"   | {"pattern": "TODO", "path": "."}       |

    func TestParseFallbackToolCalls_Invalid(t *testing.T)
    // テーブル駆動テスト:
    // - 空文字列 -> (nil, false)
    // - 非JSONテキスト -> (nil, false)
    // - name フィールドなし -> (nil, false)
    // - JSONだが配列でもオブジェクトでもない -> (nil, false)

    func TestStripMarkdownCodeFence(t *testing.T)
    // テーブル駆動テスト:
    // | input                         | expected          |
    // | "```json\n{}\n```"            | "{}"              |
    // | "```\n{}\n```"                | "{}"              |
    // | "text before\n```json\n{}\n```\ntext after" | "{}" |
    // | "{}"                          | "{}" (変更なし)     |
    ```

#### [NEW] [fallback.go](file://shared/libs/go/codingagent/fallback.go)
*   **Description**: テキストからのフォールバックツールコール解析
*   **Technical Design**:
    ```go
    package codingagent

    import (
        "encoding/json"
        "regexp"
        "strings"
    )

    // FallbackToolCall はテキストから解析されたツールコール
    type FallbackToolCall struct {
        Name      string         `json:"name"`
        Arguments map[string]any `json:"arguments"`
    }

    // ParseFallbackToolCalls はテキストからツールコールを抽出する。
    // 対応形式:
    //   - 単一オブジェクト: {"name": "Write", "arguments": {...}}
    //   - 配列: [{"name": "Write", ...}, ...]
    //   - マークダウンコードフェンス内のJSON: ```json\n{...}\n```
    func ParseFallbackToolCalls(text string) ([]FallbackToolCall, bool) {
        // 1. マークダウンコードフェンスを除去
        cleaned := StripMarkdownCodeFence(text)
        cleaned = strings.TrimSpace(cleaned)
        if cleaned == "" { return nil, false }

        // 2. 配列として試行
        if strings.HasPrefix(cleaned, "[") {
            var calls []FallbackToolCall
            if err := json.Unmarshal([]byte(cleaned), &calls); err == nil {
                if len(calls) > 0 && calls[0].Name != "" { return calls, true }
            }
        }

        // 3. 単一オブジェクトとして試行
        if strings.HasPrefix(cleaned, "{") {
            var call FallbackToolCall
            if err := json.Unmarshal([]byte(cleaned), &call); err == nil {
                if call.Name != "" { return []FallbackToolCall{call}, true }
            }
        }

        return nil, false
    }

    var markdownFenceRe = regexp.MustCompile("(?s)```(?:json|\\w*)?\n?(.*?)\n?```")

    // StripMarkdownCodeFence はマークダウンコードフェンスを除去し、中身を返す。
    // コードフェンスが無い場合はそのまま返す。
    func StripMarkdownCodeFence(text string) string {
        matches := markdownFenceRe.FindStringSubmatch(text)
        if len(matches) >= 2 { return matches[1] }
        return text
    }
    ```

---

#### [NEW] [session_store.go](file://shared/libs/go/codingagent/session_store.go)
*   **Description**: SessionStore インターフェースと SessionRecord 型
*   **Technical Design**:
    ```go
    package codingagent

    import "time"

    // SessionStore はセッション永続化の抽象インターフェース
    type SessionStore interface {
        Create(session *SessionRecord) error
        Get(id string) (*SessionRecord, error)
        Update(session *SessionRecord) error
        List() ([]*SessionRecord, error)
        Delete(id string) error
    }

    // SessionRecord はセッションの永続化レコード
    type SessionRecord struct {
        ID           string    // HAG管理のセッションID (UUID)
        AgentName    string    // "claudecode", "codex"
        Model        string
        Status       string    // "active", "completed", "error", "closed"
        WorkDir      string
        SDKSessionID string    // CLI/SDKが内部管理するセッションID (コンテキスト引き継ぎ用)
        CreatedAt    time.Time
        UpdatedAt    time.Time
    }

    // セッションステータス定数
    const (
        StatusActive    = "active"
        StatusCompleted = "completed"
        StatusError     = "error"
        StatusClosed    = "closed"
    )
    ```
*   **Logic**:
    *   `SessionStore` は agentservice パッケージ (Part3) で `MemorySessionStore` として実装する
    *   `SessionRecord.ID` はHAG管理のUUID、`SDKSessionID` はCLI/SDK内部ID (R7-4)

## Step-by-Step Implementation Guide

1.  **Step 1: パッケージディレクトリ作成**:
    *   `shared/libs/go/codingagent/` ディレクトリを作成する

2.  **Step 2: event_test.go + event.go (StreamEvent)**:
    *   `event_test.go` を作成し、JSON marshal/unmarshal テストを記述
    *   `event.go` を作成し、`StreamEvent` と `EventType` 定数を実装
    *   テスト実行で Green を確認

3.  **Step 3: interface_test.go + interface.go (CodingAgent / Session)**:
    *   `interface_test.go` を作成し、コンパイル時チェックを記述
    *   `interface.go` を作成し、インターフェースを定義
    *   テスト実行で Green を確認

4.  **Step 4: adapter_config.go (AdapterConfig)**:
    *   `adapter_config.go` を作成し、`AdapterConfig` 構造体を定義

5.  **Step 5: options_test.go + options.go (SessionOption)**:
    *   `options_test.go` を作成し、オプション合成・優先順位テストを記述
    *   `options.go` を作成し、`SessionOption` / `SessionConfig` / `ApplyDefaults` を実装
    *   テスト実行で Green を確認

6.  **Step 6: session_store.go (SessionStore Interface)**:
    *   `session_store.go` を作成し、`SessionStore` インターフェースと `SessionRecord` を定義

7.  **Step 7: vfs_test.go + vfs.go (VFSMount)**:
    *   `vfs_test.go` を作成し、VFS変換・ソート・Docker引数テストを記述
    *   `vfs.go` を作成し、`VFSMount` 型と変換関数を実装
    *   テスト実行で Green を確認

8.  **Step 8: retry_test.go + retry.go (リトライ)**:
    *   `retry_test.go` を作成し、リトライロジックテストを記述
    *   `retry.go` を作成し、`Retry` / `IsRetryableError` を実装
    *   テスト実行で Green を確認

9.  **Step 9: fallback_test.go + fallback.go (フォールバック)**:
    *   `fallback_test.go` を作成し、パーサーテストを記述
    *   `fallback.go` を作成し、`ParseFallbackToolCalls` / `StripMarkdownCodeFence` を実装
    *   テスト実行で Green を確認

10. **Step 10: ビルド検証**:
    *   Verification Plan を実行し、全テストが Green であることを確認

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh --skip-frontend --skip-etc
    ```

2.  **セルフレビュー**: testing-rules.md 11 に従い、テスト項目の網羅性を確認

### テスト項目のセルフレビュー結果

1.  **網羅性の検証**: codingagent パッケージの全公開型・関数に対してテストが定義されている。interface, event, options, vfs, retry, fallback の6つのコンポーネント全てにテストファイルがある。
2.  **証拠の十分性**: JSON marshal/unmarshal のラウンドトリップ、オプション合成の優先順位、VFS変換の具体的な入出力、リトライの成功/失敗/コンテキストキャンセル、フォールバックの複数フォーマット対応を検証している。
3.  **迂回・抜け道の排除**: コンパイル時チェック (`var _ Interface = (*Impl)(nil)`) でインターフェース準拠を保証。
4.  **依存関係の整合性**: Part1 は外部依存が logger のみ。ボトムアップ順序で event -> interface -> options -> vfs -> retry -> fallback の順にテストする。

## 継続計画について

本計画は3つのPartに分割されています:

- **Part1 (本計画)**: `codingagent` パッケージのコア抽象層
- **Part2 (010)**: `claudecode/` + `codex/` Adapter実装
- **Part3 (011)**: `agentservice/` Web API + hag.Server統合
