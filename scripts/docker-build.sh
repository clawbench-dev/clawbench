#!/usr/bin/env bash
set -e

# One-step Docker build & run for ClawBench
#
# Usage:
#   ./scripts/docker-build.sh           # build + run (port 20000)
#   ./scripts/docker-build.sh --stop    # stop and remove container
#   ./scripts/docker-build.sh --clean   # stop + remove container + volume

PORT=20000
NAME="clawbench"

# Stop existing container
if docker ps -a --format '{{.Names}}' | grep -q "^${NAME}$"; then
    echo "Stopping existing container..."
    docker stop "$NAME" >/dev/null && docker rm "$NAME" >/dev/null
fi

# Handle --stop / --clean
if [ "$1" = "--stop" ]; then
    echo "Container stopped."
    exit 0
fi

if [ "$1" = "--clean" ]; then
    echo "Removing volume..."
    docker volume rm clawbench_clawbench-data 2>/dev/null || true
    echo "Clean complete."
    exit 0
fi

# Build binary (always rebuild to pick up latest code)
echo "Building binary..."
./build.sh

# Build and run via docker compose
echo "Building and starting container on port ${PORT}..."
docker compose up -d --build 2>&1 | grep -v "^#" | grep -v "^$" || true

# Wait for server to start
sleep 3
echo ""
echo "=== Server logs ==="
docker logs "$NAME" 2>&1 | tail -5

# Extract auto-password
echo ""
PASS=$(docker exec "$NAME" cat /data/.clawbench/auto-password 2>/dev/null || docker exec "$NAME" cat /app/.clawbench/auto-password 2>/dev/null || echo "")
if [ -n "$PASS" ]; then
    echo "╔══════════════════════════════════════╗"
    echo "║  Auto-generated password: $PASS  ║"
    echo "╚══════════════════════════════════════╝"
else
    echo "No password configured (open access)"
fi

echo ""
echo "Access: http://localhost:${PORT}"
