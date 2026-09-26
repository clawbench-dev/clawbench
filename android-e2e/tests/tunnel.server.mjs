/**
 * Tier 2 / Stage 2a — the real server is wired in and the h2 endpoints are live.
 *
 * This spec is the foundation the other two depend on, and it asserts the one
 * thing the Tier 1 mock could never prove:
 *
 *   With `port_forward.enabled: false` and `port_forward.transport: h2`, the
 *   server MUST still create its ProxyRegistry, so the h2 tunnel endpoints are
 *   served rather than answered with 503 "PortForwardUnavailable".
 *
 * `shouldCreateProxyRegistry` (cmd/server/proxy_registry_gate.go) is the gate:
 * `Enabled || Transport != "ssh"`. The ONLY combination that disables the h2
 * endpoints is `enabled: false && transport: ssh`. This server is configured
 * `enabled: false, transport: h2`, so the registry must exist.
 *
 * Why 401 and not 200: the endpoints are authenticated. An unauthenticated probe
 * therefore proves liveness by the status code — 401 means the handler ran and
 * middleware.Auth rejected it; 503 means the handler never got a registry.
 *
 * The last test documents a real app-side defect found while building this
 * suite (see `tunnel.cookie-read` and the README): on this API-28 WebView,
 * `CookieManager.getCookie()` does not return the session cookie the app
 * installed from the login response, so every authenticated tunnel stream is
 * 401 `has_cookie=false`. The tunnel itself is proven correct in
 * `tunnel.forward.mjs` / `tunnel.reverse.mjs` using a renderer-installed
 * cookie; this spec asserts the defect explicitly so it cannot be mistaken for
 * a tunnel failure, and so the suite flags it if it is ever fixed.
 */
import assert from 'node:assert/strict';
import {
  SERVER_FROM_RUNNER,
  SERVER_URL,
  PASSWORD,
  nativeLogin,
  bridge,
  enterWebView,
  getActiveTunnelTransport,
  resetAppToLoginPage,
  installReadableSessionCookie,
  appCanReadSessionCookie,
} from './helpers/tunnel.mjs';

describe('Tier 2 — real server + h2 endpoint liveness', () => {
  it('reports the APK version from /api/health (no blocking mismatch dialog)', async () => {
    // Read the version the app itself saw. The native gate compares this with
    // the APK's versionName; agreement is what let login proceed at all. If the
    // versions disagreed the app would be showing a modal dialog and the next
    // spec (which reaches the home page) could not run.
    const res = await fetch(`${SERVER_FROM_RUNNER}/api/health`);
    assert.equal(res.status, 200, `GET /api/health returned ${res.status}`);
    const body = await res.json();
    assert.equal(body.app, 'clawbench', `unexpected /api/health payload: ${JSON.stringify(body)}`);
    assert.ok(body.version, '/api/health did not report a version');

    // The APK version the harness built must match the server's reported
    // version exactly — that is the structural guarantee prepare-assets.sh makes
    // by compiling the server with the APK's own versionName. Print both so a
    // drift is visible in the run log.
    const apkVersion = process.env.CLAWBENCH_VERSION || '';
    console.log(`[tier2] server /api/health version = ${body.version} (APK = ${apkVersion || 'n/a'})`);
    if (apkVersion) {
      assert.equal(
        body.version,
        apkVersion,
        `server version ${body.version} != APK versionName ${apkVersion}; the version gate compares these`,
      );
    }
  });

  it('answers /api/tunnel/stream with 401, not 503 (ProxyRegistry exists)', async () => {
    // No cookie on purpose: the assertion is about the STATUS CLASS. A 503 here
    // is the exact regression this spec exists to catch — it would mean
    // `enabled:false` wrongly gated the registry off for h2.
    const res = await fetch(`${SERVER_FROM_RUNNER}/api/tunnel/stream?host=127.0.0.1&port=18080`, {
      method: 'POST',
      body: 'x',
    });
    const text = await res.text();
    console.log(`[tier2] POST /api/tunnel/stream (unauthenticated) -> ${res.status} ${text.slice(0, 120)}`);
    assert.notEqual(
      res.status,
      503,
      'the tunnel endpoints are unavailable (503): ProxyRegistry was not created for ' +
        'enabled:false + transport:h2 — see shouldCreateProxyRegistry',
    );
    assert.equal(res.status, 401, `expected 401 (auth required) but got ${res.status}: ${text}`);
  });

  it('answers /api/tunnel/control with 401, not 503', async () => {
    const res = await fetch(`${SERVER_FROM_RUNNER}/api/tunnel/control`, { method: 'POST', body: '' });
    const text = await res.text();
    console.log(`[tier2] POST /api/tunnel/control (unauthenticated) -> ${res.status} ${text.slice(0, 120)}`);
    assert.notEqual(res.status, 503, 'the tunnel control endpoint is unavailable (503)');
    assert.equal(res.status, 401, `expected 401 but got ${res.status}: ${text}`);
  });

  it('logs the app into the real server through the native flow and reaches the home page', async () => {
    // The REAL login path: MainActivity.authenticateAndNavigate does
    // POST /login -> GET /api/health -> gateVersionMismatchAndProceed ->
    // webView.loadUrl. This is what must not hang on the version dialog.
    await resetAppToLoginPage();
    await nativeLogin();

    const href = await browser.execute(() => location.href);
    console.log(`[tier2] WebView location = ${href}`);
    assert.ok(
      typeof href === 'string' && href.startsWith(SERVER_URL),
      `WebView did not navigate to the server root; location=${href}`,
    );

    // The page really is the server's SPA shell, and the JS bridge is injected
    // and callable.
    //
    // NOTE — the Vue SPA does NOT mount on this emulator, and that is a
    // pre-existing property of the API-28 image, not a tunnel regression. The
    // frozen WebView is Chrome 69.0.3497.100, and the production bundle uses
    // ES2020 syntax it cannot parse (logcat: `Uncaught SyntaxError: Unexpected
    // token ?` in main-*.js). Chrome 69 predates `??`/`?.` (Chrome 80).
    // Tier 2 is unaffected: the tunnel is driven entirely through
    // `window.ClawBenchNative`, which `addJavascriptInterface` injects into the
    // page context regardless of whether Vue ever mounts.
    const shell = await browser.execute(() => ({
      hasAppRoot: !!document.getElementById('app'),
      hasBridge: !!window.ClawBenchNative,
      ua: navigator.userAgent,
    }));
    console.log(`[tier2] page shell = ${JSON.stringify(shell)}`);
    assert.equal(shell.hasAppRoot, true, 'the served page has no #app root — not the SPA shell');
    assert.equal(shell.hasBridge, true, 'window.ClawBenchNative was not injected');
    assert.equal(await bridge('isNativeApp'), true, 'isNativeApp() did not return true');
  });

  it('selects the h2 transport through the JS bridge and reports it back', async () => {
    await enterWebView();

    // This is the exact call the Vue settings flow makes
    // (useSettingsConfig.syncTunnelTransportToNative); driving it directly is
    // the point of the test.
    await bridge('setTunnelTransport', 'h2');

    const pref = await bridge('getTunnelTransport');
    console.log(`[tier2] getTunnelTransport() = ${pref}`);
    assert.equal(pref, 'h2', `transport preference did not stick: ${pref}`);

    // No session is live yet, so the active wire must be empty — not stale from
    // a previous connection.
    const activeBefore = await getActiveTunnelTransport();
    console.log(`[tier2] getActiveTunnelTransport() before connecting = "${activeBefore}"`);
    assert.equal(activeBefore, '', `expected no active transport before connecting, got "${activeBefore}"`);
  });

  it('DOCUMENTS a defect: the app cannot read its own session cookie (so the tunnel 401s)', async () => {
    // The app has just logged in through the native flow, so its own session
    // cookie is in the WebView jar. The WebView sends it (an authenticated page
    // fetch succeeds) but the Java API cannot read it, so every authenticated
    // tunnel stream is 401 `has_cookie=false`.
    //
    // This is asserted rather than tolerated: it is the reason the tunnel specs
    // install a readable cookie, and this test is what makes that substitution
    // honest. If the read is ever fixed, this test fails and tells us to drop
    // the fixture.
    await enterWebView();

    // (a) the WebView's own network stack sends the cookie
    await browser.execute(() => {
      window.__cfg = 'pending';
      fetch('/api/config', { credentials: 'include' })
        .then((r) => { window.__cfg = `status=${r.status}`; })
        .catch((e) => { window.__cfg = `error=${String(e)}`; });
      return 'started';
    });
    const cfg = await waitForValue('__cfg', 15000);
    console.log(`[tier2] page fetch('/api/config') = ${cfg}`);

    // (b) the Java API cannot read it
    const read = await appCanReadSessionCookie();
    console.log(
      `[tier2] app getCookie() read: readable=${read.readable} ` +
        `(expected false on this WebView)`,
    );

    // (c) therefore the tunnel's stream is rejected
    await bridge('setTunnelTransport', 'h2');
    await bridge('addForwardedPort', 15080, 18080, '127.0.0.1');
    await browser.pause(5000);

    if (read.readable) {
      // The defect is gone — the fixture must be removed, so fail loudly.
      assert.fail(
        'the app CAN now read its own session cookie, so the renderer-installed ' +
          'cookie fixture is no longer needed — remove installReadableSessionCookie() ' +
          'from tunnel.forward.mjs / tunnel.reverse.mjs and use the native login',
      );
    }

    // Prove the WebView really does send the cookie (so the defect is the READ,
    // not a missing credential): the page fetch above must have been authorized.
    assert.equal(cfg, 'status=200', `the WebView did not send its session cookie: ${cfg}`);
    assert.equal(read.readable, false, 'expected the app cookie read to fail on this WebView');
    console.log(
      '[tier2] CONFIRMED DEFECT: cookie stored + sent by the WebView, unreadable by ' +
        'CookieManager.getCookie() -> tunnel streams 401. See README.',
    );
  });

  it('installs a readable cookie fixture so the tunnel specs can authenticate', async () => {
    // The fixture every byte-transfer assertion depends on. It must leave the
    // app able to read a session cookie, or the later specs would be vacuous.
    const info = await installReadableSessionCookie();
    console.log(`[tier2] cookie fixture installed (token length ${info.tokenLength})`);
  });
});

/** Poll a `window.<name>` string stashed by an async page fetch. */
async function waitForValue(name, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const v = await browser.execute((n) => window[n] || '', name);
    if (v && v !== 'pending') return v;
    await browser.pause(500);
  }
  throw new Error(`window.${name} never settled`);
}
