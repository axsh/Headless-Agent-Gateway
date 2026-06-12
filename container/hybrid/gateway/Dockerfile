# Arctic Tern Gateway-only container (LLM Gateway Proxy)
FROM golang:1.21-bookworm AS builder
WORKDIR /build
COPY go.work go.work.sum ./
COPY shared/libs/go/ ./shared/libs/go/
COPY examples/ ./examples/
RUN cd shared/libs/go && go build -o /build/bin/tern-gateway ../../examples/cawa-server

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*
COPY --from=builder /build/bin/tern-gateway /usr/local/bin/
EXPOSE 14000
CMD ["tern-gateway", "--gateway-only"]
