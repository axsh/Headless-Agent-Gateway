FROM golang:1.26.3-alpine AS builder

WORKDIR /app

# Copy the entire workspace since we have local shared library replacements
COPY . .

WORKDIR /app/features/tern

# Build the tern binary
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/bin/tern main.go

FROM alpine:3.21

WORKDIR /app

# Copy built binary from builder
COPY --from=builder /app/bin/tern /app/tern

# Set default env or config paths
ENTRYPOINT ["/app/tern", "-config", "/etc/tern/config.yaml"]
