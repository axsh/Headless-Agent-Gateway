#!/usr/bin/env bash
# ============================================================
# view-syslog.sh — Syslog UDP Packet Listener / Viewer
#
# Listens on a UDP address (default: localhost:514) and displays
# incoming syslog messages. Useful for debugging HAG's syslog output.
# ============================================================

set -euo pipefail

# Default values
SYSLOGD="localhost:514"
FOLLOW=true
LINES=""

show_help() {
    cat << 'EOF'
Usage: view-syslog.sh [OPTIONS]

Listen and display incoming syslog messages sent to a syslog daemon.

Options:
  -s, --syslogd ADDR   Syslogd listen address in host:port format (default: localhost:514)
  -f, --follow         Keep listening and display incoming messages indefinitely (default)
  -n, --lines NUM      Stop after displaying NUM messages
  -h, --help           Show this help message and exit

Examples:
  # Listen on default localhost:514
  ./scripts/utils/view-syslog.sh

  # Listen on 127.0.0.1:10514 and stop after 10 messages
  ./scripts/utils/view-syslog.sh -s 127.0.0.1:10514 -n 10
EOF
}

# Parse options
while [[ $# -gt 0 ]]; do
    case "$1" in
        -s|--syslogd)
            if [[ -z "${2:-}" ]]; then
                echo -e "\033[0;31mError: Option $1 requires an argument.\033[0m\n" >&2
                show_help
                exit 1
            fi
            SYSLOGD="$2"
            shift 2
            ;;
        -f|--follow)
            FOLLOW=true
            shift
            ;;
        -n|--lines)
            if [[ -z "${2:-}" ]] || ! [[ "$2" =~ ^[0-9]+$ ]]; then
                echo -e "\033[0;31mError: Option $1 requires a valid numeric argument.\033[0m\n" >&2
                show_help
                exit 1
            fi
            LINES="$2"
            FOLLOW=false
            shift 2
            ;;
        -h|--help)
            show_help
            exit 0
            ;;
        *)
            echo -e "\033[0;31mError: Unknown option $1\033[0m\n" >&2
            show_help
            exit 1
            ;;
    esac
done

# Split SYSLOGD into host and port
if [[ "$SYSLOGD" =~ ^([^:]+):([0-9]+)$ ]]; then
    HOST="${BASH_REMATCH[1]}"
    PORT="${BASH_REMATCH[2]}"
else
    echo -e "\033[0;31mError: Invalid syslogd address format '$SYSLOGD'. Must be host:port.\033[0m\n" >&2
    show_help
    exit 1
fi

# Ensure python is available
if ! command -v python &> /dev/null; then
    echo "Error: Python is required to run this script (cannot find 'python' on PATH)." >&2
    exit 1
fi

# Start python UDP listener
python -c "
import socket
import sys

host = '$HOST'
port = $PORT
lines = '$LINES'

if host == 'localhost':
    host = '127.0.0.1'

limit = int(lines) if lines else None

s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
try:
    s.bind((host, port))
except Exception as e:
    print(f'Error binding to {host}:{port}: {e}', file=sys.stderr)
    sys.exit(1)

print(f'Listening for syslog messages on {host}:{port}...', file=sys.stderr)
sys.stderr.flush()

count = 0
try:
    while True:
        data, addr = s.recvfrom(4096)
        msg = data.decode('utf-8', errors='ignore').strip()
        print(msg)
        sys.stdout.flush()
        if limit is not None:
            count += 1
            if count >= limit:
                break
except KeyboardInterrupt:
    pass
"
