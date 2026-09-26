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
 * The session-cookie read on this image is a separate, image-specific WebView
 * defect (Chrome 69's `CookieManager.getCookie()` omits SameSite cookies — see
 * the README and `helpers/tunnel.mjs`). It is NOT asserted here: a test that
 * goes red when the product improves is a fix-inhibitor, not a guard. The
 * fixture the tunnel specs use is validated positively inside
 * `installReadableSessionCookie()` instead.
 */
import assert from 'node:assert/strict';
import {
  SERVER_FROM_RUNNER,
  SERVER_URL,
  nativeLogin,
  bridge,
  enterWebView,
  getActiveTunnelTransport,
  resetAppToLoginPage,
  installReadableSessionCookie,
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

  it('installs a readable cookie fixture so the tunnel specs can authenticate', async () => {
    // The fixture every byte-transfer assertion depends on. It must leave the
    // app able to read a session cookie, or the later specs would be vacuous —
    // and `installReadableSessionCookie` now proves that POSITIVELY (a non-401
    // server status for an authenticated request), so this cannot pass on an
    // empty logcat or a `shareFile` early-return.
    const info = await installReadableSessionCookie();
    console.log(`[tier2] cookie fixture installed (token length ${info.tokenLength})`);
  });
});
