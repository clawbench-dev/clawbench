/**
 * Tier 1 smoke test: prove the native shell + in-WebView login flow works on a
 * real Android runtime.
 *
 * Path under test:
 *   MainActivity launch -> file:///android_asset/login.html (first launch)
 *     -> switch into the WEBVIEW_* context
 *     -> fill host/port/password, pick HTTP
 *     -> native connectToServer(): GET /api/health, POST /login, inject cookie
 *     -> webView.loadUrl(serverUrl) -> mock home page carrying E2E_HOME_MARKER
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

describe('ClawBench Android shell — Tier 1 login smoke', () => {
  before(async () => {
    // Reset the mock's request log so assertions see only this run's traffic.
    try {
      await fetch(`${MOCK_CONTROL}/__reset`, { method: 'POST' });
    } catch (e) {
      // Non-fatal: assertions below are order-independent predicates.
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

    const host = await browser.$('#addHost');
    await host.clearValue();
    await host.setValue(MOCK_HOST);

    const port = await browser.$('#addPort');
    await port.clearValue();
    await port.setValue(MOCK_PORT);

    const password = await browser.$('#addPassword');
    await password.clearValue();
    await password.setValue(PASSWORD);

    await browser.$('#addConnectBtn').click();

    // The native layer now does GET /api/health then POST /login before loading
    // the server URL into the WebView.
    await waitForMockRequest(
      (reqs) => reqs.some((r) => r.path === '/api/health'),
      60000,
      'GET /api/health from the app',
    );
    await waitForMockRequest(
      (reqs) => reqs.some((r) => r.path === '/login' && r.method === 'POST'),
      60000,
      'POST /login from the app',
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

  it('recorded the expected request sequence at the mock server', async () => {
    const reqs = await (await fetch(`${MOCK_CONTROL}/__requests`)).json();
    const paths = reqs.map((r) => `${r.method} ${r.path}`);
    console.log(`[tier1] mock request log: ${JSON.stringify(paths)}`);
    assert.ok(paths.includes('GET /api/health'), 'missing GET /api/health');
    assert.ok(paths.includes('POST /login'), 'missing POST /login');
    // The WebView must have fetched the home page after auth.
    assert.ok(
      reqs.some((r) => r.method === 'GET' && (r.path === '/' || r.path.startsWith('/?'))),
      `home page never fetched; log=${JSON.stringify(paths)}`,
    );
  });
});
