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

# Node.js: copied from the official image instead of the distro package.
# Ubuntu 24.04's `nodejs` is v18.19.1 with npm 9, below the `>=22` most ACP
# adapters (and packages such as openai@7) now declare — installing them inside
# the container only emitted EBADENGINE warnings. Pinned to a minor release so a
# release build is reproducible; bump deliberately when a newer 22.x is needed.
ARG NODE_VERSION=22.23.1
FROM node:${NODE_VERSION}-bookworm-slim AS node

FROM ubuntu:24.04

# Install runtime dependencies:
# - ca-certificates: HTTPS (LLM provider APIs, Edge TTS WebSocket)
# - curl, bash: general utilities + qoder install script
# - git: version control (agents may clone repos, users need vc in projects)
# Node.js/npm come from the official Node image above (see the ARG). The 22+
# requirement comes from the ACP adapters' and agent packages' own engine
# fields; ClawBench's internal minimum is lower (nodeMinMajor in
# internal/platform/node_runtime.go), so a Node below 22 here is not a startup
# error — it only makes those global installs warn or misbehave.
# Edge TTS is compiled into the Go binary (github.com/lib-x/edgetts) — no Python needed.
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        bash \
        git \
    && rm -rf /var/lib/apt/lists/*

# Ship the official Node binary + npm (same glibc base: bookworm and Ubuntu 24.04
# are both Debian-family, so the binary links cleanly).
COPY --from=node /usr/local/bin/node /usr/local/bin/node
COPY --from=node /usr/local/lib/node_modules /usr/local/lib/node_modules
RUN ln -sf /usr/local/lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm && \
    ln -sf /usr/local/lib/node_modules/npm/bin/npx-cli.js /usr/local/bin/npx && \
    node --version && npm --version

WORKDIR /app

# Copy binary (frontend is embedded via go:embed)
COPY clawbench .

# Data directory (mounted as volume for persistence)
RUN mkdir -p /data/.clawbench

EXPOSE 20000

ENTRYPOINT ["./clawbench", "--port", "20000", "--data-dir", "/data/.clawbench"]
