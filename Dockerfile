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

# Copy OpenCode binary — multi-arch aware
# Local build: scripts/docker-build.sh populates docker-staging/
# CI build: release workflow passes OPENCODE_VERSION build arg; the RUN step
# below downloads the correct OpenCode binary for each target architecture.
ARG TARGETARCH
ARG OPENCODE_VERSION=""

# If OPENCODE_VERSION is set (CI), download the correct OpenCode binary for this architecture.
RUN if [ -n "$OPENCODE_VERSION" ]; then \
      OC_ARCH="x64"; \
      if [ "$TARGETARCH" = "arm64" ]; then OC_ARCH="arm64"; fi; \
      OC_URL="https://github.com/anomalyco/opencode/releases/download/v${OPENCODE_VERSION}/opencode-linux-${OC_ARCH}.tar.gz"; \
      mkdir -p .clawbench/opencode && \
      curl -sL "$OC_URL" | tar xzf - -C .clawbench/opencode && \
      chmod +x .clawbench/opencode/opencode && \
      echo "$OPENCODE_VERSION" > .clawbench/opencode/VERSION && \
      echo "OpenCode v${OPENCODE_VERSION} (${OC_ARCH}) downloaded"; \
    else \
      mkdir -p .clawbench; \
    fi

# Copy local docker-staging/ as fallback (local builds only; no-op in CI since OPENCODE_VERSION is set).
# When OPENCODE_VERSION is set above, the RUN step already populated .clawbench/opencode/,
# and this COPY overlays an empty directory (harmless).
COPY docker-staging/ .clawbench/

# Data directory (mounted as volume for persistence)
RUN mkdir -p /data/.clawbench

EXPOSE 20000

ENTRYPOINT ["./clawbench", "--port", "20000", "--data-dir", "/data/.clawbench"]
