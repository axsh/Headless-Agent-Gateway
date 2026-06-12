# Wayfinder Agent - ガードレールおよびLLMGP統合仕様書

## 1. 背景 (Background)

プロトタイプ `vv-prototype` では、シェル演算子（`|`, `&`, `;` 等）を一律で遮断する極端なセキュリティガードレールが設定されていましたが、これはパイプライン（例: `go test ./... | grep Fail`）やリダイレクトといった開発中の正当なコマンド実行まで阻害していました。
安全性を確保しつつ利便性を損なわないように、シェル演算子の制限を排除し、アクセスパス制限や権限境界の検証を主軸とした現実的なガードレールに再設計します。
また、LLMへの直接のアクセス（個別SDKの呼び出し）を廃止し、Ternの標準的なLLMGPクライアント（Bifrost）を介したポータブルなアクセス層へ統合します。

## 2. 要件 (Requirements)

### 必須要件 (Mandatory Requirements)

#### ガードレール関連要件
- **シェル演算子ブラックリストの廃止**: コマンド実行時の `;`, `&&`, `||`, `|`, `>`, `<` などのシェル連結・リダイレクト演算子の使用を一律制限せず、許可すること。
- **危険コマンドのブロック**: 破壊的または特権が必要な特定のコマンド（`Su`, `Sudo`, `Format`, `Mkfs`, `Shutdown`, `Reboot`, `Passwd`, `Useradd`, `Userdel` など）は引き続き厳しく制限・ブロックすること。
- **パス境界検証 (ValidatePath)**: エージェントが読み書きする全てのファイルパスが、設定された `WorkDir` (作業ディレクトリ) 内に収まっていることを事前に検証し、パス階層の上位（`../../` など）やシステム重要ディレクトリへのトラバーサル・シンボリックリンク脱出を拒否すること。すべてのパス解決は `WorkDir` を基準（ベースパス）として絶対パス化した上で検証されること。
- **コマンドの実行ディレクトリ**: コマンド実行ツール（`execute_command`）で外部プロセスを起動する際、プロセスの実行カレントディレクトリ（`Cmd.Dir`）には明示的に `WorkDir` を指定して起動し、意図しないディレクトリでのコマンド実行を防止すること。
- **削除許可リスト (Deletion Permission List) の永続化**:
  - エージェントがツール実行を通じて自ら作成したファイル・ディレクトリ（`FileCreationTracker`）、および起動したバックグラウンドプロセス（`CommandExecutionContext` - PID、コマンド名、引数）は、セッション状態ファイルにシリアライズして永続化すること。
  - これにより、シングルショット実行間で「自己生成オブジェクト」の情報が引き継がれ、後続の実行でも安全な削除・変更操作が可能となる。
  - 削除許可リストはセッション状態（`SessionState`）の一部としてJSON形式で保存される。
- **適合パスパターン (Allowed Path Patterns)**:
  - `rm` / `chmod` / ファイル削除操作において、削除許可リスト (`FileCreationTracker`) による自己生成チェックに加えて、**適合パスパターンによるマッチング**を許可判定に使用すること。
  - 適合パスパターンは正規表現（`regexp`）の配列として複数指定でき、対象ファイルの絶対パスがいずれかのパターンにマッチした場合は、削除許可リストに登録されていなくても削除・変更を許可する。
  - デフォルトでは `WorkDir` 配下のすべてのパスにマッチするパターン（例: `^<WorkDirの正規表現エスケープ>/`）が自動的に設定される。
  - 将来の拡張のため、設定（`AgentConfig` 等）で追加の適合パスパターンを指定可能にすること。
- **所有権・自己生成オブジェクト制限**:
  - `rm` や `chmod` は、以下のいずれかの条件を満たすファイルのみに許可すること:
    1. エージェント自身が作成したファイル・ディレクトリ（削除許可リスト `FileCreationTracker` に記録されているもの）
    2. 適合パスパターン（Allowed Path Patterns）にマッチするパス
    3. 現在の実行ユーザーが所有しているファイル
  - `chown` は、現在の実行ユーザー以外の他ユーザー（特に `root`）への所有権変更を拒否すること。
- **セッション復旧時のトラッカー整合性検証**:
  - セッションファイルからトラッカー情報をデシリアライズして復元する際、記録されたエントリの整合性を以下の手順で検証すること。
  - **ファイル/ディレクトリの検証**: `FileCreationTracker` に記録された各パスについて `os.Stat` でファイル/ディレクトリの存在を確認する。存在しない場合は、削除許可リストから当該エントリを除外する。
  - **プロセスの検証**: `CommandExecutionContext` に記録された各PIDについて、`os.FindProcess` (および可能であればプロセス名の照合) で当該プロセスが存在し、記録されたコマンド名と一致するかを確認する。一致しない場合（プロセスが終了済み、または別のプロセスに再利用されている場合）は、当該エントリを除外する。
  - これにより、前回のセッション終了後にファイルが手動削除されたケースや、PIDが別プロセスに再割当されたケースで誤った許可判定を下すことを防止する。

#### LLMGP/Bifrost統合関連要件
- **直接のAPIキー・個別SDK依存の排除**: `anthropic-sdk-go` などの個別LLMプロバイダSDKの直接呼出を廃止すること。
- **LLMGP/Bifrostクライアントの呼び出し**: LLMへの要求（テキスト生成、Tool Calling）をTernの `llmgp` パッケージまたはBifrost API経由で行うこと。
- **論理モデル名によるモデル指定**: クライアントから指定された論理モデル名（例: `"claude"`, `"gemini"`, `"openai"`) をLLMGPに引き渡し、背後の実際のエンドポイント接続やキーの解決はLLMGPに委ねること。

## 3. 実現方針 (Implementation Approach)

### ガードレール処理の改善方針

コマンド実行ハンドラ (`executeCommandHandler`) でのセキュリティチェックフローを以下のように変更します。

```go
func executeCommandHandler(ctx context.Context, workingDir string, input map[string]any,
    tracker *FileTracker, allowedPatterns []*regexp.Regexp) (string, error) {
    commandLine := input["command_line"].(string)
    
    // 1. パース処理
    command, args, err := ParseCommandLine(commandLine)
    if err != nil {
        return "", err
    }
    
    // 2. 危険なシステムコマンドのブラックリストチェック (シェル演算子はスルー)
    if isSystemBlockedCommand(command) {
        return "", fmt.Errorf("permission denied: blocked command: %s", command)
    }
    
    // 3. 所有権 / 適合パス / 動的権限チェック
    if command == "rm" || command == "chmod" {
        for _, path := range extractPaths(args) {
            absPath := resolveAbs(workingDir, path)
            // 判定順序:
            // (a) 削除許可リスト(FileTracker)に記録されている -> 許可
            // (b) 適合パスパターンにマッチする -> 許可
            // (c) 現在のOSユーザーが所有している -> 許可
            // (d) いずれにも該当しない -> 拒否
            if !tracker.IsTrackedFile(absPath) &&
               !matchesAllowedPattern(absPath, allowedPatterns) &&
               !isOwnedByCurrentUser(absPath) {
                return "", fmt.Errorf("permission denied: cannot modify/remove untracked or unowned path: %s", path)
            }
        }
    }
    
    // 4. chownのチェック
    if command == "chown" {
        if isTargetingRootOrOtherUser(args) {
            return "", fmt.Errorf("permission denied: chown to root or other users is not allowed")
        }
    }
    
    // 5. 実行
    return executeCommandRaw(workingDir, command, args)
}

// matchesAllowedPattern checks if the absolute path matches any allowed path pattern.
func matchesAllowedPattern(absPath string, patterns []*regexp.Regexp) bool {
    for _, p := range patterns {
        if p.MatchString(absPath) {
            return true
        }
    }
    return false
}
```

### 適合パスパターンの設定構造

```go
// AllowedPathConfig holds path pattern configuration for deletion permission.
type AllowedPathConfig struct {
    // Patterns is a list of regular expressions.
    // Files matching any pattern are allowed for rm/chmod operations.
    // Default: ["^<escaped WorkDir>/"] (all files under WorkDir)
    Patterns []string `json:"patterns"`
}

// CompilePatterns compiles string patterns into regexp objects.
// Invalid patterns are logged and skipped.
func (c *AllowedPathConfig) CompilePatterns() []*regexp.Regexp {
    compiled := make([]*regexp.Regexp, 0, len(c.Patterns))
    for _, p := range c.Patterns {
        re, err := regexp.Compile(p)
        if err != nil {
            continue // log warning and skip invalid pattern
        }
        compiled = append(compiled, re)
    }
    return compiled
}
```

### セッション復旧時のトラッカー整合性検証

セッションファイルの復元時に実行する整合性チェック処理の擬似コード:

```go
// ValidateTrackerState verifies the integrity of deserialized tracker data.
// Entries that no longer match the actual system state are removed.
func ValidateTrackerState(state *SessionState) {
    // 1. ファイル/ディレクトリの存在検証
    validFiles := make([]TrackedFile, 0, len(state.CreatedFiles))
    for _, f := range state.CreatedFiles {
        if _, err := os.Stat(f.Path); err == nil {
            validFiles = append(validFiles, f)
        }
        // 存在しないエントリは削除許可リストから除外
    }
    state.CreatedFiles = validFiles

    // 2. プロセスの存在・名称一致検証
    validProcs := make([]TrackedProcess, 0, len(state.RunningProcesses))
    for _, p := range state.RunningProcesses {
        if verifyProcessAlive(p.PID, p.Command) {
            validProcs = append(validProcs, p)
        }
        // PIDが別プロセスに再割当されている場合は除外
    }
    state.RunningProcesses = validProcs
}

// verifyProcessAlive checks if a process with the given PID exists
// and its command name matches the recorded name.
func verifyProcessAlive(pid int, expectedCommand string) bool {
    proc, err := os.FindProcess(pid)
    if err != nil {
        return false
    }
    // OS固有のプロセス名取得 (e.g. /proc/[pid]/comm on Linux)
    actualCommand := getProcessName(pid)
    return actualCommand == expectedCommand
}
```

### LLMGP/Bifrost統合インターフェース

エージェントコアがLLMにアクセスするインターフェースを以下のように定義します。

```go
package llm

import "context"

type ChatMessage struct {
	Role    string // "user", "assistant", "tool"
	Content string
}

type LLMGPClient interface {
	// GenerateMessage 指定された論理モデル名でメッセージを送信し、Tool Callingまたはテキストを返却
	GenerateMessage(ctx context.Context, logicalModel string, messages []ChatMessage, tools []ToolDefinition) (*LLMResponse, error)
}
```

## 4. 検証シナリオ (Verification Scenarios)

1.  **シェルパイプライン動作検証**:
    - エージェントに `execute_command` ツールを使って `git status | grep "modified"` を実行させる。
    - パイプライン演算子がブロックされず、Gitの変更ファイルのみが正常に出力されることを確認。
2.  **危険コマンド遮断検証**:
    - エージェントに `sudo apt-get update` や `rm -rf /` に相当するコマンド実行を指示。
    - ブラックリストまたは危険なコマンドパターン検知により、即座にエラーが返され、実行が拒否されることを確認。
3.  **ValidatePath境界外エラー検証**:
    - エージェントにワーキングディレクトリ外（例: `../../../../etc/passwd`）のファイル読み込みを指示。
    - `ValidatePath` によるパス境界チェックによって処理が拒否され、エラーが出力されることを確認。
4.  **LLMGPモックによる疎通検証**:
    - LLMGPクライアントをモック化し、指定された `"claude"` 論理モデル名が正しくBifrost APIリクエストにマッピングされ、送信されることを確認。
5.  **削除許可リストの永続化と復旧検証**:
    - セッション1で `write_file` ツールによりファイルを作成 -> セッション終了 -> セッションファイルの `created_files` にパスが記録されていることを確認。
    - セッション2で同じセッションIDで復旧 -> 作成済みファイルの `rm` が削除許可リスト経由で許可されることを確認。
    - 手動でファイルを削除した状態でセッションを復旧 -> 存在しないパスが削除許可リストから自動除外されることを確認。
6.  **適合パスパターン検証**:
    - `WorkDir` 配下のファイルに対する `rm` -> デフォルトの適合パスパターンにマッチして許可されることを確認。
    - `WorkDir` 外のファイルに対する `rm` -> パターン不一致かつ削除許可リストにも未登録 -> 拒否されることを確認。
    - カスタム正規表現パターン（例: `/tmp/test-.*`）を追加設定 -> マッチするパスへの操作が許可されることを確認。
7.  **セッション復旧時のPID整合性検証**:
    - バックグラウンドプロセスを起動した状態でセッションファイルに記録 -> プロセスを手動で終了 -> セッション復旧 -> 該当PIDが `RunningProcesses` から除外されていることを確認。

## 5. テスト項目 (Testing for the Requirements)

### 5.1 単体テスト (Unit Tests)
- `TestPathValidationOutsideBoundary`:
  ワーキングディレクトリ外の絶対パス、相対パスを与えて `ValidatePath` が適切にエラーを返すか検証。
- `TestAllowedShellOperators`:
  パイプやリダイレクトを含むコマンドが `executeCommandHandler` のセキュリティチェックを正常に通過することをテスト。
- `TestBlockedSystemCommands`:
  `sudo` や `shutdown` を含む入力に対して、セキュリティチェックが確実にエラーを返すことを検証。
- `TestAllowedPathPatterns`:
  正規表現パターンのコンパイル、WorkDir配下パスのマッチ成功、WorkDir外パスのマッチ失敗、複数パターンでのマッチをテーブル駆動テストで検証。
- `TestDeletionPermissionWithAllowedPatterns`:
  適合パスパターンにマッチするファイルへの `rm` が許可され、マッチしないファイルへの `rm` が拒否されることを検証。
- `TestTrackerValidation_FileRemoved`:
  `FileCreationTracker` に記録されたファイルを手動削除した状態で `ValidateTrackerState` を呼び出し、該当エントリが除外されることを検証。
- `TestTrackerValidation_ProcessReassigned`:
  存在しない、または別のプロセスに再割当されたPIDを `CommandExecutionContext` に記録した状態で `ValidateTrackerState` を呼び出し、該当エントリが除外されることを検証。

### 5.2 統合テスト (Integration Tests)
`integration_test.sh` を用いて、Ternの `llm` カテゴリとしてテストを実行：
```bash
./scripts/process/integration_test.sh --categories llm --specify tests/integration/agent/guardrail_llmgp_test.go
```
- **LLMGP/Bifrost 統合疎通テスト**:
  ローカルのLLMGPクライアントを使用してモデルへ接続し、Tool Callingを伴う思考ループが正常に開始・終了することを検証します。
