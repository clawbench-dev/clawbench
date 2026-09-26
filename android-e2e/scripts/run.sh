#!/usr/bin/env bash
# End-to-end entry point for the ClawBench Android E2E harness (Tier 1).
#
# Pipeline:
#   1. ensure the emulator base image is present (pull-image.sh)
#   2. build the debug APK (unless --skip-build) and read its versionName
#   3. stage APK + chromedriver into the build context (prepare-assets.sh)
#   4. compose build + up
#   5. wait for the emulator to finish booting (sys.boot_completed)
#   6. disable the soft IME (prevents the login form from being clipped)
#   7. install the APK (Appium's fullReset also installs, but doing it here makes
#      failures explicit and lets the first launch be deterministic)
#   8. run the WebdriverIO suite in the runner container
#   9. always tear down, and copy artifacts out before the containers go away
#
# Idempotent: safe to re-run; every stage is a no-op if already satisfied. The
# teardown trap fires on success, failure, and Ctrl-C, so no multi-GB emulator
# container is left running.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
E2E_ROOT="$(cd "$HERE/.." && pwd)"
REPO_ROOT="$(cd "$E2E_ROOT/.." && pwd)"
ANDROID_DIR="$REPO_ROOT/android"
JDK17="${JAVA_HOME_17:-/usr/lib/jvm/java-17-openjdk-amd64}"

COMPOSE_FILE="$E2E_ROOT/docker-compose.yml"
PROJECT="clawbench-android-e2e"
ARTIFACTS="$E2E_ROOT/artifacts"

SKIP_BUILD=0
KEEP_UP=0
for arg in "$@"; do
  case "$arg" in
    --skip-build) SKIP_BUILD=1 ;;
    --keep-up)    KEEP_UP=1 ;;
    -h|--help)
      sed -n '2,18p' "$0"; exit 0 ;;
    *) echo "unknown arg: $arg" >&2; exit 2 ;;
  esac
done

log() { printf '\n=== [run.sh] %s ===\n' "$*"; }

compose() { docker compose -p "$PROJECT" -f "$COMPOSE_FILE" "$@"; }

cleanup() {
  local code=$?
  if [[ "$KEEP_UP" == "1" ]]; then
    log "leaving containers up (--keep-up); tear down with:"
    echo "  docker compose -p $PROJECT -f $COMPOSE_FILE down -v"
    return $code
  fi
  log "tearing down"
  compose down -v --remove-orphans >/dev/null 2>&1 || true
  return $code
}
trap cleanup EXIT INT TERM

# Stale failure artifacts from a previous run must not be mistaken for this
# run's. Cleared here (before any container work) so --keep-up re-runs are also
# covered; the runner writes into /e2e/artifacts inside its own container, which
# is copied out at the end.
rm -rf "${ARTIFACTS:?}"
mkdir -p "$ARTIFACTS"

# ---------------------------------------------------------------- 1. image ----
log "step 1/8: emulator base image"
"$HERE/pull-image.sh"

# ------------------------------------------------------------------ 2. APK ----
log "step 2/8: build debug APK"
if [[ "$SKIP_BUILD" == "1" ]]; then
  echo "skipped (--skip-build)"
else
  ( cd "$ANDROID_DIR" && \
    JAVA_HOME="$JDK17" ANDROID_HOME="${ANDROID_HOME:-/opt/android-sdk}" \
    ANDROID_SDK_ROOT="${ANDROID_SDK_ROOT:-/opt/android-sdk}" \
    ./gradlew --quiet assembleDebug )
fi

META="$ANDROID_DIR/app/build/outputs/apk/debug/output-metadata.json"
APK="$ANDROID_DIR/app/build/outputs/apk/debug/clawbench-android-debug.apk"
[[ -f "$META" && -f "$APK" ]] || { echo "ERROR: APK/metadata missing under $ANDROID_DIR" >&2; exit 1; }
CLAWBENCH_VERSION="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["elements"][0]["versionName"])' "$META")"
echo "APK versionName = $CLAWBENCH_VERSION  (mock server will report exactly this)"
export CLAWBENCH_VERSION

# -------------------------------------------------------------- 3. staging ----
log "step 3/8: stage assets"
"$HERE/prepare-assets.sh"

# ----------------------------------------------------------------- 4. build ---
log "step 4/8: compose build + up"
compose build
compose up -d emulator mock

# ------------------------------------------------------------ 5. wait boot ----
log "step 5/8: wait for emulator boot"
BOOT_TIMEOUT="${E2E_BOOT_TIMEOUT:-600}"
deadline=$(( $(date +%s) + BOOT_TIMEOUT ))
booted=0
while (( $(date +%s) < deadline )); do
  state="$(docker compose -p "$PROJECT" -f "$COMPOSE_FILE" exec -T emulator \
             sh -c 'adb -s emulator-5554 shell getprop sys.boot_completed 2>/dev/null' \
             2>/dev/null | tr -d '\r\n' || true)"
  if [[ "$state" == "1" ]]; then booted=1; break; fi
  printf '.'
  sleep 10
done
echo
if [[ "$booted" != "1" ]]; then
  echo "ERROR: emulator did not boot within ${BOOT_TIMEOUT}s" >&2
  echo "--- emulator device log (tail) ---" >&2
  compose logs --tail=60 emulator >&2 || true
  exit 1
fi
echo "emulator booted (sys.boot_completed=1)"

# ------------------------------------------------------- 6. disable soft IME --
# The login form lives in a WebView whose body is `min-height:100dvh` +
# `display:flex; align-items:center`, and MainActivity is `adjustResize`. When
# WebdriverIO focuses an input, the WebView's own requestFocus makes the IME
# appear (mShowExplicitlyRequested=true — `hw.keyboard=yes` in the AVD does NOT
# suppress it), the window shrinks by the IME height, and the vertically-centered
# form collapses into a ~3px sliver. The next interaction then fails with
# "element not interactable". The test never needs a real keyboard (setValue
# drives the WebView DOM, not the IME), so disable every IME before the suite.
log "step 6/8: disable soft IMEs (prevents login-form clipping)"
compose exec -T emulator sh -c '
  for ime in $(adb -s emulator-5554 shell ime list -s 2>/dev/null | tr -d "\r"); do
    adb -s emulator-5554 shell ime disable "$ime" >/dev/null 2>&1 || true
  done
  remaining="$(adb -s emulator-5554 shell ime list -s 2>/dev/null | tr -d "\r" | tr "\n" " ")"
  if [ -n "$remaining" ]; then
    echo "WARNING: IMEs still enabled: $remaining" >&2
  else
    echo "all soft IMEs disabled"
  fi
'

# -------------------------------------------------------------- 7. install ----
log "step 7/8: install APK"
compose exec -T emulator sh -c \
  'adb -s emulator-5554 install -r -g /apk/clawbench-android-debug.apk' | tail -2

# ----------------------------------------------------------------- 8. tests ---
log "step 8/8: run Tier 1 smoke test"
# Detached (not `compose run --rm`) so the container survives its exit and its
# artifacts can be copied out before teardown.
compose up -d runner
RUNNER_CID="$(compose ps -q runner)"
set +e
TEST_RC="$(docker wait "$RUNNER_CID")"
set -e
compose logs runner || true

# Copy artifacts out before teardown removes the container.
if [[ -n "$RUNNER_CID" ]]; then
  docker cp "$RUNNER_CID:/e2e/artifacts/." "$ARTIFACTS/" >/dev/null 2>&1 || true
fi

if [[ "$TEST_RC" == "0" ]]; then
  log "PASS — Tier 1 smoke test succeeded"
else
  log "FAIL — Tier 1 smoke test exited $TEST_RC"
  echo "--- mock server request log (tail) ---"
  compose logs --tail=40 mock || true
  echo "--- artifacts ---"
  ls -la "$ARTIFACTS" 2>/dev/null || true
fi
exit "$TEST_RC"
