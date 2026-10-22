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
if [ -f "$PROJECT_ROOT/bin/$APP_NAME" ]; then
    BIN_FILE="$PROJECT_ROOT/bin/$APP_NAME"
elif [ -f "$PROJECT_ROOT/$APP_NAME" ]; then
    BIN_FILE="$PROJECT_ROOT/$APP_NAME"
else
    BIN_FILE="$PROJECT_ROOT/bin/$APP_NAME"
fi

DEFAULT_PID_FILE="$PROJECT_ROOT/run/state_sync.pid"
FALLBACK_PID_FILE="$PROJECT_ROOT/run/state_sync.pid"
PID_FILE=""
TARGET_PID_FILE=""

LOG_DIR="$PROJECT_ROOT/logs"
LOG_FILE="$LOG_DIR/nohup.log"

DEFAULT_ENV="prod"
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
  -e, --env <env>          Runtime environment (e.g., dev, prod) [default: prod]
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

resolve_pid_file() {
    local cfg_dir="$CONFIG_PATH"
    if [ -z "$cfg_dir" ]; then
        if [ -d "$PROJECT_ROOT/configs" ]; then
            cfg_dir="$PROJECT_ROOT/configs"
        elif [ -d "$PROJECT_ROOT/config" ]; then
            cfg_dir="$PROJECT_ROOT/config"
        else
            cfg_dir="$PROJECT_ROOT"
        fi
    fi

    local target_cfg=""
    if [ -f "$cfg_dir" ]; then
        target_cfg="$cfg_dir"
    elif [ -d "$cfg_dir" ]; then
        local env_file="config.yaml"
        if [ "$APP_ENV" != "prod" ] && [ -n "$APP_ENV" ]; then
            env_file="config.$APP_ENV.yaml"
        fi
        if [ -f "$cfg_dir/$env_file" ]; then
            target_cfg="$cfg_dir/$env_file"
        elif [ -f "$cfg_dir/config.yaml" ]; then
            target_cfg="$cfg_dir/config.yaml"
        elif [ -f "$cfg_dir/config.example.yaml" ]; then
            target_cfg="$cfg_dir/config.example.yaml"
        fi
    fi

    local parsed_pid=""
    if [ -n "$target_cfg" ] && [ -f "$target_cfg" ]; then
        # 주석(#)이 아닌 줄에서 pid_file (또는 pidFile) 설정값 추출
        parsed_pid="$(grep -E '^[[:space:]]*(pid_file|pidFile):' "$target_cfg" 2>/dev/null | grep -v '^[[:space:]]*#' | head -n1 | sed -E 's/^[[:space:]]*(pid_file|pidFile):[[:space:]]*["'\'']?([^"'\'']+)["'\'']?.*$/\2/' | tr -d '\r' | xargs)"
    fi

    if [ -n "$parsed_pid" ]; then
        TARGET_PID_FILE="$parsed_pid"
        local target_dir
        target_dir="$(dirname "$TARGET_PID_FILE")"
        if [ -f "$TARGET_PID_FILE" ]; then
            PID_FILE="$TARGET_PID_FILE"
        elif [ -d "$target_dir" ] && [ -w "$target_dir" ]; then
            PID_FILE="$TARGET_PID_FILE"
        else
            local file_base
            file_base="$(basename "$TARGET_PID_FILE")"
            if [ -z "$file_base" ] || [ "$file_base" = "." ]; then
                file_base="state_sync.pid"
            fi
            PID_FILE="$PROJECT_ROOT/run/$file_base"
        fi
    else
        TARGET_PID_FILE="$DEFAULT_PID_FILE"
        PID_FILE="$DEFAULT_PID_FILE"
    fi
}

resolve_pid_file

get_pid() {
    resolve_pid_file
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
    resolve_pid_file
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

    mkdir -p "$(dirname "$FALLBACK_PID_FILE")"
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
    echo "    Target PID:  $TARGET_PID_FILE"

    nohup "$BIN_FILE" "${args[@]}" >> "$LOG_FILE" 2>&1 &
    local new_pid=$!

    # Wait for PID file to be written by application (up to 3 seconds)
    local waited=0
    while [ $waited -lt 30 ]; do
        if [ -f "$TARGET_PID_FILE" ]; then
            PID_FILE="$TARGET_PID_FILE"
            break
        elif [ -f "$FALLBACK_PID_FILE" ]; then
            PID_FILE="$FALLBACK_PID_FILE"
            break
        fi
        if ! is_running "$new_pid"; then
            break
        fi
        sleep 0.1
        waited=$((waited + 1))
    done

    # Verify if process is still running after a short delay
    if is_running "$new_pid"; then
        echo "[+] $APP_NAME started successfully (PID: $new_pid, PID File: $PID_FILE)."
    else
        echo "[-] Failed to start $APP_NAME (process terminated immediately)." >&2
        echo "[-] Check log file for details: $LOG_FILE" >&2
        if [ -n "$PID_FILE" ] && [ -f "$PID_FILE" ]; then
            rm -f "$PID_FILE"
        fi
        exit 1
    fi
}

stop_app() {
    resolve_pid_file
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

    if [ -f "$PID_FILE" ]; then
        rm -f "$PID_FILE"
    fi
    echo "[+] $APP_NAME has been stopped."
}

status_app() {
    resolve_pid_file
    local pid
    pid="$(get_pid)"

    if [ -z "$pid" ]; then
        echo "[-] $APP_NAME is stopped."
        return 3
    fi

    if is_running "$pid"; then
        echo "[+] $APP_NAME is running (PID: $pid, PID File: $PID_FILE)."
        if command -v ps >/dev/null 2>&1; then
            ps -p "$pid" -o pid,user,%cpu,%mem,etime,args 2>/dev/null || true
        fi
        return 0
    else
        echo "[!] $APP_NAME is not running, but stale PID file exists (PID: $pid, File: $PID_FILE)."
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

