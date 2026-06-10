# 033-Logging-System-Redesign

> **Source Specification**: [024-Logging-System-Redesign.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/024-Logging-System-Redesign.md)

## Goal Description

ログシステムを再設計し、5段階レベル (TRACE/DEBUG/INFO/WARN/ERROR) の導入、複数出力先対応 (MultiWriter)、syslog フォールバック、ログ基準ルール策定を行う。本 Part 1 では logger パッケージの拡張、config 拡張、ログ基準ルール策定、ワークフロー更新までを扱う。全モジュールへのログ挿入マイグレーションは Part 2 (034) で扱う。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: ログレベルの5段階化 | Part 1 > logger/level.go, logger/logger.go, logger/default.go |
| R2: ログ出力先の複数指定 | Part 1 > config/config.go, logger/multi_writer.go, logger/builder.go |
| R3: syslog フォールバック | Part 1 > logger/writer_syslog.go |
| R4: ログ基準ルールの策定 | Part 1 > prompts/rules/logging-rules.md, execute-implementation-plan.md |
| R5: 既存コードへのログ挿入 | **Part 2 (034) で実施** |
| R6: Logger インターフェースへの Trace メソッド追加 | Part 1 > logger/logger.go, logger/default.go |
| R7: MultiWriter の実装 | Part 1 > logger/multi_writer.go |

## Proposed Changes

### logger パッケージ -- テスト

テストを先に記述し（TDD）、その後に実装する。

#### [MODIFY] [level_test.go](file://shared/libs/go/logger/level_test.go)
*   **Description**: TRACE レベルの追加に伴うテストを追加
*   **Technical Design**:
    ```go
    func TestParseLevel_Trace(t *testing.T)
    func TestLevel_String_Trace(t *testing.T)
    func TestLevel_Ordering(t *testing.T)
    ```
*   **Logic**:
    1. `ParseLevel("trace")` が `LevelTrace` を返すことを検証
    2. `LevelTrace.String()` が `"TRACE"` を返すことを検証
    3. レベルの順序が `LevelTrace < LevelDebug < LevelInfo < LevelWarn < LevelError` であることを検証

#### [MODIFY] [default_test.go](file://shared/libs/go/logger/default_test.go)
*   **Description**: Trace メソッドとレベルフィルタリングのテスト追加
*   **Technical Design**:
    ```go
    func TestDefaultLogger_Trace(t *testing.T)
    func TestDefaultLogger_LevelFiltering_Trace(t *testing.T)
    ```
*   **Logic**:
    1. `TestDefaultLogger_Trace`: レベル `LevelTrace` の logger で `Trace()` を呼び出し、出力されることを検証
    2. `TestDefaultLogger_LevelFiltering_Trace`: レベル `LevelDebug` の logger で `Trace()` を呼び出し、**出力されない**ことを検証

#### [NEW] [multi_writer_test.go](file://shared/libs/go/logger/multi_writer_test.go)
*   **Description**: MultiWriter のユニットテスト
*   **Technical Design**:
    ```go
    func TestMultiWriter_WriteToAll(t *testing.T)
    func TestMultiWriter_OneWriterFails(t *testing.T)
    func TestMultiWriter_Close(t *testing.T)
    func TestMultiWriter_Empty(t *testing.T)
    ```
*   **Logic**:
    1. `TestMultiWriter_WriteToAll`: 2つの mockWriter に同時に書き込まれることを検証
       ```go
       mw1 := &mockWriter{}
       mw2 := &mockWriter{}
       multi := NewMultiWriter(mw1, mw2)
       multi.Write(LevelInfo, []byte("hello"))
       // mw1.written == "hello" && mw2.written == "hello"
       ```
    2. `TestMultiWriter_OneWriterFails`: 1つの writer がエラーを返しても、他の writer には書き込まれることを検証。戻り値は最初のエラーを返す。
    3. `TestMultiWriter_Close`: 全 writer の Close が呼ばれることを検証
    4. `TestMultiWriter_Empty`: writer が 0 個の場合のエッジケース

#### [MODIFY] [writer_syslog_test.go](file://shared/libs/go/logger/writer_syslog_test.go)
*   **Description**: syslog フォールバック機構のテスト追加
*   **Technical Design**:
    ```go
    func TestSyslogWriter_FallbackOnConnectFailure(t *testing.T)
    func TestSyslogWriter_ReconnectOnRecovery(t *testing.T)
    ```
*   **Logic**:
    1. `TestSyslogWriter_FallbackOnConnectFailure`:
       - 到達不能なアドレスで `NewSyslogWriter` を生成
       - `Write()` を呼ぶ
       - fallback writer (stderr) に書き込まれることを検証
       - stderr に WARN メッセージが出力されることを検証
    2. `TestSyslogWriter_ReconnectOnRecovery`:
       - テスト用 UDP サーバーを起動
       - `NewSyslogWriter` でそのアドレスに接続
       - UDP サーバーを停止 -> `Write()` でフォールバック発動
       - UDP サーバーを再起動 -> `Write()` で syslog に書き込まれることを検証

#### [NEW] [builder_test.go](file://shared/libs/go/logger/builder_test.go)
*   **Description**: config からの Logger 構築ロジックのテスト
*   **Technical Design**:
    ```go
    func TestBuildFromConfig_StdoutOnly(t *testing.T)
    func TestBuildFromConfig_SyslogOnly(t *testing.T)
    func TestBuildFromConfig_Multiple(t *testing.T)
    func TestBuildFromConfig_Empty_DefaultsToStdout(t *testing.T)
    func TestBuildFromConfig_UnknownType(t *testing.T)
    ```
*   **Logic**:
    1. `TestBuildFromConfig_StdoutOnly`: `Outputs: [{Type: "stdout"}]` -> StdoutWriter が使用されることを検証
    2. `TestBuildFromConfig_SyslogOnly`: `Outputs: [{Type: "syslog", ...}]` -> SyslogWriter が使用されることを検証
    3. `TestBuildFromConfig_Multiple`: 両方指定 -> MultiWriter が内部的に使用されることを検証
    4. `TestBuildFromConfig_Empty_DefaultsToStdout`: `Outputs: nil` -> デフォルトで StdoutWriter が使用されることを検証
    5. `TestBuildFromConfig_UnknownType`: 未知の type -> error を返すことを検証

---

### logger パッケージ -- 実装

#### [MODIFY] [level.go](file://shared/libs/go/logger/level.go)
*   **Description**: TRACE レベルを追加し、既存レベルの iota 値をシフト
*   **Technical Design**:
    ```go
    const (
        // LevelTrace is the most verbose log level (data dumps, request/response bodies).
        LevelTrace Level = iota
        // LevelDebug logs processing flow (branch decisions, lifecycle events).
        LevelDebug
        // LevelInfo logs normal operation (startup, shutdown, access).
        LevelInfo
        // LevelWarn logs undesirable but non-fatal conditions (retry, slowdown).
        LevelWarn
        // LevelError logs processing failures with full context.
        LevelError
    )
    ```
*   **Logic**:
    1. `LevelTrace` を `iota` の先頭（値 0）に追加
    2. 既存の `LevelDebug` は値 1 に自動シフト（以前は 0 だった）
    3. `String()` に `case LevelTrace: return "TRACE"` を追加
    4. `ParseLevel()` に `case "trace": return LevelTrace` を追加

> [!IMPORTANT]
> **破壊的変更**: `LevelDebug` の iota 値が 0 -> 1 にシフトするため、`NewDefault(LevelDebug)` を呼んでいる外部コードの動作は変わらない（名前で参照しているため）。ただし `NewDefault(0)` のように数値リテラルで呼んでいる箇所があれば修正が必要。現在のコードベースではそのような箇所はない。

#### [MODIFY] [logger.go](file://shared/libs/go/logger/logger.go)
*   **Description**: Logger インターフェースに `Trace` メソッドを追加
*   **Technical Design**:
    ```go
    type Logger interface {
        // Trace logs trace-level data dumps (JSON bodies, headers, full payloads).
        Trace(msg string, fields ...any)

        // Debug logs debug-level processing flow events.
        Debug(msg string, fields ...any)

        // Info logs an info-level message.
        Info(msg string, fields ...any)

        // Warn logs a warning-level message.
        Warn(msg string, fields ...any)

        // Error logs an error-level message.
        Error(msg string, fields ...any)

        // WithFields returns a child logger with additional fields.
        WithFields(fields map[string]any) Logger

        // WithComponent returns a child logger with "component" field set.
        WithComponent(name string) Logger
    }
    ```

#### [MODIFY] [default.go](file://shared/libs/go/logger/default.go)
*   **Description**: DefaultLogger に `Trace` メソッドの実装を追加
*   **Technical Design**:
    ```go
    // Trace logs a trace-level message (data dumps, request/response bodies).
    func (l *DefaultLogger) Trace(msg string, fields ...any) {
        l.log(LevelTrace, msg, fields)
    }
    ```

#### [MODIFY] [writer_stdout.go](file://shared/libs/go/logger/writer_stdout.go)
*   **Description**: TRACE レベルの出力先を stdout に設定（DEBUG と同じ）
*   **Logic**: 変更不要。現在の実装は `level >= LevelWarn` で stderr にルーティングしているため、TRACE (値 0) は自動的に stdout に出力される。

#### [MODIFY] [writer_syslog.go](file://shared/libs/go/logger/writer_syslog.go)
*   **Description**: syslog フォールバック機構を追加
*   **Technical Design**:
    ```go
    type SyslogWriter struct {
        mu            sync.Mutex
        network       string
        raddr         string
        tag           string
        conn          net.Conn
        fallback      LogWriter   // fallback writer (stderr by default)
        hasFallback   bool        // true when using fallback due to conn failure
        stdoutInUse   bool        // true when stdout is also configured (skip fallback)
    }

    // NewSyslogWriter creates a SyslogWriter with fallback support.
    // If stdoutInUse is true, syslog failure does NOT fallback to stderr
    // (to avoid duplicate output).
    func NewSyslogWriter(network, raddr, tag string, stdoutInUse bool) (*SyslogWriter, error)
    ```
*   **Logic**:
    1. `Write()` で `conn == nil` の場合、再接続を試みる
    2. 再接続失敗時:
       - `stdoutInUse == false` の場合: stderr に WARN を出力し、`fallback` writer に書き込む
       - `stdoutInUse == true` の場合: stderr に WARN のみ出力（ログ本文はスキップ -- stdout で既に出力されるため）
    3. 再接続成功時: `hasFallback = false` に戻し、syslog への出力を再開。INFO ログで "syslog connection recovered" を出力
    4. `LevelTrace` の syslog priority: severity = 7 (debug) -- syslog の severity には trace がないため debug と同じにする

#### [NEW] [multi_writer.go](file://shared/libs/go/logger/multi_writer.go)
*   **Description**: 複数の LogWriter を束ねる MultiWriter を実装
*   **Technical Design**:
    ```go
    // MultiWriter fans out log writes to multiple LogWriter instances.
    // If any writer returns an error, writing continues to remaining writers.
    // The first error encountered is returned.
    type MultiWriter struct {
        writers []LogWriter
    }

    // NewMultiWriter creates a MultiWriter from one or more LogWriter instances.
    func NewMultiWriter(writers ...LogWriter) *MultiWriter {
        return &MultiWriter{writers: writers}
    }

    // Write writes the payload to all writers. Returns the first error encountered.
    func (mw *MultiWriter) Write(level Level, payload []byte) (int, error) {
        var firstErr error
        var n int
        for _, w := range mw.writers {
            written, err := w.Write(level, payload)
            if err != nil && firstErr == nil {
                firstErr = err
            }
            if written > n {
                n = written
            }
        }
        return n, firstErr
    }

    // Close closes all writers. Returns the first error encountered.
    func (mw *MultiWriter) Close() error {
        var firstErr error
        for _, w := range mw.writers {
            if err := w.Close(); err != nil && firstErr == nil {
                firstErr = err
            }
        }
        return firstErr
    }
    ```

#### [NEW] [builder.go](file://shared/libs/go/logger/builder.go)
*   **Description**: config の LogConfig から Logger を構築するファクトリ関数
*   **Technical Design**:
    ```go
    // LogOutputConfig describes a single log output destination.
    type LogOutputConfig struct {
        Type    string `yaml:"type"`              // "stdout" or "syslog"
        Network string `yaml:"network,omitempty"` // syslog: "udp" or "tcp"
        Address string `yaml:"address,omitempty"` // syslog: "host:port"
        Tag     string `yaml:"tag,omitempty"`     // syslog: tag prefix
    }

    // BuildFromConfig creates a DefaultLogger from LogConfig.
    // If no outputs are specified, defaults to stdout.
    func BuildFromConfig(level Level, outputs []LogOutputConfig) (*DefaultLogger, error) {
        if len(outputs) == 0 {
            return NewDefault(level), nil
        }

        hasStdout := false
        for _, o := range outputs {
            if o.Type == "stdout" {
                hasStdout = true
                break
            }
        }

        var writers []LogWriter
        for _, o := range outputs {
            switch o.Type {
            case "stdout":
                writers = append(writers, NewStdoutWriter())
            case "syslog":
                sw, err := NewSyslogWriter(o.Network, o.Address, o.Tag, hasStdout)
                if err != nil {
                    return nil, fmt.Errorf("create syslog writer: %w", err)
                }
                writers = append(writers, sw)
            default:
                return nil, fmt.Errorf("unknown log output type: %q", o.Type)
            }
        }

        var writer LogWriter
        if len(writers) == 1 {
            writer = writers[0]
        } else {
            writer = NewMultiWriter(writers...)
        }

        return NewDefaultWithOptions(level, &TextFormatter{}, writer), nil
    }
    ```

---

### logger パッケージ -- テストインフラ

#### [MODIFY] [testutil_test.go](file://shared/libs/go/logger/testutil_test.go)
*   **Description**: mockLogger に Trace メソッドを追加
*   **Technical Design**:
    ```go
    func (m *mockLogger) Trace(msg string, fields ...any) {
        m.mu.Lock()
        defer m.mu.Unlock()
        m.calls = append(m.calls, mockCall{method: "Trace", msg: msg, fields: fields})
    }
    ```

---

### config パッケージ

#### [MODIFY] [config_test.go](file://shared/libs/go/config/config_test.go)
*   **Description**: LogConfig の Outputs フィールドのテスト追加
*   **Technical Design**:
    ```go
    func TestLogConfig_Outputs(t *testing.T)
    ```
*   **Logic**:
    1. YAML 文字列をパースして `LogConfig.Outputs` が正しく展開されることを検証
       ```yaml
       log:
         level: "debug"
         outputs:
           - type: "stdout"
           - type: "syslog"
             network: "udp"
             address: "localhost:514"
             tag: "hag"
       ```
    2. `len(cfg.Log.Outputs) == 2` を検証
    3. `cfg.Log.Outputs[0].Type == "stdout"` を検証
    4. `cfg.Log.Outputs[1].Type == "syslog"` を検証
    5. `cfg.Log.Outputs[1].Address == "localhost:514"` を検証

#### [MODIFY] [config.go](file://shared/libs/go/config/config.go)
*   **Description**: LogConfig に Outputs フィールドを追加
*   **Technical Design**:
    ```go
    import "github.com/axsh/hag/logger"

    // LogConfig holds logging settings.
    type LogConfig struct {
        // Level is the minimum log level: "trace", "debug", "info", "warn", "error".
        Level string `yaml:"level"`

        // Outputs defines log output destinations.
        // If empty, defaults to stdout.
        Outputs []logger.LogOutputConfig `yaml:"outputs,omitempty"`
    }
    ```
*   **Logic**: LogOutputConfig 型は logger パッケージで定義し、config パッケージからインポートする。これにより循環参照を回避する。

---

### hag パッケージ

#### [MODIFY] [server.go](file://shared/libs/go/hag/server.go)
*   **Description**: `resolveLogger` を拡張して config の Outputs を使用する
*   **Technical Design**:
    ```go
    func resolveLogger(o *options, cfg *config.AppConfig) logger.Logger {
        if o.logger != nil {
            return o.logger
        }
        level := logger.ParseLevel(cfg.Log.Level)
        if len(cfg.Log.Outputs) > 0 {
            log, err := logger.BuildFromConfig(level, cfg.Log.Outputs)
            if err != nil {
                // Fallback to default if config is invalid.
                fallback := logger.NewDefault(level)
                fallback.Warn("failed to build logger from config, using default",
                    "error", err.Error())
                return fallback
            }
            return log
        }
        return logger.NewDefault(level)
    }
    ```

---

### ログ基準ルール

#### [NEW] [logging-rules.md](file://prompts/rules/logging-rules.md)
*   **Description**: ログの使用基準、コンポーネントタグ付け、フィールド命名規則を定義
*   **内容**:

```markdown
# ログ記述規範 (Logging Rules)

本規範は、ログの記述基準を定め、運用時の障害調査と開発時のデバッグを効率化することを目的とする。

## 1. ログレベルの使用基準

### 1.1 INFO (運用者向け -- 平時確認)

**定義**: システム管理者が、トラブルではない平時にでも確認しておきたいログ。正常に動作していることが分かるもの。

**出力すべき場面**:
- サーバー/コンポーネントの起動・終了
- 設定ファイルのロード完了と主要設定値のサマリ
- 外部接続の確立（DB、API、syslog）
- リクエスト受付のサマリ（詳細はDEBUG以下）

**出力してはいけないもの**:
- ループ内の反復ごとのログ
- 変数値のダンプ

### 1.2 WARN (運用者向け -- 警告)

**定義**: エラーではないが、エラーにつながる可能性がある望ましくない状態。

**出力すべき場面**:
- マージン付き閾値の超過
- スローダウンの検出
- リトライの発生（試行回数を含む）
- 継続可能な例外の発生
- ファイル読み取り失敗（フォールバックあり）
- レスポンスのフォーマット/バージョン不一致（互換性で読める場合）
- syslog 接続失敗によるフォールバック発生

### 1.3 ERROR (運用者向け -- 障害)

**定義**: 処理を続行できず諦めなければならない問題。

**必須ルール**: ERROR ログには**その時点で収集可能な有益情報を全て含める**。後から TRACE レベルにして再現するのでは機会を逃すため、ERROR ログ自体に詳細なコンテキストを含めること。

**含めるべき情報**:
- エラーメッセージとスタックトレース（利用可能な場合）
- リクエストの概要（URL、メソッド、モデル名）
- レスポンスの概要（ステータスコード、ボディの先頭 500 バイト）
- コンテキスト情報（セッションID、プロバイダー名、変換パス）
- 発生時のタイムスタンプ

**注意**: Fatal / os.Exit() のような強制終了は不要。あくまでその処理のエラーを通知する。

### 1.4 DEBUG (開発者向け -- フロー追跡)

**定義**: 何を処理しているかが分かるもの。外形からの観測だけで内部処理フローが理解できるようにする。

**出力すべき場面**:
- 関数のエントリ（主要な関数のみ。ユーティリティ関数は不要）
- 条件分岐の判定結果（例: "routing to openai provider", "using responses mode"）
- オブジェクトの生成・破棄（セッション作成、プロセス起動/終了）
- ルーティング決定の理由
- 設定値の適用結果

**パラメータ**: 処理を説明するための簡単なパラメータを含める。
```go
logger.Debug("routing request", "model", model, "provider", provider, "mode", mode)
```

### 1.5 TRACE (開発者向け -- データダンプ)

**定義**: DEBUG を補足する詳細データのダンプ。

**出力すべき場面**:
- JSON ボディの全文（リクエスト/レスポンス）
- HTTP ヘッダーの一覧
- SSE イベントの生データ
- 設定ファイルの全内容
- 変換前後のデータ比較
- CLI プロセスの stdout/stderr の生出力

**注意**: TRACE は大量のデータを出力するため、本番環境では通常無効にする。

## 2. コンポーネントタグ付けルール

全てのコンポーネントは `WithComponent()` でタグ付けし、ログにコンポーネント名を含めること。

| コンポーネント | タグ名 |
|---|---|
| HAG コアサーバー | `hag` |
| LLM Gateway Proxy | `llmgateway` |
| Agent Service | `agentservice` |
| Coding Agent (Claude Code) | `claudecode` |
| Coding Agent (Codex) | `codex` |
| WebSocket Server | `wsserver` |
| Bifrost Driver | `bifrost-driver` |
| Config Loader | `config` |
| Vault | `vault` |

## 3. フィールド命名規則

ログのキー/バリューフィールドには以下の命名規則を適用する:

- **スネークケース**を使用: `session_id`, `model_name`, `provider`
- **共通フィールド名**:
  - `session_id`: セッション識別子
  - `model`: モデル名
  - `provider`: プロバイダー名 ("anthropic", "openai")
  - `method`: HTTP メソッド
  - `path`: リクエストパス
  - `status`: HTTP ステータスコード
  - `duration_ms`: 処理時間（ミリ秒）
  - `error`: エラーメッセージ
  - `body_size`: ボディサイズ（バイト）
  - `attempt`: リトライ試行回数

## 4. 実装における注意事項

- **DEBUG ログは積極的に挿入する**。外形からの観測だけで内部処理が追跡できる透明性を目標とする。
- **TRACE ログはデータダンプ専用**。DEBUG ログに変数の中身を書かず、TRACE に分離する。
- **ERROR ログは詳細に**。後で再現するのでは遅い。その時点の情報を全て含める。
- **INFO ログは控えめに**。ループ内やリクエストごとに大量のINFOを出さない。
```

---

### ワークフロー更新

#### [MODIFY] [execute-implementation-plan.md](file://.agent/workflows/execute-implementation-plan.md)
*   **Description**: ログルールへの参照を追加
*   **Logic**: Section 1 の「ルールの読み込み」に以下を追加:
    ```markdown
    *   `prompts/rules/logging-rules.md` (ログ記述ルール)
    ```
    Section 2.3「コーディング」に以下を追加:
    ```markdown
    *   `prompts/rules/logging-rules.md` のレベル基準に従い、DEBUG ログを積極的に挿入する。
    ```

---

### examples 更新

#### [MODIFY] [config.yaml](file://examples/standalone/config.yaml)
*   **Description**: ログ出力先設定のサンプルを追加
*   **Technical Design**:
    ```yaml
    log:
      level: "info"
      outputs:
        - type: "stdout"
    #   - type: "syslog"
    #     network: "udp"
    #     address: "localhost:514"
    #     tag: "hag"
    ```

## Step-by-Step Implementation Guide

### Step 1: TRACE レベル追加とインターフェース拡張（テスト先行）
1. Edit `shared/libs/go/logger/level_test.go`: `TestParseLevel_Trace`, `TestLevel_String_Trace`, `TestLevel_Ordering` を追加
2. Edit `shared/libs/go/logger/level.go`: `LevelTrace` を iota 先頭に追加、`String()` と `ParseLevel()` を更新
3. Edit `shared/libs/go/logger/testutil_test.go`: mockLogger に `Trace()` メソッドを追加
4. Edit `shared/libs/go/logger/logger.go`: `Logger` インターフェースに `Trace()` を追加
5. Edit `shared/libs/go/logger/default_test.go`: `TestDefaultLogger_Trace`, `TestDefaultLogger_LevelFiltering_Trace` を追加
6. Edit `shared/libs/go/logger/default.go`: `Trace()` メソッドを実装
7. `git add && git commit -m "feat: add TRACE log level and Trace method to Logger interface"`

### Step 2: MultiWriter 実装（テスト先行）
1. Create `shared/libs/go/logger/multi_writer_test.go`: 4つのテストケースを実装
2. Create `shared/libs/go/logger/multi_writer.go`: `MultiWriter` 構造体と `NewMultiWriter`, `Write`, `Close` を実装
3. `git add && git commit -m "feat: implement MultiWriter for multiple log output destinations"`

### Step 3: SyslogWriter フォールバック拡張（テスト先行）
1. Edit `shared/libs/go/logger/writer_syslog_test.go`: フォールバックテスト2件を追加
2. Edit `shared/libs/go/logger/writer_syslog.go`: `fallback`, `hasFallback`, `stdoutInUse` フィールドと再接続ロジックを追加。`NewSyslogWriter` のシグネチャ変更
3. `git add && git commit -m "feat: add syslog fallback to stderr on connection failure"`

### Step 4: Builder ファクトリと Config 拡張（テスト先行）
1. Create `shared/libs/go/logger/builder_test.go`: 5つのテストケースを実装
2. Create `shared/libs/go/logger/builder.go`: `LogOutputConfig` 型と `BuildFromConfig` 関数を実装
3. Edit `shared/libs/go/config/config_test.go`: `TestLogConfig_Outputs` を追加
4. Edit `shared/libs/go/config/config.go`: `LogConfig` に `Outputs` フィールドを追加
5. Edit `shared/libs/go/hag/server.go`: `resolveLogger` を拡張
6. `git add && git commit -m "feat: add log output config and BuildFromConfig factory"`

### Step 5: ログ基準ルール策定とワークフロー更新
1. Create `prompts/rules/logging-rules.md`: ログ使用基準ルールを作成
2. Edit `.agent/workflows/execute-implementation-plan.md`: logging-rules.md への参照を追加
3. Edit `examples/standalone/config.yaml`: outputs セクションのサンプルを追加
4. `git add && git commit -m "docs: add logging rules and update workflow references"`

### Step 6: Verification Plan の実行
1. `./scripts/process/build.sh` を実行してビルドとユニットテストを確認
2. `./scripts/process/integration_test.sh --categories llm --specify "TestE2E"` を実行して統合テストを確認

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```
    *   **Log Verification**: logger パッケージの全テスト（`TestParseLevel_Trace`, `TestDefaultLogger_Trace`, `TestMultiWriter_*`, `TestSyslogWriter_Fallback*`, `TestBuildFromConfig_*`）が PASS すること

2.  **Integration Tests**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories llm --specify "TestE2E"
    ```
    *   **Log Verification**: 既存の E2E テストが PASS すること（ログレベル変更による副作用がないこと）

3.  **E2E Tests**:
    本 Part 1 は logger パッケージの内部拡張であり、外部 API の変更を含まない。既存の E2E テスト (`TestE2E_CodingAgent`, `TestCrossProvider`, `TestResponsesAPI`) がそのまま通ることで、リグレッションがないことを検証する。新規 E2E テストの追加は不要。Part 2 のログ挿入マイグレーション完了後に、ログ出力の検証 E2E テストを追加する。

### セルフレビュー結果

1. **要件対比**: R1-R4, R6-R7 は本 Part 1 でカバー。R5 は Part 2 で実施。
2. **再現性**: 全ステップにコード例とテストケースを明記。
3. **データ構造**: `LogOutputConfig`, `MultiWriter`, `SyslogWriter` の拡張フィールドを具体的に記述。
4. **テスト網羅性**: TDD で各機能にテストを先行記述。ユニット 14 ケース。
5. **統合テストの実行プラン**: `--categories llm --specify "TestE2E"` で選択実行。
6. **テスト設計**: ボトムアップ順序（Level -> Logger -> Writer -> MultiWriter -> Builder -> Config -> 統合）。
7. **E2E テスト**: 本 Part は内部拡張のため不要。理由を明記済み。

## Documentation

#### [MODIFY] [coding-rules.md](file://prompts/rules/coding-rules.md)
*   **更新内容**: Section 4 (コード品質) 等に「ログ記述は `prompts/rules/logging-rules.md` に従うこと」を追記

## 継続計画について

本計画は Part 1 です。Part 2 (034-Logging-System-Redesign-Migration) では、全モジュール (llmgateway, agentservice, codingagent, wsserver, hag) への DEBUG/TRACE ログの挿入マイグレーションを扱います。
