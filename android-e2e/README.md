# ClawBench Android E2E harness

Emulator-based instrumented tests for the ClawBench Android app. The app is a thin
WebView shell, so these tests drive a **real Android runtime**: they launch the
native activity, switch into the in-app WebView, fill the login form, and assert
the app reaches the server's home page.

This directory is deliberately self-contained and is **not** wired into the root
vitest suite or `scripts/pre-push-checks.sh`.

## Tiers

| Tier | What it proves | Status |
|------|----------------|--------|
| **1** | Native shell + WebView login flow (launch → login page → form → home) | implemented (`tests/smoke.login.mjs`) |
| **2** | h2 tunnel feature (`feat/ssh-ws-forward`) end-to-end on a real runtime | planned — slots in as additional `tests/*.mjs` |

Tier 2 reuses everything here: the same emulator, the same mock server (extended
with tunnel endpoints), the same runner. Add specs under `tests/`; the wdio config
globs `tests/**/*.mjs`.

## Quick start

```bash
cd android-e2e
./scripts/run.sh                 # full pipeline: image -> APK -> build -> up -> test -> down
./scripts/run.sh --skip-build    # reuse the existing debug APK
./scripts/run.sh --keep-up       # leave containers running for debugging
```

`run.sh` is idempotent and tears down on success, failure, and Ctrl-C, so no
multi-GB emulator container is left behind.

## Prerequisites

- Docker CLI reachable (this repo's dev container talks to a **host** docker daemon).
- **`docker compose` v2** (`docker compose`, not the standalone `docker-compose`
  binary). The harness uses `docker compose -p <project> -f <file>` with v2 syntax.
  Check with `docker compose version` (expect `v2.x` or newer).
- **~15 GB free disk** before the first run. The `budtmo/docker-android:emulator_9.0`
  image is **9.14 GB uncompressed / 2.65 GB compressed** on disk, and the derived
  emulator image plus the AVD/userdata adds several more GB. `docker system df`
  shows current usage.
- **`/dev/kvm` present AND usable.** Presence is not enough — the emulator needs
  read/write on the device node, and without working KVM it falls back to
  unusably slow software emulation (or fails to boot). Verify *usability* before
  the first run:

  ```bash
  # On the docker *host* (not inside this dev container): the node must exist.
  # Note this dev container itself has no /dev/kvm even when the host does, so a
  # failed `ls` here is not conclusive — prefer the container check below.
  ls -l /dev/kvm

  # The authoritative check. Run as root, because a fresh container has NOT yet
  # adjusted the node's ownership — budtmo's emulator startup chowns /dev/kvm to
  # its own uid (1300) as part of `change_permission()`. As the *default*
  # container user this command therefore reports "This user doesn't have
  # permissions to use KVM" even on a perfectly good host (exit 11); that is
  # expected and does NOT indicate a problem.
  docker run --rm --device /dev/kvm -u root --entrypoint sh \
    budtmo/docker-android:emulator_9.0 -c 'emulator -accel-check'
  # Expect: "KVM (version N) is installed and usable." and exit status 0.
  ```

  The container must already have the image loaded (see `scripts/pull-image.sh`).
  Once the harness is running, the authoritative check is inside the live
  container, after its startup chown:

  ```bash
  docker compose -p clawbench-android-e2e \
    -f android-e2e/docker-compose.yml exec emulator emulator -accel-check
  ```
- JDK 17 at `/usr/lib/jvm/java-17-openjdk-amd64` (AGP 8.2; the default JDK 21 fails).
- Android SDK at `/opt/android-sdk` (for the Gradle build only).
- `python3`, `curl`, `unzip` on the host running `run.sh`.

## How it works

```
scripts/run.sh
  ├─ scripts/pull-image.sh      budtmo/docker-android:emulator_9.0 if absent
  ├─ gradlew assembleDebug      android/app/build/outputs/apk/debug/*.apk
  ├─ scripts/prepare-assets.sh  APK + chromedriver 2.44 -> android-e2e/assets/
  ├─ docker compose build/up    emulator + mock (+ runner)
  ├─ wait for sys.boot_completed
  ├─ disable every soft IME     (prevents the login-form clipping flake)
  ├─ adb install -r -g <apk>
  ├─ docker compose up -d runner ; docker wait  (Tier 1 suite)
  └─ trap: compose down -v
```

### Containers

| Service | Role |
|---------|------|
| `emulator` | `budtmo/docker-android:emulator_9.0` (Android 9 / API 28) + Appium 3.7 + UiAutomator2. Built from `Dockerfile.emulator` to add a Chrome-69-capable chromedriver. |
| `mock` | Dependency-free Node server (`mock-server/server.mjs`). Serves `/api/health`, `/login`, and a marker home page. **Shares the emulator's network namespace** (`network_mode: service:emulator`). |
| `runner` | Node + WebdriverIO. Reaches Appium at `http://emulator:4723` by service name. |

### Networking (why the mock shares the emulator's netns)

Inside an Android emulator `127.0.0.1` is the emulator itself; its host is the
alias **`10.0.2.2`**. By giving the mock `network_mode: "service:emulator"` it binds
`:20000` on the emulator container's own loopback, which is exactly what `10.0.2.2`
resolves to. This avoids depending on compose DNS, host port publishing, or SLIRP
host-loopback quirks.

The runner is a *different* container and reaches the same listener at
`http://emulator:20000` (its compose-bridge address) — used only to read the mock's
request log in assertions.

### Bind mounts do not work here

The docker CLI talks to a **host** daemon, so `-v ./local:/container` resolves the
path on the *host* filesystem and mounts an empty directory (verified). Everything is
therefore delivered by:

- **`docker build`** — the build context is streamed from the client, so
  `Dockerfile.*` `COPY`s the APK, chromedriver, mock server, and tests.
- **`docker cp`** — copies artifacts back out.

That is why the mock and tests live in images rather than bind mounts.

## The WebView context problem (the hard part)

The login form lives in `file:///android_asset/login.html`, rendered **inside the
WebView**, so the test must switch from the native context into a `WEBVIEW_*`
context. Three things had to line up:

1. **The API-28 WebView is Chrome 69** (`dumpsys webviewupdate`). Appium *does* list
   the `WEBVIEW_*` context (a debuggable build enables WebView debugging implicitly),
   but switching fails with *"No Chromedriver found that can automate Chrome
   '69.0.3497'"* — no chromedriver ships for it.
2. **Only chromedriver 2.42–2.44 support Chrome 69.** `prepare-assets.sh` downloads
   **2.44** (newest of those, and it understands JSONWP sessions) and
   `Dockerfile.emulator` bakes it in; Appium is pointed at it via
   `appium:chromedriverExecutable`.
3. **Appium 3.x is W3C-only; 2.44 is JSONWP-first.** 2.44 rejects Appium's W3C
   `{"capabilities":{"alwaysMatch":...}}` payload with *"session not created:
   Missing or invalid capabilities"*. `scripts/chromedriver-shim.py` wraps the real
   2.44 binary and rewrites `GET /status` to add `ready: true` and report a
   `build.version < 75`, which makes Appium's `syncProtocol` negotiate JSONWP.

See `scripts/chromedriver-shim.py` for the detailed rationale and the exact Appium
source locations.

**No production-code change is required** — the app needs no
`setWebContentsDebuggingEnabled(true)` for a debuggable build.

## Test isolation

`MainActivity` persists the server URL in `SharedPreferences`; a second run would
skip the login page and fail confusingly. The wdio config sets `fullReset: true` /
`noReset: false`, so the app is uninstalled and reinstalled between runs, and the
first test asserts it is on the native login activity before proceeding.

## The soft-IME flake (why `run.sh` disables the IME)

The login form is a WebView whose body is `min-height: 100dvh; overflow: hidden;
display: flex; align-items: center` (`android/app/src/main/assets/login.html`), and
`MainActivity` is `android:windowSoftInputMode="adjustResize"`. When WebdriverIO
focuses an input, the **WebView's own `requestFocus` makes the IME appear** — the
AVD's `hw.keyboard=yes` does **not** suppress it (the WebView sets
`mShowExplicitlyRequested=true`). The window then shrinks by the IME height
(measured: the WebView goes from `[0,72][1080,1776]` to `[0,72][1080,1029]`), and
the vertically-centered form collapses into a ~3px sliver. The next interaction
(`#addPort.clearValue()`) then fails with `400 element not interactable`, and the
run took ~13 minutes to report it.

The test never needs a real keyboard: `setValue` drives the WebView DOM, not the
IME. So `run.sh` disables every IME after boot and before the suite:

```bash
adb -s emulator-5554 shell ime list -s          # list enabled IMEs
adb -s emulator-5554 shell ime disable <id>     # disable each
```

Verified: with the IME disabled the WebView bounds stay `[0,72][1080,1776]` across
repeated focus cycles, and the suite passes 5/5 consecutive runs. As belt-and-braces
the test also calls `browser.hideKeyboard()` (try/catch — it throws when no IME is
shown) after each field and `scrollIntoView` before each interaction.

## Failure artifacts are time-capped

A failure caused by a wedged renderer wedges the diagnostic commands too: an
uncapped `browser.saveScreenshot()` in the `afterTest` hook was observed to hang
~6 minutes *after* the test had already timed out. `wdio.conf.mjs` now wraps every
artifact capture (screenshot / page source / contexts) in a hard
`E2E_ARTIFACT_TIMEOUT_MS` cap (default 8s) that aborts cleanly and logs, and the
per-test mocha timeout is 240s. A red run now returns in ~4 minutes worst case.

## The mock server

`mock-server/server.mjs` is dependency-free (`node:http`). It implements exactly the
contract `MainActivity` depends on:

| Endpoint | Behaviour |
|----------|-----------|
| `POST /login` | `200` + `Set-Cookie: clawbench_session=...` |
| `GET /api/health` | `{"app":"clawbench","version":"<APK versionName>"}` |
| `GET /` (and anything else) | HTML home page containing `#e2e-home-marker` |
| `GET /__requests` | test-control: JSON log of every request seen |
| `POST /__reset` | test-control: clear that log |

The app calls them in the order **`POST /login` → `GET /api/health` → `GET /`**
(`authenticateAndNavigate` POSTs first; the health check and `webView.loadUrl`
run only after a 200), and the last test asserts that order.

**The version must match the APK's `versionName`.** `VersionCompare` shows a
*blocking* native dialog when the app is older than the server, which would hang the
test. `run.sh` reads `output-metadata.json` and passes it as `CLAWBENCH_VERSION`; the
server never hardcodes it (it exits with a clear error if neither that nor
`APK_METADATA` is set). `docker-compose.yml` defaults the variable to the empty
string so `docker compose down -v` (the `--keep-up` teardown hint) can interpolate
without a value — the empty default deliberately still trips the server's loud
runtime guard.

## Artifacts

`run.sh` **clears `artifacts/` at the start of every run** (including `--keep-up`
re-runs), so a `FAIL-*` file can only come from the current run. On failure the
wdio config writes a screenshot, page source, and the context list to
`android-e2e/artifacts/` (copied out of the runner container before teardown), each
capture time-capped as described above.

## Known limitations

- **Emulator boot is slow** (~2–4 min cold on first run; the AVD is created on first
  boot). `run.sh` waits up to `E2E_BOOT_TIMEOUT` (default 600s).
- **The base image pull takes ~6 minutes** (~2.65 GB) the first time, via a parallel
  client-side puller — see `scripts/pull-image.sh` for why `docker pull` cannot be
  used in this environment.
- **API 28 only.** The chromedriver/shim workaround is specific to Chrome 69. A
  newer system image would need a matching chromedriver and might not need the shim.
- **Single device, serial execution** (`maxInstances: 1`).
- Tier 2 (h2 tunnel) is not implemented yet.
