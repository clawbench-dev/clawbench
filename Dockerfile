# ClawBench runtime image — runs the pre-built binary
#
# Build locally:
#   ./scripts/docker-build.sh
#
# Or manually:
#   docker build -t clawbench .
#   docker run -d --restart unless-stopped -p 20000:20000 -v clawbench-data:/data clawbench
#
# Pull from GitHub Container Registry:
#   docker pull ghcr.io/clawbench-dev/clawbench:latest
#   docker run -d --restart unless-stopped -p 20000:20000 -v clawbench-data:/data ghcr.io/clawbench-dev/clawbench:latest
#
# --restart always / unless-stopped is required for in-app upgrades: the
# container replaces its own binary and exits (code 0), and the restart policy
# brings the new version back up. `--restart on-failure` does NOT work — a
# graceful shutdown exits 0, so it never triggers. Prefer `docker pull` +
# recreate over an in-place upgrade.

FROM ubuntu:24.04

# Install runtime dependencies:
# - ca-certificates: HTTPS (LLM provider APIs, Edge TTS WebSocket)
# - curl, bash: general utilities + qoder install script
# - git: version control (agents may clone repos, users need vc in projects)
# - nodejs, npm: 11 of 12 AI agents install via `npm install -g`
# Edge TTS is compiled into the Go binary (github.com/lib-x/edgetts) — no Python needed.
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        bash \
        git \
        nodejs \
        npm \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy binary (frontend is embedded via go:embed)
COPY clawbench .

# Data directory (mounted as volume for persistence)
RUN mkdir -p /data/.clawbench

EXPOSE 20000

ENTRYPOINT ["./clawbench", "--port", "20000", "--data-dir", "/data/.clawbench"]
