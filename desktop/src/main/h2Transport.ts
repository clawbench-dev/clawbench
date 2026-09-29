import http2 from 'node:http2'
import { Duplex } from 'node:stream'
import type { IncomingHttpHeaders, IncomingHttpStatusHeader } from 'node:http2'
import { getSessionCookie, record } from './clientLog'

/**
 * HTTP/2 stream transport for the desktop tunnel.
 *
 * The SSH tunnel multiplexes every forwarded TCP connection over one SSH
 * channel. This module does the same thing with HTTP/2 instead: one h2 stream
 * per forwarded TCP connection, so a client only needs the main port (20000)
 * reachable — no extra listener, no `ws` dependency.
 *
 * Wire protocol (see internal/handler/tunnel_stream.go):
 *   - client -> server bytes travel in the request body
 *   - server -> client bytes travel in the response body
 *   - both directions stream simultaneously (h2 is natively full-duplex)
 *
 * This is the transport layer only: it opens/owns streams and the underlying
 * session. It deliberately does NOT know about `state.forwarded`, listeners,
 * reconnects or the reverse-forward bookkeeping — that is T7's job in
 * tunnel.ts, which will consume this module through the same shape as the
 * future `transport` abstraction.
 *
 * Transport selection (design doc §2.3): h2-over-TLS is tried first, then
 * h2c. SSH remains T7's fallback and is not implemented here. A plaintext
 * deployment pays one fast TLS failure on the first connect (the handshake is
 * rejected immediately, it does not time out), so `connect()` accepts a
 * preferred transport and reports which one actually worked — the caller is
 * expected to remember it and pass it back next time.
 */

/** Which wire transport a session is using. */
export type H2TransportKind = 'tls' | 'h2c'

export interface H2ConnectOptions {
  /** Server host, e.g. `127.0.0.1`. Brackets are added for IPv6 literals. */
  host: string
  /** Server port, e.g. 20000. */
  port: number
  /**
   * Transport to try first. Defaults to `'tls'` (the design doc's priority
   * order). Pass the previously successful kind to skip the wasted probe.
   */
  prefer?: H2TransportKind
  /**
   * Overall budget for a single connect attempt, in ms. Without it a connect
   * that never settles would leave the returned promise pending forever.
   */
  timeoutMs?: number
}

export interface H2ConnectResult {
  ok: boolean
  /**
   * The transport that succeeded. Only meaningful when `ok` is true; on
   * failure it is the kind that was tried last.
   */
  kind: H2TransportKind
  /** Human-readable failure reason, empty on success. */
  error: string
}

/**
 * A duplex stream carrying one forwarded TCP connection.
 *
 * Read = bytes from the remote target, write = bytes to it. It is a real
 * `node:stream` Duplex, so existing `socket.pipe(stream).pipe(socket)` wiring
 * works unchanged. `allowHalfOpen` is on: ending the writable side sends
 * END_STREAM without closing the readable side, which is exactly the TCP
 * half-close semantics a forwarded connection needs.
 */
export type TunnelStream = Duplex

/** One parsed NDJSON control line. Shape matches internal/tunnel/ndjson.go. */
export interface ControlMessage {
  type: string
  port?: number
  code?: number
  msg?: string
  token?: string
}

/** The reverse (-R) control plane: write one NDJSON line, read them by line. */
export interface ControlStream {
  send(msg: ControlMessage): void
  /** Resolves when the peer half-closes (EOF) or the stream errors. */
  onMessage(handler: (msg: ControlMessage) => void): void
  onClose(handler: () => void): void
  close(): void
  readonly closed: boolean
}

const DEFAULT_TIMEOUT_MS = 15000
const DEFAULT_PREFER: H2TransportKind = 'tls'
const CONTROL_PATH = '/api/tunnel/control'
const STREAM_PATH = '/api/tunnel/stream'

/**
 * How often the live session is probed with an h2 PING, and how long a PING may
 * stay unanswered before the session is declared dead. 30s matches the SSH
 * path's `setServerAliveInterval(30000)` and Android's
 * `H2_PING_INTERVAL_SECONDS` (commit cb693c7c), so both desktop transports —
 * and both platforms — notice a dead peer on the same timescale.
 */
const PING_INTERVAL_MS = 30000
const PING_TIMEOUT_MS = 30000

// A control line is tiny; the server caps its scanner at 64 KiB. Matching that
// bound here means a hostile/garbled peer cannot grow the client buffer
// without limit, and the overflow surfaces as an error rather than a hang.
const MAX_CONTROL_LINE = 64 * 1024

/**
 * The live session plus the state that decides whether it can be reused.
 *
 * Module-level (not exported) because a tunnel has exactly one session; T7
 * routes every stream through this module's functions rather than holding the
 * session itself.
 */
let session: http2.ClientHttp2Session | null = null
let sessionKind: H2TransportKind = DEFAULT_PREFER
let sessionHost = ''
let sessionPort = 0
/**
 * Set once the session has failed or been closed deliberately. A torn-down
 * session must never be reused, so `isConnected()` consults this rather than
 * `session.closed` alone (the error path may leave a non-null session).
 *
 * "Failed" now includes an unanswered keepalive PING (see startHeartbeat): that
 * is the only way a half-open connection becomes observable, since a black-holed
 * socket emits neither 'error' nor 'close'.
 */
let sessionDead = false
let connecting: Promise<H2ConnectResult> | null = null
/**
 * Keepalive timer for the live session. One PING every PING_INTERVAL_MS, with a
 * PING_TIMEOUT_MS deadline of its own (Node's `ping` callback never fires when
 * the PONG never comes — measured — so the timeout cannot come from the API).
 * Cleared by closeSession(); a stale tick is additionally guarded by the
 * session identity and the generation below.
 */
let heartbeatTimer: ReturnType<typeof setTimeout> | null = null
/**
 * Bumped on every teardown. The PING race captures it and only reports a
 * timeout while it still matches, so a PING armed against a session that has
 * since been replaced cannot mark the replacement dead.
 */
let sessionGeneration = 0

function log(level: 'd' | 'i' | 'w' | 'e', msg: string): void {
  // The main process has no appLog; clientLog.record() is its logging
  // primitive (same destination as uncaught exceptions — desktop.log and the
  // server's client.log).
  try {
    record(level.toUpperCase() as 'D' | 'I' | 'W' | 'E', 'H2Transport', msg)
  } catch {
    // Logging must never break the tunnel.
  }
}

/** Bracket a bare IPv6 literal so `http2.connect` parses the authority right. */
function authorityHost(host: string): string {
  return host.includes(':') && !host.startsWith('[') ? `[${host}]` : host
}

function authority(kind: H2TransportKind, host: string, port: number): string {
  const scheme = kind === 'tls' ? 'https' : 'http'
  return `${scheme}://${authorityHost(host)}:${port}`
}

/**
 * Mark the current session dead so `isConnected()` turns false and the tunnel
 * monitor (`tunnel.ts` monitorTick) reconnects.
 *
 * Only connection-level failures belong here — a session 'error'/'close'/
 * 'goaway' or an unanswered PING. A single stream failing does NOT: a 502/403/
 * 401 is the server answering over a perfectly good h2 session (see
 * openRawStream), so marking the session dead for it would reconnect on every
 * refused target. Mirrors Android's markSessionDead (cb693c7c), minus the
 * generation parameter: in Node the generation is folded into the heartbeat's
 * identity guard below, since that is the only late reporter.
 *
 * The session is left otherwise intact (no close here): the monitor's
 * ensureTunnel() calls connect(), which tears the old session down itself.
 */
function markDead(s: http2.ClientHttp2Session, reason: string): void {
  if (s !== session || sessionDead) return
  sessionDead = true
  log('w', `session dead: ${reason}`)
}

/**
 * Attach the error handlers a session needs to not crash the main process.
 *
 * Without these, an `'error'` on the session (TLS rejected, GOAWAY, socket
 * reset) is an unhandled `'error'` event and Node throws — in Electron that is
 * a "JavaScript error occurred in the main process" dialog and a dead app.
 *
 * They also feed `markDead`: these events are the *only* signal a half-open
 * connection gives on its own, and they are what makes `isConnected()` honest
 * when the peer resets or sends GOAWAY. A silent black hole emits none of them,
 * which is what the heartbeat exists for.
 */
function armSession(s: http2.ClientHttp2Session): void {
  s.on('error', (err: NodeJS.ErrnoException) => {
    markDead(s, `error: ${err.code || err.message}`)
  })
  s.on('close', () => {
    if (s !== session) return
    sessionDead = true
    session = null
  })
  s.on('goaway', (code: number) => {
    markDead(s, `GOAWAY code=${code}`)
  })
}

/**
 * Arm the keepalive: one PING every PING_INTERVAL_MS, each with its own
 * PING_TIMEOUT_MS deadline.
 *
 * This is what turns a half-open connection into something observable. A NAT
 * rebind or a server restart leaves the TCP socket open but silent; Node emits
 * neither 'error' nor 'close', and a `ping()` issued into that hole never
 * invokes its callback (all measured). Without a self-imposed deadline the
 * session would look healthy forever, `isConnected()` would keep returning true,
 * and the monitor would never reconnect — stranding every `-R` mapping until a
 * manual reconnect. Identical to the Android defect fixed in cb693c7c, where the
 * PING is what replaces the removed `readTimeout`.
 *
 * The timer is NOT unref'd: it is short-lived and owns no socket of its own, and
 * an unref'd timer could let the process exit between ticks while the tunnel is
 * meant to stay up (the session's own socket keeps the loop alive regardless).
 */
function startHeartbeat(s: http2.ClientHttp2Session): void {
  stopHeartbeat()
  const gen = sessionGeneration
  heartbeatTimer = setTimeout(() => {
    heartbeatTimer = null
    // A teardown (closeSession) clears the timer; this guards a tick that was
    // already queued when it ran, and a PING from a superseded session.
    if (gen !== sessionGeneration || s !== session || sessionDead) return
    let answered = false
    try {
      s.ping((err?: Error | null) => {
        // A PONG arrived (or the session reported an error of its own). Either
        // way the PING did not time out, so the session stays alive; the next
        // tick is armed below.
        answered = true
        if (gen !== sessionGeneration || s !== session || sessionDead) return
        if (err) markDead(s, `ping failed: ${err.message}`)
        else startHeartbeat(s)
      })
    } catch (err) {
      // ping() throws ERR_HTTP2_INVALID_SESSION once the session is destroyed.
      markDead(s, `ping threw: ${(err as Error).message}`)
      return
    }
    setTimeout(() => {
      if (answered) return
      markDead(s, 'ping timeout')
      // No re-arm: the next connect() starts a fresh heartbeat.
    }, PING_TIMEOUT_MS)
  }, PING_INTERVAL_MS)
}

function stopHeartbeat(): void {
  if (heartbeatTimer) {
    clearTimeout(heartbeatTimer)
    heartbeatTimer = null
  }
}

/**
 * Prove the session actually speaks HTTP/2 by completing one round trip.
 *
 * This is load-bearing, not belt-and-braces: `http2.connect()` fires
 * `'connect'` on the raw TCP connect, before any h2 exchange (measured). A
 * plain HTTP/1.1 server therefore looks "connected" until the first request,
 * which is where `ERR_HTTP2_ERROR` arrives. Since a plaintext deployment
 * always starts with a TLS attempt that fails at the handshake, the failure
 * must be observable here — otherwise the h2c fallback would never trigger and
 * the tunnel would hang on a half-dead session.
 *
 * A 401 is a valid h2 session: the server answered over h2, so the transport
 * works and the cookie/credentials are a separate concern.
 */
function verifySession(s: http2.ClientHttp2Session): Promise<void> {
  return new Promise((resolve, reject) => {
    let settled = false
    const finish = (err?: Error) => {
      if (settled) return
      settled = true
      if (err) reject(err)
      else resolve()
    }
    let req: http2.ClientHttp2Stream
    try {
      req = s.request({ ':method': 'GET', ':path': '/api/ssh/info' })
    } catch (err) {
      finish(err as Error)
      return
    }
    req.on('response', (headers: IncomingHttpHeaders & IncomingHttpStatusHeader) => {
      const status = Number(headers[':status'])
      // Any real HTTP status proves h2 framing works. 401 included. Settle
      // FIRST, then release the stream: destroy() emits 'close' synchronously,
      // and the close handler below must not turn this success into an error.
      finish(status >= 100 && status <= 599 ? undefined : new Error(`unexpected status ${headers[':status']}`))
      // The probe only needs the status line. Release the stream instead of
      // leaving its (unread) body buffered — an unread response never emits
      // 'end', so the stream would stay open for the life of the session and
      // a later session.close() would wait on it.
      try { req.destroy() } catch { /* already gone */ }
    })
    req.on('error', (err: NodeJS.ErrnoException) => {
      finish(new Error(err.code || err.message))
    })
    // A session teardown closes the stream without a response; settle so the
    // promise cannot dangle (which would also leak in the test harness's
    // async-leak detector).
    req.on('close', () => finish(new Error('stream closed before response')))
    req.end()
  })
}

/** One connect attempt over a single transport kind. */
function connectVia(kind: H2TransportKind, opts: Required<Pick<H2ConnectOptions, 'host' | 'port' | 'timeoutMs'>>): Promise<http2.ClientHttp2Session> {
  return new Promise((resolve, reject) => {
    const target = authority(kind, opts.host, opts.port)
    let settled = false
    let s: http2.ClientHttp2Session | null = null

    const fail = (err: Error) => {
      if (settled) return
      settled = true
      clearTimeout(timer)
      if (s) {
        // A failed attempt's session must release its socket immediately.
        // destroy() (not close()) — close() waits for open streams, and a
        // half-open h2 session can have streams that never end.
        try { s.destroy() } catch { /* already gone */ }
      }
      reject(err)
    }
    const timer = setTimeout(() => fail(new Error('connect timed out')), opts.timeoutMs)

    try {
      s = http2.connect(target, {
        // Self-hosted instances commonly use a self-signed certificate;
        // refusing it would drop the tunnel on exactly those installs. The
        // session is authenticated by the session cookie, not by the cert.
        ...(kind === 'tls' ? { rejectUnauthorized: false } : {}),
      })
    } catch (err) {
      fail(err as Error)
      return
    }

    // Arm the error handler immediately: the failure can land before the
    // verify request even exists (TLS handshake rejection is the common case).
    s.on('error', (err: NodeJS.ErrnoException) => fail(new Error(err.code || err.message)))
    s.on('close', () => fail(new Error('session closed before it was ready')))
    s.on('goaway', (code: number) => fail(new Error(`session GOAWAY code=${code}`)))

    verifySession(s).then(
      () => {
        if (settled) return
        settled = true
        clearTimeout(timer)
        resolve(s as http2.ClientHttp2Session)
      },
      (err: Error) => fail(err),
    )
  })
}

/**
 * Establish (or reuse) the h2 session.
 *
 * Reuse is unconditional when a healthy session for the same host/port exists,
 * regardless of `prefer`: reconnecting just to switch transports would drop
 * every live forwarded connection.
 */
export async function connect(opts: H2ConnectOptions): Promise<H2ConnectResult> {
  const host = opts.host
  const port = opts.port
  const prefer = opts.prefer ?? DEFAULT_PREFER
  const timeoutMs = opts.timeoutMs ?? DEFAULT_TIMEOUT_MS

  if (isConnected() && sessionHost === host && sessionPort === port) {
    return { ok: true, kind: sessionKind, error: '' }
  }

  // Single-flight: two concurrent callers must share one session, or the
  // second would replace the first's session and strand its streams.
  if (connecting) return connecting

  const p = (async (): Promise<H2ConnectResult> => {
    const order: H2TransportKind[] = prefer === 'tls' ? ['tls', 'h2c'] : ['h2c', 'tls']
    let lastError = ''
    let lastKind: H2TransportKind = order[order.length - 1]

    for (const kind of order) {
      lastKind = kind
      try {
        const s = await connectVia(kind, { host, port, timeoutMs })
        // A previous session (failed or for another server) must not be
        // reused; drop it before publishing the new one.
        closeSession()
        session = s
        sessionKind = kind
        sessionHost = host
        sessionPort = port
        sessionDead = false
        armSession(s)
        startHeartbeat(s)
        log('i', `connected via ${kind} to ${host}:${port}`)
        return { ok: true, kind, error: '' }
      } catch (err) {
        lastError = (err as Error)?.message || String(err)
        log('w', `${kind} connect failed: ${lastError}`)
      }
    }

    return { ok: false, kind: lastKind, error: lastError || 'no transport available' }
  })().finally(() => { connecting = null })

  connecting = p
  return p
}

/** The transport the live session is using. `null` when not connected. */
export function getTransportKind(): H2TransportKind | null {
  return isConnected() ? sessionKind : null
}

/**
 * True while the session is believed healthy.
 *
 * This is the tunnel monitor's only liveness signal (`monitorTick` in
 * tunnel.ts), so it must reflect a dead connection rather than local intent:
 * `sessionDead` is set by a session 'error'/'close'/'goaway' or by an
 * unanswered keepalive PING. Before the heartbeat existed a half-open session
 * reported `true` forever, the monitor skipped it, and every `-R` mapping stayed
 * dead until a manual reconnect — the desktop half of the Android defect fixed
 * in cb693c7c.
 */
export function isConnected(): boolean {
  return !!session && !sessionDead && !session.closed && !session.destroyed
}

/** Close the current session and mark it unusable. Idempotent. */
function closeSession(): void {
  // Retire the keepalive first: a PING still in flight must not fire against
  // (or restart a heartbeat for) the session being torn down, and the timer
  // must not leak past the session it belongs to.
  stopHeartbeat()
  sessionGeneration++
  const s = session
  session = null
  sessionDead = true
  if (!s) return
  // destroy(), not close(): close() is graceful and waits for every open
  // stream to finish (measured — the session stays alive and the socket stays
  // open). Tearing down a tunnel must drop the forwarded connections with it,
  // otherwise a disconnect would leave the session and its socket alive until
  // the last forwarded connection happened to end.
  try { s.destroy() } catch { /* already gone */ }
}

/** Tear down the session. Every stream on it dies with it (by design). */
export function close(): void {
  closeSession()
}

/**
 * Read the session cookie and render it as a `cookie` request header.
 *
 * The h2 stream is authenticated by the same session cookie as the rest of the
 * API (`internal/middleware/auth.go`); h2 carries it as an ordinary header.
 * Returns `undefined` when not logged in, so the header is simply omitted and
 * the server answers 401 rather than the request being malformed.
 */
async function cookieHeader(): Promise<string | undefined> {
  const cookie = await getSessionCookie()
  return cookie || undefined
}

/**
 * Open the raw h2 stream for one request and wire it into a duplex.
 *
 * `expectStatus` is the status the caller considers a working stream. Anything
 * else (401/403/502/503) means there is no TCP connection behind it, so the
 * stream is destroyed with an error rather than handed back as a silent empty
 * duplex — otherwise the local socket would be spliced to nothing and appear
 * to hang.
 */
function openRawStream(
  path: string,
  expectStatus: number,
): Promise<{ stream: TunnelStream; headers: IncomingHttpHeaders & IncomingHttpStatusHeader }> {
  return new Promise((resolve, reject) => {
    const s = session
    if (!s || !isConnected()) {
      reject(new Error('not connected'))
      return
    }

    cookieHeader().then((cookie) => {
      // The session may have died while the cookie jar was being read.
      if (!s || !isConnected()) {
        reject(new Error('not connected'))
        return
      }

      const headers: http2.OutgoingHttpHeaders = {
        ':method': 'POST',
        ':path': path,
        'content-type': 'application/octet-stream',
        ...(cookie ? { cookie } : {}),
      }

      let raw: http2.ClientHttp2Stream
      try {
        // No `content-length`: the body is a stream with no known end.
        raw = s.request(headers)
      } catch (err) {
        reject(err as Error)
        return
      }

      let settled = false
      const fail = (err: Error) => {
        if (settled) return
        settled = true
        try { raw.destroy() } catch { /* already gone */ }
        reject(err)
      }

      raw.on('error', (err: NodeJS.ErrnoException) => fail(new Error(err.code || err.message)))

      // A session that dies before the response arrives closes the stream with
      // 'aborted'/'close' and NO 'error' (measured) — without this the caller
      // would await a promise that never settles.
      raw.on('aborted', () => fail(new Error('stream aborted before response')))
      raw.on('close', () => fail(new Error('stream closed before response')))

      raw.on('response', (resHeaders: IncomingHttpHeaders & IncomingHttpStatusHeader) => {
        if (settled) return
        const status = Number(resHeaders[':status'])
        if (status !== expectStatus) {
          fail(new Error(`stream rejected with HTTP ${resHeaders[':status']}`))
          return
        }
        settled = true
        resolve({ stream: wrapStream(raw), headers: resHeaders })
      })
    }).catch((err) => reject(err as Error))
  })
}

/**
 * Wrap an h2 stream in a plain Duplex.
 *
 * The wrapper exists for three reasons the raw stream cannot satisfy:
 *
 *  1. Backpressure, both ways. Writes wait for `'drain'` when the underlying
 *     stream is full; reads pause the underlying stream when the consumer
 *     stops draining (`_read` resumes it). `session.socket.pause()` is NOT an
 *     option — it throws `ERR_HTTP2_NO_SOCKET_MANIPULATION`.
 *  2. Half-close without full close. `_final` calls `end()` on the underlying
 *     stream, which sends END_STREAM; the readable side stays open so the
 *     remote can keep replying.
 *  3. A local write-after-end guard. The underlying stream emits
 *     `ERR_STREAM_WRITE_AFTER_END` asynchronously, which would otherwise
 *     surface as a stream error after a legitimate half-close. The wrapper
 *     tracks `_final` and drops late writes instead.
 */
function wrapStream(raw: http2.ClientHttp2Stream): TunnelStream {
  // Set by `final` once END_STREAM has been sent, so a later write is dropped
  // instead of letting the underlying stream emit
  // ERR_STREAM_WRITE_AFTER_END asynchronously.
  let ended = false

  const wrapper = new Duplex({
    allowHalfOpen: true,
    // The consumer wants more: resume the wire. Safe to call unconditionally —
    // a stream that is not paused treats resume() as a no-op.
    read() {
      raw.resume()
    },
    write(chunk: Buffer | string, _enc: BufferEncoding, cb: (err?: Error | null) => void) {
      // `ended` is checked in the write() override below, before Node's own
      // "write after end" guard can fire; this branch is the belt for a write
      // that was already queued when `final` ran.
      if (ended) { cb(); return }
      try {
        if (raw.write(chunk)) cb()
        else raw.once('drain', cb)
      } catch (err) {
        cb(err as Error)
      }
    },
    final(cb: (err?: Error | null) => void) {
      ended = true
      try {
        raw.end()
        cb()
      } catch (err) {
        cb(err as Error)
      }
    },
    destroy(err: Error | null, cb: (err?: Error | null) => void) {
      try { raw.destroy() } catch { /* already gone */ }
      cb(err)
    },
  })

  // Push remote bytes through, honouring the consumer's read pace. A false
  // return from push() means the wrapper's buffer is full; pausing the
  // underlying stream lets h2 flow control stop the sender instead of
  // buffering without limit.
  raw.on('data', (chunk: Buffer) => {
    if (!wrapper.push(chunk)) raw.pause()
  })
  raw.on('end', () => {
    // Remote closed its side. This does NOT close the writable side.
    wrapper.push(null)
  })
  // 'error' must be forwarded, not swallowed: without it the wrapper would
  // stay open forever after the underlying stream dies, and the local socket
  // spliced to it would hang.
  raw.on('error', (err: Error) => {
    wrapper.destroy(err)
  })
  // An RST_STREAM (or session teardown) closes the stream WITHOUT emitting
  // 'end' (measured), so a readable that only watched 'end' would never see
  // EOF and the local socket piped from it would hang. Ending it here covers
  // that case; on a normal close 'end' already ran, so the guard skips.
  raw.on('close', () => {
    if (!wrapper.destroyed && !wrapper.readableEnded) wrapper.push(null)
  })

  // Write-after-end guard.
  //
  // Node's Writable emits ERR_STREAM_WRITE_AFTER_END from write() itself,
  // BEFORE _write is consulted, so guarding inside the implementation is too
  // late — a chunk that races the local socket's EOF (the common case: the
  // peer closes while data is still in flight) would surface as a stream
  // error and tear down a stream that was half-closing normally. Intercepting
  // the public method drops those late writes instead. `end()` is wrapped too
  // so `writableEnded` reflects the local intent immediately.
  const baseWrite = wrapper.write.bind(wrapper)
  const baseEnd = wrapper.end.bind(wrapper)
  wrapper.write = ((chunk: unknown, enc?: unknown, cb?: unknown): boolean => {
    if (ended || wrapper.writableEnded) {
      const done = typeof enc === 'function' ? enc : cb
      if (typeof done === 'function') (done as () => void)()
      return true
    }
    return (baseWrite as (...a: unknown[]) => boolean)(chunk, enc, cb)
  }) as typeof wrapper.write
  wrapper.end = ((...args: unknown[]): typeof wrapper => {
    ended = true
    return (baseEnd as (...a: unknown[]) => typeof wrapper)(...args)
  }) as typeof wrapper.end

  return wrapper
}

/** Render `?host=&port=` with proper escaping (IPv6 literals need brackets). */
function targetQuery(host: string, port: number): string {
  const params = new URLSearchParams()
  params.set('host', host)
  params.set('port', String(port))
  return params.toString()
}

/**
 * Open a data stream to `host:port` on the server (`-L` forward).
 *
 * Resolves with a duplex carrying that TCP connection. Rejects when the
 * server could not establish it (dial failure -> 502, disallowed port -> 403,
 * unauthenticated -> 401), so the caller can destroy the local socket instead
 * of piping it into nothing.
 */
export function openStream(host: string, port: number): Promise<TunnelStream> {
  return openRawStream(`${STREAM_PATH}?${targetQuery(host, port)}`, 200).then((r) => r.stream)
}

/**
 * Open a data stream that claims a parked reverse connection (`-R`).
 *
 * The token came from an `incoming` control message and is single-use; the
 * server matches it to the connection it parked. Same duplex semantics as
 * `openStream`.
 */
export function openClaimStream(token: string): Promise<TunnelStream> {
  const params = new URLSearchParams()
  params.set('claim', token)
  return openRawStream(`${STREAM_PATH}?${params.toString()}`, 200).then((r) => r.stream)
}

/**
 * Open the long-lived `-R` control stream and expose it as NDJSON lines.
 *
 * The server's request-body direction carries `bind`/`unbind`/`ping`; the
 * response body carries `bound`/`bind_err`/`incoming`/`unbound`/`pong`.
 * Lines are framed by `\n`, matching internal/tunnel/ndjson.go.
 */
export async function openControlStream(): Promise<ControlStream> {
  const { stream } = await openRawStream(CONTROL_PATH, 200)

  let closed = false
  let buffer = ''
  const messageHandlers: Array<(msg: ControlMessage) => void> = []
  const closeHandlers: Array<() => void> = []

  const emitClose = () => {
    if (closed) return
    closed = true
    for (const h of closeHandlers) h()
  }

  stream.on('data', (chunk: Buffer) => {
    buffer += chunk.toString('utf8')
    if (buffer.length > MAX_CONTROL_LINE) {
      // A line this long means the peer is not speaking the protocol; drop the
      // stream rather than buffering without bound.
      stream.destroy(new Error('control line exceeds limit'))
      return
    }
    let nl = buffer.indexOf('\n')
    while (nl !== -1) {
      const line = buffer.slice(0, nl).trim()
      buffer = buffer.slice(nl + 1)
      if (line) {
        try {
          const msg = JSON.parse(line) as ControlMessage
          if (msg && typeof msg.type === 'string') {
            for (const h of messageHandlers) h(msg)
          }
        } catch {
          // Malformed line: skip it, mirroring the server's readControlLoop.
          // One bad byte must not drop every reverse port.
          log('w', 'dropping malformed control line')
        }
      }
      nl = buffer.indexOf('\n')
    }
  })
  stream.on('end', emitClose)
  stream.on('error', emitClose)
  stream.on('close', emitClose)

  return {
    send(msg: ControlMessage): void {
      if (closed) return
      try {
        stream.write(`${JSON.stringify(msg)}\n`)
      } catch {
        // The stream is gone; onClose has already fired or will.
      }
    },
    onMessage(handler) { messageHandlers.push(handler) },
    onClose(handler) { closeHandlers.push(handler) },
    close(): void {
      if (closed) return
      try { stream.end() } catch { /* already gone */ }
      try { stream.destroy() } catch { /* already gone */ }
    },
    get closed() { return closed },
  }
}

/**
 * Test helper — reset module state so each test starts from a clean slate.
 * Module-level session state otherwise leaks across tests (the same reason
 * tunnel.ts has disconnectTunnel()).
 */
export function _resetForTesting(): void {
  connecting = null
  closeSession()
  sessionKind = DEFAULT_PREFER
  sessionHost = ''
  sessionPort = 0
}

/**
 * Test helper — whether a keepalive tick is currently armed.
 *
 * `closeSession()`'s clearTimeout is what stops the heartbeat leaking past the
 * session it belongs to; the stale tick would otherwise be a silent no-op (its
 * session identity guard drops it), so no behavioural assertion can see a
 * missing clear. This exposes the handle's existence so the leak is testable.
 */
export function _hasHeartbeatForTesting(): boolean {
  return heartbeatTimer !== null
}
