# 002-Resolve-Main-Branch-Conflicts

> **Source Specification**: [001-Resolve-Main-Branch-Conflicts.md](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-support-lib-imports/prompts/phases/000-foundation/branches/feat-support-lib-imports/ideas/001-Resolve-Main-Branch-Conflicts.md)

## Goal Description
`feat-support-lib-imports` ブランクと `main` ブランクの競合（コンフリクト）を解消し、`main` 側の直近の全変更（PR #3〜PR #6の全機能）を完全に維持した上で、インポートパス移行（`shared/libs/go/...` および `server` パッケージへの置換）を正しく適用します。

## User Review Required
None.

## Requirement Traceability

> **Traceability Check**:
> 仕様書(Specification)の要件・決定事項をリストアップし、この計画書のどこで対応するかをマッピングしてください。
> もし仕様書の要件をこの計画で実装しない（先送りする）場合は、その理由を明記してください。

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| 1. 競合の解消 | Proposed Changes / Step-by-Step Implementation Guide |
| 2. 最新ロジックの維持 | Proposed Changes / Step-by-Step Implementation Guide |
| 3. インポートパス移行の適用 | Proposed Changes / Step-by-Step Implementation Guide |
| 4. ビルドおよびテストの通過 | Verification Plan |

---

## Proposed Changes

### examples/minimal-server

#### [MODIFY] [examples/minimal-server/main.go](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-support-lib-imports/examples/minimal-server/main.go)
*   **Description**: インポートパス `github.com/axsh/arctic-tern/tern` から `github.com/axsh/arctic-tern/server` への置換に伴う競合を解消します。
*   **Technical Design**:
    ```go
    import (
        // ...
        "github.com/axsh/arctic-tern/server"
    )

    func main() {
        // ...
        srv, err := server.New(server.WithConfigPath(configPath))
    }
    ```
*   **Logic**: `main` ブランチ側の設定ファイル読み込みおよび初期化処理フローをベースとし、インポートパスと `tern.New` の呼び出し部分のみを `server` パッケージ名へ書き換えます。

---

### shared/libs/go/llmgateway

#### [MODIFY] [shared/libs/go/llmgateway/handlerctx/context.go](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-support-lib-imports/shared/libs/go/llmgateway/handlerctx/context.go)
*   **Description**: インポートパス `shared/libs/go/...` への変更と、`main` で追加されたルーティング関連フィールド定義の衝突を解消します。
*   **Technical Design**:
    ```go
    import (
        // ...
        "github.com/axsh/arctic-tern/shared/libs/go/config"
        "github.com/axsh/arctic-tern/shared/libs/go/logger"
        "github.com/axsh/arctic-tern/shared/libs/go/vault"
    )
    ```
*   **Logic**: `main` で追加された `RoutedModel` 構造体やメソッド定義を完全に残した上で、上記インポートパスの移行を行います。

#### [MODIFY] [shared/libs/go/llmgateway/routing_test.go](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-support-lib-imports/shared/libs/go/llmgateway/routing_test.go)
*   **Description**: テストケースで使われている config パッケージのインポートパス変更と、`main` 側の設定キー変更の競合を解消します。
*   **Technical Design**:
    ```go
    import (
        // ...
        "github.com/axsh/arctic-tern/shared/libs/go/config"
    )
    ```
*   **Logic**: `main` 側でリファクタリングされた `ApiKeys` 設定キー等の最新テストケースを受け入れつつ、インポートパスを移行先に更新します。

---

### shared/libs/go/wayfinder

#### [MODIFY] [shared/libs/go/wayfinder/agent_core.go](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-support-lib-imports/shared/libs/go/wayfinder/agent_core.go)
*   **Description**: **最競合箇所。** WBS/Compaction、サブエージェントなどの大量のフィールド追加（`main`）と、インポートパス移行（`feat-support-lib-imports`）の競合を解消します。
*   **Technical Design**:
    ```go
    import (
        // ...
        "github.com/axsh/arctic-tern/shared/libs/go/codingagent"
        "github.com/axsh/arctic-tern/shared/libs/go/logger"
        "github.com/axsh/arctic-tern/shared/libs/go/wayfinder/planning"
        "github.com/axsh/arctic-tern/shared/libs/go/wayfinder/session"
        "github.com/axsh/arctic-tern/shared/libs/go/wayfinder/subagent"
        "github.com/axsh/arctic-tern/shared/libs/go/wayfinder/tools"
    )
    ```
*   **Logic**: `main` で追加された WBSやCompaction等に関する `AgentCore` 構造体の定義、ツール実行フロー、および新規メソッドをすべて維持した状態でマージし、インポートパスのみを `shared/libs/go/...` に更新します。

#### [MODIFY] [shared/libs/go/wayfinder/agent_core_test.go](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-support-lib-imports/shared/libs/go/wayfinder/agent_core_test.go)
*   **Description**: `main` 側の新規テストと `feat-support-lib-imports` 側のフォーマット調整の衝突を解消します。
*   **Technical Design**: `main` 側の最新テストコードをベースとします。
*   **Logic**: 最新のテストケースをそのまま採用し、必要に応じてインポートパスの移行を行います。

#### [MODIFY] [shared/libs/go/wayfinder/agent_runner.go](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-support-lib-imports/shared/libs/go/wayfinder/agent_runner.go)
*   **Description**: サブエージェント連携コード（`main`）とインポートパス変更の衝突を解消します。
*   **Technical Design**:
    ```go
    import (
        // ...
        "github.com/axsh/arctic-tern/shared/libs/go/logger"
        "github.com/axsh/arctic-tern/shared/libs/go/wayfinder/subagent"
    )
    ```
*   **Logic**: `main` 側の初期化ロジック変更を採用し、インポートパスを移行先に更新します。

---

### tests

#### [MODIFY] [tests/agentservice_e2e_test.go](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-support-lib-imports/tests/agentservice_e2e_test.go)
*   **Description**: `main` で追加されたE2Eテストの安定化コード（404エラーハンドリング、PID抽出、スキップ処理）と、`feat-support-lib-imports` 側のインポートパス修正の競合を解消します。
*   **Technical Design**:
    ```go
    import (
        // ...
        "github.com/axsh/arctic-tern/shared/libs/go/codingagent"
        "github.com/axsh/arctic-tern/shared/libs/go/codingagent/claudecode"
        "github.com/axsh/arctic-tern/server"
    )
    ```
*   **Logic**: `main` の最新の安定化ロジック（エラー検知時の `t.Skip` 代替のスキップ等）を維持し、インポートパスを `server` や `shared/libs/go/...` に変更します。

#### [MODIFY] [tests/codex_e2e_test.go](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-support-lib-imports/tests/codex_e2e_test.go)
*   **Description**: `main` のE2Eテスト安定化ロジックとインポートパスの競合を解消します。
*   **Technical Design**:
    ```go
    import (
        // ...
        "github.com/axsh/arctic-tern/shared/libs/go/codingagent"
        "github.com/axsh/arctic-tern/shared/libs/go/codingagent/codex"
        "github.com/axsh/arctic-tern/server"
    )
    ```
*   **Logic**: `main` の最新のテストロジックを維持し、インポートパスを `server` 等に移行します。

---

## Step-by-Step Implementation Guide

1.  **[Start Merge]**:
    *   `feat-support-lib-imports` ブランチにおいて `git merge main` を実行し、競合を発生させます。
2.  **[Resolve Examples]**:
    *   [examples/minimal-server/main.go](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-support-lib-imports/examples/minimal-server/main.go) の競合を解消します。
3.  **[Resolve LLMGateway]**:
    *   [shared/libs/go/llmgateway/handlerctx/context.go](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-support-lib-imports/shared/libs/go/llmgateway/handlerctx/context.go) および [shared/libs/go/llmgateway/routing_test.go](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-support-lib-imports/shared/libs/go/llmgateway/routing_test.go) の競合を解消します。
4.  **[Resolve Wayfinder]**:
    *   [shared/libs/go/wayfinder/agent_core.go](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-support-lib-imports/shared/libs/go/wayfinder/agent_core.go)、[shared/libs/go/wayfinder/agent_core_test.go](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-support-lib-imports/shared/libs/go/wayfinder/agent_core_test.go)、[shared/libs/go/wayfinder/agent_runner.go](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-support-lib-imports/shared/libs/go/wayfinder/agent_runner.go) の競合を解消します。
5.  **[Resolve Tests]**:
    *   [tests/agentservice_e2e_test.go](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-support-lib-imports/tests/agentservice_e2e_test.go) および [tests/codex_e2e_test.go](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-support-lib-imports/tests/codex_e2e_test.go) の競合を解消します。
6.  **[Commit Merge]**:
    *   競合解消が完了したことを確認し、マージコミットを作成します。

---

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    全体ビルドおよび単体テストを実行し、インポートパスの不整合や構文エラーがないことを確認します。
    ```bash
    ./scripts/process/build.sh --skip-frontend --skip-etc
    ```

2.  **Integration & E2E Tests**:
    E2Eテストおよび統合テストを実行し、テストがすべて正常に通過することを確認します。
    ※Linux環境の場合は、必ず `xvfb-run -a` でラップして実行します。
    ```bash
    # Linux環境
    xvfb-run -a ./scripts/process/integration_test.sh
    # またはそれ以外の環境
    ./scripts/process/integration_test.sh
    ```

3.  **E2E Tests (新規/追加)**:
    - **E2Eテスト不要の理由**: 本変更は機能追加を伴わない、純粋な `main` とのコンフリクト解消およびインポートパス移行（リファクタリング）のみであるため、E2Eテストの新規追加は不要です。既存の E2E テストが正常に通過することをもって妥当性の検証とします。

---

### テスト設計のセルフレビュー
- **網羅性の検証**: コンフリクトが解消され、既存のすべての単体テストおよびE2Eテストがパスすれば、新機能に影響を与えず正しくインポートパスが更新されたと言えます。
- **証拠 of 十分性**: ビルドスクリプトの通過および、`agentservice_e2e_test.go` 等のE2Eテストの成功により、ランタイムレベルでインポートや依存関係が正常に動作している十分な証拠が得られます。
- **依存関係の整合性**: 変更対象の下位パッケージのテスト（単体テスト）が成功した後に、上位の統合/E2Eテストを実行するビルドパイプラインに則るため、整合性は確保されます。

---

### 総合判定プロセスの計画
テスト完了後、以下の総合判定チェックを実施して最終判定を記録します。

| # | チェック項目 | 確認方法 |
|---|------------|----------|
| 1 | スキップされたテストの有無 | テストログに出力される `t.Skip` や警告マーカーを確認 |
| 2 | 部分的なエラーの見落とし | テスト全体のログに `panic` や `recovered` などの異常兆候がないか確認 |
| 3 | 迂回処理による偽成功 | テスト内でモックやダミーが過剰に機能していないか確認 |
| 4 | アダプタ・コンフィグの誤適用 | 想定する設定（`model_profiles.yaml` 等）が正しく適用されているか確認 |
| 5 | テスト間の依存・順序問題 | 必要に応じて失敗したテストを単独実行して検証 |

---

## Documentation
None.
