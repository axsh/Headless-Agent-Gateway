# HAG Agent-only container (Coding Agent with Claude Code CLI)
FROM golang:1.21-bookworm AS go-builder
WORKDIR /build
COPY go.work go.work.sum ./
COPY shared/libs/go/ ./shared/libs/go/
COPY examples/ ./examples/
RUN cd shared/libs/go && go build -o /build/bin/hag-agent ../../examples/standalone

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y \
    git curl python3 gosu procps ca-certificates \
    && rm -rf /var/lib/apt/lists/*
RUN curl -fsSL https://deb.nodesource.com/setup_22.x | bash - \
    && apt-get install -y nodejs
RUN npm install -g @anthropic-ai/claude-code

RUN groupadd -r claude && useradd -r -g claude -m -d /home/claude claude
COPY --from=go-builder /build/bin/hag-agent /usr/local/bin/
COPY container/hybrid/agent/entrypoint.sh /usr/local/bin/
RUN chmod +x /usr/local/bin/entrypoint.sh

RUN mkdir -p /workspace && chown -R claude:claude /workspace /home/claude

ENV CLAUDE_CODE_SKIP_SANDBOX=1
ENV LLM_GATEWAY_URL=""
ENV AGENT_AUTH_TOKEN=""

EXPOSE 3100
WORKDIR /workspace
ENTRYPOINT ["entrypoint.sh"]
CMD ["hag-agent", "--agent-only"]
