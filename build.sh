#!/usr/bin/env bash
set -e

NAME="clawbench"
DIST="dist"
ASSETS="assets"

# Parse arguments
TARGET_OS=""
TARGET_ARCH=""
BUILD_ANDROID=""
DOWNLOAD_OPENCODE=""
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
        --with-opencode)
            DOWNLOAD_OPENCODE=1
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

# 1. Build Go backend
echo "[2/5] Building Go backend..."

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

# 1.5 Download OpenCode binary (embedded agent)
# Default: skip. Use --with-opencode to download, or set OPENCODE_VERSION to pin a version.
# Without OPENCODE_VERSION, the latest release is fetched from GitHub API automatically.
OC_DIR=".clawbench/opencode"
if [ -n "$DOWNLOAD_OPENCODE" ]; then
    # Resolve OpenCode version: env var override > auto-detect latest from GitHub
    if [ -z "$OPENCODE_VERSION" ]; then
        OPENCODE_VERSION=$(curl -sI https://github.com/anomalyco/opencode/releases/latest 2>/dev/null | grep -i "^location:" | sed 's|.*/tag/v||' | tr -d '[:space:]')
        if [ -z "$OPENCODE_VERSION" ]; then
            echo "  ERROR: Could not detect latest OpenCode version. Set OPENCODE_VERSION manually."
            exit 1
        fi
        echo "  Auto-detected latest OpenCode version: v${OPENCODE_VERSION}"
    fi
    echo "[3/5] Downloading OpenCode v${OPENCODE_VERSION}..."
    # Determine platform for OpenCode binary
    if [ -n "$TARGET_OS" ] && [ -n "$TARGET_ARCH" ]; then
        case "$TARGET_OS" in
            linux)   OC_PLATFORM="linux-$TARGET_ARCH" ;;
            darwin)  OC_PLATFORM="darwin-$TARGET_ARCH" ;;
            windows) OC_PLATFORM="windows-$TARGET_ARCH" ;;
            *)       OC_PLATFORM="" ;;
        esac
        # OpenCode uses "x64" not "amd64" in its archive names
        OC_PLATFORM="${OC_PLATFORM/amd64/x64}"
    else
        OC_PLATFORM="$(uname -s | tr '[:upper:]' '[:lower:]')-$(uname -m)"
        OC_PLATFORM="${OC_PLATFORM/x86_64/x64}"
        OC_PLATFORM="${OC_PLATFORM/aarch64/arm64}"
    fi

    if [ -n "$OC_PLATFORM" ]; then
        OC_EXT="tar.gz"
        [ "${TARGET_OS:-}" = "windows" ] && OC_EXT="zip"
        [ "${TARGET_OS:-}" = "darwin" ] && OC_EXT="zip"
        OC_ARCHIVE="opencode-${OC_PLATFORM}.${OC_EXT}"
        OC_URL="https://github.com/anomalyco/opencode/releases/download/v${OPENCODE_VERSION}/${OC_ARCHIVE}"

        mkdir -p "$OC_DIR"
        if [ -f "$OC_DIR/VERSION" ] && [ "$(cat "$OC_DIR/VERSION")" = "$OPENCODE_VERSION" ] && [ -f "$OC_DIR/opencode" -o -f "$OC_DIR/opencode.exe" ]; then
            echo "  OpenCode v${OPENCODE_VERSION} already cached in $OC_DIR/"
        else
            echo "  Downloading $OC_URL ..."
            if [ "$OC_EXT" = "zip" ]; then
                curl -sL "$OC_URL" -o /tmp/opencode-download.zip && unzip -qo /tmp/opencode-download.zip -d "$OC_DIR" && rm -f /tmp/opencode-download.zip
            else
                curl -sL "$OC_URL" | tar xzf - -C "$OC_DIR"
            fi
            chmod +x "$OC_DIR/opencode" 2>/dev/null || true
            echo -n "$OPENCODE_VERSION" > "$OC_DIR/VERSION"
            echo "  OpenCode v${OPENCODE_VERSION} downloaded to $OC_DIR/"
        fi
    else
        echo "  Unknown platform, skipping OpenCode download"
    fi
else
    echo "[3/5] OpenCode download skipped (use --with-opencode to download embedded agent)"
fi

# 2. Build Vue frontend
echo "[4/5] Building Vue frontend..."
if [ -f "package.json" ] && command -v npm >/dev/null 2>&1; then
    if [ ! -d "node_modules" ]; then
        echo "  Installing dependencies..."
        npm install
    fi
    # Clean stale hashed assets before rebuild (index-*.js, index-*.css, manifest-*.json)
    find public/ -maxdepth 1 -name 'index-*.js' -o -name 'index-*.css' -o -name 'manifest-*.json' | xargs rm -f 2>/dev/null || true
    npm run build
    echo "  Frontend: public/"
else
    echo "  npm not found or no package.json, skipping frontend build"
fi

# 3. Build Android APK (optional)
if [ -n "$BUILD_ANDROID" ]; then
    echo "[5/5] Building Android APK..."
    if [ -d "android" ] && [ -f "android/gradlew" ]; then
        (cd android && JAVA_HOME=/usr/lib/jvm/java-17-openjdk-amd64 ./gradlew assembleRelease \
            -PversionCode=$VERSION_CODE -PversionName="$FULL_VERSION")
        echo "  APK: android/app/build/outputs/apk/release/clawbench-android.apk"
    else
        echo "  Android project not found, skipping APK build"
    fi
else
    echo "[5/5] Android APK skipped (use --android to build)"
fi

echo ""
echo "=== Build complete ==="
if [ -n "$TARGET_OS" ] && [ -n "$TARGET_ARCH" ]; then
    BINARY_NAME="$NAME"
    [ "$TARGET_OS" = "windows" ] && BINARY_NAME="${NAME}.exe"
    echo "  ./$BINARY_NAME       # Go binary ($TARGET_OS/$TARGET_ARCH)"
else
    echo "  ./$NAME              # Go binary"
fi
echo "  public/              # Frontend (if built)"
echo "  .clawbench/opencode/ # OpenCode agent binary (if --with-opencode)"
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
echo "Embedded agent:"
echo "  ./build.sh --linux --with-opencode  # Linux + OpenCode (CI release)"
echo "  OPENCODE_VERSION=1.17.10 ./build.sh --with-opencode  # Pin a specific OpenCode version"
