/**
 * Tier 2 / Stage 2c — `-R` (reverse) end-to-end over the h2 stream tunnel.
 *
 * `-R` is the harder direction because HTTP/2 forbids server-initiated streams
 * (RFC 9113 §8.4). The client therefore opens a long-lived NDJSON **control
 * stream** (`POST /api/tunnel/control`); the server binds `serverPort` on its own
 * loopback, parks each accepted connection, and announces `incoming` with a
 * single-use claim token; the client dials its target and redeems the token with
 * `POST /api/tunnel/stream?claim=<token>`.
 *
 * The topology under test:
 *
 *   [server namespace]                        [device]
 *     server binds 127.0.0.1:17080  --h2-->   app dials 127.0.0.1:18090
 *
 * The server-side client is the Tier 2 target container, which shares the
 * server's network namespace: connecting to `127.0.0.1:17080` from there is a
 * genuine "a program on the server reached the reverse port" event. The runner
 * container cannot do this — from the compose bridge the emulator is a different
 * address, not loopback.
 *
 * ## Both directions, independently witnessed
 *
 * The device target answers with `DEVICE-TARGET: <marker>` and appends the
 * request bytes to a file on the device. So:
 *   - the marker in the reply proves server -> device (the reply came back from
 *     the device through the tunnel);
 *   - the nonce in the device's recorded request proves device <- server (the
 *     request bytes traversed).
 *
 * Each nonce is unique, so a test can assert its own nonce is present without
 * resetting the file.
 *
 * ## Test isolation
 *
 * Each test that needs a bound reverse port creates it (`ensureReverseForward`)
 * rather than relying on a previous test. Each negative test is paired with a
 * POSITIVE control (a subsequent valid operation), so "it was rejected/removed"
 * cannot be satisfied by the whole tunnel having died.
 */
import assert from 'node:assert/strict';
import crypto from 'node:crypto';
import {
  R_SERVER_PORT,
  R_SERVER_PORT_2,
  R_SERVER_PORT_3,
  R_DEVICE_TARGET_PORT,
  R_DEVICE_STALL_PORT,
  REVERSE_REQUEST_LOG,
  REVERSE_STALL_LOG,
  installReadableSessionCookie,
  enterWebView,
  bridge,
  getTunnelError,
  startReverseDeviceTarget,
  stopReverseDeviceTarget,
  startReverseStallTarget,
  stopReverseStallTarget,
  readDeviceFile,
  reverseProbe,
  waitFor,
  waitForActiveWire,
} from './helpers/tunnel.mjs';

/** The reverse ports this spec owns, so teardown is complete. */
const OWNED_REVERSE_PORTS = [R_SERVER_PORT, R_SERVER_PORT_2, R_SERVER_PORT_3];

/** The bound reverse mapping for `serverPort`, or undefined. */
async function findReverse(serverPort) {
  try {
    const parsed = JSON.parse(await bridge('getForwardedPorts'));
    return parsed.find((p) => p.port === serverPort && p.direction === 'reverse');
  } catch {
    return undefined;
  }
}

/**
 * Bring up (or reuse) the reverse mapping for `serverPort` and wait until it is
 * listed. Idempotent, so every test can create what it needs.
 */
async function ensureReverseForward(serverPort = R_SERVER_PORT) {
  await enterWebView();
  await bridge('setTunnelTransport', 'h2');
  if (!(await findReverse(serverPort))) {
    await bridge('addReverseForwardedPort', serverPort, R_DEVICE_TARGET_PORT, '127.0.0.1');
  }
  const wire = await waitForActiveWire(60000);
  await waitFor(async () => (await findReverse(serverPort)) || null, 30000,
    `the reverse mapping for ${serverPort} to appear in getForwardedPorts()`);
  return wire;
}

/**
 * Add a reverse forward for each port in `reservedPorts`, then assert — only
 * once those adds have provably COMPLETED — that none of them ended up mapped.
 *
 * Why the obvious `waitFor(() => findReverse(p) ? null : true)` is not enough:
 * that predicate is truthy on the FIRST poll, so it returns immediately and the
 * assert that follows is an instantaneous sample despite the 15000ms budget.
 * Meanwhile the add is asynchronous — the bridge only enqueues an intent; the
 * work runs on `BackgroundService`'s single-thread `networkExecutor`
 * (BackgroundService.java:202) — and `addReversePortForward` inserts the mapping
 * OPTIMISTICALLY (`reversePorts.put`, :2122) BEFORE it calls `addReverse()`,
 * which is where the server rejects a reserved port, and removes the entry only
 * in the catch (:2151). For 20000/20001 (in range, so they pass the client guard
 * at :2111) the mapping therefore transiently appears, and a slow network thread
 * lets the optimistic put land between the sample and the assert: a spurious
 * failure.
 *
 * The completion barrier is `barrierPort`: a valid unbind + add issued AFTER the
 * rejected ones. Both ride the SAME single-thread executor
 * (`networkExecutor`, BackgroundService.java:202) in FIFO order, so once the
 * barrier's mapping is (re)listed every rejected add has fully returned —
 * including the catch that removed the optimistic entry. Absence is asserted
 * only after that, so the check cannot pass merely by running before the
 * optimistic put landed, and it observes the settled state, not a transient one.
 *
 * The barrier is deliberately two-phase (unbind, wait until ABSENT, then add,
 * wait until PRESENT). A bare "wait until present" would itself be the bug being
 * fixed: if a previous test left the barrier port mapped, the predicate would be
 * truthy on its first poll, before the remove/add pair had been processed. The
 * "wait until absent" phase can be vacuous only when the port is already absent,
 * which is exactly the state the subsequent add needs; the add is what carries
 * the ordering guarantee.
 *
 * (A `clawbench-port-forward-result` event would be the most direct completion
 * signal, but it is not observable here: `notifyPortForwardResult` dispatches it
 * only through `BackgroundService.webViewRef`, which is set in
 * `MainActivity.setupWebView` (:696) — and `updateWebViewRef` no-ops while the
 * service instance is null (:543-547). In this cold-start fixture the service is
 * created only by the first port op, after `setupWebView`, so the ref stays null
 * and the event never reaches the page.)
 *
 * Returns the winning wire, so the caller can reuse this as its positive
 * control.
 */
async function assertReverseAddsRejected(reservedPorts, barrierPort) {
  await enterWebView();
  // The barrier's valid bind must ride the h2 transport; make the helper
  // self-contained rather than relying on a previous test having set it.
  await bridge('setTunnelTransport', 'h2');

  // Keep a baseline reverse mapping alive for the whole helper. Phase 1 below
  // unbinds the barrier port, and `removeReversePortForward` calls `stopSelf()`
  // when BOTH port maps are empty (BackgroundService.java:2181-2184) — so the
  // unbind must never be the last mapping. Hence `barrierPort` must differ from
  // the baseline: otherwise the unbind would tear the service (and tunnel) down
  // and the barrier add could never appear.
  assert.notEqual(
    barrierPort,
    R_SERVER_PORT,
    'the barrier port must differ from the baseline port (R_SERVER_PORT)',
  );
  await ensureReverseForward(R_SERVER_PORT);

  for (const port of reservedPorts) {
    await bridge('addReverseForwardedPort', port, R_DEVICE_TARGET_PORT, '127.0.0.1');
  }

  // Completion barrier (also the spec's positive control: a valid bind must
  // still work). Phase 1: drop any stale mapping, so phase 2 is a real
  // unmapped -> mapped transition rather than an immediate truthy read. It is a
  // no-op when the barrier port is already absent (the common case).
  await bridge('removeReverseForwardedPort', barrierPort);
  await waitFor(async () => ((await findReverse(barrierPort)) ? null : true), 30000,
    `the barrier reverse mapping for ${barrierPort} to be absent before re-adding`);

  // Phase 2: a fresh, valid add enqueued behind every rejected add.
  await bridge('addReverseForwardedPort', barrierPort, R_DEVICE_TARGET_PORT, '127.0.0.1');
  const wire = await waitForActiveWire(60000);
  await waitFor(async () => (await findReverse(barrierPort)) || null, 30000,
    `the barrier reverse mapping for ${barrierPort} to appear in getForwardedPorts()`);

  for (const port of reservedPorts) {
    assert.equal(
      await findReverse(port),
      undefined,
      `a reverse mapping for the reserved port ${port} was accepted`,
    );
    console.log(`[tier2][-R] reserved port ${port} is not mapped`);
  }
  return wire;
}

describe('Tier 2 — h2 tunnel -R end-to-end', () => {
  const marker = `rev-${crypto.randomBytes(4).toString('hex')}`;

  /** Assert the device target recorded `nonce` (the device <- server direction). */
  async function assertRecorded(nonce, label) {
    const text = await waitFor(async () => {
      const t = await readDeviceFile(REVERSE_REQUEST_LOG);
      return t.includes(nonce) ? t : null;
    }, 20000, `${label}: the device target to record the request (nonce ${nonce})`);
    return text;
  }

  /** Assert one server-side probe carried a fresh nonce to the device AND back. */
  async function assertDeviceAnswer(probe, nonce, label) {
    assert.ok(
      probe.reached && probe.body.includes(`DEVICE-TARGET: ${marker}`),
      `${label}: the reverse port did not carry the connection to the device target: ` +
        `${JSON.stringify(probe).slice(0, 400)}`,
    );
    // The reply direction alone could be canned; the device's own record is the
    // independent witness for the request direction.
    const captured = await assertRecorded(nonce, label);
    console.log(`[tier2][-R] ${label}: device target captured ${captured.length} bytes incl. ${nonce}`);
  }

  before(async () => {
    await installReadableSessionCookie();
    await startReverseDeviceTarget(R_DEVICE_TARGET_PORT, marker);
  });

  after(async () => {
    try {
      await enterWebView();
      for (const port of OWNED_REVERSE_PORTS) {
        await bridge('removeReverseForwardedPort', port);
      }
    } catch {
      // Best-effort teardown.
    }
    await stopReverseDeviceTarget();
  });

  it('binds a server-side listener over h2 and lists the mapping', async () => {
    await enterWebView();

    // `addReverseForwardedPort` drives ensureConnection() itself, so no separate
    // `-L` forward is needed to bring the h2 session up.
    const wire = await ensureReverseForward(R_SERVER_PORT);

    // The h2 session must be live: a reverse mapping is only servable over a
    // connected tunnel.
    console.log(`[tier2][-R] getActiveTunnelTransport() = "${wire}"`);
    assert.ok(['tls', 'h2c'].includes(wire), `unexpected active transport "${wire}"`);

    // The mapping appears in getForwardedPorts() only once the server accepted
    // the bind. That bind rides the authenticated control stream, so it is the
    // end-to-end proof that the control plane works.
    const listed = await findReverse(R_SERVER_PORT);
    console.log(`[tier2][-R] getForwardedPorts() entry = ${JSON.stringify(listed)}`);

    const error = await getTunnelError();
    console.log(`[tier2][-R] getTunnelError() = "${error}"`);
    // The reverse bind rides the authenticated control stream and succeeded
    // (the mapping is listed above), so the session must report no error. This
    // runs before the reserved-port test below, which is the only step in this
    // spec that legitimately sets a session error.
    assert.equal(error, '', `a live h2 session with a bound reverse listener reported an error: "${error}"`);
  });

  it('carries a connection accepted on the SERVER to the DEVICE target', async () => {
    await ensureReverseForward(R_SERVER_PORT);
    const nonce = `rev-${crypto.randomUUID()}`;

    // The server-side client dials 127.0.0.1:<R_SERVER_PORT> inside the server's
    // own network namespace. That listener exists only because the device asked
    // the server to bind it, so a reply from the device target is the whole `-R`
    // path.
    const probe = await reverseProbe(R_SERVER_PORT, nonce);
    console.log(`[tier2][-R] server-side probe -> ${JSON.stringify(probe).slice(0, 400)}`);
    await assertDeviceAnswer(probe, nonce, 'first connection');
  });

  it('serves a SECOND connection to the same reverse port', async () => {
    // The first connection spends a single-use claim token. A second connection
    // must mint a fresh token and be served — a regression that cached or
    // reused the first token (or tore the control stream down after one
    // connection) would fail here while the single-connection test passed.
    await ensureReverseForward(R_SERVER_PORT);

    const nonce1 = `rev2-a-${crypto.randomUUID()}`;
    const nonce2 = `rev2-b-${crypto.randomUUID()}`;
    const p1 = await reverseProbe(R_SERVER_PORT, nonce1);
    const p2 = await reverseProbe(R_SERVER_PORT, nonce2);
    console.log(
      `[tier2][-R] probe1 -> ${JSON.stringify(p1).slice(0, 200)}\n` +
        `[tier2][-R] probe2 -> ${JSON.stringify(p2).slice(0, 200)}`,
    );
    await assertDeviceAnswer(p1, nonce1, 'first connection');
    await assertDeviceAnswer(p2, nonce2, 'second connection');
  });

  it('keeps the reverse mapping alive (and still serves) when the device target is down', async () => {
    // What this spec proves: a failed connection is PER-CONNECTION. The reverse
    // mapping must survive it (it must not be unbound), and the SAME bound port
    // must still serve once the target is back.
    //
    // What it does NOT prove — despite an earlier name that claimed it did — is
    // that the single-use claim token went unspent. The server mints a FRESH
    // token per accepted connection (`park()` per accept,
    // internal/handler/tunnel_control.go:237 -> internal/tunnel/claim.go:75), so
    // a second connection gets its own token no matter what happened to the
    // first. A "claims before dialing" bug (H2PortForwardTransport.claimIncoming)
    // would therefore NOT be caught here: the second probe would still succeed.
    //
    // The token-consumption invariant is pinned where it is observable, by the
    // Android unit test
    // `incoming_targetUnreachable_doesNotSpendTheToken`
    // (android/app/src/test/java/com/clawbench/app/tunnel/H2PortForwardTransportTest.java:485-499),
    // which asserts `claimedTokens.size() == 0` after a failed dial.
    await ensureReverseForward(R_SERVER_PORT);

    // Stop the device target so the dial fails.
    await stopReverseDeviceTarget();
    try {
      const nonceDown = `revdown-${crypto.randomUUID()}`;
      const down = await reverseProbe(R_SERVER_PORT, nonceDown, 6000);
      console.log(`[tier2][-R] probe with the device target down -> ${JSON.stringify(down).slice(0, 300)}`);
      // No marker: the device target was not there to answer.
      assert.ok(
        !down.reached || !down.body.includes(`DEVICE-TARGET: ${marker}`),
        `a down device target still produced a device answer: ${JSON.stringify(down)}`,
      );

      // The mapping must still be listed: the failed connection is per-connection
      // and must not unbind the port.
      assert.ok(
        await findReverse(R_SERVER_PORT),
        `the reverse mapping for ${R_SERVER_PORT} disappeared after a down-target connection`,
      );
    } finally {
      // Restart the SAME target (same marker) and probe again.
      await startReverseDeviceTarget(R_DEVICE_TARGET_PORT, marker);
    }

    const nonceUp = `revup-${crypto.randomUUID()}`;
    const up = await reverseProbe(R_SERVER_PORT, nonceUp, 10000);
    console.log(`[tier2][-R] probe after target restart -> ${JSON.stringify(up).slice(0, 300)}`);
    await assertDeviceAnswer(up, nonceUp, 'after the target restarted');
  });

  it('rejects a reserved server port (20000) AND a second reserved port (20001), then still serves a valid bind', async () => {
    // 20000 is the server's own HTTP port; 20001 is the h2 guard's hardcoded
    // `SSHPort = mainPort + 1` (internal/handler/tunnel_control.go). Binding
    // either in reverse would let a client take down the transport the tunnel
    // itself rides on. The server's guard (internal/tunnel/guard.go
    // ReverseBindDenied) denies both, and the client must not end up advertising
    // a mapping that cannot work.
    await enterWebView();

    // The barrier add doubles as the POSITIVE CONTROL: a valid bind on a fresh
    // port must still work and carry a connection — otherwise "the reserved port
    // is not mapped" would be equally true if the control stream had died. The
    // mapping survival is asserted inside the helper (it must be listed); here
    // we additionally carry real bytes through it.
    const wire = await assertReverseAddsRejected([20000, 20001], R_SERVER_PORT_2);
    assert.ok(['tls', 'h2c'].includes(wire), `the tunnel died after the reserved-port rejections (wire="${wire}")`);
    const nonce = `after-reserved-${crypto.randomUUID()}`;
    const probe = await reverseProbe(R_SERVER_PORT_2, nonce, 10000);
    console.log(`[tier2][-R] valid bind after rejections -> ${JSON.stringify(probe).slice(0, 240)}`);
    await assertDeviceAnswer(probe, nonce, 'a valid bind after the reserved-port rejections');

    // Clean up the control port so later tests are not affected.
    await bridge('removeReverseForwardedPort', R_SERVER_PORT_2);
  });

  it('rejects an out-of-range server port (0) and still serves a valid bind', async () => {
    // A port outside 1..65535 is rejected before any bind is sent. Port 0 is
    // dropped even earlier than the reserved ports: the service's intent
    // dispatch (`BackgroundService.java:965`, `if (serverPort > 0)`) rejects it
    // before `addReversePortForward` — and so before the optimistic put — ever
    // runs, making absence already definitive. It is still routed through the
    // same barrier helper so the positive control (a valid bind afterwards) is
    // identical in shape to the reserved-port spec.
    await enterWebView();
    // The barrier port must differ from the baseline (R_SERVER_PORT); reuse the
    // sibling port the previous spec cleaned up.
    const wire = await assertReverseAddsRejected([0], R_SERVER_PORT_2);
    assert.ok(['tls', 'h2c'].includes(wire), `the tunnel died after the out-of-range rejection (wire="${wire}")`);
    const nonce = `after-range-${crypto.randomUUID()}`;
    const probe = await reverseProbe(R_SERVER_PORT_2, nonce, 10000);
    await assertDeviceAnswer(probe, nonce, 'after the out-of-range rejection');
    await bridge('removeReverseForwardedPort', R_SERVER_PORT_2);
  });

  it('removeReverseForwardedPort tears down a reverse relay that is IN FLIGHT', async () => {
    // H2PortForwardTransport.removeReverse closes every live relay for the port
    // (H2PortForwardTransport.java:263-267); otherwise a connection the server
    // parked keeps relaying on a port the user just deleted.
    //
    // The device target STALLS (accepts, records, never replies), so the relay
    // stays in flight until something closes it. The server-side probe is issued
    // DETACHED from the runner's perspective: it is a fetch that will not settle
    // while the target stalls, so it is started without awaiting and the removal
    // happens while it is open.
    await enterWebView();
    await bridge('setTunnelTransport', 'h2');
    await startReverseStallTarget(R_DEVICE_STALL_PORT);
    try {
      await bridge('addReverseForwardedPort', R_SERVER_PORT_3, R_DEVICE_STALL_PORT, '127.0.0.1');
      await waitFor(async () => (await findReverse(R_SERVER_PORT_3)) || null, 30000,
        `the reverse mapping for ${R_SERVER_PORT_3} to appear`);

      const nonce = `rinflight-${crypto.randomUUID()}`;
      // A generous client-side bound; the teardown should end it far sooner.
      // `.catch(() => {})` prevents an unhandled rejection if the promise is
      // abandoned (it is always awaited below, but a failed waitFor before that
      // would leave it dangling).
      const probePromise = reverseProbe(R_SERVER_PORT_3, nonce, 20000);
      probePromise.catch(() => {});

      // Wait until the DEVICE recorded the request: the relay is genuinely open.
      await waitFor(async () => {
        const text = await readDeviceFile(REVERSE_STALL_LOG);
        return text.includes(nonce) ? true : null;
      }, 20000, 'the stalling device target to record the in-flight request');

      const removedAt = Date.now();
      await bridge('removeReverseForwardedPort', R_SERVER_PORT_3);

      const probe = await probePromise;
      const teardownMs = Date.now() - removedAt;
      console.log(
        `[tier2][-R] in-flight probe after removal: reached=${probe.reached} ` +
          `err=${probe.error ?? ''} ms=${probe.ms} teardown=${teardownMs}ms`,
      );
      // The teardown must end the relay promptly rather than letting the probe
      // sit out its own 20s client bound.
      assert.ok(
        teardownMs < 12000,
        `the in-flight reverse relay was not torn down by removeReverseForwardedPort: ` +
          `the probe ran ${teardownMs}ms after the removal`,
      );
      assert.ok(
        !probe.reached,
        `a torn-down reverse relay still returned a device answer: ${JSON.stringify(probe).slice(0, 200)}`,
      );

      // The listener must be gone too.
      const released = await waitFor(async () => {
        const p = await reverseProbe(R_SERVER_PORT_3, 'after-remove', 3000);
        return p.reached ? null : true;
      }, 20000, `the server-side listener on ${R_SERVER_PORT_3} to be released`);
      assert.equal(released, true);

      // POSITIVE CONTROL: the main reverse port must still work, proving the
      // removal did not kill the control stream.
      const okNonce = `after-inflight-${crypto.randomUUID()}`;
      const ok = await reverseProbe(R_SERVER_PORT, okNonce, 10000);
      await assertDeviceAnswer(ok, okNonce, 'the main reverse port after the in-flight teardown');
    } finally {
      await bridge('removeReverseForwardedPort', R_SERVER_PORT_3).catch(() => {});
      await stopReverseStallTarget();
    }
  });

  it('removeReverseForwardedPort releases the server-side listener (and leaves a sibling reverse port alive)', async () => {
    // The removal negative is paired with a POSITIVE control: after removing ONE
    // reverse mapping, a DIFFERENT bound reverse port must still carry a
    // connection. Otherwise "the listener stopped accepting" is equally
    // satisfied by the whole control stream / session having died.
    await ensureReverseForward(R_SERVER_PORT);
    await ensureReverseForward(R_SERVER_PORT_2);

    await bridge('removeReverseForwardedPort', R_SERVER_PORT);

    // The server releases the listener when the unbind is processed; poll the
    // server-side probe until it can no longer connect.
    const released = await waitFor(async () => {
      const probe = await reverseProbe(R_SERVER_PORT, 'after-remove');
      if (probe.reached) return null;
      console.log(`[tier2][-R] probe after removal -> ${JSON.stringify(probe).slice(0, 200)}`);
      return true;
    }, 30000, `the server-side listener on ${R_SERVER_PORT} to be released`);
    assert.equal(released, true);

    // Positive control: the sibling reverse port must still be served, proving
    // the removal was scoped and the control stream is alive.
    const nonce = `sibling-${crypto.randomUUID()}`;
    const probe = await reverseProbe(R_SERVER_PORT_2, nonce, 10000);
    console.log(`[tier2][-R] sibling reverse ${R_SERVER_PORT_2} -> ${JSON.stringify(probe).slice(0, 240)}`);
    await assertDeviceAnswer(probe, nonce, 'the sibling reverse port after the removal');

    await bridge('removeReverseForwardedPort', R_SERVER_PORT_2);
  });
});
