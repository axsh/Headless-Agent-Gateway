# 030-Loopback-Bind-Firewall-Suppression

> **Source Specification**: prompts/phases/000-foundation/branches/feat-llm-backend/ideas/021-Loopback-Bind-Firewall-Suppression.md

## Goal Description

サーバコンポーネント (`agentservice`, `llmgateway proxy`) およびE2Eテストユーティリティの `net.Listen` バインドアドレスを `0.0.0.0` (全インターフェース) から `127.0.0.1` (ループバック限定) に統一する。これにより、Windows環境でのファイアウォール警告ダイアログの表示を抑制し、テストの完全自動化を実現する。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: agentserviceのバインドアドレス変更 | Proposed Changes > agentservice > service.go |
| R2: llmgateway proxyのバインドアドレス変更 | Proposed Changes > llmgateway > proxy.go |
| R3: E2Eテストの freePort() 関数の修正 | Proposed Changes > E2Eテスト > agentservice_e2e_test.go |
| R4: 既存テストの正常動作維持 | Verification Plan > Build & Unit Tests |

## Proposed Changes

### agentservice

#### [MODIFY] [service.go](file://shared/libs/go/agentservice/service.go)
*   **Description**: `Launch()` メソッドのバインドアドレスを `127.0.0.1` に限定する
*   **Technical Design**:
    *   `Launch()` メソッド (L94) のアドレスフォーマット文字列を変更
*   **Logic**:
    *   変更前:
        ```go
        ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
        ```
    *   変更後:
        ```go
        ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
        ```
    *   `127.0.0.1` を使用する理由: `localhost` はDNS解決でIPv6 (`::1`) に解決される可能性があり、IPv6のループバックでもWindowsファイアウォールがダイアログを表示する場合がある。IPv4ループバックアドレスを直接指定することで確実にループバックのみにバインドされる。
    *   既存の `wsserver` (L55: `127.0.0.1:%d`) および `passthrough` (L24: `127.0.0.1:%d`) と同じパターンに統一する。

---

### llmgateway

#### [MODIFY] [proxy.go](file://shared/libs/go/llmgateway/proxy.go)
*   **Description**: `Launch()` メソッドのバインドアドレスを `127.0.0.1` に限定する
*   **Technical Design**:
    *   `Launch()` メソッド (L63) のアドレスフォーマット文字列を変更
*   **Logic**:
    *   変更前:
        ```go
        addr := fmt.Sprintf(":%d", p.port)
        ```
    *   変更後:
        ```go
        addr := fmt.Sprintf("127.0.0.1:%d", p.port)
        ```
    *   同じパッケージ内の `passthrough.go` は既に `127.0.0.1:%d` を使用しており、本変更によりパッケージ内のバインドパターンが統一される。

---

### E2Eテスト

#### [MODIFY] [agentservice_e2e_test.go](file://tests/agentservice_e2e_test.go)
*   **Description**: `freePort()` ヘルパー関数のバインドアドレスを `127.0.0.1` に限定する
*   **Technical Design**:
    *   `freePort()` 関数 (L37) のリッスンアドレスを変更
*   **Logic**:
    *   変更前:
        ```go
        l, err := net.Listen("tcp", ":0")
        ```
    *   変更後:
        ```go
        l, err := net.Listen("tcp", "127.0.0.1:0")
        ```
    *   `freePort()` はエフェメラルポート取得のためにポートを一瞬開いてすぐ閉じるユーティリティであり、外部接続を受ける必要は一切ない。`127.0.0.1` に限定することで、ポート取得時のファイアウォールダイアログも抑制される。

## Step-by-Step Implementation Guide

本変更は3つの文字列変更のみであり、ロジック変更や新規機能の追加はない。全変更を一度に実施する。

1. **Step 1: agentservice のバインドアドレス変更**:
    *   Edit `shared/libs/go/agentservice/service.go` L94:
        `fmt.Sprintf(":%d", port)` を `fmt.Sprintf("127.0.0.1:%d", port)` に変更する

2. **Step 2: llmgateway proxy のバインドアドレス変更**:
    *   Edit `shared/libs/go/llmgateway/proxy.go` L63:
        `fmt.Sprintf(":%d", p.port)` を `fmt.Sprintf("127.0.0.1:%d", p.port)` に変更する

3. **Step 3: E2Eテスト freePort() のバインドアドレス変更**:
    *   Edit `tests/agentservice_e2e_test.go` L37:
        `net.Listen("tcp", ":0")` を `net.Listen("tcp", "127.0.0.1:0")` に変更する

4. **Step 4: コミット**:
    *   3箇所の変更をまとめてコミットする
    *   コミットメッセージ: `fix: bind all servers to 127.0.0.1 to suppress Windows firewall dialogs`

5. **Step 5: Verification Plan の実行** (下記参照)

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    全体ビルドと単体テストで回帰がないことを確認する。
    ```bash
    ./scripts/process/build.sh
    ```

2.  **E2Eテスト不要の理由**:
    本変更は純粋な内部リファクタリング（バインドアドレスの文字列変更のみ）であり、外部から観測可能な動作（API、プロトコル、レスポンス内容）に変更はない。以下の理由からE2Eテストの追加は不要:
    - 変更前後でサーバは同じHTTPリクエストを同じように処理する
    - クライアントは全て `localhost` または `127.0.0.1` で接続しており、接続先の変更はない
    - 既存のE2Eテスト (`TestE2E_StandaloneHealth` 等) がビルド&実行されることで、サーバの起動と通信が正常に動作することが暗黙的に確認される（ただし本テストはclaude CLIが必要なため通常のビルドパイプラインでは実行されない）
    - Windowsファイアウォールダイアログの抑制は手動確認事項であり、自動テストで検証する手段がない

3.  **統合テスト不要の理由**:
    サーバ間通信のプロトコルやAPIに変更がないため、統合テストの実行は不要。バインドアドレスの変更はOSレベルのネットワーク設定にのみ影響し、アプリケーション層の動作には影響しない。

### テスト項目のセルフレビュー結果

1. **網羅性の検証**: ビルドと単体テストの全パスにより、3箇所の変更がコンパイルエラーやリッスンエラーを引き起こさないことが確認される。十分である。
2. **証拠の十分性**: 単体テストの中にHTTPサーバを起動してリクエストを送信するテストが含まれている場合、`127.0.0.1` でのリッスンと接続が実際に動作することが証明される。
3. **迂回・抜け道の排除**: 該当なし。バインドアドレスの変更は一方向であり、フォールバックパスは存在しない。
4. **依存関係の整合性**: 本変更は末端のネットワーク層のみであり、上位のアプリケーションロジックとの依存関係に影響しない。

### 総合判定プロセス

ビルドと単体テストの全成功をもって、以下を確認する:
- スキップされたテストがないこと
- テストログにERROR/WARN/panicがないこと
- 全てのサーバコンポーネントが正常に起動・終了すること

## Documentation

本変更はドキュメントの更新を必要としない。変更内容は内部実装の詳細であり、外部仕様やAPI仕様に影響しないため。
