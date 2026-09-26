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
| **1** | Native shell + WebView login flow (launch → login page → form → home) | implemented, green (`tests/smoke.login.mjs`) |
| **2** | h2 stream tunnel (`feat/ssh-ws-forward`) on a real runtime, against a REAL server — `-L` and `-R` byte transfer, wire selection, reconnect, concurrency, binary transparency, half-close/drain-grace, negative paths, teardown, and the 503 config gate | implemented, green (`tests/tunnel.{server,forward,reverse}.mjs`) [^same-site] |

[^same-site]: **Caveat:** the byte transfer is real, but on this API-28 image it
    runs with a harness-installed cookie, because the app cannot read its own
    session cookie here — see "A real image defect, worked around by the
    harness" below. The tunnel *code* is proven correct; the shipped
    login → tunnel path on this image 401s. A reader who skims the table must
    not conclude the shipped path works end to end.

Tier 2 reuses everything here — the same emulator, the same runner — but swaps
the mock for a **real `clawbench` server built from this worktree**, because the
tunnel is full-duplex and the mock cannot be (see "Why Tier 2 needs a real
server").

## Quick start

```bash
cd android-e2e
./scripts/run.sh --tier1          # native shell + login smoke (default)
./scripts/run.sh --tier2          # h2 tunnel -L / -R (real server)
./scripts/run.sh --all            # both, Tier 1 first
./scripts/run.sh --tier2 --skip-build    # reuse the existing debug APK
./scripts/run.sh --tier2 --keep-up       # leave containers running for debugging
```

`run.sh` is idempotent and tears down on success, failure, and Ctrl-C, so no
multi-GB emulator container is left behind.

## A real image defect, worked around by the harness (not hidden)

**The tunnel implementation is correct**, and that is proven by a decisive
control rather than by the fixture: a server built with production cookie
attributes **minus `SameSite`** (via `go build -overlay`, production source
untouched) made the REAL native login produce a readable cookie, and the tunnel
then carried real bytes end to end **with no fixture at all**. But on this
API-28 image the shipped login → tunnel path cannot authenticate: the app cannot
read its own session cookie, so every authenticated stream is 401. The suite is
built to surface that rather than to pass around it — and, importantly, it does
**not** assert that the defect exists (see "What the suite does not assert").

### The defect

`AndroidTunnelPlatform.sessionCookie()` builds the tunnel's `Cookie` header from
`CookieManager.getCookie(serverUrl)`. On this API-28 WebView (Chrome
69.0.3497.100) that call **omits any cookie carrying `SameSite=Lax` or
`SameSite=Strict`** — regardless of who wrote it. The server always sets
`SameSite=Lax` (`internal/handler/auth.go:241`), so the app cannot read its own
session cookie, even though:

1. the cookie **is** persisted (`run-as <pkg> sqlite3 .../app_webview/Cookies`
   shows `10.0.2.2 | clawbench_session | 64 bytes | path=/`), and
2. the WebView's own network stack **does** send it (an auth-protected
   `GET /api/config` from the page returns `200`).

Only the Java `getCookie()` read omits it. The discriminator is **`SameSite`,
not who wrote the cookie** — measured 2×2:

| writer | `SameSite` | `getCookie()` |
|--------|------------|---------------|
| app `CookieManager.setCookie()` | `Lax` | **NOT readable** |
| app `CookieManager.setCookie()` | *(none)* | readable |
| renderer `document.cookie` | *(none)* | readable |
| renderer `document.cookie` | `Lax` | **NOT readable** |

(An earlier version of this section claimed the discriminator was
app-vs-renderer provenance. That was wrong; this 2×2 is the measured result.)

So every authenticated `POST /api/tunnel/stream` is `401`
(`auth: rejecting request ... has_cookie=false`), and no payload traverses the
shipped path.

**All 14 `getCookie()` call sites are affected**, not just the tunnel: the
shipped native WebSocket push path (`BackgroundService.connectNativeWs`), the
`AppLog` relay, `shareFile`/`shareFiles`, the download paths, `ShareIn`, the
sandbox handoff, and `PendingEventsWorker` all read the same API and are
likewise unauthenticated on this image.

### The harness fixture (and why it is still a real test)

`tests/helpers/tunnel.mjs installReadableSessionCookie()` reaches a state where
the app *can* build an authenticated request:

1. `pm clear` the app (removes the persisted URL **and** any HttpOnly cookie),
2. `connectToServer(url, '')` — lands the WebView on the server origin without
   installing a session cookie,
3. authenticate from the runner (`POST /login`) to learn the real token,
4. write it from the page **without a `SameSite` attribute**:
   `document.cookie='clawbench_session=<token>; path=/'`.

`getCookie()` now returns it, and everything downstream is the production path:
`AndroidTunnelPlatform.sessionCookie` → OkHttp h2 duplex stream → the server's
relay → the target. The fixture substitutes only *where the cookie is installed*
(and omits `SameSite`, the one shape this WebView reads back), because the app's
own read is broken by the image.

The fixture's own validity is asserted **positively**: after installing the
cookie, `installReadableSessionCookie()` calls `appCanReadSessionCookie()`, which
drives an authenticated request and requires a real server status that is not
`401`. A missing cookie (or a `shareFile` early-return, or empty logcat) cannot
satisfy it, so the byte-transfer assertions that follow cannot be vacuous.

### What the suite does *not* assert

An earlier revision of `tests/tunnel.server.mjs` **asserted the defect** and
`assert.fail`ed if the app's read ever started working. That was removed: a test
that goes red when the product improves is a fix-inhibitor, not a guard. The
suite now asserts only the *positive* prerequisites (the fixture works, the
tunnel carries bytes); nothing depends on the defect persisting.

### Recommendation (production change, not applied here)

Capture the cookie value at login. `handleAuthResponse` already receives the
`Set-Cookie` headers (`MainActivity.java:1672`) and injects them into the
WebView; persist the session cookie value there and have
`AndroidTunnelPlatform.sessionCookie` read that, instead of round-tripping
through `CookieManager`. That removes the dependency on the WebView API and
**fixes all 14 `getCookie()` call sites at once** — the tunnel, the native-WS
push path, the `AppLog` relay, file share/download, `ShareIn`, the sandbox
handoff and `PendingEventsWorker` — not just the tunnel.

## Why Tier 2 needs a real server

The tunnel is full-duplex by construction: on one HTTP/2 stream the **request
body carries client→server bytes** while the **response body carries
server→client bytes**. Any half-duplex mock — including Tier 1's
`mock-server/server.mjs` and MockWebServer — consumes the whole request body
before it may write a response, so it deadlocks a duplex stream. The
implementation plan calls this out explicitly
(`docs/plans/2026-09-25-h2-tunnel-implementation.md`, T14: "MockWebServer 是半双工的").

Tier 2 therefore builds and runs the real server:

```
scripts/prepare-assets.sh --with-server
  └─ CGO_ENABLED=0 go build -ldflags "-X clawbench/internal/version.Version=<APK versionName>" \
       -o assets/clawbench-server ./cmd/server
```

`Dockerfile.server` packages that binary with `server-config/config.yaml`, and
compose runs it in the **emulator's network namespace** (`network_mode:
service:emulator`) so the app reaches it at `10.0.2.2:20000` — the same address
the Tier 1 mock used.

### The config that must not regress

```yaml
port_forward:
  enabled: false   # disables ONLY the SSH listener
  transport: h2    # h2-only
```

This is the load-bearing combination: `shouldCreateProxyRegistry`
(`cmd/server/proxy_registry_gate.go`) is `Enabled || Transport != "ssh"`, so
`enabled: false` + `transport: h2` **must still create the ProxyRegistry**. If it
did not, every `/api/tunnel/*` request would be `503 PortForwardUnavailable`. The
only combination that legitimately disables the h2 endpoints is
`enabled: false` + `transport: ssh`.

`tests/tunnel.server.mjs` asserts this on an **authenticated** request: the
handler's nil-registry guard is *inside* the handler, and `middleware.Auth`
returns `401` before the handler runs for any request without a cookie. An
unauthenticated probe therefore cannot observe `503` at all — an earlier revision
asserted `status !== 503` without a cookie and could never fail (the 401 came
from the middleware regardless of the registry). The spec now:

- probes unauthenticated and asserts **401** (a statement about the auth layer);
- probes **authenticated** and asserts the handler was reached — a dead-target
  dial is **502**, and the control stream is **200** — never `503`;
- and pins the `503` branch with a **positive control**: the `server-no-h2`
  compose service runs the same binary with `enabled: false` + `transport: ssh`
  (`server-config/config.no-h2.yaml`) on port **20002**, and both endpoints are
  asserted to answer `503` there. Without that control, "not 503" would be
  consistent with a 503 branch that does not exist.

The control server runs alongside the h2 server for the whole Tier 2 phase
(`network_mode: service:emulator`, so the runner reaches it at
`http://emulator:20002`), which is far cheaper than a second emulator boot.

### Version agreement (why login does not hang)

On login, native does `POST /login`, then `GET /api/health` and compares the
reported `version` against the APK's `versionName`
(`MainActivity.gateVersionMismatchAndProceed` → `VersionCompare.shouldShowMismatch`).
When `appVersion < serverVersion` the app shows a **blocking** dialog and the
test hangs.

`prepare-assets.sh` makes them agree structurally: it compiles the server binary
with `-X clawbench/internal/version.Version=<the APK's versionName>`, read from
the same `output-metadata.json` `run.sh` uses for the Tier 1 mock. The comparison
itself is a numeric dotted-core compare that ignores the `-<distance>-g<hash>`
suffix, so only the `vX.Y.Z` base matters — but pinning the exact string means
the two are provably identical rather than merely compatible.

## Tier 2 topology

Everything the tunnel needs lives in the emulator's network namespace:

```
[emulator container netns]                       [device]
  10.0.2.2:20000  real clawbench server (enabled:false, transport:h2)
  127.0.0.1:20002 negative-config control server (enabled:false, transport:ssh)
  127.0.0.1:18080 tunnel target (node:net)  <──h2──  127.0.0.1:15080  (-L listener)
  127.0.0.1:17080 reverse listener (server binds) <──h2──  127.0.0.1:18090  (-R device target)
  0.0.0.0:18081   target/control HTTP server
```

| Service | Role |
|---------|------|
| `server` | The real `clawbench` binary (`Dockerfile.server`), h2-only config |
| `server-no-h2` | The same binary with `enabled:false, transport:ssh` on :20002 — the 503 positive control |
| `target` | `target-server/server.mjs` — the `-L` dial target + a control API for the runner |

Both server services share the emulator's netns (like `target`), so the runner
reaches them at `http://emulator:20000` and `http://emulator:20002`.

`target` must share the server's namespace because the h2 `-L` handler dials the
target **from the server's namespace**, so `127.0.0.1:18080` is only a valid
target there. Its control port `18081` is additionally reachable by the runner at
`http://emulator:18081` (compose service name).

## Driving the app and asserting

The transport is selected in the **WebView (Vue) UI** — there is no native
transport picker — so the specs switch into the `WEBVIEW_*` context and call the
JS bridge directly (`window.ClawBenchNative`, injected at `MainActivity.java:693`,
class `WebAppInterface`):

| Bridge method | Used for |
|---|---|
| `setTunnelTransport("h2")` | select the transport (the point of the test) |
| `getTunnelTransport()` | the preference in effect |
| `getActiveTunnelTransport()` | **which wire actually won** — `"tls"` / `"h2c"` / `""` |
| `addForwardedPort(local, target, host)` | `-L` |
| `addReverseForwardedPort(server, target, host)` | `-R` |
| `removeForwardedPort` / `removeReverseForwardedPort` | teardown |
| `getForwardedPorts()` | JSON list, `direction` is `"forward"` / `"reverse"` |
| `testPortReachable(port)` | TCP-accept check only |
| `getTunnelError()` / `getTunnelErrorType()` | error surface |

`getActiveTunnelTransport()` is the headline result: against this plain-HTTP
server the TLS probe fails at the handshake (fast, not a timeout) and **h2c**
wins. Both `tls` and `h2c` are accepted by the assertions; the observed value is
printed as `[tier2] getActiveTunnelTransport() = "..."`.

### Authentication for the tunnel specs

The tunnel's data and control streams are authenticated. Because the app cannot
read its own `SameSite=Lax` session cookie on this WebView (see above), the
`-L`/`-R` specs install a readable (SameSite-less) cookie with the real server
token first (`installReadableSessionCookie`), which asserts the install worked
*positively*. `tests/tunnel.forward.mjs` and `tests/tunnel.reverse.mjs` then
exercise the full production path with that credential. The suite does **not**
assert that the app's own read fails — see "What the suite does not assert".

### Proving real bytes, from inside the emulator

`testPortReachable()` only proves a TCP accept, so Tier 2 also transfers real
payload with **device-side `toybox nc`** run through Appium's `mobile: shell`:

- **`-L`**: the device sends an HTTP request carrying a random nonce to
  `127.0.0.1:15080`; the target echoes what it received. A matching response
  proves server→client, and the target's own recorded request log proves
  client→server. Both witnesses must agree.
- **`-R`**: the server-side client (`target`'s `/__reverse/probe`, inside the
  server's namespace) connects to `127.0.0.1:17080` and sends a nonce; the
  device target replies with its marker and records the request. Again both
  directions are witnessed independently.

### Target reply modes

The `-L` target's behavior is selectable at runtime (`POST
/__target/mode?mode=`), so a spec can make a probe that would pass against a
lenient target fail against a strict one:

| Mode | Behavior | Used to prove |
|------|----------|---------------|
| `http` (default) | reply on headers-complete, echoing the request | the byte-transfer assertions |
| `eof` | reply **only** on client EOF | the half-close really traversed the tunnel |
| `raw` | read to EOF, echo bytes verbatim | 64 KiB binary byte-transparency (`cmp` on-device) |
| `stall` | close the write side at once, never reply | the bounded drain grace (design §4.2.1) |
| `delay` | record the request, wait 10s, then reply | a relay that is in flight when its port is removed |

The mode is snapshotted per connection, so flipping it cannot change the
behavior of a connection that is already open.

### Probes beyond the single request

- **Binary**: `probeLocalPortRaw` pushes a payload to the device, streams it
  through the forward, and runs `cmp` **on the device** — a base64 round trip
  through Appium would hide a byte-transparency bug.
- **Concurrent**: `probeLocalPortConcurrent` fans out N `nc` probes on the
  device (T14 calls for 20-way) and checks each reply plus the target's log.
- **Half-close / stalled**: dedicated device scripts keep the request body open
  (`probeLocalPortStalled`) or rely on EOF (`probeLocalPortHalfClose`).
- **In flight**: `startDetachedLocalProbe` runs a probe with `setsid` so the
  spec can remove the port *while* the relay is live — Appium serialises its
  commands, so a blocking probe could never be concurrent with the removal.

**API 28 device tools (probed, not assumed):** `toybox` (multi-call, includes
`nc`, `netstat`, `setsid`, `pkill`, `md5sum`, `cmp`, `dd`, `timeout`),
`/system/xbin/nc` (BSD flavour), `sh`. The applet list was read out of the
system image with `debugfs` rather than assumed. There is **no** `curl`,
`wget`, or `busybox`. Several measured constraints shaped the probes:

- **The device `-R` target uses `nc -L` (capital), not a `while true; nc -l`
  loop.** A loop drops connections: `nc` exits when its connection ends and the
  loop needs a moment to respawn, and a connection arriving in that window gets
  `ECONNREFUSED`. Measured: a loop serving ~1 connection/s served only ~half of
  20 sequential probes, every failure an immediate `ECONNREFUSED`. `nc -L` keeps
  one listening socket and forks a handler per connection, so there is no window.
- **The handler emits its response from a FILE with `cat`.** A shell builtin
  `printf` is block-buffered when stdout is a socket, so the response would sit
  in the buffer until the handler exits — which it cannot do while still
  reading — and the peer times out.
- **The handler's request recorder is bounded (`timeout 2 cat >> log`).** In the
  `-R` direction the device target never observes the server-side client's
  half-close (the server relay does not forward it to the device — design
  §4.2.1), so an unbounded `cat` would block until the relay's drain grace tore
  the stream down and the handler would never exit.
- `mkfifo` is **denied** (`Permission denied`, SELinux), so no FIFO shape is
  available.

### Appium `mobile: shell` needs a server flag

Appium 3.x refuses `mobile: shell` unless the server was started with
`--allow-insecure *:adb_shell` (the `*:` destination prefix is required; a bare
`adb_shell` is rejected). budtmo starts Appium as
`/usr/local/bin/appium $APPIUM_ADDITIONAL_ARGS`, so `Dockerfile.emulator` sets:

```
APPIUM_ADDITIONAL_ARGS="--allow-insecure *:adb_shell"
```

The `appium:allowInsecure` **capability** alone is not enough — it is the server
flag that gates the command.

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
- **Go toolchain at `/usr/local/go/bin/go`** (Tier 2 only — it builds the real
  server binary). Override with `GO_BIN` if it lives elsewhere.
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
- Tier 2 authenticates through a **harness fixture** (a page-written, SameSite-less
  cookie) because on this image `getCookie()` omits the app's own `SameSite=Lax`
  session cookie. The tunnel code itself is exercised end to end and proven
  correct (see "A real image defect, worked around by the harness" above); the
  shipped login → tunnel path on this image 401s. The suite does not assert the
  defect, so it stays green if the app-side read is fixed.
