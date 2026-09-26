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

  it('does NOT spend the claim token when the device target is down', async () => {
    // The single-use token invariant: claimIncoming dials the target BEFORE it
    // opens the claim stream (H2PortForwardTransport.java:321-331), so a down
    // target must leave the token unspent. This is the POSITIVE proof of that:
    // the first probe fails, the target is restarted, and a SECOND probe on the
    // SAME bound port must succeed. If the failed attempt had burned the token,
    // the mapping would be permanently dead until a rebind.
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

    for (const reserved of [20000, 20001]) {
      await bridge('addReverseForwardedPort', reserved, R_DEVICE_TARGET_PORT, '127.0.0.1');
      await waitFor(async () => ((await findReverse(reserved)) ? null : true), 15000,
        `no reverse mapping to be created for the reserved port ${reserved}`);
      assert.equal(
        await findReverse(reserved),
        undefined,
        `a reverse mapping for the reserved port ${reserved} was accepted`,
      );
      console.log(`[tier2][-R] reserved port ${reserved} is not mapped`);
    }

    // POSITIVE CONTROL: the rejections must not have killed the tunnel. A valid
    // bind on a fresh port must still work and carry a connection — otherwise
    // "the reserved port is not mapped" would be equally true if the control
    // stream had died.
    const wire = await ensureReverseForward(R_SERVER_PORT_2);
    assert.ok(['tls', 'h2c'].includes(wire), `the tunnel died after the reserved-port rejections (wire="${wire}")`);
    const nonce = `after-reserved-${crypto.randomUUID()}`;
    const probe = await reverseProbe(R_SERVER_PORT_2, nonce, 10000);
    console.log(`[tier2][-R] valid bind after rejections -> ${JSON.stringify(probe).slice(0, 240)}`);
    await assertDeviceAnswer(probe, nonce, 'a valid bind after the reserved-port rejections');

    // Clean up the control port so later tests are not affected.
    await bridge('removeReverseForwardedPort', R_SERVER_PORT_2);
  });

  it('rejects an out-of-range server port (0) and still serves a valid bind', async () => {
    // A port outside 1..65535 is rejected by the client before any bind is sent
    // (BackgroundService.addReversePortForward's own guard). The pairing with a
    // positive control is what makes this meaningful: it distinguishes
    // "rejected this port" from "the tunnel died".
    await enterWebView();
    await bridge('addReverseForwardedPort', 0, R_DEVICE_TARGET_PORT, '127.0.0.1');
    await waitFor(async () => ((await findReverse(0)) ? null : true), 15000,
      'no reverse mapping to be created for port 0');
    assert.equal(await findReverse(0), undefined, 'a reverse mapping for port 0 was accepted');
    console.log('[tier2][-R] out-of-range port 0 is not mapped');

    // Positive control: the tunnel still works.
    const wire = await ensureReverseForward(R_SERVER_PORT);
    assert.ok(['tls', 'h2c'].includes(wire), `the tunnel died after the out-of-range rejection (wire="${wire}")`);
    const nonce = `after-range-${crypto.randomUUID()}`;
    const probe = await reverseProbe(R_SERVER_PORT, nonce, 10000);
    await assertDeviceAnswer(probe, nonce, 'after the out-of-range rejection');
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
