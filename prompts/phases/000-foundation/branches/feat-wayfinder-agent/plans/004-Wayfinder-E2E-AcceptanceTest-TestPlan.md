# 004-Wayfinder-E2E-AcceptanceTest-TestPlan

> **Source Specification**:
> - [000-Wayfinder-Agent-Overview.md](file://prompts/phases/000-foundation/branches/feat-wayfinder-agent/ideas/000-Wayfinder-Agent-Overview.md)
> - [001-Wayfinder-Session-Management-and-Serialization.md](file://prompts/phases/000-foundation/branches/feat-wayfinder-agent/ideas/001-Wayfinder-Session-Management-and-Serialization.md)
> - [003-Wayfinder-Guardrails-and-LLMGP-Integration.md](file://prompts/phases/000-foundation/branches/feat-wayfinder-agent/ideas/003-Wayfinder-Guardrails-and-LLMGP-Integration.md)

## Goal Description

Wayfinder Agent Part 1-4 の全実装が統合された段階で、**モックを一切使わない完全なE2E受け入れテスト**を実施するためのテスト計画。

ternサーバーを実際に起動し、ternctl CLI からWayfinder Agentを指定して、以下の一連のシナリオを実LLMバックエンドで検証する:

1. コード生成（ファイル作成）
2. セッション resume によるコード変更
3. セッション resume によるファイル削除
4. バックグラウンドプロセスの起動と、resume セッションでの kill

上記シナリオを **4つのモデルファミリー** で各実行し、モデル依存性がないことを確認する:
- Claude (Sonnet)
- GPT Codex
- Gemini 2.5 Flash
- Ollama (Qwen)

## User Review Required

> [!IMPORTANT]
> **Ollamaモデルの選択**: テスト用プロファイル `tests/testdata/model_profiles.yaml` にOllamaプロバイダが既に登録済みです（`gemma3:4b`）。ユーザー指定のQwen (`qwen3:8b`) を使用する場合は、同ファイルに `qwen3:8b` を追加し、ローカルの Ollama サーバー上で `ollama pull qwen3:8b` を実行する必要があります。あるいは既存の `gemma3:4b` で代替することも可能です。既存の `checkOllamaAvailable` ヘルパー（`tests/llm_ollama_test.go`）を再利用します。

> [!WARNING]
> **LLM応答の非決定性**: 実LLMを使用するため、応答の正確な文字列一致ではなく、「ファイルが存在するか」「内容にキーワードが含まれるか」「プロセスが消えたか」等の**観測可能な副作用**で合否判定を行います。

---

## 1. 要件一覧 (Extracted Requirements)

| ID | 要件 | 分類 |
| :--- | :--- | :--- |
| REQ-001 | ternサーバー起動し、wayfinderエージェントが登録・認識されること | 統合 |
| REQ-002 | ternctl からwayfinder を指定してコード生成（ファイル作成）ができること | 機能 |
| REQ-003 | セッションIDを `--resume` で引き継いでコード変更（ファイル内容の編集）ができること | 機能 |
| REQ-004 | resume セッションでファイル削除ができること | 機能 |
| REQ-005 | バックグラウンドで `sleep` プロセスを起動し、resume セッションでそのプロセスを `kill` できること | 機能 |
| REQ-006 | 上記 REQ-002~005 のシナリオが、シングルショットの連続実行として10秒以内（sleep 10の制約）で完了すること | 非機能 |
| REQ-007 | 上記シナリオが Claude Sonnet で動作すること | 統合 |
| REQ-008 | 上記シナリオが GPT Codex で動作すること | 統合 |
| REQ-009 | 上記シナリオが Gemini 2.5 Flash で動作すること | 統合 |
| REQ-010 | 上記シナリオが Ollama (Qwen) で動作すること | 統合 |
| REQ-011 | セッション状態がセッションファイルとして永続化されていること（ファイルの存在確認） | 機能 |
| REQ-012 | ガードレールが適切に動作していること（WorkDir外へのアクセス拒否） | 非機能 |

---

## 2. 要件別 実現根拠と検証設計

### REQ-001: Wayfinder Agent の登録と認識

#### 2.1 実現根拠 (Evidence of Fulfillment)
1. **[E-001-1]**: ternサーバーの `/health` エンドポイントが `"wayfinder"` をエージェント一覧に含むこと
2. **[E-001-2]**: ternctl の `agents` コマンドで `wayfinder` が表示されること

#### 2.2 確認手段 (Verification Methods)

| 根拠ID | 確認の視点 | 確認手段 |
| :--- | :--- | :--- |
| E-001-1 | API応答確認 | `/health` の JSON レスポンスの `agents` 配列に `"wayfinder"` が含まれること |
| E-001-2 | プロセス確認 | ternctl `agents` コマンドの標準出力に `wayfinder` の行が含まれること |

#### 2.3 確認手順 (Detailed Procedures)

##### E-001-1: Health API に wayfinder が含まれる
1. **前提条件**: ternサーバーが起動済み、wayfinder が CodingAgent として登録済み
2. **入力**: `GET /health`
3. **操作手順**: HTTP GET リクエストを送信
4. **期待結果**: `{"status": "ok", "agents": [..., "wayfinder", ...], ...}`
5. **判定基準**: `agents` 配列に文字列 `"wayfinder"` が存在する

##### E-001-2: ternctl agents で wayfinder が表示される
1. **前提条件**: ternctlバイナリがビルド済み、ternサーバーが起動済み
2. **入力**: `ternctl --server <URL> agents`
3. **操作手順**: サブプロセスとして実行し、stdout を取得
4. **期待結果**: 出力に `wayfinder` の行が含まれる
5. **判定基準**: `strings.Contains(output, "wayfinder")` が true

#### 2.4 テストシナリオ (Test Scenarios)

##### TC-001: Wayfinder Agent のヘルスチェック

*   **対応要件**: REQ-001
*   **対応根拠**: E-001-1, E-001-2
*   **テスト種別**: E2Eテスト
*   **配置先**: `tests/wayfinder_e2e_test.go`
*   **テスト関数名**: `TestE2E_Wayfinder_Health`
*   **テストシナリオ**:
    1. [Arrange] `startWayfinderE2EServer(t)` でternサーバーをwayfinder付きで起動
    2. [Act] `/health` に GET リクエスト
    3. [Assert] `agents` に `"wayfinder"` が含まれること、`status` が `"ok"` であること
*   **実装メモ**: `startE2EServer` を参考に、wayfinder adapter を登録するバージョンを作成

---

### REQ-002: コード生成（ファイル作成）

#### 2.1 実現根拠 (Evidence of Fulfillment)
1. **[E-002-1]**: ternctl で `--agent wayfinder` を指定して `write_file` を伴うプロンプトを送信し、指定ファイルがWorkDir内に作成されること
2. **[E-002-2]**: 作成されたファイルの内容が、プロンプトで指定したキーワードを含むこと
3. **[E-002-3]**: SSEストリーム内に `tool_use` イベントが含まれること（ツール呼び出しの証跡）
4. **[E-002-4]**: セッションが `completed` ステータスで終了し、`agent_session_id` が返却されること

#### 2.2 確認手段 (Verification Methods)

| 根拠ID | 確認の視点 | 確認手段 |
| :--- | :--- | :--- |
| E-002-1 | ファイル出力確認 | `os.Stat` でファイル存在を確認 |
| E-002-2 | データ確認 | `os.ReadFile` で内容を読み込み、`strings.Contains` でキーワードマッチ |
| E-002-3 | 通信確認 | SSEイベントに `type: tool_use` が含まれるか |
| E-002-4 | API応答確認 | `GET /api/v1/sessions/<id>` で `status: completed` と `agent_session_id` が非空 |

#### 2.3 確認手順 (Detailed Procedures)

##### E-002-1: ファイルが作成される
1. **前提条件**: ternサーバー起動済み、一時WorkDir作成済み
2. **入力**: プロンプト `"Create a file named greet.go in the current directory. The file should contain a Go function named Greet that returns the string 'Hello Wayfinder'. Do nothing else."`
3. **操作手順**: ternctl run --agent wayfinder --prompt <prompt> --work-dir <workDir> を実行
4. **期待結果**: `<workDir>/greet.go` が存在する
5. **判定基準**: `os.Stat(filepath.Join(workDir, "greet.go"))` が err == nil

##### E-002-2: ファイル内容にキーワードが含まれる
1. **前提条件**: E-002-1 完了後
2. **入力**: なし
3. **操作手順**: `os.ReadFile(filepath.Join(workDir, "greet.go"))`
4. **期待結果**: `"Hello Wayfinder"` と `"func Greet"` が含まれる
5. **判定基準**: `strings.Contains` で両方 true

#### 2.4 テストシナリオ (Test Scenarios)

##### TC-002: コード生成（ファイル作成）

*   **対応要件**: REQ-002
*   **対応根拠**: E-002-1, E-002-2, E-002-3, E-002-4
*   **テスト種別**: E2Eテスト
*   **配置先**: `tests/wayfinder_e2e_test.go`
*   **テスト関数名**: 各モデル別テスト関数内のステップ1（下記 TC-100系を参照）
*   **テストシナリオ**:
    1. [Arrange] ternサーバー起動、一時WorkDir作成
    2. [Act] ternctl run --agent wayfinder --model <model> --prompt <生成プロンプト> --work-dir <dir>
    3. [Assert] ファイル存在確認、内容確認、ternctl出力に `[Tool:` が含まれること、セッション status=completed

---

### REQ-003: セッション resume によるコード変更

#### 2.1 実現根拠 (Evidence of Fulfillment)
1. **[E-003-1]**: `--resume <session_id>` を使って送信した変更指示により、既存ファイルの内容が変更されること
2. **[E-003-2]**: 変更後のファイル内容にプロンプトで指示した新しいキーワードが含まれ、元のキーワードが置換または追加されていること

#### 2.2 確認手段 (Verification Methods)

| 根拠ID | 確認の視点 | 確認手段 |
| :--- | :--- | :--- |
| E-003-1 | ファイル出力確認 | ファイルの内容を読み込み、変更前と異なることを確認 |
| E-003-2 | データ確認 | 新しいキーワードが含まれていることを `strings.Contains` で検証 |

#### 2.3 確認手順 (Detailed Procedures)

##### E-003-1: resume でファイル内容が変更される
1. **前提条件**: TC-002 完了後、`greet.go` が存在し、`agent_session_id` が取得済み
2. **入力**: プロンプト `"Edit the file greet.go to change the Greet function so it returns 'Hello Wayfinder v2' instead. Do nothing else."`
3. **操作手順**: `ternctl run --resume <session_id> --prompt <変更プロンプト> --work-dir <workDir>` を実行
4. **期待結果**: `greet.go` の内容に `"Hello Wayfinder v2"` が含まれる
5. **判定基準**: `strings.Contains(content, "Hello Wayfinder v2")` が true

#### 2.4 テストシナリオ (Test Scenarios)

##### TC-003: セッション resume によるコード変更

*   **対応要件**: REQ-003
*   **対応根拠**: E-003-1, E-003-2
*   **テスト種別**: E2Eテスト
*   **配置先**: `tests/wayfinder_e2e_test.go`
*   **テスト関数名**: 各モデル別テスト関数内のステップ2
*   **テストシナリオ**:
    1. [Arrange] TC-002 で取得した session_id を使用
    2. [Act] ternctl run --resume <session_id> --prompt <変更プロンプト>
    3. [Assert] ファイル内容に `"Hello Wayfinder v2"` が含まれること

---

### REQ-004: セッション resume によるファイル削除

#### 2.1 実現根拠 (Evidence of Fulfillment)
1. **[E-004-1]**: resume セッションで削除指示を出すと、ファイルが実際に存在しなくなること
2. **[E-004-2]**: 削除後にディレクトリ一覧を確認し、該当ファイルが含まれないこと

#### 2.2 確認手段 (Verification Methods)

| 根拠ID | 確認の視点 | 確認手段 |
| :--- | :--- | :--- |
| E-004-1 | ファイル出力確認 | `os.Stat` でファイル不在を確認 (`os.IsNotExist`) |
| E-004-2 | データ確認 | `os.ReadDir` で親ディレクトリを走査し、ファイル名が含まれないこと |

#### 2.3 確認手順 (Detailed Procedures)

##### E-004-1: resume でファイルが削除される
1. **前提条件**: TC-003 完了後、`greet.go` が変更済み、同じ session_id を使用
2. **入力**: プロンプト `"Delete the file greet.go from the current directory. Do nothing else."`
3. **操作手順**: `ternctl run --resume <session_id> --prompt <削除プロンプト> --work-dir <workDir>` を実行
4. **期待結果**: `<workDir>/greet.go` が存在しない
5. **判定基準**: `os.Stat(filepath.Join(workDir, "greet.go"))` が `os.IsNotExist(err)` を返す

#### 2.4 テストシナリオ (Test Scenarios)

##### TC-004: セッション resume によるファイル削除

*   **対応要件**: REQ-004
*   **対応根拠**: E-004-1, E-004-2
*   **テスト種別**: E2Eテスト
*   **配置先**: `tests/wayfinder_e2e_test.go`
*   **テスト関数名**: 各モデル別テスト関数内のステップ3
*   **テストシナリオ**:
    1. [Arrange] TC-003 で使用した session_id を継続使用
    2. [Act] ternctl run --resume <session_id> --prompt <削除プロンプト>
    3. [Assert] `greet.go` が存在しないこと (`os.IsNotExist`)

---

### REQ-005: バックグラウンドプロセスの起動と kill

#### 2.1 実現根拠 (Evidence of Fulfillment)
1. **[E-005-1]**: resume セッションで `sleep 10` をバックグラウンド起動する指示を出し、プロセスが実際に存在すること
2. **[E-005-2]**: 同セッション(次のシングルショット)で `kill` 指示を出し、プロセスが消滅すること
3. **[E-005-3]**: `sleep 10` の10秒以内に全ステップが完了すること（タイムアウト検証）

#### 2.2 確認手段 (Verification Methods)

| 根拠ID | 確認の視点 | 確認手段 |
| :--- | :--- | :--- |
| E-005-1 | プロセス確認 | ternctl出力からPIDを抽出し、`os.FindProcess` + signal 0 でプロセス存在確認 |
| E-005-2 | プロセス確認 | kill後に同PIDが存在しないことを確認（`signal 0` が失敗する） |
| E-005-3 | データ確認 | テスト開始から完了までの経過時間が10秒以内であること |

#### 2.3 確認手順 (Detailed Procedures)

##### E-005-1: sleep プロセスのバックグラウンド起動
1. **前提条件**: TC-004 完了後、同じ session_id を使用
2. **入力**: プロンプト `"Run the command 'sleep 10' in the background. Report the PID of the background process. Do nothing else."`
3. **操作手順**: `ternctl run --resume <session_id> --prompt <起動プロンプト> --work-dir <workDir>` を実行
4. **期待結果**: 出力にPID番号が含まれる。該当PIDのプロセスが存在する。
5. **判定基準**: 正規表現でPIDを抽出し、`os.FindProcess(pid)` + signal 0 が成功

##### E-005-2: sleep プロセスの kill
1. **前提条件**: E-005-1 完了後、PIDが取得済み
2. **入力**: プロンプト `"Kill the background process with PID <PID>. Then verify it is no longer running. Do nothing else."`
3. **操作手順**: `ternctl run --resume <session_id> --prompt <killプロンプト> --work-dir <workDir>` を実行
4. **期待結果**: 該当PIDのプロセスが存在しない
5. **判定基準**: `syscall.Kill(pid, 0)` がエラーを返す (Windowsの場合は `tasklist /FI "PID eq <PID>"` で該当なし)

##### E-005-3: 10秒以内に完了
1. **前提条件**: 全ステップ開始前にタイムスタンプ記録
2. **入力**: なし
3. **操作手順**: テスト完了時にタイムスタンプ差を計算
4. **期待結果**: E-005-1 開始からE-005-2 の検証完了までが10秒以内
5. **判定基準**: `elapsed < 10 * time.Second`

#### 2.4 テストシナリオ (Test Scenarios)

##### TC-005: バックグラウンドプロセスの起動と kill

*   **対応要件**: REQ-005, REQ-006
*   **対応根拠**: E-005-1, E-005-2, E-005-3
*   **テスト種別**: E2Eテスト
*   **配置先**: `tests/wayfinder_e2e_test.go`
*   **テスト関数名**: 各モデル別テスト関数内のステップ4-5
*   **テストシナリオ**:
    1. [Arrange] TC-004 で使用した session_id を継続使用。`time.Now()` で開始時刻記録。
    2. [Act-1] ternctl run --resume <session_id> --prompt <sleep起動プロンプト>
    3. [Assert-1] 出力からPIDを正規表現で抽出。`os.FindProcess` + signal 0 でプロセス生存確認。
    4. [Act-2] ternctl run --resume <session_id> --prompt <killプロンプト (PID埋め込み)>
    5. [Assert-2] signal 0 がエラー（プロセス消滅確認）
    6. [Assert-3] `time.Since(start) < 10 * time.Second`

---

### REQ-011: セッション状態の永続化確認

#### 2.1 実現根拠 (Evidence of Fulfillment)
1. **[E-011-1]**: SessionDir配下にセッションIDに対応するJSONファイルが存在すること
2. **[E-011-2]**: JSONファイルの内容をパースして、`messages` 配列が空でないこと

#### 2.4 テストシナリオ (Test Scenarios)

##### TC-006: セッションファイルの永続化確認

*   **対応要件**: REQ-011
*   **対応根拠**: E-011-1, E-011-2
*   **テスト種別**: E2Eテスト
*   **配置先**: `tests/wayfinder_e2e_test.go`
*   **テスト関数名**: 各モデル別テスト関数内の追加検証
*   **テストシナリオ**:
    1. [Arrange] TC-002 完了後
    2. [Act] `SessionDir/<session_id>.json` の存在確認
    3. [Assert] ファイルが存在し、JSONとしてパース可能で、`messages` 配列が非空であること

---

### REQ-012: ガードレールの動作確認

#### 2.4 テストシナリオ (Test Scenarios)

##### TC-007: WorkDir外アクセスのブロック

*   **対応要件**: REQ-012
*   **対応根拠**: WorkDir外パスへの読み書きが拒否されること
*   **テスト種別**: E2Eテスト
*   **配置先**: `tests/wayfinder_e2e_test.go`
*   **テスト関数名**: `TestE2E_Wayfinder_GuardrailBlock`
*   **テストシナリオ**:
    1. [Arrange] ternサーバー起動、一時WorkDir作成
    2. [Act] プロンプト `"Read the file /etc/passwd and show its contents."` を送信
    3. [Assert] `/etc/passwd` の内容がSSEイベントに含まれ**ない**こと。エラーイベントまたはツール実行拒否のメッセージが含まれること。

---

## 3. テスト実装サマリー

### モデル別統合テスト設計

各モデルで TC-002 -> TC-003 -> TC-004 -> TC-005 を**1つのテスト関数内で連続実行**する。セッションID を引き継ぎながら5ステップのシナリオを走破する。

| モデルファミリー | モデル名 | テスト関数名 | 対応要件 |
| :--- | :--- | :--- | :--- |
| Claude Sonnet | `claude-sonnet-4-20250514` | `TestE2E_Wayfinder_FullScenario_Claude` | REQ-007 |
| GPT Codex | `gpt-5.3-codex` | `TestE2E_Wayfinder_FullScenario_GPTCodex` | REQ-008 |
| Gemini 2.5 Flash | `gemini-2.5-flash` | `TestE2E_Wayfinder_FullScenario_Gemini` | REQ-009 |
| Ollama Qwen | `qwen3:8b` (要 `tests/testdata/model_profiles.yaml` に追加、既存 `gemma3:4b` での代替も可) | `TestE2E_Wayfinder_FullScenario_Ollama` | REQ-010 |

### テストケース一覧

| TC-ID | テストケース名 | 対応要件 | テスト種別 | 配置先 |
| :--- | :--- | :--- | :--- | :--- |
| TC-001 | Wayfinder Agent ヘルスチェック | REQ-001 | E2E | `tests/wayfinder_e2e_test.go` |
| TC-002 | コード生成（ファイル作成） | REQ-002 | E2E | `tests/wayfinder_e2e_test.go` (各モデル関数内) |
| TC-003 | セッション resume によるコード変更 | REQ-003 | E2E | `tests/wayfinder_e2e_test.go` (各モデル関数内) |
| TC-004 | セッション resume によるファイル削除 | REQ-004 | E2E | `tests/wayfinder_e2e_test.go` (各モデル関数内) |
| TC-005 | バックグラウンドプロセス起動と kill | REQ-005, REQ-006 | E2E | `tests/wayfinder_e2e_test.go` (各モデル関数内) |
| TC-006 | セッションファイルの永続化確認 | REQ-011 | E2E | `tests/wayfinder_e2e_test.go` (各モデル関数内) |
| TC-007 | WorkDir外アクセスのブロック | REQ-012 | E2E | `tests/wayfinder_e2e_test.go` |
| TC-100 | 全シナリオ走破 (Claude Sonnet) | REQ-007 | E2E | `tests/wayfinder_e2e_test.go` |
| TC-101 | 全シナリオ走破 (GPT Codex) | REQ-008 | E2E | `tests/wayfinder_e2e_test.go` |
| TC-102 | 全シナリオ走破 (Gemini 2.5 Flash) | REQ-009 | E2E | `tests/wayfinder_e2e_test.go` |
| TC-103 | 全シナリオ走破 (Ollama Qwen) | REQ-010 | E2E | `tests/wayfinder_e2e_test.go` |

### 要件カバレッジマトリクス

| 要件 | 単体テスト | 統合テスト | E2Eテスト | カバー状態 |
| :--- | :--- | :--- | :--- | :--- |
| REQ-001 | - | - | TC-001 | 完全 |
| REQ-002 | - | - | TC-002 (TC-100~103内) | 完全 |
| REQ-003 | - | - | TC-003 (TC-100~103内) | 完全 |
| REQ-004 | - | - | TC-004 (TC-100~103内) | 完全 |
| REQ-005 | - | - | TC-005 (TC-100~103内) | 完全 |
| REQ-006 | - | - | TC-005 (TC-100~103内) | 完全 |
| REQ-007 | - | - | TC-100 | 完全 |
| REQ-008 | - | - | TC-101 | 完全 |
| REQ-009 | - | - | TC-102 | 完全 |
| REQ-010 | - | - | TC-103 | 条件付き (ローカルOllamaサーバー起動要、`tests/testdata/model_profiles.yaml` に qwen3:8b 追加要) |
| REQ-011 | - | - | TC-006 (TC-100~103内) | 完全 |
| REQ-012 | - | - | TC-007 | 完全 |

---

## 4. テスト実装の詳細設計

### ヘルパー関数

```go
// startWayfinderE2EServer は wayfinder adapter を登録してternサーバーを起動する。
// 既存の startE2EServer パターンに準拠する。
func startWayfinderE2EServer(t *testing.T) (baseURL string, cleanup func()) {
    // 1. freePort で GW/WS/AS ポートを取得
    // 2. 一時config.yaml を生成（agent_service.disable_sandbox: true）
    // 3. tern.New + tern.Launch
    // 4. wayfinder.New() で adapter を作成
    // 5. srv.AgentService().RegisterAgent(adapter)
    // 6. baseURL と cleanup を返す
}

// runTernctl はternctl サブプロセスを実行し、stdout と session_id を返す。
func runTernctl(t *testing.T, ternctlBin, serverURL string, args ...string) (output string, sessionID string) {
    // 1. exec.CommandContext でternctl を起動
    // 2. CombinedOutput で出力取得
    // 3. 出力から "Session created: <id>" または正規表現で session_id を抽出
    // 4. output と sessionID を返す
}

// runTernctlResume は --resume でternctl を実行する。
func runTernctlResume(t *testing.T, ternctlBin, serverURL, sessionID, prompt, workDir string) string {
    // ternctl --server <URL> run --resume <ID> --prompt <msg> --work-dir <dir>
}

// extractPIDFromOutput は出力テキストからPID番号を正規表現で抽出する。
func extractPIDFromOutput(output string) (int, error) {
    // 正規表現: PID\s*[:=]?\s*(\d+) または "PID (\d+)" パターン
}

// isProcessAlive はPIDのプロセスが生存しているか確認する。
func isProcessAlive(pid int) bool {
    // Unix: syscall.Kill(pid, 0)
    // Windows: tasklist /FI "PID eq <pid>" + 出力解析
}
```

### テスト関数の構造

```go
// runFullScenario は4モデルで共通のシナリオを実行する内部関数。
func runFullScenario(t *testing.T, modelName string) {
    t.Helper()

    baseURL, cleanup := startWayfinderE2EServer(t)
    defer cleanup()

    ternctlBin := resolveTernctlBin(t)
    workDir := t.TempDir()

    // ---- Step 1: コード生成 ----
    output1, sessionID := runTernctl(t, ternctlBin, baseURL,
        "run", "--agent", "wayfinder", "--model", modelName,
        "--prompt", "Create a file named greet.go ...",
        "--work-dir", workDir,
    )
    // Assert: greet.go 存在、内容に "Hello Wayfinder" 含む
    // Assert: output に "[Tool:" 含む
    // Assert: session status = completed
    // Assert: セッションファイル存在 (TC-006)

    // ---- Step 2: resume でコード変更 ----
    output2 := runTernctlResume(t, ternctlBin, baseURL, sessionID,
        "Edit the file greet.go to change ...",
        workDir,
    )
    // Assert: greet.go に "Hello Wayfinder v2" 含む

    // ---- Step 3: resume でファイル削除 ----
    output3 := runTernctlResume(t, ternctlBin, baseURL, sessionID,
        "Delete the file greet.go ...",
        workDir,
    )
    // Assert: greet.go が存在しない (os.IsNotExist)

    // ---- Step 4: sleep バックグラウンド起動 ----
    startTime := time.Now()
    output4 := runTernctlResume(t, ternctlBin, baseURL, sessionID,
        "Run the command 'sleep 10' in the background. Report the PID ...",
        workDir,
    )
    pid, err := extractPIDFromOutput(output4)
    // Assert: PIDが抽出できる、プロセスが生存している

    // ---- Step 5: sleep プロセスの kill ----
    output5 := runTernctlResume(t, ternctlBin, baseURL, sessionID,
        fmt.Sprintf("Kill the background process with PID %d. Verify it is no longer running.", pid),
        workDir,
    )
    // Assert: プロセスが消滅 (isProcessAlive == false)
    // Assert: 10秒以内 (time.Since(startTime) < 10 * time.Second)

    elapsed := time.Since(startTime)
    t.Logf("Process lifecycle (start + kill) completed in %v", elapsed)
    if elapsed >= 10*time.Second {
        t.Errorf("Process lifecycle took %v, expected < 10s", elapsed)
    }
}

// 各モデルのテスト関数
func TestE2E_Wayfinder_FullScenario_Claude(t *testing.T) {
    runFullScenario(t, "claude-sonnet-4-20250514")
}

func TestE2E_Wayfinder_FullScenario_GPTCodex(t *testing.T) {
    runFullScenario(t, "gpt-5.3-codex")
}

func TestE2E_Wayfinder_FullScenario_Gemini(t *testing.T) {
    runFullScenario(t, "gemini-2.5-flash")
}

func TestE2E_Wayfinder_FullScenario_Ollama(t *testing.T) {
    runFullScenario(t, "qwen3:8b")
}
```

### Windows / Linux 差異の取り扱い

| 項目 | Linux / macOS | Windows |
| :--- | :--- | :--- |
| プロセス存在確認 | `syscall.Kill(pid, 0)` | `tasklist /FI "PID eq <pid>"` 出力解析 |
| sleep コマンド | `sleep 10` | `timeout /t 10 /nobreak` (またはGit Bash sleep) |
| ternctl バイナリ | `../bin/ternctl` | `../bin/ternctl.exe` |
| プロセス kill | `kill <pid>` | `taskkill /PID <pid> /F` |

> ビルドタグ (`_unix.go`, `_windows.go`) でOS固有ヘルパーを分離する。

### テストのタイムアウト設計

| ステップ | 想定完了時間 | タイムアウト設定 | 根拠 |
| :--- | :--- | :--- | :--- |
| Step 1 (コード生成) | 5-30秒 (LLM応答依存) | 120秒 | ケースB: LLM応答は可変 |
| Step 2 (コード変更) | 5-30秒 | 120秒 | ケースB |
| Step 3 (ファイル削除) | 3-15秒 | 60秒 | ケースB: 比較的単純な操作 |
| Step 4 (sleep起動) | 3-15秒 | 60秒 | ケースB |
| Step 5 (kill) | 3-15秒 | 60秒 | ケースB |
| 全体 | 20-105秒 | 420秒 (7分) | 全5ステップ合計 |
| Process lifecycle (Step4+5) | 6-30秒 | < 10秒 (10秒判定) | REQ-006: sleep 10以内 |

> [!IMPORTANT]
> `Process lifecycle` の10秒判定は、`sleep 10` が10秒で自然終了することを利用した検証です。kill後にプロセスが消えていることを10秒以内に確認できれば、killが正しく機能したことの証拠になります。ただし、LLM応答のレイテンシが大きい場合を考慮し、Step 4の**ternctlコマンド開始**ではなく、**ternctl出力からPIDを取得した時点**を計測開始とします。

---

## 5. Step-by-Step Implementation Guide

> 本テスト計画は、Part 1-4 の全実装が完了した後に実行されるE2E受け入れテストです。

1.  **OS固有ヘルパーの実装**:
    - [x] Create `tests/wayfinder_helpers_unix_test.go` (build tag: `!windows`)
      - `isProcessAlive(pid)`: `syscall.Kill(pid, 0)` によるプロセス存在確認
    - [x] Create `tests/wayfinder_helpers_windows_test.go` (build tag: `windows`)
      - `isProcessAlive(pid)`: `tasklist /FI "PID eq <pid>"` の出力解析
    - [x] `git commit -m "test(wayfinder): add OS-specific process checking helpers"`

2.  **共通ヘルパーの実装**:
    - [x] Create `tests/wayfinder_e2e_test.go`
      - ファイルヘッダー: package宣言
      - `startWayfinderE2EServer(t)`: ternサーバー起動 + wayfinder adapter登録
      - `createWayfinderSession(t, ...)`: HTTP API経由でセッション作成
      - `sendWayfinderMessage(t, ...)`: SSEメッセージ送信
      - `extractPIDFromOutput(output)`: PID抽出
    - [x] `git commit -m "test(wayfinder): add E2E test helpers for wayfinder"`

3.  **TC-001: ヘルスチェック テスト実装**:
    - [x] `TestE2E_Wayfinder_Health` を `tests/wayfinder_e2e_test.go` に追加
    - [x] テスト実行: PASS

4.  **TC-007: ガードレール テスト実装**:
    - [x] `TestE2E_Wayfinder_GuardrailBlock` を追加

5.  **共通シナリオ関数 `runFullScenario` の実装**:
    - [x] `runFullScenario(t, modelName)` を実装
      - Step 1: コード生成 + ファイル/内容検証
      - Step 2: resume コード変更 + 内容検証
      - Step 3: resume ファイル削除 + 不在検証
      - Step 4: resume sleep起動 + PID抽出 + 生存確認
      - Step 5: resume kill + 消滅確認 + 10秒判定
      - セッションファイル永続化確認 (TC-006)

6.  **TC-100: Claude Sonnet テスト実装**:
    - [x] `TestE2E_Wayfinder_FullScenario_Claude` を追加

7.  **TC-101: GPT Codex テスト実装**:
    - [x] `TestE2E_Wayfinder_FullScenario_GPTCodex` を追加

8.  **TC-102: Gemini 2.5 Flash テスト実装**:
    - [x] `TestE2E_Wayfinder_FullScenario_Gemini` を追加

9.  **TC-103: Ollama Qwen テスト実装**:
    - [x] `checkOllamaAvailable(t)` ヘルパーを `tests/llm_ollama_test.go` から再利用
    - [x] `TestE2E_Wayfinder_FullScenario_Ollama` を追加

10. **ビルドとテスト実行**:
    - [x] ビルド実行 (build.sh PASS)
    - [x] TC-001 (ヘルスチェック) PASS
    - [/] 統合テスト実行（VaultにAPIキー登録待ち）
    - [ ] 全テスト結果の確認と記録

---

## 6. Verification Plan

### 前提条件

- Part 1-4 の全実装が完了し、`build.sh` が成功する状態であること
- 各LLMプロバイダの APIキーが `vault` に登録済みであること
- Ollama テストの場合: ローカルで `ollama run qwen3:8b` が実行可能であること

### Automated Verification

1.  **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```

2.  **E2E テスト (モデル別実行)**:

    Claude Sonnet:
    ```bash
    ./scripts/process/integration_test.sh --categories llm --specify "TestE2E_Wayfinder_FullScenario_Claude"
    ```

    GPT Codex:
    ```bash
    ./scripts/process/integration_test.sh --categories llm --specify "TestE2E_Wayfinder_FullScenario_GPTCodex"
    ```

    Gemini 2.5 Flash:
    ```bash
    ./scripts/process/integration_test.sh --categories llm --specify "TestE2E_Wayfinder_FullScenario_Gemini"
    ```

    Ollama Qwen:
    ```bash
    ./scripts/process/integration_test.sh --categories llm --specify "TestE2E_Wayfinder_FullScenario_Ollama"
    ```

3.  **基盤テスト (ヘルスチェック + ガードレール)**:
    ```bash
    ./scripts/process/integration_test.sh --categories llm --specify "TestE2E_Wayfinder_Health"
    ./scripts/process/integration_test.sh --categories llm --specify "TestE2E_Wayfinder_GuardrailBlock"
    ```

4.  **全E2Eテスト一括実行**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories llm --specify "TestE2E_Wayfinder"
    ```

### 合格基準

- 全4モデルで TC-001 ~ TC-007 が PASS
- 各モデルで5ステップシナリオ (生成 -> 変更 -> 削除 -> sleep起動 -> kill) が全てPASS
- Process lifecycle (Step 4+5) が全モデルで10秒以内に完了
- セッションファイルがSessionDir配下に存在
- ガードレールテストで `/etc/passwd` の内容が漏洩しない
