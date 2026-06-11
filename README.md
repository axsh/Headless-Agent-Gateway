# Tern

> Fly with the best agent. Anywhere. Anytime.

The Arctic Tern makes one of the longest migrations on Earth.

It never stays where conditions are no longer optimal.

When the environment changes, it moves.

We believe developers should be able to do the same.

The AI ecosystem evolves too quickly to commit to a single agent forever.

Tern helps you keep your context while the ecosystem evolves around you.

---

## What is Tern?

Tern is an open-source framework that lets you use any Coding Agent with any Language Model through a unified interface.

More importantly, Tern is being designed around a simple idea:

**Your work should not be trapped inside a single agent, model, or vendor ecosystem.**

Today you may prefer Claude Code.

Tomorrow you may prefer Codex.

Next year an entirely new agent may become the best choice.

Tern allows you to adapt without starting over.

---

## Quick Example

```go
agent := tern.New()

agent.Use("claude-code")
agent.Model("claude-sonnet")

result, err := agent.Run(task)
```

Switch agents:

```go
agent.Use("codex")
agent.Model("gpt-5")
```

Switch models:

```go
agent.Use("claude-code")
agent.Model("qwen3-local")
```

The goal is simple:

Keep your workflow.

Keep your context.

Adapt continuously.

---

## Why Tern?

The AI ecosystem moves incredibly fast.

New models become state-of-the-art.

New Coding Agents introduce better workflows.

Local models become practical.

Hosted services evolve.

Pricing changes.

Capabilities change.

Developers naturally want to use whatever works best today.

The problem is not choice.

The problem is migration.

Changing agents, changing models, or moving between cloud and local deployments often requires rebuilding workflows, integrations, tooling, and habits.

Many developers simply stay where they are because moving is expensive.

Tern exists to reduce that friction.

---

## Use Cases

### Keep Your Favorite Agent

Use the Coding Agent you like most while choosing the model that best fits your requirements.

Examples:

* Claude Code + Claude
* Claude Code + private Anthropic-compatible endpoint
* Claude Code + local deployment

---

### Move Between Model Providers

Switch models without rebuilding your workflow.

Examples:

* GPT-5 → GPT-OSS
* Gemini → Gemma
* Cloud → Local
* Hosted → Private

---

### Reduce Vendor Lock-In

Keep your options open as the ecosystem evolves.

Choose based on:

* Quality
* Cost
* Privacy
* Compliance
* Latency
* Availability

---

### Future: Context-Preserving Agent Migration

Our long-term goal is to allow developers to move between Coding Agents while preserving context.

```text
Claude Code
      ↓
    Codex
      ↓
 Gemini CLI
```

without losing the work already done.

This is one of the core ideas behind Tern.

---

## Vision

We envision a future where:

* Context is portable
* Coding Agents are interchangeable
* Language Models are interchangeable
* Local and cloud deployments coexist
* Vendor lock-in becomes optional
* Developers continuously adapt as the ecosystem evolves

Tern is being built to make that future possible.

---

## Core Principles

### Context Portability

Your work should not be trapped inside a single agent.

### Agent Neutral

Use the Coding Agent that works best for you.

### Model Neutral

Use the Language Model that works best for you.

### Local First

Local models should be first-class citizens.

### Open Standards

Interoperability should be built into the ecosystem.

---

## Roadmap

### Phase 1

* [ ] CAWA specification draft
* [ ] Claude Code adapter
* [ ] Codex adapter
* [ ] Gemini CLI adapter
* [ ] OpenAI backend
* [ ] Anthropic backend
* [ ] Ollama backend

### Phase 2

* [ ] Session portability
* [ ] Context export/import
* [ ] Model switching
* [ ] Agent switching

### Phase 3

* [ ] Context-preserving agent migration
* [ ] Live agent handoff
* [ ] Multi-agent orchestration

---

## Internal Architecture

Tern consists of three major components:

### CAWA

**Coding Agent Web API**

A common interface for Coding Agents.

CAWA aims to provide a consistent API regardless of the underlying agent implementation.

Supported adapters will eventually include:

* Claude Code
* Codex
* Gemini CLI
* Additional future agents

---

### LLMGP

**LLM Gateway Protocol**

A common interface for Language Models.

LLMGP allows agents to communicate with:

* OpenAI
* Anthropic
* Gemini
* Ollama
* vLLM
* llama.cpp
* Future model providers

through a unified abstraction layer.

---

### Integration Layer

The Integration Layer connects Coding Agents and Language Models through common interfaces.

```text
Application
      │
      ▼

     Tern

 ├─ CAWA
 │   ├─ Claude Code
 │   ├─ Codex
 │   ├─ Gemini CLI
 │   └─ Future Agents
 │
 ├─ LLMGP
 │   ├─ OpenAI
 │   ├─ Anthropic
 │   ├─ Gemini
 │   ├─ Ollama
 │   ├─ vLLM
 │   ├─ llama.cpp
 │   └─ Future Models
 │
 └─ Integration Layer
```

---

## Open Standards

### Coding Agent Web API (CAWA)

CAWA aims to provide a common interoperability layer for Coding Agents.

Comparable examples include:

* OpenAPI for HTTP APIs
* OCI for containers
* MCP for AI tool integration

We believe Coding Agents will eventually need a similar interoperability layer.

Status: Early design phase.

Contributors welcome.

---

## Installation

TODO: Write installation instructions.

---

## Quick Start

TODO: Write quick start guide.

---

## Supported Agents

TODO: Add support matrix.

---

## Supported Models

TODO: Add support matrix.

---

## Documentation

TODO: Add documentation links.

---

## Design Documents

TODO: Add architecture and protocol specifications.

---

## Contributing

Tern is still in its early stages.

We are actively looking for:

* Contributors
* Early adopters
* Adapter developers
* Protocol designers
* Specification reviewers
* Architecture discussions

Ideas, experiments, criticism, and feedback are all welcome.

Please open an issue and join the discussion.

---

## FAQ

### Is Tern another Coding Agent?

No.

Tern is the interoperability layer around Coding Agents and Language Models.

### Is Tern another model provider?

No.

Tern works with existing model providers and local inference engines.

### Why build Tern?

Because the ecosystem evolves too quickly to commit to a single agent forever.

---

## License

TODO: Specify license.
