#!/usr/bin/env bash
# End-to-end entry point for the ClawBench Android E2E harness.
#
# Pipeline:
#   1. ensure the emulator base image is present (pull-image.sh)
#   2. build the debug APK (unless --skip-build) and read its versionName
#   3. stage APK + chromedriver (+ the real server binary for Tier 2)
#   4. compose build + up the services for the selected tier
#   5. wait for the emulator to finish booting (sys.boot_completed)
#   6. disable the soft IME (prevents the login form from being clipped)
#   7. install the APK (Appium's fullReset also installs, but doing it here makes
#      failures explicit and lets the first launch be deterministic)
#   8. run the WebdriverIO suite in the runner container
#   9. always tear down, and copy artifacts out before the containers go away
#
# Tiers:
#   --tier1   native shell + WebView login smoke against the mock server (fast)
#   --tier2   h2 stream tunnel `-L`/`-R` against a REAL server (this worktree)
#   --all     both, Tier 1 first
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
TIER="1"
for arg in "$@"; do
  case "$arg" in
    --skip-build) SKIP_BUILD=1 ;;
    --keep-up)    KEEP_UP=1 ;;
    --tier1)      TIER="1" ;;
    --tier2)      TIER="2" ;;
    --all)        TIER="all" ;;
    -h|--help)
      sed -n '2,22p' "$0"; exit 0 ;;
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
  # --remove-orphans also drops the Tier 2 services when only Tier 1 ran (they
  # were never created, so this is a no-op) and vice versa.
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
echo "APK versionName = $CLAWBENCH_VERSION"
echo "  -> Tier 1: the mock server reports exactly this"
echo "  -> Tier 2: the real server binary is COMPILED with this, so /api/health matches"
export CLAWBENCH_VERSION

# -------------------------------------------------------------- 3. staging ----
log "step 3/8: stage assets"
if [[ "$TIER" == "1" ]]; then
  "$HERE/prepare-assets.sh"
else
  "$HERE/prepare-assets.sh" --with-server
fi

# ----------------------------------------------------------------- 4. build ---
log "step 4/8: compose build (tier=$TIER)"
compose build
# Containers are started per phase (see run_phase below): both server variants
# bind :20000 in the emulator's netns, so exactly one may be up at a time.
compose up -d emulator

# ------------------------------------------------------- 5. wait for boot ----
# Two robustness measures, both from measured failures on a cold boot:
#
#   - Every probe is wrapped in `timeout`. `adb shell` against a still-booting
#     device can wedge for minutes; without a per-attempt cap a single hung probe
#     consumes the entire budget (observed: 600s elapsed with only two polls
#     completed, then "did not boot" while the device was in fact up).
#   - After several consecutive wedges, restart the adb server. The device can be
#     fully booted (`sys.boot_completed=1`) while adbd's host side is stuck; an
#     `adb kill-server && adb start-server` recovered it instantly (measured).
log "step 5/8: wait for emulator boot"
BOOT_TIMEOUT="${E2E_BOOT_TIMEOUT:-900}"
PROBE_TIMEOUT="${E2E_BOOT_PROBE_TIMEOUT:-25}"
deadline=$(( $(date +%s) + BOOT_TIMEOUT ))
booted=0
wedges=0
while (( $(date +%s) < deadline )); do
  set +e
  state="$(timeout "$PROBE_TIMEOUT" docker compose -p "$PROJECT" -f "$COMPOSE_FILE" exec -T emulator \
             sh -c 'adb -s emulator-5554 shell getprop sys.boot_completed 2>/dev/null' \
             2>/dev/null | tr -d '\r\n')"
  rc=$?
  set -e
  if [[ "$state" == "1" ]]; then booted=1; break; fi
  if [[ "$rc" == "124" ]]; then
    wedges=$((wedges + 1))
    printf 'w'
    if (( wedges % 3 == 0 )); then
      echo " (adb probe wedged ${wedges}x; restarting adb server)"
      timeout 60 docker compose -p "$PROJECT" -f "$COMPOSE_FILE" exec -T emulator \
        sh -c 'adb kill-server 2>/dev/null; adb start-server 2>/dev/null; true' >/dev/null 2>&1 || true
    fi
  else
    printf '.'
  fi
  sleep 5
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

# ---------------------------------------------------------- test runner ------
# One phase = one server + one suite run. `--all` runs two phases (the mock for
# Tier 1, the real server for Tier 2) because both bind :20000 in the emulator's
# netns and so cannot be up at once. Each phase's wdio session uses
# `fullReset: true`, so the app is reinstalled and starts on the login page
# again — the phases do not share app state.
#
# Sets PHASE_RC to the suite's exit code.
run_phase() {
  local tier="$1" server_service="$2" extra_services="$3"
  PHASE_RC=0

  log "phase tier=$tier: start $server_service ${extra_services:-(no extra services)}"
  # shellcheck disable=SC2086 # word-split is intended: extra_services is a list
  compose up -d emulator "$server_service" $extra_services

  # The app logs in as soon as the suite starts, so the server must already be
  # accepting. The Tier 1 mock binds instantly; the real server needs a few
  # seconds to open its SQLite DB and start workers. Poll /api/health from the
  # emulator container (which shares the server's netns) rather than sleeping.
  log "phase tier=$tier: wait for $server_service on :20000"
  local server_timeout="${E2E_SERVER_TIMEOUT:-120}"
  local deadline=$(( $(date +%s) + server_timeout ))
  local server_up=0
  while (( $(date +%s) < deadline )); do
    if compose exec -T emulator sh -c \
        'python3 - <<PY 2>/dev/null
import urllib.request
print(urllib.request.urlopen("http://127.0.0.1:20000/api/health", timeout=3).status)
PY' | tr -d '\r\n' | grep -q '^200$'; then
      server_up=1; break
    fi
    printf '.'
    sleep 5
  done
  echo
  if [[ "$server_up" != "1" ]]; then
    echo "ERROR: $server_service did not answer /api/health within ${server_timeout}s" >&2
    compose logs --tail=80 "$server_service" >&2 || true
    PHASE_RC=1
    return 0
  fi
  echo "$server_service is serving /api/health (200)"

  log "phase tier=$tier: run suite"
  # Detached (not `compose run --rm`) so the container survives its exit and its
  # artifacts can be copied out before teardown. E2E_TIER selects the spec files.
  # A recreated runner picks up the exported value: `compose up -d` recreates the
  # container when its config (including env) changed.
  export E2E_TIER="$tier"
  compose up -d --force-recreate runner
  local runner_cid
  runner_cid="$(compose ps -q runner)"
  set +e
  PHASE_RC="$(docker wait "$runner_cid")"
  set -e
  compose logs runner || true

  # Copy artifacts out before the phase's containers go away.
  docker cp "$runner_cid:/e2e/artifacts/." "$ARTIFACTS/" >/dev/null 2>&1 || true

  if [[ "$PHASE_RC" != "0" ]]; then
    echo "--- $server_service log (tail) ---"
    compose logs --tail=40 "$server_service" || true
    if [[ "$tier" != "1" ]]; then
      echo "--- tunnel target log (tail) ---"
      compose logs --tail=40 target || true
    fi
    echo "--- artifacts ---"
    ls -la "$ARTIFACTS" 2>/dev/null || true
  fi

  # Stop the runner and this phase's server so the next phase can bind :20000.
  compose rm -sf runner >/dev/null 2>&1 || true
  # shellcheck disable=SC2086
  compose rm -sf "$server_service" $extra_services >/dev/null 2>&1 || true
}

# ----------------------------------------------------------------- 8. tests ---
OVERALL_RC=0
if [[ "$TIER" == "1" || "$TIER" == "all" ]]; then
  run_phase 1 mock ""
  if [[ "$PHASE_RC" == "0" ]]; then
    log "PASS — Tier 1 suite succeeded"
  else
    log "FAIL — Tier 1 suite exited $PHASE_RC"
    OVERALL_RC="$PHASE_RC"
  fi
fi
if [[ "$TIER" == "2" || "$TIER" == "all" ]]; then
  # Skip Tier 2 under --all when Tier 1 already failed: a broken login would
  # make every Tier 2 spec fail for the same reason, and the log would be noise.
  if [[ "$OVERALL_RC" != "0" && "$TIER" == "all" ]]; then
    log "SKIP — Tier 2 (Tier 1 failed; fix that first)"
  else
    run_phase 2 server "target"
    if [[ "$PHASE_RC" == "0" ]]; then
      log "PASS — Tier 2 suite succeeded"
    else
      log "FAIL — Tier 2 suite exited $PHASE_RC"
      OVERALL_RC="$PHASE_RC"
    fi
  fi
fi

exit "$OVERALL_RC"
