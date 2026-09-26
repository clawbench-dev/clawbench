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
 * (`AndroidTunnelPlatform.sessionCookie`). On this API-28 image that call does
 * NOT return a cookie the app itself installed with `CookieManager.setCookie()`
 * (the login response), even though:
 *
 *   - the cookie IS persisted (`sqlite app_webview/Cookies`), and
 *   - the WebView's network stack DOES send it (an auth-protected page fetch
 *     returns 200).
 *
 * A cookie written by the **renderer** (`document.cookie`) IS returned. So the
 * discriminator is who set the cookie, not the HttpOnly flag (verified against a
 * server built with `HttpOnly: false` — still unreadable). HttpOnly does matter
 * for the workaround: the app's own HttpOnly cookie cannot be overwritten by the
 * page, so the fixture must start from an app with no session cookie at all.
 *
 * `installReadableSessionCookie()` therefore wipes the app, lands it on the
 * server origin WITHOUT a password (so no cookie is installed), authenticates
 * from the runner to learn the token, and has the page write it. That is a
 * **harness fixture for a broken app-side read**, not a substitute for the
 * tunnel: every hop from `AndroidTunnelPlatform.sessionCookie` through OkHttp to
 * the server relay is still exercised. The defect itself is asserted separately
 * in `tunnel.server.mjs` and reported as a production recommendation.
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
/** `-R`: server-side listener port and its device-side target. */
export const R_SERVER_PORT = 17080;
export const R_DEVICE_TARGET_PORT = 18090;

export const HOME_MARKER = 'e2e-home-marker';

/** Device-side scratch dir. `/data/local/tmp` is world-writable and survives. */
const DEVICE_TMP = '/data/local/tmp';

/** The two wires the h2 transport can win on. */
export const H2_WIRES = ['tls', 'h2c'];

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

/** The transport preference in effect, e.g. `"h2"`. */
export async function getTunnelTransport() {
  return bridge('getTunnelTransport');
}

/** The wire that actually carried the last connect: `"tls"`, `"h2c"`, or `""`. */
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

/** Wait for a live h2 session and return the winning wire. */
export async function waitForActiveWire(timeoutMs = 60000) {
  return waitFor(async () => {
    const active = await getActiveTunnelTransport();
    return H2_WIRES.includes(active) ? active : null;
  }, timeoutMs, 'getActiveTunnelTransport() to report tls or h2c');
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
  const payload = Buffer.from(content, 'utf8').toString('base64');
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

// ------------------------------------------------- app state / login ---------

/** The login form's connect button (proves the app is on the login asset). */
const LOGIN_BUTTON = '#addConnectBtn';

/**
 * Wipe the app and relaunch it onto the first-launch login page.
 *
 * Required by the cookie fixture: the app's own session cookie is HttpOnly, so
 * the page cannot overwrite it, and `getCookie()` does not return it either.
 * Wiping the app is the only way to reach a state where the renderer can install
 * a readable cookie. `pm clear` also removes the persisted server URL, so the
 * app cold-starts on the login page rather than auto-connecting.
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
 * After this the app holds its OWN (HttpOnly) session cookie and is on the
 * server origin — but `getCookie()` cannot read that cookie, so this is used
 * only by the spec that asserts that defect, never by the tunnel fixtures.
 */
export async function nativeLogin() {
  await enterWebView();
  const href = await browser.execute(() => location.href).catch(() => '');
  if (typeof href === 'string' && href.startsWith(SERVER_URL)) return; // already in

  const connectBtn = await browser.$('input[name="addProtocol"][value="http"]');
  if (!(await connectBtn.isExisting())) {
    throw new Error(`not on the login page (location=${href})`);
  }
  await connectBtn.click();
  await fillField('#addHost', SERVER_HOST);
  await fillField('#addPort', SERVER_PORT);
  await fillField('#addPassword', PASSWORD);
  await browser.$('#addConnectBtn').scrollIntoView({ block: 'center', inline: 'center' });
  await browser.$('#addConnectBtn').click();

  await waitFor(async () => {
    const contexts = await browser.getContexts();
    const webview = contexts.find((c) => c.startsWith('WEBVIEW'));
    if (!webview) return false;
    if ((await browser.getContext()) !== webview) await browser.switchContext(webview);
    const h = await browser.execute(() => location.href);
    return typeof h === 'string' && h.startsWith(SERVER_URL) ? h : false;
  }, 90000, `the WebView to reach ${SERVER_URL}`);

  const errorVisible = await browser.execute(() => {
    const e = document.getElementById('addErrorMsg');
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
export async function fetchSessionToken() {
  const res = await fetch(`${SERVER_FROM_RUNNER}/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ password: PASSWORD }),
  });
  assert.equal(res.status, 200, 'the harness password is not accepted by the server');
  const setCookies = res.headers.getSetCookie
    ? res.headers.getSetCookie()
    : [res.headers.get('set-cookie')];
  const session = setCookies.find((c) => c && c.startsWith('clawbench_session='));
  assert.ok(session, `POST /login did not set a session cookie: ${JSON.stringify(setCookies)}`);
  return session.split(';')[0].slice('clawbench_session='.length);
}

/** Does the app's OWN cookie read (`shareFile` -> getCookie) yield a cookie? */
export async function appCanReadSessionCookie() {
  await deviceShell('logcat', ['-c']);
  await bridge('shareFile', '/etc/hosts', 'text/plain');
  await browser.pause(4000);
  const log = (await deviceShell('logcat', ['-d', '-s', 'ClawBench:*'])).stdout || '';
  return {
    readable: !log.includes('no auth cookie') && !log.includes('no server URL'),
    downloadCode: /download failed, code=(\d+)/.exec(log)?.[1] ?? null,
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
  // every later byte-transfer assertion would be vacuous.
  const read = await appCanReadSessionCookie();
  assert.ok(
    read.readable,
    `the fixture installed a cookie but the app still cannot read it: ${JSON.stringify(read)}`,
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
 */
export const L_PROBE_SCRIPT = `#!/system/bin/sh
# $1 = device-local port, $2 = nonce
PORT="$1"
NONCE="$2"
printf 'GET /tunnel-probe?nonce=%s HTTP/1.1\\r\\nHost: probe\\r\\nConnection: close\\r\\n\\r\\n' "$NONCE" \\
  | toybox nc -w 8 127.0.0.1 "$PORT"
`;

/**
 * Run the `-L` probe from the device and return `{stdout, stderr}`.
 * `nc` exits non-zero on a refused connection; that is returned, not thrown, so
 * the negative test can inspect it.
 */
export async function probeLocalPort(port, nonce) {
  return runDeviceScript('lprobe.sh', L_PROBE_SCRIPT, [String(port), nonce]);
}

/**
 * Device-side `-R` target: a loop that answers every connection with a fixed
 * HTTP body carrying `marker`, and records every request it receives.
 *
 * Run detached with `setsid` so it survives the `mobile: shell` invocation that
 * started it (a plain `&` is killed when the adb shell session ends).
 *
 * Two details are load-bearing and were measured on API 28's toybox:
 *
 *   - the response is written to a **regular file** and fed to `nc` on stdin.
 *     A `mkfifo` is denied here (`mkfifo: ...: Permission denied` — SELinux), and
 *     piping `printf ... | nc -l` instead would send the reply and immediately
 *     EOF, so `nc` exits before it has read the request.
 *   - requests are **appended** (`>>`) to a separate file, truncated once before
 *     the loop. Opening the same path with `>` on each iteration (the obvious
 *     shape) truncates the previous request before it can be read.
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
: > "$D/req"
while true; do
  printf 'HTTP/1.1 200 OK\\r\\nContent-Type: text/plain\\r\\nContent-Length: %s\\r\\nConnection: close\\r\\n\\r\\n%s' "$LEN" "$BODY" > "$D/resp"
  toybox nc -l -p "$PORT" < "$D/resp" >> "$D/req"
done
`;
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
  // The whole launch is a pushed script for the same quoting reason as above.
  await pushFile(
    `${DEVICE_TMP}/rt/launch.sh`,
    `#!/system/bin/sh\nsetsid ${path} ${port} ${marker} >/dev/null 2>&1 < /dev/null &\n`,
  );
  await deviceShell('sh', [`${DEVICE_TMP}/rt/launch.sh`]);
  await waitFor(async () => {
    const { stdout } = await deviceShell('netstat', ['-ltn']);
    return stdout.includes(`:${port} `);
  }, 20000, `device-side target on 127.0.0.1:${port} to be listening`);
}

/**
 * Kill any leftover device-side target loop from a previous run.
 *
 * The bracketed patterns (`[r]t/rt.sh`) keep `pkill -f` from matching the
 * `sh -c` that runs it — an unbracketed pattern kills the killing shell. The
 * whole command is a pushed script so the brackets and quotes survive Appium's
 * unquoted argument joining.
 */
export async function stopReverseDeviceTarget() {
  await pushFile(
    `${DEVICE_TMP}/rt/stop.sh`,
    `#!/system/bin/sh
pkill -f '[r]t/rt.sh'
pkill -f '[n]c -l -p ${R_DEVICE_TARGET_PORT}'
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
 * Prove the `-L` target is genuinely reachable in its own namespace, bypassing
 * the tunnel entirely. Used as a control: a failed tunnel assertion with a dead
 * target would prove nothing.
 */
export async function targetEchoDirect(nonce) {
  return control(`/__target/echo?nonce=${encodeURIComponent(nonce)}`);
}

/**
 * Dial the SERVER-side `-R` listener from inside the server's own network
 * namespace (the target container shares it) and return the raw reply.
 *
 * This is the server-side client the `-R` direction needs: the listener is bound
 * on the server's 127.0.0.1, so a process in that namespace is the only thing
 * that can reach it — the runner container cannot (its view of the emulator is
 * the compose-bridge address, not loopback).
 */
export async function reverseProbe(port, nonce) {
  return control(`/__reverse/probe?port=${port}&nonce=${encodeURIComponent(nonce)}`);
}
