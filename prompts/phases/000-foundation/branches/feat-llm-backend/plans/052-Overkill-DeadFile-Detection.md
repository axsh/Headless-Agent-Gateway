# 052-Overkill-DeadFile-Detection

> **Source Specification**: [041-Overkill-DeadFile-Detection.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/041-Overkill-DeadFile-Detection.md)

## Goal Description

overkill ツールにデッドファイル検出機能を追加する。ファイル内の全シンボルがデッドである場合にファイル単位で報告し、`--execute` モードではファイルごと削除する。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| デッドファイル判定 | Proposed Changes > deadcode.go |
| レポート出力 (テキスト) | Proposed Changes > reporter.go |
| レポート出力 (JSON) | Proposed Changes > reporter.go |
| --execute モードでファイル削除 | Proposed Changes > executor.go |
| テストファイルの考慮 | Proposed Changes > deadcode.go (既存動作維持) |
| デッドシンボルとの二重報告排除 (任意) | Proposed Changes > deadcode.go |

## Proposed Changes

### features/overkill/analyzer

---

#### [MODIFY] [deadcode_test.go](file://features/overkill/analyzer/deadcode_test.go)
*   **Description**: デッドファイル検出のテストを追加 (TDD: テストを先に書く)
*   **Technical Design**:
    ```go
    func TestAnalyze_DeadFile(t *testing.T) {
        dir := setupTestDir(t, map[string]string{
            "main.go": `package main
    func main() { Used() }
    func Used() {}
    `,
            "dead.go": `package main
    func DeadA() {}
    func DeadB() {}
    `,
        })
        result, err := Analyze(dir, nil)
        if err != nil {
            t.Fatalf("Analyze failed: %v", err)
        }
        // dead.go should be detected as a dead file.
        if len(result.DeadFiles) != 1 {
            t.Fatalf("expected 1 dead file, got %d", len(result.DeadFiles))
        }
        if result.DeadFiles[0].File != filepath.Join(dir, "dead.go") {
            t.Errorf("expected dead file 'dead.go', got %q", result.DeadFiles[0].File)
        }
        // DeadA and DeadB should NOT be in DeadSymbols (二重報告排除).
        for _, sym := range result.DeadSymbols {
            if sym.Name == "DeadA" || sym.Name == "DeadB" {
                t.Errorf("dead file symbol %q should not be in DeadSymbols", sym.Name)
            }
        }
    }

    func TestAnalyze_PartiallyDeadFile(t *testing.T) {
        dir := setupTestDir(t, map[string]string{
            "lib.go": `package lib
    func Used() string { return helper() }
    func helper() string { return "hi" }
    func Unused() {}
    `,
        })
        result, err := Analyze(dir, nil)
        if err != nil {
            t.Fatalf("Analyze failed: %v", err)
        }
        // lib.go has both used and unused symbols, should NOT be a dead file.
        if len(result.DeadFiles) != 0 {
            t.Errorf("expected 0 dead files for partially dead file, got %d", len(result.DeadFiles))
        }
        // Unused should still be in DeadSymbols.
        found := false
        for _, sym := range result.DeadSymbols {
            if sym.Name == "Unused" {
                found = true
            }
        }
        if !found {
            t.Error("Unused should be in DeadSymbols")
        }
    }

    func TestExecute_DeadFile(t *testing.T) {
        dir := setupTestDir(t, map[string]string{
            "main.go": `package main
    func main() { Used() }
    func Used() {}
    `,
            "dead.go": `package main
    func DeadA() {}
    func DeadB() {}
    `,
        })
        result, err := Analyze(dir, nil)
        if err != nil {
            t.Fatalf("Analyze failed: %v", err)
        }
        removed, err := Execute(result)
        if err != nil {
            t.Fatalf("Execute failed: %v", err)
        }
        if removed != 2 {
            t.Errorf("expected 2 removed (1 file with 2 symbols), got %d", removed)
        }
        // dead.go should no longer exist.
        if _, err := os.Stat(filepath.Join(dir, "dead.go")); !os.IsNotExist(err) {
            t.Error("dead.go should have been deleted")
        }
        // main.go should still exist.
        if _, err := os.Stat(filepath.Join(dir, "main.go")); err != nil {
            t.Errorf("main.go should still exist: %v", err)
        }
    }
    ```

#### [MODIFY] [deadcode.go](file://features/overkill/analyzer/deadcode.go)
*   **Description**: `DeadFile` 型と `DeadFiles` フィールドを `AnalysisResult` に追加。`Analyze()` 末尾にデッドファイル判定ロジックを追加。
*   **Technical Design**:
    ```go
    // DeadFile represents a file where all definitions are dead.
    type DeadFile struct {
        File    string      // file path
        Package string      // Go package name
        Symbols []SymbolDef // dead symbols in this file
    }

    // AnalysisResult holds the complete analysis results.
    type AnalysisResult struct {
        DeadSymbols []SymbolDef // symbols with no references (excluding dead file members)
        DeadFiles   []DeadFile  // files where all definitions are dead
        AllDefs     []SymbolDef // all collected definitions
        AllRefs     []SymbolRef // all collected references
    }
    ```
*   **Logic** (Analyze() 末尾に追加):
    1. `deadSet` を構築: `map[filePath+name]bool` でデッドシンボルを索引
    2. `fileDefs` を構築: `map[filePath][]SymbolDef` でファイルごとの定義を集約
    3. 各ファイルについて:
       - 定義数が 0 のファイルはスキップ（空ファイル）
       - 全定義が `deadSet` に含まれている場合、`DeadFile` として追加
    4. `DeadFiles` に属するシンボルを `DeadSymbols` から除外

#### [MODIFY] [reporter.go](file://features/overkill/analyzer/reporter.go)
*   **Description**: デッドファイルの報告をテキスト・JSON 両方に追加
*   **Technical Design** (テキスト):
    ```
    === Dead File Report ===

    DEAD FILE  path/to/file.go  (2 dead symbols)
      - func  DeadA
      - func  DeadB

    === Dead Code Report ===
    (既存レポート、デッドファイル所属シンボルは除外)
    ```
*   **Technical Design** (JSON):
    ```go
    type jsonReport struct {
        DeadFiles   []jsonDeadFile `json:"dead_files"`
        DeadSymbols []jsonSymbol   `json:"dead_symbols"`
        Summary     jsonSummary    `json:"summary"`
    }

    type jsonDeadFile struct {
        File    string       `json:"file"`
        Package string       `json:"package"`
        Symbols []jsonSymbol `json:"symbols"`
    }

    type jsonSummary struct {
        DeadFileCount int `json:"dead_file_count"`
        DeadCount     int `json:"dead_count"`
        TotalDefs     int `json:"total_defs"`
        TotalRefs     int `json:"total_refs"`
        PackageCount  int `json:"package_count"`
    }
    ```

#### [MODIFY] [executor.go](file://features/overkill/analyzer/executor.go)
*   **Description**: デッドファイルを `os.Remove()` で削除するロジックを追加
*   **Technical Design**:
    ```go
    func Execute(result *AnalysisResult) (int, error) {
        removed := 0

        // 1. Delete dead files first.
        for _, df := range result.DeadFiles {
            if err := os.Remove(df.File); err != nil {
                return removed, fmt.Errorf("remove dead file %s: %w", df.File, err)
            }
            removed += len(df.Symbols)
        }

        // 2. Remove individual dead symbols from remaining files.
        // (既存ロジック)
        ...
    }
    ```

## Step-by-Step Implementation Guide

1. **テストを先に書く (TDD Red)**:
   - `deadcode_test.go` に `TestAnalyze_DeadFile`, `TestAnalyze_PartiallyDeadFile`, `TestExecute_DeadFile` を追加
   - テスト実行 -> 失敗を確認
   - コミット

2. **DeadFile 型と Analyze() 拡張 (TDD Green)**:
   - `deadcode.go` に `DeadFile` 型を追加
   - `AnalysisResult` に `DeadFiles` フィールドを追加
   - `Analyze()` 末尾にデッドファイル判定ロジックを追加
   - テスト実行 -> `TestAnalyze_DeadFile`, `TestAnalyze_PartiallyDeadFile` が PASS
   - コミット

3. **レポーター拡張**:
   - `reporter.go` の `ReportText` にデッドファイルセクションを追加
   - `reporter.go` の `ReportJSON` に `dead_files` フィールドを追加
   - コミット

4. **エグゼキューター拡張**:
   - `executor.go` の `Execute` にデッドファイル削除ロジックを追加
   - テスト実行 -> `TestExecute_DeadFile` が PASS
   - コミット

5. **ビルドとテスト実行** (Verification Plan 参照)

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```
    - `features/overkill` の全テストが成功すること
    - `bin/overkill` バイナリが生成されること

2. **Integration Tests**:
    E2E テストの追加は不要。理由: overkill は独立した CLI ツールであり、既存のサーバー API に影響しない。ツール自体の動作確認は単体テストで十分カバーされる。

### テスト設計セルフレビュー

- **網羅性**: デッドファイル検出 (全シンボルがデッド)、部分的デッドファイル (非検出)、デッドファイル削除の3ケースをカバー
- **証拠の十分性**: ファイルの存在チェック (`os.Stat`)、`DeadFiles` リストの内容確認、`DeadSymbols` からの除外確認
- **迂回排除**: テストディレクトリ内に明確な「全デッド」と「部分デッド」のファイルを配置し、判定ロジックが正しく動作することを確認
- **依存関係**: Analyze (判定) -> Reporter (出力) -> Execute (削除) のボトムアップ順

### 総合判定

全テスト完了後、testing-rules.md Section 12 に従い総合判定を実施する。
