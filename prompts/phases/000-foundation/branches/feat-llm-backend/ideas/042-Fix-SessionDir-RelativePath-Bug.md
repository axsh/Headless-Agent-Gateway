# 042: SessionDir 相対パスによるディレクトリ二重化バグの修正

## 背景 (Background)

`ternctl run` コマンドで `--work-dir tmp` を指定し、`--session-dir` を省略した場合、Claude Code の設定ディレクトリが `tmp/tmp/.claudecode/` という二重パスで作成されてしまうバグが発生している。

### 現象の再現

```bash
./bin/ternctl --log-level trace run --agent claudecode \
  --prompt "please run 'pwd' command and report the result." \
  --work-dir tmp
```

実行結果のセッション情報:
```json
{
  "session_dir": "tmp\\.claudecode",
  "work_dir": "tmp"
}
```

実際に作成されるディレクトリ:
```
tmp/tmp/.claudecode/          <- 二重化している
tmp/tmp/.claudecode/projects/
tmp/tmp/.claudecode/sessions/
...
```

期待されるディレクトリ:
```
tmp/.claudecode/              <- work_dir 直下に作成されるべき
```

### 原因分析

問題は `session_dir` が**相対パス**のまま各コンポーネントに渡されることで発生する:

1. **handler.go (L97-102)**: `session_dir` 未指定時のフォールバック処理
   ```go
   record.SessionDir = filepath.Join(record.WorkDir, "."+record.AgentName)
   // => "tmp/.claudecode" (相対パス)
   ```

2. **process.go (L86-88)**: 環境変数への設定
   ```go
   env["CLAUDE_CONFIG_DIR"] = cfg.SessionDir
   // => "tmp/.claudecode" (相対パスのまま)
   ```

3. **process.go (L140)**: プロセスのCWD設定
   ```go
   cmd.Dir = cfg.WorkDir
   // => "tmp" (相対パスのまま)
   ```

4. **Claude Code CLI の挙動**: `CLAUDE_CONFIG_DIR` が相対パスの場合、CWD (`cmd.Dir = "tmp"`) からの相対で解決される
   - 結果: `tmp/` (CWD) + `tmp/.claudecode` (相対CLAUDE_CONFIG_DIR) = `tmp/tmp/.claudecode/`

Web検索の結果、Claude Code公式ドキュメントでも **`CLAUDE_CONFIG_DIR` には絶対パスを使用することが強く推奨** されている。相対パスの場合、CLIを起動するディレクトリからの相対で解決されるため、一貫性のない動作になる。

同じ問題は Codex の `CODEX_HOME` にも潜在的に存在する。

## 要件 (Requirements)

### 必須要件

1. **R1: session_dir の絶対パス化**: `SessionDir` をCLIプロセスに渡す前に、絶対パスに解決する
   - `options.go` の `ApplyDefaults` 関数、または `handler.go` のフォールバック処理で絶対パス化を行う
   - 修正箇所は最小限にし、全アダプター(Claude Code, Codex)に統一的に適用されるようにする

2. **R2: work_dir の絶対パス化**: `WorkDir` も同様に相対パスで渡された場合は絶対パスに解決する
   - `cmd.Dir` に相対パスが渡されると、Go の `exec.Command` は親プロセスのCWDからの相対で解決するが、`session_dir` の起点として正しく使えるよう絶対パスにする

3. **R3: 既存の明示指定への影響なし**: `--session-dir` で明示的に絶対パスを指定した場合の動作は変更しない

4. **R4: session_dir ストアの整合性**: `SessionRecord` に保存される `session_dir` も絶対パスで保存する（セッション復帰時の一貫性）

### 任意要件

5. **R5: ログの改善**: 絶対パス解決後のパスをログに出力し、デバッグ時にパス解決結果を確認できるようにする

## 実現方針 (Implementation Approach)

### 方針: `ApplyDefaults` での一括絶対パス化

最も影響範囲が小さく、全アダプターに統一的に適用できるアプローチとして、`options.go` の `ApplyDefaults` 関数で `WorkDir` と `SessionDir` を絶対パスに解決する。

#### 変更対象ファイル

1. **`shared/libs/go/codingagent/options.go`** (ApplyDefaults関数)
   - `cfg.WorkDir` を `filepath.Abs()` で絶対パスに変換
   - `SessionDir` のフォールバック計算は絶対パス化された `WorkDir` を使用するため、結果も自然に絶対パスになる
   - 明示指定された `SessionDir` が相対パスの場合も `filepath.Abs()` で解決

2. **`shared/libs/go/agentservice/handler.go`** (handleCreateSession関数)
   - `handler.go` のフォールバック処理（L97-102）は `ApplyDefaults` と重複している
   - **案A**: handler.go 側のフォールバック処理でも `filepath.Abs()` を適用
   - **案B**: handler.go 側のフォールバック処理を削除し、`ApplyDefaults` に統一（推奨）
   - handler.go で保存するレコードにも絶対パスが入るため、R4も自然に満たされる

#### 重複フォールバック処理について

現在、`SessionDir` のフォールバック処理が2箇所に存在する:
- `handler.go` L96-103: セッション作成時のレコードへの書き込み
- `options.go` L105-114: `ApplyDefaults` 関数（実際のCLI起動時）

これは意図的な設計（セッション情報にフォールバック後のパスを記録するため）と考えられるが、ロジックの重複とバグの温床になっている。**案B（handler.goでは絶対パス化のみ行い、フォールバックは `ApplyDefaults` に委任）** が推奨される。ただし、handler.go 側でもレコードに `session_dir` を正しく記録する必要があるため、完全な削除は慎重に検討する。

### コード変更イメージ

#### options.go の ApplyDefaults

```go
func ApplyDefaults(cfg *SessionConfig, ac *AdapterConfig) {
    if cfg.WorkDir == "" {
        cfg.WorkDir = ac.DefaultWorkDir
    }
    // Resolve WorkDir to absolute path
    if cfg.WorkDir != "" {
        if abs, err := filepath.Abs(cfg.WorkDir); err == nil {
            cfg.WorkDir = abs
        }
    }

    if cfg.Model == "" {
        cfg.Model = ac.DefaultModel
    }
    // ... (EnvVars は変更なし)

    // SessionDir fallback: explicit > AdapterConfig > WorkDir/.AgentName > WorkDir
    if cfg.SessionDir == "" {
        if ac.DefaultSessionDir != "" {
            cfg.SessionDir = ac.DefaultSessionDir
        } else if cfg.WorkDir != "" && ac.AgentName != "" {
            cfg.SessionDir = filepath.Join(cfg.WorkDir, "."+ac.AgentName)
        } else if cfg.WorkDir != "" {
            cfg.SessionDir = cfg.WorkDir
        }
    }
    // Resolve SessionDir to absolute path
    if cfg.SessionDir != "" {
        if abs, err := filepath.Abs(cfg.SessionDir); err == nil {
            cfg.SessionDir = abs
        }
    }
}
```

#### handler.go の handleCreateSession

```go
// SessionDir and WorkDir: resolve to absolute paths for consistency.
if record.WorkDir != "" {
    if abs, err := filepath.Abs(record.WorkDir); err == nil {
        record.WorkDir = abs
    }
}
if record.SessionDir == "" && record.WorkDir != "" {
    if record.AgentName != "" {
        record.SessionDir = filepath.Join(record.WorkDir, "."+record.AgentName)
    } else {
        record.SessionDir = record.WorkDir
    }
}
if record.SessionDir != "" {
    if abs, err := filepath.Abs(record.SessionDir); err == nil {
        record.SessionDir = abs
    }
}
```

## 検証シナリオ (Verification Scenarios)

### シナリオ1: work_dir のみ指定（session_dir 省略）

1. `ternctl run --agent claudecode --prompt "run pwd" --work-dir tmp` を実行
2. セッション情報の `session_dir` が絶対パス（例: `C:\Users\...\tmp\.claudecode`）であることを確認
3. 実際に作成されるディレクトリが `tmp/.claudecode/` であること（二重化しない）を確認
4. Claude Code の CWD が `tmp/` の絶対パスであることを確認

### シナリオ2: session_dir を明示的に相対パスで指定

1. `ternctl run --agent claudecode --prompt "run pwd" --work-dir tmp --session-dir mysession` を実行
2. セッション情報の `session_dir` が絶対パスに解決されていることを確認
3. 実際のディレクトリが `mysession/` 直下に作成されること（`tmp/mysession/` にはならない）を確認

### シナリオ3: session_dir を絶対パスで指定（既存動作の維持）

1. `ternctl run --agent claudecode --prompt "run pwd" --work-dir tmp --session-dir /tmp/my-session` を実行
2. 動作が従来と変わらないことを確認

## テスト項目 (Testing for the Requirements)

### 単体テスト

- `options_test.go`: `ApplyDefaults` に対するテスト
  - 相対パスの `WorkDir` が絶対パスに解決されるか
  - 相対パスの `SessionDir` が絶対パスに解決されるか
  - `SessionDir` フォールバック時に絶対パスで生成されるか
  - 絶対パスの `WorkDir`/`SessionDir` が変更されないか

- `claudecode/process_test.go`: `BuildEnv` で `CLAUDE_CONFIG_DIR` が絶対パスで設定されるか
- `codex/process_test.go`: `BuildEnv` で `CODEX_HOME` が絶対パスで設定されるか

### 統合テスト

```bash
./scripts/process/integration_test.sh --categories common
```

### ビルド

```bash
./scripts/process/build.sh
```
