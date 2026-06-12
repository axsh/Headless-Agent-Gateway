# 041: Overkill デッドファイル検出の追加

## 背景 (Background)

050 で実装した overkill ツールは、個別のシンボル（関数、型、変数等）単位でデッドコードを検出する機能を持つ。しかし、「ファイル内の全シンボルがデッドである」場合にファイル丸ごと不要であることを報告する機能がない。

実プロジェクトのリファクタリングでは、使われなくなったファイル全体を検出・削除したいケースが頻繁にある（例: 038 で削除した `passthrough.go` のように）。現在のシンボル単位のレポートでは、ファイル内の全シンボルが個別に DEAD と報告されるだけで、「このファイル丸ごと消してよい」という判断を利用者が手動で行う必要がある。

## 要件 (Requirements)

### 必須要件

1. **デッドファイル判定**: あるファイルに定義された全シンボルがデッドシンボルに含まれている場合、そのファイルを「デッドファイル」として識別する
2. **レポート出力 (テキスト)**: デッドファイルを個別のシンボルとは区別して、ファイル単位でまとめて報告する
   - 例: `DEAD FILE  shared/libs/go/llmgateway/passthrough.go  (2 symbols: NewPassthroughDriver, PassthroughDriver)`
3. **レポート出力 (JSON)**: `dead_files` フィールドを追加し、デッドファイルの一覧を出力する
4. **--execute モード**: デッドファイルの場合はシンボル個別削除ではなくファイル自体を `os.Remove()` で削除する
5. **テストファイルの考慮**: `_test.go` は定義を収集しないため、デッドファイル判定の対象外とする（現状の動作を維持）

### 任意要件

6. **デッドシンボルとの二重報告の排除**: デッドファイルに属するシンボルは `dead_symbols` から除外し、`dead_files` のみに含める（レポートの見通しを改善）
7. **空ファイル判定**: 定義が0個のファイル（コメントのみ、package 宣言のみ等）もデッドファイル候補として報告する

## 実現方針 (Implementation Approach)

### AnalysisResult の拡張

```go
type DeadFile struct {
    File    string      // file path
    Package string      // Go package name
    Symbols []SymbolDef // dead symbols in this file
}

type AnalysisResult struct {
    DeadSymbols []SymbolDef // symbols with no references (excluding dead file members)
    DeadFiles   []DeadFile  // files where all definitions are dead
    AllDefs     []SymbolDef
    AllRefs     []SymbolRef
}
```

### Analyze() の拡張

`Analyze()` 関数の末尾に以下のロジックを追加:

1. ファイルごとの定義数を集計: `map[filePath][]SymbolDef`
2. ファイルごとのデッドシンボル数を集計
3. 定義数 > 0 かつ 全定義がデッドの場合、`DeadFiles` に追加
4. `DeadSymbols` からデッドファイルに属するシンボルを除外

### レポーターの拡張

テキストレポート:
```
=== Dead File Report ===

DEAD FILE  shared/libs/go/llmgateway/passthrough.go  (2 dead symbols)
  - func  NewPassthroughDriver
  - type  PassthroughDriver

=== Dead Code Report ===
(既存のシンボル単位レポート、デッドファイル所属シンボルを除外)
```

JSON レポート:
```json
{
  "dead_files": [
    {
      "file": "shared/libs/go/llmgateway/passthrough.go",
      "package": "llmgateway",
      "symbols": [...]
    }
  ],
  "dead_symbols": [...],
  "summary": {
    "dead_file_count": 1,
    "dead_count": 15,
    ...
  }
}
```

### エグゼキューターの拡張

```go
func Execute(result *AnalysisResult) (int, error) {
    // 1. Delete dead files first (os.Remove)
    // 2. Then remove individual dead symbols from remaining files
}
```

## 検証シナリオ (Verification Scenarios)

1. テスト用ディレクトリに「全シンボルがデッド」なファイルを作成し、`Analyze()` が `DeadFiles` にそのファイルを含めることを確認
2. デッドファイルに属するシンボルが `DeadSymbols` から除外されることを確認
3. 一部のシンボルのみデッドなファイルは `DeadFiles` に含まれないことを確認
4. テキストレポートに `DEAD FILE` 行が出力されることを確認
5. JSON レポートに `dead_files` フィールドが含まれることを確認
6. `--execute` モードでデッドファイルが物理削除されることを確認

## テスト項目 (Testing for the Requirements)

### 単体テスト

```bash
cd features/overkill && go test -v ./analyzer/ -run TestAnalyze_DeadFile
cd features/overkill && go test -v ./analyzer/ -run TestAnalyze_PartiallyDeadFile
cd features/overkill && go test -v ./analyzer/ -run TestReport
cd features/overkill && go test -v ./analyzer/ -run TestExecute_DeadFile
```

### ビルド検証

```bash
./scripts/process/build.sh
```

overkill は独立した CLI ツールであり、既存のサーバー API に影響しないため、統合テストの追加は不要。
