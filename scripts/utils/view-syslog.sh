#!/usr/bin/env bash
# ============================================================
# view-syslog.sh — Docker Syslogd Log Viewer
#
# Tails the syslog messages inside the running 'syslogd' container.
# ============================================================

set -euo pipefail

# Default values
FOLLOW=true
LINES="100"
CONTAINER_NAME="syslogd"
LOG_FILE="//var/log/messages"

show_help() {
    cat << 'EOF'
Usage: view-syslog.sh [OPTIONS]

Tail and display syslog messages from the running syslogd container.

Options:
  -c, --container NAME  Docker container name (default: syslogd)
  -f, --follow          Follow the log output (default)
  --no-follow           Do not follow the log output
  -n, --lines NUM       Output the last NUM lines (default: 100)
  -h, --help            Show this help message and exit

Examples:
  # Tail syslogd container with follow
  ./scripts/utils/view-syslog.sh

  # Show last 20 lines without follow
  ./scripts/utils/view-syslog.sh -n 20 --no-follow
EOF
}

# Parse options
while [[ $# -gt 0 ]]; do
    case "$1" in
        -c|--container)
            if [[ -z "${2:-}" ]]; then
                echo -e "\033[0;31mError: Option $1 requires an argument.\033[0m\n" >&2
                show_help
                exit 1
            fi
            CONTAINER_NAME="$2"
            shift 2
            ;;
        -f|--follow)
            FOLLOW=true
            shift
            ;;
        --no-follow)
            FOLLOW=false
            shift
            ;;
        -n|--lines)
            if [[ -z "${2:-}" ]] || ! [[ "$2" =~ ^[0-9]+$ ]]; then
                echo -e "\033[0;31mError: Option $1 requires a valid numeric argument.\033[0m\n" >&2
                show_help
                exit 1
            fi
            LINES="$2"
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

# Ensure docker is installed
if ! command -v docker &> /dev/null; then
    echo "Error: docker is required to run this script." >&2
    exit 1
fi

# Check if the container is running
if ! docker ps --filter "name=${CONTAINER_NAME}" --filter "status=running" --format "{{.Names}}" | grep -q "^${CONTAINER_NAME}$"; then
    echo "Error: Container '${CONTAINER_NAME}' is not running." >&2
    exit 1
fi

# Tail the syslog file inside container
TAIL_ARGS="-n ${LINES}"
if [ "${FOLLOW}" = true ]; then
    TAIL_ARGS="${TAIL_ARGS} -f"
fi

# Disable path conversion in MSYS/Git Bash
export MSYS_NO_PATHCONV=1

echo "Tailing ${LOG_FILE} from container '${CONTAINER_NAME}'..." >&2
exec docker exec "${CONTAINER_NAME}" tail ${TAIL_ARGS} "${LOG_FILE}"
