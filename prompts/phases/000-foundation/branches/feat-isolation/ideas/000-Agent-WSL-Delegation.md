# 仕様書: Ternエージェント実行環境の隔離連携におけるWSL2プロセス委譲

## 1. 背景 (Background)
Ternは安全なサンドボックス隔離環境を提供するために、WSL2上の隔離領域 (Overlay FS) を使用してエージェントにファイル編集などの操作を行わせます。
しかし、Windows上で動作するTernサーバーから、Windows上のエージェントCLIプロセス (Claude Code 等) を直接起動しようとする際、作業ディレクトリにWSL2内のネットワークパス (UNCパス: `\\wsl.localhost\Ubuntu\tmp\...`) を指定して起動すると、Windows APIおよび内部シェルの制約によって `chdir` (カレントディレクトリ変更) エラーが発生し、起動に失敗します。
この問題を解決するため、作業ディレクトリがWSL2のパスを指している場合には、Windowsホスト上で直接プロセスを起動する代わりに、`wsl.exe` を介してWSL2内部でエージェントプロセスを動作させる委譲 (ラッパー) 実行機構を導入します。

---

## 2. 要件 (Requirements)

### 必須要件

#### REQ-WSL-01: WSL2プロセス委譲実行機構
* **対象環境**: ホストOSがWindowsの場合。
* **検知条件**: エージェント実行アダプター (Claude Codeアダプター、Codexアダプター等) は、セッション起動時に指定された作業ディレクトリ (`WorkDir`) が WSL2 側のパス (UNCパス `\\wsl.localhost\...` や `\\wsl$\...`、または `/` から始まるLinuxパス) を指していることを検知した場合、ローカル起動ではなく `wsl.exe` を介してプロセスを起動すること。
* **後方互換性**: `WorkDir` が Windows の通常のローカルパス (C:ドライブ等) の場合や、WSL2が有効でない環境では、従来通り Windows ホスト上のプロセスとして直接起動すること。

#### REQ-WSL-02: Bubblewrap (bwrap) サンドボックスの自動適用
* **動作内容**: `wsl.exe` 経由でのプロセス起動の際、Ternのアダプター設定 (`DisableSandbox = false`) に従い、WSL2内部の `bwrap` コマンドでエージェントプロセスをラッピングして実行すること。
* **条件分岐**:
  * `DisableSandbox = false` (デフォルト) の場合: WSL2上で `bwrap --dev-bind / / [command]` 形式でエージェントプロセスを起動する。この際、CLI内部の多重サンドボックス起動を防ぐため、Claude Codeアダプターなどの固有のサンドボックススキップ環境変数は設定しない。
  * `DisableSandbox = true` の場合: `bwrap` ラッピングを適用せず、直接エージェントプロセスを起動する。Claude Codeアダプターでは `CLAUDE_CODE_SKIP_SANDBOX=1` を設定する。

#### REQ-WSL-03: 環境変数の透過的引き渡し
* **動作内容**: `wsl.exe` 経由でプロセスを起動する際、ホスト側で解決したシークレット情報 (Keyring等から取得した `ANTHROPIC_API_KEY` や `OPENAI_API_KEY` 等) およびTernの動作に必要な環境変数を、WSL2上のプロセスへ環境変数として自動的に透過させて渡すこと。
* **パス変換**: 環境変数の値に Windows の WSL2 UNCパスが含まれる場合 (例: `CLAUDE_CONFIG_DIR` や `CODEX_HOME` 等)、WSL2 Linux 内部の絶対パス表記 (例: `/tmp/...`) に自動で変換した上で WSL2 プロセスへ渡すこと。

#### REQ-WSL-04: WSL2側でのエージェントランタイム自動検出
* **動作内容**: `wsl.exe` 経由でエージェント (例: `claude` CLI) を起動する前に、WSL2内の指定ディストリビューション内に対応するCLI (Linux版) がインストールされており、実行可能かを検証すること。
* **エラーハンドリング**: 未検出の場合は、起動を中断し、適切なインストール警告メッセージ (例：`agent runtime "claude" not found in WSL2. Please install it in WSL2 (example: npm install -g @anthropic-ai/claude-code)`) を含むエラーを返却すること。

---

## 3. 実現方針 (Implementation Approach)

### 3.1 新規共通モジュールの配置
各アダプター (Claude Code, Codex) が共通してWSL2への委譲処理を実行できるようにするため、`shared/libs/go/codingagent/` の配下に新しく `wsl_executor.go` を作成します。

### 3.2 WSLパスの判定とLinuxパスへの変換ロジック
WindowsのUNCパスから、ディストリビューション名とLinux絶対パスを抽出します。
* `\\wsl.localhost\Ubuntu\tmp\merged` -> ディストリビューション: `Ubuntu`, Linuxパス: `/tmp/merged`
* `\\wsl$\Debian\workspace` -> ディストリビューション: `Debian`, Linuxパス: `/workspace`

### 3.3 コマンド組み立てロジック
`wsl.exe` 経由で Linux コマンドを実行する際、`env` コマンドを経由させることで環境変数を確実に引き渡します。
また、作業ディレクトリの切り替えには `wsl.exe` の `--chdir` オプションを使用します。

**起動コマンドの基本形 (DisableSandbox = false):**
```bash
wsl.exe -d [distro] --chdir [LinuxWorkDir] -- env [ENV_VARS...] bwrap --dev-bind / / [command] [args...]
```

**起動コマンドの基本形 (DisableSandbox = true):**
```bash
wsl.exe -d [distro] --chdir [LinuxWorkDir] -- env [ENV_VARS...] [command] [args...]
```

---

## 4. 検証シナリオ (Verification Scenarios)

### シナリオ1: 通常のWindowsローカル起動の動作維持
* **手順**:
  1. Windows上で `WorkDir` に通常の Windows ローカルパス (例: `C:\Users\...\work`) を指定してセッションを作成する。
* **期待結果**:
  1. `wsl.exe` は使用されず、Windowsネイティブのプロセスとしてエージェントが起動されること。

### シナリオ2: WSL2プロセス委譲起動 (サンドボックス有効)
* **前提条件**: WSL2内に `claude` (Linux版) がインストールされていること。
* **手順**:
  1. Windows上で `WorkDir` に WSL2 UNCパス (例: `\\wsl.localhost\Ubuntu\tmp\vv5-stage-1\merged`) を指定してセッションを作成する。この際 `DisableSandbox = false` とする。
* **期待結果**:
  1. `wsl.exe` の事前チェックを通過し、`wsl.exe -d Ubuntu --chdir /tmp/vv5-stage-1/merged -- env ... bwrap --dev-bind / / claude ...` の形式でプロセスが起動すること。
  2. 環境変数 `ANTHROPIC_API_KEY` などのシークレット情報が WSL2 プロセスへ透過的に渡されること。
  3. `CLAUDE_CONFIG_DIR` の値が、Linux形式の絶対パス (例: `/tmp/vv5-stage-1/.claudecode`) に自動変換されて渡されること。

### シナリオ3: ランタイム未検出時の警告動作
* **前提条件**: WSL2内に `nonexistent` という名前のコマンドが存在しないこと。
* **手順**:
  1. Windows上で `WorkDir` に WSL2 UNCパスを指定し、存在しないエージェントコマンド `nonexistent` を指定してセッションを作成する。
* **期待結果**:
  1. 起動が即座に中断され、`agent runtime "nonexistent" not found in WSL2. Please install it...` という警告を含んだエラーが返却されること。

---

## 5. テスト項目 (Testing for the Requirements)

要件を満たしていることを検証するため、自動化された再現用テスト、単体テスト、および統合テストを実行します。

### 5.1 再現用E2Eテストの追加 (テストファーストの適用)
* **必要性**:
  問題修正の前に、問題が実際に発生していることを証明し、修正後にその問題が解消されたことを機械的に確認するため、E2Eテストに「WSL2パスを模した作業ディレクトリでの起動失敗テスト」を追加します。
* **テスト内容**:
  1. `tests/agentservice_e2e_test.go` に `TestE2E_WSLDelegation_FailReproduction` を追加する。
  2. このテストでは `WorkDir` にあえてWSL2のパス (例: `\\wsl.localhost\Ubuntu\tmp\test-reproduce`) を指定してエージェントのセッション作成を試みる。
  3. 修正前のコードでは、Windowsプロセスが直接起動を試みるため、`chdir` の失敗により起動エラー (失敗) になることをアサートする。
  4. 修正後のコードでは、WSL2プロセス委譲機構が働いて正常に起動することを確認する (WSL2環境が存在しない場合は、事前チェックエラーで安全にスキップまたは適切にエラー返却されること)。

### 5.2 単体テストの追加と充実
`shared/libs/go/codingagent/wsl_executor_test.go` を新規作成し、以下の項目を網羅的にテストします。
* **パス変換の網羅的検証**:
  * `\\wsl.localhost\Ubuntu\...` などの標準的なUNCパス
  * `\\wsl$\Ubuntu\...` のような旧来のUNCパス
  * `/tmp/merged` などのLinux形式のパス
  * Windowsローカルパス (C:ドライブ等) で `isWSL = false` になること
* **環境変数パス変換の検証**:
  `BuildEnv` から組み立てられた環境変数のうち、値がWSL2のパスであるもの (例: `CLAUDE_CONFIG_DIR`) が正しくLinux形式の絶対パスに変換されていること。
* **WSLCommandBuilderの組み立て検証**:
  サンドボックスの有無や環境変数、ディストリビューション指定に応じて、`wsl.exe` 起動用のコマンドおよび引数が期待通り組み立てられること。

### 5.3 エラー・警告ハンドリングのテスト
* WSL2がインストールされていない、または `wsl.exe` が見つからない環境でのセーフフォールバックとエラー処理の動作検証。
* 指定されたWSLディストリビューションがインストールされていない場合、およびエージェントのランタイム (CLI) がWSL内にインストールされていない場合の警告文出力テスト。

### 5.4 テストの実行手順
追加したテストおよび既存のテストを含む検証は、以下のコマンドで実行します。

```bash
# 全体ビルドおよび単体テスト
./scripts/process/build.sh

# codingagent 関連の統合テストの実行
./scripts/process/integration_test.sh --categories "common" --specify "CodingAgent|WSL"
```
