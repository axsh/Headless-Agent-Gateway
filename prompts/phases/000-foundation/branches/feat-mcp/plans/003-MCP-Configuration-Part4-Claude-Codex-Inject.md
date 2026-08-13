# 003-MCP-Configuration-Part4-Claude-Codex-Inject

> **Source Specification**: [prompts/phases/000-foundation/branches/feat-mcp/ideas/000-MCP-Configuration.md](file://prompts/phases/000-foundation/branches/feat-mcp/ideas/000-MCP-Configuration.md)
>
> **Series**: Part 4 / 4 — Claude Code / Codex への MCP ネイティブ注入 (R8)  
> **Depends on**: Part 1 (mcp_servers on SessionRecord)

## Goal Description

Client API の `mcp_servers` を Claude Code / Codex が読むネイティブ設定へ、エージェント起動前に注入する。CLI 自身が MCP クライアントとして stdio/http 接続する。Tern ホスト (Part 2) とは二重接続しない。ローカル関数の CLI 供給 (O1) は含めない。

## User Review Required

1. **Claude 注入先 (本計画で採用・仕様 R8-1 の修正)**: Claude Code は `settings.json` の `mcpServers` を読まない (公式: project は `.mcp.json`、user/local は `~/.claude.json`)。Tern では次を採用する:
   - 主: `{work_dir}/.mcp.json` に project-scope `mcpServers` をマージ書き込み。
   - Tern 管理エントリは SessionRecord 上のキー集合で置換 (replaceKeys 削除後に再書き込み)。
   - `config_dir` オーバーレイの allowlist に `.mcp.json` は追加しない。
2. **Codex 注入先 (本計画で採用)**: `{session_dir}/config.toml` (CODEX_HOME=session_dir) へ MCP をマージ。`WriteConfigTOML` (gateway) の後に実施。スキーマは実装時に CLI 版で確定。非対応フィールドは Warning + 当該サーバ skip。
3. **仕様ドキュメント追随**: 本 Part 完了時に ideas の R8-1 を `settings.json` から `.mcp.json` に修正する。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point |
| :--- | :--- |
| R8-1 Claude 注入 | claudecode MCP inject to work_dir/.mcp.json |
| R8-2 Codex 注入 | codex MCP inject to session_dir/config.toml |
| R8-3 stdio + http マッピング | mapMCPServerToClaude / ToCodex |
| R8-4 起動前・PATCH 次回反映 | adapter CreateSession / StartProcess 前 |
| R8-5 マージ方針 | Tern キー集合で置換、他キー保持 |
| R8-6 Vault 解決 | inject 前 Resolve |
| R8-7 二重接続禁止 | Claude/Codex パスでは mcp.Manager を起動しない |
| R8-8 空なら何もしない | early return |
| VS7 / VS8 | integration tests |
| D3 / D6 | ネイティブ注入必須 |

## Proposed Changes

### 共有マッピング

#### [NEW] [shared/libs/go/toolconfig/resolve.go](file://shared/libs/go/toolconfig/resolve.go)
*   **Description**: Vault 解決済み MCPServerConfig を返す。
*   **Technical Design**:

```go
type SecretResolver interface {
	Resolve(ref string) (string, error)
}

func ResolveMCPServerSecrets(cfg MCPServerConfig, r SecretResolver) (MCPServerConfig, error)
```

*   **Logic**: env/headers の各値をコピーし `vault://` のみ Resolve。r == nil かつ vault 参照ありなら error。

#### [NEW] [shared/libs/go/toolconfig/resolve_test.go](file://shared/libs/go/toolconfig/resolve_test.go)
*   **Description**: 平文通過、vault 成功/失敗。

### Claude inject

#### [NEW] [shared/libs/go/codingagent/claudecode/mcp_inject_test.go](file://shared/libs/go/codingagent/claudecode/mcp_inject_test.go)
*   **Description**: TDD。一時 work_dir の `.mcp.json` 期待値、既存非 Tern サーバ保持、stdio/http マッピング、enabled false は非書込。

#### [NEW] [shared/libs/go/codingagent/claudecode/mcp_inject.go](file://shared/libs/go/codingagent/claudecode/mcp_inject.go)
*   **Description**: `.mcp.json` への書き込み。
*   **Technical Design**:

```go
func InjectMCPServers(workDir string, replaceKeys []string, servers map[string]toolconfig.MCPServerConfig, resolver toolconfig.SecretResolver) error
```

*   **Logic**:
    1. `servers` 空でも `replaceKeys` があれば管理キー削除のみ行う。
    2. `{workDir}/.mcp.json` を読み、無ければ `{"mcpServers":{}}`。
    3. `replaceKeys` を削除後、各 server を Claude 形式でセット:

```json
{
  "mcpServers": {
    "filesystem": {
      "type": "stdio",
      "command": "npx",
      "args": ["-y", "..."],
      "env": {}
    },
    "remote-docs": {
      "type": "http",
      "url": "https://...",
      "headers": {}
    }
  }
}
```

    4. Tern `transport: http` → Claude `"type": "http"`。stdio は `"type": "stdio"` + command/args/env。
    5. 原子的書き込み (temp + rename)。秘密をログに出さない。

#### [MODIFY] [shared/libs/go/codingagent/claudecode/adapter.go](file://shared/libs/go/codingagent/claudecode/adapter.go)
*   **Description**: config_dir overlay の後、StartProcess の前に Inject。`mcp.Manager` は起動しない。

### Codex inject

#### [NEW] [shared/libs/go/codingagent/codex/mcp_inject_test.go](file://shared/libs/go/codingagent/codex/mcp_inject_test.go)
*   **Description**: TOML マージのテーブル駆動テスト。

#### [NEW] [shared/libs/go/codingagent/codex/mcp_inject.go](file://shared/libs/go/codingagent/codex/mcp_inject.go)
*   **Description**: session_dir の config.toml に MCP をマージ。
*   **Technical Design**:

```go
func InjectMCPServers(sessionDir string, replaceKeys []string, servers map[string]toolconfig.MCPServerConfig, resolver toolconfig.SecretResolver) error
```

*   **Logic**:
    1. WriteConfigTOML 後の config.toml を読む。
    2. Codex スキーマへマップ (実装時に CLI 版で確定)。例:

```toml
[mcp_servers.filesystem]
command = "npx"
args = ["-y", "@modelcontextprotocol/server-filesystem", "/workspace"]

[mcp_servers.remote-docs]
url = "https://mcp.example.com/mcp"
```

    3. headers 非対応なら Warning + skip。
    4. gateway 用セクションは破壊しない。

#### [MODIFY] [shared/libs/go/codingagent/codex/adapter.go](file://shared/libs/go/codingagent/codex/adapter.go)
*   **Description**: overlay + WriteConfigTOML の後に Inject。Manager 非起動。

#### [MODIFY] [shared/libs/go/agentservice/handler.go](file://shared/libs/go/agentservice/handler.go)
*   **Description**: SendMessage で全エージェントへ `WithMCPServers(record.MCPServers)`。Wayfinder は Part 2 Manager、CLI は Inject のみ。

### 仕様追従

#### [MODIFY] [prompts/phases/000-foundation/branches/feat-mcp/ideas/000-MCP-Configuration.md](file://prompts/phases/000-foundation/branches/feat-mcp/ideas/000-MCP-Configuration.md)
*   **Description**: R8-1 の例示を settings.json から work_dir/.mcp.json に修正。オープン論点 3 をクローズ。

### Integration / E2E

#### [NEW] [tests/common_mcp_inject_claude_test.go](file://tests/common_mcp_inject_claude_test.go)
*   **Description**: VS7。CreateSession(claudecode) + mcp_servers → work_dir/.mcp.json 内容アサーション。live フラグは既存 e2e 慣例に合わせ任意。

#### [NEW] [tests/common_mcp_inject_codex_test.go](file://tests/common_mcp_inject_codex_test.go)
*   **Description**: VS8。session_dir/config.toml に MCP セクションがあること。

#### [MODIFY] [tests/agentservice_integration_test.go](file://tests/agentservice_integration_test.go)
*   **Description**: 必要なら config_dir switch と注入の非干渉回帰。

## Step-by-Step Implementation Guide

1. Implement toolconfig.ResolveMCPServerSecrets + tests.
2. Claude mcp_inject tests (fail) → implement .mcp.json writer; wire adapter.
3. Codex mcp_inject tests (fail) → implement TOML merge; wire after WriteConfigTOML.
4. Pass MCPServers to all agents from agentservice; CLI must not start mcp.Manager.
5. Integration tests VS7/VS8.
6. Update idea spec R8-1 wording.
7. Verify Part 4 then full series regression commands below.

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**: `./scripts/process/build.sh`
2. **Integration Tests**: `./scripts/process/integration_test.sh --categories common --specify mcp_inject`
3. **E2E Tests**: `tests/common_mcp_inject_claude_test.go`, `tests/common_mcp_inject_codex_test.go` (必須は注入ファイル内容)

### Full regression after all parts

```bash
./scripts/process/build.sh
./scripts/process/integration_test.sh --categories common --specify mcp_session_api
./scripts/process/integration_test.sh --categories common --specify mcp_host
./scripts/process/integration_test.sh --categories common --specify function_calling
./scripts/process/integration_test.sh --categories common --specify mcp_inject
```

## Documentation

- docs/ReferenceManual-WebAPIs.md に注入先 (Claude=work_dir/.mcp.json、Codex=session_dir/config.toml) を追記
- 仕様 ideas R8-1 修正、および仕様「Client API 使用例」の Claude/Codex HTTP 例が ReferenceManual と一致していること
- examples/mcp-session-tools/README から Claude/Codex エージェント指定の一行例へリンクしてよい
