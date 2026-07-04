# 000-codex-stdin-prompt

> **Source Specification**: prompts/phases/000-foundation/branches/fix-codex-stdin-blocking/ideas/000-codex-stdin-prompt.md

## Goal Description

`codex` CLI へのプロンプト受け渡しを、コマンドライン引数から標準入力 (stdin) 経由に変更し、Windows のコマンドライン文字数制限 (8,191文字) によるクラッシュを解消する。また、`codex` および `claudecode` の標準エラー出力 (stderr) をリアルタイムでログに記録し、トラブルシューティングを容易にする。

## User Review Required

None. (ユーザーレビュー済み: claudecode にも stderr リアルタイムログを適用する方針で合意)

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: プロンプトを stdin 経由で渡す (引数ではなく) | Proposed Changes > codex/process.go: BuildArgs, StartProcess |
| R2: コマンドライン引数に `-` を指定 | Proposed Changes > codex/process.go: BuildArgs |
| R3: stderr をリアルタイムでスキャンし Debug ログ出力 | Proposed Changes > codex/process.go, claudecode/process.go: StartProcess (stderrスキャンgoroutine) |
| R4: stdout の未解析行ログを Debug レベルに引き上げ | Proposed Changes > codex/process.go: StartProcess (stdoutスキャンループ) |
| R5: stdin 書き込み後に適切に閉じる | Proposed Changes > codex/process.go: StartProcess (io.Pipe + goroutine) |
| R6: claudecode に stderr リアルタイムログを適用 | Proposed Changes > claudecode/process.go: StartProcess |

## Proposed Changes

### codex パッケージ

#### [MODIFY] [process_test.go](file://shared/libs/go/codingagent/codex/process_test.go)
*   **Description**: `BuildArgs` の変更に対応するユニットテストの更新、および stdin 関連テストの追加。
*   **Technical Design**:
    ```go
    func TestCodexBuildArgs_StdinMode(t *testing.T) {
        // prompt 非空の場合、末尾が "-" になることを検証
    }
    func TestCodexBuildArgs_EmptyPrompt(t *testing.T) {
        // prompt 空の場合、"-" が付かないことを検証
    }
    ```
*   **Logic**:
    *   `BuildArgs("some prompt", overrides)` 呼び出し後、末尾の引数が `"-"` であることを確認する。
    *   `BuildArgs("", overrides)` 呼び出し後、引数に `"-"` が含まれないことを確認する。
    *   既存の `TestCodexBuildArgs` テストは、プロンプトが引数末尾に含まれる前提のアサーションを修正する。

---

#### [MODIFY] [process.go](file://shared/libs/go/codingagent/codex/process.go)
*   **Description**: `BuildArgs` と `StartProcess` を変更し、プロンプトを stdin 経由で渡す。stderr リアルタイムログを追加。
*   **Technical Design**:

    **`BuildArgs` 関数シグネチャの変更**:
    ```go
    // BuildArgs constructs codex CLI arguments for non-interactive execution.
    // When prompt is non-empty, "-" is appended to instruct codex to read from stdin.
    func BuildArgs(prompt string, configOverrides []string) []string {
        args := []string{
            "exec",
            "--json",
            "--dangerously-bypass-approvals-and-sandbox",
            "--ignore-user-config",
        }
        args = append(args, configOverrides...)
        if prompt != "" {
            args = append(args, "-")
        }
        return args
    }
    ```

    **`StartProcess` の変更 (stdin パイプ)**:
    ```go
    // cmd.Stdin の設定部分を変更:
    // 旧: cmd.Stdin = bytes.NewReader(nil)
    // 新: io.Pipe を使い、prompt を書き込んだ後 close する。
    stdinReader, stdinWriter := io.Pipe()
    cmd.Stdin = stdinReader

    // プロンプト書き込み goroutine (cmd.Start() の後に実行)
    go func() {
        defer stdinWriter.Close()
        if cfg.Prompt != "" {
            if _, err := io.WriteString(stdinWriter, cfg.Prompt); err != nil {
                log.Warn("failed to write prompt to stdin", "error", err)
            }
        }
    }()
    ```

    **`StartProcess` の変更 (stderr リアルタイムログ)**:
    ```go
    // 旧: var stderrBuf bytes.Buffer; cmd.Stderr = &stderrBuf
    // 新: cmd.StderrPipe() でリアルタイムスキャン + バッファ蓄積

    stderrPipe, err := cmd.StderrPipe()
    if err != nil {
        cancel()
        return nil, nil, fmt.Errorf("stderr pipe: %w", err)
    }

    var stderrBuf bytes.Buffer
    // stderr をリアルタイムにスキャンする goroutine
    stderrDone := make(chan struct{})
    go func() {
        defer close(stderrDone)
        scanner := bufio.NewScanner(stderrPipe)
        for scanner.Scan() {
            line := scanner.Text()
            stderrBuf.WriteString(line)
            stderrBuf.WriteString("\n")
            log.Debug("CLI stderr line", "line", line)
        }
    }()
    ```

    **`StartProcess` の変更 (stdout 未解析行ログレベル)**:
    ```go
    // 旧:
    // log.Trace("unhandled codex event type (ignored)", "line", line)
    // 新:
    log.Debug("unhandled codex event type (ignored)", "line", line)
    ```

    **stdout goroutine 内での stderr 完了待ち**:
    ```go
    // cmd.Wait() の前に stderrDone を待つ
    <-stderrDone
    if err := cmd.Wait(); err != nil {
        // ... 既存のエラー処理 (stderrBuf.String() でエラーメッセージ取得)
    }
    ```

*   **Logic**:
    1. `BuildArgs`: `prompt` が空でなければ末尾に `"-"` を追加。`prompt` 文字列そのものは引数に含めない。
    2. `StartProcess`: `io.Pipe` でstdinリーダー/ライターを作成し、`cmd.Stdin` にリーダーを設定。`cmd.Start()` 後に goroutine でプロンプトを書き込み、書き込み完了後に `stdinWriter.Close()` を呼ぶことで、codex に EOF を通知する。
    3. `cmd.StderrPipe()` で stderr をリアルタイムに読み取り、各行を `log.Debug` で出力しつつ `stderrBuf` にも蓄積する。プロセス終了時のエラーメッセージ報告は既存ロジックをそのまま利用する。
    4. stdout スキャンループの `ParseExecEvent` が `nil` を返した場合のログレベルを `Trace` から `Debug` に変更する。

### claudecode パッケージ

#### [MODIFY] [process.go](file://shared/libs/go/codingagent/claudecode/process.go)
*   **Description**: `StartProcess` の stderr 処理をリアルタイムログ出力に変更する。プロンプトの渡し方 (`-p` フラグ) は変更しない。
*   **Technical Design**:

    **`StartProcess` の変更 (stderr リアルタイムログ)**:
    ```go
    // 旧 (L168-170):
    // var stderrBuf bytes.Buffer
    // cmd.Stderr = &stderrBuf
    //
    // 新: cmd.StderrPipe() でリアルタイムスキャン + バッファ蓄積

    stderrPipe, err := cmd.StderrPipe()
    if err != nil {
        cancel()
        return nil, nil, fmt.Errorf("stderr pipe: %w", err)
    }

    var stderrBuf bytes.Buffer
    stderrDone := make(chan struct{})
    go func() {
        defer close(stderrDone)
        scanner := bufio.NewScanner(stderrPipe)
        for scanner.Scan() {
            line := scanner.Text()
            stderrBuf.WriteString(line)
            stderrBuf.WriteString("\n")
            log.Debug("CLI stderr line", "line", line)
        }
    }()
    ```

    **stdout goroutine 内での stderr 完了待ち**:
    ```go
    // cmd.Wait() の前に stderrDone を待つ
    <-stderrDone
    if err := cmd.Wait(); err != nil {
        // ... 既存のエラー処理 (stderrBuf.String())
    }
    ```

*   **Logic**:
    1. `cmd.Stderr = &stderrBuf` を `cmd.StderrPipe()` に置き換え、stderr をリアルタイムにスキャンする goroutine を追加する。codex と同じパターン。
    2. `ProcessManager` の `stderrBuf` フィールドは `*bytes.Buffer` のまま維持する（`Stop()` メソッドで参照されるため）。goroutine 内で `&stderrBuf` のポインタを `ProcessManager` に渡す。
    3. stdout goroutine 内で `cmd.Wait()` の前に `<-stderrDone` を追加し、stderr スキャンの完了を待つ。

## Step-by-Step Implementation Guide

1.  **テストの更新 (TDD: Red)**:
    *   `shared/libs/go/codingagent/codex/process_test.go` を編集:
        *   既存の `TestCodexBuildArgs` を修正: 「末尾の引数が prompt 文字列そのもの」から「末尾の引数が `"-"` 」に期待値を変更する。
        *   新規テスト `TestCodexBuildArgs_StdinMode` を追加: prompt 非空の場合、末尾が `"-"` であることを検証する。
        *   新規テスト `TestCodexBuildArgs_EmptyPrompt` を追加: prompt 空の場合、`"-"` が引数に含まれないことを検証する。
    *   この時点でテストが FAIL することを確認する。

2.  **`BuildArgs` の変更 (TDD: Green)**:
    *   `shared/libs/go/codingagent/codex/process.go` の `BuildArgs` 関数を変更:
        *   L41 の `args = append(args, prompt)` を削除。
        *   代わりに `if prompt != "" { args = append(args, "-") }` を追加。
    *   テストが PASS することを確認する。
    *   `git commit`

3.  **`StartProcess` の stdin パイプ変更**:
    *   `shared/libs/go/codingagent/codex/process.go` の `StartProcess` 関数を変更:
        *   L184-185 の `cmd.Stdin = bytes.NewReader(nil)` を削除。
        *   代わりに `io.Pipe()` を使用した stdin パイプを設定するコードを追加（上記 Technical Design 参照）。
        *   `import` に `"io"` を追加する。
        *   `cmd.Start()` の直後にプロンプト書き込み goroutine を追加。
    *   `git commit`

4.  **`StartProcess` の stderr リアルタイムログ追加**:
    *   `shared/libs/go/codingagent/codex/process.go` の `StartProcess` 関数を変更:
        *   L187-189 の `var stderrBuf bytes.Buffer; cmd.Stderr = &stderrBuf` を削除。
        *   代わりに `cmd.StderrPipe()` + scanner goroutine + `stderrDone` チャネルのコードを追加（上記 Technical Design 参照）。
        *   stdout goroutine 内で `cmd.Wait()` の前に `<-stderrDone` を追加し、stderr スキャン完了を待つ。
    *   `git commit`

5.  **stdout 未解析行のログレベル変更**:
    *   `shared/libs/go/codingagent/codex/process.go` の L219:
        *   `log.Trace("unhandled codex event type (ignored)", "line", line)` を `log.Debug(...)` に変更する。
    *   `git commit`

6.  **`claudecode` の stderr リアルタイムログ追加**:
    *   `shared/libs/go/codingagent/claudecode/process.go` の `StartProcess` 関数を変更:
        *   L168-170 の `var stderrBuf bytes.Buffer; cmd.Stderr = &stderrBuf` を削除。
        *   代わりに `cmd.StderrPipe()` + scanner goroutine + `stderrDone` チャネルのコードを追加（codex と同じパターン）。
        *   L182 の `ProcessManager` 初期化で `stderrBuf: &stderrBuf` を維持する。
        *   stdout goroutine 内 (L199-200 付近) で `cmd.Wait()` の前に `<-stderrDone` を追加。
    *   `git commit`

7.  **ビルドとユニットテスト実行**:
    *   `./scripts/process/build.sh` を実行し、コンパイルエラーがないことを確認する。
    *   ユニットテスト (`process_test.go`) が全て PASS することを確認する。

8.  **E2E テスト実行 (codex + claudecode)**:
    *   `./scripts/process/integration_test.sh --specify "TestCodexE2E|TestE2E_"` を実行し、codex および claudecode の E2E テストがパスすることを確認する。

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```

2.  **Unit Tests (codex package)**:
    ビルドスクリプトに含まれるが、以下のテストケースが PASS することを特に確認する:
    *   `TestCodexBuildArgs` (修正済み: `-` が末尾であること)
    *   `TestCodexBuildArgs_StdinMode` (新規: prompt 非空 → `-` あり)
    *   `TestCodexBuildArgs_EmptyPrompt` (新規: prompt 空 → `-` なし)

3.  **Integration Tests (Codex + ClaudeCode E2E)**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestCodexE2E|TestE2E_"
    ```
    *   **Log Verification**: サーバーログに以下が出力されることを確認する:
        *   `CLI stderr line` のエントリ (codex/claudecode 両方で stderr 出力がある場合)
        *   `unhandled codex event type` が `Debug` レベルで出力されること
        *   `Reading additional input from stdin...` エラーが出力されないこと

4.  **E2E Tests (新規/追加)**:

    今回、外部から観測可能な API の動作変更はありません（プロンプトの渡し方は内部実装の変更のみ）。既存の E2E テストが stdin 経由での動作 (codex) および stderr リアルタイムログ (codex/claudecode) を検証するリグレッションテストとして機能します。

    **codex E2E**: `TestCodexE2E_FileCreation`, `TestCodexE2E_GeminiModel_FileCreation`, `TestCodexE2E_GPT5Codex_FileCreation`, `TestCodexE2E_ErrorPropagation`, `TestCodexE2E_TernctlRealCommand`
    **claudecode E2E**: `TestE2E_CodingAgentStreaming`, `TestE2E_CodingAgentError`, `TestE2E_SessionContinuation`

    新規 E2E テストの追加は不要と判断します:
    *   理由: codex の変更は stdin 経由でのプロンプト渡しへの内部リファクタリングであり、claudecode の変更は stderr ログ出力のみ。API レスポンスの形式やセッションのライフサイクルに変更はない。
    *   既存の E2E テスト群がすべて PASS すれば、変更が正常に動作していることの証拠となる。

### テスト項目設計のセルフレビュー (Section 11)

テスト項目は以下のボトムアップ順序で設計:

1. **末端 (BuildArgs)**: 引数構築ロジックの正確性 → `TestCodexBuildArgs_StdinMode`, `TestCodexBuildArgs_EmptyPrompt`
2. **中間 (StartProcess stdin/stderr)**: プロセス起動とI/Oパイプの動作 → 既存 E2E テストで間接検証 (codex + claudecode)
3. **全体 (E2E)**: サーバー経由でのセッション作成・メッセージ送信・応答受信 → `TestCodexE2E_*`, `TestE2E_*`

**観点チェックリスト:**

| # | 観点 | 対応状況 |
|---|------|----------|
| 1 | 正常系 | `TestCodexBuildArgs_StdinMode`: prompt あり → `-` 付加。E2E テストで実際のファイル生成確認 |
| 2 | 異常系/境界値 | `TestCodexBuildArgs_EmptyPrompt`: 空 prompt → `-` なし。`TestCodexE2E_ErrorPropagation`: 到達不能なgateway |
| 3 | 外部連携 | E2E テストで実際の codex CLI を使用 |
| 4 | データ一貫性 | stdin に書き込んだ prompt が codex に正しく渡り、期待通りのファイルが生成される |
| 5 | 状態遷移 | セッションステータスが completed に遷移することを E2E で検証 |
| 6 | 設定反映 | BuildEnv テストで環境変数の正確性を検証 (既存テスト、変更なし) |
| 7 | 副作用 | stdinWriter.Close() による EOF 送信、stderrDone チャネルによるリソース解放 |

**セルフレビュー結果 (Section 11.4):**

1. **網羅性**: BuildArgs の入出力パターン (prompt あり/なし) と、E2E での実際の codex CLI 実行を組み合わせることで、変更箇所を網羅している。
2. **証拠の十分性**: E2E テストはファイル生成とセッションステータス完了を検証しており、「stdin 経由でプロンプトが正しく渡った」ことの十分な証拠となる。
3. **迂回排除**: codex CLI は引数にプロンプトが含まれない場合、stdin から読み取る以外に入力を得る方法がないため、迂回の可能性はない。
4. **依存関係**: BuildArgs → StartProcess → E2E の依存チェーンが保たれている。

### 総合判定プロセス (Section 12)

全テスト完了後、以下を確認する:

1. スキップされたテストがないか (特に codex/claude CLI 未検出による Skip)
2. stderr に `Reading additional input from stdin...` が出力されていないか
3. 全 E2E テストが成功しており、フォールバック/リトライによる偽成功でないか
4. codex および claudecode アダプターがそれぞれ使用されていることをログで確認
5. claudecode の stderr リアルタイムログ (`CLI stderr line`) がサーバーログに出力されていることを確認

## Documentation

本計画で `prompts/specifications` フォルダ配下に影響を受ける既存ドキュメントはありません。
