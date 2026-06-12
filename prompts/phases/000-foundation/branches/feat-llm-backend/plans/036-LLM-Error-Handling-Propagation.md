# 036-LLM-Error-Handling-Propagation

> **Source Specification**: [026-LLM-Error-Handling-Propagation.md](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/prompts/phases/000-foundation/branches/feat-llm-backend/ideas/026-LLM-Error-Handling-Propagation.md)

## Goal Description

LLM側 (Google Gemini API 等) で 429 (Rate Limit Exceeded) や 500 等のエラーが発生した際、その異常状態が `cawa-client` 側に伝わらず正常終了 (`completed` ステータスかつ exit code 0) になってしまう課題を解決します。
サーバー側のセッションレコードにエラー状態およびメッセージを記録し、`cawa-client` 側で SSE エラーイベントやエラーステータスを検知して非ゼロ (exit code 1) で終了するようにします。

## User Review Required

None.

## Requirement Traceability

> **Traceability Check**:
> 仕様書(Specification)の要件・決定事項をリストアップし、この計画書のどこで対応するかをマッピングしてください。

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| 1. セッションステータスのエラー状態化 (エラー発生時はStatusErrorに更新) | Proposed Changes > [handler.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/shared/libs/go/agentservice/handler.go) |
| 2. エラーメッセージの保存 (SessionRecordにErrorフィールドを追加) | Proposed Changes > [session_store.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/shared/libs/go/codingagent/session_store.go) |
| 3. クライアント側での異常検知と非ゼロ終了 (streamSSEのerror受信時およびsession status="error"時にexit 1) | Proposed Changes > [main.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/examples/cawa-client/main.go) |
| 4. 統合テストでの検証 (Error発生時のStatusError遷移とエラーメッセージ記録の検証) | Proposed Changes > [agentservice_integration_test.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/tests/agentservice_integration_test.go) |

---

## Proposed Changes

### 1. Backend (Go)

#### [MODIFY] [session_store.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/shared/libs/go/codingagent/session_store.go)
*   **Description**: `SessionRecord` 構造体に `Error` フィールドを追加します。
*   **Technical Design**:
    *   ```go
        type SessionRecord struct {
        	ID             string    `json:"id"`
        	AgentName      string    `json:"agent_name"`
        	Model          string    `json:"model"`
        	Status         string    `json:"status"`
        	Error          string    `json:"error,omitempty"` // 追加: エラーメッセージ
        	WorkDir        string    `json:"work_dir"`
        	AgentSessionID string    `json:"agent_session_id"`
        	SessionDir     string    `json:"session_dir"`
        	CreatedAt      time.Time `json:"created_at"`
        	UpdatedAt      time.Time `json:"updated_at"`
        }
        ```

#### [MODIFY] [handler.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/shared/libs/go/agentservice/handler.go)
*   **Description**: `streamSSE` および `respondJSON` にて `EventError` の発生を検知し、セッションレコードのステータスを `StatusError` ("error") に、かつ `Error` フィールドにエラー内容を設定して保存します。
*   **Technical Design**:
    *   `streamSSE` / `respondJSON` 関数内で `hasError bool` と `errorMsg string` をローカル変数として定義。
    *   チャネル `ch` のループ処理内で、`ev.Type == codingagent.EventError` の場合に `hasError = true` および `errorMsg = ev.Content` (空ならデフォルトメッセージ) を設定。
    *   ループ終了後のセッションレコード更新ロジックを以下のように変更:
        ```go
        if record, err := s.sessions.Get(sessionID); err == nil {
        	if hasError {
        		record.Status = codingagent.StatusError
        		if errorMsg != "" {
        			record.Error = errorMsg
        		} else {
        			record.Error = "unknown error occurred during execution"
        		}
        	} else {
        		record.Status = codingagent.StatusCompleted
        	}
        	s.sessions.Update(record)
        }
        ```

### 2. Frontend CLI (Go)

#### [MODIFY] [main.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/examples/cawa-client/main.go)
*   **Description**: `streamSSE` 関数のシグネチャを `error` を返すように変更し、エラー受信時に即座にエラーを返します。`cmdRun` / `cmdLogs` および `cmdSessionByID` でエラー検知時に `os.Exit(1)` します。
*   **Technical Design**:
    *   ```go
        // streamSSE reads SSE data lines and prints events to stdout.
        // Returns error if an error event is received.
        func streamSSE(body io.Reader) error {
        	scanner := bufio.NewScanner(body)
        	var lastError error
        	for scanner.Scan() {
        		line := scanner.Text()
        		// ...
        		var ev struct {
        			Type     string `json:"type"`
        			Content  string `json:"content"`
        			ToolName string `json:"tool_name,omitempty"`
        		}
        		// ...
        		switch ev.Type {
        		// ...
        		case "error":
        			log.Error("SSE error event received", "error", ev.Content)
        			fmt.Fprintf(os.Stderr, "\n[Error] %s\n", ev.Content)
        			lastError = fmt.Errorf("%s", ev.Content)
        		// ...
        		}
        	}
        	return lastError
        }
        ```
    *   `cmdRun` および `cmdLogs` で `streamSSE` の戻り値を確認し、非nilなら `os.Exit(1)`。
    *   `cmdSessionByID` でセッション情報をパースした際、`status` が `"error"` の場合は標準エラー出力にエラー情報を出力して `os.Exit(1)`。
        ```go
        if status, ok := session["status"].(string); ok && status == "error" {
        	errMsg := "unknown error"
        	if msg, ok := session["error"].(string); ok && msg != "" {
        		errMsg = msg
        	}
        	fmt.Fprintf(os.Stderr, "Session failed with error: %s\n", errMsg)
        	os.Exit(1)
        }
        ```

### 3. Verification

#### [MODIFY] [agentservice_integration_test.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/tests/agentservice_integration_test.go)
*   **Description**: 新しい統合テストケース `TestCawaClientErrorPropagation` を追加し、`cawa-client` 経由でのエラー発生時の挙動を検証します。
*   **Technical Design**:
    *   `go build` により `cawa-client` バイナリを一時ビルド。
    *   `errorMockAgent` を登録した `agentservice` テストサーバーを起動。
    *   `cawa-client run` を実行し、終了コードが `1` であること、かつ標準エラー出力にエラーメッセージが含まれることを検証。
    *   `cawa-client session --id {id}` を実行し、終了コードが `1` であること、かつ標準エラー出力にエラーメッセージが含まれることを検証。

---

## Step-by-Step Implementation Guide

1.  **[Prepare Store]**:
    *   [session_store.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/shared/libs/go/codingagent/session_store.go) を編集し、`SessionRecord` に `Error` フィールドを追加します。
2.  **[Server Changes]**:
    *   [handler.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/shared/libs/go/agentservice/handler.go) を編集し、`streamSSE` および `respondJSON` で `codingagent.EventError` を検知した際、ステータスを `codingagent.StatusError` にし、エラー内容を `record.Error` に設定するように実装します。
3.  **[Client Changes]**:
    *   [main.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/examples/cawa-client/main.go) を編集し、`streamSSE` の戻り値を `error` に変更、およびエラー発生時の非ゼロ終了ロジックを実装します。
4.  **[Test Creation]**:
    *   [agentservice_integration_test.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/tests/agentservice_integration_test.go) に `TestCawaClientErrorPropagation` テストケースを追加します。
5.  **[Verify & Verdict]**:
    *   ビルドおよびテストスクリプトを実行し、正常にパスすることを確認します。

---

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ビルドスクリプトを実行します。
    ```bash
    ./scripts/process/build.sh
    ```

2.  **Integration Tests**:
    追加した統合テストを実行します。
    ```bash
    ./scripts/process/integration_test.sh --specify "TestCawaClientErrorPropagation"
    ```
    *   **Log Verification**: テスト実行時の `syslog` にて、`cawa-client` やサーバー側でのエラーハンドリングログが正しく出力されていることを確認します。
    *   また、すべての統合テストが正常にパスすることを確認します:
    ```bash
    ./scripts/process/integration_test.sh
    ```

3.  **E2E Tests (新規/追加)**:
    E2Eテストレベルの検証として、`cawa-client` 自体の終了コードや挙動を検証するため、`tests/agentservice_integration_test.go` に `TestCawaClientErrorPropagation` を追加します。このテストは mock agent のエラーが `cawa-client` に正しく伝播され、実際に OS プロセスとして非ゼロコードで終了し、かつ `session` コマンドでも非ゼロ終了することをE2Eと同等レベルで検証するものです。

    #### [MODIFY] [agentservice_integration_test.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/tests/agentservice_integration_test.go)
    *   **テストケース**: `TestCawaClientErrorPropagation`
    *   **検証ポイント**: 
        1. `cawa-client run` が exit code 1 で終了すること。
        2. `cawa-client session` が exit code 1 で終了すること。
        3. 標準エラー出力に期待するエラーメッセージ（"claude exited with code 1: authentication failed"）が含まれること。

---

## Test Design Self-Review & Post-Test Verdict

### テスト項目設計のセルフレビュー (Testing Rules §11.4)

1.  **網羅性の検証**: 
    本計画で追加する `TestCawaClientErrorPropagation` が成功すれば、エラー発生時のステータス更新・エラー文言記録・`cawa-client` の exit code 1 での終了およびエラー出力、という全要件が実プロセス動作レベルで完全に検証されます。
2.  **証拠の十分性**: 
    テストコード内で単に `err != nil` を見るだけでなく、標準エラー出力の文字列検証および `session` API 経由での `StatusError` 状態への遷移まで確認しているため、証拠は十分です。
3.  **迂回・抜け道の排除**: 
    モックサーバーではなく、実際に `cawa-client` のバイナリを `go build` して別プロセスとして実行し HTTP/SSE で通信させるため、実際の使用環境と全く同じコードパスを通ることを保証します。
4.  **依存関係の整合性**: 
    下層の `SessionRecord` フィールド追加、および `handler.go` でのイベントパース処理が正しく動作していることを前提に、最上層の `cawa-client` の挙動を検証するボトムアップな構造になっています。

### 総合判定プロセスの計画 (Testing Rules §12)

全テスト完了後、以下の項目を網羅的に確認し、`walkthrough.md` に結果を記録した上で判定を下します。
*   スキップされたテストの有無
*   部分的なエラーの見落とし (テストログ内の `WARN`/`ERROR` 等の確認)
*   迂回処理による偽成功の排除
*   テスト間の依存・順序問題の確認

---

## Documentation

None.
