# 021: ループバックバインド統一によるWindowsファイアウォールダイアログ抑制

## 背景 (Background)

自動テスト（特にサーバ/クライアント系のE2Eテスト）を実行する際、Windowsセキュリティのファイアウォール警告ダイアログが繰り返し表示される。このダイアログは、起動したプログラムがプライベートネットワークで通信してよいかの許可を求めるもので、通信を実際にブロックするわけではないためテストプロセス自体は進行する。しかし、以下の理由で問題となっている:

- **自動テストの妨げ**: テストを放置して自動実行したいにもかかわらず、人間への確認ダイアログが蓄積する
- **ゴミリソース**: 応答されないダイアログがシステムリソースとして残り続ける
- **完全自動化の阻害**: CI/ローカルの完全自動テストの達成を妨げる

### 原因分析

Goの `net.Listen("tcp", ":0")` や `net.Listen("tcp", fmt.Sprintf(":%d", port))` のようにホスト部分を省略した場合、OSは**すべてのネットワークインターフェース** (`0.0.0.0`) にバインドする。Windowsファイアウォールはこれを検知し、外部ネットワークからの接続許可を求めるダイアログを表示する。

一方、`net.Listen("tcp", "127.0.0.1:0")` のように**ループバックアドレスを明示的に指定**した場合、ループバックインターフェースのみでの通信となり、Windowsファイアウォールはこれを外部通信とみなさないためダイアログは表示されない。

### 現状のコードベース

プロジェクト内で既に `127.0.0.1` にバインドしているコンポーネントと、未対応のコンポーネントが混在している:

| コンポーネント | ファイル | バインドアドレス | 状態 |
|---|---|---|---|
| wsserver | `shared/libs/go/wsserver/server.go:55` | `127.0.0.1:%d` | 対応済み |
| passthrough | `shared/libs/go/llmgateway/passthrough.go:24` | `127.0.0.1:%d` | 対応済み |
| agentservice | `shared/libs/go/agentservice/service.go:94` | `:%d` | **未対応** |
| llmgateway proxy | `shared/libs/go/llmgateway/proxy.go:63` | `:%d` | **未対応** |
| E2Eテスト freePort | `tests/agentservice_e2e_test.go:37` | `:0` | **未対応** |

## 要件 (Requirements)

### 必須要件

1. **R1: agentserviceのバインドアドレス変更**
   - `shared/libs/go/agentservice/service.go` の `Launch()` メソッドで、`fmt.Sprintf(":%d", port)` を `fmt.Sprintf("127.0.0.1:%d", port)` に変更する
   - 変更前: `net.Listen("tcp", fmt.Sprintf(":%d", port))`
   - 変更後: `net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))`

2. **R2: llmgateway proxyのバインドアドレス変更**
   - `shared/libs/go/llmgateway/proxy.go` の `Launch()` メソッドで、`fmt.Sprintf(":%d", p.port)` を `fmt.Sprintf("127.0.0.1:%d", p.port)` に変更する
   - 変更前: `addr := fmt.Sprintf(":%d", p.port)`
   - 変更後: `addr := fmt.Sprintf("127.0.0.1:%d", p.port)`

3. **R3: E2Eテストの freePort() 関数の修正**
   - `tests/agentservice_e2e_test.go` の `freePort()` 関数で、`:0` を `127.0.0.1:0` に変更する
   - 変更前: `net.Listen("tcp", ":0")`
   - 変更後: `net.Listen("tcp", "127.0.0.1:0")`

4. **R4: 既存テストの正常動作維持**
   - 全ての単体テストが変更後も正常にパスすること
   - ビルドが成功すること

### 任意要件

なし。本変更はすべて必須要件のみで構成される。

## 実現方針 (Implementation Approach)

### 変更方針

各対象ファイルのバインドアドレス文字列を機械的に `127.0.0.1` プレフィックス付きに変更する。変更は最小限で、ロジックの変更は発生しない。

### 対象ファイルと変更内容

#### 1. `shared/libs/go/agentservice/service.go` (L94)

```diff
-	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
+	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
```

#### 2. `shared/libs/go/llmgateway/proxy.go` (L63)

```diff
-	addr := fmt.Sprintf(":%d", p.port)
+	addr := fmt.Sprintf("127.0.0.1:%d", p.port)
```

#### 3. `tests/agentservice_e2e_test.go` (L37)

```diff
-	l, err := net.Listen("tcp", ":0")
+	l, err := net.Listen("tcp", "127.0.0.1:0")
```

### 設計上の決定事項

- **`127.0.0.1` を使用し、`localhost` は使用しない**: `localhost` はDNS解決を経由するため、IPv6 (`::1`) に解決される可能性がある。`127.0.0.1` はIPv4ループバックアドレスを直接指定するため、確実にループバックインターフェースのみにバインドされる。これは既存の `wsserver` と `passthrough` のパターンとも一致する。
- **`ProxyURL()` の返り値は変更しない**: `proxy.go` の `ProxyURL()` は現在 `http://localhost:%d` を返している。クライアント側の接続先としての `localhost` は問題なく `127.0.0.1` に解決されるため、この返り値は変更しない。ただし、一貫性を高めるために `http://127.0.0.1:%d` に変更することも検討できるが、本仕様のスコープ外とする。

## 検証シナリオ (Verification Scenarios)

1. 3箇所の対象ファイルを修正する
2. `scripts/process/build.sh` を実行し、全体ビルドと単体テストが全てパスすることを確認する
3. 修正後、テスト実行時にWindowsファイアウォールのダイアログが表示されないことを手動で確認する（自動化対象外）

## テスト項目 (Testing for the Requirements)

### ビルドと単体テスト

変更対象はバインドアドレスの文字列のみであり、新規ロジックの追加はない。既存テストで回帰を確認する:

```bash
./scripts/process/build.sh
```

### 影響範囲の分析

- 変更対象は `agentservice`、`llmgateway`、E2Eテストのユーティリティ関数の3箇所
- いずれもバインドアドレスの文字列変更のみで、APIやプロトコルに変更はない
- `agentservice` と `llmgateway` のリッスン先が `0.0.0.0` から `127.0.0.1` に変わるが、クライアントは全て `localhost` または `127.0.0.1` で接続しているため影響なし
- 統合テストの実行は本変更では不要（サーバ間通信のプロトコルやAPIは変更なし）
