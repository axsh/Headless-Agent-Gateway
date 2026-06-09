#!/bin/bash
# Fix volume ownership
chown -R claude:claude /workspace 2>/dev/null || true
# Drop to claude user
exec gosu claude "$@"
