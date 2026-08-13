# mcp-session-tools

Demonstrates Client API configuration of `mcp_servers` and `functions` using `client/v1`.

## Prerequisites

- A running Tern agent service (default `http://localhost:3100`)

## Run

```bash
go run ./examples/mcp-session-tools --server http://localhost:3100 --work-dir /path/to/workspace
```

## HTTP equivalents

See `docs/ReferenceManual-WebAPIs.md` sections Create Session / Get Session / Update Session for curl-ready JSON examples.
