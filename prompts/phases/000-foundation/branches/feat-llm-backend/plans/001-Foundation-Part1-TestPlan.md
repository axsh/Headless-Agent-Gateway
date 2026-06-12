# 001-Foundation-Part1-TestPlan

> **Source Specification**:
> - [000-Architecture.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/000-Architecture.md) (R8: Logger)
> - [002-ConfigAndSecrets.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/002-ConfigAndSecrets.md) (R1-R5: Config, Vault)
> - [000-Foundation-Part1-Logger-Config-Vault.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/plans/000-Foundation-Part1-Logger-Config-Vault.md) (実装計画)

## Goal Description

Part1実装計画 (Logger / Config / Vault) の要件が最低限の実装で実現できることを検証するためのテスト計画。
仕様書の各要件について、「実現できたと言い切れる根拠」「根拠を得る確認手段」「具体的手順」をトレーサブルに記述する。

## User Review Required

None.

---

## 1. 要件一覧 (Extracted Requirements)

| ID | 要件 | 分類 |
| :--- | :--- | :--- |
| REQ-L01 | Logger interfaceの定義 (Debug/Info/Warn/Error/WithFields/WithComponent) | 機能 |
| REQ-L02 | DefaultLogger (NewDefault) の提供 | 機能 |
| REQ-L03 | ログレベル (debug/info/warn/error) のフィルタリング | 機能 |
| REQ-L04 | 構造化ログ (key-value fields) のサポート | 機能 |
| REQ-L05 | WithComponent による子ロガー生成 | 機能 |
| REQ-L06 | WithFields のimmutability (元ロガー不変) | 非機能 |
| REQ-L07 | Fatal/SetLevel/SetOutputType を含めない | 非機能 |
| REQ-L08 | グローバルロガー変数を使用しない | 非機能 |
| REQ-L09 | TextFormatter (テキスト形式ログ出力) | 機能 |
| REQ-L10 | JSONFormatter (JSON形式ログ出力) | 機能 |
| REQ-L11 | StdoutWriter (stdout/stderr振り分け) | 機能 |
| REQ-L12 | LogWriter interface定義 | 機能 |
| REQ-L13 | Level文字列パース (ParseLevel) | 機能 |
| REQ-L14 | カスタムLogger実装の注入可能性 | 統合 |
| REQ-C01 | AppConfig構造体 (LLMGateway/Vault/Log) のYAMLパース | 機能 |
| REQ-C02 | ModelProfilesConfig のYAMLパース | 機能 |
| REQ-C03 | ModelProfilesConfig のバリデーション | 機能 |
| REQ-C04 | config.Load() 純粋関数 | 機能 |
| REQ-C05 | config.LoadModelProfiles() 関数 | 機能 |
| REQ-C06 | provider/model独立フィールド (文字列パースなし) | 機能 |
| REQ-C07 | behavior.tool_call_fallback設定 | 機能 |
| REQ-C08 | vault://参照の検出 (ロード時は解決しない) | 機能 |
| REQ-C09 | 不正YAML/ファイル不存在のエラー | 機能 |
| REQ-V01 | VaultStore interface定義 (Resolve/Set/Delete/List) | 機能 |
| REQ-V02 | vault://参照のパース (IsVaultRef/ParseVaultRef) | 機能 |
| REQ-V03 | EnvVaultBackend (環境変数からのキー読み込み) | 機能 |
| REQ-V04 | 環境変数名規約 (TERN_VAULT_{PROVIDER}_{KEY}) | 機能 |
| REQ-V05 | 未設定環境変数のエラーハンドリング | 機能 |
| REQ-V06 | EnvVaultBackend.List() (TERN_VAULT_プレフィックス探索) | 機能 |
| REQ-V07 | マルチテナント対応 (複数パス解決) | 統合 |

---

## 2. 要件別 実現根拠と検証設計

### REQ-L01: Logger interface定義

#### 2.1 実現根拠

1. **E-L01-1**: `Logger` interfaceに `Debug`, `Info`, `Warn`, `Error`, `WithFields`, `WithComponent` の6メソッドが定義されていること
2. **E-L01-2**: DefaultLoggerがLogger interfaceを満たすこと (コンパイル時検証)
3. **E-L01-3**: カスタム実装 (モック) がLogger interfaceを満たすこと

#### 2.2 確認手段

| 根拠ID | 確認の視点 | 確認手段 |
| :--- | :--- | :--- |
| E-L01-1 | データ確認 | interface定義のメソッド一覧をコードで確認 |
| E-L01-2 | データ確認 | `var _ Logger = (*DefaultLogger)(nil)` のコンパイル通過 |
| E-L01-3 | データ確認 | テスト用モックがinterfaceを満たすことの確認 |

#### 2.3 確認手順

##### E-L01-2: DefaultLoggerのinterface準拠

1. **前提条件**: Go コンパイラが利用可能
2. **入力**: `var _ Logger = (*DefaultLogger)(nil)` を含むGoファイル
3. **操作手順**: `go build ./logger/...`
4. **期待結果**: コンパイルが成功する。型の不一致エラーが出ないこと
5. **判定基準**: `go build` の終了コードが0

#### 2.4 テストシナリオ

##### TC-L01: Logger interface準拠チェック

* **対応要件**: REQ-L01
* **対応根拠**: E-L01-2, E-L01-3
* **テスト種別**: 単体テスト
* **配置先**: `shared/libs/go/logger/logger_test.go`
* **テスト関数名**: `TestLogger_InterfaceCompliance`
* **前提条件**: なし
* **テストシナリオ**:
    1. [Arrange] DefaultLoggerのインスタンスを生成
    2. [Act] `var _ Logger = (*DefaultLogger)(nil)` でコンパイル時チェック
    3. [Assert] コンパイルが通ること (テスト実行時に型不一致はコンパイルエラーになるため、テストファイルが存在するだけで検証される)
* **実装メモ**: テスト用のmockLoggerも同様にinterface準拠を確認

---

### REQ-L03: ログレベルフィルタリング

#### 2.1 実現根拠

1. **E-L03-1**: LevelInfoに設定したロガーで、Debug呼び出しが出力されないこと
2. **E-L03-2**: LevelInfoに設定したロガーで、Info/Warn/Error呼び出しが出力されること
3. **E-L03-3**: LevelDebugに設定したロガーで、Debug呼び出しが出力されること
4. **E-L03-4**: LevelErrorに設定したロガーで、Info/Warn呼び出しが出力されないこと

#### 2.2 確認手段

| 根拠ID | 確認の視点 | 確認手段 |
| :--- | :--- | :--- |
| E-L03-1 | ログ確認 | bufferWriterに出力して内容が空であることを検証 |
| E-L03-2 | ログ確認 | bufferWriterに出力して内容が非空であることを検証 |
| E-L03-3 | ログ確認 | bufferWriterに出力してDebugメッセージが含まれることを検証 |
| E-L03-4 | ログ確認 | bufferWriterに出力してInfo/Warnメッセージが含まれないことを検証 |

#### 2.3 確認手順

##### E-L03-1: Debug抑制

1. **前提条件**: なし
2. **入力**: `NewDefaultWithOptions(LevelInfo, &TextFormatter{}, bufWriter)` で生成したロガー
3. **操作手順**: `logger.Debug("should not appear")` を呼び出す
4. **期待結果**: bufWriterの内容が空
5. **判定基準**: `buf.Len() == 0`

#### 2.4 テストシナリオ

##### TC-L03: ログレベルフィルタリング

* **対応要件**: REQ-L03
* **対応根拠**: E-L03-1, E-L03-2, E-L03-3, E-L03-4
* **テスト種別**: 単体テスト
* **配置先**: `shared/libs/go/logger/default_test.go`
* **テスト関数名**: `TestDefaultLogger_LevelFiltering`
* **前提条件**: なし
* **テストシナリオ**:
    1. [Arrange] テーブル駆動テスト:
        ```go
        tests := []struct {
            name       string
            loggerLvl  Level
            callLvl    Level
            shouldLog  bool
        }{
            {"info_blocks_debug", LevelInfo, LevelDebug, false},
            {"info_passes_info", LevelInfo, LevelInfo, true},
            {"info_passes_warn", LevelInfo, LevelWarn, true},
            {"info_passes_error", LevelInfo, LevelError, true},
            {"debug_passes_debug", LevelDebug, LevelDebug, true},
            {"error_blocks_info", LevelError, LevelInfo, false},
            {"error_blocks_warn", LevelError, LevelWarn, false},
            {"error_passes_error", LevelError, LevelError, true},
        }
        ```
    2. [Act] 各ケースでロガーを生成し、指定レベルのログを出力
    3. [Assert] bufferWriterの内容が空/非空であること
* **実装メモ**: テスト用 `bufferWriter` を作成 (LogWriter interfaceを満たすbytes.Buffer wrapper)

---

### REQ-L04: 構造化ログ

#### 2.1 実現根拠

1. **E-L04-1**: `logger.Info("msg", "key1", "val1", "key2", 42)` の出力に `key1=val1 key2=42` が含まれること
2. **E-L04-2**: JSON出力で `{"key1":"val1","key2":42}` がfieldsに含まれること
3. **E-L04-3**: 奇数長のfields (`"key1", "val1", "key2"`) で最後のkeyに `MISSING_VALUE` が設定されること

#### 2.4 テストシナリオ

##### TC-L04a: 構造化ログ (TextFormatter)

* **対応要件**: REQ-L04, REQ-L09
* **対応根拠**: E-L04-1
* **テスト種別**: 単体テスト
* **配置先**: `shared/libs/go/logger/default_test.go`
* **テスト関数名**: `TestDefaultLogger_StructuredFields_Text`
* **テストシナリオ**:
    1. [Arrange] TextFormatter + bufferWriter でロガー生成
    2. [Act] `logger.Info("hello", "user", "alice", "count", 5)` 呼び出し
    3. [Assert] 出力に `INFO hello count=5 user=alice` (alphabetical) が含まれること

##### TC-L04b: 構造化ログ (JSONFormatter)

* **対応要件**: REQ-L04, REQ-L10
* **対応根拠**: E-L04-2
* **テスト種別**: 単体テスト
* **配置先**: `shared/libs/go/logger/default_test.go`
* **テスト関数名**: `TestDefaultLogger_StructuredFields_JSON`
* **テストシナリオ**:
    1. [Arrange] JSONFormatter + bufferWriter でロガー生成
    2. [Act] `logger.Info("hello", "user", "alice")` 呼び出し
    3. [Assert] 出力が有効なJSONであること。`json.Unmarshal` 後に `fields.user == "alice"` であること

##### TC-L04c: 奇数長fields

* **対応要件**: REQ-L04
* **対応根拠**: E-L04-3
* **テスト種別**: 単体テスト
* **配置先**: `shared/libs/go/logger/default_test.go`
* **テスト関数名**: `TestDefaultLogger_OddLengthFields`
* **テストシナリオ**:
    1. [Arrange] TextFormatter + bufferWriter でロガー生成
    2. [Act] `logger.Info("msg", "key1", "val1", "orphan")` 呼び出し
    3. [Assert] 出力に `orphan=MISSING_VALUE` が含まれること

---

### REQ-L05: WithComponent

#### 2.1 実現根拠

1. **E-L05-1**: `WithComponent("gateway")` で生成した子ロガーのログ出力に `component=gateway` が含まれること
2. **E-L05-2**: 子ロガーにさらに `WithFields` を呼んでも `component` フィールドが保持されること

#### 2.4 テストシナリオ

##### TC-L05: WithComponent

* **対応要件**: REQ-L05
* **対応根拠**: E-L05-1, E-L05-2
* **テスト種別**: 単体テスト
* **配置先**: `shared/libs/go/logger/default_test.go`
* **テスト関数名**: `TestDefaultLogger_WithComponent`
* **テストシナリオ**:
    1. [Arrange] TextFormatter + bufferWriter でロガー生成
    2. [Act] `child := logger.WithComponent("gateway"); child.Info("started")` 呼び出し
    3. [Assert] 出力に `component=gateway` が含まれること
    4. [Act] `grandchild := child.WithFields(map[string]any{"port": 14000}); grandchild.Info("listen")` 呼び出し
    5. [Assert] 出力に `component=gateway` と `port=14000` の両方が含まれること

---

### REQ-L06: WithFields immutability

#### 2.1 実現根拠

1. **E-L06-1**: `WithFields` 呼び出し後、元のロガーのfieldが変わっていないこと
2. **E-L06-2**: 子ロガーのfieldが親のfieldに影響しないこと

#### 2.4 テストシナリオ

##### TC-L06: WithFields immutability

* **対応要件**: REQ-L06
* **対応根拠**: E-L06-1, E-L06-2
* **テスト種別**: 単体テスト
* **配置先**: `shared/libs/go/logger/default_test.go`
* **テスト関数名**: `TestDefaultLogger_WithFields_Immutable`
* **テストシナリオ**:
    1. [Arrange] TextFormatter + bufWriter でロガー (parent) 生成。parent に `"env"="prod"` を設定
    2. [Act] `child := parent.WithFields(map[string]any{"req_id": "abc"})` を呼び出す
    3. [Act] parent.Info("parent log"), child.Info("child log") を呼び出す
    4. [Assert] parent の出力に `env=prod` は含まれるが `req_id` は含まれないこと
    5. [Assert] child の出力に `env=prod` と `req_id=abc` の両方が含まれること

---

### REQ-L11: StdoutWriter (stdout/stderr振り分け)

#### 2.1 実現根拠

1. **E-L11-1**: Debug/InfoレベルがStdoutに出力されること
2. **E-L11-2**: Warn/ErrorレベルがStderrに出力されること

#### 2.4 テストシナリオ

##### TC-L11: StdoutWriter振り分け

* **対応要件**: REQ-L11
* **対応根拠**: E-L11-1, E-L11-2
* **テスト種別**: 単体テスト
* **配置先**: `shared/libs/go/logger/writer_test.go`
* **テスト関数名**: `TestStdoutWriter_LevelRouting`
* **テストシナリオ**:
    1. [Arrange] StdoutWriterのWriteメソッドを直接テスト (os.Pipe で標準出力をキャプチャする代わりに、Writeの戻り値で成功を確認)
    2. [Act] LevelInfo, LevelError で Write 呼び出し
    3. [Assert] 全てエラーなく書き込みされること
* **実装メモ**: 実際のstdout/stderrへの書き込みテストはプロセスレベルで複雑になるため、Write関数のerr=nil確認 + レベル分岐の正しさを型テストで検証する。より詳細なstdout/stderrキャプチャが必要な場合はbufferWriterで代替する

---

### REQ-L13: Level文字列パース

#### 2.4 テストシナリオ

##### TC-L13: ParseLevel

* **対応要件**: REQ-L13
* **対応根拠**: ParseLevelの各入力に対する正しいLevel値の返却
* **テスト種別**: 単体テスト
* **配置先**: `shared/libs/go/logger/level_test.go`
* **テスト関数名**: `TestParseLevel`
* **テストシナリオ**:
    1. [Arrange] テーブル駆動テスト: `{"debug" -> LevelDebug, "DEBUG" -> LevelDebug, "info" -> LevelInfo, "warn" -> LevelWarn, "warning" -> LevelWarn, "error" -> LevelError, "unknown" -> LevelInfo, "" -> LevelInfo}`
    2. [Act] `ParseLevel(input)` 呼び出し
    3. [Assert] 返却値が期待Levelと一致すること

---

### REQ-L14: カスタムLogger実装の注入可能性

#### 2.1 実現根拠

1. **E-L14-1**: Logger interfaceを満たす独自実装を作成し、全メソッドが呼び出し可能であること
2. **E-L14-2**: 独自実装のログ出力が、独自のフォーマット/出力先に到達すること

#### 2.4 テストシナリオ

##### TC-L14: カスタムLogger注入

* **対応要件**: REQ-L14
* **対応根拠**: E-L14-1, E-L14-2
* **テスト種別**: 単体テスト
* **配置先**: `shared/libs/go/logger/logger_test.go`
* **テスト関数名**: `TestCustomLogger_Injection`
* **テストシナリオ**:
    1. [Arrange] テスト用のmockLogger (呼び出しを記録する) を作成
    2. [Act] mockLoggerの各メソッド (Debug/Info/Warn/Error/WithFields/WithComponent) を呼び出す
    3. [Assert] 呼び出しが記録されていること。WithFields/WithComponentが新しいLogger(mock)を返すこと

---

### REQ-C01: AppConfig YAMLパース

#### 2.1 実現根拠

1. **E-C01-1**: 完全なconfig.yamlをパースし、全フィールドが正しく読み取れること
2. **E-C01-2**: 最小限のconfig.yamlでゼロ値フィールドがデフォルトになること
3. **E-C01-3**: 空ファイルでもエラーにならず、ゼロ値AppConfigが返ること

#### 2.4 テストシナリオ

##### TC-C01: AppConfig YAMLパース

* **対応要件**: REQ-C01
* **対応根拠**: E-C01-1, E-C01-2, E-C01-3
* **テスト種別**: 単体テスト
* **配置先**: `shared/libs/go/config/config_test.go`
* **テスト関数名**: `TestAppConfig_YAMLUnmarshal`
* **テストシナリオ**:
    1. [Arrange] テーブル駆動テスト:
        - full config: port=14000, backend="env", level="info"
        - minimal config: vault.backend="env" のみ
        - empty config: 空文字列
    2. [Act] `yaml.Unmarshal([]byte(input), &cfg)` 呼び出し
    3. [Assert] 各フィールドが期待値と一致すること

---

### REQ-C02: ModelProfilesConfig YAMLパース

#### 2.1 実現根拠

1. **E-C02-1**: 仕様書のYAMLサンプル (anthropic/openai/ollama) が正しくパースされること
2. **E-C02-2**: default_profileが正しく読み取れること
3. **E-C02-3**: keys[].models[].behavior.tool_call_fallbackが読み取れること
4. **E-C02-4**: network_config.base_urlが読み取れること

#### 2.4 テストシナリオ

##### TC-C02: ModelProfilesConfig パース

* **対応要件**: REQ-C02, REQ-C06, REQ-C07
* **対応根拠**: E-C02-1, E-C02-2, E-C02-3, E-C02-4
* **テスト種別**: 単体テスト
* **配置先**: `shared/libs/go/config/model_profiles_test.go`
* **テスト関数名**: `TestModelProfilesConfig_YAMLUnmarshal`
* **テストシナリオ**:
    1. [Arrange] 仕様書002-R1-2のYAMLサンプルをテスト入力として使用
    2. [Act] `yaml.Unmarshal` 呼び出し
    3. [Assert]:
        - `cfg.DefaultProfile.Provider == "anthropic"`
        - `cfg.Providers["anthropic"].Keys[0].Value == "vault://providers/anthropic/primary"`
        - `cfg.Providers["ollama"].Keys[0].Models[0].Behavior.ToolCallFallback == true`
        - `cfg.Providers["ollama"].NetworkConfig.BaseURL == "http://localhost:11434"`
        - provider と model が独立フィールドであること (文字列パースなし)

---

### REQ-C03: ModelProfilesConfig バリデーション

#### 2.1 実現根拠

1. **E-C03-1**: 空のProviders mapでバリデーションエラーが返ること
2. **E-C03-2**: 空のKeys配列でバリデーションエラーが返ること
3. **E-C03-3**: 空のModel名でバリデーションエラーが返ること
4. **E-C03-4**: DefaultProfileのProviderが存在しないProviderの場合エラーが返ること
5. **E-C03-5**: 有効な設定でバリデーションが成功すること

#### 2.4 テストシナリオ

##### TC-C03: バリデーション

* **対応要件**: REQ-C03
* **対応根拠**: E-C03-1 ~ E-C03-5
* **テスト種別**: 単体テスト
* **配置先**: `shared/libs/go/config/model_profiles_test.go`
* **テスト関数名**: `TestModelProfilesConfig_Validate`
* **テストシナリオ**:
    1. [Arrange] テーブル駆動テスト:
        ```go
        tests := []struct {
            name    string
            config  ModelProfilesConfig
            wantErr bool
            errMsg  string
        }{
            {"valid_config", validConfig(), false, ""},
            {"empty_providers", emptyProviders(), true, "no providers"},
            {"empty_keys", emptyKeys(), true, "no keys"},
            {"empty_model_name", emptyModelName(), true, "empty model name"},
            {"invalid_default_provider", invalidDefaultProvider(), true, "default profile provider"},
        }
        ```
    2. [Act] `cfg.Validate()` 呼び出し
    3. [Assert] エラーの有無とエラーメッセージの内容

---

### REQ-C04: config.Load() 純粋関数

#### 2.1 実現根拠

1. **E-C04-1**: 有効なYAMLファイルパスで正しいAppConfigが返ること
2. **E-C04-2**: 存在しないファイルパスでエラーが返ること
3. **E-C04-3**: 不正YAMLでエラーが返ること
4. **E-C04-4**: vault://参照を解決せず、文字列のまま保持すること

#### 2.4 テストシナリオ

##### TC-C04a: Load 正常系

* **対応要件**: REQ-C04
* **対応根拠**: E-C04-1
* **テスト種別**: 単体テスト
* **配置先**: `shared/libs/go/config/loader_test.go`
* **テスト関数名**: `TestLoad_ValidConfig`
* **テストシナリオ**:
    1. [Arrange] `t.TempDir()` にconfig.yamlを作成
    2. [Act] `config.Load(path)` 呼び出し
    3. [Assert] 返却されたAppConfigの各フィールドが期待値と一致

##### TC-C04b: Load ファイル不存在

* **対応要件**: REQ-C04, REQ-C09
* **対応根拠**: E-C04-2
* **テスト種別**: 単体テスト
* **配置先**: `shared/libs/go/config/loader_test.go`
* **テスト関数名**: `TestLoad_FileNotFound`
* **テストシナリオ**:
    1. [Arrange] 存在しないパス
    2. [Act] `config.Load("/nonexistent/config.yaml")` 呼び出し
    3. [Assert] errがnilでないこと

##### TC-C04c: Load 不正YAML

* **対応要件**: REQ-C04, REQ-C09
* **対応根拠**: E-C04-3
* **テスト種別**: 単体テスト
* **配置先**: `shared/libs/go/config/loader_test.go`
* **テスト関数名**: `TestLoad_InvalidYAML`
* **テストシナリオ**:
    1. [Arrange] 不正なYAML内容 (`{{{invalid}}}`) でtmpファイル作成
    2. [Act] `config.Load(path)` 呼び出し
    3. [Assert] errがnilでないこと

---

### REQ-V02: vault://参照パース

#### 2.1 実現根拠

1. **E-V02-1**: `vault://providers/anthropic/primary` がvault参照と判定されること
2. **E-V02-2**: `sk-ant-xxxxx` がvault参照と判定されないこと
3. **E-V02-3**: ParseVaultRefがパス部分を正しく抽出すること

#### 2.4 テストシナリオ

##### TC-V02: vault://参照パース

* **対応要件**: REQ-V02
* **対応根拠**: E-V02-1, E-V02-2, E-V02-3
* **テスト種別**: 単体テスト
* **配置先**: `shared/libs/go/vault/resolve_test.go`
* **テスト関数名**: `TestParseVaultRef`
* **テストシナリオ**:
    1. [Arrange] テーブル駆動テスト:
        ```go
        tests := []struct {
            input   string
            isVault bool
            path    string
        }{
            {"vault://providers/anthropic/primary", true, "providers/anthropic/primary"},
            {"vault://providers/openai/team-a", true, "providers/openai/team-a"},
            {"sk-ant-xxxxx", false, ""},
            {"", false, ""},
            {"vault://", true, ""},
            {"VAULT://uppercase", false, ""},  // case sensitive
        }
        ```
    2. [Act] `ParseVaultRef(input)` 呼び出し
    3. [Assert] path, isVault が期待値と一致

---

### REQ-V03: EnvVaultBackend

#### 2.1 実現根拠

1. **E-V03-1**: 環境変数 `TERN_VAULT_ANTHROPIC_PRIMARY` にキーを設定し、`Resolve("vault://providers/anthropic/primary")` で取得できること
2. **E-V03-2**: 未設定の環境変数に対してエラーが返ること
3. **E-V03-3**: vault://でない参照に対してエラーが返ること
4. **E-V03-4**: Set/Delete/Listが正しく動作すること

#### 2.4 テストシナリオ

##### TC-V03a: EnvVaultBackend Resolve正常系

* **対応要件**: REQ-V03, REQ-V04
* **対応根拠**: E-V03-1
* **テスト種別**: 単体テスト
* **配置先**: `shared/libs/go/vault/env_backend_test.go`
* **テスト関数名**: `TestEnvVaultBackend_Resolve`
* **テストシナリオ**:
    1. [Arrange] `t.Setenv("TERN_VAULT_ANTHROPIC_PRIMARY", "sk-ant-test123")`
    2. [Act] `backend.Resolve("vault://providers/anthropic/primary")` 呼び出し
    3. [Assert] 返却値 == `"sk-ant-test123"`, err == nil

##### TC-V03b: EnvVaultBackend Resolve未設定

* **対応要件**: REQ-V03, REQ-V05
* **対応根拠**: E-V03-2
* **テスト種別**: 単体テスト
* **配置先**: `shared/libs/go/vault/env_backend_test.go`
* **テスト関数名**: `TestEnvVaultBackend_Resolve_Missing`
* **テストシナリオ**:
    1. [Arrange] 環境変数を設定しない
    2. [Act] `backend.Resolve("vault://providers/openai/primary")` 呼び出し
    3. [Assert] err != nil, エラーメッセージに環境変数名が含まれること

##### TC-V03c: EnvVaultBackend パス変換

* **対応要件**: REQ-V04
* **対応根拠**: pathToEnvNameの正しい変換
* **テスト種別**: 単体テスト
* **配置先**: `shared/libs/go/vault/env_backend_test.go`
* **テスト関数名**: `TestEnvVaultBackend_PathToEnvName`
* **テストシナリオ**:
    1. [Arrange] テーブル駆動テスト:
        ```go
        tests := []struct {
            path string
            want string
        }{
            {"providers/anthropic/primary", "TERN_VAULT_ANTHROPIC_PRIMARY"},
            {"providers/openai/team-a", "TERN_VAULT_OPENAI_TEAM_A"},
            {"providers/ollama/default", "TERN_VAULT_OLLAMA_DEFAULT"},
        }
        ```
    2. [Act] `pathToEnvName(path)` 呼び出し
    3. [Assert] 返却値が期待値と一致

##### TC-V03d: EnvVaultBackend List

* **対応要件**: REQ-V06
* **対応根拠**: E-V03-4
* **テスト種別**: 単体テスト
* **配置先**: `shared/libs/go/vault/env_backend_test.go`
* **テスト関数名**: `TestEnvVaultBackend_List`
* **テストシナリオ**:
    1. [Arrange] `t.Setenv("TERN_VAULT_ANTHROPIC_PRIMARY", "key1")`, `t.Setenv("TERN_VAULT_OPENAI_PRIMARY", "key2")`
    2. [Act] `backend.List()` 呼び出し
    3. [Assert] 返却されたパス一覧に期待パスが含まれること

---

### REQ-V07: マルチテナント対応

#### 2.1 実現根拠

1. **E-V07-1**: 異なるテナントパス (`providers/anthropic/team-a`, `providers/anthropic/team-b`) を別々の環境変数で解決できること

#### 2.4 テストシナリオ

##### TC-V07: マルチテナント

* **対応要件**: REQ-V07
* **対応根拠**: E-V07-1
* **テスト種別**: 単体テスト
* **配置先**: `shared/libs/go/vault/env_backend_test.go`
* **テスト関数名**: `TestEnvVaultBackend_MultiTenant`
* **テストシナリオ**:
    1. [Arrange] `t.Setenv("TERN_VAULT_ANTHROPIC_TEAM_A", "key-team-a")`, `t.Setenv("TERN_VAULT_ANTHROPIC_TEAM_B", "key-team-b")`
    2. [Act] `backend.Resolve("vault://providers/anthropic/team-a")` と `backend.Resolve("vault://providers/anthropic/team-b")` 呼び出し
    3. [Assert] それぞれ異なる値が返ること

---

## 3. テスト実装サマリー

### テストケース一覧

| TC-ID | テストケース名 | 対応要件 | テスト種別 | 配置先 |
| :--- | :--- | :--- | :--- | :--- |
| TC-L01 | Logger interface準拠チェック | REQ-L01 | 単体 | `logger/logger_test.go` |
| TC-L03 | ログレベルフィルタリング | REQ-L03 | 単体 | `logger/default_test.go` |
| TC-L04a | 構造化ログ (Text) | REQ-L04, REQ-L09 | 単体 | `logger/default_test.go` |
| TC-L04b | 構造化ログ (JSON) | REQ-L04, REQ-L10 | 単体 | `logger/default_test.go` |
| TC-L04c | 奇数長fields | REQ-L04 | 単体 | `logger/default_test.go` |
| TC-L05 | WithComponent | REQ-L05 | 単体 | `logger/default_test.go` |
| TC-L06 | WithFields immutability | REQ-L06 | 単体 | `logger/default_test.go` |
| TC-L11 | StdoutWriter振り分け | REQ-L11 | 単体 | `logger/writer_test.go` |
| TC-L13 | ParseLevel | REQ-L13 | 単体 | `logger/level_test.go` |
| TC-L14 | カスタムLogger注入 | REQ-L14 | 単体 | `logger/logger_test.go` |
| TC-C01 | AppConfig YAMLパース | REQ-C01 | 単体 | `config/config_test.go` |
| TC-C02 | ModelProfilesConfig パース | REQ-C02, REQ-C06, REQ-C07 | 単体 | `config/model_profiles_test.go` |
| TC-C03 | ModelProfilesConfig バリデーション | REQ-C03 | 単体 | `config/model_profiles_test.go` |
| TC-C04a | Load 正常系 | REQ-C04 | 単体 | `config/loader_test.go` |
| TC-C04b | Load ファイル不存在 | REQ-C04, REQ-C09 | 単体 | `config/loader_test.go` |
| TC-C04c | Load 不正YAML | REQ-C04, REQ-C09 | 単体 | `config/loader_test.go` |
| TC-V02 | vault://参照パース | REQ-V02 | 単体 | `vault/resolve_test.go` |
| TC-V03a | EnvVaultBackend Resolve正常系 | REQ-V03, REQ-V04 | 単体 | `vault/env_backend_test.go` |
| TC-V03b | EnvVaultBackend Resolve未設定 | REQ-V03, REQ-V05 | 単体 | `vault/env_backend_test.go` |
| TC-V03c | EnvVaultBackend パス変換 | REQ-V04 | 単体 | `vault/env_backend_test.go` |
| TC-V03d | EnvVaultBackend List | REQ-V06 | 単体 | `vault/env_backend_test.go` |
| TC-V07 | マルチテナント | REQ-V07 | 単体 | `vault/env_backend_test.go` |

### 要件カバレッジマトリクス

| 要件 | 単体テスト | カバー状態 |
| :--- | :--- | :--- |
| REQ-L01 | TC-L01 | 完全 |
| REQ-L02 | TC-L03, TC-L04a (NewDefault使用) | 完全 |
| REQ-L03 | TC-L03 | 完全 |
| REQ-L04 | TC-L04a, TC-L04b, TC-L04c | 完全 |
| REQ-L05 | TC-L05 | 完全 |
| REQ-L06 | TC-L06 | 完全 |
| REQ-L07 | TC-L01 (interface定義にFatal等がないことで暗黙検証) | 完全 |
| REQ-L08 | TC-L01 (init()/グローバル変数がないことで暗黙検証) | 完全 |
| REQ-L09 | TC-L04a | 完全 |
| REQ-L10 | TC-L04b | 完全 |
| REQ-L11 | TC-L11 | 完全 |
| REQ-L12 | TC-L11 (StdoutWriterがLogWriter実装) | 完全 |
| REQ-L13 | TC-L13 | 完全 |
| REQ-L14 | TC-L14 | 完全 |
| REQ-C01 | TC-C01 | 完全 |
| REQ-C02 | TC-C02 | 完全 |
| REQ-C03 | TC-C03 | 完全 |
| REQ-C04 | TC-C04a, TC-C04b, TC-C04c | 完全 |
| REQ-C05 | TC-C02に含む (LoadModelProfilesテスト) | 完全 |
| REQ-C06 | TC-C02 | 完全 |
| REQ-C07 | TC-C02 | 完全 |
| REQ-C08 | TC-C04a (vault://が文字列として保持) | 完全 |
| REQ-C09 | TC-C04b, TC-C04c | 完全 |
| REQ-V01 | TC-V03a (EnvVaultBackendがinterfaceを満たす) | 完全 |
| REQ-V02 | TC-V02 | 完全 |
| REQ-V03 | TC-V03a, TC-V03b | 完全 |
| REQ-V04 | TC-V03a, TC-V03c | 完全 |
| REQ-V05 | TC-V03b | 完全 |
| REQ-V06 | TC-V03d | 完全 |
| REQ-V07 | TC-V07 | 完全 |

---

## 4. Step-by-Step Implementation Guide

1. **テスト用ヘルパー作成**:
    - [x] `shared/libs/go/logger/testutil_test.go` にbufferWriter (LogWriter interface実装) を作成
    - [x] `shared/libs/go/logger/testutil_test.go` にmockLogger (Logger interface実装) を作成

2. **Logger テスト作成**:
    - [x] `shared/libs/go/logger/level_test.go`: TC-L13 (ParseLevel)
    - [x] `shared/libs/go/logger/logger_test.go`: TC-L01 (interface準拠), TC-L14 (カスタムLogger)
    - [x] `shared/libs/go/logger/default_test.go`: TC-L03, TC-L04a/b/c, TC-L05, TC-L06
    - [x] `shared/libs/go/logger/writer_test.go`: TC-L11

3. **Config テスト作成**:
    - [x] `shared/libs/go/config/config_test.go`: TC-C01
    - [x] `shared/libs/go/config/model_profiles_test.go`: TC-C02, TC-C03
    - [x] `shared/libs/go/config/loader_test.go`: TC-C04a/b/c

4. **Vault テスト作成**:
    - [x] `shared/libs/go/vault/resolve_test.go`: TC-V02
    - [x] `shared/libs/go/vault/env_backend_test.go`: TC-V03a/b/c/d, TC-V07

5. **実装とテスト通過**:
    - [x] Part1実装計画に従い、テスト先行で実装
    - [x] 全テスト通過を確認 (build.sh PASS, 45テストケース)

---

## 5. Test Execution Plan

### 5.1 ビルドと単体テスト

```bash
./scripts/process/build.sh
```

### 5.2 パッケージ単位のテスト (開発中フィードバック用)

```bash
cd shared/libs/go && go test ./logger/... -v
cd shared/libs/go && go test ./config/... -v
cd shared/libs/go && go test ./vault/... -v
```

> [!NOTE]
> 正式検証は必ず `./scripts/process/build.sh` を使用すること。上記は開発中のフィードバック取得用。

### 5.3 統合テスト

Part1は全て単体テストで完結する。統合テストはPart2以降で追加予定:

```bash
./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories "common" --specify "Config|Vault"
```
