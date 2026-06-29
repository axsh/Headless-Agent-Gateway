# 002-Codex-CLI-Version-Parse-Fix

> **Source Specification**: [002-Codex-CLI-Version-Parse-Fix.md](file:///prompts/phases/000-foundation/branches/feat-multi-modal-api/ideas/002-Codex-CLI-Version-Parse-Fix.md)

## Goal Description
Ternサーバー起動時のエージェントCLIバージョン検出において、テスト環境などで発生する `codex-cli 0.139.0` のような出力に対するパースエラー（invalid version format）を解消します。
エージェントごとにCLIの出力フォーマットや検証ポリシー（最小要件の有無やバージョン閾値など）が異なるため、ファクトリパターン（`VersionParser` インターフェースおよび具象実装 `ClaudeVersionParser`, `CodexVersionParser`）を導入し、パース・検証ロジックを分離します。

## User Review Required
None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| 1. ファクトリパターンの導入によるパース・検証ロジックの分離 | Proposed Changes > version.go |
| 2. `claudecode` 向けのバージョンパースと検証 | Proposed Changes > version.go |
| 3. `codex` 向けのバージョンパースと検証 | Proposed Changes > version.go |
| 4. `detectCLIVersions` の修正 | Proposed Changes > service.go |
| 5. ユニットテストの修正・追加 | Proposed Changes > version_test.go |

## Proposed Changes

### agentservice パッケージ

---

#### [MODIFY] [version_test.go](file:///shared/libs/go/agentservice/version_test.go)
*   **Description**: 既存のパースおよびチェックテストを、エージェントごとの具象実装およびファクトリを対象にしたテスト（`TestClaudeVersionParser`, `TestCodexVersionParser`）に刷新します。
*   **Technical Design**:
    *   `TestParseCLIVersion` と `TestCheckCLIVersion` を削除し、`TestClaudeVersionParser` と `TestCodexVersionParser` を実装します。
    *   TDDの規則に従い、実装前にテストを変更して実行し、失敗すること（Red）を確認します。
*   **Logic**:
    ```go
    package agentservice

    import "testing"

    func TestClaudeVersionParser(t *testing.T) {
        parser := GetVersionParser("claudecode")
        if parser == nil {
            t.Fatal("expected non-nil parser for claudecode")
        }

        // Test Parse
        parseTests := []struct {
            name      string
            input     string
            wantMajor int
            wantMinor int
            wantPatch int
            wantErr   bool
        }{
            {
                name:      "v2.1.169 with suffix",
                input:     "2.1.169 (Claude Code)",
                wantMajor: 2, wantMinor: 1, wantPatch: 169,
            },
            {
                name:      "v2.0.14 old version",
                input:     "2.0.14 (Claude Code)",
                wantMajor: 2, wantMinor: 0, wantPatch: 14,
            },
            {
                name:      "version only",
                input:     "2.1.169",
                wantMajor: 2, wantMinor: 1, wantPatch: 169,
            },
            {
                name:      "with leading v",
                input:     "v2.1.169",
                wantMajor: 2, wantMinor: 1, wantPatch: 169,
            },
            {
                name:    "empty string",
                input:   "",
                wantErr: true,
            },
        }

        for _, tt := range parseTests {
            t.Run("Parse_"+tt.name, func(t *testing.T) {
                major, minor, patch, err := parser.Parse(tt.input)
                if tt.wantErr {
                    if err == nil {
                        t.Error("expected error, got nil")
                    }
                    return
                }
                if err != nil {
                    t.Fatalf("unexpected error: %v", err)
                }
                if major != tt.wantMajor || minor != tt.wantMinor || patch != tt.wantPatch {
                    t.Errorf("got %d.%d.%d, want %d.%d.%d",
                        major, minor, patch,
                        tt.wantMajor, tt.wantMinor, tt.wantPatch)
                }
            })
        }

        // Test Check
        checkTests := []struct {
            name    string
            raw     string
            wantErr bool
        }{
            {name: "v2.1.169 OK", raw: "2.1.169 (Claude Code)", wantErr: false},
            {name: "v2.1.0 exact minimum", raw: "2.1.0", wantErr: false},
            {name: "v2.0.14 too old", raw: "2.0.14 (Claude Code)", wantErr: true},
            {name: "v1.99.0 too old", raw: "1.99.0", wantErr: true},
            {name: "v3.0.0 future OK", raw: "3.0.0", wantErr: false},
            {name: "unavailable skipped", raw: "unavailable", wantErr: false},
            {name: "empty skipped", raw: "", wantErr: false},
        }

        for _, tt := range checkTests {
            t.Run("Check_"+tt.name, func(t *testing.T) {
                err := parser.Check(tt.raw)
                if tt.wantErr && err == nil {
                    t.Error("expected error, got nil")
                }
                if !tt.wantErr && err != nil {
                    t.Errorf("unexpected error: %v", err)
                }
            })
        }
    }

    func TestCodexVersionParser(t *testing.T) {
        parser := GetVersionParser("codex")
        if parser == nil {
            t.Fatal("expected non-nil parser for codex")
        }

        // Test Parse
        parseTests := []struct {
            name      string
            input     string
            wantMajor int
            wantMinor int
            wantPatch int
            wantErr   bool
        }{
            {
                name:      "codex-cli version",
                input:     "codex-cli 0.139.0",
                wantMajor: 0, wantMinor: 139, wantPatch: 0,
            },
            {
                name:      "version only",
                input:     "0.139.0",
                wantMajor: 0, wantMinor: 139, wantPatch: 0,
            },
            {
                name:      "with leading v",
                input:     "v1.2.3",
                wantMajor: 1, wantMinor: 2, wantPatch: 3,
            },
            {
                name:    "empty string",
                input:   "",
                wantErr: true,
            },
        }

        for _, tt := range parseTests {
            t.Run("Parse_"+tt.name, func(t *testing.T) {
                major, minor, patch, err := parser.Parse(tt.input)
                if tt.wantErr {
                    if err == nil {
                        t.Error("expected error, got nil")
                    }
                    return
                }
                if err != nil {
                    t.Fatalf("unexpected error: %v", err)
                }
                if major != tt.wantMajor || minor != tt.wantMinor || patch != tt.wantPatch {
                    t.Errorf("got %d.%d.%d, want %d.%d.%d",
                        major, minor, patch,
                        tt.wantMajor, tt.wantMinor, tt.wantPatch)
                }
            })
        }

        // Test Check
        checkTests := []struct {
            name string
            raw  string
        }{
            {name: "any version is OK", raw: "codex-cli 0.139.0"},
            {name: "even old version is OK", raw: "0.1.0"},
            {name: "empty raw is OK", raw: ""},
        }

        for _, tt := range checkTests {
            t.Run("Check_"+tt.name, func(t *testing.T) {
                err := parser.Check(tt.raw)
                if err != nil {
                    t.Errorf("unexpected error: %v", err)
                }
            })
        }
    }
    ```

---

#### [MODIFY] [version.go](file:///shared/libs/go/agentservice/version.go)
*   **Description**: `VersionParser` インターフェース、具象構造体 `ClaudeVersionParser`, `CodexVersionParser` および `GetVersionParser` ファクトリを実装します。
*   **Technical Design**:
    *   既存の `checkCLIVersion` および `parseCLIVersion` 関数を削除します。
    *   正規表現 `\d+\.\d+(?:\.\d+)?` を用いたパースロジックを各構造体の `Parse` メソッドに実装します。
*   **Logic**:
    ```go
    package agentservice

    import (
        "fmt"
        "regexp"
        "strconv"
        "strings"
    )

    const minClaudeCLIVersion = "2.1.0"

    // VersionParser defines interface for agent-specific version parsing and verification.
    type VersionParser interface {
        Parse(raw string) (major, minor, patch int, err error)
        Check(raw string) error
    }

    var versionRegex = regexp.MustCompile(`\d+\.\d+(?:\.\d+)?`)

    // GetVersionParser returns the VersionParser implementation for the given agent.
    func GetVersionParser(agentName string) VersionParser {
        switch agentName {
        case "claudecode":
            return &ClaudeVersionParser{}
        case "codex":
            return &CodexVersionParser{}
        default:
            return nil
        }
    }

    type ClaudeVersionParser struct{}

    func (p *ClaudeVersionParser) Parse(raw string) (major, minor, patch int, err error) {
        versionStr := versionRegex.FindString(raw)
        if versionStr == "" {
            return 0, 0, 0, fmt.Errorf("invalid version format: %q", raw)
        }
        segments := strings.Split(versionStr, ".")
        major, _ = strconv.Atoi(segments[0])
        minor, _ = strconv.Atoi(segments[1])
        if len(segments) >= 3 {
            patch, _ = strconv.Atoi(segments[2])
        }
        return major, minor, patch, nil
    }

    func (p *ClaudeVersionParser) Check(raw string) error {
        if raw == "" || raw == "unavailable" {
            return nil
        }
        major, minor, _, err := p.Parse(raw)
        if err != nil {
            return fmt.Errorf("failed to parse CLI version %q: %w", raw, err)
        }
        minMajor, minMinor, _, _ := p.Parse(minClaudeCLIVersion)
        if major < minMajor || (major == minMajor && minor < minMinor) {
            return fmt.Errorf(
                "Claude Code CLI version %s is not supported. Minimum required: %s. Run \"claude update\" to upgrade",
                raw, minClaudeCLIVersion,
            )
        }
        return nil
    }

    type CodexVersionParser struct{}

    func (p *CodexVersionParser) Parse(raw string) (major, minor, patch int, err error) {
        versionStr := versionRegex.FindString(raw)
        if versionStr == "" {
            return 0, 0, 0, fmt.Errorf("invalid version format: %q", raw)
        }
        segments := strings.Split(versionStr, ".")
        major, _ = strconv.Atoi(segments[0])
        minor, _ = strconv.Atoi(segments[1])
        if len(segments) >= 3 {
            patch, _ = strconv.Atoi(segments[2])
        }
        return major, minor, patch, nil
    }

    func (p *CodexVersionParser) Check(raw string) error {
        return nil
    }
    ```

---

#### [MODIFY] [service.go](file:///shared/libs/go/agentservice/service.go)
*   **Description**: `detectCLIVersions` において `GetVersionParser` を用いて、エージェント特有のパーサーとチェッカーを呼び出します。
*   **Technical Design**:
    *   `checkCLIVersion` の直接呼び出しを廃止し、`GetVersionParser` で取得したパーサーを介して `Parse` と `Check` を行います。
*   **Logic**:
    ```go
    // detectCLIVersions runs "claude --version" / "codex --version" once at init.
    // Returns a map of agent name -> version string (or "unavailable").
    // Logs an error if a detected version does not meet the minimum requirement.
    func detectCLIVersions(agents map[string]codingagent.CodingAgent, log logger.Logger) map[string]string {
        versions := make(map[string]string)
        cliNames := map[string]string{
            "claudecode": "claude",
            "codex":      "codex",
        }
        for agentName := range agents {
            cliName, ok := cliNames[agentName]
            if !ok {
                versions[agentName] = "unavailable"
                continue
            }
            out, err := exec.Command(cliName, "--version").Output()
            if err != nil {
                versions[agentName] = "unavailable"
                continue
            }
            versionStr := strings.TrimSpace(string(out))
            versions[agentName] = versionStr

            // Use the agent-specific parser/validator from factory
            parser := GetVersionParser(agentName)
            if parser != nil {
                if _, _, _, err := parser.Parse(versionStr); err != nil {
                    if log != nil {
                        log.Error("failed to parse CLI version: "+err.Error(), "agent", agentName)
                    }
                } else if verErr := parser.Check(versionStr); verErr != nil {
                    if log != nil {
                        log.Error(verErr.Error(), "agent", agentName)
                    }
                }
            }
        }
        return versions
    }
    ```

## Step-by-Step Implementation Guide

1.  **Step 1: テストコードの修正 (TDD - Red)**:
    *   [version_test.go](file:///shared/libs/go/agentservice/version_test.go) の内容を、ファクトリベース of `TestClaudeVersionParser` と `TestCodexVersionParser` に書き換える。
    *   ビルドおよび単体テストを実行し、テストが失敗するかコンパイルエラーになることを確認する。
        ```bash
        ./scripts/process/build.sh --skip-frontend --skip-etc
        ```

2.  **Step 2: ファクトリおよびパース構造体の実装 (TDD - Green)**:
    *   [version.go](file:///shared/libs/go/agentservice/version.go) を修正。`VersionParser` インターフェース、`ClaudeVersionParser`, `CodexVersionParser` 各構造体、および `GetVersionParser` ファクトリを実装する。
    *   ビルドおよび単体テストを実行し、[version_test.go](file:///shared/libs/go/agentservice/version_test.go) のすべてのテストがパスすることを確認する。
        ```bash
        ./scripts/process/build.sh --skip-frontend --skip-etc
        ```
    *   正常パスしたらGitコミット: `test: implement VersionParser factory and rewrite test cases`

3.  **Step 3: サービスでの呼び出し修正**:
    *   [service.go](file:///shared/libs/go/agentservice/service.go) の `detectCLIVersions` を修正。ファクトリ経由で取得した `parser` を用いる形に書き換える。
    *   ビルドおよび全体単体テストを実行する。
        ```bash
        ./scripts/process/build.sh
        ```
    *   正常パスしたらGitコミット: `fix: run agent-specific CLI version check using factory`

4.  **Step 4: 統合テストおよび総合判定**:
    *   単体テストおよび既存のサーバー統合テストを実行し、リグレッションがないこと、`codex-cli 0.139.0` に起因するパースエラーログが出力されなくなったことを確認する。
        ```bash
        ./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories "common"
        ```

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```
    *   **検証項目**: `TestClaudeVersionParser` および `TestCodexVersionParser` を含む全ユニットテストが正常にパスすること。

2.  **Integration Tests**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories "common"
    ```
    *   **Log Verification**: テスト実行の出力ログにおいて、以前出力されていた `ERROR failed to parse CLI version "codex-cli 0.139.0"...` というログが含まれなくなっていることを確認する。

3.  **E2E Tests (新規/追加)**:
    *   本件は内部のバージョン検証方式のポリモーフィックなリファクタリングであり、外部から観測可能な新規 API 等の追加はないため、新規の E2E テストの追加は省略します。既存のサーバー起動・停止テストで異常ログが出力されないことをもって検証とします。

### テスト項目設計のセルフレビュー
*   **網羅性の検証**: ファクトリから返されるエージェント別パーサーそれぞれについて、様々な形式（バージョン名プレフィックス、vプレフィックス、パッチ省略等）のパースと、本来あるべきチェック挙動（Claudeでの最小検証、Codexでのスキップ）が網羅的にテストされているため、正当性を保証できます。
*   **証拠 of 十分性**: 各具象実装について、期待する semver 値およびエラー挙動を完全に確認できており、十分な証拠が得られます。
*   **迂回・抜け道の排除**: `detectCLIVersions` にて直接ファクトリ関数 `GetVersionParser` を利用し、エージェントごとに独立した実装でパース・チェックされるため、抜け道はありません。

### 総合判定プロセス
テスト実行完了後、以下の判定基準に沿って総合判定を実施し、ウォークスルーに記録します。
1. スキップされたテストがないか
2. テストログ内に意図しない `ERROR` や `panic` などの異常兆候がないか
3. 正確に期待通りのコードパスを通っているか

## Documentation
本修正は内部実装の修正であり、ドキュメントの更新は不要です。
