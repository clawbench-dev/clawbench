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
    apt-get install -y --no-install-recommends ca-certificates curl && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy binary and frontend
COPY clawbench .
COPY public/ ./public/

# Copy embedded agent download helper and config
COPY scripts/download-embedded-agent.sh ./scripts/download-embedded-agent.sh
COPY embedded-agents.yaml ./embedded-agents.yaml

# Copy embedded agent binary — multi-arch aware
# Local build: scripts/docker-build.sh populates docker-staging/
# CI build: passes EMBEDDED_AGENT_ID + EMBEDDED_AGENT_VERSION build args; the RUN step
# below downloads the correct agent binary for each target architecture.
ARG TARGETARCH
ARG EMBEDDED_AGENT_ID=""
ARG EMBEDDED_AGENT_VERSION=""
# Backward compat: OPENCODE_VERSION maps to EMBEDDED_AGENT_ID=opencode
# If both OPENCODE_VERSION and EMBEDDED_AGENT_* are set, EMBEDDED_AGENT_* takes precedence.
ARG OPENCODE_VERSION=""

# If EMBEDDED_AGENT_VERSION is set (CI), download the correct agent binary for this architecture.
RUN if [ -n "$EMBEDDED_AGENT_ID" ] && [ -n "$EMBEDDED_AGENT_VERSION" ]; then \
      . ./scripts/download-embedded-agent.sh && \
      download_embedded_agent_for_docker "$EMBEDDED_AGENT_ID" "$EMBEDDED_AGENT_VERSION"; \
    elif [ -n "$OPENCODE_VERSION" ]; then \
      . ./scripts/download-embedded-agent.sh && \
      download_embedded_agent_for_docker opencode "$OPENCODE_VERSION"; \
    else \
      mkdir -p .clawbench; \
    fi

# Copy local docker-staging/ as fallback (local builds only; no-op in CI when version is set).
# When a version is set above, the RUN step already populated .clawbench/,
# and this COPY overlays an empty directory (harmless).
COPY docker-staging/ .clawbench/

# Data directory (mounted as volume for persistence)
RUN mkdir -p /data/.clawbench

EXPOSE 20000

ENTRYPOINT ["./clawbench", "--port", "20000", "--data-dir", "/data/.clawbench"]
