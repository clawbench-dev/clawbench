#!/usr/bin/env bash
set -e

NAME="clawbench"
DIST="dist"
ASSETS="assets"

# Load shared shell utilities
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/scripts/common.sh"

# Parse arguments
TARGET_OS=""
TARGET_ARCH=""
BUILD_ANDROID=""
DO_RESTART=""
RESTART_SKIP_BUILD=""
RESTART_PORT=""
RESTART_DETACHED=""
for arg in "$@"; do
    case "$arg" in
        --windows)
            TARGET_OS="windows"
            TARGET_ARCH="amd64"
            ;;
        --linux)
            TARGET_OS="linux"
            TARGET_ARCH="amd64"
            ;;
        --linux-arm64)
            TARGET_OS="linux"
            TARGET_ARCH="arm64"
            ;;
        --darwin)
            TARGET_OS="darwin"
            TARGET_ARCH="arm64"
            ;;
        --darwin-amd64)
            TARGET_OS="darwin"
            TARGET_ARCH="amd64"
            ;;
        --target=*)
            TARGET="${arg#--target=}"
            TARGET_OS="${TARGET%%/*}"
            TARGET_ARCH="${TARGET##*/}"
            ;;
        --android)
            BUILD_ANDROID=1
            ;;
        --restart)
            DO_RESTART=1
            ;;
        --restart-skip-build)
            DO_RESTART=1
            RESTART_SKIP_BUILD=1
            ;;
        --restart-port=*)
            RESTART_PORT="${arg#--restart-port=}"
            ;;
        --restart-detached)
            RESTART_DETACHED=1
            ;;
    esac
done

# Resolve restart port from config if not specified
_resolve_port() {
    local port
    port=$(grep "^port:" "$SCRIPT_DIR/config/config.yaml" 2>/dev/null | awk '{print $2}' | tr -d '"')
    echo "${port:-20000}"
}

echo "=== Building $NAME ==="

# Derive version from git (e.g. v1.0.0, v0.30.0-30-g830bb6c, or short SHA)
VERSION=$(git describe --tags --always 2>/dev/null || echo "dev")
# Detect release: git describe --exact-match succeeds only when HEAD is on a tag
IS_RELEASE=false
if git describe --tags --exact-match HEAD >/dev/null 2>&1; then
    IS_RELEASE=true
fi
# Build time suffix for dev builds: -mmddHHMM (e.g. -07291030)
BUILD_TIME_SUFFIX=$(date +"%m%d%H%M")
# Compose full version: dev builds append time suffix, release builds are clean
if $IS_RELEASE; then
    FULL_VERSION="$VERSION"
else
    FULL_VERSION="$VERSION-$BUILD_TIME_SUFFIX"
fi
LDFLAGS="-X 'clawbench/internal/version.Version=$FULL_VERSION'"
# Derive versionCode from git commit count (monotonically increasing for Play Store)
VERSION_CODE=$(git rev-list --count HEAD 2>/dev/null || echo "1")
echo "  Version: $FULL_VERSION (code: $VERSION_CODE, release: $IS_RELEASE)"

# 1. Build Vue frontend (must come before Go build so embed dir is populated)
echo "[1/5] Building Vue frontend..."
if [[ -n "$RESTART_SKIP_BUILD" ]]; then
    echo "  Frontend build skipped (--restart-skip-build)"
elif [ -f "package.json" ] && command -v npm >/dev/null 2>&1; then
    if [ ! -d "node_modules" ]; then
        echo "  Installing dependencies..."
        npm install || { echo "ERROR: npm install failed" >&2; exit 1; }
    fi
    # Clean all stale build output before rebuild.
    # Vite generates new hashed filenames each build but does not remove old ones,
    # so leftover chunks (diagram JS, CSS, fonts, etc.) accumulate indefinitely.
    # Preserve only index.html and assets/ (static user assets); Vite regenerates the rest.
    find public/ -maxdepth 1 -type f ! -name 'index.html' -delete 2>/dev/null || true
    npm run build || { echo "ERROR: npm run build failed" >&2; exit 1; }
    echo "  Frontend: public/"

    # Copy ALL frontend build output for Go embed (go:embed all:dist in internal/frontend/)
    # Vite outputs index.html, JS/CSS chunks, fonts (woff2), images (png),
    # manifest, service worker, etc. — all must be embedded.
    rm -rf internal/frontend/dist
    cp -r public internal/frontend/dist
    echo "  Frontend copied for embedding: internal/frontend/dist/"
else
    echo "  npm not found or no package.json, skipping frontend build"
    echo "  (Go binary will use empty embed — serve from disk public/ if available)"
fi

# 2. Build Android APK (optional, before Go build so APK is embedded)
if [ -n "$BUILD_ANDROID" ]; then
    echo "[2/5] Building Android APK..."
    if [ -d "android" ] && [ -f "android/gradlew" ]; then
        (cd android && JAVA_HOME=/usr/lib/jvm/java-17-openjdk-amd64 ./gradlew assembleRelease \
            -PversionCode=$VERSION_CODE -PversionName="$FULL_VERSION") || { echo "ERROR: Android APK build failed" >&2; exit 1; }
        echo "  APK: android/app/build/outputs/apk/release/clawbench-android.apk"
        if [ -f android/app/build/outputs/apk/release/clawbench-android.apk ]; then
            mkdir -p internal/frontend/dist/assets
            cp android/app/build/outputs/apk/release/clawbench-android.apk internal/frontend/dist/assets/
            echo "  APK copied for embedding: internal/frontend/dist/assets/"
        else
            echo "  Warning: APK not found at expected path, skipping copy"
        fi
    else
        echo "  Android project not found, skipping APK build"
    fi
else
    echo "[2/5] Android APK skipped (use --android to build)"
fi

# 3. Build Go backend (after frontend + APK so embed dir is populated)
# In restart mode with --restart-skip-build, skip this step
if [[ -n "$RESTART_SKIP_BUILD" ]]; then
    echo "[3/5] Go backend build skipped (--restart-skip-build)"
    BIN="$SCRIPT_DIR/$NAME"
    if [[ ! -f "$BIN" ]]; then
        echo "ERROR: Binary not found at $BIN. Run with build first." >&2
        exit 1
    fi
elif command -v go >/dev/null 2>&1; then
    echo "[3/5] Building Go backend..."
    if [ -n "$TARGET_OS" ] && [ -n "$TARGET_ARCH" ]; then
        BINARY_NAME="$NAME"
        if [ "$TARGET_OS" = "windows" ]; then
            BINARY_NAME="${NAME}.exe"
        fi
        GOOS=$TARGET_OS GOARCH=$TARGET_ARCH CGO_ENABLED=0 go build -ldflags "$LDFLAGS" -o "$BINARY_NAME" ./cmd/server || { echo "ERROR: Go cross-compile failed" >&2; exit 1; }
        echo "  Cross-compiled: $BINARY_NAME ($TARGET_OS/$TARGET_ARCH)"
    elif [ -n "$BUILD_ANDROID" ]; then
        # Android/Termux Go binary: must be PIE (interpreter /system/bin/linker64).
        # GOOS=android emits PIE automatically. wlynxg/anet (via frp -> pion) uses
        # go:linkname to net.zoneCache, which Go >= 1.23 rejects at link time, so
        # disable linkname checks for this target only.
        GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "-checklinkname=0 $LDFLAGS" -o "$NAME" ./cmd/server || { echo "ERROR: Go Android build failed" >&2; exit 1; }
        echo "  Go Android binary: ./$NAME (GOOS=android/arm64, PIE)"
    else
        go build -ldflags "$LDFLAGS" -o "$NAME" ./cmd/server || { echo "ERROR: Go build failed" >&2; exit 1; }
        echo "  Go binary: ./$NAME"
    fi
    # Build ACP mock agent binary (for E2E testing with ACP stdio transport)
    go build -o "acp-mock" ./cmd/acp-mock || { echo "ERROR: acp-mock build failed" >&2; exit 1; }
    echo "  ACP mock: ./acp-mock"
else
    echo "  Go not found, skipping backend build"
fi

# 4. (Skipped — embedded agent download removed)
echo "[4/5] Skipped (embedded agent download removed)"

echo ""
echo "=== Build complete ==="
if [ -n "$TARGET_OS" ] && [ -n "$TARGET_ARCH" ]; then
    BINARY_NAME="$NAME"
    [ "$TARGET_OS" = "windows" ] && BINARY_NAME="${NAME}.exe"
    echo "  ./$BINARY_NAME       # Go binary ($TARGET_OS/$TARGET_ARCH, frontend+APK embedded)"
else
    echo "  ./$NAME              # Go binary (frontend+APK embedded)"
fi
echo "  public/              # Frontend on disk (used if present, overrides embed)"

# === Restart logic ===
# Build runs in the foreground so compile errors are visible directly. Only
# the stop+start is delegated to a detached session: when running inside
# ClawBench's own PTY, killing ClawBench destroys the PTY and sends SIGHUP
# to every process in the old session. Detaching via setsid lets the restart
# survive the parent's death.
if [[ -n "$DO_RESTART" ]]; then
    PORT=$(_resolve_port)
    if [[ -n "$RESTART_PORT" ]]; then
        PORT="$RESTART_PORT"
    fi
    BIN="$SCRIPT_DIR/$NAME"
    LOG="$SCRIPT_DIR/.clawbench/build-and-restart.log"
    mkdir -p "$SCRIPT_DIR/.clawbench"

    # === Foreground mode: build already done, delegate stop+start ===
    if [[ -z "$RESTART_DETACHED" ]]; then
        echo ""
        echo "=== Restarting ClawBench ==="
        echo "  Port: $PORT"
        echo "  Log:  $LOG"
        echo "  Detaching restart into a new session..."
        setsid "$SCRIPT_DIR/build.sh" --restart --restart-detached --restart-skip-build \
            ${RESTART_PORT:+--restart-port=$RESTART_PORT} \
            >> "$LOG" 2>&1 &
        SETSID_PID=$!
        sleep 1
        if kill -0 "$SETSID_PID" 2>/dev/null; then
            echo "  Detached restart process started (PID $SETSID_PID)."
            echo "  The current terminal session will disconnect when ClawBench stops."
            echo "  Reconnect after restart completes."
        else
            echo "  ERROR: Failed to start detached restart process. Check $LOG"
            exit 1
        fi
        exit 0
    fi

    # === Detached session: stop old ClawBench + start new one ===
    echo ""
    echo "=== Restarting ClawBench ==="
    echo "  Port: $PORT"
    echo "  PID:  $$"
    echo "  Log:  $LOG"

    # Step 1: Stop current ClawBench process by port
    echo "[restart] Stopping current ClawBench on port $PORT..."
    _stop_servers "" "$PORT" "clawbench"

    # Wait for port to be fully released
    echo "[restart] Waiting for port $PORT to be released..."
    WAITED=0
    while [[ $WAITED -lt 30 ]]; do
        BOUND=""
        if command -v ss >/dev/null 2>&1; then
            BOUND=$(ss -tlnp 2>/dev/null | grep ":$PORT") || true
        fi
        if [[ -z "$BOUND" ]]; then
            break
        fi
        sleep 1
        WAITED=$((WAITED + 1))
    done
    if [[ $WAITED -ge 30 ]]; then
        echo "ERROR: Port $PORT still occupied after 30s. Aborting." >&2
        exit 1
    fi
    echo "[restart] Port $PORT released."

    # Step 2: Start new ClawBench in background
    echo "[restart] Starting ClawBench..."
    cd "$SCRIPT_DIR"

    PORT_ARGS=""
    if [[ -n "$RESTART_PORT" ]]; then
        PORT_ARGS="--port $RESTART_PORT"
    fi

    # setsid ensures the new ClawBench is in its own session, fully detached.
    # Strip the orphan markers inherited from the spawning shell: when build.sh
    # runs inside a ClawBench PTY, its environment carries CLAWBENCH_CHILD=1,
    # which would otherwise propagate to the new server and make it look like an
    # orphan AI subprocess to another instance's CleanupOrphans (leading to the
    # new server being killed on startup).
    env -u CLAWBENCH_CHILD -u CLAWBENCH_NO_SUPERVISOR setsid "$BIN" $PORT_ARGS >> "$LOG" 2>&1 &
    NEW_PID=$!
    echo "  New PID: $NEW_PID"

    # Sanity check: verify process is alive
    sleep 2
    if ! kill -0 "$NEW_PID" 2>/dev/null; then
        echo "ERROR: ClawBench process exited immediately. Check $LOG" >&2
        exit 1
    fi

    # Wait for port to bind (up to 15s)
    WAITED=0
    while [[ $WAITED -lt 15 ]]; do
        BOUND=""
        if command -v ss >/dev/null 2>&1; then
            BOUND=$(ss -tlnp 2>/dev/null | grep ":$PORT") || true
        fi
        if [[ -n "$BOUND" ]]; then
            break
        fi
        sleep 1
        WAITED=$((WAITED + 1))
    done

    if [[ $WAITED -ge 15 ]]; then
        echo "WARNING: Port $PORT not yet bound after 15s. Process may still be starting."
    else
        echo "[restart] ClawBench restarted successfully on port $PORT."
        show_auto_password "$SCRIPT_DIR/.clawbench/auto-password"
    fi
    echo "Done."
else
    echo ""
    echo "Run with: ./$NAME"
    echo ""
    echo "Build options:"
    echo "  ./build.sh --windows        # Windows amd64"
    echo "  ./build.sh --linux          # Linux amd64"
    echo "  ./build.sh --linux-arm64    # Linux arm64"
    echo "  ./build.sh --darwin         # macOS arm64 (Apple Silicon)"
    echo "  ./build.sh --darwin-amd64   # macOS amd64 (Intel)"
    echo "  ./build.sh --target=darwin/arm64"
    echo "  ./build.sh --android        # Android APK (release)"
    echo ""
    echo "Restart options (build + restart):"
    echo "  ./build.sh --restart            # Build and restart ClawBench"
    echo "  ./build.sh --restart-skip-build # Restart without rebuilding"
    echo "  ./build.sh --restart --restart-port=8080  # Restart on specific port"
    echo ""
fi
