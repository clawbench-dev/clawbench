#!/usr/bin/env node
/**
 * Tier 2 tunnel target + test-control server.
 *
 * This is NOT a mock of ClawBench — the Tier 2 suite runs against a real
 * `clawbench` server (see Dockerfile.server). This process provides the two
 * things the real server cannot:
 *
 *   1. A **target** for the `-L` forward to dial. `POST /api/tunnel/stream`
 *      dials `host:port` *from the server's network namespace*, so the target
 *      must live in that same namespace. It shares the emulator's netns
 *      (`network_mode: service:emulator`), which is also where the real server
 *      runs.
 *
 *   2. A **test-control** surface the WebdriverIO runner (a different
 *      container) can drive: start/stop the target, select its reply mode to
 *      assert the negative/half-close paths, and read back the exact bytes the
 *      target received through the tunnel.
 *
 * Two listeners:
 *
 *   TARGET_PORT (default 18080) — a raw `node:net` target whose behavior is
 *     selected by MODE (see below). Every mode records the exact request bytes
 *     it saw, which is how the suite proves bytes really traversed the tunnel
 *     client→server (the request arrived) and server→client (the response came
 *     back).
 *
 *   CONTROL_PORT (default 18081) — `node:http`, reached by the runner at
 *     `http://emulator:18081` (compose service name). Endpoints:
 *       POST /__target/stop    — stop accepting on TARGET_PORT
 *       POST /__target/start   — (re)start it
 *       GET  /__target/status  — { listening, mode, requests }
 *       POST /__target/mode?mode=<m> — select the reply mode (see below)
 *       GET  /__target/echo?nonce=<x> — out-of-band fetch, used to prove the
 *         target is genuinely reachable directly (a control for the tunnel
 *         assertion: a failed tunnel with a dead target proves nothing).
 *       POST /__reset          — clear the recorded request log
 *
 * Reply modes (`POST /__target/mode?mode=`):
 *
 *   http   (default) — behave like a real HTTP/1.1 server: reply as soon as the
 *                      request headers are complete (or on EOF, or after a 3s
 *                      idle), echoing what it received as `TARGET-ECHO:<req>`.
 *                      This is the shape every byte-transfer assertion uses.
 *   eof              — reply ONLY after the client half-closes (EOF). It exists
 *                      because the default `http` mode replies on
 *                      headers-complete, so it passes even if the half-close
 *                      were broken: a probe against `eof` cannot get an answer
 *                      unless the client's FIN really traversed the tunnel.
 *   raw              — read until EOF, then echo the bytes back VERBATIM (no
 *                      HTTP framing). Used for the byte-transparency test: a
 *                      binary payload must come back byte-identical, which an
 *                      HTTP-framed echo cannot prove.
 *   stall            — accept, then close the write side immediately without
 *                      replying. The server-side relay sees the target's
 *                      response direction end while the client's request body
 *                      is still open, which is exactly the case the bounded
 *                      `relayDrainGrace` exists for (design §4.2.1).
 *   delay            — accept, record the request, wait DELAY_MS, then reply.
 *                      Keeps a relay "in flight" for a teardown assertion that
 *                      is two-sided (prompt teardown vs the delay it would
 *                      otherwise have waited out).
 *
 * Dependency-free (`node:net`, `node:http`) so it needs no npm install.
 */
import net from 'node:net';
import http from 'node:http';
import crypto from 'node:crypto';

const TARGET_PORT = Number(process.env.TARGET_PORT || 18080);
const CONTROL_PORT = Number(process.env.CONTROL_PORT || 18081);
const TARGET_HOST = process.env.TARGET_HOST || '127.0.0.1';
const CONTROL_HOST = process.env.CONTROL_HOST || '0.0.0.0';
/** How long `delay` mode waits before replying. Long enough that a teardown
 *  that failed to close the relay is unmistakable (the probe would sit here). */
const DELAY_MS = Number(process.env.TARGET_DELAY_MS || 10000);

/** Valid reply modes. An unknown mode is rejected rather than silently defaulted. */
const MODES = new Set(['http', 'eof', 'raw', 'stall', 'delay']);

/** The active reply mode. */
let mode = 'http';

/**
 * Every request the target has received, newest last.
 *
 * Entries are capped (see recordRequest): the binary test pushes 64 KiB, and a
 * status response carrying a megabyte of payload would be useless to a human
 * reading a failure. `raw` mode records a digest instead of the payload.
 */
const received = [];

const RECORD_CAP = 8192;

/** Record one received request (string modes) and return the capped text. */
function recordRequest(text) {
  const capped = text.length > RECORD_CAP ? text.slice(0, RECORD_CAP) : text;
  received.push(capped);
  return capped;
}

/**
 * The default `http` reply: behave like a real HTTP/1.1 server — reply as soon
 * as the request headers are complete — with a deterministic body that ECHOES
 * what it received.
 *
 * Echoing is the point: the runner sends a random nonce through the tunnel and
 * asserts the same nonce comes back in the response. A canned reply could be
 * produced without the bytes ever arriving; an echo cannot.
 *
 * Replying on headers-complete (rather than waiting for the peer to half-close)
 * matters twice:
 *
 *   - It is what a real HTTP server does, so the probe is a realistic workload
 *     rather than a bespoke EOF protocol.
 *   - A client that keeps its write side open (the direct `/__target/echo`
 *     control probe does exactly that) would otherwise stall until the idle
 *     timeout. The earlier EOF-only shape made that control probe time out,
 *     which is a false negative on the target's liveness.
 *
 * EOF is still honoured as a trigger, because the `-L` probe half-closes after
 * its request and the tunnel propagates that EOF: whichever arrives first wins.
 */
function httpReply(socket, chunks) {
  const req = Buffer.concat(chunks);
  const text = recordRequest(req.toString('utf8'));
  const body = `TARGET-ECHO:${text}`;
  const head =
    'HTTP/1.1 200 OK\r\n' +
    'Content-Type: text/plain\r\n' +
    `Content-Length: ${Buffer.byteLength(body)}\r\n` +
    'Connection: close\r\n\r\n';
  socket.end(head + body);
}

function handleConnection(socket) {
  // Snapshot the mode: a connection's behavior must not change underneath it if
  // a spec flips the mode between probes while this one is still open.
  const connMode = mode;
  const chunks = [];
  socket.setNoDelay(true);

  /** Whether `received` already has this connection's request (record once). */
  let recorded = false;
  const recordOnce = () => {
    if (recorded) return;
    recorded = true;
    const req = Buffer.concat(chunks);
    if (connMode === 'raw') {
      // Digest, not payload: 64 KiB of random bytes is not useful inline, and
      // the byte-exactness witness for `raw` is the device-side `cmp`.
      received.push(`RAW:${req.length}:${crypto.createHash('md5').update(req).digest('hex')}`);
    } else {
      recordRequest(req.toString('utf8'));
    }
  };

  // `stall` closes without ever reading: the relay's target->client direction
  // ends at once while the client body stays open (the drain-grace case).
  if (connMode === 'stall') {
    recordOnce();
    socket.end();
    return;
  }

  const idle = setTimeout(() => {
    // Neither headers nor EOF arrived; answer with what we have rather than
    // hanging the test. Only the reply-shaped modes use this: `raw`/`eof` are
    // EOF-triggered by construction.
    if (connMode === 'http' || connMode === 'delay') reply();
  }, 3000);

  let done = false;
  function reply() {
    if (done) return;
    done = true;
    clearTimeout(idle);

    if (connMode === 'raw') {
      recordOnce();
      socket.end(Buffer.concat(chunks));
      return;
    }
    if (connMode === 'eof') {
      const req = Buffer.concat(chunks);
      const text = recordRequest(req.toString('utf8'));
      const body = `TARGET-EOF:${text}`;
      const head =
        'HTTP/1.1 200 OK\r\n' +
        'Content-Type: text/plain\r\n' +
        `Content-Length: ${Buffer.byteLength(body)}\r\n` +
        'Connection: close\r\n\r\n';
      socket.end(head + body);
      return;
    }
    if (connMode === 'delay') {
      recordOnce();
      // Hold the relay in flight, then answer. The request is deliberately
      // recorded BEFORE the wait so a spec can witness it arriving while the
      // connection is still open.
      setTimeout(() => {
        const text = Buffer.concat(chunks).toString('utf8');
        const body = `TARGET-DELAY:${text}`;
        const head =
          'HTTP/1.1 200 OK\r\n' +
          'Content-Type: text/plain\r\n' +
          `Content-Length: ${Buffer.byteLength(body)}\r\n` +
          'Connection: close\r\n\r\n';
        socket.end(head + body);
      }, DELAY_MS).unref();
      return;
    }
    httpReply(socket, chunks);
  }

  socket.on('data', (c) => {
    chunks.push(c);
    if (connMode === 'delay') {
      // Record as soon as the request is complete so the runner can witness it
      // arriving while the connection is still open, then answer (after
      // DELAY_MS) or on EOF below.
      if (!recorded && Buffer.concat(chunks).includes('\r\n\r\n')) recordOnce();
      if (!done) reply();
      return;
    }
    if (connMode === 'http' && Buffer.concat(chunks).includes('\r\n\r\n')) {
      // Headers complete? Reply now. The probe uses `Connection: close` and a
      // body-less GET, so end-of-headers is end-of-request for this target.
      reply();
    }
    // `eof`/`raw` deliberately ignore headers-complete: they answer only on EOF.
  });
  socket.on('end', () => reply());
  socket.on('error', () => {
    clearTimeout(idle);
    // A torn-down relay surfaces as ECONNRESET here; the request may still have
    // been recorded, which is what the teardown spec asserts.
    if (connMode === 'delay') recordOnce();
  });
  socket.on('close', () => {
    clearTimeout(idle);
    if (connMode === 'delay') recordOnce();
  });
}

let targetServer = null;
function startTarget() {
  return new Promise((resolve) => {
    if (targetServer) return resolve();
    targetServer = net.createServer(handleConnection);
    targetServer.on('error', (e) => console.error(`[target] listen error: ${e.message}`));
    targetServer.listen(TARGET_PORT, TARGET_HOST, () => {
      console.log(`[target] listening on ${TARGET_HOST}:${TARGET_PORT} (mode=${mode})`);
      resolve();
    });
  });
}

function stopTarget() {
  return new Promise((resolve) => {
    if (!targetServer) return resolve();
    const s = targetServer;
    targetServer = null;
    s.close(() => resolve());
    // A connection in flight would otherwise keep close() pending; the
    // negative-path test wants the listener gone immediately.
    setTimeout(resolve, 500).unref();
  });
}

// ---------------------------------------------------------------- control ----

function json(res, status, obj) {
  const buf = Buffer.from(JSON.stringify(obj));
  res.writeHead(status, { 'Content-Type': 'application/json', 'Content-Length': buf.length });
  res.end(buf);
}

const control = http.createServer((req, res) => {
  const url = new URL(req.url || '/', 'http://x');
  const path = url.pathname;

  if (path === '/__target/status') {
    return json(res, 200, { listening: !!targetServer, port: TARGET_PORT, mode, requests: received });
  }
  if (path === '/__reset' && req.method === 'POST') {
    received.length = 0;
    return json(res, 200, { ok: true });
  }
  if (path === '/__target/mode' && req.method === 'POST') {
    const next = url.searchParams.get('mode') || '';
    if (!MODES.has(next)) {
      return json(res, 400, { ok: false, error: `unknown mode ${JSON.stringify(next)}`, modes: [...MODES] });
    }
    mode = next;
    console.log(`[target] mode=${mode}`);
    return json(res, 200, { ok: true, mode });
  }
  if (path === '/__target/stop' && req.method === 'POST') {
    return stopTarget().then(() => json(res, 200, { ok: true, listening: !!targetServer }));
  }
  if (path === '/__target/start' && req.method === 'POST') {
    return startTarget().then(() => json(res, 200, { ok: true, listening: !!targetServer }));
  }
  if (path === '/__target/echo') {
    // Out-of-band probe: fetch the target directly from the server's own
    // namespace, bypassing the tunnel entirely. Proves the target is alive so a
    // failed tunnel assertion cannot be blamed on a dead target.
    //
    // Only meaningful in `http` mode (the only mode that answers a client that
    // keeps its write side open); the helper asserts the mode first.
    const nonce = url.searchParams.get('nonce') || '';
    const sock = net.connect(TARGET_PORT, TARGET_HOST);
    const chunks = [];
    sock.setTimeout(3000);
    sock.on('connect', () => sock.write(`GET /direct?nonce=${nonce} HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n`));
    sock.on('data', (c) => chunks.push(c));
    sock.on('end', () => {
      const body = Buffer.concat(chunks).toString('utf8');
      return json(res, 200, { ok: body.includes(nonce), body });
    });
    sock.on('error', (e) => json(res, 200, { ok: false, error: e.message }));
    sock.on('timeout', () => {
      sock.destroy();
      json(res, 200, { ok: false, error: 'timeout' });
    });
    return;
  }
  if (path === '/__reverse/probe') {
    // The server-side client for `-R`. The reverse listener is bound on the
    // SERVER's 127.0.0.1, and this container shares that network namespace, so
    // a connect from here is a genuine "a program on the server reached the
    // reverse port" event. The runner container cannot do this: from the
    // compose bridge the emulator is a different address, not loopback.
    //
    // The reply is whatever the DEVICE-side target sent. That target answers
    // with a fixed `DEVICE-TARGET: <marker>` body and appends the request bytes
    // to a file on the device, so:
    //   - `body` carrying the marker proves server -> device (the response came
    //     back from the device through the tunnel), and
    //   - the device's recorded request file proves device <- server (the
    //     request bytes traversed), read by the spec from the device.
    //
    // The request is written with `write()`, NOT `end()`: the device target
    // replies on headers-complete and never observes this client's half-close
    // (the server relay does not forward it to the device — design §4.2.1), so
    // half-closing here buys nothing and would only make the relay's drain
    // grace observable in this probe's wall clock.
    //
    // `ms` (optional) bounds how long this client waits for the device target
    // to answer, so a spec can tell "the teardown closed it" from "the socket
    // timeout fired". Default 8000.
    const port = Number(url.searchParams.get('port') || 0);
    const nonce = url.searchParams.get('nonce') || '';
    const ms = Number(url.searchParams.get('ms') || 8000);
    if (!port) return json(res, 400, { error: 'port required' });
    const sock = net.connect(port, '127.0.0.1');
    const chunks = [];
    sock.setTimeout(ms);
    sock.on('connect', () =>
      sock.write(`GET /reverse-probe?nonce=${nonce} HTTP/1.1\r\nHost: reverse\r\nConnection: close\r\n\r\n`),
    );
    sock.on('data', (c) => chunks.push(c));
    sock.on('end', () => {
      const body = Buffer.concat(chunks).toString('utf8');
      // `reached` is "the device target answered through the tunnel at all",
      // which is the server -> device direction.
      return json(res, 200, { reached: body.length > 0, nonceInBody: body.includes(nonce), body });
    });
    sock.on('error', (e) => json(res, 200, { reached: false, error: e.message }));
    sock.on('timeout', () => {
      sock.destroy();
      json(res, 200, { reached: false, error: 'timeout' });
    });
    return;
  }
  return json(res, 404, { error: 'not found', path });
});

startTarget().then(() => {
  control.listen(CONTROL_PORT, CONTROL_HOST, () => {
    console.log(`[target] control listening on http://${CONTROL_HOST}:${CONTROL_PORT}`);
  });
});
