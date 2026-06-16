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

# Copy Pi binary for setup wizard — multi-arch aware
# Local build: scripts/docker-build.sh populates docker-staging/
# CI build: release workflow passes PI_VERSION build arg; the RUN step
# below downloads the correct Pi binary for each target architecture.
ARG TARGETARCH
ARG PI_VERSION=""

# If PI_VERSION is set (CI), download the correct Pi binary for this architecture.
# If not set (local build without --with-pi), fall back to docker-staging/ contents.
RUN if [ -n "$PI_VERSION" ]; then \
      PI_ARCH="x64"; \
      if [ "$TARGETARCH" = "arm64" ]; then PI_ARCH="arm64"; fi; \
      PI_URL="https://github.com/earendil-works/pi/releases/download/v${PI_VERSION}/pi-linux-${PI_ARCH}.tar.gz"; \
      mkdir -p .clawbench/pi && \
      curl -sL "$PI_URL" | tar xzf - -C .clawbench/pi --strip-components=1 && \
      chmod +x .clawbench/pi/pi && \
      echo "$PI_VERSION" > .clawbench/pi/VERSION && \
      echo "Pi v${PI_VERSION} (${PI_ARCH}) downloaded"; \
    else \
      mkdir -p .clawbench; \
    fi

# Copy local docker-staging/ as fallback (local builds only; no-op in CI since PI_VERSION is set).
# When PI_VERSION is set above, the RUN step already populated .clawbench/pi/,
# and this COPY overlays an empty directory (harmless).
COPY docker-staging/ .clawbench/

# Data directory (mounted as volume for persistence)
RUN mkdir -p /data/.clawbench

EXPOSE 20000

ENTRYPOINT ["./clawbench", "--port", "20000", "--data-dir", "/data/.clawbench"]
