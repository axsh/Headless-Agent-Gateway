# 000-Agent-WSL-Delegation

> **Source Specification**: prompts/phases/000-foundation/branches/feat-isolation/ideas/000-Agent-WSL-Delegation.md

## Goal Description
ホストOSがWindows環境において、Ternエージェントの作業ディレクトリ (WorkDir) が WSL2 側のパス (UNCパス等) を指している場合に、Windows上で直接プロセスを起動する代わりに `wsl.exe` を介してプロセスを起動し、Bubblewrap (bwrap) サンドボックス、環境変数の引き渡し、およびランタイム自動検出を適用するための WSL2 プロセス委譲実行機構を導入します。

## User Review Required
None.

## Requirement Traceability

> **Traceability Check**:
> 仕様書(Specification)の要件・決定事項をリストアップし、この計画書のどこで対応するかをマッピングしてください。

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| **REQ-WSL-01**: WSL2プロセス委譲実行機構 | Proposed Changes > wsl_executor.go, claudecode/process.go, codex/process.go |
| **REQ-WSL-02**: Bubblewrap (bwrap) サンドボックスの自動適用 | Proposed Changes > wsl_executor.go |
| **REQ-WSL-03**: 環境変数の透過的引き渡し | Proposed Changes > wsl_executor.go, claudecode/process.go, codex/process.go |
| **REQ-WSL-04**: WSL2側でのエージェントランタイム自動検出 | Proposed Changes > wsl_executor.go, claudecode/process.go, codex/process.go |

---

## Proposed Changes

### shared/libs/go/codingagent

#### [NEW] [wsl_executor_test.go](file://shared/libs/go/codingagent/wsl_executor_test.go)
* **Description**: WSLパス判定、パス変換、およびコマンド組み立てロジックの単体テスト。
* **Technical Design**:
  * `TestParseWSLPath`: Windows環境における各種WSLパス (UNCパス、Linuxパス表記、Windowsローカルパス) が正しくパースされ、ディストリビューション名とLinux絶対パスが抽出できることを検証します。
  * `TestConvertToLinuxPath`: WSLパスがLinux形式のパスに正しく変換されること、および通常のWindowsパスはそのまま返されることを検証します。
  * `TestWSLCommandBuilder`: `WSLCommandBuilder` が、サンドボックスの有無や環境変数に応じて期待通りの `wsl.exe` コマンドおよび引数を組み立てることを検証します。

#### [NEW] [wsl_executor.go](file://shared/libs/go/codingagent/wsl_executor.go)
* **Description**: WSL判定、パス変換、および `wsl.exe` コマンド組み立てを行う共通ヘルパーモジュール。
* **Technical Design**:
  * ```go
    package codingagent

    import (
    	"context"
    	"os/exec"
    )

    // ParseWSLPath はWindowsのWSLパスをパースし、ディストリビューション名とLinux絶対パスを返します。
    func ParseWSLPath(path string) (distro string, linuxPath string, isWSL bool)

    // ConvertToLinuxPath はWindowsのWSLパスをLinux形式の絶対パスに変換します。WSLパスでない場合は入力をそのまま返します。
    func ConvertToLinuxPath(path string) string

    // VerifyWSLRuntime はWSL内に指定されたコマンドが存在するか 'which' コマンドで検証します。
    func VerifyWSLRuntime(ctx context.Context, distro string, cmdName string) error

    // WSLCommandBuilder はwsl.exe経由での実行コマンドを組み立てるビルダー構造体。
    type WSLCommandBuilder struct {
    	Distro         string
    	WorkDir        string // WSL内の絶対Linuxパス
    	Command        string // 起動コマンド (例: "claude", "codex")
    	Args           []string // 起動引数
    	Env            []string // 環境変数 (KEY=VALUE形式)
    	DisableSandbox bool
    }

    func (b *WSLCommandBuilder) BuildCmd(ctx context.Context) *exec.Cmd
    ```
* **Logic**:
  * `ParseWSLPath`:
    * ホストがWindowsでない場合は即座に `false` を返す。
    * パスが `\\wsl.localhost\` もしくは `\\wsl$\` で始まる場合、プレフィックスを除去して最初のバックスラッシュまでの文字列をディストリビューション名、以降を Linux スタイルの絶対パス (バックスラッシュを `/` に変換) とする。
    * パスが `/` で始まる場合、デフォルトのディストリビューションとして扱い、ディストリビューション名を空、Linuxパスを入力パスそのままとして `true` を返す。
  * `VerifyWSLRuntime`:
    * `wsl.exe` コマンドを用いて `which [cmdName]` を実行する。終了コードが非0の場合は、対応するインストール警告メッセージを伴うエラーを返却する。
  * `WSLCommandBuilder.BuildCmd`:
    * `wslArgs` に `-d [Distro]`, `--chdir [WorkDir]`, `--` を順に格納。
    * 続いて `env` を追加し、`Env` スライスを巡回して各環境変数の値が WSL パスであれば Linux パス形式に変換した上で `KEY=LinuxPath` 形式にして `wslArgs` に追加する。
    * `DisableSandbox` が `false` の場合、`bwrap`, `--dev-bind`, `/`, `/` を `wslArgs` に追加する。
    * 最後に `Command` と `Args` を追加し、`exec.CommandContext(ctx, "wsl.exe", wslArgs...)` を生成して返す。

#### [MODIFY] [process.go (claudecode)](file://shared/libs/go/codingagent/claudecode/process.go)
* **Description**: `claudecode` アダプターのプロセス起動時に、WSL2プロセス委譲実行を適用します。
* **Technical Design & Logic**:
  * `BuildEnv`:
    * 環境変数 `CLAUDE_CONFIG_DIR` (値が `cfg.SessionDir`) を構築する際、もし `cfg.SessionDir` が WSL パスであれば、Linux パス形式に変換してマップに格納する。
  * `StartProcess`:
    * `cfg.WorkDir` に対し `codingagent.ParseWSLPath` を実行。
    * WSL パスである場合:
      1. `VerifyWSLRuntime` を実行して `claude` CLI が存在するかをチェックする。
      2. 存在する場合、`WSLCommandBuilder` を用いて `wsl.exe` による実行コマンドを組み立て、これを `cmd` とする。
    * WSL パスでない場合:
      1. 従来通り Windows ホスト上のプロセスとして直接 `exec.CommandContext(procCtx, "claude", args...)` を生成する。

#### [MODIFY] [process.go (codex)](file://shared/libs/go/codingagent/codex/process.go)
* **Description**: `codex` アダプターのプロセス起動時に、同様の WSL2 プロセス委譲実行を適用します。
* **Technical Design & Logic**:
  * `BuildEnv`:
    * 環境変数 `CODEX_HOME` (値が `cfg.SessionDir`) などを構築する際、もし WSL パスであれば Linux パス形式に変換する。
  * `StartProcess`:
    * `cfg.WorkDir` に対し `codingagent.ParseWSLPath` を実行。
    * WSL パスである場合:
      1. `VerifyWSLRuntime` を実行して `codex` CLI が存在するかをチェックする。
      2. 存在する場合、`WSLCommandBuilder` を用いて `wsl.exe` による実行コマンドを組み立て、これを `cmd` とする。
    * WSL パスでない場合:
      1. 従来通り `exec.CommandContext(procCtx, "codex", args...)` を生成する。

### tests

#### [MODIFY] [agentservice_e2e_test.go](file://tests/agentservice_e2e_test.go)
* **Description**: 再現用E2Eテストケースの追加。
* **Technical Design**:
  * `TestE2E_WSLDelegation_FailReproduction` を追加。
  * `WorkDir` に WSL2 UNCパス (`\\wsl.localhost\Ubuntu\tmp\test-reproduce`) を指定してセッションを作成し、起動を試みる。
  * 修正前：Goの Windows プロセスが直接起動を試みるため、`chdir` エラーによりセッション作成が失敗することを確認（アサート）する。
  * 修正後：委譲機構により正常に WSL2 上で起動が試みられることを確認する。

---

## Step-by-Step Implementation Guide

1.  **Reproduce Problem (TDD Step)**:
    *   `tests/agentservice_e2e_test.go` に再現用テストケース `TestE2E_WSLDelegation_FailReproduction` を記述。
    *   ビルドおよびテストを実行し、テストが期待通り失敗することを確認する。
    *   コマンド:
        ```bash
        ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestE2E_WSLDelegation_FailReproduction"
        ```

2.  **Implement wsl_executor_test.go**:
    *   `shared/libs/go/codingagent/wsl_executor_test.go` を作成し、パース、パス変換、および `WSLCommandBuilder` のテストケースを記述。

3.  **Implement wsl_executor.go**:
    *   `shared/libs/go/codingagent/wsl_executor.go` を作成。
    *   単体テストが通るまでロジックを実装・調整する。
    *   コマンド:
        ```bash
        ./scripts/process/build.sh
        ```

4.  **Integrate claudecode process**:
    *   `shared/libs/go/codingagent/claudecode/process.go` の `StartProcess` と `BuildEnv` を修正し、WSL 委譲起動およびパス変換を統合する。

5.  **Integrate codex process**:
    *   `shared/libs/go/codingagent/codex/process.go` の `StartProcess` と `BuildEnv` を修正し、同様に WSL 委譲起動とパス変換を統合する。

6.  **Verify via Reproducing E2E Test**:
    *   再度再現用テストを実行し、エラーが解消され正常に委譲起動が試みられることを検証する。

---

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ビルドスクリプトを実行して全体ビルドおよび新設された単体テスト (`wsl_executor_test.go`) がパスすることを確認します。
    ```bash
    ./scripts/process/build.sh
    ```

2.  **Integration & E2E Tests**:
    E2Eテストを実行して、WSL 委譲機能および再現用テストが正常にパスすることを確認します。
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "WSLDelegation|wsl"
    ```
    *   **Log Verification**: 起動時のログにおいて、WSL2パスが検知され、`wsl.exe` による委譲起動コマンドが実行されていること、および `bwrap` サンドボックスが適用されていることをトレースログ等から確認します。

3.  **E2E Tests (新規/追加)**:
    #### [MODIFY] [agentservice_e2e_test.go](file://tests/agentservice_e2e_test.go)
    *   **テストケース**: `TestE2E_WSLDelegation_FailReproduction`
    *   **検証ポイント**: `WorkDir` に WSL2 UNCパスを指定した場合に、`chdir` エラーにならずに WSL2 上で正しく起動が試みられること。

### Test Design Self-Review
*   **網羅性**: パス判定・変換、環境変数処理、サンドボックス適用、ランタイムチェックの全要件が単体およびE2Eテストで網羅されています。
*   **証拠の十分性**: `wsl.exe` コマンド組み立てロジックは単体テストで、実際の委譲処理の成功・失敗はE2Eテストでアサートされます。
*   **迂回排除**: ダミーパスによる検証ではなく、実機に近い挙動を確認するため UNC パスを実際に設定したE2Eテストケースでアサートします。
*   **依存関係**: 単体テスト -> E2Eテストの順でボトムアップに検証を実施します。

### 総合判定プロセス
テスト実行スクリプトによるテストがすべて成功し、且つビルドエラーがないことを以て総合合格と判定します。

---

## Documentation

なし。
`prompts/specifications` フォルダおよび既存の仕様書ドキュメントに影響はありません。
