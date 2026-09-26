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
 * Both directions are asserted independently:
 *   server -> device: the reply body carries the device target's marker.
 *   device <- server: the device target recorded the exact request bytes,
 *                     including a random nonce the server-side client generated.
 */
import assert from 'node:assert/strict';
import crypto from 'node:crypto';
import {
  R_SERVER_PORT,
  R_DEVICE_TARGET_PORT,
  installReadableSessionCookie,
  enterWebView,
  bridge,
  getActiveTunnelTransport,
  getTunnelError,
  startReverseDeviceTarget,
  stopReverseDeviceTarget,
  readDeviceFile,
  reverseProbe,
  waitFor,
  waitForActiveWire,
} from './helpers/tunnel.mjs';

const DEVICE_TMP = '/data/local/tmp';

describe('Tier 2 — h2 tunnel -R end-to-end', () => {
  const marker = `rev-${crypto.randomBytes(4).toString('hex')}`;

  before(async () => {
    await installReadableSessionCookie();
    await startReverseDeviceTarget(R_DEVICE_TARGET_PORT, marker);
  });

  after(async () => {
    try {
      await enterWebView();
      await bridge('removeReverseForwardedPort', R_SERVER_PORT);
    } catch {
      // Best-effort teardown.
    }
    await stopReverseDeviceTarget();
  });

  it('binds a server-side listener over h2 and lists the mapping', async () => {
    await enterWebView();

    // `addReverseForwardedPort` drives ensureConnection() itself, so no separate
    // `-L` forward is needed to bring the h2 session up.
    await bridge('setTunnelTransport', 'h2');
    await bridge('addReverseForwardedPort', R_SERVER_PORT, R_DEVICE_TARGET_PORT, '127.0.0.1');

    // The h2 session must be live: a reverse mapping is only servable over a
    // connected tunnel.
    const wire = await waitForActiveWire(60000);
    console.log(`[tier2][-R] getActiveTunnelTransport() = "${wire}"`);

    // The mapping appears in getForwardedPorts() only once the server accepted
    // the bind. That bind rides the authenticated control stream, so it is the
    // end-to-end proof that the control plane works.
    const listed = await waitFor(async () => {
      const raw = await bridge('getForwardedPorts');
      let parsed = [];
      try {
        parsed = JSON.parse(raw);
      } catch {
        return null;
      }
      return parsed.find((p) => p.port === R_SERVER_PORT && p.direction === 'reverse') || null;
    }, 30000, `the reverse mapping for ${R_SERVER_PORT} to appear in getForwardedPorts()`);

    console.log(`[tier2][-R] getForwardedPorts() entry = ${JSON.stringify(listed)}`);

    const error = await getTunnelError();
    if (error) console.log(`[tier2][-R] getTunnelError() = "${error}"`);
  });

  it('carries a connection accepted on the SERVER to the DEVICE target', async () => {
    const nonce = `rev-${crypto.randomUUID()}`;

    // The server-side client dials 127.0.0.1:<R_SERVER_PORT> inside the server's
    // own network namespace. That listener exists only because the device asked
    // the server to bind it, so a reply from the device target is the whole `-R`
    // path.
    const probe = await reverseProbe(R_SERVER_PORT, nonce);
    console.log(`[tier2][-R] server-side probe -> ${JSON.stringify(probe).slice(0, 400)}`);

    // server -> device: the reply came back from the DEVICE target, so it
    // carries that target's marker. (The device target replies with a fixed
    // body rather than echoing, so the nonce is proven on the device side
    // below, not here.)
    assert.ok(
      probe.reached && probe.body.includes(`DEVICE-TARGET: ${marker}`),
      `the reverse port did not carry the connection to the device target: ${JSON.stringify(probe)}`,
    );

    // device <- server: the device target recorded the request bytes the
    // server-side client sent, nonce included. Independent witness — the marker
    // above proves the reply direction, this proves the request direction.
    const captured = await waitFor(async () => {
      const text = await readDeviceFile(`${DEVICE_TMP}/rt/req`);
      return text.includes(nonce) ? text : null;
    }, 20000, `the device target to record the server-side request (nonce ${nonce})`);
    console.log(`[tier2][-R] device target captured ${captured.length} bytes incl. the nonce`);
  });

  it('rejects a reserved server port (20000)', async () => {
    await enterWebView();
    // 20000 is the server's own HTTP port — binding it in reverse would let a
    // client take down the very server the tunnel rides on. The server's guard
    // (internal/tunnel/guard.go ReverseBindDenied) denies it, and the client
    // must not end up advertising a mapping that cannot work.
    await bridge('addReverseForwardedPort', 20000, R_DEVICE_TARGET_PORT, '127.0.0.1');

    await waitFor(async () => {
      const parsed = JSON.parse(await bridge('getForwardedPorts'));
      return parsed.find((p) => p.port === 20000) ? null : true;
    }, 15000, 'no reverse mapping to be created for the reserved port 20000');

    const parsed = JSON.parse(await bridge('getForwardedPorts'));
    assert.equal(
      parsed.find((p) => p.port === 20000),
      undefined,
      `a reverse mapping for the reserved port 20000 was accepted: ${JSON.stringify(parsed)}`,
    );
    console.log('[tier2][-R] reserved port 20000 is not mapped');
  });

  it('removeReverseForwardedPort releases the server-side listener', async () => {
    await enterWebView();
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
  });
});
