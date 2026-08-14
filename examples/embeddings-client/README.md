# Embeddings Client Example

Minimal example that uses Tern's Embeddings Client API (`ListEmbeddingModels` and `CreateEmbedding`).

This path **does not** create a Coding Agent session. AgentService proxies the request to LLMGP (`POST /v1/embeddings`).

## Prerequisites

1. Start a Tern server (for example via `examples/minimal-server` or `features/tern`).
2. Register at least one model with `mode: embedding` in `model_profiles.yaml` (see `settings/example/model_profiles.yaml`).
3. Provide provider credentials when required (OpenAI / Google via vault).

## Usage

```bash
go run . [server-url] [model-name]
```

Examples:

```bash
go run .
go run . http://localhost:3100 text-embedding-3-small
```
