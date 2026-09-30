#!/usr/bin/env bash
# Ensure the budtmo Android emulator base image exists locally.
#
# Why not just `docker pull`: this environment's docker CLI talks to a *host*
# daemon whose configured HTTP proxy resets every external HTTPS CONNECT, so
# `docker pull budtmo/docker-android` fails with "connection reset by peer".
# Registry mirrors are reachable from inside containers, but throttle each
# connection to ~40 KB/s — too slow for this 2.65 GB image in series.
#
# So: run the parallel range-request puller in a throwaway container (fast direct
# egress), `docker cp` the tarball out, and `docker load` it. Expect ~6 minutes.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
IMAGE="${E2E_BASE_IMAGE:-budtmo/docker-android:emulator_9.0}"
CONNS="${E2E_PULL_CONNS:-24}"

# Registry mirrors to try, in order, for each image. A mirror can serve a
# corrupt or truncated blob (measured: CI run 36567786806, node:20-alpine's
# config blob came back with a wrong digest) — and it can also be down entirely.
# The puller retries a bad blob against the SAME mirror, so a mirror that is
# persistently wrong would still fail; the fallback below is what covers that.
#
# Reachability measured 2026-09-29 from this environment's build containers:
# daocloud and docker.1ms.run answer and serve correct bytes; dockerproxy.net
# times out on TLS and the rest (tencentyun/ustc/163/registry-1.docker.io) do not
# resolve. Do not reorder blindly — verify a candidate actually serves the image.
#
# E2E_REGISTRY_MIRROR overrides the list with a single mirror (back-compat and
# for pinning one source deliberately).
if [[ -n "${E2E_REGISTRY_MIRROR:-}" ]]; then
  MIRRORS=("$E2E_REGISTRY_MIRROR")
else
  MIRRORS=(
    "https://docker.m.daocloud.io"
    "https://docker.1ms.run"
  )
fi

# Pull an image client-side into the daemon, because the daemon's proxy resets
# every external HTTPS CONNECT (so `docker pull` fails) while containers have fast
# direct egress to registry mirrors.
#
#   pull_image <repo> <tag> <os/arch> <local-name> <conns>
pull_image() {
  local repo="$1" tag="$2" platform="$3" local_name="$4" conns="$5"

  # `docker image inspect` can fail for a tag that `docker images` shows, when the
  # tag exists only in the containerd image store; re-tag by ID to be sure.
  if ! docker image inspect "$local_name" >/dev/null 2>&1; then
    local id
    id="$(docker images --format '{{.ID}} {{.Repository}}:{{.Tag}}' \
            | awk -v n="$local_name" '$2==n {print $1; exit}')"
    [[ -n "$id" ]] && docker tag "$id" "$local_name" || true
  fi
  if docker image inspect "$local_name" >/dev/null 2>&1; then
    echo "[pull-image] $local_name already present"
    return 0
  fi

  echo "[pull-image] $local_name not found locally; pulling ($conns conns)"
  # Must be *running* (not merely created) so the apk-install loop can `docker exec`.
  # `/out` is the puller's tarball destination and does not exist in a fresh
  # alpine container, so it is created as part of container startup — without it
  # the pull downloads every blob and then dies in `tarfile.open("/out/...")`.
  local cid
  cid="$(docker run -d alpine:latest sh -c 'mkdir -p /out; apk add --no-cache python3 curl >/dev/null 2>&1; sleep 7200')"

  docker cp "$HERE/oci-pull-parallel.py" "$cid:/pull.py"
  for _ in $(seq 1 40); do
    if docker exec "$cid" sh -c 'command -v python3 >/dev/null && command -v curl >/dev/null' 2>/dev/null; then
      break
    fi
    sleep 2
  done

  local rc=1 load_out="" mirror
  for mirror in "${MIRRORS[@]}"; do
    mirror="${mirror%/}"
    echo "[pull-image]   trying $mirror"
    docker exec "$cid" rm -f /out/image.tar 2>/dev/null || true
    rc=0
    docker exec "$cid" python3 /pull.py "$mirror" "$repo" "$tag" "$platform" \
      /out/image.tar "$conns" "$local_name" || rc=$?
    if [[ "$rc" == "0" ]]; then
      local tmp
      tmp="$(mktemp -t clawbench-e2e-image.XXXXXX.tar)"
      docker cp "$cid:/out/image.tar" "$tmp"
      load_out="$(docker load -i "$tmp")"
      rm -f "$tmp"
      echo "$load_out"
      break
    fi
    echo "[pull-image]   $mirror failed (rc=$rc); trying next mirror" >&2
  done

  docker rm -f "$cid" >/dev/null 2>&1 || true
  [[ "$rc" == "0" ]] || { echo "ERROR: image pull failed for $local_name (tried: ${MIRRORS[*]})" >&2; return "$rc"; }

  # `docker load` can register the image only under its ID (the containerd image
  # store makes `docker image inspect <tag>` fail even though `docker images`
  # lists the tag), so re-tag explicitly using the ID `docker load` reported.
  if ! docker image inspect "$local_name" >/dev/null 2>&1; then
    local id
    id="$(printf '%s\n' "$load_out" | sed -n 's/^Loaded image ID: //p' | head -1)"
    if [[ -z "$id" ]]; then
      id="$(docker images --format '{{.ID}} {{.Repository}}:{{.Tag}}' \
              | awk -v n="$local_name" '$2==n {print $1; exit}')"
    fi
    [[ -n "$id" ]] || { echo "ERROR: loaded image but cannot resolve $local_name" >&2; return 1; }
    docker tag "$id" "$local_name"
  fi
  echo "[pull-image] ready: $local_name"
}

pull_image budtmo/docker-android emulator_9.0 linux/amd64 "$IMAGE" "$CONNS"
# node:20-alpine backs the runner and mock images and is equally unpullable here.
pull_image library/node 20-alpine linux/amd64 node:20-alpine 16

echo "[pull-image] images ready"
