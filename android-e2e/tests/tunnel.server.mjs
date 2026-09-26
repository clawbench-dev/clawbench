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
 * ## Why the probes are AUTHENTICATED (this used to be a vacuous test)
 *
 * The endpoints are registered via `register` (internal/handler/handler.go),
 * which wraps the handler in `middleware.Auth`. With a password configured —
 * the harness sets one — `Auth` returns **401 before the handler runs** for any
 * request without a valid session cookie (internal/middleware/auth.go). The
 * tunnel handlers' 503 branch lives INSIDE the handler
 * (internal/handler/tunnel_stream.go / tunnel_control.go, the
 * `service.ProxyService == nil` guard), so on an unauthenticated request it is
 * **unreachable**: the 401 is produced by the middleware regardless of the
 * registry's state.
 *
 * An earlier revision of this spec probed unauthenticated and asserted
 * `status !== 503`, with a comment claiming "a 503 here is the exact regression
 * this spec exists to catch". That assertion could not fail: no registry state
 * can make an unauthenticated request answer anything but 401. The probes below
 * therefore carry a real session cookie (`authFetch`), which is the only way
 * the handler — and its nil-registry guard — is actually reached.
 *
 * The 503 branch is then pinned by a POSITIVE CONTROL: the
 * `server-no-h2` compose service (server-config/config.no-h2.yaml, `enabled:
 * false` + `transport: ssh`) is the one combination that yields a nil registry,
 * and it is asserted to answer 503 on an authenticated request. Without that
 * control, "401 not 503" would be consistent with a 503 branch that does not
 * exist at all.
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
  NO_H2_SERVER,
  nativeLogin,
  bridge,
  enterWebView,
  getActiveTunnelTransport,
  resetAppToLoginPage,
  installReadableSessionCookie,
  authFetch,
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
    } else {
      // The assertion above is silently skipped when the variable is absent, so
      // say so loudly. run.sh always exports it; a direct `wdio` invocation does
      // not, and a skipped version check is exactly how a blocking mismatch
      // dialog would slip through.
      console.warn(
        '[tier2] CLAWBENCH_VERSION is unset — the APK/server version equality was NOT checked ' +
          '(run via scripts/run.sh, which exports it from output-metadata.json)',
      );
    }
  });

  it('answers /api/tunnel/stream with 401 when UNAUTHENTICATED (auth runs before the handler)', async () => {
    // No cookie: middleware.Auth must reject this before the handler. This is a
    // statement about the AUTH layer, not about the registry — see the header.
    // The registry assertion is the authenticated probe below.
    const res = await fetch(`${SERVER_FROM_RUNNER}/api/tunnel/stream?host=127.0.0.1&port=18080`, {
      method: 'POST',
      body: 'x',
    });
    const text = await res.text();
    console.log(`[tier2] POST /api/tunnel/stream (unauthenticated) -> ${res.status} ${text.slice(0, 120)}`);
    assert.equal(res.status, 401, `expected 401 (auth required) but got ${res.status}: ${text}`);
  });

  it('answers /api/tunnel/control with 401 when UNAUTHENTICATED', async () => {
    const res = await fetch(`${SERVER_FROM_RUNNER}/api/tunnel/control`, { method: 'POST', body: '' });
    const text = await res.text();
    console.log(`[tier2] POST /api/tunnel/control (unauthenticated) -> ${res.status} ${text.slice(0, 120)}`);
    assert.equal(res.status, 401, `expected 401 but got ${res.status}: ${text}`);
  });

  it('serves /api/tunnel/stream to an AUTHENTICATED request (registry exists for enabled:false + h2)', async () => {
    // THE registry assertion. The handler is now actually entered, so its nil
    // guard is reachable. The probe targets an UNLISTENED loopback port so the
    // handler's own dial fails deterministically: the expected status is 502
    // (TunnelTargetUnreachable), which proves the handler ran, passed the port
    // whitelist and reached the dial — i.e. the registry existed. A 503 would
    // mean service.ProxyService == nil (the regression); a 401 would mean the
    // cookie did not authenticate.
    const port = 18099; // nothing listens here; see the topology in the README
    const res = await authFetch(
      `${SERVER_FROM_RUNNER}/api/tunnel/stream?host=127.0.0.1&port=${port}`,
      { method: 'POST', body: 'x' },
    );
    const text = await res.text();
    console.log(`[tier2] POST /api/tunnel/stream (authenticated, dead target) -> ${res.status} ${text.slice(0, 160)}`);
    assert.notEqual(
      res.status,
      503,
      'the tunnel endpoints are unavailable (503): ProxyRegistry was not created for ' +
        'enabled:false + transport:h2 — see shouldCreateProxyRegistry',
    );
    assert.notEqual(res.status, 401, `an authenticated request was rejected by auth: ${text}`);
    assert.equal(
      res.status,
      502,
      `expected 502 (handler reached its dial and the target was unreachable), got ${res.status}: ${text}`,
    );
  });

  it('serves /api/tunnel/control to an AUTHENTICATED request (registry exists for enabled:false + h2)', async () => {
    // Same reasoning as the stream probe: the nil guard is inside the handler,
    // so only an authenticated request can reach it. A successful control bind
    // is an NDJSON stream, so the status is read from the headers and the body
    // is abandoned.
    const res = await authFetch(`${SERVER_FROM_RUNNER}/api/tunnel/control`, {
      method: 'POST',
      body: '',
    });
    console.log(`[tier2] POST /api/tunnel/control (authenticated) -> ${res.status}`);
    // Stop the stream (if it opened) without reading it to completion.
    try {
      await res.body?.cancel();
    } catch {
      // Already finished.
    }
    assert.notEqual(res.status, 503, 'the tunnel control endpoint is unavailable (503)');
    assert.notEqual(res.status, 401, 'an authenticated request was rejected by auth');
    assert.equal(
      res.status,
      200,
      `expected 200 (control stream opened) but got ${res.status} — a live registry must serve this`,
    );
  });

  it('POSITIVE CONTROL: enabled:false + transport:ssh really does answer 503 on both endpoints', async () => {
    // The only combination that yields a nil ProxyRegistry. Asserting 503 here
    // is what makes the "not 503" assertions above meaningful: it proves the
    // branch they rule out is real and reachable, rather than dead code that
    // could never fire.
    //
    // `authFetch` is given NO_H2_SERVER as its base so the token is minted on
    // :20002 and the port-scoped cookie name (`cb20002_clawbench_session`) is
    // derived from it. Authenticating against :20000 and then sending that
    // cookie to :20002 would be a 401, not a 503.
    const stream = await authFetch(
      `${NO_H2_SERVER}/api/tunnel/stream?host=127.0.0.1&port=18080`,
      { method: 'POST', body: 'x' },
      NO_H2_SERVER,
    );
    const streamText = await stream.text();
    console.log(
      `[tier2] POST /api/tunnel/stream (no-h2 config, authenticated) -> ${stream.status} ${streamText.slice(0, 160)}`,
    );
    assert.equal(
      stream.status,
      503,
      `expected 503 (nil ProxyRegistry) from the enabled:false + transport:ssh server, ` +
        `got ${stream.status}: ${streamText}`,
    );

    const control = await authFetch(
      `${NO_H2_SERVER}/api/tunnel/control`,
      { method: 'POST', body: '' },
      NO_H2_SERVER,
    );
    const controlText = await control.text();
    console.log(
      `[tier2] POST /api/tunnel/control (no-h2 config, authenticated) -> ${control.status} ${controlText.slice(0, 160)}`,
    );
    assert.equal(
      control.status,
      503,
      `expected 503 (nil ProxyRegistry) from the enabled:false + transport:ssh server, ` +
        `got ${control.status}: ${controlText}`,
    );
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

  it('reports the transport preference verbatim for each recognised value, and "" active before any connect', async () => {
    // P0 transport-preference matrix. The preference is a pure round-trip
    // through the JS bridge (setTunnelTransport -> BackgroundService static ->
    // getTunnelTransport); it must not be normalized or dropped. `getActive`
    // stays empty for every value because nothing has connected.
    await enterWebView();
    for (const pref of ['ssh', 'h2', 'both']) {
      await bridge('setTunnelTransport', pref);
      const got = await bridge('getTunnelTransport');
      console.log(`[tier2] setTunnelTransport(${pref}) -> getTunnelTransport() = ${got}`);
      assert.equal(got, pref, `preference ${pref} did not round-trip (got ${got})`);
      const active = await getActiveTunnelTransport();
      assert.equal(active, '', `getActiveTunnelTransport() must be "" before any connect, got "${active}"`);
    }
    // Restore the h2 preference for the specs that follow.
    await bridge('setTunnelTransport', 'h2');
    assert.equal(await bridge('getTunnelTransport'), 'h2');
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
