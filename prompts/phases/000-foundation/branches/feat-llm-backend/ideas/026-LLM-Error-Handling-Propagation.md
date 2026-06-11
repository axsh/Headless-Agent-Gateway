# 仕様書: LLM側エラーの伝播とクライアント側でのエラー検知 (026-LLM-Error-Handling-Propagation)

## 背景 (Background)

現在の Headless-Agent-Gateway (HAG) では、マルチターン対話中に LLM 側 (Google Gemini API など) で 429 (Rate Limit Exceeded) や 500 等のエラーが発生して Claude CLI サブプロセスが異常終了した場合でも、`agentservice` 側は無条件にセッションのステータスを `completed` (完了) に更新してしまいます。

このため、`cawa-client` (および `claude` CLI) 経由で実行したクライアント側からは、異常終了が発生したにもかかわらず常に正常終了したように見え、どう終了したのかが分かるログもセッション情報に残らないという課題があります。

## 要件 (Requirements)

1. **セッションステータスのエラー状態化**:
   * セッション実行中にエラーイベント (`codingagent.EventError`) が発生した場合は、セッションステータスを `completed` ではなく `error` として更新・保存すること。
2. **エラーメッセージの保存**:
   * セッションの永続化レコード (`SessionRecord`) に、発生したエラーメッセージを格納する `error` フィールドを追加すること。
   * ステータス取得 API (`GET /api/v1/sessions/{id}`) から返却される JSON にこのエラーメッセージを含めること。
3. **クライアント側での異常検知と終了ステータス**:
   * `cawa-client run` または `cawa-client logs` コマンドで対話・ログのストリーミングを行っている際、ストリーム中にエラーイベントを受信した場合はプロセスが非ゼロの終了コード (`os.Exit(1)`) で終了すること。
   * `cawa-client session` コマンドでセッション情報を表示した際、エラー情報があれば画面に出力すること。

## 実現方針 (Implementation Approach)

### 1. セッションレコードの拡張
* **ファイル**: [session_store.go](file:///shared/libs/go/codingagent/session_store.go)
* `SessionRecord` 構造体に `Error` フィールドを追加します。
  ```go
  type SessionRecord struct {
  	ID             string    `json:"id"`
  	AgentName      string    `json:"agent_name"`
  	Model          string    `json:"model"`
  	Status         string    `json:"status"`
  	Error          string    `json:"error,omitempty"` // エラーメッセージ用フィールド
  	WorkDir        string    `json:"work_dir"`
  	AgentSessionID string    `json:"agent_session_id"`
  	SessionDir     string    `json:"session_dir"`
  	CreatedAt      time.Time `json:"created_at"`
  	UpdatedAt      time.Time `json:"updated_at"`
  }
  ```

### 2. サーバー側のエラー検知とステータス更新
* **ファイル**: [handler.go](file:///shared/libs/go/agentservice/handler.go)
* `streamSSE` および `respondJSON` 関数において、イベントチャネル `ch` を読み取るループ中で `ev.Type == codingagent.EventError` のイベントが発生したかを追跡します。
* ループ終了後、エラーが発生していた場合は `record.Status = codingagent.StatusError` および `record.Error = ev.Content` としてセッションレコードを更新します。

### 3. クライアント側のエラー伝播と非ゼロ終了
* **ファイル**: [main.go](file:///examples/cawa-client/main.go)
* `streamSSE` 関数のシグネチャを `streamSSE(body io.Reader) error` に変更し、`ev.Type == "error"` の受信時にエラーを返すようにします。
* `cmdRun` および `cmdLogs` にて `streamSSE` からエラーが返された場合、プロセスを `os.Exit(1)` で終了させます。
* `cmdSessionByID` 関数にてセッション情報を取得した際、`status` が `"error"` もしくは `error` フィールドにメッセージが含まれている場合は、標準エラー出力にエラーメッセージを出力し、非ゼロ終了するようにします。

## 検証シナリオ (Verification Scenarios)

1. **エラー発生時のステータス確認**:
   * 不正な API キーの設定、あるいはレート制限 (429) の発生などにより意図的に LLM 呼び出しエラーを発生させます。
   * `cawa-client run` を実行し、エラー発生時に `cawa-client` が非ゼロコードで終了することを確認します。
   * `cawa-client session --id {id}` を実行して、取得されたセッション情報が以下のようになっていることを確認します。
     ```json
     {
       "id": "...",
       "status": "error",
       "error": "upstream returned 429"
     }
     ```

2. **正常実行時のステータス確認**:
   * 正常に処理が完了するプロンプトを送信し、セッションが `"status": "completed"` で終了し、`error` フィールドが含まれない（あるいは空文字列である）ことを確認します。

## テスト項目 (Testing for the Requirements)

* **統合テストでの検証**:
  * [agentservice_integration_test.go](file:///tests/agentservice_integration_test.go) に、エラーを発生させたセッションが最終的に `StatusError` に遷移し、エラーメッセージが `SessionRecord` に記録されていることを確認するテストケースを追加します。
  * `integration_test.sh` を使用して、追加したテストケースが自動検証で正常にパスすることを確認します。
