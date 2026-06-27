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
EMBEDDED_AGENT_ID="${EMBEDDED_AGENT_ID:-opencode}"
echo "Building binary with embedded agent: $EMBEDDED_AGENT_ID"
./build.sh --embed-agent="$EMBEDDED_AGENT_ID"

# Source download helper to read agent config
# shellcheck source=scripts/download-embedded-agent.sh
. ./scripts/download-embedded-agent.sh

# Prepare staging directory for embedded binary
rm -rf docker-staging
AGENT_SUBDIR=$(parse_embedded_agent_config "$EMBEDDED_AGENT_ID" subdir)
AGENT_CMD=$(parse_embedded_agent_config "$EMBEDDED_AGENT_ID" cmd)
mkdir -p "docker-staging/$AGENT_SUBDIR"
if [ -d ".clawbench/$AGENT_SUBDIR" ] && [ -f ".clawbench/$AGENT_SUBDIR/$AGENT_CMD" ]; then
    cp -r ".clawbench/$AGENT_SUBDIR/"* "docker-staging/$AGENT_SUBDIR/"
    echo "$EMBEDDED_AGENT_ID binary included in image (with dependencies)"
else
    echo "$EMBEDDED_AGENT_ID binary not found"
    echo "  (Run ./build.sh --embed-agent=$EMBEDDED_AGENT_ID to download it)"
fi

# Build and run via docker compose (staging dir must exist during build)
echo "Building and starting container on port ${PORT}..."
docker compose up -d --build 2>&1 | grep -v "^#" | grep -v "^$" || true

# Clean up staging (after compose build is done)
rm -rf docker-staging

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
