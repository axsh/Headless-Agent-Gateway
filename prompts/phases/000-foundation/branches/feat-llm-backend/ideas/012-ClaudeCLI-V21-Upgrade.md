# 012: Claude CLI v2.1 対応 -- プロセス管理・プロトコルの更新

## 背景 (Background)

### 問題の発見

本プロジェクトの Coding Agent サブシステム (`claudecode` パッケージ) は、Claude Code CLI をサブプロセスとして起動し、JSON Lines プロトコルでイベントストリームを処理する。しかし、以下の問題が判明した:

1. **CLI バージョン 2.0.14 が `ANTHROPIC_BASE_URL` を無視する**: Gateway 経由でのリクエストが全く到達しなかった。デバッグ HTTP サーバーを用いた検証で、CLI が環境変数を完全に無視して直接 `api.anthropic.com` に接続することを確認済み。

2. **CLI v2.1.169 へのアップデートで解決**: `claude update` により v2.1.169 に更新した結果、`ANTHROPIC_BASE_URL` が正しく機能し、Gateway 経由でリクエストがルーティングされることを確認。

3. **v2.1.x で必要な新しい CLI フラグ**: `--bare` フラグの追加が必須。OAuth/Keychain を無効化し `ANTHROPIC_API_KEY` のみで認証するモード。

4. **JSON Lines プロトコルの変化**: v2.1.x では出力フォーマットが変更されており、新しいイベントタイプ (`thinking_tokens`, `text` コンテンツブロック直接出力など) に対応する必要がある。

### 参考資料

- 調査レポート: vv4 リファレンスとの比較分析
- 設計方針: [agent-abstraction-research.md L55-73](file:///prompts/designs/vv4/agent-abstraction-research.md#L55-L73) -- Go言語から直接 CLI サブプロセスを管理する方式
- 非公式 Go SDK: [dotcommander/agent-sdk-go](https://github.com/dotcommander/agent-sdk-go) -- 同等のパターンで実装されている

---

## 要件 (Requirements)

### 必須要件

#### R1: `--bare` フラグの検討結果 (不採用)

- `--bare` フラグは OAuth/Keychain をスキップし `ANTHROPIC_API_KEY` のみで認証するモード
- **不採用の理由**: `--bare` は CLI が直接 Anthropic API に対して有効な API キーで認証することを前提とする。本プロジェクトの Gateway アーキテクチャでは、API キーは Gateway 側 (keyring/vault) で管理し、CLI にはダミーキー (`not-needed`) を渡す設計のため、`--bare` を使うと CLI 内部で認証エラー (`is_error: true`, exit code 1) が発生する
- v2.1 では `--bare` なしでも `ANTHROPIC_BASE_URL` 環境変数が正しく機能するため、`--bare` は不要

#### R2: v2.1 の JSON Lines プロトコル対応

v2.1.169 の出力フォーマットの変更に対応する:

- **`system/thinking_tokens` イベント**: 新しいイベントタイプ。`estimated_tokens` と `estimated_tokens_delta` フィールドを持つ。現状は無視 (nil 返却) で良い。
- **`assistant` メッセージ構造の変更**: v2.0 では `subtype: "text"` で `text` フィールドに直接テキストが入っていた。v2.1 では `message.content[]` 配列に `type: "text"` ブロックとして格納される。
- **`assistant/thinking` コンテンツブロック**: `content` 配列に `type: "thinking"` ブロックが含まれる。署名 (`signature`) フィールドを持つ。無視するか、ログ用にパススルーする。
- **`result` イベント構造の拡張**: `stop_reason`, `terminal_reason`, `modelUsage` など新しいフィールドが追加。`result` テキストの取得パスに変更なし。

#### R3: `text` イベントの抽出方法の修正

- v2.0: `stream_event` -> `event.delta.text` でテキストストリーミング
- v2.1: `assistant` -> `message.content[].type == "text"` -> `.text` でテキスト取得
- 両方式に対応するか、v2.1 形式のみに切り替える

#### R4: `--output-format stream-json` に `--verbose` が必須

- v2.1 では `--output-format stream-json` を使用する場合、`--verbose` フラグが必須
- 既存の `BuildArgs()` では `--verbose` は設定済み (変更不要)

#### R5: README.md に Claude Code CLI の対応バージョンを記載

- `README.md` の「前提条件」テーブル (L8-14) に Claude Code CLI の最低バージョン要件を追加する
- 記載内容: `Claude Code CLI` | `2.1.x 以上` | `claude update` でアップデート可能。v2.0.x は `ANTHROPIC_BASE_URL` 非対応
- ヘルスチェック出力例 (L315) のバージョン番号を `2.0.14` から `2.1.x` に更新する
- 「前提条件」セクション付近に v2.0.x からの移行注意事項を追記する

#### R6: `--max-turns` の設定

- 長いタスクでの早期ループ終了を防止するため、`--max-turns` オプションを追加する
- vv4 リファレンスでは `maxTurns: 200` を設定
- `SessionConfig` に `MaxTurns` フィールドを追加し、`BuildArgs()` で `--max-turns N` を生成する
- デフォルト値: 200 (vv4 と同じ)

#### R7: stdin 警告の抑制

- v2.1 で `-p` (print mode) 使用時に `Warning: no stdin data received in 3s` の警告が発生する
- `cmd.Stdin` に空の Reader をセットして警告を抑制する

#### R8: サーバ起動時の CLI バージョンチェック

- 既存の `detectCLIVersions()` ([service.go L190-212](file:///shared/libs/go/agentservice/service.go#L190-L212)) を拡張する
- `claude --version` の出力 (例: `2.1.169 (Claude Code)`) からメジャー・マイナーバージョンを抽出する
- 最低要件 (`>= 2.1.0`) を満たさない場合:
  - ログに **明確なエラーメッセージ** を出力する (例: `Claude Code CLI version 2.0.14 is not supported. Minimum required: 2.1.0. Run "claude update" to upgrade.`)
  - ヘルスチェック API (`/health`) のレスポンスにも警告情報を含める
- CLI が見つからない場合 (`unavailable`) は既存の動作を維持 (エージェント未登録)

---

## 実現方針 (Implementation Approach)

### 変更対象ファイル

#### 1. `shared/libs/go/codingagent/claudecode/process.go`

**`BuildArgs()` の修正**:

```go
func BuildArgs(cfg *codingagent.SessionConfig) []string {
    args := []string{
        "--bare",                          // R1: 新規追加
        "--output-format", "stream-json",
        "--verbose",                       // R4: 既存 (維持)
        "--permission-mode", "bypassPermissions",
    }
    // ... 既存のオプション追加ロジック
}
```

**`StartProcess()` の修正**:

```go
// R7: stdin 警告の抑制
cmd.Stdin = bytes.NewReader(nil)  // 空の Reader で即座に EOF を返す
```

#### 2. `shared/libs/go/codingagent/claudecode/protocol.go`

**`ParseJSONLinesEvent()` の修正**:

```go
case "system":
    if raw.Subtype == "init" {
        // 既存: init イベント処理
    }
    // R2: thinking_tokens は無視
    return nil

case "assistant":
    var msg messagePayload
    if err := json.Unmarshal(raw.Message, &msg); err != nil {
        return nil
    }
    for _, block := range msg.Content {
        switch block.Type {
        case "tool_use":
            // 既存のツール使用イベント
        case "text":
            // R3: v2.1 テキストブロック対応
            return &codingagent.StreamEvent{
                Type:    codingagent.EventText,
                Content: block.Text,  // contentBlock に Text フィールド追加
            }
        case "thinking":
            // R2: thinking ブロックは無視
        }
    }
```

**`contentBlock` 構造体の拡張**:

```go
type contentBlock struct {
    Type      string         `json:"type"`
    Text      string         `json:"text,omitempty"`     // R3: 新規追加
    Name      string         `json:"name,omitempty"`
    Input     map[string]any `json:"input,omitempty"`
    ToolUseID string         `json:"tool_use_id,omitempty"`
    Content   string         `json:"content,omitempty"`
}
```

#### 3. `shared/libs/go/codingagent/options.go`

```go
// R6: MaxTurns フィールド追加
type SessionConfig struct {
    // ... 既存フィールド
    MaxTurns int // Maximum agent turns (0 = CLI default, recommended: 200)
}

func WithMaxTurns(n int) SessionOption {
    return func(c *SessionConfig) { c.MaxTurns = n }
}
```

#### 5. `shared/libs/go/agentservice/service.go`

**`detectCLIVersions()` にバージョン検証を追加**:

```go
// R8: バージョンチェック
const minCLIVersion = "2.1.0"

func detectCLIVersions(agents map[string]codingagent.CodingAgent, logger logger.Logger) map[string]string {
    // ... 既存のバージョン検出ロジック
    // バージョン文字列 (例: "2.1.169 (Claude Code)") から数値部分を抽出
    // semver 比較で minCLIVersion 未満の場合:
    //   logger.Error("Claude Code CLI version X.Y.Z is not supported. Minimum required: 2.1.0. Run 'claude update' to upgrade.")
}
```

#### 4. `README.md`

**前提条件テーブルに行を追加**:

```markdown
| Claude Code CLI | 2.1.x 以上 | `claude update` でアップデート。v2.0.x は非対応 |
```

**ヘルスチェック出力例のバージョン更新** (L315):

```diff
-    "claudecode": "2.0.14 (Claude Code)"
+    "claudecode": "2.1.x (Claude Code)"
```

### 変更しないもの

- `BuildEnv()`: 環境変数の構築ロジックは変更不要。`ANTHROPIC_BASE_URL` と `ANTHROPIC_API_KEY` の設定方法は v2.1 でそのまま機能する。
- `adapter_config.go`: `AdapterConfig` 構造体は変更不要。
- Gateway側 (`proxy_anthropic.go`, `llmgateway`): 変更不要。`/v1/models` エンドポイントは既に実装済み。

---

## 検証シナリオ (Verification Scenarios)

### シナリオ 1: Gateway 経由での基本動作

1. `standalone` を起動 (`./bin/standalone -config ./examples/standalone/config.yaml`)
2. `codingagent` パッケージの `StartProcess()` で CLI を起動
3. Gateway のログに `anthropic request routed` が記録される
4. CLI が正常に応答を返し、`EventText` イベントが発火する
5. `EventResult` イベントでセッションが正常終了する

### シナリオ 2: `--bare` フラグによる OAuth スキップ

1. ユーザーの OAuth ログイン状態に関係なく、`ANTHROPIC_API_KEY` で認証される
2. ユーザーの `~/.claude/settings.json` がヘッドレス実行に干渉しない
3. `--model` フラグで指定したモデルが使用される

### シナリオ 3: v2.1 プロトコルの正しいパース

1. `system/init` イベントから `session_id` が取得できる
2. `system/thinking_tokens` イベントがエラーを起こさず無視される
3. `assistant` メッセージから `type: "text"` ブロックのテキストが `EventText` として取得できる
4. `assistant` メッセージの `type: "thinking"` ブロックがエラーを起こさず無視される
5. `result` イベントが `EventResult` として正しくパースされる

### シナリオ 4: CLI バージョンチェック

1. Claude Code CLI v2.1.x がインストールされた環境でサーバを起動する
2. ログにバージョン情報が出力される (エラーなし)
3. `/health` エンドポイントの `cli_versions` に正しいバージョンが表示される
4. (v2.0.x がインストールされた場合) エラーログに「Minimum required: 2.1.0. Run 'claude update' to upgrade.」が出力される

---

## テスト項目 (Testing for the Requirements)

### 単体テスト

#### `process_test.go`

| テスト | 対応要件 | 内容 |
|--------|---------|------|
| `TestBuildArgs_IncludesBareFlag` | R1 | `--bare` が引数リストに含まれる |
| `TestBuildArgs_VerboseRequired` | R4 | `--verbose` が引数リストに含まれる |
| `TestBuildArgs_WithMaxTurns` | R6 | `--max-turns N` が引数に含まれる |

#### `service_test.go`

| テスト | 対応要件 | 内容 |
|--------|---------|------|
| `TestParseCLIVersion_Valid` | R8 | `"2.1.169 (Claude Code)"` から `2.1.169` を抽出 |
| `TestParseCLIVersion_Old` | R8 | `"2.0.14 (Claude Code)"` がバージョン不足と判定される |
| `TestParseCLIVersion_Unavailable` | R8 | `"unavailable"` が正しく処理される |

#### `protocol_test.go`

| テスト | 対応要件 | 内容 |
|--------|---------|------|
| `TestParseJSONLinesEvent_V21_TextBlock` | R3 | v2.1形式の `assistant/text` ブロックが `EventText` にパースされる |
| `TestParseJSONLinesEvent_V21_ThinkingTokens` | R2 | `system/thinking_tokens` イベントが nil を返す (無視) |
| `TestParseJSONLinesEvent_V21_ThinkingBlock` | R2 | `assistant/thinking` ブロックがエラーなく処理される |
| `TestParseJSONLinesEvent_V21_Result` | R2 | v2.1 拡張フィールド付き `result` が正しくパースされる |
| `TestParseJSONLinesEvent_V20_StreamEvent` | R3 | v2.0 形式の `stream_event/text_delta` も引き続き動作する (後方互換) |

### ビルド・全体検証

1. ビルド + 単体テスト:
   ```
   scripts/process/build.sh
   ```

2. バックエンド統合テスト (LLM関連):
   ```
   scripts/process/integration_test.sh --categories "llm"
   ```

3. 共通機能リグレッション:
   ```
   scripts/process/integration_test.sh --categories "common"
   ```
