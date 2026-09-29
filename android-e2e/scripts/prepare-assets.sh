#!/usr/bin/env bash
# Stage build inputs for the emulator image into android-e2e/assets/.
#
# Why staging at all: this environment's docker CLI talks to a *host* daemon, so
# bind mounts resolve on the host filesystem and come up empty. `docker build`
# streams the build context from the client instead, so anything the emulator
# image needs (APK, chromedriver) must live inside the build context.
#
# Two artifacts:
#   assets/clawbench-android-debug.apk  — the app under test (from the Gradle build)
#   assets/chromedriver-244             — Chrome-69-capable chromedriver for the
#                                         API-28 WebView (see chromedriver-shim.py)
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
E2E_ROOT="$(cd "$HERE/.." && pwd)"
REPO_ROOT="$(cd "$E2E_ROOT/.." && pwd)"
ASSETS="$E2E_ROOT/assets"

# The real-server binary is only needed for Tier 2. Building it costs a full Go
# link (~10-20s), so Tier 1 skips it.
WITH_SERVER=0
for arg in "$@"; do
  case "$arg" in
    --with-server) WITH_SERVER=1 ;;
    *) echo "unknown arg: $arg" >&2; exit 2 ;;
  esac
done

mkdir -p "$ASSETS"

# ---------------------------------------------------------------- APK ---------
APK_SRC="${E2E_APK_SRC:-$REPO_ROOT/android/app/build/outputs/apk/debug/clawbench-android-debug.apk}"
if [[ ! -f "$APK_SRC" ]]; then
  echo "ERROR: APK not found at $APK_SRC" >&2
  echo "       build it first:  (cd android && JAVA_HOME=/usr/lib/jvm/java-17-openjdk-amd64 ./gradlew assembleDebug)" >&2
  exit 1
fi
cp -f "$APK_SRC" "$ASSETS/clawbench-android-debug.apk"
echo "[assets] APK      $(du -h "$ASSETS/clawbench-android-debug.apk" | cut -f1)  <- $APK_SRC"

# ---------------------------------------------------------- chromedriver ------
# Chrome 69 is only supported by chromedriver 2.42-2.44. 2.44 is the newest of
# those and also speaks JSONWP sessions, which the shim needs.
CD_DEST="$ASSETS/chromedriver-244"
if [[ -x "$CD_DEST" ]] && "$CD_DEST" --version 2>/dev/null | grep -q "2\.44"; then
  echo "[assets] chromedriver already present: $("$CD_DEST" --version)"
else
  echo "[assets] downloading chromedriver 2.44 (Chrome 69 support)..."
  TMP="$(mktemp -d)"
  trap 'rm -rf "$TMP"' EXIT
  # Primary source; npmmirror binary mirror is the fallback.
  for url in \
    "https://chromedriver.storage.googleapis.com/2.44/chromedriver_linux64.zip" \
    "https://registry.npmmirror.com/-/binary/chromedriver/2.44/chromedriver_linux64.zip"; do
    echo "[assets]   trying $url"
    if curl -fsSL --max-time 120 -o "$TMP/cd.zip" "$url"; then
      break
    fi
    rm -f "$TMP/cd.zip"
  done
  [[ -f "$TMP/cd.zip" ]] || { echo "ERROR: could not download chromedriver 2.44" >&2; exit 1; }
  unzip -o -q "$TMP/cd.zip" -d "$TMP"
  mv -f "$TMP/chromedriver" "$CD_DEST"
  chmod 0755 "$CD_DEST"
  echo "[assets] chromedriver $("$CD_DEST" --version)"
fi

# --------------------------------------------------- real server (Tier 2) -----
# Tier 2 needs a REAL clawbench server: the mock is half-duplex and cannot carry
# the tunnel's full-duplex streams (docs/plans/2026-09-25-h2-tunnel-implementation.md,
# T14). Built here rather than in the Dockerfile so the toolchain stays on the
# host, and so the version can be pinned from the APK's own metadata.
#
# VERSION PINNING IS LOAD-BEARING. On login, native does POST /login, then GET
# /api/health and compares the reported version against the APK's versionName
# (MainActivity.gateVersionMismatchAndProceed -> VersionCompare.shouldShowMismatch).
# When appVersion < serverVersion the app shows a BLOCKING dialog and the test
# hangs. The comparison is a numeric dotted-core compare that ignores the
# `-<distance>-g<hash>` suffix, so only the vX.Y.Z base must agree — but pinning
# the *exact* APK versionName (from output-metadata.json, the same source
# run.sh uses for the Tier 1 mock) makes the two provably identical rather than
# merely compatible.
if [[ "$WITH_SERVER" != "1" ]]; then
  echo "[assets] skipping real server build (Tier 2 only; pass --with-server)"
else
GO_BIN="${GO_BIN:-/usr/local/go/bin/go}"
if [[ ! -x "$GO_BIN" ]]; then
  GO_BIN="$(command -v go || true)"
fi
if [[ -z "$GO_BIN" || ! -x "$GO_BIN" ]]; then
  echo "ERROR: Go toolchain not found (set GO_BIN); Tier 2 needs a real server binary" >&2
  exit 1
fi

# The server version to report on /api/health. Prefer the APK's versionName (the
# thing the gate compares against); fall back to `git describe` so the script
# still works standalone.
SERVER_VERSION="${CLAWBENCH_VERSION:-}"
if [[ -z "$SERVER_VERSION" ]]; then
  SERVER_VERSION="$(git -C "$REPO_ROOT" describe --tags --always 2>/dev/null || echo dev)"
fi
echo "[assets] building real server binary (version=$SERVER_VERSION)..."

# CGO_ENABLED=0 -> a static binary that runs on the minimal alpine base. The
# embed directory must be non-empty or `go:embed all:dist` fails to compile; the
# real frontend build output (.clawbench-web/) is staged into it when present
# (build.sh does the same), otherwise a .gitkeep is enough to compile.
#
# Tier 2 DOES assert on the SPA: tunnel.server.mjs requires `#app` to exist on
# the server's root ("the served page has no #app root — not the SPA shell").
# The `.gitkeep` branch below is therefore only a compile stub for a tree that
# has no frontend build — a real Tier 2 run must provide .clawbench-web/, which
# is why the CI job builds the frontend before this script runs.
if [[ -f "$REPO_ROOT/.clawbench-web/index.html" ]]; then
  rm -rf "$REPO_ROOT/internal/frontend/dist"
  cp -r "$REPO_ROOT/.clawbench-web" "$REPO_ROOT/internal/frontend/dist"
else
  mkdir -p "$REPO_ROOT/internal/frontend/dist"
  touch "$REPO_ROOT/internal/frontend/dist/.gitkeep"
fi

( cd "$REPO_ROOT" && \
  CGO_ENABLED=0 "$GO_BIN" build \
    -ldflags "-X clawbench/internal/version.Version=$SERVER_VERSION" \
    -o "$ASSETS/clawbench-server" ./cmd/server )
chmod 0755 "$ASSETS/clawbench-server"
echo "[assets] real server $(du -h "$ASSETS/clawbench-server" | cut -f1)  version=$SERVER_VERSION"
fi

echo "[assets] staged in $ASSETS:"
ls -la "$ASSETS"
