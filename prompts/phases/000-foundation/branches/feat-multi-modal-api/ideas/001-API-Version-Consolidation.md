# 001-API-Version-Consolidation

## 背景 (Background)

マルチモーダル対応のために v2 API エンドポイントを追加したが、以下の問題が明らかになった:

1. **server側**: 現在 `HTTPHandler()` で v1 と v2 のルートを両方ハードコードで登録している。特定のバージョンだけを有効にする手段がない。
2. **client側**: `client` パッケージは v1 API (`/api/v1/sessions/:id/messages`) にハードコードされており、v2 API を呼ぶメソッドが存在しない。README では `SendMessageV2()` や `client.ContentPart` を記載しているが、実際には未実装。
3. **バージョニングの混乱**: v1 は `{"message": "..."}` 形式、v2 は `{"content": [...]}` 形式と、リクエスト構造が根本的に異なる。しかし v1 を使っている外部ユーザーは現時点では存在しないため、後方互換を維持する意味が薄い。

### ユーザーの方針決定

ユーザーは以下の方針を選択した:

- **v1 API は廃止する**。現時点で外部ユーザーがいないため、後方互換を維持するコストに見合わない。
- **現在の v2 API を新しい v1 として位置づけ直す**。つまり、マルチモーダル対応のコンテンツブロック配列形式 (`{"content": [...]}`) が最初のサポートバージョン (v1) となる。
- **client パッケージは `client/v1/` として再構成する**。旧 `client/` は削除または非推奨化する。

---

## 要件 (Requirements)

### R1: サーバー側 API バージョン制御

- R1-1: `server.WithEnableVersion()` オプション関数を追加する。
  - 引数: 可変長の `int` (例: `server.WithEnableVersion(1)`, `server.WithEnableVersion(1, 2)`)。
  - 存在しないバージョン番号が指定された場合、`server.New()` が適切な `error` を返す。
  - 現時点でサポートされるバージョンは `1` のみ (旧v2を新v1に統合)。
- R1-2: `WithEnableVersion()` が指定されない場合、全てのサポート済みバージョンが有効になる (デフォルト動作)。
- R1-3: `agentservice.Server` の `HTTPHandler()` は、有効なバージョンのルートのみを登録する。

### R2: API エンドポイントの再番号付け

- R2-1: 旧 v2 エンドポイント (`/api/v2/sessions/:id/messages`) を新 v1 (`/api/v1/sessions/:id/messages`) として移行する。
  - リクエスト形式: `{"content": [{"type": "text", "text": "..."}, {"type": "image", ...}]}`
  - テキストのみの場合も content 配列形式を使用する。
- R2-2: 旧 v1 のメッセージ送信エンドポイント (`{"message": "..."}` 形式) は削除する。
- R2-3: セッション管理系 (POST/GET/DELETE sessions, terminate, logs, agents, models) はバージョン非依存であったが、これらも新 v1 のパスで統一する。

### R3: クライアントパッケージの再構成

- R3-1: 新しい `client/v1/` パッケージを作成する。
  - import パス: `github.com/axsh/arctic-tern/client/v1`
  - `SendMessage()` メソッドは `[]ContentPart` を受け取る形式とする。
  - `ContentPart`, `ImageSource` 型をクライアントパッケージ内に定義 (または `codingagent` パッケージから re-export)。
- R3-2: 旧 `client/` パッケージは非推奨 (deprecated) とする。
  - 既存のコードは残すが、ファイル冒頭に `// Deprecated:` コメントを追加する。
  - `README.md` の例は全て `client/v1` に更新する。
- R3-3: `examples/minimal-client` を `client/v1` を使用するように更新する。

### R4: exampleの更新

- R4-1: `examples/minimal-server/main.go` に `server.WithEnableVersion(1)` の使用例をコメントで示す。
- R4-2: `examples/minimal-client/main.go` を `client/v1` パッケージに更新し、マルチモーダル送信の例もコメントで示す。

### R5: README の整合性

- R5-1: README.md のサンプルコードを全て新しい API/パッケージ構造に合わせて更新する。
- R5-2: v1 (旧v2) がコンテンツブロック配列形式であることを明記する。

---

## 実現方針 (Implementation Approach)

### サーバー側

```go
// options.go に追加
func WithEnableVersion(versions ...int) Option {
    return func(o *options) {
        o.enableVersions = versions
    }
}

// server.New() 内で検証
supportedVersions := map[int]bool{1: true}
for _, v := range o.enableVersions {
    if !supportedVersions[v] {
        return nil, fmt.Errorf("unsupported API version: %d", v)
    }
}
```

### agentservice 側

```go
// HTTPHandler() でバージョンに応じてルート登録
func (s *Server) HTTPHandler() http.Handler {
    mux := http.NewServeMux()
    mux.HandleFunc("/health", s.handleHealth)

    if s.isVersionEnabled(1) {
        mux.HandleFunc("/api/v1/agents", s.routeAgents)
        mux.HandleFunc("/api/v1/models", s.routeModels)
        mux.HandleFunc("/api/v1/sessions", s.routeSessions)
        mux.HandleFunc("/api/v1/sessions/", s.routeSessionByID)
        // v1 メッセージ送信は content block 形式 (旧 v2)
    }
    return mux
}
```

### クライアント側

```
client/
  client.go          // Deprecated: use client/v1
  session.go         // Deprecated
  ...
client/v1/
  client.go          // New v1 client
  session.go         // SendMessage(ctx, []ContentPart)
  content.go         // ContentPart, ImageSource types
  stream.go          // Stream handling (reuse/copy from client/)
```

### ハンドラ統合

- `handler_v2.go` の `handleSendMessageV2` を `handler.go` の `handleSendMessage` としてマージする。
- 旧 v1 の `handleSendMessage` (`{"message": "..."}` 形式) は削除する。
- `handler_v2.go` ファイルは統合後に削除する。

---

## 検証シナリオ (Verification Scenarios)

1. `server.New(server.WithEnableVersion(1))` でサーバーを起動し、`/api/v1/sessions` が利用可能であることを確認する。
2. `server.New(server.WithEnableVersion(99))` が `error` を返すことを確認する。
3. `server.New()` (バージョン指定なし) で全ルートが登録されることを確認する。
4. `client/v1` パッケージで `SendMessage(ctx, []ContentPart{...})` を呼び出し、テキストのみメッセージが送信できることを確認する。
5. `client/v1` パッケージで画像付きメッセージが送信できることを確認する。
6. Wayfinder エージェントに画像付きメッセージを送ると `501 Not Implemented` が返ることを確認する。
7. `examples/minimal-client` がビルド・実行可能であることを確認する。
8. 旧 `client/` パッケージに Deprecated コメントが付いていることを確認する。

---

## テスト項目 (Testing for the Requirements)

### 単体テスト

```bash
./scripts/process/build.sh
```

- `server/` パッケージ: `WithEnableVersion` のバリデーション、不正バージョンのエラー
- `agentservice/`: バージョン別ルート登録、ハンドラ動作
- `client/v1/`: `SendMessage` の content block 送信、ストリーム処理

### 結合テスト

```bash
./scripts/process/integration_test.sh --specify "TestMultimodal"
./scripts/process/integration_test.sh --specify "TestAgentService"
```

- 既存テストが新v1パスで動作することを確認
- マルチモーダルテストが新v1パスで動作することを確認
- リグレッションがないことを確認
