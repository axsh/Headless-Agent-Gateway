# Multi-stage build for Arctic Tern All-in-One container
# Stage 1: Go backend build
FROM golang:1.21-bookworm AS go-builder
WORKDIR /build
COPY go.work go.work.sum ./
COPY shared/libs/go/ ./shared/libs/go/
COPY examples/ ./examples/
RUN cd shared/libs/go && go build -o /build/bin/tern-server ../../examples/cawa-server

# Stage 2: Runtime
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y \
    git curl python3 make g++ gosu \
    procps jq file wget ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Install Node.js (Claude Code CLI requires it)
RUN curl -fsSL https://deb.nodesource.com/setup_22.x | bash - \
    && apt-get install -y nodejs

# Install Claude Code CLI
RUN npm install -g @anthropic-ai/claude-code

# Create non-root user (Claude CLI 2.1.x forbids root)
RUN groupadd -r claude && useradd -r -g claude -m -d /home/claude claude

COPY --from=go-builder /build/bin/tern-server /usr/local/bin/
COPY container/all-in-one/entrypoint.sh /usr/local/bin/
RUN chmod +x /usr/local/bin/entrypoint.sh

RUN mkdir -p /workspace && chown -R claude:claude /workspace /home/claude

ENV CLAUDE_CODE_SKIP_SANDBOX=1
ENV ANTHROPIC_API_KEY="not-needed"
ENV LLM_GATEWAY_URL="http://localhost:14000"
ENV DEFAULT_MODEL=""
ENV AGENT_AUTH_TOKEN=""

EXPOSE 14000 3100
WORKDIR /workspace
ENTRYPOINT ["entrypoint.sh"]
CMD ["tern-server"]
