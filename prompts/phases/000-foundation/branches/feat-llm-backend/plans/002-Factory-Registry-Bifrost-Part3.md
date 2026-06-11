# 002-Factory-Registry-Bifrost-Part3

> **Source Specification**: prompts/phases/000-foundation/branches/feat-llm-backend/ideas/030-Factory-Registry-And-Bifrost-Unification.md

## Goal Description

本 Part3 では、Part1-2 で構築した基盤と機能の上に、仕上げ作業を実施する:

1. **R6: Example の簡素化とリネーム** (standalone -> cawa-server/Viper/Cobra, minimal-server, minimal-client)
2. **R7: レガシーコード削除** (R3 の Bifrost SDK パスが安定動作確認後)

依存関係: R1 + R5 -> R6 / R3 安定動作 -> R7 (convert_*.go 削除)

## User Review Required

> [!WARNING]
> **R7 レガシーコード削除**: convert_a2o.go, convert_a2g.go, convert_a2r.go, stream_converter.go, provider_forwarder.go の合計約2,000行を削除します。Bifrost SDK パスが全プロバイダーで安定動作していることが前提です。削除前に全テスト成功を確認します。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R6-1: standalone -> cawa-server リネーム | Proposed Changes > examples/cawa-server/ |
| R6-1: registerCodingAgents() 廃止 | Proposed Changes > cawa-server/main.go |
| R6-1: Viper/Cobra 導入 | Proposed Changes > cawa-server/*.go |
| R6-2: minimal-server example | Proposed Changes > examples/minimal-server/ |
| R6-2: minimal-client example | Proposed Changes > examples/minimal-client/ |
| R6-3: 最小コード example テスト | Proposed Changes > tests/examples_build_test.go |
| R7: convert_a2o.go 削除 | Proposed Changes > llmgateway/ |
| R7: convert_a2g.go 削除 | Proposed Changes > llmgateway/ |
| R7: convert_a2r.go 削除 | Proposed Changes > llmgateway/ |
| R7: stream_converter.go 削除 | Proposed Changes > llmgateway/ |
| R7: provider_forwarder.go 削除 | Proposed Changes > llmgateway/ |

## Proposed Changes

### R6: Example の簡素化とリネーム

#### [RENAME + MODIFY] examples/standalone/ -> [examples/cawa-server/](file://examples/cawa-server/)
*   **Description**: standalone を cawa-server にリネームし、Viper/Cobra で CLI アプリとして再構成
*   **Technical Design**:
    *   `git mv examples/standalone examples/cawa-server`
    *   Cobra の `rootCmd` 構造:
        ```go
        // cawa-server/cmd/root.go
        var rootCmd = &cobra.Command{
            Use:   "cawa-server",
            Short: "Arctic-tern Coding Agent Web Application Server",
            RunE:  runServer,
        }

        func init() {
            rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "config.yaml", "config file path")
            rootCmd.PersistentFlags().IntVar(&port, "port", 0, "agent service port (overrides config)")
            rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "", "log level")
        }
        ```
    *   `main.go` は最小限:
        ```go
        package main

        import (
            "os"
            "github.com/axsh/arctic-tern/examples/cawa-server/cmd"
        )

        func main() {
            if err := cmd.Execute(); err != nil {
                os.Exit(1)
            }
        }
        ```
    *   Viper で設定ファイル読み込み:
        ```go
        // cawa-server/cmd/config.go
        func initConfig() {
            viper.SetConfigFile(cfgFile)
            viper.AutomaticEnv()
            if err := viper.ReadInConfig(); err != nil {
                // ...
            }
        }
        ```
    *   `registerCodingAgents()` を **削除** (R1 の init() 自己登録で代替)
    *   import で `_ "github.com/axsh/arctic-tern/codingagent/claudecode"` を追加するだけで自動登録
*   **Logic**:
    *   現在の standalone/main.go (約400行) を以下に分割:
        *   `cmd/root.go`: Cobra rootCmd + flags
        *   `cmd/server.go`: サーバー起動ロジック (tern.New + Launch)
        *   `cmd/config.go`: Viper 設定読み込み
        *   `main.go`: Cobra Execute のみ (10行以下)

#### [NEW] [examples/minimal-server/main.go](file://examples/minimal-server/main.go)
*   **Description**: tern サーバーの最小起動コード (仕様書 R6-2)
*   **Technical Design** (仕様書から継承):
    ```go
    package main

    import (
        "context"
        "fmt"
        "log"

        "github.com/axsh/arctic-tern/config"
        "github.com/axsh/arctic-tern/tern"
        _ "github.com/axsh/arctic-tern/codingagent/claudecode"
    )

    func main() {
        cfg := config.Load("config.yaml")
        srv, err := tern.New(
            tern.WithAgentServiceConfig(cfg.AgentServiceConfig()),
            tern.WithLLMServiceConfig(cfg.LLMServiceConfig()),
        )
        if err != nil {
            log.Fatal(err)
        }
        ctx := context.Background()
        if err := srv.Launch(ctx); err != nil {
            log.Fatal(err)
        }
        defer srv.Shutdown(ctx)

        fmt.Printf("tern server running on %s\n", srv.AgentService().URL())
    }
    ```

#### [NEW] [examples/minimal-client/main.go](file://examples/minimal-client/main.go)
*   **Description**: client ライブラリの最小利用コード (仕様書 R6-2)
*   **Technical Design** (仕様書から継承):
    ```go
    package main

    import (
        "context"
        "log"
        "os"

        "github.com/axsh/arctic-tern/client"
    )

    func main() {
        ctx := context.Background()
        c := client.New("http://localhost:3100")

        session, err := c.CreateSession(ctx, client.SessionRequest{
            Agent:   "claudecode",
            Model:   "sonnet", // model_profiles.yaml のモデル名
            WorkDir: ".",
        })
        if err != nil {
            log.Fatal(err)
        }
        defer session.Terminate(ctx)
        log.Printf("Session: %s", session.ID)

        stream, err := session.SendMessage(ctx, "Create a file called hello.txt with the content 'Hello, World!'")
        if err != nil {
            log.Fatal(err)
        }

        if err := stream.Output(os.Stdout); err != nil {
            log.Fatal(err)
        }
    }
    ```

#### [NEW] [tests/examples_build_test.go](file://tests/examples_build_test.go)
*   **Description**: 最小コード example がコンパイルできることを検証するテスト (R6-3)
*   **Technical Design**:
    ```go
    //go:build integration

    package tests

    func TestExamples_MinimalServer_Builds(t *testing.T) {
        cmd := exec.Command("go", "build", "-o", "/dev/null", "./examples/minimal-server/")
        cmd.Dir = projectRoot()
        output, err := cmd.CombinedOutput()
        if err != nil {
            t.Fatalf("minimal-server build failed: %v\n%s", err, output)
        }
    }

    func TestExamples_MinimalClient_Builds(t *testing.T) {
        cmd := exec.Command("go", "build", "-o", "/dev/null", "./examples/minimal-client/")
        cmd.Dir = projectRoot()
        output, err := cmd.CombinedOutput()
        if err != nil {
            t.Fatalf("minimal-client build failed: %v\n%s", err, output)
        }
    }
    ```

---

### R7: レガシーコード削除

#### [DELETE] 変換コード
*   `shared/libs/go/llmgateway/convert_a2o.go` (357行)
*   `shared/libs/go/llmgateway/convert_a2o_test.go` (451行)
*   `shared/libs/go/llmgateway/convert_a2g.go` (492行)
*   `shared/libs/go/llmgateway/convert_a2g_test.go` (377行)
*   `shared/libs/go/llmgateway/convert_a2r.go` (516行)
*   `shared/libs/go/llmgateway/convert_a2r_test.go` (477行)
*   `shared/libs/go/llmgateway/stream_converter.go` (291行)
*   `shared/libs/go/llmgateway/stream_converter_test.go` (225行)
*   **合計削除**: 約 3,186行

#### [DELETE] レガシーフォワーダー
*   `shared/libs/go/llmgateway/provider_forwarder.go` (335行)
*   `shared/libs/go/llmgateway/provider_forwarder_test.go` (308行)
*   **合計削除**: 約 643行

#### [MODIFY] [proxy_anthropic.go](file://shared/libs/go/llmgateway/proxy_anthropic.go)
*   **Description**: legacy fallback パスを削除し、Bifrost SDK パスのみにする

---

## Step-by-Step Implementation Guide

### Phase A: R6 Example 簡素化

1.  **standalone -> cawa-server リネーム**:
    *   `git mv examples/standalone examples/cawa-server`
    *   コミット: `refactor: rename standalone to cawa-server`

2.  **Viper/Cobra 導入**:
    *   `cmd/` サブディレクトリを作成
    *   `root.go`, `server.go`, `config.go` を作成
    *   `main.go` を最小化
    *   `registerCodingAgents()` を削除、import 自動登録に変更
    *   `./scripts/process/build.sh` でビルド成功を確認
    *   コミット: `refactor: introduce viper/cobra for cawa-server CLI`

3.  **minimal-server / minimal-client 作成**:
    *   `examples/minimal-server/main.go` を作成
    *   `examples/minimal-client/main.go` を作成
    *   `./scripts/process/build.sh` でビルド成功を確認
    *   コミット: `feat: add minimal-server and minimal-client examples`

4.  **Example ビルドテスト追加**:
    *   `tests/examples_build_test.go` を作成
    *   コミット: `test: add build verification tests for minimal examples`

### Phase B: R7 レガシーコード削除

5.  **全テスト実行 (削除前の最終確認)**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh
    ```
    *   全テスト成功を確認

6.  **proxy_anthropic.go から legacy fallback 削除**:
    *   Bifrost SDK パスのみにする
    *   `./scripts/process/build.sh` でテスト成功を確認
    *   コミット: `refactor: remove legacy fallback from anthropic handler`

7.  **convert_*.go + stream_converter.go 削除**:
    *   `git rm` で削除
    *   `./scripts/process/build.sh` でビルド成功を確認
    *   コミット: `feat: remove legacy conversion code (3186 lines)`

8.  **provider_forwarder.go 削除**:
    *   `git rm` で削除
    *   `./scripts/process/build.sh` でビルド成功を確認
    *   コミット: `feat: remove legacy provider forwarder (643 lines)`

### Phase C: 最終検証 + プッシュ

9.  **全テスト実行**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh
    ```

10. **E2E テスト実行**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestClaudeCodeE2E"
    ```

11. **プッシュ**: `git push`

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```

2.  **Integration Tests**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh
    ```
    *   R7: 全テストが convert_*.go 削除後もリグレッションなしで成功すること

3.  **E2E Tests**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestClaudeCodeE2E"
    ```
    *   R6: cawa-server (Viper/Cobra) が正常にサーバー起動・動作すること
    *   R7: Bifrost SDK パスのみで E2E が通ること

    **E2Eテストの方針**: R7 のレガシーコード削除後は、既存 E2E テストが全て通ることで「Bifrost SDK パスのみで全機能が動作している」ことを検証する。R6 の minimal-server/client はビルドテスト (TestExamples_MinimalServer_Builds) で検証する (実行時の動作は既存 E2E と同等のため)。

### テスト項目セルフレビュー (testing-rules 11.4)

1.  **網羅性**: R6 はビルドテスト + cawa-server E2E で検証。R7 はレガシー削除後の全テスト成功でカバー。
2.  **証拠の十分性**: ビルドテストは exit code 0 で検証。E2E はレスポンス内容まで検証。
3.  **迂回排除**: R7 削除後に legacy コードが残存していないことはビルド成功 (参照エラーなし) で保証。
4.  **依存関係**: R6 ビルドテスト -> E2E -> R7 削除 -> 全テスト再実行の順。

### 総合判定プロセス (testing-rules 12)

全テスト完了後、testing-rules 12.2 のチェック項目7点を確認し、総合判定を記述する。

## Documentation

#### [MODIFY] [README.md](file://README.md)
*   **更新内容**: examples セクションを更新 (cawa-server, minimal-server, minimal-client の説明追加)

#### [MODIFY] [prompts/specifications/](file://prompts/specifications/) 配下の関連ドキュメント
*   **更新内容**: HAG -> tern、Headless-Agent-Gateway -> arctic-tern の名称変更を反映 (本 Part1 で実施済みの場合はスキップ)
