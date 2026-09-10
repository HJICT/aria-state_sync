#!/usr/bin/env bash
#
# Process management script for StateSync
# Supports: start, stop, restart, status
# Configurable parameters: -e/--env, -c/--config
#

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [ "$(basename "$SCRIPT_DIR")" = "bin" ]; then
    PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
else
    PROJECT_ROOT="$SCRIPT_DIR"
fi

APP_NAME="state-sync"
BIN_FILE="$PROJECT_ROOT/bin/$APP_NAME"
PID_DIR="$PROJECT_ROOT/run"
PID_FILE="$PID_DIR/$APP_NAME.pid"
LOG_DIR="$PROJECT_ROOT/logs"
LOG_FILE="$LOG_DIR/nohup.log"

DEFAULT_ENV="dev"
APP_ENV=""
CONFIG_PATH=""
COMMAND=""

print_usage() {
    cat << EOF
Usage: $(basename "$0") {start|stop|restart|status} [options]

Commands:
  start           Start the application in the background
  stop            Stop the running application gracefully
  restart         Restart the application
  status          Show application running status

Options:
  -e, --env <env>          Runtime environment (e.g., dev, prod) [default: dev]
  -c, --config <path>      Configuration directory or file path
  -h, --help               Display this help message

Examples:
  $(basename "$0") start
  $(basename "$0") start -e prod
  $(basename "$0") start --env dev --config ./configs
  $(basename "$0") status
  $(basename "$0") restart -e prod
  $(basename "$0") stop

EOF
}

# Parse command line arguments
while [ $# -gt 0 ]; do
    case "$1" in
        start|stop|restart|status)
            if [ -n "$COMMAND" ]; then
                echo "Error: Multiple commands specified ('$COMMAND' and '$1')" >&2
                print_usage
                exit 1
            fi
            COMMAND="$1"
            shift
            ;;
        -e|--env)
            if [ -z "$2" ] || [[ "$2" == -* ]]; then
                echo "Error: Option '$1' requires an argument." >&2
                print_usage
                exit 1
            fi
            APP_ENV="$2"
            shift 2
            ;;
        -c|--config)
            if [ -z "$2" ] || [[ "$2" == -* ]]; then
                echo "Error: Option '$1' requires an argument." >&2
                print_usage
                exit 1
            fi
            CONFIG_PATH="$2"
            shift 2
            ;;
        -h|--help|help)
            print_usage
            exit 0
            ;;
        *)
            echo "Error: Unknown argument '$1'" >&2
            print_usage
            exit 1
            ;;
    esac
done

if [ -z "$COMMAND" ]; then
    echo "Error: No command specified." >&2
    print_usage
    exit 1
fi

# Fallback to default environment if not specified
if [ -z "$APP_ENV" ]; then
    APP_ENV="$DEFAULT_ENV"
fi

get_pid() {
    if [ -f "$PID_FILE" ]; then
        cat "$PID_FILE" 2>/dev/null || echo ""
    fi
}

is_running() {
    local pid="$1"
    if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
        return 0
    else
        return 1
    fi
}

start_app() {
    local pid
    pid="$(get_pid)"

    if is_running "$pid"; then
        echo "[-] $APP_NAME is already running (PID: $pid)."
        return 0
    fi

    if [ -n "$pid" ]; then
        echo "[!] Removing stale PID file ($PID_FILE, PID: $pid)..."
        rm -f "$PID_FILE"
    fi

    if [ ! -f "$BIN_FILE" ]; then
        echo "[-] Binary not found: $BIN_FILE" >&2
        echo "[-] Please build the project first using 'make build'." >&2
        exit 1
    fi

    mkdir -p "$PID_DIR"
    mkdir -p "$LOG_DIR"

    local args=("--env" "$APP_ENV")
    if [ -n "$CONFIG_PATH" ]; then
        args+=("--config" "$CONFIG_PATH")
    fi

    echo "[*] Starting $APP_NAME..."
    echo "    Environment: $APP_ENV"
    if [ -n "$CONFIG_PATH" ]; then
        echo "    Config Path: $CONFIG_PATH"
    fi
    echo "    Log File:    $LOG_FILE"
    echo "    PID File:    $PID_FILE"

    nohup "$BIN_FILE" "${args[@]}" >> "$LOG_FILE" 2>&1 &
    local new_pid=$!
    echo "$new_pid" > "$PID_FILE"

    # Verify if process is still running after a short delay
    sleep 1
    if is_running "$new_pid"; then
        echo "[+] $APP_NAME started successfully (PID: $new_pid)."
    else
        echo "[-] Failed to start $APP_NAME (process terminated immediately)." >&2
        echo "[-] Check log file for details: $LOG_FILE" >&2
        rm -f "$PID_FILE"
        exit 1
    fi
}

stop_app() {
    local pid
    pid="$(get_pid)"

    if [ -z "$pid" ]; then
        echo "[-] $APP_NAME is not running (PID file not found)."
        return 0
    fi

    if ! is_running "$pid"; then
        echo "[!] Process (PID: $pid) is not running, but stale PID file found."
        echo "[*] Cleaning up $PID_FILE..."
        rm -f "$PID_FILE"
        return 0
    fi

    echo "[*] Stopping $APP_NAME (PID: $pid)..."
    kill -15 "$pid" 2>/dev/null || true

    local timeout=10
    local count=0
    while is_running "$pid"; do
        sleep 1
        count=$((count + 1))
        if [ "$count" -ge "$timeout" ]; then
            echo "[!] Process did not stop gracefully within $timeout seconds. Forcing shutdown (SIGKILL)..."
            kill -9 "$pid" 2>/dev/null || true
            sleep 1
            break
        fi
    done

    rm -f "$PID_FILE"
    echo "[+] $APP_NAME has been stopped."
}

status_app() {
    local pid
    pid="$(get_pid)"

    if [ -z "$pid" ]; then
        echo "[-] $APP_NAME is stopped."
        return 3
    fi

    if is_running "$pid"; then
        echo "[+] $APP_NAME is running (PID: $pid)."
        if command -v ps >/dev/null 2>&1; then
            ps -p "$pid" -o pid,user,%cpu,%mem,etime,args 2>/dev/null || true
        fi
        return 0
    else
        echo "[!] $APP_NAME is not running, but stale PID file exists (PID: $pid)."
        return 1
    fi
}

case "$COMMAND" in
    start)
        start_app
        ;;
    stop)
        stop_app
        ;;
    restart)
        stop_app
        start_app
        ;;
    status)
        status_app
        ;;
esac

