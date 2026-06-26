#!/usr/bin/env bash
set -e

NAME="clawbench"
DIST="dist"
ASSETS="assets"

# Parse arguments
TARGET_OS=""
TARGET_ARCH=""
BUILD_ANDROID=""
DOWNLOAD_PI=""
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
        --with-pi)
            DOWNLOAD_PI=1
            ;;
    esac
done

echo "=== Building $NAME ==="

# 0. Provider models generation is done by CI only (see .github/workflows/ci.yml and release.yml).
# Local builds use the existing .clawbench/provider_models.json if present.

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

# 1.5 Download Pi binary (embedded agent for setup wizard)
# Default: skip. Use --with-pi to download, or set PI_VERSION to pin a version.
# Without PI_VERSION, the latest release is fetched from GitHub API automatically.
PI_DIR=".clawbench/pi"
if [ -n "$DOWNLOAD_PI" ]; then
    # Resolve Pi version: env var override > auto-detect latest from GitHub
    if [ -z "$PI_VERSION" ]; then
        PI_VERSION=$(curl -sI https://github.com/earendil-works/pi/releases/latest 2>/dev/null | grep -i "^location:" | sed 's|.*/tag/v||' | tr -d '[:space:]')
        if [ -z "$PI_VERSION" ]; then
            echo "  ERROR: Could not detect latest Pi version. Set PI_VERSION manually."
            exit 1
        fi
        echo "  Auto-detected latest Pi version: v${PI_VERSION}"
    fi
    echo "[3/5] Downloading Pi v${PI_VERSION}..."
    # Determine platform for Pi binary
    if [ -n "$TARGET_OS" ] && [ -n "$TARGET_ARCH" ]; then
        case "$TARGET_OS" in
            linux)   PI_PLATFORM="linux-$TARGET_ARCH" ;;
            darwin)  PI_PLATFORM="darwin-$TARGET_ARCH" ;;
            windows) PI_PLATFORM="windows-$TARGET_ARCH" ;;
            *)       PI_PLATFORM="" ;;
        esac
        # Pi uses "x64" not "amd64" in its archive names
        PI_PLATFORM="${PI_PLATFORM/amd64/x64}"
    else
        PI_PLATFORM="$(uname -s | tr '[:upper:]' '[:lower:]')-$(uname -m)"
        PI_PLATFORM="${PI_PLATFORM/x86_64/x64}"
        PI_PLATFORM="${PI_PLATFORM/aarch64/arm64}"
    fi

    if [ -n "$PI_PLATFORM" ]; then
        PI_EXT="tar.gz"
        [ "${TARGET_OS:-}" = "windows" ] && PI_EXT="zip"
        PI_ARCHIVE="pi-${PI_PLATFORM}.${PI_EXT}"
        PI_URL="https://github.com/earendil-works/pi/releases/download/v${PI_VERSION}/${PI_ARCHIVE}"

        mkdir -p "$PI_DIR"
        if [ -f "$PI_DIR/VERSION" ] && [ "$(cat "$PI_DIR/VERSION")" = "$PI_VERSION" ] && [ -f "$PI_DIR/pi" -o -f "$PI_DIR/pi.exe" ]; then
            echo "  Pi v${PI_VERSION} already cached in $PI_DIR/"
        else
            echo "  Downloading $PI_URL ..."
            if [ "$PI_EXT" = "zip" ]; then
                curl -sL "$PI_URL" -o /tmp/pi-download.zip && unzip -qo /tmp/pi-download.zip -d /tmp/pi-download && cp -r /tmp/pi-download/pi/* "$PI_DIR/" && rm -rf /tmp/pi-download /tmp/pi-download.zip
            else
                curl -sL "$PI_URL" | tar xzf - -C "$PI_DIR" --strip-components=1
            fi
            chmod +x "$PI_DIR/pi" 2>/dev/null || true
            echo -n "$PI_VERSION" > "$PI_DIR/VERSION"
            echo "  Pi v${PI_VERSION} downloaded to $PI_DIR/"
        fi
    else
        echo "  Unknown platform, skipping Pi download"
    fi
else
    echo "[3/5] Pi download skipped (use --with-pi to download embedded agent)"
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
echo "  .clawbench/pi/       # Pi agent binary (if --with-pi)"
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
echo "  ./build.sh --linux --with-pi  # Linux + Pi binary (CI release)"
echo "  PI_VERSION=0.79.0 ./build.sh --with-pi  # Pin a specific Pi version"
