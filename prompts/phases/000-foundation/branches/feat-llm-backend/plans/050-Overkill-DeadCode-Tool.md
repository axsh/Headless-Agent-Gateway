# 050-Overkill-DeadCode-Tool

> **Source Specification**: [039-Overkill-DeadCode-Tool.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/039-Overkill-DeadCode-Tool.md)

## Goal Description

tree-sitter を用いた Go デッドコード検出/削除ツール `overkill` を `features/overkill/` に実装する。

## User Review Required

- tree-sitter の Go バインディングとして、公式の `github.com/tree-sitter/go-tree-sitter` を使用する。CGO が必要（ビルド環境に C コンパイラが必要）。
- Go の `go/ast` / `go/parser` 標準ライブラリという選択肢もあるが、仕様書の要件に従い tree-sitter を使用する。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| tree-sitter で Go パース | Proposed Changes > analyzer/parser.go |
| シンボル定義の抽出 | Proposed Changes > analyzer/symbols.go |
| シンボル参照の収集 | Proposed Changes > analyzer/symbols.go |
| デッドコード判定アルゴリズム | Proposed Changes > analyzer/deadcode.go |
| レポート出力 | Proposed Changes > analyzer/reporter.go |
| --execute 自動削除モード | Proposed Changes > analyzer/executor.go |
| --path, --exclude, --json, --verbose オプション | Proposed Changes > main.go |
| init()/main() 除外 | analyzer/deadcode.go の除外ルール |
| _test.go の扱い | analyzer/symbols.go の収集ロジック |
| // overkill:ignore コメント | analyzer/symbols.go の定義抽出時に判定 |

## Proposed Changes

### features/overkill

---

#### [NEW] [go.mod](file://features/overkill/go.mod)
*   **Description**: overkill ツールの Go モジュール定義
*   **Technical Design**:
    ```
    module github.com/axsh/arctic-tern/features/overkill

    go 1.24

    require (
        github.com/tree-sitter/go-tree-sitter v0.24.0
        github.com/tree-sitter/tree-sitter-go v0.23.0
    )
    ```
    実際のバージョンは `go mod tidy` で解決する。

#### [NEW] [main.go](file://features/overkill/main.go)
*   **Description**: CLI エントリポイント
*   **Technical Design**:
    ```go
    package main

    import (
        "flag"
        "fmt"
        "os"
        "strings"

        "github.com/axsh/arctic-tern/features/overkill/analyzer"
    )

    func main() {
        path := flag.String("path", ".", "Root directory to scan")
        exclude := flag.String("exclude", "reference_repo,vendor,tmp", "Comma-separated exclude patterns")
        execute := flag.Bool("execute", false, "Actually remove dead code (default: report only)")
        jsonOut := flag.Bool("json", false, "Output in JSON format")
        verbose := flag.Bool("verbose", false, "Show reference details")
        flag.Parse()

        excludePatterns := strings.Split(*exclude, ",")

        result, err := analyzer.Analyze(*path, excludePatterns)
        if err != nil {
            fmt.Fprintf(os.Stderr, "Error: %v\n", err)
            os.Exit(1)
        }

        if *jsonOut {
            analyzer.ReportJSON(os.Stdout, result)
        } else {
            analyzer.ReportText(os.Stdout, result, *verbose)
        }

        if *execute {
            removed, err := analyzer.Execute(result)
            if err != nil {
                fmt.Fprintf(os.Stderr, "Error during execution: %v\n", err)
                os.Exit(1)
            }
            fmt.Printf("\nRemoved %d dead symbols.\n", removed)
        }
    }
    ```

#### [NEW] [analyzer/parser.go](file://features/overkill/analyzer/parser.go)
*   **Description**: tree-sitter を使った Go ソースファイルのパース
*   **Technical Design**:
    ```go
    package analyzer

    import (
        sitter "github.com/tree-sitter/go-tree-sitter"
        golang "github.com/tree-sitter/tree-sitter-go"
    )

    // ParseFile parses a Go source file and returns the tree-sitter tree.
    func ParseFile(source []byte) (*sitter.Tree, error) {
        parser := sitter.NewParser()
        defer parser.Close()
        parser.SetLanguage(sitter.NewLanguage(golang.Language()))
        tree := parser.Parse(source, nil)
        return tree, nil
    }
    ```
*   **Logic**: 各 `.go` ファイルを読み込み、`ParseFile` で AST を生成。

#### [NEW] [analyzer/symbols.go](file://features/overkill/analyzer/symbols.go)
*   **Description**: AST からシンボル定義と参照を収集
*   **Technical Design**:
    ```go
    package analyzer

    // SymbolKind represents the kind of a symbol.
    type SymbolKind string
    const (
        KindFunc   SymbolKind = "func"
        KindMethod SymbolKind = "method"
        KindType   SymbolKind = "type"
        KindConst  SymbolKind = "const"
        KindVar    SymbolKind = "var"
    )

    // SymbolDef represents a symbol definition.
    type SymbolDef struct {
        Name       string
        Kind       SymbolKind
        Package    string     // Go package name
        File       string     // file path
        Line       int        // 1-indexed line number
        Exported   bool       // starts with uppercase
        Receiver   string     // for methods: receiver type name
        StartByte  uint       // for deletion: start of the declaration
        EndByte    uint       // for deletion: end of the declaration
        Ignored    bool       // has // overkill:ignore comment
    }

    // SymbolRef represents a reference to a symbol.
    type SymbolRef struct {
        Name    string
        File    string
        Line    int
        Package string // Go package name of the referencing file
    }

    // CollectSymbols walks the AST and extracts definitions and references.
    func CollectSymbols(tree *sitter.Tree, source []byte, filePath string, pkgName string, isTestFile bool) ([]SymbolDef, []SymbolRef) {
        // ... tree-sitter node traversal
    }
    ```
*   **Logic**:
    - `function_declaration` ノード -> `KindFunc` 定義
    - `method_declaration` ノード -> `KindMethod` 定義
    - `type_declaration` > `type_spec` ノード -> `KindType` 定義
    - `const_declaration` > `const_spec` ノード -> `KindConst` 定義
    - `var_declaration` > `var_spec` ノード -> `KindVar` 定義
    - `identifier`, `type_identifier` ノード -> 参照
    - `_test.go` ファイルの場合: 定義は収集しない、参照は収集する
    - 各定義の直前のコメントに `overkill:ignore` があれば `Ignored = true`

#### [NEW] [analyzer/deadcode.go](file://features/overkill/analyzer/deadcode.go)
*   **Description**: デッドコード判定ロジック
*   **Technical Design**:
    ```go
    package analyzer

    // AnalysisResult holds the complete analysis results.
    type AnalysisResult struct {
        DeadSymbols []SymbolDef      // symbols with no references
        AllDefs     []SymbolDef      // all collected definitions
        AllRefs     []SymbolRef      // all collected references
    }

    // Analyze scans the directory tree and finds dead code.
    func Analyze(rootDir string, excludePatterns []string) (*AnalysisResult, error) {
        // 1. Walk rootDir, find all .go files (excluding patterns)
        // 2. For each file: ParseFile -> CollectSymbols
        // 3. Build reference map: map[symbolKey][]SymbolRef
        // 4. For each definition:
        //    a. Skip if name is "init" or "main"
        //    b. Skip if Ignored is true
        //    c. If unexported: check references in same package
        //    d. If exported: check references in all packages (excluding defining file)
        //    e. If no references found: add to DeadSymbols
        // 5. Return AnalysisResult
    }
    ```
*   **Logic**:
    - シンボルキー: `{package}/{name}` (エクスポート) or `{package}:{name}` (非エクスポート)
    - メソッドの場合: `{package}/{ReceiverType}.{MethodName}`
    - 定義自身のファイル内参照は、定義行以外の参照のみカウント
    - インターフェースメソッドの除外: tree-sitter だけでは型推論できないため、`interface_type` ノード内の `method_spec` のメソッド名は参照としてカウント（保守的アプローチ）

#### [NEW] [analyzer/reporter.go](file://features/overkill/analyzer/reporter.go)
*   **Description**: レポート出力 (テキスト / JSON)
*   **Technical Design**:
    ```go
    package analyzer

    import (
        "encoding/json"
        "fmt"
        "io"
        "sort"
    )

    // ReportText outputs dead code report in human-readable format.
    func ReportText(w io.Writer, result *AnalysisResult, verbose bool) {
        // Group by package, sort by file:line
        // Format:
        //   === Dead Code Report ===
        //   Package: llmgateway (shared/libs/go/llmgateway)
        //     DEAD func  NewPassthroughDriver     passthrough.go:19
        //   Summary: N dead symbols found across M packages
    }

    // ReportJSON outputs dead code report in JSON format.
    func ReportJSON(w io.Writer, result *AnalysisResult) {
        // JSON structure: { "dead_symbols": [...], "summary": {...} }
    }
    ```

#### [NEW] [analyzer/executor.go](file://features/overkill/analyzer/executor.go)
*   **Description**: デッドコードの物理削除
*   **Technical Design**:
    ```go
    package analyzer

    // Execute removes dead code from source files.
    // Returns the number of symbols removed.
    func Execute(result *AnalysisResult) (int, error) {
        // 1. Group dead symbols by file
        // 2. For each file:
        //    a. Read file content
        //    b. Remove dead symbol declarations (using StartByte/EndByte)
        //    c. Sort removals in reverse order (to preserve byte offsets)
        //    d. Write back modified content
        //    e. Run gofmt on the file
        // 3. If all symbols in a file are dead, delete the file entirely
    }
    ```

---

### テストファイル

#### [NEW] [analyzer/parser_test.go](file://features/overkill/analyzer/parser_test.go)
*   **Description**: tree-sitter パーサーのテスト
*   **テストケース**:
    - `TestParseFile_SimpleFunction`: 関数宣言をパースできる
    - `TestParseFile_TypeDeclaration`: 型宣言をパースできる
    - `TestParseFile_InvalidSyntax`: 構文エラーでもクラッシュしない

#### [NEW] [analyzer/symbols_test.go](file://features/overkill/analyzer/symbols_test.go)
*   **Description**: シンボル収集のテスト
*   **テストケース**:
    - `TestCollectSymbols_ExportedFunc`: エクスポート関数定義を検出
    - `TestCollectSymbols_Method`: メソッド定義を検出
    - `TestCollectSymbols_References`: 識別子参照を収集
    - `TestCollectSymbols_TestFile`: `_test.go` の定義は収集しない
    - `TestCollectSymbols_IgnoreComment`: `// overkill:ignore` コメント付き定義

#### [NEW] [analyzer/deadcode_test.go](file://features/overkill/analyzer/deadcode_test.go)
*   **Description**: デッドコード判定のテスト
*   **テストケース**:
    - `TestAnalyze_DetectsUnusedExportedFunc`: 参照なしエクスポート関数を検出
    - `TestAnalyze_SkipsInit`: `init()` は検出しない
    - `TestAnalyze_SkipsMain`: `main()` は検出しない
    - `TestAnalyze_SkipsUsedFunc`: 参照ありシンボルは検出しない
    - `TestAnalyze_CrossPackageReference`: 別パッケージからの参照をカウント
    - `TestAnalyze_TestFileReference`: テストファイルからの参照をカウント

## Step-by-Step Implementation Guide

1. **go.mod 作成と依存インストール**:
   - `features/overkill/go.mod` を作成
   - `go mod tidy` で tree-sitter 依存を解決

2. **parser.go + parser_test.go 作成**:
   - tree-sitter パーサーを実装
   - テストで基本的なパースが動作することを確認

3. **symbols.go + symbols_test.go 作成**:
   - シンボル定義・参照の収集ロジックを実装
   - テストで各種シンボルの検出を確認

4. **deadcode.go + deadcode_test.go 作成**:
   - デッドコード判定アルゴリズムを実装
   - テストで判定ロジックを確認

5. **reporter.go 作成**:
   - テキスト・JSON レポーターを実装

6. **executor.go 作成**:
   - デッドコード削除ロジックを実装

7. **main.go 作成**:
   - CLI エントリポイントを実装

8. **ビルド確認**:
   ```bash
   ./scripts/process/build.sh
   ```

9. **実プロジェクトでの動作確認**:
   ```bash
   ./bin/overkill --path ./shared/libs/go
   ```
   既知のデッドコード (`PassthroughDriver`, `TryFallbackOpenAIResponse`, `AllProviders`) が検出されることを確認

10. **コミット**: `git commit -m "feat: add overkill dead code detection tool"`

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```
    *   `bin/overkill` バイナリが生成されること
    *   `features/overkill` の全テストが成功すること

2. **Integration Tests (既知デッドコードの検出)**:
    E2Eテストの追加は不要。理由: overkill は独立したCLIツールであり、既存のサーバーAPI動作に影響しない。ツール自体の動作確認は単体テストで十分カバーされる。

    手動での確認（開発中の理解のため。テストコードの代替ではない）:
    ```bash
    ./bin/overkill --path ./shared/libs/go --exclude reference_repo
    ```
    *   既知のデッドコード (`PassthroughDriver`, `TryFallbackOpenAIResponse`) が検出されること

### テスト設計セルフレビュー

- **網羅性**: parser -> symbols -> deadcode の各レイヤーでテストを設計。ボトムアップ順序。
- **証拠の十分性**: 各テストで「検出されるべきシンボルが検出される」「検出されるべきでないシンボルが検出されない」を確認。
- **迂回排除**: テストデータはインメモリの Go ソースコード文字列を使用し、ファイルシステムに依存しない。
- **依存関係**: parser -> symbols -> deadcode の順で依存。各層の単体テストが独立して成功することを確認。

## Documentation

#### [MODIFY] [features/README.md](file://features/README.md)
*   **更新内容**: `overkill` ツールの説明を追加
