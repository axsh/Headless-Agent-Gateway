# Issue #26 Verification Traceability

This document maps GitHub [Issue #26](https://github.com/axsh/arctic-tern/issues/26) failure modes to fixes (000) and verification tests (001). When all R16 gate commands pass, Issue #26 is considered **fully resolved**.

Related: [SSE Chunk Protocol](./sse-chunk-protocol.md)

## Failure Mode Traceability

| Issue #26 symptom | 000 fix | 001 verification | Test(s) |
|-------------------|---------|------------------|---------|
| FM1: `bufio.Scanner: token too long` on oversized SSE | `tool_result_part` chunking + client reassembly | R6, R7, R10, R14 | `TestCodexE2E_ClientV1_MaxTruncatedToolOutputTerminalEvent`, `TestCodexE2E_LegacyClient_MaxTruncatedToolOutputTerminalEvent`, `TestSplitStreamEventForSSE_*`, `TestSSEConsumerReference_DefaultScannerReadsChunkedStream` |
| FM2: ~120s `data:` silence / downstream timeout | Chunking keeps `data:` events flowing | R9, R11 | `TestCodexE2E_ClientV1_NoDataSilenceDuringLargeToolTurn`, `TestCodexE2E_ClientV1_RipgrepLikeMultiLineOutput` |
| L2: session `status` stuck `active` | Early status on terminal events | R8, R13, 000 R1b | `TestStreamSSERelay_EarlyStatusUpdate`, `TestStreamSSERelay_DisconnectUpdatesStatus`, `TestRespondJSONRelay_EarlyStatusOnTerminalEvent`, `TestCodexE2E_SessionStatusOnTerminalEvent`, `TestCodexE2E_ClientV1_DisconnectAfterTerminalEventUpdatesStatus` |
| Production: ripgrep-heavy Codex turn | Same chunking + status fixes | R11 | `TestCodexE2E_ClientV1_RipgrepLikeMultiLineOutput`, `TestCodexE2E_RealCLI_ClientV1_LargeSearchOutput` |

## Definition of Complete Resolution

All of the following must be true (from 001 specification):

1. **FM1**: Up to 256 KiB `tool_result` works with default 64 KiB scanner on official Go clients (`client/v1`, legacy `client`).
2. **FM2**: Large tool turns complete without prolonged `data:` silence (R9 time bounds).
3. **L2**: Session status becomes `completed`/`error` promptly; no `active` zombie after disconnect.
4. **Production-like**: Ripgrep-style fake E2E passes; real Codex E2E skips when CLI/API unavailable.
5. **Protocol**: Every SSE `data:` line JSON is strictly under 64 KiB (R10 property tests).

## R11b Skip Alternative

When `TestCodexE2E_RealCLI_ClientV1_LargeSearchOutput` skips (no `codex` CLI, 404/upstream API errors), the following passing tests provide equivalent coverage:

- `TestCodexE2E_ClientV1_MaxTruncatedToolOutputTerminalEvent` (256 KiB end-to-end)
- `TestCodexE2E_ClientV1_NoDataSilenceDuringLargeToolTurn` (FM2 regression)
- `TestCodexE2E_ClientV1_RipgrepLikeMultiLineOutput` (multi-line search-shaped output)

## R16 Completion Gate

Run all commands; each must exit 0:

```bash
./scripts/process/build.sh
./scripts/process/integration_test.sh --specify "TestCodexE2E"
./scripts/process/integration_test.sh --specify "TestCodexScanner"
./scripts/process/integration_test.sh --specify "TestSSEConsumer"
./scripts/process/integration_test.sh --specify "TestSplitStreamEvent"
./scripts/process/integration_test.sh --specify "TestStreamSSERelay"
./scripts/process/integration_test.sh --specify "TestRespondJSONRelay"
```

Note: `TestSplitStreamEvent`, `TestStreamSSERelay`, and `TestRespondJSONRelay` unit tests run via `build.sh` in `shared/libs/go/codingagent` and `shared/libs/go/agentservice`. Integration filters may report "no tests to run" for those packages; unit coverage is in the build step.

## 000 Regression Tests (must remain PASS)

| Test | Purpose |
|------|---------|
| `TestCodexE2E_ClientV1_LargeToolOutputTerminalEvent` | 65537 B client/v1 repro |
| `TestCodexE2E_LargeToolOutputTerminalEvent` | L1 server parser path |
| `TestCodexScannerIntegration_LargeOutputMissingEventResult` | L1 agent scanner |
| `TestHandleSendMessage_SSEChunkedToolResult` | Handler SSE chunking |
