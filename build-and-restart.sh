#!/usr/bin/env bash
#
# ClawBench 编译 + 后台重启脚本
#
# 用法:
#   ./build-and-restart.sh              # 编译并后台重启 ClawBench
#   ./build-and-restart.sh --skip-build # 跳过编译，仅重启
#   ./build-and-restart.sh --port 8080  # 指定重启后的端口
#
# 原理:
#   1. 编译新二进制（调用 ./build.sh）
#   2. 杀死当前 ClawBench 进程（按端口查找）
#   3. 等待端口释放
#   4. 后台启动新二进制
#
#   脚本可以从 ClawBench 的 Web 终端执行（即「在 ClawBench 内重启
#   ClawBench 自己」）。为此脚本会在杀死旧进程之前通过 setsid 将
#   自身脱离到新会话组，确保旧进程退出时脚本不会被 SIGHUP 杀死。

set -e

# Load shared shell utilities
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/scripts/common.sh"

SKIP_BUILD=""
CLI_PORT=""
DETACHED=""
for arg in "$@"; do
    case "$arg" in
        --skip-build)  SKIP_BUILD=1 ;;
        --port=*)      CLI_PORT="${arg#--port=}" ;;
        --detached)    DETACHED=1 ;;
        *)
            echo "Unknown argument: $arg"
            echo "Usage: $0 [--skip-build] [--port PORT]"
            exit 1
            ;;
    esac
done

# Resolve port from config
_resolve_port() {
    local port
    port=$(grep "^port:" "$SCRIPT_DIR/config/config.yaml" 2>/dev/null | awk '{print $2}' | tr -d '"')
    echo "${port:-20000}"
}

PORT=$(_resolve_port)
if [[ -n "$CLI_PORT" ]]; then
    PORT="$CLI_PORT"
fi

BIN="$SCRIPT_DIR/clawbench"
LOG="$SCRIPT_DIR/.clawbench/build-and-restart.log"

mkdir -p "$SCRIPT_DIR/.clawbench"

# If not yet detached, re-launch self via setsid into a new session group.
# This is essential when running inside ClawBench's PTY: killing ClawBench
# destroys the PTY, sending SIGHUP to all processes in the old session.
# By moving into a new session first, we survive the parent's death.
if [[ -z "$DETACHED" ]]; then
    echo "=== ClawBench Build & Restart ==="
    echo "  Detaching into new session..."
    # Re-launch ourselves with --detached flag; setsid puts us in a new
    # session group so we won't get SIGHUP when the parent process dies.
    # Redirect all output to the log file since the current PTY will die.
    setsid "$SCRIPT_DIR/build-and-restart.sh" --detached \
        ${SKIP_BUILD:+--skip-build} \
        ${CLI_PORT:+--port=$CLI_PORT} \
        >> "$LOG" 2>&1 &
    SETSID_PID=$!
    # Give the detached script a moment to start, then report back
    sleep 1
    if kill -0 "$SETSID_PID" 2>/dev/null; then
        echo "  Detached process started (PID $SETSID_PID)."
        echo "  Log: $LOG"
        echo "  The current terminal session will disconnect when ClawBench stops."
        echo "  Reconnect after restart completes."
    else
        echo "  ERROR: Failed to start detached process. Check $LOG"
        exit 1
    fi
    exit 0
fi

# === From here, we are in a detached session — the parent PTY may die ===

echo "=== ClawBench Build & Restart (detached) ==="
echo "  Port: $PORT"
echo "  PID:  $$"
echo "  Log:  $LOG"

# Step 1: Build (unless skipped)
if [[ -z "$SKIP_BUILD" ]]; then
    echo "[1/3] Building..." >> "$LOG"
    cd "$SCRIPT_DIR"
    if ! ./build.sh >> "$LOG" 2>&1; then
        echo "ERROR: Build failed. Aborting restart." >> "$LOG"
        exit 1
    fi
    echo "[1/3] Build complete." >> "$LOG"
else
    echo "[1/3] Build skipped (--skip-build)." >> "$LOG"
    if [[ ! -f "$BIN" ]]; then
        echo "ERROR: Binary not found at $BIN. Run with build first." >> "$LOG"
        exit 1
    fi
fi

# Step 2: Stop current ClawBench process by port
echo "[2/3] Stopping current ClawBench on port $PORT..." >> "$LOG"
_stop_servers "" "$PORT" "clawbench"

# Wait for port to be fully released
echo "[2/3] Waiting for port $PORT to be released..." >> "$LOG"
WAITED=0
while [[ $WAITED -lt 30 ]]; do
    BOUND=""
    if command -v ss >/dev/null 2>&1; then
        BOUND=$(ss -tlnp 2>/dev/null | grep ":$PORT") || true
    fi
    if [[ -z "$BOUND" ]]; then
        break
    fi
    sleep 1
    WAITED=$((WAITED + 1))
done
if [[ $WAITED -ge 30 ]]; then
    echo "ERROR: Port $PORT still occupied after 30s. Aborting." >> "$LOG"
    exit 1
fi
echo "[2/3] Port $PORT released." >> "$LOG"

# Step 3: Start new ClawBench in background
echo "[3/3] Starting ClawBench..." >> "$LOG"
cd "$SCRIPT_DIR"

PORT_ARGS=""
if [[ -n "$CLI_PORT" ]]; then
    PORT_ARGS="--port $CLI_PORT"
fi

# setsid ensures the new ClawBench is in its own session, fully detached
setsid "$BIN" $PORT_ARGS >> "$LOG" 2>&1 &
NEW_PID=$!
echo "  New PID: $NEW_PID" >> "$LOG"

# Sanity check: verify process is alive
sleep 2
if ! kill -0 "$NEW_PID" 2>/dev/null; then
    echo "ERROR: ClawBench process exited immediately. Check $LOG" >> "$LOG"
    exit 1
fi

# Wait for port to bind (up to 15s)
WAITED=0
while [[ $WAITED -lt 15 ]]; do
    BOUND=""
    if command -v ss >/dev/null 2>&1; then
        BOUND=$(ss -tlnp 2>/dev/null | grep ":$PORT") || true
    fi
    if [[ -n "$BOUND" ]]; then
        break
    fi
    sleep 1
    WAITED=$((WAITED + 1))
done

if [[ $WAITED -ge 15 ]]; then
    echo "WARNING: Port $PORT not yet bound after 15s. Process may still be starting." >> "$LOG"
else
    echo "[3/3] ClawBench restarted successfully on port $PORT." >> "$LOG"
    # Show password info in log
    show_auto_password "$SCRIPT_DIR/.clawbench/auto-password" >> "$LOG" 2>&1
fi

echo "Done." >> "$LOG"
