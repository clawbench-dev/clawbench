# ClawBench runtime image — runs the pre-built binary
#
# Build locally:
#   ./scripts/docker-build.sh
#
# Or manually:
#   docker build -t clawbench .
#   docker run -p 20000:20000 -v clawbench-data:/data clawbench
#
# Pull from GitHub Container Registry:
#   docker pull ghcr.io/clawbench-dev/clawbench:latest
#   docker run -d -p 20000:20000 -v clawbench-data:/data ghcr.io/clawbench-dev/clawbench:latest

FROM ubuntu:24.04

# Install runtime dependencies:
# - ca-certificates: HTTPS (LLM provider APIs, Edge TTS WebSocket)
# Edge TTS is compiled into the Go binary (github.com/lib-x/edgetts) — no Python needed.
RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates curl bash && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy binary (frontend is embedded via go:embed)
COPY clawbench .

# Data directory (mounted as volume for persistence)
RUN mkdir -p /data/.clawbench

EXPOSE 20000

ENTRYPOINT ["./clawbench", "--port", "20000", "--data-dir", "/data/.clawbench"]
