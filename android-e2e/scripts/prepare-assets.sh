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

echo "[assets] staged in $ASSETS:"
ls -la "$ASSETS"
