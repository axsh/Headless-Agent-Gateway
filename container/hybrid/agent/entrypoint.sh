#!/bin/bash
chown -R claude:claude /workspace 2>/dev/null || true
exec gosu claude "$@"
