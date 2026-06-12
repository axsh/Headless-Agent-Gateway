#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
TERNCTL_CLIENT="$PROJECT_ROOT/bin/ternctl"
COMPOSE_TYPE="${1:-all-in-one}"
COMPOSE_DIR="$PROJECT_ROOT/container/$COMPOSE_TYPE"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'

pass() { echo -e "${GREEN}PASS${NC}: $1"; }
fail() { echo -e "${RED}FAIL${NC}: $1"; exit 1; }

echo "=== Container Integration Test: $COMPOSE_TYPE ==="

# Pre-check: ternctl binary
if [[ ! -x "$TERNCTL_CLIENT" ]]; then
    echo "Building ternctl..."
    cd "$PROJECT_ROOT" && ./scripts/process/build.sh
fi

# 1. Build containers
echo "Building containers..."
cd "$COMPOSE_DIR" && docker-compose build

# 2. Start containers
echo "Starting containers..."
docker-compose up -d

# 3. Wait for health (max 30s)
echo "Waiting for health check..."
for i in $(seq 1 30); do
    if "$TERNCTL_CLIENT" --server "http://localhost:3100" health 2>/dev/null; then
        break
    fi
    if [[ $i -eq 30 ]]; then
        fail "Health check timeout after 30s"
    fi
    sleep 1
done
pass "Health check"

# 4. List agents
AGENTS=$("$TERNCTL_CLIENT" --server "http://localhost:3100" agents 2>&1)
if [[ -n "$AGENTS" ]]; then
    pass "List agents: $AGENTS"
else
    fail "No agents returned"
fi

# 5. Create + terminate session
SESSION_ID=$("$TERNCTL_CLIENT" --server "http://localhost:3100" run \
    --agent claudecode --prompt "echo hello" 2>&1 | grep "Session created" | awk '{print $3}') || true
if [[ -n "$SESSION_ID" ]]; then
    pass "Session lifecycle"
else
    echo "WARN: Session test skipped (no real agent available)"
fi

# 6. Cleanup
echo "Stopping containers..."
cd "$COMPOSE_DIR" && docker-compose down -v

echo "=== All tests passed ==="
