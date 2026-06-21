#!/usr/bin/env bash
#
# ClawBench 开发调试启动脚本
#
# 用法:
#   ./dev-server.sh              # 后台启动 Vite HMR（代理到生产后端的 dev HTTP 端口）
#   ./dev-server.sh --fg         # 前台启动
#   ./dev-server.sh --stop       # 停止 Vite
#   ./dev-server.sh --restart    # 重启 Vite
#
# 原理:
#   生产后端（./clawbench）在 TLS 模式下会额外监听一个 localhost-only 的 HTTP 端口
#   （dev_port，默认 Port+2，如 20002）。本脚本只启动 Vite HMR，代理到该端口，
#   与生产服务共享同一套数据，无需独立的后端实例。
#

set -e

NAME="clawbench-dev"

# All runtime data under .clawbench/ (green portable deployment)
VITE_PID_FILE=".clawbench/dev-vite.pid"
VITE_LOG=".clawbench/dev-vite.log"

# Resolve effective port from config (fallback to default)
_resolve_port() {
    local port
    port=$(grep "^port:" "config/config.yaml" 2>/dev/null | awk '{print $2}' | tr -d '"')
    echo "${port:-20000}"
}

PROD_PORT=$(_resolve_port)
DEV_HTTP_PORT=$((PROD_PORT + 2))
FRONTEND_PORT=$((PROD_PORT + 3))

# Load shared shell utilities
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/scripts/common.sh"

# Check that the production backend's dev HTTP port is reachable
check_dev_port() {
    local listening=""
    if command -v ss >/dev/null 2>&1; then
        listening=$(ss -tlnp 2>/dev/null | grep "127.0.0.1:${DEV_HTTP_PORT}" | head -1)
    fi
    if [[ -z "$listening" ]]; then
        echo "WARNING: Production dev HTTP port $DEV_HTTP_PORT not detected." >&2
        echo "  Make sure the production server is running: ./clawbench" >&2
        echo "  (dev_port auto-enables when TLS is on, default: port+2)" >&2
        echo "" >&2
        read -p "  Start production server now? [y/N] " -r
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            ./clawbench &
            sleep 1
        else
            echo "Aborted." >&2
            exit 1
        fi
    fi
}

show_auto_password() {
    local auto_pw_file=".clawbench/auto-password"
    if [[ -f "$auto_pw_file" ]]; then
        local pw
        pw=$(cat "$auto_pw_file")
        echo "  Password: $pw (auto-generated)"
    fi
}

_stop_vite() {
    if [[ -f "$VITE_PID_FILE" ]]; then
        local pid
        pid=$(cat "$VITE_PID_FILE")
        if kill -0 "$pid" 2>/dev/null; then
            echo "Stopping Vite (PID $pid)..."
            kill "$pid"
            sleep 0.5
            if kill -0 "$pid" 2>/dev/null; then
                kill -9 "$pid" 2>/dev/null || true
            fi
        fi
        rm -f "$VITE_PID_FILE"
    fi

    # Fallback: kill by port
    local pids=""
    if command -v ss >/dev/null 2>&1; then
        pids=$(ss -tlnp 2>/dev/null | grep ":$FRONTEND_PORT " | grep -oP 'pid=\K[0-9]+' | sort -u | tr '\n' ' ')
    fi
    if [[ -n "$pids" ]]; then
        echo "Killing orphan process on port $FRONTEND_PORT (PIDs: $pids)..."
        echo "$pids" | xargs kill -9 2>/dev/null || true
    fi
}

start_dev() {
    _stop_vite
    sleep 0.3

    mkdir -p .clawbench
    check_dev_port

    echo "=== Starting $NAME (dev mode) ==="
    echo "  Backend:  http://localhost:$DEV_HTTP_PORT (production dev port)"
    echo "  Frontend: http://localhost:$FRONTEND_PORT (Vite HMR)"
    echo ""

    # Start Vite dev server — proxy to production backend's dev HTTP port
    # Use setsid to create new session so Vite survives parent shell exit
    VITE_BACKEND_PORT=$DEV_HTTP_PORT VITE_BACKEND_PROTO=http VITE_FRONTEND_PORT=$FRONTEND_PORT setsid npx vite --port $FRONTEND_PORT > "$VITE_LOG" 2>&1 &
    VITE_PID=$!
    # setsid spawns a new process; find the actual Vite node process
    sleep 1
    # The PID we got is the setsid wrapper which may have exited;
    # find the real Vite process listening on the port
    REAL_PID=""
    if command -v ss >/dev/null 2>&1; then
        REAL_PID=$(ss -tlnp 2>/dev/null | grep ":$FRONTEND_PORT " | grep -oP 'pid=\K[0-9]+' | head -1)
    fi
    if [[ -n "$REAL_PID" ]]; then
        echo "$REAL_PID" > "$VITE_PID_FILE"
    else
        echo "$VITE_PID" > "$VITE_PID_FILE"
    fi

    sleep 1
    if ! kill -0 $(cat "$VITE_PID_FILE") 2>/dev/null; then
        echo "Failed to start Vite. Check $VITE_LOG" >&2
        rm -f "$VITE_PID_FILE"
        exit 1
    fi

    echo "Vite dev server started (PID $(cat "$VITE_PID_FILE")) on port $FRONTEND_PORT"
    echo ""

    show_auto_password
    echo "Open http://localhost:$FRONTEND_PORT in your browser"
    echo "Log: $VITE_LOG"
}

# Parse arguments
ACTION="start"
FOREGROUND=""
while [[ $# -gt 0 ]]; do
    case "$1" in
        --fg)
            FOREGROUND=1
            ;;
        --stop)
            ACTION=stop
            ;;
        --restart)
            ACTION=restart
            ;;
        *)
            echo "未知参数: $1"
            echo "用法: $0 [--fg] [--stop] [--restart]"
            exit 1
            ;;
    esac
    shift
done

case "$ACTION" in
    stop)
        echo "Stopping Vite..."
        _stop_vite
        echo "Done."
        ;;
    restart)
        echo "Restarting Vite..."
        _stop_vite
        sleep 1
        start_dev
        ;;
    start)
        if [[ -n "$FOREGROUND" ]]; then
            check_dev_port
            echo "=== Starting $NAME (dev mode, foreground) ==="
            echo "  Backend:  http://localhost:$DEV_HTTP_PORT (production dev port)"
            echo "  Frontend: http://localhost:$FRONTEND_PORT (Vite HMR)"
            echo ""
            VITE_BACKEND_PORT=$DEV_HTTP_PORT VITE_BACKEND_PROTO=http VITE_FRONTEND_PORT=$FRONTEND_PORT exec npx vite --port $FRONTEND_PORT
        else
            start_dev
        fi
        ;;
esac
