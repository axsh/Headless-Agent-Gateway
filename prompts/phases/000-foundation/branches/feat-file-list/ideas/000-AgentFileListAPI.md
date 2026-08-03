# 000 - Tern アーティファクト管理 Web API

## 背景 (Background)

Tern はさまざまな Coding Agent（Cursor、Claude Code など）へ司令を出すオーケストレーター兼プロキシとして機能する。
Agent がセッション中に実行する Tool call（`write_file`、`edit_file`、`create_file` 等）には、
どのファイルを新規作成・変更・削除したかの情報が含まれる。

現状では以下の課題がある：

- Tern の Client 側から「Agent が触ったファイル」を把握する手段がない
- CI/CD パイプラインが成果物ファイルをプログラマティックに取得できない
- 複数 Agent を並走させるケースでどの Agent が何を生成したか追跡できない
- Agent にユーザーが用意したデータ（設定ファイル、データセット等）を渡す統一的な方法がない

本仕様では、Tern が管理する **アーティファクト（Artifact）** の概念を導入し、Web API と MCP 経由で
一元的にアクセスできる仕組みを定義する。

---

## アーティファクトの種別

```
Artifact
├── System Artifact  ... Coding Agent が Tool call で生成・変更したファイル
│                        実ファイルシステムと連動（プロジェクト内の実パスを参照）
└── User Artifact    ... ユーザーが任意にアップロードするデータ
                         実ファイルシステムとは切り離し（Tern が管理するストレージに配置）
                         DB 上の論理キーでアクセス
```

### System Artifact

- Coding Agent の Tool call ログを解析して自動収集
- プロジェクトルート相対パスを論理キー（`key`）として使用
- 実ファイルシステム上の現物ファイルを参照
- セッション・Agent 別に操作履歴（create / update / delete）を記録

### User Artifact

- ユーザーが Web API / UI 経由でアップロード
- `key` はユーザーが自由に定義する論理パス（例: `datasets/input.csv`）
- 実ファイルは Tern が管理するストレージ領域（`/var/tern/artifacts/user/` 等）に配置
- 実ファイルパスと `key` のマッピングは DB が保持
- Coding Agent は MCP ツール経由で `key` を使ってアクセス可能

---

## 要件 (Requirements)

### 必須要件

| # | 要件 |
|---|------|
| R1 | Coding Agent 別・セッション別に Tool call から「ファイル create / update / delete」イベントを記録 |
| R2 | System Artifact の一覧をセッション・Agent フィルタ付きで取得できる |
| R3 | セッション横断（全セッション・全 Agent）で System Artifact を検索できる |
| R4 | ファイル名・パスの glob フィルタリングをクエリパラメータで指定できる |
| R5 | ページネーション（`page` / `per_page`）をサポート |
| R6 | System Artifact の単一ファイルダウンロードを提供 |
| R7 | System Artifact の複数ファイル ZIP ダウンロードを提供 |
| R8 | User Artifact のアップロード・取得・更新・削除を提供 |
| R9 | User Artifact を `key`（論理パス）でアクセスできる |
| R10 | Coding Agent が MCP ツール経由で User Artifact を `key` で参照できる |
| R11 | アーティファクトのソース（`system` / `user`）を区別できる |
| R12 | 対応 Agent：Cursor Agent、Claude Code、Codex（拡張可能な設計） |

### 任意要件（Nice-to-have）

| # | 要件 |
|---|------|
| O1 | System Artifact のファイル差分（diff）取得エンドポイント |
| O2 | アーティファクト操作イベントの SSE によるリアルタイム通知 |
| O3 | ファイルのメタデータ（サイズ、MIME タイプ、SHA256）の返却 |
| O4 | User Artifact へのタグ付け・検索 |
| O5 | Coding Agent が MCP ツール経由で User Artifact を書き込む（アップロードする）機能 |

---

## 実現方針 (Implementation Approach)

### アーキテクチャ概要

```
┌──────────────────────────────────────────────────────────────────┐
│                          Tern Server                              │
│                                                                    │
│  ┌──────────────┐    ┌────────────────────────────────────────┐  │
│  │ Agent Proxy  │───▶│  Tool Call Log Analyzer                 │  │
│  │ (per agent)  │    │  (System Artifact イベント抽出)           │  │
│  └──────────────┘    └─────────────────┬──────────────────────┘  │
│                                         │ SystemArtifactEvent      │
│                       ┌─────────────────▼──────────────────────┐  │
│                       │     Artifact Store (SQLite)             │  │
│                       │   ┌────────────────────────────────┐   │  │
│                       │   │ system_artifact_events テーブル  │   │  │
│                       │   ├────────────────────────────────┤   │  │
│                       │   │ user_artifacts テーブル          │   │  │
│                       │   └────────────────────────────────┘   │  │
│                       └──────────┬──────────────────────────────┘  │
│                                  │                                  │
│          ┌───────────────────────┼────────────────────────┐        │
│          │                       │                        │        │
│  ┌───────▼──────────┐  ┌────────▼──────────┐  ┌─────────▼──────┐ │
│  │ /artifacts/system│  │ /artifacts/user   │  │   MCP Server   │ │
│  │  Web API          │  │  Web API          │  │  (for Agents)  │ │
│  └──────────────────┘  └───────────────────┘  └────────────────┘ │
└──────────────────────────────────────────────────────────────────┘
        ▲ HTTP                                         ▲ MCP
┌───────┴──────────┐                        ┌─────────┴──────────┐
│   Tern Client    │                        │   Coding Agent     │
└──────────────────┘                        └────────────────────┘
```

### コンポーネント設計

#### 1. Tool Call Log Analyzer（System Artifact 専用）

各 Coding Agent の Tool call ログをパースし、ファイル操作を検出する。

**対象 Tool call（Agent 種別ごと）:**

| Agent | 検出対象 Tool | 操作種別 |
|-------|------------|--------|
| Cursor Agent | `Write`, `StrReplace`, `Delete` | create / update / delete |
| Claude Code | `Write`, `Edit`, `MultiEdit` | create / update / delete |
| 共通 | ファイルパスを含む任意の tool result | 推定 |

#### 2. Artifact Store

**DB スキーマ（SQLite）:**

```sql
-- セッション管理
CREATE TABLE sessions (
  id          TEXT PRIMARY KEY,   -- UUID
  agent_id    TEXT NOT NULL,      -- "cursor", "claude-code" 等
  agent_name  TEXT,
  started_at  DATETIME NOT NULL,
  ended_at    DATETIME,
  metadata    JSON
);

-- System Artifact イベント（1 ファイルに複数イベントあり）
CREATE TABLE system_artifact_events (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id   TEXT NOT NULL REFERENCES sessions(id),
  agent_id     TEXT NOT NULL,
  key          TEXT NOT NULL,       -- プロジェクトルート相対パス（論理キー）
  actual_path  TEXT,                -- 実ファイルシステムの絶対パス
  operation    TEXT NOT NULL,       -- "create" | "update" | "delete"
  occurred_at  DATETIME NOT NULL,
  tool_name    TEXT,                -- 元の Tool call 名
  content_sha  TEXT                 -- SHA256
);

-- User Artifact（1 レコード = 1 アーティファクト）
CREATE TABLE user_artifacts (
  id           TEXT PRIMARY KEY,    -- UUID
  key          TEXT NOT NULL UNIQUE, -- ユーザー定義の論理キー（例: "datasets/input.csv"）
  actual_path  TEXT NOT NULL,       -- Tern 管理ストレージの絶対パス
  filename     TEXT NOT NULL,       -- 元のファイル名
  size         INTEGER,
  mime_type    TEXT,
  content_sha  TEXT,
  created_at   DATETIME NOT NULL,
  updated_at   DATETIME NOT NULL,
  metadata     JSON                 -- タグ等（O4 任意要件）
);

-- インデックス
CREATE INDEX idx_sae_session   ON system_artifact_events(session_id);
CREATE INDEX idx_sae_agent     ON system_artifact_events(agent_id);
CREATE INDEX idx_sae_key       ON system_artifact_events(key);
CREATE INDEX idx_sae_occurred  ON system_artifact_events(occurred_at);
```

#### 3. MCP Server（Coding Agent 向け）

Coding Agent が User Artifact を `key` で参照できる MCP ツールを提供する。
実ファイルパスは Agent に公開せず、Tern を経由したアクセスのみ許可する。

| MCP ツール名 | 概要 |
|------------|------|
| `list_user_artifacts` | User Artifact の一覧取得（key, filename, size, mime_type） |
| `get_user_artifact` | `key` 指定でファイルコンテンツ取得（テキスト / base64 バイナリ） |
| `put_user_artifact` | `key` 指定でファイルをアップロード（O5 任意要件） |

---

## Web API 設計

### 設計指針

- **リソース階層**: `artifacts/system` と `artifacts/user` を完全に分離
- **セッションはフィルタ**: セッション ID はパス階層でなくクエリパラメータで指定
  - 理由：「ファイルがセッションに属する」ではなく「ファイルをセッションでフィルタできる」
- **先行事例を参考**: GitHub Contents API・GitHub Actions Artifacts API・AWS S3 ListObjectsV2

### ベース URL

```
/api/v1
```

---

### System Artifact API

#### 一覧取得

```
GET /api/v1/artifacts/system
```

**クエリパラメータ:**

| パラメータ | 型 | 説明 | 例 |
|-----------|---|------|----|
| `q` | string | glob フィルタ（key 対象、doublestar 形式） | `q=**/*.go` |
| `agent_id` | string | Agent 種別で絞り込み（複数可） | `agent_id=cursor` |
| `session_id` | string | セッション ID で絞り込み（複数可） | `session_id=A&session_id=B` |
| `operation` | string | `create` / `update` / `delete` | `operation=create` |
| `since` | datetime | この日時以降（ISO 8601） | `since=2026-07-01T00:00:00Z` |
| `until` | datetime | この日時以前 | |
| `include_deleted` | bool | 削除済みを含む（デフォルト false） | |
| `page` | int | ページ番号（1 始まり） | `page=2` |
| `per_page` | int | 件数（最大 100、デフォルト 30） | `per_page=50` |
| `sort` | string | `key` / `occurred_at` / `operation` | `sort=occurred_at` |
| `order` | string | `asc` / `desc` | `order=desc` |

**レスポンス例:**

```json
{
  "source": "system",
  "total_count": 42,
  "page": 1,
  "per_page": 30,
  "items": [
    {
      "key": "internal/handler/user.go",
      "operation": "create",
      "agent_id": "cursor",
      "session_id": "sess-abc123",
      "occurred_at": "2026-07-31T10:23:45Z",
      "tool_name": "Write",
      "sha": "a3f9c2...",
      "size": 2048
    },
    {
      "key": "internal/handler/user_test.go",
      "operation": "update",
      "agent_id": "cursor",
      "session_id": "sess-abc123",
      "occurred_at": "2026-07-31T10:25:01Z",
      "tool_name": "StrReplace",
      "sha": "b8e1d4...",
      "size": 1536
    }
  ]
}
```

> **設計ノート**: 同一ファイルへの複数操作はそれぞれ独立したアイテムとして返す。
> 「現在存在するファイルのみ」に絞る場合は `include_deleted=false`（デフォルト）で最後の操作が
> `delete` でないものが返る。

---

#### 単一ファイルのメタデータ取得

```
GET /api/v1/artifacts/system/{key}
```

`{key}` は URL エンコードされたプロジェクトルート相対パス。

**クエリパラメータ:**

| パラメータ | 型 | 説明 |
|-----------|---|------|
| `session_id` | string | セッションに絞った操作履歴を表示 |

**レスポンス例:**

```json
{
  "source": "system",
  "key": "internal/handler/user.go",
  "operations": [
    { "agent_id": "cursor", "session_id": "sess-abc123", "operation": "create", "occurred_at": "...", "tool_name": "Write" },
    { "agent_id": "cursor", "session_id": "sess-abc123", "operation": "update", "occurred_at": "...", "tool_name": "StrReplace" }
  ],
  "current_sha": "b8e1d4...",
  "size": 2048,
  "mime_type": "text/x-go"
}
```

---

#### 単一ファイルのコンテンツダウンロード

```
GET /api/v1/artifacts/system/{key}/content
```

実ファイルシステムから読み取りバイナリストリームで返す。
`Content-Disposition: attachment; filename="..."` を付与。

---

#### 複数ファイルの ZIP ダウンロード

```
POST /api/v1/artifacts/system/archive
Content-Type: application/json

{
  "keys": [
    "internal/handler/user.go",
    "internal/handler/user_test.go"
  ],
  "format": "zip"
}
```

glob による一括指定も可能:

```json
{
  "q": "**/*.go",
  "session_id": "sess-abc123",
  "format": "zip"
}
```

**レスポンス:** `Content-Type: application/zip`

---

### User Artifact API

#### 一覧取得

```
GET /api/v1/artifacts/user
```

**クエリパラメータ:**

| パラメータ | 型 | 説明 |
|-----------|---|------|
| `q` | string | glob フィルタ（key 対象） |
| `page` | int | ページ番号 |
| `per_page` | int | 件数 |
| `sort` | string | `key` / `created_at` / `updated_at` / `size` |
| `order` | string | `asc` / `desc` |

**レスポンス例:**

```json
{
  "source": "user",
  "total_count": 5,
  "page": 1,
  "per_page": 30,
  "items": [
    {
      "key": "datasets/input.csv",
      "filename": "input.csv",
      "size": 102400,
      "mime_type": "text/csv",
      "sha": "c4d7e8...",
      "created_at": "2026-07-30T09:00:00Z",
      "updated_at": "2026-07-31T08:00:00Z"
    }
  ]
}
```

---

#### アップロード（新規 / 上書き）

```
PUT /api/v1/artifacts/user/{key}
Content-Type: multipart/form-data

file=@input.csv
```

または:

```
PUT /api/v1/artifacts/user/{key}
Content-Type: application/octet-stream

<binary body>
```

- `{key}` は任意の論理パス（例: `datasets/input.csv`、`configs/prod.yaml`）
- 実ファイルは Tern 管理ストレージに配置（実ファイルパスはユーザーに公開しない）
- `key` が既存の場合は上書き（`updated_at` を更新）

**レスポンス（201 Created）:**

```json
{
  "source": "user",
  "key": "datasets/input.csv",
  "filename": "input.csv",
  "size": 102400,
  "mime_type": "text/csv",
  "sha": "c4d7e8...",
  "created_at": "2026-07-31T10:00:00Z",
  "updated_at": "2026-07-31T10:00:00Z"
}
```

---

#### メタデータ取得

```
GET /api/v1/artifacts/user/{key}
```

---

#### コンテンツダウンロード

```
GET /api/v1/artifacts/user/{key}/content
```

---

#### 削除

```
DELETE /api/v1/artifacts/user/{key}
```

---

#### 複数ファイルの ZIP ダウンロード

```
POST /api/v1/artifacts/user/archive
Content-Type: application/json

{
  "keys": ["datasets/input.csv", "configs/prod.yaml"],
  "format": "zip"
}
```

---

### エンドポイント一覧サマリー

```
# System Artifacts（Agent 生成）
GET    /api/v1/artifacts/system                      一覧（全 Agent・全セッション）
GET    /api/v1/artifacts/system?session_id=X         セッションフィルタ
GET    /api/v1/artifacts/system?agent_id=cursor      Agent フィルタ
GET    /api/v1/artifacts/system/{key}                単一メタデータ
GET    /api/v1/artifacts/system/{key}/content        単一ダウンロード
POST   /api/v1/artifacts/system/archive              複数 ZIP ダウンロード

# User Artifacts（ユーザーアップロード）
GET    /api/v1/artifacts/user                        一覧
PUT    /api/v1/artifacts/user/{key}                  アップロード / 上書き
GET    /api/v1/artifacts/user/{key}                  単一メタデータ
GET    /api/v1/artifacts/user/{key}/content          単一ダウンロード
DELETE /api/v1/artifacts/user/{key}                  削除
POST   /api/v1/artifacts/user/archive                複数 ZIP ダウンロード

# セッション管理
GET    /api/v1/agents                                Agent 一覧
GET    /api/v1/agents/{agent_id}/sessions            セッション一覧
GET    /api/v1/agents/{agent_id}/sessions/{id}       セッション詳細
```

---

### MCP ツール仕様（Coding Agent 向け）

Coding Agent は Tern が提供する MCP Server に接続し、User Artifact を `key` で参照できる。
実ファイルパスは Agent に公開しない（Tern 管理ストレージの詳細を隠蔽）。

```yaml
tools:
  - name: list_user_artifacts
    description: |
      List available user-uploaded artifacts with their logical keys.
    input:
      q: string (optional, glob filter)
      page: int (optional)
      per_page: int (optional)
    output:
      items: [{key, filename, size, mime_type}]

  - name: get_user_artifact
    description: |
      Retrieve the content of a user artifact by its logical key.
      Returns text for text/* MIME types, base64 for binary.
    input:
      key: string (required)
      encoding: "text" | "base64" (default: "text")
    output:
      key: string
      filename: string
      mime_type: string
      content: string

  - name: put_user_artifact  # Optional (O5)
    description: |
      Upload or overwrite a user artifact by its logical key.
    input:
      key: string (required)
      content: string (required, text or base64)
      encoding: "text" | "base64" (default: "text")
      mime_type: string (optional)
```

---

## API 利用イメージ（ユースケース）

### ユースケース 1: セッション終了後に変更ファイルを確認する

```bash
# 最新セッションの ID を取得
SESSION_ID=$(curl -s "http://tern/api/v1/agents/cursor/sessions?per_page=1" \
  | jq -r '.items[0].id')

# そのセッションで触ったファイルを一覧表示
curl "http://tern/api/v1/artifacts/system?session_id=${SESSION_ID}" \
  | jq '.items[].key'
```

### ユースケース 2: Go ファイルだけを ZIP でダウンロード（セッション指定）

```bash
curl -X POST "http://tern/api/v1/artifacts/system/archive" \
  -H "Content-Type: application/json" \
  -d "{\"q\": \"**/*.go\", \"session_id\": \"${SESSION_ID}\", \"format\": \"zip\"}" \
  --output session_go_files.zip
```

### ユースケース 3: CI/CD パイプラインで成果物を取得

```yaml
# GitHub Actions 例
- name: Download agent-generated files
  run: |
    curl -X POST "http://tern/api/v1/artifacts/system/archive" \
      -H "Content-Type: application/json" \
      -d "{\"q\": \"**/*.go\", \"session_id\": \"${{ env.TERN_SESSION_ID }}\", \"format\": \"zip\"}" \
      --output artifacts.zip
    unzip artifacts.zip -d ./generated
```

### ユースケース 4: 全 Agent・全セッションで特定ファイルの更新履歴を追う

```bash
curl "http://tern/api/v1/artifacts/system?q=**/user.go&sort=occurred_at&order=desc" \
  | jq '.items[] | {session: .session_id, agent: .agent_id, op: .operation, at: .occurred_at}'
```

### ユースケース 5: ユーザーがデータセットをアップロードし、Agent に参照させる

```bash
# 1. ユーザーがデータをアップロード
curl -X PUT "http://tern/api/v1/artifacts/user/datasets/customers.csv" \
  --data-binary @customers.csv \
  -H "Content-Type: text/csv"

# 2. Agent (Cursor) に MCP 経由でタスクを指示
#    Agent は list_user_artifacts → get_user_artifact("datasets/customers.csv") で参照可能

# 3. Tern Client でアップロード済み一覧を確認
curl "http://tern/api/v1/artifacts/user?q=datasets/**"
```

### ユースケース 6: System / User 混在の一括取得（クライアント側で合成）

```bash
# System と User を並列取得して merge
system=$(curl -s "http://tern/api/v1/artifacts/system?q=**/*.go")
user=$(curl -s "http://tern/api/v1/artifacts/user")
echo "$system" "$user" | jq -s '[.[].items[]]'
```

---

## 設計上の決定事項

### セッションをパス階層から除外した理由

`/sessions/{id}/files/{path}` ではなく、`/artifacts/system?session_id=X&key=path` を採用。

- ファイルはセッションに「所属」するのではなく、セッションは「生成した文脈の記録」に過ぎない
- 同一ファイルが複数セッションで更新されることがある
- セッション横断クエリが自然に表現できる（`session_id` クエリを省略するだけ）

### User Artifact のストレージ分離

- `key`（論理パス）と実ファイルパスを DB でマッピング
- 実ファイルは Tern 管理ストレージ（`/var/tern/artifacts/user/{uuid}`）に配置
- `key` 変更時も実ファイルの移動が不要
- Agent に実ファイルパスを公開しないセキュリティ上の利点

### `key` の名前空間

- System: プロジェクトルート相対パス（例: `internal/handler/user.go`）
- User: ユーザー定義の任意パス（例: `datasets/input.csv`）
- 両者が衝突する可能性があるため、URL パスの `system/` / `user/` プレフィックスで明示的に分離

### glob フィルタリング

- `q` パラメータは [doublestar](https://github.com/bmatcuk/doublestar) 形式の glob
- 例: `**/*.go`、`internal/**`、`*_test.go`

### ストレージ

- **デフォルト**: SQLite（組み込み、運用コストゼロ）
- **将来の拡張**: PostgreSQL 対応（インターフェース抽象化）

---

## Go クライアントライブラリ設計

`client/v1` パッケージの既存パターン（`Client` + functional options + サブリソース）に倣い、
アーティファクト操作を `SystemArtifactClient` / `UserArtifactClient` としてサブクライアント方式で提供する。

### 型定義

```go
// --- 共通型 ---

// ArtifactListOptions はアーティファクト一覧取得のフィルタ・ページネーション条件を表す。
type ArtifactListOptions struct {
    Q             string   // glob フィルタ（例: "**/*.go"）
    AgentID       []string // System のみ：Agent ID フィルタ（複数可）
    SessionID     []string // System のみ：セッション ID フィルタ（複数可）
    Operation     string   // System のみ："create" | "update" | "delete"
    Since         time.Time
    Until         time.Time
    IncludeDeleted bool
    Page          int
    PerPage       int
    Sort          string   // "key" | "occurred_at" | "size" | "created_at" 等
    Order         string   // "asc" | "desc"
}

// SystemArtifactItem はシステムアーティファクトの 1 エントリ。
type SystemArtifactItem struct {
    Key        string    `json:"key"`
    Operation  string    `json:"operation"`
    AgentID    string    `json:"agent_id"`
    SessionID  string    `json:"session_id"`
    OccurredAt time.Time `json:"occurred_at"`
    ToolName   string    `json:"tool_name"`
    SHA        string    `json:"sha"`
    Size       int64     `json:"size"`
}

// SystemArtifactList はシステムアーティファクト一覧レスポンス。
type SystemArtifactList struct {
    TotalCount int                  `json:"total_count"`
    Page       int                  `json:"page"`
    PerPage    int                  `json:"per_page"`
    Items      []SystemArtifactItem `json:"items"`
}

// UserArtifactItem はユーザーアーティファクトの 1 エントリ。
type UserArtifactItem struct {
    Key       string    `json:"key"`
    Filename  string    `json:"filename"`
    Size      int64     `json:"size"`
    MIMEType  string    `json:"mime_type"`
    SHA       string    `json:"sha"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
}

// UserArtifactList はユーザーアーティファクト一覧レスポンス。
type UserArtifactList struct {
    TotalCount int                `json:"total_count"`
    Page       int                `json:"page"`
    PerPage    int                `json:"per_page"`
    Items      []UserArtifactItem `json:"items"`
}

// ArchiveRequest はアーカイブ（ZIP）ダウンロードのリクエスト。
type ArchiveRequest struct {
    Keys      []string // 個別 key 指定
    Q         string   // glob で一括指定（Keys と排他ではない、両方指定で union）
    SessionID []string // System のみ
    Format    string   // "zip"（デフォルト）
}
```

### SystemArtifactClient

```go
// SystemArtifactClient は /api/v1/artifacts/system エンドポイントを操作するクライアント。
type SystemArtifactClient struct {
    c *Client
}

// SystemArtifacts は SystemArtifactClient を返す。
func (c *Client) SystemArtifacts() *SystemArtifactClient {
    return &SystemArtifactClient{c: c}
}

// List はシステムアーティファクトの一覧を取得する。
func (s *SystemArtifactClient) List(ctx context.Context, opts ArtifactListOptions) (*SystemArtifactList, error)

// Get は指定 key のメタデータ（全操作履歴含む）を取得する。
func (s *SystemArtifactClient) Get(ctx context.Context, key string) (*SystemArtifactItem, error)

// Download は指定 key のファイルコンテンツを io.ReadCloser で返す。
// 呼び出し元は必ず Close すること。
func (s *SystemArtifactClient) Download(ctx context.Context, key string) (io.ReadCloser, error)

// DownloadTo は指定 key のファイルを dst に保存する。
func (s *SystemArtifactClient) DownloadTo(ctx context.Context, key, dst string) error

// Archive は ArchiveRequest に基づいて ZIP アーカイブを io.ReadCloser で返す。
func (s *SystemArtifactClient) Archive(ctx context.Context, req ArchiveRequest) (io.ReadCloser, error)

// ArchiveTo は Archive の結果を dst ファイルに保存する。
func (s *SystemArtifactClient) ArchiveTo(ctx context.Context, req ArchiveRequest, dst string) error
```

### UserArtifactClient

```go
// UserArtifactClient は /api/v1/artifacts/user エンドポイントを操作するクライアント。
type UserArtifactClient struct {
    c *Client
}

// UserArtifacts は UserArtifactClient を返す。
func (c *Client) UserArtifacts() *UserArtifactClient {
    return &UserArtifactClient{c: c}
}

// List はユーザーアーティファクトの一覧を取得する。
func (u *UserArtifactClient) List(ctx context.Context, opts ArtifactListOptions) (*UserArtifactList, error)

// Put はファイルを key に関連付けてアップロードする。key が既存の場合は上書き。
// r はファイルコンテンツの io.Reader。mimeType が空の場合は自動検出。
func (u *UserArtifactClient) Put(ctx context.Context, key string, r io.Reader, mimeType string) (*UserArtifactItem, error)

// PutFile はローカルファイルを key にアップロードする便利メソッド。
func (u *UserArtifactClient) PutFile(ctx context.Context, key, localPath string) (*UserArtifactItem, error)

// Get は指定 key のメタデータを取得する。
func (u *UserArtifactClient) Get(ctx context.Context, key string) (*UserArtifactItem, error)

// Download は指定 key のファイルコンテンツを io.ReadCloser で返す。
func (u *UserArtifactClient) Download(ctx context.Context, key string) (io.ReadCloser, error)

// DownloadTo は指定 key のファイルを dst に保存する。
func (u *UserArtifactClient) DownloadTo(ctx context.Context, key, dst string) error

// Delete は指定 key のユーザーアーティファクトを削除する。
func (u *UserArtifactClient) Delete(ctx context.Context, key string) error

// Archive は ArchiveRequest に基づいて ZIP アーカイブを io.ReadCloser で返す。
func (u *UserArtifactClient) Archive(ctx context.Context, req ArchiveRequest) (io.ReadCloser, error)

// ArchiveTo は Archive の結果を dst ファイルに保存する。
func (u *UserArtifactClient) ArchiveTo(ctx context.Context, req ArchiveRequest, dst string) error
```

---

## README サンプルコード

以下のサンプルは `README.md` の「Artifact API Examples」セクションとして掲載することを想定している。

### Artifact API Examples

#### 1. セッション完了後に生成ファイルを一覧表示する

```go
import client "github.com/axsh/arctic-tern/client/v1"

c := client.New("http://localhost:3100")

// セッションを作成してタスクを実行
session, _ := c.CreateSession(ctx, client.SessionRequest{
    Agent:   "claudecode",
    WorkDir: ".",
})
stream, _ := session.SendText(ctx, "Implement the user authentication module")
stream.Output(os.Stdout)

// セッション中に生成・変更されたファイルを確認
list, _ := c.SystemArtifacts().List(ctx, client.ArtifactListOptions{
    SessionID: []string{session.ID},
    Q:         "**/*.go",
})
for _, item := range list.Items {
    fmt.Printf("[%s] %s\n", item.Operation, item.Key)
}
```

#### 2. 生成された Go ファイルを ZIP でダウンロードする

```go
err := c.SystemArtifacts().ArchiveTo(ctx, client.ArchiveRequest{
    Q:         "**/*.go",
    SessionID: []string{session.ID},
}, "generated_go_files.zip")
```

#### 3. ユーザーデータをアップロードして Agent に参照させる

```go
// データセットをアップロード
_, err := c.UserArtifacts().PutFile(ctx, "datasets/customers.csv", "./local/customers.csv")

// Agent はセッション内で MCP 経由により
// get_user_artifact("datasets/customers.csv") でアクセスできる
session, _ := c.CreateSession(ctx, client.SessionRequest{Agent: "claudecode", WorkDir: "."})
stream, _ := session.SendText(ctx, "Analyze the customers.csv dataset and generate a summary report")
stream.Output(os.Stdout)
```

#### 4. 複数セッション横断でファイル変更履歴を追う

```go
// 全セッションで user.go が変更された履歴を確認
list, _ := c.SystemArtifacts().List(ctx, client.ArtifactListOptions{
    Q:     "**/user.go",
    Sort:  "occurred_at",
    Order: "desc",
})
for _, item := range list.Items {
    fmt.Printf("[%s] agent=%s session=%s at=%s\n",
        item.Operation, item.AgentID, item.SessionID, item.OccurredAt)
}
```

#### 5. CI/CD パイプラインでの成果物取得パターン

```go
// セッションを実行して成果物を収集
session, _ := c.CreateSession(ctx, client.SessionRequest{Agent: "claudecode", WorkDir: "."})
err := session.SendTextWithHandlers(ctx, "Generate the complete API handler suite", client.StreamHandlers{
    OnText: func(t string) { fmt.Print(t) },
})

// 変更されたファイルをすべて ZIP で保存
_ = c.SystemArtifacts().ArchiveTo(ctx, client.ArchiveRequest{
    SessionID: []string{session.ID},
}, fmt.Sprintf("artifacts-%s.zip", session.ID))

session.Terminate(ctx)
```

#### 6. User Artifact の管理（一覧・削除）

```go
ua := c.UserArtifacts()

// 一覧表示
list, _ := ua.List(ctx, client.ArtifactListOptions{Q: "datasets/**"})
for _, item := range list.Items {
    fmt.Printf("%s  (%d bytes, updated %s)\n", item.Key, item.Size, item.UpdatedAt)
}

// 個別ダウンロード
_ = ua.DownloadTo(ctx, "datasets/customers.csv", "./local/customers_backup.csv")

// 不要になったアーティファクトを削除
_ = ua.Delete(ctx, "datasets/customers.csv")
```

---

## 検証シナリオ (Verification Scenarios)

### System Artifact の収集

1. Cursor Agent セッション中に `Write` Tool call → DB に `create` イベント記録
2. 同セッション中に `StrReplace` → 同 key に `update` イベント追記
3. `GET /api/v1/artifacts/system?session_id={id}` で一覧取得 → 該当ファイル表示
4. `q=**/*.go` フィルタ → `.go` ファイルのみ返る
5. `POST /api/v1/artifacts/system/archive` で ZIP ダウンロード → ZIP に対象ファイル含む
6. `GET /api/v1/artifacts/system?q=**/user.go` → 複数セッションの操作履歴が返る

### User Artifact の Web API 操作

7. `PUT /api/v1/artifacts/user/datasets/input.csv` でアップロード → `key` で取得できる
8. `GET /api/v1/artifacts/user/datasets/input.csv/content` → ファイルコンテンツ取得できる
9. User Artifact を `DELETE` → 一覧から消える、`content` 取得が 404
10. `POST /api/v1/artifacts/user/archive` → 複数 User Artifact が 1 つの ZIP にまとまる

### Coding Agent からの MCP アクセス

11. Tern の MCP Server に接続した Coding Agent セッション内で `list_user_artifacts` を呼び出す → アップロード済みの User Artifact 一覧が返る
12. `list_user_artifacts(q="datasets/**")` を呼び出す → glob フィルタが効き `datasets/` 配下のみ返る
13. `get_user_artifact("datasets/input.csv")` を呼び出す → テキストコンテンツが返る
14. `get_user_artifact("images/logo.png", encoding="base64")` を呼び出す → base64 エンコードされたバイナリが返る
15. 存在しない key を `get_user_artifact` で取得 → エラーレスポンスが返り、セッションが継続する
16. `put_user_artifact("outputs/report.md", content="...", encoding="text")` を呼び出す（O5 任意）→ Web API で `GET /api/v1/artifacts/user/outputs/report.md` により取得できる

---

## テスト項目 (Testing)

### 統合テスト

```bash
# GUI カテゴリ（API エンドポイント動作確認）
./scripts/process/integration_test.sh --categories gui --specify "artifact-api"

# タスクエンジンカテゴリ（Tool call ログ解析）
./scripts/process/integration_test.sh --categories taskengine --specify "tool-call-analyzer"

# taskengine カテゴリ（MCP Server 経由の User Artifact アクセス）
./scripts/process/integration_test.sh --categories taskengine --specify "mcp-user-artifact"
```

### 単体テスト

| 対象 | テスト内容 |
|------|-----------|
| `ToolCallAnalyzer` | 各 Agent の Tool call JSON → 正しい `SystemArtifactEvent` 生成 |
| `ArtifactStore` | CRUD・glob フィルタ・ページネーション・session フィルタ |
| `SystemArtifactAPI` | 一覧・メタデータ・コンテンツ・アーカイブの正常系・異常系 |
| `UserArtifactAPI` | アップロード・取得・削除・アーカイブの正常系・異常系 |
| `key` 重複チェック | User Artifact の `PUT`（新規 vs 上書き） |
| ZIP アーカイブ | System / User それぞれの複数ファイル圧縮の正常系 |
| `MCPServer.ListUserArtifacts` | glob フィルタあり・なし・ページネーションの正常系 |
| `MCPServer.GetUserArtifact` | テキスト取得・base64 取得・存在しない key のエラー処理 |
| `MCPServer.PutUserArtifact` | テキスト・base64 アップロード後に Web API で取得できること（O5 任意） |
| MCP 認証・隔離 | Agent セッション外からの MCP 呼び出しが拒否されること |
