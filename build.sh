#!/usr/bin/env bash
set -e

NAME="clawbench"
DIST="dist"
ASSETS="assets"

# Parse arguments
TARGET_OS=""
TARGET_ARCH=""
BUILD_ANDROID=""
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
    esac
done

echo "=== Building $NAME ==="

# Derive version from git (e.g. v1.0.0, v0.30.0-30-g830bb6c, or short SHA)
VERSION=$(git describe --tags --always 2>/dev/null || echo "dev")
# Detect release: git describe --exact-match succeeds only when HEAD is on a tag
IS_RELEASE=false
if git describe --tags --exact-match HEAD >/dev/null 2>&1; then
    IS_RELEASE=true
fi
# Build time (fixed at script start, shared by backend and APK)
BUILD_TIME=$(date +"%Y-%m-%d %H:%M:%S")
# Compose full version: dev builds include build time, release builds are clean
if $IS_RELEASE; then
    FULL_VERSION="$VERSION"
else
    FULL_VERSION="$VERSION ($BUILD_TIME)"
fi
LDFLAGS="-X 'clawbench/internal/version.Version=$FULL_VERSION'"
# Derive versionCode from git commit count (monotonically increasing for Play Store)
VERSION_CODE=$(git rev-list --count HEAD 2>/dev/null || echo "1")
echo "  Version: $FULL_VERSION (code: $VERSION_CODE, release: $IS_RELEASE)"

# 1. Build Vue frontend (must come before Go build so embed dir is populated)
echo "[1/5] Building Vue frontend..."
if [ -f "package.json" ] && command -v npm >/dev/null 2>&1; then
    if [ ! -d "node_modules" ]; then
        echo "  Installing dependencies..."
        npm install
    fi
    # Clean stale hashed assets before rebuild (index-*.js, index-*.css, manifest-*.json)
    find public/ -maxdepth 1 -name 'index-*.js' -o -name 'index-*.css' -o -name 'manifest-*.json' | xargs rm -f 2>/dev/null || true
    npm run build
    echo "  Frontend: public/"

    # Copy frontend build output for Go embed (go:embed all:dist in internal/frontend/)
    # Include ALL JS/CSS files, not just index-* — Vite dynamic imports produce
    # lazy chunks (e.g. pdf-*.js, dagre-*.js, mermaid diagram chunks).
    rm -rf internal/frontend/dist
    mkdir -p internal/frontend/dist
    # Restore .gitkeep so go:embed works on fresh clone
    touch internal/frontend/dist/.gitkeep
    cp public/index.html internal/frontend/dist/
    cp public/*.js public/*.css internal/frontend/dist/ 2>/dev/null || true
    cp public/manifest-*.json public/sw.js internal/frontend/dist/ 2>/dev/null || true
    cp -r public/assets internal/frontend/dist/assets 2>/dev/null || true
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
            -PversionCode=$VERSION_CODE -PversionName="$FULL_VERSION")
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
echo "[3/5] Building Go backend..."

if command -v go >/dev/null 2>&1; then
    if [ -n "$TARGET_OS" ] && [ -n "$TARGET_ARCH" ]; then
        BINARY_NAME="$NAME"
        if [ "$TARGET_OS" = "windows" ]; then
            BINARY_NAME="${NAME}.exe"
        fi
        GOOS=$TARGET_OS GOARCH=$TARGET_ARCH go build -ldflags "$LDFLAGS" -o "$BINARY_NAME" ./cmd/server
        echo "  Cross-compiled: $BINARY_NAME ($TARGET_OS/$TARGET_ARCH)"
    else
        go build -ldflags "$LDFLAGS" -o "$NAME" ./cmd/server
        echo "  Go binary: ./$NAME"
    fi
    # Build ACP mock agent binary (for E2E testing with ACP stdio transport)
    if command -v go >/dev/null 2>&1; then
        go build -o "acp-mock" ./cmd/acp-mock
        echo "  ACP mock: ./acp-mock"
    fi
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
echo ""
echo "Run with: ./$NAME"
echo ""
echo "Cross-compile targets:"
echo "  ./build.sh --windows        # Windows amd64"
echo "  ./build.sh --linux          # Linux amd64"
echo "  ./build.sh --darwin         # macOS arm64 (Apple Silicon)"
echo "  ./build.sh --darwin-amd64   # macOS amd64 (Intel)"
echo "  ./build.sh --target=darwin/arm64"
echo "  ./build.sh --android          # Android APK (release)"
echo ""
