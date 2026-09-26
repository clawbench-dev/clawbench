/**
 * Tier 2 / Stage 2b — `-L` over the h2 stream tunnel.
 *
 * `-L` is the primary path: the CLIENT owns a local ServerSocket
 * (H2PortForwardTransport.addLocal binds 127.0.0.1:<localPort>), and every
 * accepted connection opens one `POST /api/tunnel/stream?host=&port=` whose
 * request body carries client->server bytes and whose response body carries
 * server->client bytes.
 *
 * The headline result is `getActiveTunnelTransport()`: which wire actually won,
 * `tls` or `h2c`. Against this plain-HTTP server the TLS probe fails at the
 * handshake (fast, not a timeout) and h2c wins — both are accepted here, and the
 * observed value is printed.
 *
 * ## Authentication fixture
 *
 * The data streams are authenticated, and the app builds their `Cookie` header
 * from `CookieManager.getCookie(serverUrl)`. On this API-28 WebView that call
 * omits any `SameSite=Lax` cookie — including the one the app installed itself
 * (see the README and `helpers/tunnel.mjs`) — so `before()` installs a readable
 * cookie with the same real server token. Everything from
 * `AndroidTunnelPlatform.sessionCookie` onwards is the production path.
 */
import assert from 'node:assert/strict';
import crypto from 'node:crypto';
import {
  L_LOCAL_PORT,
  TARGET_PORT,
  installReadableSessionCookie,
  enterWebView,
  bridge,
  getActiveTunnelTransport,
  getTunnelError,
  getTunnelErrorType,
  probeLocalPort,
  clearDeviceLog,
  readDeviceLog,
  targetStatus,
  startTarget,
  stopTarget,
  resetTargetLog,
  targetEchoDirect,
  waitFor,
  waitForActiveWire,
} from './helpers/tunnel.mjs';

describe('Tier 2 — h2 tunnel -L', () => {
  before(async () => {
    // Deterministic starting state: a readable session cookie, the target up,
    // and its request log empty.
    await installReadableSessionCookie();
    await startTarget();
    await resetTargetLog();
  });

  after(async () => {
    try {
      await enterWebView();
      await bridge('removeForwardedPort', L_LOCAL_PORT);
    } catch {
      // Best-effort: the session may already be gone on a hard failure.
    }
    await startTarget();
  });

  it('opens a -L forward over h2 and reports which wire won', async () => {
    await enterWebView();
    await bridge('setTunnelTransport', 'h2');
    assert.equal(await bridge('getTunnelTransport'), 'h2');

    // The control proves the target is alive independently of the tunnel, so a
    // later failure cannot be blamed on a dead target.
    const directNonce = `direct-${crypto.randomUUID()}`;
    const direct = await targetEchoDirect(directNonce);
    assert.ok(direct.ok, `target is not reachable directly (bypassing the tunnel): ${JSON.stringify(direct)}`);

    await bridge('addForwardedPort', L_LOCAL_PORT, TARGET_PORT, '127.0.0.1');

    // addForwardedPort posts an intent to BackgroundService, which connects on
    // its own network thread — so poll for the session to come up.
    const wire = await waitForActiveWire(60000);

    // === HEADLINE RESULT ===
    console.log(`[tier2] getActiveTunnelTransport() = "${wire}"`);
    assert.ok(['tls', 'h2c'].includes(wire), `unexpected active transport "${wire}"`);

    const error = await getTunnelError();
    const errorType = await getTunnelErrorType();
    console.log(`[tier2] getTunnelError() = "${error}" getTunnelErrorType() = "${errorType}"`);
    // A live, healthy h2 session must report no session-level error. Asserting
    // it here (rather than only logging) turns a regression that leaves a stale
    // error on a successful connect into a failure.
    assert.equal(error, '', `a live h2 session reported a session error: "${error}"`);
    assert.equal(errorType, '', `a live h2 session reported an error type: "${errorType}"`);
  });

  it('makes the local port reachable from inside the emulator', async () => {
    await enterWebView();
    const reachable = await bridge('testPortReachable', L_LOCAL_PORT);
    console.log(`[tier2] testPortReachable(${L_LOCAL_PORT}) = ${reachable}`);
    assert.equal(reachable, true, `bridge reports 127.0.0.1:${L_LOCAL_PORT} is not accepting connections`);

    // A device-side TCP connect, independent of the bridge's own check. A bare
    // TCP accept would pass even if the tunnel carried nothing, so this asserts
    // the probe actually round-tripped a request to the target and got the
    // target's echo back: `TARGET-ECHO:` is emitted only by the server-side
    // target (target-server/server.mjs), never by the app or the listener.
    const probe = await probeLocalPort(L_LOCAL_PORT, 'reachability-only');
    console.log(
      `[tier2] device-side connect: stdout=${JSON.stringify(probe.stdout.slice(0, 120))} ` +
        `stderr=${JSON.stringify(probe.stderr.slice(0, 120))}`,
    );
    assert.ok(
      probe.stdout.includes('TARGET-ECHO:'),
      `the device-side probe did not get the target's echo (the listener accepted a ` +
        `connection but no bytes traversed the tunnel): ` +
        `stdout=${JSON.stringify(probe.stdout)} stderr=${JSON.stringify(probe.stderr)}`,
    );
  });

  it('round-trips real bytes through the tunnel to the server-side target', async () => {
    await enterWebView();
    await resetTargetLog();

    const nonce = `tunnel-${crypto.randomUUID()}`;
    const { stdout, stderr } = await probeLocalPort(L_LOCAL_PORT, nonce);
    console.log(`[tier2] device probe stdout = ${JSON.stringify(stdout.slice(0, 400))}`);
    if (stderr) console.log(`[tier2] device probe stderr = ${JSON.stringify(stderr.slice(0, 400))}`);

    // server -> client: the target echoes back exactly what it received, and the
    // nonce only exists in this test.
    assert.ok(
      stdout.includes('TARGET-ECHO:') && stdout.includes(nonce),
      `no echoed nonce came back through the tunnel. stdout=${JSON.stringify(stdout)} stderr=${JSON.stringify(stderr)}`,
    );

    // client -> server: the target recorded the exact request bytes. A second,
    // independent witness — the response alone could in principle be
    // synthesized, but the target's own log cannot.
    const status = await targetStatus();
    const sawNonce = (status.requests || []).some((r) => r.includes(nonce));
    console.log(`[tier2] target request log entries = ${(status.requests || []).length}, nonce seen = ${sawNonce}`);
    assert.ok(
      sawNonce,
      `the target never received the probe bytes; log=${JSON.stringify(status.requests)}`,
    );
  });

  it('drops the connection (no payload) when the target is down, and keeps the listener', async () => {
    await enterWebView();
    await resetTargetLog();

    // Stop the target. The local listener stays bound (it is independent of the
    // target), so the failure surfaces per-connection.
    await stopTarget();
    try {
      const status = await targetStatus();
      assert.equal(status.listening, false, 'target is still listening after stopTarget()');

      const nonce = `down-${crypto.randomUUID()}`;
      await clearDeviceLog();
      const { stdout, stderr } = await probeLocalPort(L_LOCAL_PORT, nonce);

      // No echo can come back: the server had nothing to dial.
      assert.ok(
        !stdout.includes('TARGET-ECHO:'),
        `the tunnel returned a target response while the target was down: ${JSON.stringify(stdout)}`,
      );
      console.log(
        `[tier2] probe against a down target: stdout=${JSON.stringify(stdout.slice(0, 160))} ` +
          `stderr=${JSON.stringify(stderr.slice(0, 160))}`,
      );

      // The target never saw the bytes either (independent witness: nothing
      // arrived because there was nothing listening).
      const after = await targetStatus();
      assert.equal(
        (after.requests || []).length,
        0,
        `target recorded requests while down: ${JSON.stringify(after.requests)}`,
      );

      // The listener must still be up: a per-connection target failure is not a
      // reason to tear down the forward.
      const stillReachable = await bridge('testPortReachable', L_LOCAL_PORT);
      assert.equal(
        stillReachable,
        true,
        'the local listener was torn down by a single failed connection',
      );

      // The PER-CONNECTION failure must be classified. This is the error field
      // that is meaningful for a dead target: the app's `serve()` catches the
      // failed dial and logs the classified kind (`TARGET_UNREACHABLE`, mapped
      // from the server's 502) plus the raw message. Without this assertion the
      // negative test would only prove "no payload came back", which a silently
      // dropped connection would also satisfy.
      const failureLine = await waitFor(async () => {
        const log = await readDeviceLog();
        return /H2: openStream \S+ failed: [A-Z_]+ .+/.exec(log)?.[0] ?? null;
      }, 15000, 'the per-connection dial failure to be classified in logcat');
      console.log(`[tier2] per-connection failure log = ${JSON.stringify(failureLine)}`);
      assert.ok(
        failureLine.includes('TARGET_UNREACHABLE'),
        `the failed dial to the down target was not classified as TARGET_UNREACHABLE: ` +
          `${JSON.stringify(failureLine)}`,
      );
      assert.ok(
        failureLine.includes('502'),
        `the classification did not carry the server's 502 status: ${JSON.stringify(failureLine)}`,
      );

      // The SESSION-level error surface must stay empty. The per-connection
      // dial failure path never writes BackgroundService.lastError: `serve()`
      // logs the classified kind and closes the relay, and returns. So the
      // session error is only ever set by session-level failures (auth,
      // connect, stream teardown), not by one dead target. Asserting emptiness
      // here is the meaningful check — asserting a non-empty type would be
      // asserting a bug.
      const errorType = await getTunnelErrorType();
      const error = await getTunnelError();
      console.log(
        `[tier2] after down-target probe: getTunnelErrorType()="${errorType}" ` +
          `getTunnelError()="${error}" (must stay empty — session is healthy)`,
      );
      assert.equal(
        errorType,
        '',
        `a per-connection target failure leaked into the session error type: "${errorType}"`,
      );
      assert.equal(
        error,
        '',
        `a per-connection target failure leaked into the session error message: "${error}"`,
      );
    } finally {
      await startTarget();
    }
  });

  it('removeForwardedPort tears the local listener down', async () => {
    await enterWebView();
    await bridge('removeForwardedPort', L_LOCAL_PORT);

    const gone = await waitFor(async () => {
      const reachable = await bridge('testPortReachable', L_LOCAL_PORT);
      return reachable === false;
    }, 20000, `127.0.0.1:${L_LOCAL_PORT} to stop accepting connections`);

    assert.equal(gone, true);
    console.log(`[tier2] after removeForwardedPort: testPortReachable(${L_LOCAL_PORT}) = false`);
  });
});
