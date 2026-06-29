# 002-Codex-CLI-Version-Parse-Fix

## 背景 (Background)
現在、Ternサーバーの起動時に各エージェント（`claudecode` や `codex`）のCLIバージョンを検出する処理（`detectCLIVersions`）が実行されます。
しかし、テスト環境や特定の実行環境において `codex --version` を実行した際、出力が `codex-cli 0.139.0` となる場合があります。
現行のバージョンパース処理（`parseCLIVersion`）は、バージョン文字列の最初のトークン（スペース区切り）をバージョン番号とみなしてパースするため、`codex-cli` がバージョン番号として抽出され、`invalid version format: "codex-cli"` というエラーが発生します。

また、`detectCLIVersions` では検出したすべてのCLIのバージョンに対して `minClaudeCLIVersion` （"2.1.0"）との比較による検証（`checkCLIVersion`）を一律の関数で実行しています。そのため、仮に `codex` のバージョンが `0.139.0` と正しくパースできた場合でも、`0.139.0 < 2.1.0` であるため、最小要件を満たさないとしてエラーログが出力されてしまいます。

エージェントごとにCLIの出力フォーマットや検証ポリシー（最小要件の有無やバージョン閾値など）が異なるため、これらを単一のグローバルなパース関数・検証関数で処理することは望ましくありません。

これらの問題を解決するために、以下の修正を行います。

## 要件 (Requirements)
1. **ファクトリパターンの導入によるパース・検証ロジックの分離**:
   - `claudecode` や `codex` などのエージェントごとに、個別のパースおよび検証（最小バージョン要件チェック）ロジックを提供する `VersionParser` インターフェースと、そのファクトリ関数（`GetVersionParser`）を導入すること。
2. **`claudecode` 向けのバージョンパースと検証**:
   - `claudecode` 向けのパースでは、出力文字列（例: `2.1.169 (Claude Code)`）から `{n1}.{n2}.{n3}`（または `{n1}.{n2}`）のパターンの部分を正規表現等を用いて抽出し、パースすること。
   - `minClaudeCLIVersion` ("2.1.0") とのバージョン比較（最小バージョン要件チェック）を適用すること。
3. **`codex` 向けのバージョンパースと検証**:
   - `codex` 向けのパースでは、出力文字列（例: `codex-cli 0.139.0`）から `{n1}.{n2}.{n3}` のパターンの部分を正規表現等を用いて抽出し、パースすること。
   - `codex` については最小バージョン検証ルールを適用せず（常に成功とし）、検出されたバージョン文字列の保存のみを行うこと（エラーログを出力しない）。

## 実装方針 (Implementation Approach)
1. **`VersionParser` インターフェースの追加 (`shared/libs/go/agentservice/version.go`)**:
   ```go
   type VersionParser interface {
       Parse(raw string) (major, minor, patch int, err error)
       Check(raw string) error
   }
   ```
2. **具象パース実装の追加 (`shared/libs/go/agentservice/version.go`)**:
   - `ClaudeVersionParser` と `CodexVersionParser` の各構造体を定義する。
   - それぞれのエージェントの出力形式に合わせた正規表現等による抽出と検証ロジック（`Parse`, `Check` メソッド）を実装する。
   - エージェント名に応じたパーサーを返すファクトリ関数 `GetVersionParser(agentName string) VersionParser` を定義する。
3. **`detectCLIVersions` の修正 (`shared/libs/go/agentservice/service.go`)**:
   - `GetVersionParser(agentName)` から取得したパーサーを介して、各エージェントのパースおよび検証を呼び出すように書き換える。
4. **ユニットテストの修正・追加 (`shared/libs/go/agentservice/version_test.go`)**:
   - 既存の `TestParseCLIVersion` および `TestCheckCLIVersion` を廃止し、ファクトリ経由で取得した `ClaudeVersionParser` と `CodexVersionParser` の挙動をそれぞれ網羅的にテストする `TestClaudeVersionParser` と `TestCodexVersionParser` を実装する。

## 検証シナリオ (Verification Scenarios)
1. サーバー起動時およびテスト実行時において、`codex-cli 0.139.0` のパース失敗によるエラーログ `ERROR failed to parse CLI version "codex-cli 0.139.0": invalid version format: "codex-cli"` が出力されないことを確認する。
2. `claudecode` に対する最小バージョン要件チェックが引き続き正常に機能することを確認する（無効なバージョンや古いバージョンを指定した場合にエラーとなること）。

## テスト項目 (Testing for the Requirements)
1. **ユニットテスト実行**:
   - `shared/libs/go/agentservice` パッケージ配下のテストを実行し、すべてのテストがパスすることを確認する。
   ```bash
   go test -v ./shared/libs/go/agentservice/...
   ```
2. **ビルドおよび全ユニットテスト**:
   - `scripts/process/build.sh` を実行し、プロジェクト全体のビルドおよび全単体テストが正常に完了することを確認する。
   ```bash
   ./scripts/process/build.sh
   ```
