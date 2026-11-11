#!/usr/bin/env bash
#
# ClawBench 共享 Shell 工具函数
# 所有脚本通过 source 此文件来复用公共逻辑
#

# show_auto_password prints the auto-generated password from the given file,
# if it exists.
show_auto_password() {
    local auto_pw_file="$1"
    if [[ -f "$auto_pw_file" ]]; then
        local pw
        pw=$(cat "$auto_pw_file")
        echo "  Password: $pw (auto-generated, saved in $auto_pw_file)"
    fi
}

# check_binary ensures the Go binary exists, building it if necessary.
# Arguments:
#   $1 - BIN       path to the binary
#   $2 - CONFIG    path to the config file (optional, for future use)
#   $3 - BUILD_CMD command to build the binary (optional, defaults to go build)
check_binary() {
    local bin="$1"
    local config="${2:-}"
    local build_cmd="${3:-go build -o $bin ./cmd/server}"

    if [[ ! -f "$bin" ]]; then
        echo "Binary not found, building..."
        if command -v go >/dev/null 2>&1; then
            eval "$build_cmd"
        else
            echo "Error: Go not found and binary missing." >&2
            exit 1
        fi
    fi
}

# _pid_alive reports whether the given PID is a running (non-zombie) process.
#
# A zombie answers `kill -0` until its parent reaps it, so a plain existence
# check would keep a waiter blocked long after the process is actually gone —
# the same trap that makes the restart sentinel spin.
_pid_alive() {
    local pid="$1"
    kill -0 "$pid" 2>/dev/null || return 1
    if [[ -r "/proc/$pid/stat" ]]; then
        # Field 3 is the state, but field 2 (comm) may contain spaces and
        # parentheses — slice after the LAST ')' instead of splitting on spaces.
        local state
        state=$(sed -e 's/^.*) //' -e 's/ .*//' "/proc/$pid/stat" 2>/dev/null)
        [[ "$state" == "Z" ]] && return 1
    fi
    return 0
}

# _wait_pids_gone waits until every PID in $1 has exited, up to $2 seconds.
# Prints the PIDs still alive on timeout and returns 1; returns 0 when all are
# gone (nothing printed).
#
# Polling for process EXIT — rather than sleeping a fixed number of seconds or
# watching the port — is what makes a restart safe: the data-directory lock is
# released when the process exits, which happens long after the port is freed.
_wait_pids_gone() {
    local pids="$1"
    local timeout="${2:-30}"
    local ticks=0
    local pid alive
    while true; do
        alive=""
        for pid in $pids; do
            _pid_alive "$pid" && alive="$alive $pid"
        done
        if [[ -z "$alive" ]]; then
            return 0
        fi
        if [[ $ticks -ge $((timeout * 10)) ]]; then
            echo "$alive"
            return 1
        fi
        sleep 0.1
        ticks=$((ticks + 1))
    done
}

# _stop_servers stops processes tracked by the given PID file and/or by port.
# Arguments:
#   $1 - PID_FILE  path to the PID file (may be empty)
#   $2 - PORT      port to kill orphaned processes (may be empty)
#   $3 - NAME      display name for the service (optional, default "server")
#
# Returns only once the process has actually exited (or after SIGKILL), because
# callers start a replacement immediately afterwards.
_stop_servers() {
    local pid_file="$1"
    local port="$2"
    local name="${3:-server}"
    local grace="${STOP_GRACE_SECONDS:-30}"

    # Stop by PID file first
    if [[ -n "$pid_file" && -f "$pid_file" ]]; then
        local pid
        pid=$(cat "$pid_file")
        if _pid_alive "$pid"; then
            echo "Stopping $name (PID $pid)..."
            kill "$pid" 2>/dev/null || true
            local remaining=""
            if ! remaining=$(_wait_pids_gone "$pid" "$grace"); then
                echo "PID$remaining did not exit after ${grace}s, sending SIGKILL..."
                echo "$remaining" | xargs kill -9 2>/dev/null || true
                _wait_pids_gone "$remaining" 5 >/dev/null || true
            fi
        fi
        rm -f "$pid_file"
    fi

    # Fallback: kill by port (use ss/netstat — never block like lsof can)
    if [[ -n "$port" ]]; then
        local pids=""
        if command -v ss >/dev/null 2>&1; then
            pids=$(ss -tlnp 2>/dev/null | grep ":$port" | grep -oP 'pid=\K[0-9]+' | sort -u | tr '\n' ' ')
        elif command -v netstat >/dev/null 2>&1; then
            pids=$(netstat -tlnp 2>/dev/null | grep ":$port" | grep -oP '\s[0-9]+/' | grep -oP '[0-9]+' | sort -u | tr '\n' ' ')
        fi
        if [[ -n "$pids" ]]; then
            echo "Killing orphan process on port $port (PIDs:$pids)..."
            echo "$pids" | xargs kill 2>/dev/null || true
            local remaining=""
            if ! remaining=$(_wait_pids_gone "$pids" "$grace"); then
                echo "PIDs$remaining still alive after ${grace}s, sending SIGKILL..."
                echo "$remaining" | xargs kill -9 2>/dev/null || true
                _wait_pids_gone "$remaining" 5 >/dev/null || true
            fi
        fi

        # Wait for port to be fully released
        local waited=0
        while [[ $waited -lt 5 ]]; do
            local bound=""
            if command -v ss >/dev/null 2>&1; then
                bound=$(ss -tlnp 2>/dev/null | grep ":$port") || true
            fi
            if [[ -z "$bound" ]]; then
                break
            fi
            sleep 0.5
            waited=$((waited + 1))
        done
    fi
}