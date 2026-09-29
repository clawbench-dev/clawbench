/**
 * Shared helpers for the Tier 2 h2-tunnel specs.
 *
 * Tier 2 runs against a REAL clawbench server (Dockerfile.server) — the Tier 1
 * mock is half-duplex and cannot carry the tunnel's simultaneous request and
 * response streams. Everything the tunnel needs is reachable from the emulator:
 *
 *   10.0.2.2:20000  the real server (shares the emulator's netns)
 *   127.0.0.1:15080 a `-L` local listener the app opens on the device
 *   127.0.0.1:18080 the `-L` target, in the server's netns
 *   127.0.0.1:18090 a `-R` target the spec runs on the device
 *   127.0.0.1:17080 a `-R` server-side listener (bound on the server's loopback)
 *
 * Two independent observation channels are used, and they must agree:
 *
 *   - the **JS bridge** (`window.ClawBenchNative`, MainActivity.WebAppInterface)
 *     for tunnel state and forward management, and
 *   - **device-side `toybox nc`** run through Appium's `mobile: shell`, for the
 *     actual bytes. A bridge `testPortReachable()` only proves a TCP accept; the
 *     `nc` probe proves real bidirectional payload transfer through the tunnel.
 *
 * ## The session-cookie fixture (why `installReadableSessionCookie` exists)
 *
 * The tunnel's data streams are authenticated, and the app builds their
 * `Cookie` header from `CookieManager.getCookie(serverUrl)`
 * (`AndroidTunnelPlatform.sessionCookie`). On this API-28 image (Chrome
 * 69.0.3497.100) that call **omits any cookie carrying `SameSite=Lax` or
 * `SameSite=Strict`**, regardless of who wrote it — `setCookie()` or the
 * renderer's `document.cookie`. The server always sets `SameSite=Lax`
 * (`internal/handler/auth.go`), so the app cannot read its own session cookie,
 * even though:
 *
 *   - the cookie IS persisted (`sqlite app_webview/Cookies`), and
 *   - the WebView's network stack DOES send it (an auth-protected page fetch
 *     returns 200).
 *
 * The measured 2x2 (writer x SameSite):
 *
 *   | writer              | SameSite     | getCookie() |
 *   |---------------------|--------------|-------------|
 *   | app `setCookie`     | `Lax`        | NOT readable |
 *   | app `setCookie`     | (none)       | readable     |
 *   | renderer `document` | (none)       | readable     |
 *   | renderer `document` | `Lax`        | NOT readable |
 *
 * This is an image-specific WebView *read* bug, not a tunnel bug: a server built
 * with production attributes minus `SameSite` (via `go build -overlay`) let the
 * REAL native login produce a readable cookie and carried real bytes end to end
 * with no fixture at all. The tunnel code is correct; on this image the shipped
 * path 401s. All 14 `getCookie()` call sites are affected, not just the tunnel.
 *
 * `installReadableSessionCookie()` therefore wipes the app, lands it on the
 * server origin WITHOUT a password (so no session cookie is installed),
 * authenticates from the runner to learn the token, and has the page write it
 * with **no SameSite attribute** — the one shape this WebView will read back.
 * That is a **harness fixture for an image-specific read bug**, not a substitute
 * for the tunnel: every hop from `AndroidTunnelPlatform.sessionCookie` through
 * OkHttp to the server relay is still exercised. See `tunnel.server.mjs` and the
 * README; do NOT assert that the read fails, because that would make the suite
 * red the moment the app is fixed.
 *
 * Appium 3.x refuses `mobile: shell` unless the server was started with
 * `--allow-insecure *:adb_shell` (Dockerfile.emulator sets APPIUM_ADDITIONAL_ARGS
 * for exactly this) — see the README.
 */
import assert from 'node:assert/strict';

export const SERVER_HOST = process.env.E2E_SERVER_HOST || '10.0.2.2';
export const SERVER_PORT = process.env.E2E_SERVER_PORT || '20000';
/** The server as the APP sees it (SLIRP's host alias, inside the emulator). */
export const SERVER_URL = `http://${SERVER_HOST}:${SERVER_PORT}`;
/**
 * The same server as the RUNNER sees it. 10.0.2.2 only resolves inside the
 * emulator; the runner is a separate container on the compose bridge and reaches
 * the emulator's network namespace by service name.
 */
export const SERVER_FROM_RUNNER = `http://emulator:${SERVER_PORT}`;
export const PASSWORD = process.env.E2E_PASSWORD || 'e2e-test-password';

/** The app package under test, for device-side private-data reads. */
export const APP_PACKAGE = process.env.E2E_APP_PACKAGE || 'com.clawbench.app.debug';
export const APP_ACTIVITY = process.env.E2E_APP_ACTIVITY || 'com.clawbench.app.MainActivity';

/** The Tier 2 control server, reached by compose service name (not the app). */
export const TARGET_CONTROL = process.env.E2E_TARGET_CONTROL || 'http://emulator:18081';
export const TARGET_PORT = Number(process.env.E2E_TARGET_PORT || 18080);

/** `-L`: device-side listener port and its server-side target. */
export const L_LOCAL_PORT = 15080;
/** A SECOND `-L` listener, for the "two simultaneous forwards" test. */
export const L_LOCAL_PORT_2 = 15081;
/** `-R`: server-side listener port and its device-side target. */
export const R_SERVER_PORT = 17080;
/** A SECOND `-R` server port, for the "removal is scoped" positive control. */
export const R_SERVER_PORT_2 = 17081;
/** A THIRD `-R` server port, for the in-flight teardown test (stall target). */
export const R_SERVER_PORT_3 = 17082;
export const R_DEVICE_TARGET_PORT = 18090;
/** The device port a STALLING `-R` target listens on (never replies). */
export const R_DEVICE_STALL_PORT = 18092;

/**
 * How long the device `nc` probe waits. The first stream of a session pays the
 * OkHttp connect + h2c probe + server-side dial; under emulator load that can
 * exceed a few seconds, so this is generous on purpose (the probe's own wall
 * clock is what the bounded-grace test measures, and it is bounded well under
 * this).
 */
export const L_PROBE_TIMEOUT_SEC = 20;

export const HOME_MARKER = 'e2e-home-marker';

/** Device-side scratch dir. `/data/local/tmp` is world-writable and survives. */
const DEVICE_TMP = '/data/local/tmp';

/** The live h2 session's transport family. `getActiveTunnelTransport()` no
 *  longer distinguishes the wire kind (see that function), so this is `["h2"]`. */
export const H2_WIRES = ['h2'];

// ---------------------------------------------------------------- generic ----

export async function pause(ms) {
  await browser.pause(ms);
}

/**
 * Poll `fn` until it returns truthy, or throw with `what` after `timeoutMs`.
 * Returns the truthy value so callers can use it.
 */
export async function waitFor(fn, timeoutMs, what) {
  const deadline = Date.now() + timeoutMs;
  let last;
  let lastErr;
  while (Date.now() < deadline) {
    try {
      last = await fn();
      if (last) return last;
    } catch (e) {
      lastErr = e;
    }
    await browser.pause(500);
  }
  throw new Error(
    `timed out after ${timeoutMs}ms waiting for ${what}` +
      (lastErr ? ` (last error: ${lastErr.message})` : ` (last value: ${JSON.stringify(last)})`),
  );
}

// ------------------------------------------------------------- WebView -------

/** Switch into the (single) WEBVIEW_* context, waiting for it to appear. */
export async function enterWebView() {
  // Fast path: already in a WebView (the common case — `bridge()` calls this on
  // every invocation, and the device-shell helpers restore the context anyway).
  const current = await browser.getContext().catch(() => null);
  if (current && current.startsWith('WEBVIEW')) return current;

  const webview = await waitFor(async () => {
    const contexts = await browser.getContexts();
    return contexts.find((c) => c.startsWith('WEBVIEW')) || null;
  }, 60000, 'a WEBVIEW_* context');
  if ((await browser.getContext().catch(() => null)) !== webview) {
    await browser.switchContext(webview);
  }
  return webview;
}

/**
 * Re-resolve and switch to the WebView context. A page navigation can change the
 * context name (and chromedriver attaches to a new devtools target), so callers
 * must not hold on to the name from before the navigation.
 */
export async function resyncWebView() {
  return enterWebView();
}

// ------------------------------------------------------------- bridge --------

/**
 * Call a `window.ClawBenchNative` method and return its value.
 *
 * The bridge is a `@JavascriptInterface`, so calls are synchronous and the
 * return value comes straight back. Throws if the bridge is absent (which would
 * mean the page is not the app's WebView).
 *
 * Ensures the WebView context first: the device-shell helpers hop out to
 * NATIVE_APP and back, and a spec that calls a bridge method right after one
 * must not depend on the hop having restored the context.
 */
export async function bridge(method, ...args) {
  await enterWebView();
  return browser.execute(
    (m, a) => {
      const api = window.ClawBenchNative;
      if (!api) throw new Error('window.ClawBenchNative is not injected');
      if (typeof api[m] !== 'function') throw new Error(`ClawBenchNative.${m} is not a function`);
      return api[m](...a);
    },
    method,
    args,
  );
}

/**
 * The transport preference in effect, derived from the local h2 toggle:
 * `"h2"` when enabled, `"ssh"` otherwise.
 */
export async function getTunnelTransport() {
  return bridge('getTunnelTransport');
}

/**
 * The transport FAMILY of the live h2 session: `"h2"`, or `""` when there is no
 * live h2 session.
 *
 * This used to be the wire kind (`"tls"` / `"h2c"`). The bridge was refactored
 * when the string preference became a boolean toggle
 * (`setTunnelTransportH2Enabled`): `getActiveTunnelTransport()` now returns the
 * family name, matching the desktop client's `getActiveTransport()`. The wire
 * kind is no longer observable from JS — so `waitForActiveWire` below waits for
 * `"h2"` and the specs must not assert `tls`/`h2c`.
 */
export async function getActiveTunnelTransport() {
  return bridge('getActiveTunnelTransport');
}

/** The last tunnel error message (may be empty). */
export async function getTunnelError() {
  return bridge('getTunnelError');
}

/** The classified error type: `auth|network|server|hostkey|unknown|""`. */
export async function getTunnelErrorType() {
  return bridge('getTunnelErrorType');
}

/**
 * Wait for a live h2 session and return its transport family.
 *
 * The name is kept from when this returned a wire kind; the return value is now
 * always `"h2"` (see `getActiveTunnelTransport`). Callers that only need "is an
 * h2 session live?" are unchanged.
 */
export async function waitForActiveWire(timeoutMs = 60000) {
  return waitFor(async () => {
    const active = await getActiveTunnelTransport();
    return H2_WIRES.includes(active) ? active : null;
  }, timeoutMs, 'getActiveTunnelTransport() to report a live h2 session');
}

// -------------------------------------------------------- device shell -------

/** The native context name. `mobile: shell` / `mobile: pushFile` only work here. */
export const NATIVE_CONTEXT = 'NATIVE_APP';

/**
 * Run `fn` in the NATIVE_APP context, then restore whatever context was active.
 *
 * Appium's `mobile:` commands are UiAutomator2 extensions and are rejected with
 * "Did not receive an appropriate execute method parameters object" while a
 * WEBVIEW_* context is selected. The specs otherwise live in the WebView (that
 * is where the bridge is), so every device-shell call has to hop out and back.
 *
 * A failure still restores the context, so one bad `nc` invocation cannot leave
 * the session parked in NATIVE_APP and cascade into every later bridge call.
 */
async function inNativeContext(fn) {
  const previous = await browser.getContext().catch(() => null);
  if (previous !== NATIVE_CONTEXT) {
    await browser.switchContext(NATIVE_CONTEXT);
  }
  try {
    return await fn();
  } finally {
    if (previous !== NATIVE_CONTEXT && previous) {
      try {
        await browser.switchContext(previous);
      } catch {
        // The page may have navigated; the next bridge() call re-syncs anyway.
      }
    }
  }
}

/**
 * Push a file to the device (base64, so no shell quoting is involved).
 *
 * Preferred over inlining a command in `mobile: shell`: Appium builds an
 * `adb shell sh -c "<command>"` string, and the newlines/backslashes a raw HTTP
 * probe needs survive that round trip badly (verified: `printf` arrived mangled).
 * A pushed script plus simple argv sidesteps the whole class of bug.
 *
 * The params object is passed as a SINGLE argument, not wrapped in an array:
 * WebdriverIO sends `args: <rest params>`, so `execute('mobile: pushFile', p)`
 * becomes the `args: [p]` Appium expects. Wrapping it again sends
 * `args: [[p]]` and Appium rejects it with
 * "Did not receive an appropriate execute method parameters object".
 */
export async function pushFile(remotePath, content) {
  // A Buffer is passed through as-is (arbitrary bytes); a string is UTF-8.
  // base64 either way, so the payload never touches a shell.
  const buf = Buffer.isBuffer(content) ? content : Buffer.from(content, 'utf8');
  const payload = buf.toString('base64');
  await inNativeContext(() =>
    browser.execute('mobile: pushFile', { remotePath, payload }),
  );
}

/** Run a command on the device; returns `{ stdout, stderr }`. */
export async function deviceShell(command, args = [], { includeStderr = true } = {}) {
  const res = await inNativeContext(() =>
    // Single arg, not wrapped — see pushFile's note on the args shape.
    browser.execute('mobile: shell', { command, args, includeStderr }),
  );
  // UiAutomator2 returns {stdout, stderr}; older shapes return a string.
  if (typeof res === 'string') return { stdout: res, stderr: '' };
  return { stdout: res?.stdout ?? '', stderr: res?.stderr ?? '' };
}

/**
 * Run a shell script on the device with argv, returning `{ stdout, stderr }`.
 *
 * The script is pushed once per call so a spec can never be tripped by a stale
 * file from a previous run.
 */
export async function runDeviceScript(name, content, args = []) {
  const path = `${DEVICE_TMP}/${name}`;
  await pushFile(path, content);
  return deviceShell('sh', [path, ...args]);
}

/** Read a file from the device as a string (empty when missing). */
export async function readDeviceFile(path) {
  const { stdout } = await deviceShell('cat', [path]);
  return stdout;
}

/** Clear logcat so a subsequent readDeviceLog() sees only new lines. */
export async function clearDeviceLog() {
  await deviceShell('logcat', ['-c']);
}

/** Read the app's own logcat lines (every AppLog call uses the ClawBench tag). */
export async function readDeviceLog() {
  return (await deviceShell('logcat', ['-d', '-s', 'ClawBench:*'])).stdout || '';
}

// ------------------------------------------------- app state / login ---------

/**
 * Wipe the app and relaunch it onto the first-launch login page.
 *
 * Required by the cookie fixture: the app's own session cookie is HttpOnly, so
 * page JS cannot overwrite it, and it is `SameSite=Lax`, so `getCookie()` does
 * not return it either (see the file header). Wiping the app is the only way to
 * reach a state where the renderer can install a readable (SameSite-less)
 * cookie. `pm clear` also removes the persisted server URL, so the app
 * cold-starts on the login page rather than auto-connecting.
 */
export async function resetAppToLoginPage() {
  await deviceShell('pm', ['clear', APP_PACKAGE]);
  await deviceShell('am', ['start', '-n', `${APP_PACKAGE}/${APP_ACTIVITY}`]);
  const webview = await waitFor(async () => {
    try {
      const contexts = await browser.getContexts();
      return contexts.find((c) => c.startsWith('WEBVIEW')) || null;
    } catch {
      return null;
    }
  }, 60000, 'a WEBVIEW context after the app reset');
  await browser.switchContext(webview);
  // The login asset must have rendered, or `connectToServer` is not yet callable.
  await waitFor(async () => {
    const href = await browser.execute(() => location.href).catch(() => '');
    return typeof href === 'string' && href.startsWith('file://') ? href : null;
  }, 60000, 'the login page asset to load');
  return webview;
}

/**
 * Drive the native login form (the flow `MainActivity.authenticateAndNavigate`
 * implements: POST /login -> GET /api/health -> webView.loadUrl).
 *
 * The login page was refactored (0c2da01c, e8d8d57c): the protocol radio and the
 * separate host/port inputs are gone. The address is one field (`#addAddress`)
 * that must be a complete `scheme://host:port` (url-utils.js buildServerUrl),
 * and Add/Edit now only SAVES — it no longer auto-connects. So the flow is:
 * fill address + password, press Add, then press Connect. This helper used to
 * drive `#addHost`/`#addPort`/`input[name="addProtocol"]`, all of which no
 * longer exist; it was only ever masked because Tier 1 failed first.
 *
 * After this the app holds its OWN (HttpOnly, `SameSite=Lax`) session cookie and
 * is on the server origin. On this image `getCookie()` will not read that cookie
 * back (SameSite omission — see the file header), so the tunnel fixtures use
 * `installReadableSessionCookie` instead; this is used by the specs that verify
 * the native login flow itself.
 */
export async function nativeLogin() {
  await enterWebView();
  const href = await browser.execute(() => location.href).catch(() => '');
  if (typeof href === 'string' && href.startsWith(SERVER_URL)) return; // already in

  // On the login page the add form is the visible one (first launch has no
  // saved servers). `#addAddress` is the single address field.
  const address = await browser.$('#addAddress');
  if (!(await address.isExisting())) {
    throw new Error(`not on the login page (location=${href})`);
  }
  await fillField('#addAddress', SERVER_URL);
  await fillField('#addPassword', PASSWORD);

  // Add only SAVES; it does not authenticate (0c2da01c).
  await browser.$('#addConnectBtn').scrollIntoView({ block: 'center', inline: 'center' });
  await browser.$('#addConnectBtn').click();

  // Saving returns to the saved-server view with the Connect form shown; the
  // password is hidden because it was stored with the entry. Connect is what
  // runs the native authenticateAndNavigate flow.
  const connectBtn = await browser.$('#connectBtn');
  await connectBtn.waitForDisplayed({ timeout: 15000 });
  await connectBtn.scrollIntoView({ block: 'center', inline: 'center' });
  await connectBtn.click();

  await waitFor(async () => {
    const contexts = await browser.getContexts();
    const webview = contexts.find((c) => c.startsWith('WEBVIEW'));
    if (!webview) return false;
    if ((await browser.getContext()) !== webview) await browser.switchContext(webview);
    const h = await browser.execute(() => location.href);
    return typeof h === 'string' && h.startsWith(SERVER_URL) ? h : false;
  }, 90000, `the WebView to reach ${SERVER_URL}`);

  const errorVisible = await browser.execute(() => {
    const e = document.getElementById('errorMsg');
    return !!e && getComputedStyle(e).display !== 'none';
  });
  assert.equal(errorVisible, false, 'login error banner is visible');
}

/** Fill a login field defensively (scroll, clear, type, dismiss any IME). */
async function fillField(selector, value) {
  const el = await browser.$(selector);
  await el.scrollIntoView({ block: 'center', inline: 'center' });
  await el.clearValue();
  await el.setValue(value);
  try {
    await browser.hideKeyboard();
  } catch {
    // Expected when no IME is shown; run.sh disables every IME anyway.
  }
}

/** Land the WebView on the server origin WITHOUT installing a session cookie. */
export async function connectToOriginWithoutPassword() {
  await bridge('connectToServer', SERVER_URL, '');
  await waitFor(async () => {
    const h = await browser.execute(() => location.href).catch(() => '');
    return typeof h === 'string' && h.startsWith(SERVER_URL) ? h : null;
  }, 90000, `the WebView to reach ${SERVER_URL} without a password`);
}

/**
 * Authenticate from the RUNNER and return the raw session token.
 *
 * The token is a server-generated random value (see internal/handler/auth.go);
 * reading it here is what lets the fixture hand the page a *valid* credential
 * without the app installing one itself.
 */
export async function fetchSessionToken(base = SERVER_FROM_RUNNER) {
  const res = await fetch(`${base}/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ password: PASSWORD }),
  });
  assert.equal(res.status, 200, `the harness password is not accepted by ${base}`);
  const setCookies = res.headers.getSetCookie
    ? res.headers.getSetCookie()
    : [res.headers.get('set-cookie')];
  // Match on the name SUFFIX: a non-default port is scoped by the server
  // (model.ScopedCookieName prefixes it, e.g. `cb20001_clawbench_session`), so
  // an exact-prefix match would silently find nothing on a non-default port.
  const session = setCookies.find(
    (c) => c && /(^|;\s*)(cb\d+_)?clawbench_session=/.test(c),
  );
  assert.ok(session, `POST /login did not set a session cookie: ${JSON.stringify(setCookies)}`);
  const name = session.slice(0, session.indexOf('='));
  return session.slice(name.length + 1).split(';')[0];
}

/**
 * A `fetch` that carries a real session cookie.
 *
 * `middleware.Auth` (internal/middleware/auth.go) rejects every request without
 * a valid cookie with 401 BEFORE the handler runs. That is why an
 * unauthenticated probe can never observe a handler-internal status such as the
 * tunnel handlers' 503: the handler is never entered. Anything asserting on a
 * status the HANDLER produces must therefore authenticate first — which is the
 * whole point of this helper.
 *
 * `base` is BOTH where the token is minted and where the cookie name is derived
 * from. The two must come from the same server: the cookie is port-scoped
 * (model.ScopedCookieName), so a token minted on :20000 is named
 * `clawbench_session` and would not authenticate a request to a non-default
 * port, whose cookie is `cb<port>_clawbench_session`. Deriving the name from the
 * CALLER's base (rather than hardcoding it) is what keeps the two in sync.
 */
export async function authFetch(url, init = {}, base = SERVER_FROM_RUNNER) {
  const token = await fetchSessionToken(base);
  const port = new URL(base).port;
  const cookieName = port && port !== '20000' ? `cb${port}_clawbench_session` : 'clawbench_session';
  const headers = new Headers(init.headers || {});
  headers.set('Cookie', `${cookieName}=${token}`);
  return fetch(url, { ...init, headers });
}

/**
 * Does the app's OWN cookie read (`shareFile` -> `CookieManager.getCookie()`)
 * yield a cookie the server accepts?
 *
 * This is a **positive** detector, not an absence one. An earlier version
 * reported `readable: true` whenever neither "no auth cookie" nor "no server
 * URL" appeared in logcat — which is also true when logcat is empty, or when
 * `shareFile` bails out before ever reaching the `getCookie()` call (e.g. an
 * invalid path logs only `shareFile: invalid path: ...`). That made the fixture
 * assertion below vacuous.
 *
 * The evidence now required is a server RESPONSE STATUS for the download:
 *
 *   - `shareFile` only issues its OkHttp request after `getCookie()` returned a
 *     non-empty cookie; an empty read logs `shareFile: no auth cookie` and
 *     returns. So a status line implies the cookie WAS read.
 *   - The endpoint is auth-wrapped (`middleware.Auth`), so `401` means the
 *     server rejected that cookie. Any other status (403 from `requireProject`
 *     when no project cookie is set, 404 for a missing file, 200 for a hit)
 *     means the request passed authentication — the read worked AND the
 *     credential was accepted.
 *
 * The probe path is deliberately one that cannot be served, so a successful
 * download (which logs no status) cannot make this a false negative: with no
 * project cookie the server answers 403 before it ever stats the file.
 */
export async function appCanReadSessionCookie() {
  await deviceShell('logcat', ['-c']);
  // A path that cannot resolve to a served file: forces a non-2xx status line.
  await bridge('shareFile', '/e2e-cookie-read-probe-does-not-exist', 'text/plain');

  // Poll rather than a fixed sleep: the request runs on a background thread and
  // the status line is what we are waiting for.
  let log = '';
  const deadline = Date.now() + 20000;
  while (Date.now() < deadline) {
    await browser.pause(1000);
    log = (await deviceShell('logcat', ['-d', '-s', 'ClawBench:*'])).stdout || '';
    if (/shareFile: download failed, code=\d+/.test(log)) break;
    // A pre-request bail-out cannot produce a status line; stop early.
    if (log.includes('shareFile: no auth cookie') || log.includes('shareFile: no server URL')) break;
  }

  const downloadCode = /shareFile: download failed, code=(\d+)/.exec(log)?.[1] ?? null;
  const noCookie = log.includes('shareFile: no auth cookie');
  const noServer = log.includes('shareFile: no server URL');
  return {
    // Positive evidence only: a real status that is not an auth rejection.
    readable: downloadCode !== null && downloadCode !== '401',
    downloadCode,
    noCookie,
    noServer,
    raw: log,
  };
}

/**
 * Reach a state where the app can build an authenticated tunnel request:
 * a readable session cookie in the WebView jar.
 *
 * See the file header for why this is necessary and why it is still a real test
 * of the tunnel. Returns the token length for the log.
 */
export async function installReadableSessionCookie() {
  await resetAppToLoginPage();
  await connectToOriginWithoutPassword();
  const token = await fetchSessionToken();
  const doc = await browser.execute((t) => {
    document.cookie = `clawbench_session=${t}; path=/`;
    return document.cookie;
  }, token);
  assert.ok(
    typeof doc === 'string' && doc.includes('clawbench_session='),
    `the page could not install a readable session cookie (document.cookie=${JSON.stringify(doc)})`,
  );
  // The app's own read path must now see it; otherwise the fixture failed and
  // every later byte-transfer assertion would be vacuous. `appCanReadSessionCookie`
  // returns positive evidence (a non-401 server status), so this cannot pass on
  // an empty logcat or a `shareFile` early-return.
  const read = await appCanReadSessionCookie();
  assert.ok(
    read.readable,
    `the fixture installed a cookie but the app still cannot read it: ` +
      `downloadCode=${read.downloadCode} noCookie=${read.noCookie} ` +
      `noServer=${read.noServer} raw=${JSON.stringify(read.raw.slice(0, 400))}`,
  );
  return { tokenLength: token.length, documentCookie: doc };
}

// --------------------------------------------------- device-side probes ------

/**
 * `-L` probe script: open a TCP connection to the device-local forwarded port,
 * send an HTTP request carrying a nonce, and print the reply to stdout.
 *
 * `toybox nc` half-closes stdin after the piped request, which the server-side
 * relay propagates to the target as EOF — that is what makes the target's
 * read-until-EOF reply arrive. Without the half-close the target would wait for
 * its idle timeout and the probe would be slow and flaky.
 *
 * The timeout is `L_PROBE_TIMEOUT_SEC` rather than a literal so the flake
 * surface is one place: the FIRST stream of a session pays the OkHttp connect +
 * h2c probe + server-side dial, which under emulator load can exceed a tight
 * literal. The bounded-grace test measures the probe's own wall clock and is
 * unaffected by a generous ceiling.
 */
export const L_PROBE_SCRIPT = `#!/system/bin/sh
# $1 = device-local port, $2 = nonce
PORT="$1"
NONCE="$2"
printf 'GET /tunnel-probe?nonce=%s HTTP/1.1\\r\\nHost: probe\\r\\nConnection: close\\r\\n\\r\\n' "$NONCE" \\
  | toybox nc -w ${L_PROBE_TIMEOUT_SEC} 127.0.0.1 "$PORT"
`;

/**
 * Run the `-L` probe from the device and return `{stdout, stderr, ms}`.
 * `nc` exits non-zero on a refused connection; that is returned, not thrown, so
 * the negative test can inspect it.
 *
 * `ms` is the measured wall clock of the whole device-side invocation, which is
 * what the bounded-grace and teardown tests assert on (a probe that returns
 * promptly is the observable; `nc`'s exit code alone cannot distinguish "the
 * stream ended" from "nothing happened").
 */
export async function probeLocalPort(port, nonce) {
  const started = Date.now();
  const res = await runDeviceScript('lprobe.sh', L_PROBE_SCRIPT, [String(port), nonce]);
  return { ...res, ms: Date.now() - started };
}

/**
 * Device-side script that streams a payload to a local port and records the
 * raw reply, so byte-exactness can be checked ON THE DEVICE (no base64 round
 * trip through Appium, which would hide a byte-transparency bug).
 *
 * Args: $1 = port, $2 = payload path, $3 = reply path, $4 = md5 of the payload.
 * Prints `SENT <bytes>` and `OK` / `MISMATCH` / `SHORT:<n>`.
 *
 * `toybox nc` half-closes stdin at EOF (that is what ends the request), and the
 * reply is written to $3 by redirecting stdout. `cmp` is a toybox applet on this
 * image (verified by extracting the system image), as is `md5sum`.
 */
export const L_RAW_PROBE_SCRIPT = `#!/system/bin/sh
# $1 = device-local port, $2 = payload path, $3 = reply path
PORT="$1"
PAYLOAD="$2"
REPLY="$3"
SIZE=$(wc -c < "$PAYLOAD")
toybox nc -w ${L_PROBE_TIMEOUT_SEC} 127.0.0.1 "$PORT" < "$PAYLOAD" > "$REPLY"
GOT=$(wc -c < "$REPLY")
echo "SENT $SIZE GOT $GOT"
if [ "$GOT" != "$SIZE" ]; then echo "SHORT:$GOT"; exit 3; fi
if cmp -s "$PAYLOAD" "$REPLY"; then echo OK; else echo MISMATCH; exit 4; fi
`;

/**
 * Push a binary payload to the device, stream it through the `-L` forward, and
 * compare the reply to the payload ON THE DEVICE.
 *
 * `payload` is a Node Buffer. It is pushed base64-encoded (see pushFile), which
 * round-trips arbitrary bytes exactly — unlike passing the bytes through
 * Appium's shell, which is what the previous text-only probe had to avoid.
 *
 * Returns `{stdout, stderr, ms, payloadMd5}`.
 */
export async function probeLocalPortRaw(port, payload, name = 'rawprobe') {
  const payloadPath = `${DEVICE_TMP}/${name}.bin`;
  const replyPath = `${DEVICE_TMP}/${name}.out`;
  await pushFile(payloadPath, payload);
  const started = Date.now();
  const res = await runDeviceScript(`${name}.sh`, L_RAW_PROBE_SCRIPT, [
    String(port),
    payloadPath,
    replyPath,
  ]);
  return { ...res, ms: Date.now() - started, payloadPath, replyPath };
}

/**
 * Device-side probe that HALF-CLOSES its write side and keeps reading, for the
 * half-close test.
 *
 * It is a dedicated script rather than `nc` because the observable is "data
 * that the target sent AFTER it saw our EOF still arrives". `toybox nc` with a
 * piped stdin does half-close, but it also exits as soon as its own stdout
 * closes; here the read must outlive the request. `nc` is given the request on
 * stdin from a regular file (so stdin reaches EOF = half-close) while its
 * stdout is captured.
 */
export const L_HALFCLOSE_PROBE_SCRIPT = `#!/system/bin/sh
# $1 = device-local port, $2 = nonce
PORT="$1"
NONCE="$2"
REQ=${DEVICE_TMP}/halfclose.req
printf 'GET /half-close?nonce=%s HTTP/1.1\\r\\nHost: probe\\r\\nConnection: close\\r\\n\\r\\n' "$NONCE" > "$REQ"
toybox nc -w ${L_PROBE_TIMEOUT_SEC} 127.0.0.1 "$PORT" < "$REQ"
`;

export async function probeLocalPortHalfClose(port, nonce) {
  const started = Date.now();
  const res = await runDeviceScript('halfclose.sh', L_HALFCLOSE_PROBE_SCRIPT, [
    String(port),
    nonce,
  ]);
  return { ...res, ms: Date.now() - started };
}

/**
 * `-L` probe for the BOUNDED DRAIN GRACE (design §4.2.1).
 *
 * The ordinary probe half-closes (its piped request reaches EOF), which ends the
 * client -> target direction BEFORE the target ends — the path that
 * deliberately skips the grace. To observe the grace the client must keep its
 * request body OPEN while the target ends its response direction, so the
 * feeder below never EOFs: it sends the request, then a keepalive every second.
 *
 * Against a `stall` target (which closes its write side at once) the server
 * handler therefore sits in `RelayDuplex`'s bounded window
 * (internal/tunnel/relay.go, relayDrainGrace = 5s) and only then returns, which
 * sends END_STREAM; the client's response body ends, the Android relay closes
 * the local socket, and `nc` (whose stdout just closed) exits. So the pipeline's
 * wall clock IS the grace — bounded, not immediate and not infinite.
 *
 * The feeder is a subshell in a pipeline: when `nc` exits, the feeder's next
 * write takes SIGPIPE and the pipeline completes. `|| exit 0` is belt-and-braces
 * for a shell that reports the broken pipe as an error instead.
 */
export const L_STALL_PROBE_SCRIPT = `#!/system/bin/sh
# $1 = device-local port, $2 = nonce
PORT="$1"
NONCE="$2"
(
  printf 'GET /stall-probe?nonce=%s HTTP/1.1\\r\\nHost: probe\\r\\nConnection: close\\r\\n\\r\\n' "$NONCE"
  while :; do
    printf 'keepalive-%s\\n' "$NONCE" || exit 0
    sleep 1
  done
) | toybox nc -w ${L_PROBE_TIMEOUT_SEC} 127.0.0.1 "$PORT"
`;

/**
 * Run the stall probe and return `{stdout, stderr, ms}`. `ms` is the whole
 * pipeline's wall clock, i.e. how long the stream stayed open — the observable
 * the bounded-grace test asserts on.
 */
export async function probeLocalPortStalled(port, nonce) {
  const started = Date.now();
  const res = await runDeviceScript('stallprobe.sh', L_STALL_PROBE_SCRIPT, [
    String(port),
    nonce,
  ]);
  return { ...res, ms: Date.now() - started };
}

/**
 * Device-side script that fires `COUNT` probes at a local port CONCURRENTLY and
 * waits for all of them, each hard-bounded by `timeout`.
 *
 * Why not just call `probeLocalPort` in a Node loop: the spec would serialise,
 * and the design's concurrency requirement (implementation plan T14: 20-way)
 * is about the SERVER multiplexing many h2 streams on one connection. Running
 * the fan-out on the device makes the streams genuinely concurrent, and the
 * target's request log then witnesses that every nonce arrived.
 *
 * `timeout` bounds each probe so a wedged stream cannot park the whole script
 * (and thus the mocha test) — the point is a prompt, diagnosable failure.
 */
export function concurrentProbeScript(count, timeoutSec) {
  return `#!/system/bin/sh
# $1 = port, $2 = nonce prefix, $3 = count
PORT="$1"
PREFIX="$2"
COUNT="$3"
D=${DEVICE_TMP}/conc
mkdir -p "$D"
i=1
while [ "$i" -le "$COUNT" ]; do
  # Zero-padded so no nonce is a prefix of another ("-001" cannot collide with
  # "-010" the way "-1" and "-10" would in a substring search).
  N=$(printf '%03d' "$i")
  printf 'GET /tunnel-probe?nonce=%s-%s HTTP/1.1\\r\\nHost: probe\\r\\nConnection: close\\r\\n\\r\\n' "$PREFIX" "$N" \\
    | timeout ${timeoutSec} toybox nc -w ${timeoutSec} 127.0.0.1 "$PORT" > "$D/out.$N" 2>&1 &
  i=$((i+1))
done
wait
OK=0
i=1
while [ "$i" -le "$COUNT" ]; do
  N=$(printf '%03d' "$i")
  if grep -q "TARGET-ECHO:.*$PREFIX-$N " "$D/out.$N" 2>/dev/null; then OK=$((OK+1)); fi
  i=$((i+1))
done
echo "PROBED $COUNT OK $OK"
`;
}

/**
 * Fire `count` concurrent `-L` probes from the device, all carrying
 * `<prefix>-<n>` nonces, and return `{stdout, stderr, ms, ok, count}` where
 * `ok` is how many replies carried their own nonce back.
 */
export async function probeLocalPortConcurrent(port, prefix, count = 16, timeoutSec = L_PROBE_TIMEOUT_SEC) {
  const started = Date.now();
  const res = await runDeviceScript(
    'concprobe.sh',
    concurrentProbeScript(count, timeoutSec),
    [String(port), prefix, String(count)],
  );
  const m = /PROBED (\d+) OK (\d+)/.exec(res.stdout);
  return {
    ...res,
    ms: Date.now() - started,
    count: m ? Number(m[1]) : 0,
    ok: m ? Number(m[2]) : 0,
  };
}

/**
 * Start a `-L` probe DETACHED on the device and return immediately.
 *
 * Needed whenever something must happen while the probe is in flight (the
 * in-flight teardown test): Appium serialises its commands, so a blocking
 * `mobile: shell` probe would hold the session and a concurrent `bridge()` call
 * would only run after the probe had already finished — making the test
 * vacuous. The detached probe runs as its own process (via `setsid`, so it
 * outlives the adb shell) and writes:
 *
 *   ${DEVICE_TMP}/detached/<tag>.out   — the probe's stdout
 *   ${DEVICE_TMP}/detached/<tag>.done  — the literal text DONE, once it exits
 */
export async function startDetachedLocalProbe(port, nonce, tag) {
  const dir = `${DEVICE_TMP}/detached`;
  // The parent directory must exist BEFORE pushFile: Appium's pushFile does not
  // create it, and a missing directory fails the push.
  await deviceShell('mkdir', ['-p', dir]);
  // The probe itself is a pushed script (so the request's quotes/backslashes
  // survive Appium's unquoted argument joining — see runDeviceScript).
  await pushFile(
    `${dir}/${tag}.sh`,
    `#!/system/bin/sh
printf 'GET /tunnel-probe?nonce=%s HTTP/1.1\\r\\nHost: probe\\r\\nConnection: close\\r\\n\\r\\n' "$1" \\
  | toybox nc -w ${L_PROBE_TIMEOUT_SEC} 127.0.0.1 "$2"
`,
  );
  // A launcher is likewise a pushed script: setsid detaches the probe so the
  // adb shell can return while the probe is still running.
  await pushFile(
    `${dir}/${tag}.launch.sh`,
    `#!/system/bin/sh
D=${dir}
rm -f "$D/${tag}.done" "$D/${tag}.out"
setsid sh -c "$D/${tag}.sh '${nonce}' ${port} > $D/${tag}.out 2>&1; echo DONE > $D/${tag}.done" \\
  >/dev/null 2>&1 < /dev/null &
`,
  );
  await deviceShell('chmod', ['755', `${dir}/${tag}.sh`, `${dir}/${tag}.launch.sh`]);
  await deviceShell('sh', [`${dir}/${tag}.launch.sh`]);
}

/**
 * Wait for a detached probe started by {@link startDetachedLocalProbe} to
 * finish. Returns `{stdout, ms, finishedAt}` where `ms` is measured from the
 * CALLER's `startedAt` and `finishedAt` is an absolute timestamp.
 *
 * The caller wants `finishedAt` to measure a teardown relative to the moment it
 * ISSUED the teardown, not relative to the probe's launch: the launch-to-target
 * window is dominated by test polling and would otherwise be counted against the
 * teardown's budget.
 */
export async function awaitDetachedLocalProbe(tag, startedAt, timeoutMs = 30000) {
  const donePath = `${DEVICE_TMP}/detached/${tag}.done`;
  await waitFor(async () => {
    const text = await readDeviceFile(donePath);
    return text.includes('DONE') ? true : null;
  }, timeoutMs, `the detached probe ${tag} to finish`);
  const finishedAt = Date.now();
  const stdout = await readDeviceFile(`${DEVICE_TMP}/detached/${tag}.out`);
  return { stdout, ms: finishedAt - startedAt, finishedAt };
}

/**
 * Device-side `-R` target: answers every connection with a fixed HTTP body
 * carrying `marker`, and records every request it receives.
 *
 * Run detached with `setsid` so it survives the `mobile: shell` invocation that
 * started it (a plain `&` is killed when the adb shell session ends).
 *
 * ## Why `nc -L` (capital) and not a `while true; nc -l` loop
 *
 * The obvious shape — a shell loop that runs `nc -l -p PORT` once per
 * connection — drops connections. `nc` exits when its connection ends and the
 * loop needs a moment to start the next one; a connection arriving in that
 * window gets ECONNREFUSED. Measured on this image: a loop serving ~1
 * connection/s served only ~half of 20 sequential probes, and every failure was
 * `ECONNREFUSED` (not a timeout), including right after a successful one.
 * `nc -L` keeps ONE listening socket and forks a handler per connection, so
 * there is no window at all.
 *
 * ## Why the handler is `cat <file>; timeout N cat >> <log>`
 *
 * Three measured constraints:
 *
 *   - A shell builtin `printf` writing the response is BLOCK-buffered when
 *     stdout is a socket (not a TTY), so the bytes sit in the buffer until the
 *     handler exits — which it cannot do while it is still reading. The peer
 *     then times out. Emitting the response from a FILE with `cat` (a separate
 *     process that flushes on exit) avoids this.
 *   - The trailing `cat` records the request. It must be BOUNDED (`timeout`):
 *     in the `-R` direction the device target never observes the server-side
 *     client's half-close — the server relay only learns of it from the parked
 *     connection and does not forward EOF to the device (design §4.2.1, the
 *     same asymmetry in the other direction). An unbounded `cat` therefore
 *     blocks until the relay's drain grace tears the stream down, and the
 *     handler never exits.
 *   - toybox `cat` writes each chunk as it reads it (no stdio buffering to a
 *     file), so the request is on disk well before the bound expires.
 *
 * The response file is rebuilt on every start, so a restart with the same
 * marker is still deterministic.
 */
export function reverseTargetScript(marker) {
  return `#!/system/bin/sh
# $1 = port, $2 = marker
PORT="$1"
MARKER="$2"
D=${DEVICE_TMP}/rt
mkdir -p "$D"
BODY="DEVICE-TARGET: $MARKER"
LEN=\${#BODY}
printf 'HTTP/1.1 200 OK\\r\\nContent-Type: text/plain\\r\\nContent-Length: %s\\r\\nConnection: close\\r\\n\\r\\n%s' "$LEN" "$BODY" > "$D/resp"
# -L keeps one listening socket and forks a handler per connection (no rebind
# gap). The handler emits the fixed response, then records the request.
setsid toybox nc -L -p "$PORT" sh -c "cat $D/resp; timeout 2 cat >> $D/req" >/dev/null 2>&1 < /dev/null &
`;
}

/**
 * The device-side file the `-R` target appends every request to. Unique nonces
 * accumulate, so a spec can assert presence without resetting it.
 */
export const REVERSE_REQUEST_LOG = `${DEVICE_TMP}/rt/req`;

/**
 * A STALLING `-R` device target: accepts a connection, records the request, and
 * never replies and never closes.
 *
 * Used by the in-flight teardown test. The `-R` relay's lifetime is bounded by
 * the server's claim timeout, not by the target, so a stalling target keeps the
 * claim stream open until `removeReverseForwardedPort` closes the relay
 * (H2PortForwardTransport.java:263-267). The stall target is a plain `cat`
 * (no response, no bound), which blocks on its read until the relay is torn
 * down.
 */
export function reverseStallTargetScript() {
  return `#!/system/bin/sh
# $1 = port
PORT="$1"
D=${DEVICE_TMP}/rstall
mkdir -p "$D"
: > "$D/req"
# cat copies the request into the log and then blocks on the read: it never
# answers, so the connection stays in flight until something else closes it.
setsid toybox nc -L -p "$PORT" sh -c "cat >> $D/req" >/dev/null 2>&1 < /dev/null &
`;
}

/** The device-side file the stalling `-R` target records requests into. */
export const REVERSE_STALL_LOG = `${DEVICE_TMP}/rstall/req`;

/** Start the detached stalling `-R` target and wait until it is listening. */
export async function startReverseStallTarget(port) {
  const path = `${DEVICE_TMP}/rstall/stall.sh`;
  await deviceShell('mkdir', ['-p', `${DEVICE_TMP}/rstall`]);
  await pushFile(path, reverseStallTargetScript());
  await deviceShell('chmod', ['755', path]);
  await deviceShell('sh', [path, String(port)]);
  await waitFor(async () => {
    const { stdout } = await deviceShell('netstat', ['-ltn']);
    return stdout.includes(`:${port} `);
  }, 20000, `stalling device target on 127.0.0.1:${port} to be listening`);
}

/** Kill the stalling `-R` target. */
export async function stopReverseStallTarget() {
  await pushFile(
    `${DEVICE_TMP}/rstall/stop.sh`,
    `#!/system/bin/sh
pkill -f '[n]c -L -p ${R_DEVICE_STALL_PORT}'
rm -f ${DEVICE_TMP}/rstall/req
exit 0
`,
  );
  await deviceShell('sh', [`${DEVICE_TMP}/rstall/stop.sh`]);
}

/** Start the detached `-R` device target and wait until it is listening. */
export async function startReverseDeviceTarget(port, marker) {
  const path = `${DEVICE_TMP}/rt/rt.sh`;
  await stopReverseDeviceTarget();
  // mkdir as plain argv: Appium's mobile:shell does NOT quote, so `sh -c 'mkdir
  // -p <dir>'` arrives as `sh -c mkdir -p <dir>` and mkdir gets no argument
  // (observed: "mkdir: Needs 1 argument").
  await deviceShell('mkdir', ['-p', `${DEVICE_TMP}/rt`]);
  await pushFile(path, reverseTargetScript(marker));
  await deviceShell('chmod', ['755', path]);
  // setsid detaches the loop from this adb shell, so it outlives the invocation.
  await deviceShell('sh', [path, String(port), marker]);
  await waitFor(async () => {
    const { stdout } = await deviceShell('netstat', ['-ltn']);
    return stdout.includes(`:${port} `);
  }, 20000, `device-side target on 127.0.0.1:${port} to be listening`);
}

/**
 * Kill any leftover device-side target from a previous run.
 *
 * The bracketed patterns (`[n]c -L`) keep `pkill -f` from matching the `sh -c`
 * that runs it — an unbracketed pattern kills the killing shell. The whole
 * command is a pushed script so the brackets and quotes survive Appium's
 * unquoted argument joining.
 */
export async function stopReverseDeviceTarget() {
  await pushFile(
    `${DEVICE_TMP}/rt/stop.sh`,
    `#!/system/bin/sh
pkill -f '[n]c -L -p ${R_DEVICE_TARGET_PORT}'
pkill -f '[n]c -l -p ${R_DEVICE_TARGET_PORT}'
pkill -f '[r]t/rt.sh'
rm -f ${DEVICE_TMP}/rt/req
exit 0
`,
  );
  await deviceShell('sh', [`${DEVICE_TMP}/rt/stop.sh`]);
}

// -------------------------------------------------------- control plane ------

/** Fetch JSON from the Tier 2 control server (the runner's own channel). */
export async function control(path, init) {
  const res = await fetch(`${TARGET_CONTROL}${path}`, init);
  return res.json();
}

/** True when the `-L` target is accepting connections. */
export async function targetStatus() {
  return control('/__target/status');
}

export async function startTarget() {
  return control('/__target/start', { method: 'POST' });
}

export async function stopTarget() {
  return control('/__target/stop', { method: 'POST' });
}

export async function resetTargetLog() {
  return control('/__reset', { method: 'POST' });
}

/**
 * Select the target's reply mode (see target-server/server.mjs).
 *
 *   http  — reply on headers-complete, echoing the request (the default)
 *   eof   — reply ONLY after the client half-closes (proves the FIN traversed)
 *   raw   — read to EOF, echo bytes verbatim (binary transparency)
 *   stall — close the write side at once without replying (drain-grace case)
 *   delay — record the request, wait, then reply (in-flight with a deadline)
 *
 * An unknown mode is rejected by the target, so a typo fails loudly rather than
 * silently defaulting.
 */
export async function setTargetMode(mode) {
  const res = await control(`/__target/mode?mode=${encodeURIComponent(mode)}`, { method: 'POST' });
  assert.equal(res.ok, true, `target refused mode ${mode}: ${JSON.stringify(res)}`);
  return res;
}

/**
 * Prove the `-L` target is genuinely reachable in its own namespace, bypassing
 * the tunnel entirely. Used as a control: a failed tunnel assertion with a dead
 * target would prove nothing.
 *
 * Only meaningful in `http` mode (the only mode that answers a client which
 * keeps its write side open). It asserts that first so a mode left over from a
 * previous test cannot make the control fail for the wrong reason.
 */
export async function targetEchoDirect(nonce) {
  const status = await targetStatus();
  assert.equal(
    status.mode,
    'http',
    `targetEchoDirect needs http mode (target is in "${status.mode}"); ` +
      `call setTargetMode('http') first`,
  );
  return control(`/__target/echo?nonce=${encodeURIComponent(nonce)}`);
}

/**
 * Restore the target to the deterministic default state every byte-transfer
 * assertion assumes: `http` mode, listening, empty request log.
 *
 * Call this in `beforeEach`/at the top of a test that changes the mode, so a
 * failure cannot leak a mode (or a `hold` connection) into the next test.
 */
export async function resetTarget() {
  await setTargetMode('http');
  await startTarget();
  await resetTargetLog();
  return targetStatus();
}

/**
 * Dial the SERVER-side `-R` listener from inside the server's own network
 * namespace (the target container shares it) and return the raw reply.
 *
 * This is the server-side client the `-R` direction needs: the listener is bound
 * on the server's 127.0.0.1, so a process in that namespace is the only thing
 * that can reach it — the runner container cannot (its view of the emulator is
 * the compose-bridge address, not loopback).
 *
 * `ms` bounds how long this client waits for the device target to answer, so a
 * teardown test can tell "the teardown closed the connection" (a prompt
 * `error`/short reply) from "the socket timeout fired" (exactly `ms` elapsed
 * with no bytes). Default 8000.
 */
export async function reverseProbe(port, nonce, ms = 8000) {
  const started = Date.now();
  const res = await control(
    `/__reverse/probe?port=${port}&nonce=${encodeURIComponent(nonce)}&ms=${ms}`,
  );
  return { ...res, ms: Date.now() - started };
}
