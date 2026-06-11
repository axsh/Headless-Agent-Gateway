FROM golang:1.26.3-alpine AS builder

WORKDIR /app

# Copy the entire workspace since we have local shared library replacements
COPY . .

WORKDIR /app/examples/cawa-server

# Build the cawa-server binary
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/bin/cawa-server main.go

FROM alpine:3.21

WORKDIR /app

# Copy built binary from builder
COPY --from=builder /app/bin/cawa-server /app/cawa-server

# Set default env or config paths
ENTRYPOINT ["/app/cawa-server", "-config", "/etc/tern/config.yaml"]
