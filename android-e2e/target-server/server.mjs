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
 *      container) can drive: start/stop the target to assert the negative path,
 *      and read back the exact bytes the target received through the tunnel.
 *
 * Two listeners:
 *
 *   TARGET_PORT (default 18080) — a raw `node:net` echo-ish HTTP target.
 *     It answers any request with a deterministic body and, crucially, records
 *     the exact request bytes it saw. That is how the suite proves bytes really
 *     traversed the tunnel client→server (the request arrived) and
 *     server→client (the response came back).
 *
 *   CONTROL_PORT (default 18081) — `node:http`, reached by the runner at
 *     `http://emulator:18081` (compose service name). Endpoints:
 *       POST /__target/stop    — stop accepting on TARGET_PORT
 *       POST /__target/start   — (re)start it
 *       GET  /__target/status  — { listening, requests }
 *       GET  /__target/echo?nonce=<x> — out-of-band fetch, used to prove the
 *         target is genuinely reachable directly (a control for the tunnel
 *         assertion: a failed tunnel with a dead target proves nothing).
 *       POST /__reset          — clear the recorded request log
 *
 * Dependency-free (`node:net`, `node:http`) so it needs no npm install.
 */
import net from 'node:net';
import http from 'node:http';

const TARGET_PORT = Number(process.env.TARGET_PORT || 18080);
const CONTROL_PORT = Number(process.env.CONTROL_PORT || 18081);
const TARGET_HOST = process.env.TARGET_HOST || '127.0.0.1';
const CONTROL_HOST = process.env.CONTROL_HOST || '0.0.0.0';

/** Every request body the target has received, newest last. */
const received = [];

/**
 * The target protocol: behave like a real HTTP/1.1 server — reply as soon as the
 * request headers are complete — with a deterministic body that ECHOES what it
 * received.
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
function handleConnection(socket) {
  const chunks = [];
  socket.setNoDelay(true);

  const idle = setTimeout(() => {
    // Neither headers nor EOF arrived; answer with what we have rather than
    // hanging the test.
    reply();
  }, 3000);

  let done = false;
  function reply() {
    if (done) return;
    done = true;
    clearTimeout(idle);
    const req = Buffer.concat(chunks);
    received.push(req.toString('utf8'));
    if (process.env.TARGET_VERBOSE) {
      console.log(`[target] received ${req.length} bytes`);
    }
    const body = `TARGET-ECHO:${req.toString('utf8')}`;
    const head =
      'HTTP/1.1 200 OK\r\n' +
      'Content-Type: text/plain\r\n' +
      `Content-Length: ${Buffer.byteLength(body)}\r\n` +
      'Connection: close\r\n\r\n';
    socket.end(head + body);
  }

  socket.on('data', (c) => {
    chunks.push(c);
    // Headers complete? Reply now. The probe uses `Connection: close` and a
    // body-less GET, so end-of-headers is end-of-request for this target.
    if (Buffer.concat(chunks).includes('\r\n\r\n')) reply();
  });
  socket.on('end', reply);
  socket.on('error', () => {
    clearTimeout(idle);
  });
}

let targetServer = null;
function startTarget() {
  return new Promise((resolve) => {
    if (targetServer) return resolve();
    targetServer = net.createServer(handleConnection);
    targetServer.on('error', (e) => console.error(`[target] listen error: ${e.message}`));
    targetServer.listen(TARGET_PORT, TARGET_HOST, () => {
      console.log(`[target] listening on ${TARGET_HOST}:${TARGET_PORT}`);
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
    return json(res, 200, { listening: !!targetServer, port: TARGET_PORT, requests: received });
  }
  if (path === '/__reset' && req.method === 'POST') {
    received.length = 0;
    return json(res, 200, { ok: true });
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
    // with a fixed `DEVICE-TARGET: <marker>` body and records the request
    // separately, so:
    //   - `body` carrying the marker proves server -> device (the response came
    //     back from the device through the tunnel), and
    //   - the device's recorded request file proves device <- server (the
    //     request bytes traversed), read by the spec from the device.
    // `nonceInBody` is therefore expected to be false for this target shape and
    // is reported only so a future echoing target is visible.
    const port = Number(url.searchParams.get('port') || 0);
    const nonce = url.searchParams.get('nonce') || '';
    if (!port) return json(res, 400, { error: 'port required' });
    const sock = net.connect(port, '127.0.0.1');
    const chunks = [];
    sock.setTimeout(8000);
    sock.on('connect', () => sock.write(`GET /reverse-probe?nonce=${nonce} HTTP/1.1\r\nHost: reverse\r\nConnection: close\r\n\r\n`));
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
