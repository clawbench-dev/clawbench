/**
 * Tier 1 smoke test: prove the native shell + in-WebView login flow works on a
 * real Android runtime.
 *
 * Path under test:
 *   MainActivity launch -> file:///android_asset/login.html (first launch)
 *     -> switch into the WEBVIEW_* context
 *     -> fill host/port/password, pick HTTP
 *     -> native connectToServer(): POST /login, then (on 200) GET /api/health,
 *        inject the auth cookie
 *     -> webView.loadUrl(serverUrl) -> GET / -> mock home page carrying
 *        E2E_HOME_MARKER
 *
 * The request order is significant and asserted: `authenticateAndNavigate`
 * (`MainActivity.java:1274`) calls `performLoginRequest` (POST /login) FIRST, and
 * only inside `handleAuthResponse` on a 200 does it call `performHealthCheck`
 * (GET /api/health, :1696) before `webView.loadUrl(url)` issues GET / (:1711).
 *
 * The make-or-break dependency is the WebView context switch; if that regresses
 * the very first assertion fails loudly with the available contexts.
 */
import assert from 'node:assert/strict';

const APP_PACKAGE = process.env.E2E_APP_PACKAGE || 'com.clawbench.app.debug';
// The mock server shares the emulator's network namespace, so the app reaches it
// at 10.0.2.2 (SLIRP's alias for the emulator's host loopback). The runner is a
// different container: it reaches the same listener through the emulator's
// compose-bridge address, which is how we read the request log below.
const MOCK_HOST = process.env.E2E_MOCK_HOST || '10.0.2.2';
const MOCK_PORT = process.env.E2E_MOCK_PORT || '20000';
const MOCK_CONTROL = process.env.E2E_MOCK_CONTROL || `http://emulator:${MOCK_PORT}`;
const HOME_MARKER = 'e2e-home-marker';
const PASSWORD = 'e2e-password';

/** Poll the mock server's request log until `pred` is satisfied. */
async function waitForMockRequest(pred, timeoutMs, what) {
  const deadline = Date.now() + timeoutMs;
  let last = [];
  while (Date.now() < deadline) {
    try {
      const res = await fetch(`${MOCK_CONTROL}/__requests`);
      last = await res.json();
      if (pred(last)) return last;
    } catch (e) {
      // mock not up yet / transient; keep polling
    }
    await browser.pause(500);
  }
  throw new Error(
    `timed out waiting for ${what}; mock saw: ${JSON.stringify(last.map((r) => `${r.method} ${r.path}`))}`,
  );
}

/**
 * Defensive fill: scroll the field into view, then clear + type, and dismiss any
 * soft IME that appeared.
 *
 * scripts/run.sh disables every IME before the suite, so in the normal case no
 * keyboard appears and `hideKeyboard()` throws immediately (there is nothing to
 * hide). It is kept as a belt-and-braces measure: if an IME ever does surface
 * (e.g. a future image re-enables one), the shrunk, vertically-centered login
 * form would collapse and the *next* field interaction would fail with
 * "element not interactable". Hiding after each field keeps the form full-height
 * regardless.
 */
async function fillField(selector, value) {
  const el = await browser.$(selector);
  await el.scrollIntoView({ block: 'center', inline: 'center' });
  await el.clearValue();
  await el.setValue(value);
  try {
    await browser.hideKeyboard();
  } catch (e) {
    // Expected when no IME is shown ("no keyboard" / unsupported); ignore.
  }
}

describe('ClawBench Android shell — Tier 1 login smoke', () => {
  before(async () => {
    // Reset the mock's request log so the final test's ordering assertion sees
    // only this run's app traffic (and not a previous run's, or the runner's own
    // /__requests polling).
    try {
      await fetch(`${MOCK_CONTROL}/__reset`, { method: 'POST' });
    } catch (e) {
      // Non-fatal: the per-request waits below are membership-based, so a reset
      // failure only weakens the ordering assertion, not the flow assertions.
    }
  });

  it('launches into the first-launch login page (native context)', async () => {
    // fullReset reinstalls the app, so SharedPreferences are empty and
    // MainActivity loads file:///android_asset/login.html.
    const activity = await browser.getCurrentActivity();
    assert.ok(
      activity.includes('MainActivity'),
      `expected MainActivity, got ${activity}`,
    );

    // The app starts on the native login asset; give the WebView time to load
    // before asserting on contexts (getContexts() only lists WEBVIEW once a page
    // is attached).
    await browser.pause(2000);
  });

  it('exposes a WEBVIEW_* context for the login page', async () => {
    let contexts = [];
    const deadline = Date.now() + 60000;
    while (Date.now() < deadline) {
      contexts = await browser.getContexts();
      if (contexts.some((c) => c.startsWith('WEBVIEW'))) break;
      await browser.pause(1000);
    }
    console.log(`[tier1] getContexts() = ${JSON.stringify(contexts)}`);
    assert.ok(
      contexts.includes('NATIVE_APP'),
      `expected NATIVE_APP in contexts, got ${JSON.stringify(contexts)}`,
    );
    const webview = contexts.find((c) => c.startsWith('WEBVIEW'));
    assert.ok(
      webview,
      `no WEBVIEW_* context found — the WebView is not debuggable or not loaded. ` +
      `contexts=${JSON.stringify(contexts)}`,
    );
    await browser.switchContext(webview);
    assert.equal(await browser.getContext(), webview);
  });

  it('renders the server-address form inside the WebView', async () => {
    for (const id of ['addHost', 'addPort', 'addPassword', 'addConnectBtn']) {
      const el = await browser.$(`#${id}`);
      assert.ok(await el.isExisting(), `#${id} not found in the WebView DOM`);
    }
  });

  it('connects to the mock server and reaches the home page', async () => {
    // Protocol: HTTP (the form defaults to HTTPS).
    await browser.$('input[name="addProtocol"][value="http"]').click();

    await fillField('#addHost', MOCK_HOST);
    await fillField('#addPort', MOCK_PORT);
    await fillField('#addPassword', PASSWORD);

    await browser.$('#addConnectBtn').scrollIntoView({ block: 'center', inline: 'center' });
    await browser.$('#addConnectBtn').click();

    // Native order: POST /login first, then GET /api/health only on a 200
    // (`authenticateAndNavigate` -> `handleAuthResponse`), then the WebView
    // loads the server root.
    await waitForMockRequest(
      (reqs) => reqs.some((r) => r.path === '/login' && r.method === 'POST'),
      60000,
      'POST /login from the app',
    );
    await waitForMockRequest(
      (reqs) => reqs.some((r) => r.path === '/api/health'),
      60000,
      'GET /api/health from the app',
    );

    // The app navigates the WebView to the server root; the marker proves we left
    // login.html. Re-resolve the context: the page change can spawn a new
    // WebView context name.
    const deadline = Date.now() + 90000;
    let found = false;
    while (Date.now() < deadline && !found) {
      const contexts = await browser.getContexts();
      const webview = contexts.find((c) => c.startsWith('WEBVIEW'));
      if (webview) {
        if ((await browser.getContext()) !== webview) {
          await browser.switchContext(webview);
        }
        const marker = await browser.$(`#${HOME_MARKER}`);
        if (await marker.isExisting()) {
          found = true;
          assert.equal(await marker.getText(), 'e2e-home-ok');
        }
      }
      if (!found) await browser.pause(1500);
    }
    assert.ok(found, `home marker #${HOME_MARKER} never appeared after login`);

    // The error banner must not be showing.
    const errorVisible = await browser.execute(
      () => {
        const e = document.getElementById('addErrorMsg');
        return !!e && getComputedStyle(e).display !== 'none';
      },
    );
    assert.equal(errorVisible, false, 'login error banner is visible');
  });

  it('records the native auth requests in order: POST /login -> GET /api/health -> GET /', async () => {
    const reqs = await (await fetch(`${MOCK_CONTROL}/__requests`)).json();
    const paths = reqs.map((r) => `${r.method} ${r.path}`);
    console.log(`[tier1] mock request log: ${JSON.stringify(paths)}`);

    // Strip this test's own control traffic (/__requests, /__reset) so the
    // ordering assertion below sees only what the app did.
    const appReqs = reqs.filter((r) => !r.path.startsWith('/__'));
    const appPaths = appReqs.map((r) => `${r.method} ${r.path}`);

    // The app is known to issue these three, in this exact order:
    //   POST /login      (authenticateAndNavigate)
    //   GET  /api/health (handleAuthResponse, only after a 200 login)
    //   GET  /           (webView.loadUrl, after the health check)
    // Asserting the order (not just membership) catches a regression that
    // reorders or drops a step, which the previous membership-only check missed.
    const idx = (method, path) =>
      appReqs.findIndex((r) => r.method === method && r.path.split('?')[0] === path);
    const login = idx('POST', '/login');
    const health = idx('GET', '/api/health');
    const home = appReqs.findIndex(
      (r) => r.method === 'GET' && (r.path === '/' || r.path.startsWith('/?')),
    );

    assert.ok(login >= 0, `missing POST /login; log=${JSON.stringify(appPaths)}`);
    assert.ok(health >= 0, `missing GET /api/health; log=${JSON.stringify(appPaths)}`);
    assert.ok(home >= 0, `home page never fetched; log=${JSON.stringify(appPaths)}`);
    assert.ok(
      login < health,
      `expected POST /login before GET /api/health; log=${JSON.stringify(appPaths)}`,
    );
    assert.ok(
      health < home,
      `expected GET /api/health before GET /; log=${JSON.stringify(appPaths)}`,
    );
  });
});
