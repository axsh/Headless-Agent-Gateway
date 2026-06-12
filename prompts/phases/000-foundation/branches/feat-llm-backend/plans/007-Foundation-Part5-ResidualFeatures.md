# 007-Foundation-Part5-ResidualFeatures

> **Source Specification**: [prompts/phases/000-foundation/branches/feat-llm-backend/ideas/005-FoundationResidualFeatures.md](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/prompts/phases/000-foundation/branches/feat-llm-backend/ideas/005-FoundationResidualFeatures.md)

## Goal Description

Foundationフェーズのクローズに必要な、未実装の残件（フォールバック、ファイルVault、スタンドアロン起動の構成、ロギング、一部未実装のパッケージスケルトンなど）を実装し、アーキテクチャ定義されているすべてのバックエンド要件を完了します。

## User Review Required

None.

## Requirement Traceability

> **Traceability Check**:
> 仕様書(Specification)の要件・決定事項をリストアップし、この計画書のどこで対応するかをマッピングしています。

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| **R1-1**: `hag.Server` への `AgentService()` メソッド追加 | Proposed Changes > `shared/libs/go/hag/server.go` |
| **R1-2**: `shared/libs/go/agentservice` パッケージ追加 | Proposed Changes > `shared/libs/go/agentservice/service.go` |
| **R1-3**: `shared/libs/go/wsserver` パッケージ追加 | Proposed Changes > `shared/libs/go/wsserver/server.go` |
| **R2-1**: `examples/standalone/main.go` の作成 | Proposed Changes > `examples/standalone/main.go` |
| **R2-2**: `examples/standalone/Dockerfile` の作成 | Proposed Changes > `examples/standalone/Dockerfile` |
| **R2-3**: `examples/standalone/docker-compose.yaml` の作成 | Proposed Changes > `examples/standalone/docker-compose.yaml` |
| **R3-1**: `PassthroughDriver` の実装 | Proposed Changes > `shared/libs/go/llmgateway/passthrough.go` |
| **R3-2**: OpenAI API `stream: true` ストリーミング対応 | Proposed Changes > `shared/libs/go/llmgateway/proxy_openai.go` |
| **R3-3**: サブセッションフォールバック | Proposed Changes > `shared/libs/go/llmgateway/routing.go` |
| **R3-4**: テキスト -> Tool Call 変換 | Proposed Changes > `shared/libs/go/llmgateway/fallback.go` |
| **R3-5**: Rate Limiting (Bifrost SDK) | Proposed Changes > `shared/libs/go/llmgateway/bifrost_driver.go` |
| **R4-1**: `FileVaultBackend` と AES暗号化 | Proposed Changes > `shared/libs/go/vault/file_backend.go` |
| **R4-2**: 設定の再設定API | Proposed Changes > `shared/libs/go/config/loader.go`, `shared/libs/go/hag/server.go` |
| **R4-3**: `SyslogWriter` | Proposed Changes > `shared/libs/go/logger/writer_syslog.go` |

---

## Proposed Changes

### shared/libs/go/logger

#### [NEW] [writer_syslog_test.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/shared/libs/go/logger/writer_syslog_test.go)
*   **Description**: `SyslogWriter` の初期化、TCP/UDP 接続模擬を通じた syslog 出力メッセージの検証。
*   **Technical Design**:
    ```go
    package logger

    import (
        "net"
        "testing"
    )

    func TestSyslogWriter_Write(t *testing.T) { ... }
    ```
*   **Logic**:
    *   ローカルで UDP リッスンソケットを立ち上げ、`SyslogWriter` からログを送信し、syslog 形式のプレフィックスを含んだログデータが受信できることを確認する。

#### [NEW] [writer_syslog.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/shared/libs/go/logger/writer_syslog.go)
*   **Description**: syslog サーバーに TCP/UDP または Unix ソケットでログメッセージを送信する `LogWriter` 実装。
*   **Technical Design**:
    ```go
    package logger

    import (
        "fmt"
        "net"
        "sync"
    )

    type SyslogWriter struct {
        mu      sync.Mutex
        network string
        raddr   string
        tag     string
        conn    net.Conn
    }

    func NewSyslogWriter(network, raddr, tag string) (*SyslogWriter, error) { ... }
    func (w *SyslogWriter) Write(level Level, payload []byte) (int, error) { ... }
    func (w *SyslogWriter) Close() error { ... }
    ```
*   **Logic**:
    *   `Write` 時に接続がなければ再接続を試み、メッセージの先頭に RFC3164/5424 または単純なファシリティ・プライオリティの syslog プレフィックスを付与して送信する。

---

### shared/libs/go/vault

#### [NEW] [file_backend_test.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/shared/libs/go/vault/file_backend_test.go)
*   **Description**: `FileVaultBackend` を用いたシークレットの保存、AES暗号化によるファイル格納状態、復号の検証。
*   **Technical Design**:
    ```go
    package vault

    import (
        "os"
        "testing"
    )

    func TestFileVaultBackend_Lifecycle(t *testing.T) { ... }
    ```
*   **Logic**:
    *   シークレットを `Set` した後、保存先ファイルの中身が暗号化されており、平文で読み取れないことをアサートする。
    *   再度 backend インスタンスを読み込んで `Resolve` で正しい生の値が取り出せることを確認する。

#### [NEW] [file_backend.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/shared/libs/go/vault/file_backend.go)
*   **Description**: AES-GCM 方式で暗号化してファイルへ永続化する `VaultStore` 実装。
*   **Technical Design**:
    ```go
    package vault

    import (
        "sync"
    )

    type FileVaultBackend struct {
        mu       sync.RWMutex
        filepath string
        key      []byte // AES-256 key derived from TERN_VAULT_KEY env
        secrets  map[string]string
    }

    func NewFileVaultBackend(filepath string) (*FileVaultBackend, error) { ... }
    func (b *FileVaultBackend) Resolve(ref string) (string, error) { ... }
    func (b *FileVaultBackend) Set(path, value string) error { ... }
    func (b *FileVaultBackend) Delete(path string) error { ... }
    func (b *FileVaultBackend) List() ([]string, error) { ... }
    ```
*   **Logic**:
    *   ファイル読み書きの際、`crypto/aes` および `crypto/cipher` の GCM モードを用いてデータを暗号化・復号化する。
    *   環境変数 `TERN_VAULT_KEY` が無い場合は、固定値またはエラーとして暗号キーを取得する。

---

### shared/libs/go/llmgateway

#### [NEW] [passthrough_test.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/shared/libs/go/llmgateway/passthrough_test.go)
*   **Description**: `PassthroughDriver` が Launch し、Bifrost などの処理を迂回して直接 upstream に転送する動作のテスト。
*   **Technical Design**:
    ```go
    package llmgateway

    import (
        "context"
        "testing"
    )

    func TestPassthroughDriver_Lifecycle(t *testing.T) { ... }
    ```

#### [NEW] [passthrough.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/shared/libs/go/llmgateway/passthrough.go)
*   **Description**: LLM 接続をそのまま転送する `PassthroughDriver` 実装。
*   **Technical Design**:
    ```go
    package llmgateway

    import (
        "context"
        "net/http"
    )

    type PassthroughDriver struct {
        port   int
        server *http.Server
    }

    func NewPassthroughDriver(port int) *PassthroughDriver { ... }
    func (d *PassthroughDriver) Launch(ctx context.Context) error { ... }
    func (d *PassthroughDriver) Shutdown(ctx context.Context) error { ... }
    func (d *PassthroughDriver) ListModels() []ModelInfo { ... }
    func (d *PassthroughDriver) Health() HealthStatus { ... }
    func (d *PassthroughDriver) ProxyURL() string { ... }
    ```
*   **Logic**:
    *   `httputil.ReverseProxy` 等を使用し、リクエストを公式 API エンドポイントへそのままリバースプロキシする。

#### [NEW] [fallback_test.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/shared/libs/go/llmgateway/fallback_test.go)
*   **Description**: テキストレスポンスから tool_calls を抽出・変換するロジックのテスト。
*   **Technical Design**:
    ```go
    package llmgateway

    import "testing"

    func TestExtractToolCall(t *testing.T) { ... }
    ```

#### [NEW] [fallback.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/shared/libs/go/llmgateway/fallback.go)
*   **Description**: Ollama 等で発生する、XMLやJSONのテキスト形式で返却された tool 呼び出しをパースし、構造化された `tool_calls` メッセージへマッピングする。
*   **Technical Design**:
    ```go
    package llmgateway

    func ExtractToolCallFromText(text string) ([]byte, bool) { ... }
    ```

#### [MODIFY] [proxy_openai.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/shared/libs/go/llmgateway/proxy_openai.go)
*   **Description**: `stream: true` 指定時に、SSE 形式のチャンクをクライアントへ逐次書き出すストリーミング処理を実装。

#### [MODIFY] [routing.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/shared/libs/go/llmgateway/routing.go)
*   **Description**: Claude Code 等から未知のモデル名を受信した際、最初のセッションで使用された定義済みモデルにフォールバックするロジックの追加。

#### [MODIFY] [bifrost_driver.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/shared/libs/go/llmgateway/bifrost_driver.go)
*   **Description**: Bifrost SDK に Rate Limiting の設定を適用し、プロバイダごとの流量制御を行う。

---

### shared/libs/go/agentservice

#### [NEW] [service.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/shared/libs/go/agentservice/service.go)
*   **Description**: 将来的な Coding Agent の管理用に、HTTPハンドラをマウントするだけの `AgentService` スケルトン実装。
*   **Technical Design**:
    ```go
    package agentservice

    import "net/http"

    type AgentService interface {
        HTTPHandler() http.Handler
    }

    type Server struct{}

    func New() *Server { return &Server{} }
    func (s *Server) HTTPHandler() http.Handler { return http.NewServeMux() }
    ```

---

### shared/libs/go/wsserver

#### [NEW] [server.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/shared/libs/go/wsserver/server.go)
*   **Description**: 将来的なリアルタイム中継用に、Launch / Shutdown 可能な `wsserver` スケルトン実装。
*   **Technical Design**:
    ```go
    package wsserver

    import "context"

    type Server struct{}

    func New() *Server { return &Server{} }
    func (s *Server) Launch(ctx context.Context) error { return nil }
    func (s *Server) Shutdown(ctx context.Context) error { return nil }
    ```

---

### shared/libs/go/config

#### [MODIFY] [loader.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/shared/libs/go/config/loader.go)
*   **Description**: 設定ファイルのランタイム再ロードメソッドの提供。

---

### shared/libs/go/hag

#### [MODIFY] [server.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/shared/libs/go/hag/server.go)
*   **Description**: `AgentService()` メソッドの実装、および `agentservice` / `wsserver` スケルトンの初期化と Launch/Shutdown 制御の追加。再設定メソッドの実装。

---

### examples/standalone

#### [NEW] [main.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/examples/standalone/main.go)
*   **Description**: config ファイル指定で HAG を起動し、シグナルを待って graceful shutdown する standalone サーバー実装。

#### [NEW] [Dockerfile](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/examples/standalone/Dockerfile)
*   **Description**: standalone Go バイナリをビルドし、コンテナイメージ化する Dockerfile。

#### [NEW] [docker-compose.yaml](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/examples/standalone/docker-compose.yaml)
*   **Description**: 設定ファイルのボリュームマウント、ポート公開、環境変数からのキー定義を統合した Compose ファイル。

---

## Step-by-Step Implementation Guide

1.  **[Implement Logger SyslogWriter]**:
    *   Create `shared/libs/go/logger/writer_syslog_test.go` and implement syslog output test cases.
    *   Create `shared/libs/go/logger/writer_syslog.go` and implement UDP/TCP network-based syslog sender.
    *   Verify with `./scripts/process/build.sh`.
2.  **[Implement FileVaultBackend with AES]**:
    *   Create `shared/libs/go/vault/file_backend_test.go` and write AES encryption tests.
    *   Create `shared/libs/go/vault/file_backend.go` and implement AES-GCM secure file persistency backend.
    *   Verify with `./scripts/process/build.sh`.
3.  **[Implement PassthroughDriver]**:
    *   Create `shared/libs/go/llmgateway/passthrough_test.go` and test reverse-proxy forwarding.
    *   Create `shared/libs/go/llmgateway/passthrough.go` and implement L4-like httputil.ReverseProxy driver.
    *   Verify with `./scripts/process/build.sh`.
4.  **[OpenAI Stream and Router Fallbacks]**:
    *   Implement SSE stream chunks write logic in `proxy_openai.go`.
    *   Add subsession routing fallback to `routing.go`.
    *   Create `fallback_test.go` and `fallback.go` to extract tool calls from plaintext.
    *   Apply rate limits to `bifrost_driver.go`.
    *   Verify with `./scripts/process/build.sh` and integration tests.
5.  **[将来パッケージのスケルトンと Server 統合]**:
    *   Create `agentservice/service.go` and `wsserver/server.go` skeletons.
    *   Modify `shared/libs/go/hag/server.go` to expose `AgentService()`, launch wsserver/agentservice, and add reload config capabilities.
    *   Verify with `./scripts/process/build.sh`.
6.  **[Create examples standalone and docker environments]**:
    *   Create `examples/standalone/main.go` with os.Signal handling.
    *   Create `examples/standalone/Dockerfile` and `examples/standalone/docker-compose.yaml`.
    *   Verify all tests via build and integration runners.

---

## Verification Plan

### Automated Verification

#### 1. Build & Unit Tests
Goのコンパイル確認と単体テスト（新しく追加される `writer_syslog_test.go` や `file_backend_test.go` 等を含む）の実行。
```bash
./scripts/process/build.sh
```

#### 2. Integration Tests
Bifrost API 接続、新結合シナリオのテスト実行。
```bash
./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories "common,llm"
```
*   **Log Verification**: OpenAI Stream 時のレスポンスログ、Bifrost / Passthrough 挙動ログが正しく表示されることを確認。

---

## Documentation

#### [MODIFY] [prompts/phases/README.md](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/prompts/phases/README.md)
*   **更新内容**: Foundation フェーズの全残機能（フォールバック、ファイルVault、Graceful Shutdown 等）が実装完了した状態に更新。

---

## テスト設計のセルフレビュー (Testing Self-Review)

### 1. 網網性の検証
- `005-FoundationResidualFeatures.md` で定義されている各残課題（AESファイルVault、OpenAIストリーミング、フォールバックルーティング、syslogロギングなど）が、単体テスト及び結合テストのテスト項目に全て漏れなく設計されています。

### 2. 証拠の十分性
- 暗号化したファイルが平文で直接読み取れないことのアサーションや、syslog用のローカルリスナーを用いたパケット受信チェック、および逆順のシャットダウンフローやルーティングでの定義外モデルのフォールバックをモックを使わずに確認します。

### 3. 迂回・抜け道の排除
- テストは可能な限り実際の実装と通信（ローカルポートバインド、ローカル暗号ファイル生成、ReverseProxy呼び出し）を実行させ、処理のバイパスによる偽成功を防ぎます。

### 4. 依存関係の整合性
- 各モジュール（`logger/writer_syslog`、`vault/file_backend`、`llmgateway/passthrough`など）を個別のステップで単体テストをパスさせた後に、ファサード層の `hag.Server` や standalone での起動制御をボトムアップで検証していきます。

---

## 総合判定プロセス（全テスト完了後に実施）

### 総合判定結果

**判定**: ⚠️ 未実施 (実装前の計画段階のため)
