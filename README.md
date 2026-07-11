<p align="center">
  <img src="docs/resources/images/tern_logo_circle_transparent.png" alt="Tern Logo" width="200">
</p>

# Tern

> Fly with the best agent. Anywhere. Anytime.

Tern is an open-source framework for running Coding Agents and Language Models through a common interoperability layer.

The project is built around a simple belief:

**The AI ecosystem evolves too quickly to commit to a single agent forever.**

New Coding Agents appear regularly.
New Language Models continuously redefine what is possible.
Local inference is becoming increasingly practical.
Deployment requirements vary across organizations and projects.

Developers should be able to adapt to these changes without rebuilding their workflows each time.

Tern aims to make that possible by enabling portable context, agent interoperability, and model interoperability.

---

## Why Tern?

The Arctic Tern is known for making one of the longest migrations on Earth.

Rather than remaining in one place, it continuously moves toward better conditions.

We believe software tooling should be able to evolve in a similar way.

A developer may prefer Claude Code today, Codex tomorrow, and a completely different agent next year. The same is true for Language Models, where capabilities, pricing, privacy requirements, and deployment options continue to change.

Tern is designed to help developers adapt to these changes while preserving the most important asset in an AI workflow: context.

---

## What Tern Provides

Tern focuses on three capabilities.

### Context Portability

Work performed in one environment should not become inaccessible when moving to another.

Tern is being designed around portable context that can move with the developer as tools evolve.

### Agent Interoperability

Coding Agents should be interchangeable.

Applications should not need to be rewritten whenever a new Coding Agent becomes available.

### Model Interoperability

Language Models should be interchangeable.

Developers should be free to choose between hosted services, private deployments, and local inference depending on their requirements.

---

## Example

The `examples/` directory contains working samples that demonstrate Tern's core concepts.

### Server ([examples/minimal-server](examples/minimal-server/main.go))

All built-in coding agents and LLM providers are auto-registered by the `server` package.
Starting a server requires only a config path:

```go
srv, err := server.New(server.WithConfigPath("config.yaml"))
// Optional: enable specific API versions only:
// srv, err := server.New(server.WithConfigPath("config.yaml"), server.WithEnableVersion(1))
srv.Launch(ctx)
defer srv.Shutdown(ctx)
```

### Client Examples

Tern client libraries ([examples/minimal-client](examples/minimal-client/main.go), [examples/multimodal-client](examples/multimodal-client/main.go)) simplify session interaction.

For long-running SSE streams, disable the HTTP client timeout:

```go
c := client.New("http://localhost:3100", client.WithNoTimeout())
```

### Vault API Examples

Tern also provides a reusable Vault API package at [`vault/`](vault), designed for tool authors who want Vault functionality without embedding CLI parsing logic.

#### 1. Low-level Vault Operations

```go
import (
    sharedvault "github.com/axsh/arctic-tern/shared/libs/go/vault"
    apivault "github.com/axsh/arctic-tern/vault"
)

svc := apivault.NewService(sharedvault.NewKeyringVaultBackend())
_, _ = svc.Set("anthropic", "", "sk-ant-...")
res, _ := svc.Get("anthropic", "", false)
states, _ := svc.Status([]string{"anthropic", "openai", "google"})
_ = res
_ = states
```

#### 2. High-level CLI Facade (main-ready)

```go
runner := apivault.NewCLIRunner(apivault.CLIConfig{
    Store:      sharedvault.NewKeyringVaultBackend(),
    Stdin:      os.Stdin,
    Stdout:     os.Stdout,
    Stderr:     os.Stderr,
    AppName:    "vault-cli",
    AppVersion: "0.1.0",
})
os.Exit(runner.Run(os.Args[1:]))
```

The `features/vault-cli` binary uses this facade and now serves as a minimal sample entrypoint for the public Vault API.

#### 1. Setup and Session Initialization

First, import the library, connect to the Tern server, and create an active session:

```go
import client "github.com/axsh/arctic-tern/client/v1"

c := client.New("http://localhost:3100", client.WithNoTimeout())

session, _ := c.CreateSession(ctx, client.SessionRequest{
    Agent:   "claudecode",          // Use Wayfinder, Claude Code, or Codex
    WorkDir: ".",
})
defer session.Terminate(ctx)
```

#### 2. Send Message Options

**Option A: Send Text-only Messages (convenience method)**

```go
stream, _ := session.SendText(ctx, "Create hello.txt with 'Hello, World!'")
stream.Output(os.Stdout)
```

**Option B: Send Image alongside Text (SendImageFile shortcut)**

For typical image + text queries, pass the local file path directly. Media type is automatically detected:

```go
stream, _ := session.SendImageFile(ctx, "screenshot.png", "Describe this image:")
stream.Output(os.Stdout)
```

**Option C: Complex Multimodal Layouts (Message Builder)**

For multiple text blocks or multiple images in a single message:

```go
parts, _ := client.NewMessage().
    Text("Compare these two images:").
    ImageFile("image1.png").
    ImageFile("image2.png").
    Build()

stream, _ := session.SendMessage(ctx, parts)
stream.Output(os.Stdout)
```

#### 3. Interactive Agent Execution

Coding Agents (Codex, Claude Code) may ask the user for input during a task. Tern detects this and emits a `user_input_required` SSE event. The session enters `suspended` status until the client responds.

**Respond to a user-input request (low-level API):**

```go
// After receiving user_input_required on the SSE stream:
stream, _ := session.Respond(ctx, "Yes, proceed with Option A")
stream.Output(os.Stdout)
```

**Handle user input with callbacks (recommended):**

```go
err := session.SendTextWithHandlers(ctx, "Refactor the auth module", client.StreamHandlers{
    OnText: func(text string) { fmt.Print(text) },
    OnUserInputRequired: func(ev client.UserInputRequiredEvent) (string, error) {
        // ev.Content holds the question; ev.Choices holds structured options (Wayfinder only)
        return "Use the default approach and continue", nil
    },
})
```

If `OnUserInputRequired` is not set, the SDK stops with an error when input is required. This prevents unintended auto-responses.

While a session is `suspended`, only `respond` and `terminate` are accepted. Sending a new message to the same session returns HTTP 409 Conflict.

If a session appears stuck, call `session.Terminate(ctx)` to cancel the underlying agent process.

### Multimodal Messages and Context

Tern acts as a stateless proxy for multimodal content: each `SendMessage` request carries the content for that turn. The server does not automatically re-inject images from prior turns. Re-send images when the agent needs visual context from earlier messages.

Codex enforces a 1 MB stdin limit. Configure per-agent limits in `model_profiles.yaml` under `coding_agents` (see Quick Start). Requests exceeding the limit are rejected before the agent starts.

### Agent and Model Interoperability

Switching agents or models is a matter of changing session parameters. The surrounding application remains unchanged:

```go
client.SessionRequest{Agent: "claudecode", Model: "sonnet"}   // Claude Code
client.SessionRequest{Agent: "codex",      Model: "gpt-5.5"}  // Codex
```

---

## Example Use Cases

### Maintain Existing Workflows

Organizations often standardize on a Coding Agent while allowing flexibility in model selection.

Examples include:

* Claude Code with hosted Anthropic models
* Claude Code with private Anthropic-compatible deployments
* Claude Code with local inference infrastructure

### Evaluate New Models

Teams should be able to experiment with new models without rebuilding integrations.

Examples include:

* GPT-5 → GPT-OSS
* Gemini → Gemma
* Hosted → Local
* Cloud → Private

### Reduce Migration Costs

The cost of changing tools often exceeds the cost of adopting them.

Tern aims to reduce that cost by providing common interfaces and portable context.

### Future: Context-Preserving Agent Migration

A long-term goal of the project is to support movement between Coding Agents while preserving context and workflow continuity.

```text
Claude Code
      ↓
    Codex
      ↓
 Gemini CLI
```

without requiring developers to start over.

---

## Vision

We envision a future where:

* Context is portable
* Coding Agents are interchangeable
* Language Models are interchangeable
* Local and cloud deployments coexist
* Vendor lock-in becomes optional

Tern is being built to support that future.

---

## Architecture Overview

Tern consists of three major components.

### CAWA

Coding Agent Web API.

CAWA defines a common interface for Coding Agents. The v1 API uses structured content blocks supporting both text-only and multimodal inputs (text + images). It also supports interactive execution: agents can request user input mid-task via `user_input_required` events, and clients reply through the `respond` endpoint.

### LLMGP

LLM Gateway Protocol.

LLMGP defines a common interface for Language Models and inference backends.

### Integration Layer

The Integration Layer connects Coding Agents and Language Models through a unified abstraction.

Additional architectural details will be documented separately.

---

## Roadmap

### Phase 1

* [x] CAWA API specification v1
* [x] Key Vault support
* [x] Claude Code CLI adapter
* [x] Codex CLI adapter
* [ ] Gemini CLI adapter (replaced by Antigravity SDK?)
* [x] OpenAI LLM backend
* [x] Anthropic LLM backend
* [x] Google LLM backend
* [x] Ollama LLM backend
* [x] Wayfinder adapter (Embedded Original Coding Agent - Beta)
* [x] Tern SDK v1
* [x] CAWA API v1 (multimodal content blocks)
* [x] Multimodal support (image input via v1 API)
* [ ] Interactive agent execution (`user_input_required`, `respond`, `suspended`)

### Phase 2

* [ ] Agent interaction protocol (SSE reconnect, advanced orchestration)
* [ ] MCP support
* [ ] Tern CLI
* [ ] Tern SDK v2
* [ ] Session portability
* [ ] Context export/import
* [ ] Agent switching
* [ ] Model switching
* [ ] Multimodal Voice support

### Phase 3

* [ ] Context-preserving agent migration
* [ ] Live agent handoff
* [ ] Multi-agent orchestration
* [ ] Scale-out deployment
* [ ] Statistic / Prometheus

---

## Status

Tern is currently in the early design and implementation phase.

Contributors, reviewers, and early adopters are welcome.

---

## Installation

### Prerequisites

| Requirement | Version | Notes |
| --- | --- | --- |
| Go | 1.26 or later | `go.mod` specifies `go 1.26.3` |
| Git | 2.x or later | Used for submodule checkout |
| Docker / Docker Compose | Docker 20.10+ / Compose v2+ | Only required for container deployment |
| Claude Code CLI | 2.1.x or later | Update with `claude update` |
| Codex CLI | Latest | Optional, for Codex agent support |
| OS | Windows / macOS / Linux | Cross-platform |

> **Note**: On Windows, Git Bash is recommended. Build scripts are written in bash.

> **Important**: Claude Code CLI v2.0.x ignores the `ANTHROPIC_BASE_URL` environment variable,
> which prevents Gateway-proxied requests from working. Make sure to update to v2.1.x or later
> by running `claude update`.

You will also need API keys for at least one LLM provider (OpenAI, Anthropic, or Google).

### Build from Source

```bash
git clone https://github.com/axsh/arctic-tern.git
cd arctic-tern

# Build all features and examples
./scripts/process/build.sh
```

Built binaries are placed in the `bin/` directory:

| Binary | Description |
| --- | --- |
| `bin/tern` | Tern server (full-featured, production) |
| `bin/ternctl` | CLI client for interacting with a running tern server |
| `bin/vault-cli` | Key Vault management CLI |
| `bin/minimal-server` | Minimal server example |
| `bin/minimal-client` | Minimal client example |

---

## Quick Start

### 1. Store API keys in the vault

**Option A: OS Keyring** (recommended for local development)

```bash
# Store an API key using the provider shorthand
# (--provider anthropic expands to vault key: providers/anthropic/default)
$ ./bin/vault-cli set --provider anthropic
Enter value: sk-ant-...

# For non-default key names, use --key with a full path
$ ./bin/vault-cli set --key providers/openai/team-a

# Check which providers have keys registered
$ ./bin/vault-cli status
```

**Option B: Environment Variables** (recommended for Docker / CI)

Set `vault.backend: "env"` in your `config.yaml`, then export API keys as environment variables.
Vault references in `model_profiles.yaml` are mapped to environment variable names automatically:

```bash
# vault://providers/openai/default    -> TERN_VAULT_OPENAI_DEFAULT
# vault://providers/anthropic/primary -> TERN_VAULT_ANTHROPIC_PRIMARY
export TERN_VAULT_OPENAI_DEFAULT="sk-proj-your-openai-api-key"
export TERN_VAULT_ANTHROPIC_PRIMARY="sk-ant-your-anthropic-api-key"

$ ./bin/tern --config settings/demo/config.yaml
```

The mapping rule is: `vault://providers/{provider}/{key}` becomes `TERN_VAULT_{PROVIDER}_{KEY}` (uppercased, hyphens replaced with underscores).

### 2. Configure the server

Configuration files are located in the `settings/` directory:

* `settings/example/` -- Minimal configuration for getting started
* `settings/demo/` -- Full-featured configuration for development

Create a `config.yaml`:

```yaml
llm_gateway:
  port: 14000
  model_profiles_path: "model_profiles.yaml"
vault:
  backend: "keyring"
agent_service:
  port: 3100
log:
  level: "info"
  outputs:
    - type: "stdout"
```

Create a `model_profiles.yaml`:

```yaml
default_profile:
  provider: anthropic
  model: claude-sonnet-4-20250514

providers:
  anthropic:
    api_keys:
      - name: default
        secret: vault://providers/anthropic/default
        models:
          - name: claude-sonnet-4-20250514
          - name: claude-opus-4-20250514
  openai:
    api_keys:
      - name: default
        secret: vault://providers/openai/default
        models:
          - name: gpt-4o
          - name: gpt-5.5
  ollama:
    api_keys:
      - name: default
        models:
          - name: qwen2.5-coder:7b
            behavior:
              tool_call_fallback: true
    network_config:
      base_url: "http://localhost:11434"

coding_agents:
  codex:
    max_prompt_bytes: 1048576      # 1 MB stdin limit
    max_execution_seconds: 3600
    idle_timeout_seconds: 300
    execution_mode: interactive    # interactive | single_shot
  claudecode:
    max_execution_seconds: 3600
    idle_timeout_seconds: 300
    execution_mode: interactive
```

`execution_mode` controls how the server talks to the CLI:

| Mode | Behavior |
| --- | --- |
| `interactive` | Keeps stdin open so the agent can request user input mid-task (default) |
| `single_shot` | Sends the prompt and closes stdin immediately (legacy behavior) |

### 3. Start the server

In one terminal, start the tern server:

```bash
$ ./bin/tern --config ./settings/demo/config.yaml
tern server started and running...
```

The server exposes:
* CAWA Agent Service on port `3100` (configurable via `agent_service.port`)
* LLM Gateway on port `14000` (configurable via `llm_gateway.port`)

### 4. Run a task with ternctl

Open another terminal and interact with the server:

```bash
# Check server health
$ ./bin/ternctl health

# List available agents and models
$ ./bin/ternctl agents
$ ./bin/ternctl models

# Run a coding task
$ ./bin/ternctl run \
    --agent claudecode \
    --prompt "Analyze the current directory structure and create a summary report in REPORT.md" \
    --work-dir ./tmp
```

When the task completes, ternctl outputs session details as JSON:

```json
{
  "agent_name": "claudecode",
  "id": "a95db64cb646901efb395a18d817a37d",
  "status": "completed",
  "work_dir": "tmp"
}
```

Session `status` values include `active`, `suspended` (waiting for user input), `completed`, `error`, and `closed`. Check status with `ternctl session --id <session-id>`.

### 5. Continue an existing session

Use `--resume` with the session `id` from the previous output to continue the conversation:

```bash
$ ./bin/ternctl run \
    --resume a95db64cb646901efb395a18d817a37d \
    --prompt "Add a table of contents to REPORT.md"
```

The agent resumes the previous session with full context of the prior conversation.

### 6. Or use the Go client library

```go
import client "github.com/axsh/arctic-tern/client/v1"

c := client.New("http://localhost:3100", client.WithNoTimeout())
session, _ := c.CreateSession(ctx, client.SessionRequest{
    Agent:   "claudecode",
    WorkDir: ".",
})
defer session.Terminate(ctx)

// 1. Text-only message (convenience method)
stream, _ := session.SendText(ctx, "Create hello.txt")
stream.Output(os.Stdout)

// 2. Multimodal message with image (convenience method)
stream, _ = session.SendImageFile(ctx, "screenshot.png", "What is in this screenshot?")
stream.Output(os.Stdout)

// 3. Interactive message with user-input callback
_ = session.SendTextWithHandlers(ctx, "Refactor auth.go", client.StreamHandlers{
    OnText: func(text string) { fmt.Print(text) },
    OnUserInputRequired: func(ev client.UserInputRequiredEvent) (string, error) {
        return "Proceed with the safer option", nil
    },
})
```

---

## Documentation

### Project Structure

```
tern/
  client/             # Go client library (Root package)
  server/             # Server framework (Root package)
  features/           # Deployable applications
    tern/             # Main server (CAWA + LLMGP)
    ternctl/          # CLI client
    vault-cli/        # Key Vault management CLI
  shared/libs/go/     # Shared Go libraries
    codingagent/      # Coding Agent adapters
    llmgateway/       # LLM Gateway providers
    config/           # Configuration loading
    vault/            # Secret management
  settings/           # Configuration files
    example/          # Minimal config for getting started
    demo/             # Full-featured config for development
  examples/           # Working examples
    minimal-server/   # Minimal server setup
    minimal-client/   # Minimal client usage
  scripts/            # Build and test scripts
  docs/               # Documentation resources
```

Detailed API documentation and protocol specifications are planned for a future release.

---

## Design Documents

Tern is built around two core protocols:

* **CAWA (Coding Agent Web API)** -- A REST/WebSocket API that abstracts Coding Agent lifecycle and communication. Agents register themselves via Go `init()` imports, making it simple to add support for new agents.

* **LLMGP (LLM Gateway Protocol)** -- A reverse-proxy layer that routes LLM requests to configured providers (OpenAI, Anthropic, Google, Ollama). API keys are managed through a secure vault with support for keyring, environment variables, and encrypted storage.

Full protocol specifications are being developed and will be published as the project matures.

---

## Testing

```bash
# Full build + unit tests
./scripts/process/build.sh

# Integration tests
./scripts/process/integration_test.sh

# Unit tests for shared libraries only
cd shared/libs/go
go test ./... -v
```

---

## Contributing

Ideas, experiments, implementation feedback, and specification discussions are welcome.

Please open an issue to start a conversation.

---

## License

Apache License 2.0. See [LICENSE](LICENSE) for details.
