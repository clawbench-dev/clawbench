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
 *
 * ## Test isolation
 *
 * Each test that depends on a forward creates what it needs (`ensureLocalForward`)
 * rather than leaning on a previous test's state: a failure in an early test
 * otherwise turns every later one into a confusing failure. Each test that
 * changes the target's mode restores it, so a failure cannot leak a mode into
 * the next test.
 */
import assert from 'node:assert/strict';
import crypto from 'node:crypto';
import {
  L_LOCAL_PORT,
  L_LOCAL_PORT_2,
  TARGET_PORT,
  installReadableSessionCookie,
  enterWebView,
  bridge,
  getActiveTunnelTransport,
  getTunnelError,
  getTunnelErrorType,
  probeLocalPort,
  probeLocalPortRaw,
  probeLocalPortConcurrent,
  probeLocalPortHalfClose,
  probeLocalPortStalled,
  startDetachedLocalProbe,
  awaitDetachedLocalProbe,
  clearDeviceLog,
  readDeviceLog,
  targetStatus,
  startTarget,
  stopTarget,
  resetTargetLog,
  setTargetMode,
  resetTarget,
  targetEchoDirect,
  waitFor,
  waitForActiveWire,
} from './helpers/tunnel.mjs';

/** The `-L` forwards this spec owns, so teardown is complete. */
const OWNED_LOCAL_PORTS = [L_LOCAL_PORT, L_LOCAL_PORT_2];

/**
 * Bring up (or reuse) the `-L` forward for `localPort` and wait until the h2
 * session is live. Idempotent, so every test can call it without depending on a
 * previous test having succeeded.
 */
async function ensureLocalForward(localPort = L_LOCAL_PORT) {
  await enterWebView();
  await bridge('setTunnelTransport', 'h2');
  const existing = JSON.parse(await bridge('getForwardedPorts')).find(
    (p) => p.port === localPort && p.direction === 'forward',
  );
  if (!existing) {
    await bridge('addForwardedPort', localPort, TARGET_PORT, '127.0.0.1');
  }
  // addForwardedPort posts an intent to BackgroundService, which connects on
  // its own network thread — so poll for the session to come up.
  const wire = await waitForActiveWire(60000);
  // The listener binds only once the transport is connected and the replay ran;
  // poll so a caller cannot race it.
  await waitFor(async () => (await bridge('testPortReachable', localPort)) === true, 30000,
    `127.0.0.1:${localPort} to accept connections`);
  return wire;
}

describe('Tier 2 — h2 tunnel -L', () => {
  before(async () => {
    // Deterministic starting state: a readable session cookie, the target up in
    // http mode, and its request log empty.
    await installReadableSessionCookie();
    await resetTarget();
  });

  after(async () => {
    try {
      await enterWebView();
      for (const port of OWNED_LOCAL_PORTS) {
        await bridge('removeForwardedPort', port);
      }
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
    await ensureLocalForward();
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
        `stderr=${JSON.stringify(probe.stderr.slice(0, 120))} ms=${probe.ms}`,
    );
    assert.ok(
      probe.stdout.includes('TARGET-ECHO:'),
      `the device-side probe did not get the target's echo (the listener accepted a ` +
        `connection but no bytes traversed the tunnel): ` +
        `stdout=${JSON.stringify(probe.stdout)} stderr=${JSON.stringify(probe.stderr)}`,
    );
  });

  it('round-trips real bytes through the tunnel to the server-side target', async () => {
    await ensureLocalForward();
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

  it('reconnectTunnel() preserves the -L forward and its reachability', async () => {
    // The exact regression commit c225cd7a fixed: under h2, reconnectTunnel()
    // reported success while every local listener stayed closed (the ready-time
    // rebuild lived only in ssh2's client.on('ready'), and h2's connect() just
    // flips state.connected). It was previously unguarded end to end.
    //
    // The observable is threefold, because each alone can pass while the bug is
    // present:
    //   - getActiveTunnelTransport() is a live h2 wire (the session reconnected);
    //   - testPortReachable(L_LOCAL_PORT) is true (the LISTENER was rebuilt —
    //     the bug left it closed while the session was up);
    //   - a FRESH nonce round-trips (the rebuilt listener really carries bytes,
    //     not just accepts a TCP connection).
    await ensureLocalForward();

    // reconnectTunnel() blocks the JavaBridge thread for up to 15s; it returns a
    // boolean. Call it directly so the assertion is about the reconnect result,
    // not an async callback.
    const ok = await bridge('reconnectTunnel');
    console.log(`[tier2] reconnectTunnel() = ${ok}`);
    assert.equal(ok, true, 'reconnectTunnel() reported failure');

    // The wire must still be a live h2 session after the reconnect. reconnectTunnel
    // is blocking and completes ensureConnection, but poll anyway: the h2 kind
    // is published as the session comes up.
    const wire = await waitFor(
      async () => {
        const w = await getActiveTunnelTransport();
        return ['tls', 'h2c'].includes(w) ? w : null;
      },
      30000,
      'getActiveTunnelTransport() to report a live h2 wire after reconnect',
    );
    console.log(`[tier2] after reconnect: getActiveTunnelTransport() = "${wire}"`);

    // The listener must have been rebuilt. Poll: the replay runs on the
    // network thread just after the session comes up.
    const reachable = await waitFor(
      async () => (await bridge('testPortReachable', L_LOCAL_PORT)) === true,
      30000,
      `127.0.0.1:${L_LOCAL_PORT} to be reachable again after reconnect`,
    );
    assert.equal(reachable, true, 'the -L listener was not rebuilt by reconnectTunnel()');

    // And it must carry bytes — a listener that accepts but relays nothing would
    // satisfy testPortReachable alone.
    await resetTargetLog();
    const nonce = `reconnect-${crypto.randomUUID()}`;
    const { stdout, stderr } = await probeLocalPort(L_LOCAL_PORT, nonce);
    console.log(`[tier2] post-reconnect probe stdout = ${JSON.stringify(stdout.slice(0, 240))}`);
    assert.ok(
      stdout.includes('TARGET-ECHO:') && stdout.includes(nonce),
      `the rebuilt listener did not carry bytes after reconnect: ` +
        `stdout=${JSON.stringify(stdout)} stderr=${JSON.stringify(stderr)}`,
    );
    const status = await targetStatus();
    assert.ok(
      (status.requests || []).some((r) => r.includes(nonce)),
      `the target never saw the post-reconnect probe; log=${JSON.stringify(status.requests)}`,
    );
  });

  it('two simultaneous local forwards carry independent traffic', async () => {
    // Only 15080 was ever exercised. Two listeners at once proves the per-port
    // listener map and the per-connection stream fan-out are independent, which
    // a single forward cannot distinguish from a global singleton.
    const wire = await ensureLocalForward(L_LOCAL_PORT);
    console.log(`[tier2] first forward wire = "${wire}"`);
    await ensureLocalForward(L_LOCAL_PORT_2);

    const reach1 = await bridge('testPortReachable', L_LOCAL_PORT);
    const reach2 = await bridge('testPortReachable', L_LOCAL_PORT_2);
    console.log(`[tier2] reachable: ${L_LOCAL_PORT}=${reach1} ${L_LOCAL_PORT_2}=${reach2}`);
    assert.equal(reach1, true, `127.0.0.1:${L_LOCAL_PORT} is not reachable`);
    assert.equal(reach2, true, `127.0.0.1:${L_LOCAL_PORT_2} is not reachable`);

    await resetTargetLog();
    const nonce1 = `two-a-${crypto.randomUUID()}`;
    const nonce2 = `two-b-${crypto.randomUUID()}`;
    // Sequential on purpose: each probe hops out to NATIVE_APP for
    // `mobile: shell` and back, and Appium serialises its commands anyway — two
    // parallel calls would only race on the context switch. The property under
    // test is that the two LISTENERS coexist and carry independent traffic, not
    // that the two probes overlap.
    const p1 = await probeLocalPort(L_LOCAL_PORT, nonce1);
    const p2 = await probeLocalPort(L_LOCAL_PORT_2, nonce2);
    console.log(
      `[tier2] forward1 stdout=${JSON.stringify(p1.stdout.slice(0, 120))} ` +
        `forward2 stdout=${JSON.stringify(p2.stdout.slice(0, 120))}`,
    );
    // Each port must carry its OWN nonce: a bug that wired both listeners to one
    // stream would deliver one nonce twice (or cross them).
    assert.ok(p1.stdout.includes(nonce1), `forward ${L_LOCAL_PORT} lost its nonce: ${JSON.stringify(p1.stdout)}`);
    assert.ok(!p1.stdout.includes(nonce2), `forward ${L_LOCAL_PORT} saw the OTHER port's nonce`);
    assert.ok(p2.stdout.includes(nonce2), `forward ${L_LOCAL_PORT_2} lost its nonce: ${JSON.stringify(p2.stdout)}`);
    assert.ok(!p2.stdout.includes(nonce1), `forward ${L_LOCAL_PORT_2} saw the OTHER port's nonce`);

    const status = await targetStatus();
    const reqs = status.requests || [];
    assert.ok(reqs.some((r) => r.includes(nonce1)), `target never saw nonce1; log=${JSON.stringify(reqs)}`);
    assert.ok(reqs.some((r) => r.includes(nonce2)), `target never saw nonce2; log=${JSON.stringify(reqs)}`);
  });

  it('carries many concurrent connections through ONE forward', async () => {
    // Implementation plan T14 calls for 20-way concurrency; server-side
    // concurrency is unit-tested but never end to end. One h2 connection must
    // multiplex N simultaneous streams, and the target's request log must show
    // every nonce (the per-probe replies prove the response direction).
    await ensureLocalForward();

    const N = 16; // T14 asks for 20; 16 keeps the emulator's nc fan-out bounded
    // Zero-padded nonces so no nonce is a substring of another (see the probe
    // script): `-001` and `-010` cannot collide the way `-1` and `-10` would.
    const prefix = `conc-${crypto.randomBytes(4).toString('hex')}`;
    await resetTargetLog();

    const probe = await probeLocalPortConcurrent(L_LOCAL_PORT, prefix, N);
    console.log(
      `[tier2] concurrent probe: ${JSON.stringify(probe.stdout.trim())} ` +
        `stderr=${JSON.stringify(probe.stderr.slice(0, 200))} ms=${probe.ms}`,
    );
    assert.equal(probe.count, N, `the device script did not probe ${N} times: ${JSON.stringify(probe.stdout)}`);
    assert.equal(
      probe.ok,
      N,
      `only ${probe.ok}/${N} concurrent probes got their own echo back: ${JSON.stringify(probe.stdout)}`,
    );

    // Independent witness: the target recorded every nonce. The echo above
    // proves each response arrived; this proves each REQUEST arrived.
    const status = await targetStatus();
    const reqs = status.requests || [];
    const missing = [];
    for (let i = 1; i <= N; i += 1) {
      const n = String(i).padStart(3, '0');
      if (!reqs.some((r) => r.includes(`${prefix}-${n} `))) missing.push(n);
    }
    console.log(`[tier2] target saw ${reqs.length} requests, missing nonces: ${JSON.stringify(missing)}`);
    assert.deepEqual(
      missing,
      [],
      `the target never received concurrent nonces ${JSON.stringify(missing)}; log=${JSON.stringify(reqs)}`,
    );
  });

  it('round-trips a 64 KiB BINARY payload byte-identically', async () => {
    // The default probe is a ~120-byte UTF-8 HTTP GET; a byte-transparency bug
    // (e.g. any text decoding on the path) would pass it. This pushes 64 KiB of
    // arbitrary bytes through `raw` mode and compares ON THE DEVICE with `cmp`,
    // so a single flipped byte fails.
    await ensureLocalForward();
    await setTargetMode('raw');
    try {
      // Reset so the RAW record found below is unambiguously this test's.
      await resetTargetLog();
      // Non-UTF-8 on purpose: every byte value 0..255, including NUL and 0xFF.
      const payload = Buffer.alloc(64 * 1024);
      for (let i = 0; i < payload.length; i += 1) payload[i] = i % 256;

      const res = await probeLocalPortRaw(L_LOCAL_PORT, payload, 'binary64k');
      console.log(
        `[tier2] raw probe: stdout=${JSON.stringify(res.stdout.trim())} ` +
          `stderr=${JSON.stringify(res.stderr.slice(0, 200))} ms=${res.ms}`,
      );
      assert.ok(res.stdout.includes('OK'), `the 64 KiB payload did not round-trip byte-identically: ${JSON.stringify(res.stdout)} ${JSON.stringify(res.stderr)}`);

      // Independent witness: the target recorded the payload's length and md5.
      const expectedMd5 = crypto.createHash('md5').update(payload).digest('hex');
      const status = await targetStatus();
      const line = (status.requests || []).find((r) => r.startsWith('RAW:'));
      console.log(`[tier2] target raw record = ${JSON.stringify(line)} (expected md5 ${expectedMd5})`);
      assert.ok(line, `the target did not record a RAW request; log=${JSON.stringify(status.requests)}`);
      assert.equal(line, `RAW:${payload.length}:${expectedMd5}`, 'the target received different bytes than were sent');
    } finally {
      await resetTarget();
    }
  });

  it('re-adding a removed port works', async () => {
    // add -> remove -> add -> probe. A removal that left stale state (a closed
    // listener still in the map, a half-torn-down relay) would make the second
    // add a no-op, and the probe would then fail.
    await ensureLocalForward();

    await bridge('removeForwardedPort', L_LOCAL_PORT);
    const gone = await waitFor(
      async () => (await bridge('testPortReachable', L_LOCAL_PORT)) === false,
      20000,
      `127.0.0.1:${L_LOCAL_PORT} to stop accepting after removal`,
    );
    assert.equal(gone, true);

    // Re-add and probe. `ensureLocalForward` re-adds because the mapping is gone.
    await ensureLocalForward();
    await resetTargetLog();
    const nonce = `readd-${crypto.randomUUID()}`;
    const { stdout, stderr } = await probeLocalPort(L_LOCAL_PORT, nonce);
    console.log(`[tier2] re-add probe stdout = ${JSON.stringify(stdout.slice(0, 200))}`);
    assert.ok(
      stdout.includes('TARGET-ECHO:') && stdout.includes(nonce),
      `the re-added forward did not carry bytes: stdout=${JSON.stringify(stdout)} stderr=${JSON.stringify(stderr)}`,
    );
    const status = await targetStatus();
    assert.ok(
      (status.requests || []).some((r) => r.includes(nonce)),
      `the target never saw the re-add probe; log=${JSON.stringify(status.requests)}`,
    );
  });

  it('removeForwardedPort tears down a relay that is IN FLIGHT', async () => {
    // H2PortForwardTransport.removeLocal closes the listener AND every live
    // relay for the port (H2PortForwardTransport.java:180-187); otherwise a
    // connection on a port the user just deleted keeps relaying. The `delay`
    // target holds a relay open for 10s, so a teardown that only closed the
    // listener would leave the probe waiting out the full delay.
    //
    // The probe runs DETACHED on the device (see startDetachedLocalProbe):
    // Appium serialises commands, so a blocking probe would finish before the
    // removal could ever be issued, which is exactly the vacuity this avoids.
    await ensureLocalForward();
    await setTargetMode('delay');
    try {
      await resetTargetLog();

      const nonce = `inflight-${crypto.randomUUID()}`;
      const tag = `inflight-${Date.now()}`;
      const started = Date.now();
      await startDetachedLocalProbe(L_LOCAL_PORT, nonce, tag);

      // Wait until the target has RECORDED the request: at that point the relay
      // is genuinely open and the target is sitting in its delay.
      const inFlight = await waitFor(async () => {
        const status = await targetStatus();
        return (status.requests || []).some((r) => r.includes(nonce));
      }, 20000, 'the detached probe to reach the target');
      assert.equal(inFlight, true);

      // Remove the port while the relay is live. Measure the teardown from THIS
      // moment: the launch-to-target window above is dominated by test polling
      // and must not count against the teardown's budget.
      const removedAt = Date.now();
      await bridge('removeForwardedPort', L_LOCAL_PORT);

      const probe = await awaitDetachedLocalProbe(tag, started);
      const teardownMs = probe.finishedAt - removedAt;
      console.log(
        `[tier2] in-flight probe after removal: total=${probe.ms}ms teardown=${teardownMs}ms ` +
          `stdout=${JSON.stringify(probe.stdout.slice(0, 120))}`,
      );
      // The teardown must have ended the probe promptly — well before the 10s
      // delay the target would otherwise have waited out.
      assert.ok(
        teardownMs < 8000,
        `the in-flight relay was not torn down by removeForwardedPort: the probe ran ${teardownMs}ms ` +
          `after the removal (the target's delay is 10s, so this means the relay outlived the removal)`,
      );

      // The listener must be gone too.
      const gone = await waitFor(
        async () => (await bridge('testPortReachable', L_LOCAL_PORT)) === false,
        20000,
        `127.0.0.1:${L_LOCAL_PORT} to stop accepting after removal`,
      );
      assert.equal(gone, true);
    } finally {
      await resetTarget();
      await ensureLocalForward();
    }
  });

  it('a stalled target does not hang the stream (bounded drain grace)', async () => {
    // The design's central compromise (design §4.2.1): stdlib h2 cannot
    // half-close a response in-stream, so when the target's response direction
    // ends while the client's request body is still open, the handler stays
    // alive for a BOUNDED grace (relayDrainGrace, default 5s,
    // internal/tunnel/relay.go) and then returns. Without the bound the handler
    // parks forever — the deadlock T3 found.
    //
    // The probe deliberately KEEPS ITS REQUEST BODY OPEN (a keepalive every
    // second) — the ordinary probe half-closes and skips the grace entirely. The
    // target is in `stall` mode, so its response direction ends immediately.
    // The observable is the probe's wall clock: it must return, bounded by the
    // grace (~5s) and well under the 20s nc timeout that a hang would hit.
    await ensureLocalForward();
    await setTargetMode('stall');
    try {
      const probe = await probeLocalPortStalled(L_LOCAL_PORT, `stall-${crypto.randomUUID()}`);
      console.log(
        `[tier2] stalled-target probe: ms=${probe.ms} stdout=${JSON.stringify(probe.stdout.slice(0, 120))} ` +
          `stderr=${JSON.stringify(probe.stderr.slice(0, 120))}`,
      );
      // Bounded: the grace is 5s, plus the tunnel round trip. 15s is a generous
      // ceiling that still fails loudly if the handler never returns (the nc
      // timeout is 20s, so a hang would show up as ~20s, not ~5s).
      assert.ok(
        probe.ms < 15000,
        `the stalled target hung the stream for ${probe.ms}ms; the relay's drain grace must bound it`,
      );
      // ...and it must NOT have returned immediately: an immediate return is the
      // T3 bug (the client's in-flight bytes are dropped) that the grace exists
      // to avoid. Measured against the real server this path is ~5s; the 2s
      // floor distinguishes "waited the grace" from "gave up at once".
      assert.ok(
        probe.ms > 2000,
        `the stalled target ended the stream after only ${probe.ms}ms; the bounded grace ` +
          `(~5s) should have drained the client's in-flight bytes first`,
      );
      // The response direction ended, so nothing came back. Asserting this makes
      // the test about the relay, not about a probe that merely failed early.
      assert.ok(
        !probe.stdout.includes('TARGET-'),
        `a stalled target returned data: ${JSON.stringify(probe.stdout)}`,
      );
      // The listener must survive a single stalled connection.
      assert.equal(
        await bridge('testPortReachable', L_LOCAL_PORT),
        true,
        'the local listener was torn down by one stalled connection',
      );
    } finally {
      await resetTarget();
    }
  });

  it('delivers data the target sends AFTER the client half-closes', async () => {
    // A true half-close: the client ends its write side, the target sees EOF,
    // and ONLY THEN sends its answer. `eof` mode replies exclusively on EOF, so
    // this cannot pass if the client's FIN never traversed the tunnel — the
    // default `http` target replies on headers-complete and would mask that.
    await ensureLocalForward();
    await setTargetMode('eof');
    try {
      await resetTargetLog();
      const nonce = `halfclose-${crypto.randomUUID()}`;
      const probe = await probeLocalPortHalfClose(L_LOCAL_PORT, nonce);
      console.log(
        `[tier2] half-close probe: ms=${probe.ms} stdout=${JSON.stringify(probe.stdout.slice(0, 200))} ` +
          `stderr=${JSON.stringify(probe.stderr.slice(0, 160))}`,
      );
      assert.ok(
        probe.stdout.includes('TARGET-EOF:') && probe.stdout.includes(nonce),
        `the target's post-EOF reply did not come back (the half-close may not have ` +
          `traversed the tunnel): stdout=${JSON.stringify(probe.stdout)} stderr=${JSON.stringify(probe.stderr)}`,
      );
      // Independent witness: the target received the request before it answered.
      const status = await targetStatus();
      assert.ok(
        (status.requests || []).some((r) => r.includes(nonce)),
        `the target never received the half-close probe; log=${JSON.stringify(status.requests)}`,
      );
    } finally {
      await resetTarget();
    }
  });

  it('drops the connection (no payload) when the target is down, and keeps the listener', async () => {
    await ensureLocalForward();
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

  it('removeForwardedPort tears the local listener down (and leaves a sibling forward alive)', async () => {
    // The removal negative is paired with a POSITIVE control: after removing
    // ONE mapping, a DIFFERENT live mapping must still round-trip. Otherwise
    // "the port stopped accepting" is equally satisfied by the whole session
    // having died (auth expiry, server restart, a crashed transport), which is
    // not what removeForwardedPort is supposed to do.
    await ensureLocalForward(L_LOCAL_PORT);
    await ensureLocalForward(L_LOCAL_PORT_2);

    await bridge('removeForwardedPort', L_LOCAL_PORT);

    const gone = await waitFor(async () => {
      const reachable = await bridge('testPortReachable', L_LOCAL_PORT);
      return reachable === false;
    }, 20000, `127.0.0.1:${L_LOCAL_PORT} to stop accepting connections`);

    assert.equal(gone, true);
    console.log(`[tier2] after removeForwardedPort: testPortReachable(${L_LOCAL_PORT}) = false`);

    // Positive control: the sibling forward must be untouched and still carry
    // bytes, proving the removal was scoped and the session is healthy.
    await resetTargetLog();
    const nonce = `sibling-${crypto.randomUUID()}`;
    const probe = await probeLocalPort(L_LOCAL_PORT_2, nonce);
    console.log(`[tier2] sibling forward ${L_LOCAL_PORT_2} probe = ${JSON.stringify(probe.stdout.slice(0, 160))}`);
    assert.ok(
      probe.stdout.includes('TARGET-ECHO:') && probe.stdout.includes(nonce),
      `removing ${L_LOCAL_PORT} also broke the sibling forward ${L_LOCAL_PORT_2}: ` +
        `stdout=${JSON.stringify(probe.stdout)} stderr=${JSON.stringify(probe.stderr)}`,
    );
    assert.equal(
      await bridge('testPortReachable', L_LOCAL_PORT_2),
      true,
      `the sibling listener ${L_LOCAL_PORT_2} was torn down by removing ${L_LOCAL_PORT}`,
    );
  });
});
