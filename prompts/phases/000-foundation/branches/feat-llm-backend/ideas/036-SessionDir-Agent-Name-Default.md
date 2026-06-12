# 036: SessionDir デフォルト値にエージェント名を含める

## 背景 (Background)

現在、`session_dir` が未指定の場合、`work_dir` がそのまま `session_dir` として使用される。これには以下の課題がある:

1. **セッションデータとワークスペースの混在**: Claude Code CLI は `session_dir`（`CLAUDE_CONFIG_DIR`）配下にセッションファイルやキャッシュを保存する。`session_dir == work_dir` だと、ユーザーの作業ファイルとセッションデータが同じディレクトリに混在する。
2. **複数エージェントの衝突**: 同一 `work_dir` で異なるエージェント（`claudecode`, `codex`）を使用した場合、セッションデータが同じディレクトリに書き込まれ、衝突する可能性がある。
3. **E2Eテストの `session_dir` 指定の冗長性**: テストコードで `filepath.Join(workDir, "sessions")` のように手動で `session_dir` を構築しており、テスト毎に同じパターンの記述が必要になっている。

### 現在のフォールバック箇所（2箇所）

| 箇所 | ファイル | 現在の挙動 |
| :--- | :--- | :--- |
| HTTPハンドラ | `agentservice/handler.go:95-98` | `session_dir == ""` なら `work_dir` をセット |
| SessionConfig | `codingagent/options.go:103-110` | `SessionDir == ""` なら `AdapterConfig.DefaultSessionDir` -> `WorkDir` の順でフォールバック |

## 要件 (Requirements)

### 必須要件

1. **`session_dir` 未指定時のデフォルト値変更**: `session_dir` が明示的に指定されない場合、デフォルト値を以下とする:
   - `{work_dir}/.{agent_name}`
   - 例: `work_dir=tmp`, `agent=claudecode` の場合 -> `session_dir=tmp/.claudecode`
   - 例: `work_dir=/workspace/project`, `agent=codex` の場合 -> `session_dir=/workspace/project/.codex`

2. **ドット接頭辞の採用**: ディレクトリ名の先頭にドット(`.`)を付けることで、隠しディレクトリとし、ユーザーの作業ファイルと視覚的に区別する。

3. **明示的指定の優先**: `session_dir` がAPIリクエストやCLIオプションで明示的に指定された場合は、その値をそのまま使用する（フォールバックは適用しない）。

4. **変更箇所の統一**: フォールバックロジックは以下の2箇所に存在するため、両方を修正する:
   - `agentservice/handler.go` の `handleCreateSession`
   - `codingagent/options.go` の `ApplyDefaults`

### 任意要件

- `cawa-client` の `--session-dir` デフォルト値の説明文を更新する。

## 実現方針 (Implementation Approach)

### 変更対象ファイル

#### 1. `shared/libs/go/agentservice/handler.go`

```go
// Before:
if record.SessionDir == "" && record.WorkDir != "" {
    record.SessionDir = record.WorkDir
}

// After:
if record.SessionDir == "" && record.WorkDir != "" {
    record.SessionDir = filepath.Join(record.WorkDir, "."+record.AgentName)
}
```

#### 2. `shared/libs/go/codingagent/options.go`

`ApplyDefaults` は `AgentName` を知らないため、このレイヤーではフォールバックを行わない方針とする。`AgentName` を知っている `handler.go` 側で一元的にフォールバックを行う。

ただし、`ApplyDefaults` の `WorkDir` フォールバックは、テストやプログラマティックAPIでの利用のために残す場合、以下の方針で処理する:
- `AdapterConfig` に `AgentName` フィールドを追加する案
- または `ApplyDefaults` でのフォールバックを削除し、`handler.go` に一本化する案

上記の方針はどちらが良いか、実装計画時に検討する。

#### 3. E2Eテスト

テストコードの `createE2ESession` ヘルパーで手動構築していた `session_dir` を削除し、サーバー側のフォールバックに委ねる。

## 検証シナリオ (Verification Scenarios)

1. `session_dir` を指定せずにセッションを作成し、レスポンスの `session_dir` が `{work_dir}/.{agent_name}` であることを確認する
2. `session_dir` を明示的に指定した場合、その値がそのまま使われることを確認する
3. `cawa-client` で `--session-dir` を省略して実行し、`{work_dir}/.claudecode` ディレクトリにセッションデータが保存されることを確認する
4. 異なるエージェント（`claudecode`, `codex`）で同一 `work_dir` を使用した場合、セッションデータが `.claudecode` と `.codex` にそれぞれ分離されることを確認する

## テスト項目 (Testing for the Requirements)

### 単体テスト

- `handler.go` の `handleCreateSession` で `session_dir` 未指定時のデフォルト値テスト
- `options.go` の `ApplyDefaults` の修正に対するテスト（方針による）

### 統合テスト

変更は `agentservice` と `codingagent` に影響するため:

```bash
# ビルドと単体テスト
./scripts/process/build.sh

# 統合テスト（セッション関連）
./scripts/process/integration_test.sh --specify "TestE2E_"
```
