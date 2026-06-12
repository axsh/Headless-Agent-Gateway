<p align="center">
  <img src="docs/resources/images/hag_witch_circle_transparent_2.png" alt="HAG Logo" width="200">
</p>

<h1 align="center">HAG (Headless Agent Gateway)</h1>

<p align="center">
LLM プロバイダ (OpenAI, Anthropic, Google 等) への API リクエストを透過的にプロキシし、<br>
コーディングエージェントの WebSocket ログストリーミングやシークレット管理を統合的に提供するゲートウェイサーバです。
</p>

## 前提条件

| 項目 | バージョン | 備考 |
|------|-----------|------|
| Go | 1.24.0 以上 | `go.mod` では `go 1.26.3` を指定 |
| Git | 2.x 以上 | サブモジュール取得に使用 |
| Docker / Docker Compose | Docker 20.10+ / Compose v2+ | Docker デモ実行時のみ必要 |
| Claude Code CLI | 2.1.x 以上 | `claude update` でアップデート。v2.0.x は非対応 |
| OS | Windows / macOS / Linux | クロスプラットフォーム対応 |

> **Note**: Windows 環境では Git Bash の利用を推奨します。ビルドスクリプトは bash で記述されています。

> **Important**: Claude Code CLI v2.0.x は `ANTHROPIC_BASE_URL` 環境変数を無視するため、
> Gateway 経由のリクエストが機能しません。必ず v2.1.x 以上にアップデートしてください。
> アップデート方法: `claude update` を実行してください。

## プロジェクト構成

```
.
├── features/           # 機能モジュール (個別の Go モジュール)
│   └── hag/            # HAG 本体のエントリポイント (将来拡張用)
├── shared/
│   └── libs/
│       └── go/         # 共有ライブラリ群 (Go モジュール: github.com/axsh/hag)
│           ├── hag/           # HAG サーバコア (New, Launch, Shutdown)
│           ├── llmgateway/    # LLM Gateway プロキシ (OpenAI / Anthropic)
│           ├── wsserver/      # WebSocket サーバ (ログストリーミング)
│           ├── tasklog/       # エージェントログ管理
│           ├── vault/         # シークレット管理 (Keyring / Env / File バックエンド)
│           ├── config/        # 設定ファイルローダー
│           ├── agentservice/  # エージェントサービス
│           ├── codingagent/   # コーディングエージェント
│           └── logger/        # ロガー
├── examples/           # 動作デモ・サンプル
│   ├── standalone/     # HAG サーバ単体起動デモ (Docker 対応)
│   ├── cawa-client/    # Coding Agent Web API クライアント CLI
│   ├── log-viewer/     # WebSocket ログビューア / シミュレータ
│   └── vault-cli/      # シークレット管理 CLI ツール
├── container/          # Docker コンテナ構成
│   ├── all-in-one/     # Gateway + Agent 統合コンテナ
│   └── hybrid/         # Gateway / Agent 分離コンテナ
├── tests/              # 統合テスト
├── scripts/
│   └── process/
│       ├── build.sh              # フルビルド & 単体テスト
│       └── integration_test.sh   # 統合テスト
├── bin/                # ビルド成果物 (.gitignore 対象)
└── reference_repo/     # 参照リポジトリ (Git サブモジュール)
```

## ビルド方法

### リポジトリのクローンとサブモジュール取得

```bash
git clone git@github.com:axsh/Headless-Agent-Gateway.git
cd Headless-Agent-Gateway
git submodule update --init --recursive
```

### フルビルド (ビルドスクリプト)

プロジェクト全体のビルドと単体テストをまとめて実行します。
ビルド成果物は `bin/` ディレクトリに出力されます。

```bash
./scripts/process/build.sh
```

このスクリプトは以下を順番に実行します:

1. `features/*/` 配下の各 Go モジュールをビルド & テスト
2. `shared/libs/go/` の共有ライブラリをビルド & テスト
3. `examples/*/` 配下の各サンプルをビルド & テスト

### 個別ビルド

特定のコンポーネントだけをビルドする場合:

```bash
# 共有ライブラリ
cd shared/libs/go
go build ./...
go test ./...

# standalone サンプル
cd examples/standalone
go build -o ../../bin/standalone .

# log-viewer サンプル
cd examples/log-viewer
go build -o ../../bin/log-viewer .

# vault-cli サンプル
cd examples/vault-cli
go build -o ../../bin/vault-cli .
```

## デモの実行方法

### 1. cawa-client - Coding Agent Web API クライアント

Coding Agent Web API (AgentService) と対話するための CLI ツールです。
セッションの作成、メッセージの送信 (SSE ストリーミング)、ログの確認などが行えます。

#### 前提条件

- HAG サーバ (standalone) が起動していること (手順は下記「2. standalone」を参照)
- Claude Code CLI (`claude`) がインストールされていること

#### ビルド

```bash
cd examples/cawa-client
go build -o ../../bin/cawa-client .
cd ../..
```

#### 使い方

```bash
# ヘルスチェック (サーバの状態と CLI バージョンを確認)
./bin/cawa-client --server http://localhost:3100 health

# ログレベルを指定してヘルスチェックを実行 (デバッグ情報を詳細表示)
./bin/cawa-client --log-level debug health
# さらに詳細なトレースログ (リクエスト/レスポンスボディのダンプなど) を出力
./bin/cawa-client --log-level trace health

# 利用可能なエージェント一覧
./bin/cawa-client agents

# 利用可能なモデル一覧 (プロバイダごとにグループ化、デフォルトモデルを表示)
./bin/cawa-client models

# セッション作成 + メッセージ送信 (SSE ストリーミング, デフォルトモデル)
./bin/cawa-client run --agent claudecode --prompt "Hello, what can you do?"

# ログレベルを指定してセッションを作成し実行
./bin/cawa-client --log-level trace run --agent claudecode --prompt "Create a hello.py file"

# モデルを明示指定してセッション作成
./bin/cawa-client run --agent claudecode --model claude-sonnet-4-20250514 --prompt "Hello"

# セッション状態の確認
./bin/cawa-client session --id <SESSION_ID>

# セッションログのストリーミング
./bin/cawa-client logs --id <SESSION_ID>

# セッションの強制終了
./bin/cawa-client terminate --id <SESSION_ID>
```

`--server` オプションでサーバ URL を指定できます (デフォルト: `http://localhost:3100`)。
`--log-level` オプションでログ出力レベルを指定できます (デフォルト: `info`、指定可能レベル: `trace`, `debug`, `info`, `warn`, `error`)。

> [!IMPORTANT]
> **グローバルオプションの位置ルールについて**
> `--log-level` や `--server` などのグローバルオプションは、`run` や `health` などのサブコマンド（非オプション引数）よりも**前に**指定する必要があります。
>
> * **正しい構成 (動作する)**: `cawa-client [グローバルオプション] [サブコマンド] [サブコマンド用オプション]`
>   * 例: `./bin/cawa-client --log-level trace run --agent claudecode --prompt "Create a hello.py file"`
> * **誤った構成 (エラーになる)**: `cawa-client [サブコマンド] [グローバルオプション] ...`
>   * 例: `./bin/cawa-client run --log-level trace --agent claudecode --prompt "Create a hello.py file"`
>
> **理由**: Go言語の標準コマンドライン解析ツール (`flag` パッケージ) の仕様上、オプションではない引数 (サブコマンド名) が現れた時点でグローバルオプションの解析が停止するためです。サブコマンドより後ろにグローバルオプションを指定すると、定義されていないオプションとしてエラー (`flag provided but not defined`) になります。

> [!NOTE]
> `--model` で指定するモデル名は `model_profiles.yaml` に登録されている名称でなければなりません。
> 未登録のモデル名を指定した場合、セッション作成が 400 エラーで拒否され、
> 利用可能なモデル名の一覧がレスポンスに含まれます。

#### デモ実行フロー

```bash
# 1. 別ターミナルで HAG サーバを起動 (下記「2. standalone」の手順を参照)
./bin/standalone -config examples/standalone/config.yaml

# 2. ヘルスチェック
./bin/cawa-client health
# 出力例:
# Status: 200
# {
#   "status": "ok",
#   "agents": ["claudecode"],
#   "cli_versions": {"claudecode": "2.1.x"},
#   "gateway": {"status": "ok"}
# }

# 3. Coding Agent にタスクを実行させる
./bin/cawa-client run \
  --agent claudecode \
  --prompt "Create a hello.py file that prints Hello World" \
  --work-dir /path/to/workspace
# SSE でリアルタイムに応答がストリーミングされる
```

### 2. standalone - HAG サーバ単体起動

HAG サーバを起動して LLM Gateway プロキシと WebSocket ログサーバを立ち上げるデモです。

#### ローカル実行

```bash
# ビルド (共通)
cd examples/standalone
go build -o ../../bin/standalone .
cd ../..
```

> **Note**: `examples/standalone/config.yaml` の `model_profiles_path` はデフォルトで
> Docker コンテナ用のパス (`/etc/hag/model_profiles.yaml`) が設定されています。
> ローカル実行時は、実際の `model_profiles.yaml` のパスに変更してください。
>
> ```yaml
> llm_gateway:
>   port: 14000
>   model_profiles_path: "examples/standalone/model_profiles.yaml"  # ローカル用に変更
> ```

API キーの設定方法は Vault バックエンドによって異なります。
`config.yaml` の `vault.backend` で切り替えます。

**方法 A: 環境変数バックエンド (`vault.backend: "env"`) -- デフォルト**

```bash
# LLM プロバイダの API キーを環境変数で設定する。
# model_profiles.yaml の vault:// 参照が以下のように環境変数名に変換される:
#   vault://providers/openai/default    → HAG_VAULT_OPENAI_DEFAULT
#   vault://providers/anthropic/primary → HAG_VAULT_ANTHROPIC_PRIMARY
# 各環境変数の値に、対応するプロバイダの実際の API キーを設定する。
export HAG_VAULT_OPENAI_DEFAULT="sk-proj-your-openai-api-key"
export HAG_VAULT_ANTHROPIC_PRIMARY="sk-ant-your-anthropic-api-key"

# 起動 (プロジェクトルートから)
./bin/standalone -config examples/standalone/config.yaml
```

**方法 B: OS キーリングバックエンド (`vault.backend: "keyring"`)**

vault-cli で事前に API キーを登録済みであれば、環境変数の設定は不要です。
`config.yaml` の `vault.backend` を `"keyring"` に変更してください。

```bash
# vault-cli で API キーを登録 (初回のみ)
./bin/vault-cli set --provider openai
./bin/vault-cli set --provider anthropic

# 登録できたことを確認
./bin/vault-cli status
# 出力例:
#   LLM Provider Status:
#     openai: registered
#     anthropic: registered

# config.yaml の vault.backend を "keyring" に変更後、起動
./bin/standalone -config examples/standalone/config.yaml
```

サーバが起動すると以下のポートでリッスンします:

| ポート | 用途 |
|--------|------|
| 3100  | Coding Agent Web API (AgentService) |
| 14000 | LLM Gateway プロキシ |
| 18080 | WebSocket (ログストリーミング) |

#### Docker Compose で実行

```bash
cd examples/standalone

# 起動
docker compose up --build

# バックグラウンド起動
docker compose up --build -d

# 停止
docker compose down
```

> **Note**: `docker-compose.yaml` にはテスト用のダミーAPIキーが設定されています。
> 実際のAPIキーに差し替えて使用してください。

#### config.yaml の書式

```yaml
llm_gateway:
  port: 14000                                    # LLM Gateway プロキシポート
  model_profiles_path: "model_profiles.yaml"     # モデルプロファイル定義
log:
  level: "info"                                  # ログレベル (debug/info/warn/error)
vault:
  backend: "env"                                 # シークレットバックエンド (env/keyring/file)
websocket:
  port: 18080                                    # WebSocket サーバポート (省略時は OS がランダム割り当て)
agent_service:
  port: 3100                                     # AgentService HTTP ポート (デフォルト: 3100)
```

#### model_profiles.yaml の書式

```yaml
default_profile:
  provider: anthropic                              # デフォルトモデルのプロバイダ
  model: claude-sonnet-4-20250514                   # デフォルトモデル名

providers:
  openai:
    keys:
      - name: default
        value: vault://providers/openai/default    # vault:// で Vault から解決
        models:
          - name: gpt-4o
          - name: gpt-4o-mini
          - name: gpt-4.1-mini
  anthropic:
    keys:
      - name: default
        value: vault://providers/anthropic/default
        models:
          - name: claude-sonnet-4-20250514
  google:
    keys:
      - name: default
        value: vault://providers/google/default
        models:
          - name: gemini-2.5-flash
```

`default_profile` セクションはセッション作成時にモデルが指定されなかった場合のフォールバック先を定義します。

#### モデルの指定方法

セッション作成時にモデルを指定することで、使用する LLM を切り替えることができます。

**デフォルトモデル**: モデルを指定しない場合は、`model_profiles.yaml` の `default_profile` セクションで定義されたモデルが使用されます。

**利用可能なモデルの確認**:

```bash
# cawa-client で確認
./bin/cawa-client models

# API で確認 (GET /api/v1/models)
curl http://localhost:3100/api/v1/models
```

**cawa-client での指定**:

```bash
# デフォルトモデルを使用
./bin/cawa-client run --agent claudecode --prompt "Hello"

# Anthropic モデルを明示指定
./bin/cawa-client run --agent claudecode --model claude-sonnet-4-20250514 --prompt "Hello"
```

**API での指定** (POST `/api/v1/sessions`):

```bash
# モデル指定あり
curl -X POST http://localhost:3100/api/v1/sessions \
  -H "Content-Type: application/json" \
  -d '{"agent": "claudecode", "model": "claude-sonnet-4-20250514", "work_dir": "/tmp/work"}'

# モデル指定なし (DefaultModel にフォールバック)
curl -X POST http://localhost:3100/api/v1/sessions \
  -H "Content-Type: application/json" \
  -d '{"agent": "claudecode", "work_dir": "/tmp/work"}'
```

**モデルバリデーション**: `model_profiles.yaml` に登録されていないモデル名を指定した場合、400 エラーが返されます。

```json
{
  "error": "unknown model: nonexistent-model",
  "available_models": ["claude-sonnet-4-20250514", "gpt-4o", "gemini-2.5-flash"]
}
```

> **Important**: Coding Agent (claudecode) は Claude CLI を使用するため、指定可能なモデルは Anthropic プロバイダーのモデルに限定されます。
> OpenAI や Google のモデルは、LLM Gateway の直接 API (`/v1/chat/completions` 等) 経由で利用できます。

### 3. Coding Agent デモ (cawa-client)

standalone サーバに対して Coding Agent API を操作するデモです。
`cawa-client` は AgentService (ポート 3100) に接続し、ヘルスチェック、エージェント一覧、セッション作成/メッセージ送信 (SSE) を行います。

> **前提条件**: standalone サーバが起動していること (上記「2. standalone」を参照)。
> Claude Code CLI (`claude`) がインストール済みで PATH に通っている場合、`run` コマンドで実際にコーディングエージェントを起動できます。

#### Step 1: ヘルスチェック

```bash
./bin/cawa-client health
```

出力例:
```
Status: 200
{
  "agents": [
    "claudecode"
  ],
  "cli_versions": {
    "claudecode": "2.1.x (Claude Code)"
  },
  "gateway": {
    "status": "ok",
    "url": "http://localhost:14000"
  },
  "status": "ok"
}
```

`agents` が空の場合、`claude` CLI が PATH に見つからなかったことを意味します。

#### Step 2: エージェント一覧

```bash
./bin/cawa-client agents
```

出力例:
```
claudecode
```

#### Step 3: セッション作成とメッセージ送信

```bash
# Claude Code エージェントでプロンプトを実行 (SSE ストリーミング)
./bin/cawa-client run --agent claudecode --prompt "Create a hello.py file" --work-dir ./tmp/

# ログレベルに trace を指定してプロンプトを実行 (詳細な通信ログ等を表示)
./bin/cawa-client --log-level trace run --agent claudecode --prompt "Create a hello.py file" --work-dir ./tmp/
```

出力例:
```
Session created: 30c5db0aeb058d6e1a97f0f03e81ba41

--- Stream completed ---

{
  "agent_name": "claudecode",
  "created_at": "2026-06-09T22:18:22.130Z",
  "id": "30c5db0aeb058d6e1a97f0f03e81ba41",
  "status": "completed",
  "work_dir": "./tmp/"
}
```

#### その他のコマンド

```bash
# セッション状態の確認
./bin/cawa-client session --id <session-id>

# セッションのログをストリーミング
./bin/cawa-client logs --id <session-id>

# セッションの終了
./bin/cawa-client terminate --id <session-id>

# 接続先サーバを変更する場合
./bin/cawa-client --server http://localhost:4000 health

# ログレベルを変更して実行する場合
./bin/cawa-client --log-level debug health
```

### 4. log-viewer - WebSocket ログビューア

WebSocket 経由でエージェントのログをリアルタイムに表示するビューアです。
シミュレータモードを使えば、HAG サーバとダミーログ生成を一括で起動して動作を確認できます。

#### シミュレータモード (単体で動作確認)

サーバ起動 + ダミーログ生成を一括で実行します。別途サーバを起動する必要はありません。

```bash
# ビルド
cd examples/log-viewer
go build -o ../../bin/log-viewer .

# シミュレータ起動 (サーバ + ダミーログ生成)
../../bin/log-viewer --simulate

# 別のターミナルでビューアを接続
../../bin/log-viewer --url ws://localhost:18080/ws
```

シミュレータは以下のような階層的なエージェントログを生成します:

- テキスト応答 (root)
  - Thinking (思考過程)
  - Tool Use / Tool Result (ツール呼び出しと結果)
  - エラーハンドリング

#### ビューアモード (既存サーバに接続)

```bash
# standalone サーバが起動している状態で接続
../../bin/log-viewer --url ws://localhost:18080/ws
```

### 5. vault-cli - シークレット管理 CLI

OS のキーリング (macOS Keychain / Windows Credential Manager / Linux Secret Service) を使って
LLM プロバイダの API キーを安全に管理するための CLI ツールです。

```bash
# ビルド
cd examples/vault-cli
go build -o ../../bin/vault-cli .

# APIキーの登録
../../bin/vault-cli set --provider openai
# プロンプトが表示されるので API キーを入力

# stdin からの登録 (非対話)
echo "sk-your-api-key" | ../../bin/vault-cli set --provider openai --stdin

# 登録状況の確認
../../bin/vault-cli status

# 特定のキーの確認
../../bin/vault-cli get --provider openai

# キーの値を表示 (注意: 平文で出力されます)
../../bin/vault-cli get --provider openai --reveal

# 登録済みキーの一覧
../../bin/vault-cli list

# キーの削除
../../bin/vault-cli delete --provider openai
```

## テストの実行

```bash
# 単体テスト (ビルドスクリプト経由)
./scripts/process/build.sh

# 統合テスト
./scripts/process/integration_test.sh

# 共有ライブラリの単体テストのみ
cd shared/libs/go
go test ./... -v
```

## ライセンス

Apache License 2.0 - 詳細は [LICENSE](LICENSE) ファイルを参照してください。
