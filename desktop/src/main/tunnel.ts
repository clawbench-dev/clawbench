import net from 'node:net'
import http from 'node:http'
import https from 'node:https'
import type { Duplex } from 'node:stream'
import { Client } from 'ssh2'
import { getPassword } from './secrets'
import { getStore } from './store'
import {
  createH2TunnelTransport,
  type ControlMessage,
  type ControlStream,
  type H2TunnelTransport,
  type TransportConnectOptions,
  type TransportPreference,
  type TunnelTransport,
} from './transport'

export type TunnelErrorType = 'auth' | 'network' | 'hostkey' | 'unknown' | ''

/** 'forward' = ssh -L (server → local), 'reverse' = ssh -R (local → server). */
export type ForwardDirection = 'forward' | 'reverse'

export interface TunnelState {
  connected: boolean
  error: string
  errorType: TunnelErrorType
  /**
   * The DESIRED set of forwards, keyed by the port the OTHER side listens on:
   * the local port for a forward, the server-side port for a reverse. This is
   * intent, not runtime state: it survives a disconnect so a reconnect can
   * rebuild every listener (Android's BackgroundService does the same — its
   * `disconnectInternal()` deliberately keeps `forwardedPorts`).
   *
   * The live listeners live in `forwardServers` below; reverse forwards have no
   * local listener (the server binds) and are tracked only here.
   */
  forwarded: Map<number, { targetPort: number; host: string; direction: ForwardDirection }>
}

const state: TunnelState = { connected: false, error: '', errorType: '', forwarded: new Map() }
let client: Client | null = null

// ---------------------------------------------------------------------------
// Transport selection
// ---------------------------------------------------------------------------

/**
 * The active transport. Rebuilt on every connect attempt from the current
 * preference; `disconnectTunnel` closes and drops it. Everything below routes
 * through it so the SSH and h2 paths share one set of listeners, bookkeeping
 * and reconnect logic.
 */
let transport: TunnelTransport | null = null

/**
 * The SSH transport currently owns an ssh2 `Client`, which lives in this
 * module (`client`). Because `transport` is recreated per attempt while the
 * client outlives it (the monitor reconnects without a full teardown), the
 * adapter is cached and rebuilt only when it has no live client to wrap.
 */
let sshTransport: TunnelTransport | null = null

/**
 * Which transport to use. Electron is hard-wired to SSH: there is no live
 * caller of `setTransportPreference()` in production, so the server's
 * `port_forward.transport` value ('h2'/'both') is deliberately never consumed
 * here. The SSH default keeps today's behavior (and the existing test suite)
 * byte-for-byte identical.
 *
 * The setter is retained — `tunnel.h2.test.ts` drives the h2 dispatch tests
 * through it directly — and the h2 path below is kept so the transport can be
 * enabled later without rewriting it.
 */
let transportPreference: TransportPreference = 'ssh'

/** Which transport actually carried the last successful connect. */
let activeTransport: 'ssh' | 'h2' = 'ssh'

/**
 * The h2 wire kind (h2-over-TLS vs h2c) that worked last. A plaintext
 * deployment rejects the TLS attempt at the handshake, so remembering the kind
 * avoids paying that rejection on every reconnect (design doc §2.3). Module
 * state, not persisted: the cost of getting it wrong once per process is one
 * fast failed handshake, while a stale store entry would need its own
 * invalidation rules (and T8 owns persistence).
 */
let lastH2Kind: 'tls' | 'h2c' | null = null

export function setTransportPreference(pref: TransportPreference): void {
  transportPreference = pref
}

export function getTransportPreference(): TransportPreference {
  return transportPreference
}

/** The transport that carried the last successful connect. */
export function getActiveTransport(): 'ssh' | 'h2' {
  return activeTransport
}

/**
 * The h2 adapter, created lazily on first use.
 *
 * It is cached (rather than created per attempt) because it owns the -R
 * control stream: dropping the adapter between attempts would orphan a live
 * control stream, whose server-side binds would then be invisible to the new
 * one. T6's `connect()` keeps the session, so the adapter outlives attempts.
 */
let h2Transport: H2TunnelTransport | null = null

function getH2Transport(): H2TunnelTransport {
  if (!h2Transport) h2Transport = createH2TunnelTransport()
  return h2Transport
}

/** The h2 wire kind in use, or null when the active transport is not h2. */
export function getTunnelTransportKind(): 'tls' | 'h2c' | null {
  return transport && transport === h2Transport ? h2Transport.getKind() : null
}

/** Ordered transport candidates for the current preference (design doc §2.3). */
function transportCandidates(): Array<'ssh' | 'h2'> {
  if (transportPreference === 'ssh') return ['ssh']
  if (transportPreference === 'h2') return ['h2']
  // 'both': probe h2 first, then fall back to SSH.
  return ['h2', 'ssh']
}

// Upper bound on a single connect attempt. Without it a connect that never
// emits `ready` or `error` (dropped SYN, half-open peer) would leave the
// returned promise pending forever, and every caller awaiting it — notably
// addForwardedPort — would hang with no listener ever bound.
const CONNECT_TIMEOUT_MS = 20000

// SSH keepalive, mirroring Android's JSch settings
// (BackgroundService: setServerAliveInterval(30000) / setServerAliveCountMax(3)).
// Without it an idle tunnel is silently dropped by NAT/stateful firewalls — the
// observed failure was a remote client whose connection died after exactly
// 12.5s, after which nothing restored it.
const KEEPALIVE_INTERVAL_MS = 30000
const KEEPALIVE_COUNT_MAX = 3

// Connection monitor: Android has a 15s poll with 5/10/30/60/120s backoff and
// automatic re-establishment of every forward. The desktop shell had no
// equivalent, so any drop was permanent until the user manually hit refresh.
const MONITOR_INTERVAL_MS = 15000
const RECONNECT_DELAYS_MS = [5000, 10000, 30000, 60000, 120000]

// The listening net.Server for each forwarded local port. Kept alongside
// `state.forwarded` because removing a forward must CLOSE the listener — the
// map entry alone is bookkeeping, and dropping it without closing leaves the
// local port bound forever (a leak that also makes re-adding the same port
// fail with EADDRINUSE).
const forwardServers = new Map<number, net.Server>()

/**
 * Reverse (ssh -R) forwards currently registered with the server, keyed by the
 * port the SERVER bound. There is no local listener for these — the server owns
 * the socket — so this map exists to (a) route incoming `tcp connection` events
 * to the right local target and (b) unforward them on teardown.
 *
 * The value's `serverPort` may differ from the key when the server allocated a
 * port (we requested 0), so callers must use `serverPort` for unforwardIn.
 */
const reverseForwards = new Map<number, { targetPort: number; host: string; serverPort: number }>()

// In-flight connect attempt, if any. Callers that arrive while a connect is
// running must join it instead of starting a second one: openClient replaces
// the module's client, so a second attempt would strand the first caller's
// promise (see ensureTunnel).
let connecting: Promise<boolean> | null = null

// Connection monitor. Armed whenever there is at least one desired forward, so
// a dropped tunnel is restored without the user having to notice and hit
// refresh. Mirrors Android's BackgroundService connection monitor.
let monitorTimer: ReturnType<typeof setInterval> | null = null
let reconnectAttempt = 0
let lastAttemptAt = 0

function startMonitor(): void {
  if (monitorTimer) return
  monitorTimer = setInterval(() => { void monitorTick() }, MONITOR_INTERVAL_MS)
  // Never let the monitor alone keep the app's event loop alive.
  monitorTimer.unref?.()
}

function stopMonitor(): void {
  if (!monitorTimer) return
  clearInterval(monitorTimer)
  monitorTimer = null
  reconnectAttempt = 0
}

/** Keep the monitor armed exactly while there is something worth maintaining. */
function syncMonitor(): void {
  if (state.forwarded.size > 0) startMonitor()
  else stopMonitor()
}

/**
 * One monitor tick: if the tunnel is down but forwards are still desired,
 * reconnect (with backoff) so the transport's ready path rebuilds them.
 */
async function monitorTick(): Promise<void> {
  if (state.forwarded.size === 0) { stopMonitor(); return }
  // Liveness comes from the transport, not from the ssh2 `client` field: under
  // h2 there is no client, and an SSH-only check would treat a healthy h2
  // tunnel as down on every tick — inflating reconnectAttempt until a real drop
  // waited out the 120s backoff instead of reconnecting on the next tick.
  if (state.connected && transport?.isConnected()) { reconnectAttempt = 0; return }
  if (connecting) return // an attempt is already in flight; let it finish

  if (reconnectAttempt > 0) {
    const delay = RECONNECT_DELAYS_MS[Math.min(reconnectAttempt - 1, RECONNECT_DELAYS_MS.length - 1)]
    if (Date.now() - lastAttemptAt < delay) return
  }
  reconnectAttempt++
  lastAttemptAt = Date.now()
  // On success ensureTunnel() rebuilds every desired port; failures are retried
  // on subsequent ticks with longer delays.
  await ensureTunnel()
}

/** Close and forget the listener for one local port, if any. */
function closeForwardServer(localPort: number): void {
  const server = forwardServers.get(localPort)
  if (!server) return
  forwardServers.delete(localPort)
  try { server.close() } catch { /* already closed */ }
}

/** Close every live listener. Does NOT touch `state.forwarded` (the intent). */
function closeAllForwardServers(): void {
  for (const localPort of [...forwardServers.keys()]) closeForwardServer(localPort)
}

/**
 * Unregister a reverse forward from the server.
 *
 * `key` is the entry's key in `reverseForwards`; the actual server-side port may
 * differ (the server allocated one when we asked for 0), so the transport must
 * be told the recorded `serverPort`, not the key.
 */
function unforwardReverse(key: number): void {
  const entry = reverseForwards.get(key)
  reverseForwards.delete(key)
  if (!entry || !transport || !state.connected) return
  // Best effort, like ssh2's unforwardIn callback: the server also releases
  // every bind owned by the connection when it drops.
  void transport.unbind(entry.serverPort)
}

/** Unregister every reverse forward. Does NOT touch `state.forwarded` (the intent). */
function unforwardAllReverse(): void {
  for (const key of [...reverseForwards.keys()]) unforwardReverse(key)
}

export function isTunnelConnected(): boolean { return state.connected }
export function getTunnelError(): string { return state.error }
export function getTunnelErrorType(): TunnelErrorType { return state.errorType }
export function getForwardedPorts(): Array<{ port: number; host: string; direction: ForwardDirection }> {
  return [...state.forwarded.entries()].map(([port, v]) => ({ port, host: v.host, direction: v.direction }))
}

/**
 * Map a connect failure onto the error type the UI understands
 * (`'auth' | 'network' | 'hostkey' | 'unknown' | ''`).
 *
 * The ssh2 branches are unchanged. The h2 additions map onto the same
 * vocabulary; `hostkey` stays SSH-only, since h2 has no host key concept and
 * mislabeling a TLS problem as one would send the user to an SSH panel that
 * cannot fix it.
 */
function classifyError(err: Error & { level?: string; code?: string }): TunnelErrorType {
  // --- ssh2 (unchanged, and deliberately checked first) ---
  if (err.level === 'client-authentication') return 'auth'
  if (err.code === 'ENOTFOUND' || err.code === 'ECONNREFUSED' || err.code === 'ETIMEDOUT') return 'network'
  if (err.level === 'client-timeout') return 'hostkey'

  const code = err.code || ''
  const message = err.message || ''

  // --- h2 / socket additions ---
  // A wrong-version peer (h2c pointed at a plain HTTP/1.1 server) surfaces as
  // ERR_HTTP2_ERROR, and a rejected stream/frame as ERR_HTTP2_STREAM_ERROR —
  // both are connectivity outcomes, not auth.
  if (
    code === 'ERR_HTTP2_ERROR' ||
    code === 'ERR_HTTP2_STREAM_ERROR' ||
    code === 'ERR_HTTP2_GOAWAY_SESSION' ||
    code === 'ERR_HTTP2_INVALID_SESSION' ||
    code === 'ERR_HTTP2_SESSION_ERROR' ||
    code === 'ECONNRESET' ||
    code === 'EPIPE'
  ) return 'network'
  // TLS failures are reported as a message, not a stable code (OpenSSL's
  // reason strings differ per version), so they must be matched by text too.
  if (
    code === 'ERR_TLS_CERT_ALTNAME_INVALID' ||
    code === 'DEPTH_ZERO_SELF_SIGNED_CERT' ||
    code === 'UNABLE_TO_VERIFY_LEAF_SIGNATURE' ||
    /certificate|handshake|\bTLS\b|\bSSL\b/i.test(message)
  ) return 'network'

  return 'unknown'
}

/**
 * Open a NEW SSH client and resolve once it is ready (or has definitively
 * failed). Does NOT tear down an existing client first — callers own that
 * decision, so `ensureTunnel` can join an in-flight attempt rather than
 * cancelling it.
 *
 * Always settles: every terminal path (ready / error / close / timeout /
 * synchronous throw from connect) resolves the promise exactly once.
 *
 * This is the SSH transport's `connect()` implementation and its body is
 * unchanged from the pre-transport version — it still owns the `client`
 * module state, the error classification and the ready-time rebuild.
 */
function openClient(host: string, port: number, username: string): Promise<boolean> {
  return new Promise((resolve) => {
    let settled = false
    let timer: ReturnType<typeof setTimeout> | undefined
    const done = (ok: boolean) => {
      if (settled) return
      settled = true
      if (timer) clearTimeout(timer)
      resolve(ok)
    }

    state.error = ''
    state.errorType = ''

    const c = new Client()
    // A client that errored without emitting 'close' can still be referenced
    // here (the single-flight guard means no connect is in flight, so anything
    // present is a zombie). Release its socket rather than leaking it. Its own
    // 'close' handler is identity-guarded, so it cannot tear down the new one.
    const stale = client
    client = c
    if (stale && stale !== c) {
      try { stale.end() } catch { /* already gone */ }
    }
    timer = setTimeout(() => {
      // Give up on this attempt: drop the reference and release the socket, or
      // a connection that completes after the deadline would pass the
      // `client === c` guard below and flip the module to "connected" for a
      // promise that already reported failure.
      if (client === c) {
        client = null
        state.connected = false
        state.error = state.error || 'connection timed out'
        try { c.end() } catch { /* ignore */ }
      }
      done(false)
    }, CONNECT_TIMEOUT_MS)

    c.on('ready', () => {
      // A superseded client (replaced by a newer connect) must not touch shared
      // state or bind listeners for a connection that is no longer current.
      if (client !== c) { done(false); return }
      state.connected = true
      state.error = ''
      state.errorType = ''
      reconnectAttempt = 0
      // The ready-time rebuild is NOT done here any more. It moved to
      // ensureTunnel()'s success path so that BOTH transports rebuild: h2's
      // connect() only flips state.connected, so a reconnect used to leave
      // every local listener dead while reporting success (see ensureTunnel).
      done(true)
    })
      // 'tcp connection' is not in @types/ssh2's Client overloads even though
      // ssh2 emits it for forwarded-tcpip channels, so the handler is attached
      // through a cast. The event is only emitted after forwardIn() registers a
      // matching forward.
      .on('tcp connection' as never, ((info: { destIP: string; destPort: number; origIP: string; origPort: number }, accept: () => any, reject: () => void) => {
        // The server accepted a connection on a port we asked it to forward and
        // is handing it to us. Dial the real target locally and splice.
        const entry = reverseForwards.get(info.destPort)
        if (!entry) {
          // No matching forward (e.g. a stale entry the server still holds).
          reject()
          return
        }
        const upstream = net.connect({ port: entry.targetPort, host: entry.host || '127.0.0.1', allowHalfOpen: true })
        upstream.on('connect', () => {
          const stream = accept()
          // Both ends need an 'error' handler and a half-close-aware splice —
          // see spliceDuplex. The upstream dial opts into allowHalfOpen for the
          // same reason the local listener does: without it, the target's FIN
          // would auto-destroy the socket and drop any bytes still owed to it.
          spliceDuplex(upstream, stream)
        })
        upstream.on('error', () => { reject() })
      }) as never)
      .on('error', (err: Error & { level?: string; code?: string }) => {
        if (client === c) {
          state.connected = false
          state.error = err.message
          state.errorType = classifyError(err)
        }
        done(false)
      })
      .on('close', () => {
        // Only the CURRENT client may clear state or close listeners: during a
        // reconnect the old client's close fires after the new one is already
        // live, and an unguarded teardown here would kill the new forwards.
        if (client === c) {
          client = null
          state.connected = false
          // Only the runtime listeners die here. The desired set is kept so the
          // monitor (or a caller) can rebuild it.
          closeAllForwardServers()
          // The server released its side of every reverse forward when the
          // connection dropped, so the bookkeeping is stale. Clearing it lets
          // rebuildAllForwards re-register cleanly instead of trying to
          // unforward ports the server no longer holds.
          reverseForwards.clear()
          // An unexpected drop: keep/arm the monitor so the forwards come back
          // on their own. disconnectTunnel() stops it for an explicit teardown.
          syncMonitor()
        }
        done(false)
      })

    try {
      c.connect({
        host,
        port,
        username,
        password: getPassword(),
        keepaliveInterval: KEEPALIVE_INTERVAL_MS,
        keepaliveCountMax: KEEPALIVE_COUNT_MAX,
      })
    } catch (err) {
      state.connected = false
      state.error = String((err as Error)?.message || err)
      state.errorType = 'unknown'
      done(false)
    }
  })
}

/**
 * The SSH transport: a thin adapter over this module's own ssh2 state.
 *
 * It is defined here rather than in `transport.ts` because every one of its
 * operations is an existing module primitive (openClient / the ssh2 client's
 * forwardOut / reverseForwards / state.error). Moving the state out would be a
 * far larger change than "wrap the transport", and the adapter's job is only
 * to present that state through the `TunnelTransport` shape.
 */
function createSshTransport(): TunnelTransport {
  return {
    connect(opts: TransportConnectOptions): Promise<boolean> {
      return openClient(opts.host, opts.port, opts.username || DEFAULT_SSH_USER)
    },
    openStream(host: string, port: number): Promise<import('node:stream').Duplex> {
      return new Promise((resolve, reject) => {
        const c = client
        if (!c) {
          reject(new Error('not connected'))
          return
        }
        c.forwardOut('127.0.0.1', 0, host || 'localhost', port, (err, stream) => {
          if (err) reject(err)
          else resolve(stream)
        })
      })
    },
    // The -R data plane is push-based over SSH: the client's 'tcp connection'
    // handler (installed in openClient) dials the local target and accepts the
    // channel directly, so there is no client-initiated claim stream.
    openClaimStream(): Promise<import('node:stream').Duplex> {
      return Promise.reject(new Error('claim streams are h2-only'))
    },
    openControlStream(): Promise<ControlStream> {
      return Promise.reject(new Error('control streams are h2-only'))
    },
    bind(serverPort: number): Promise<number | null> {
      return new Promise((resolve) => {
        const c = client
        if (!c || !state.connected) { resolve(null); return }
        c.forwardIn('127.0.0.1', serverPort, (err: Error | undefined, realPort?: number) => {
          if (err) { resolve(null); return }
          // The server may allocate a different port (we asked for 0, or it
          // remapped a privileged one).
          resolve(realPort || serverPort)
        })
      })
    },
    unbind(serverPort: number): Promise<void> {
      const c = client
      if (!c || !state.connected) return Promise.resolve()
      try {
        c.unforwardIn('127.0.0.1', serverPort, () => { /* best effort */ })
      } catch { /* connection already gone */ }
      return Promise.resolve()
    },
    close(): void {
      if (client) {
        try { client.end() } catch { /* ignore */ }
        client = null
      }
    },
    isConnected(): boolean {
      return state.connected && !!client
    },
    getLastError(): string {
      return state.error
    },
  }
}

/**
 * The cached SSH adapter. Rebuilt when it no longer wraps a live client, so a
 * reconnect after a full teardown (`disconnectTunnel` nulls `client`) creates
 * a fresh adapter instead of keeping a stale one.
 */
function getSshTransport(): TunnelTransport {
  if (!sshTransport || !client) sshTransport = createSshTransport()
  return sshTransport
}

/**
 * Tear down the transport and every live listener, but KEEP the desired
 * forward set. Keeping it is what lets a reconnect restore the user's ports —
 * clearing it here silently dropped every mapping.
 */
export function disconnectTunnel(): void {
  // An explicit teardown means "stop maintaining this" — only an unexpected
  // close should leave the monitor running to recover.
  stopMonitor()
  // Unforward reverse mappings while the connection is still alive, so the
  // server releases its listeners promptly. Dropping the connection alone
  // would also free them, but only once the server notices.
  unforwardAllReverse()
  // close() is the transport-agnostic teardown: the SSH adapter ends the ssh2
  // client, the h2 adapter closes the control stream and destroys the session.
  if (transport) {
    try { transport.close() } catch { /* ignore */ }
    transport = null
  }
  if (client) {
    try { client.end() } catch { /* ignore */ }
    client = null
  }
  state.connected = false
  // Close every listener, not just the map entries — otherwise the local ports
  // stay bound after a disconnect/reconnect cycle.
  closeAllForwardServers()
}

/**
 * Splice a local socket to a tunnel stream, preserving TCP half-close.
 *
 * `net.createServer` defaults to `allowHalfOpen: false`, which destroys the
 * whole socket the moment the local peer sends FIN. The target's reply then has
 * nowhere to land: a request/response where the client half-closes after the
 * request lost the entire response (measured: expected a trailer, received "").
 * The listener below therefore opts in to `allowHalfOpen: true`.
 *
 * That option moves the closing responsibility to us: with the default Node
 * destroyed the socket for us, but with it on an accepted socket whose peer is
 * gone stays open until BOTH directions end. A plain `pipe()` chain cannot
 * express that — pipe propagates 'end' but never 'close' — so the fd leak this
 * option classically causes is closed explicitly here:
 *
 *   - either side reaching 'close' (a clean FIN-after-FIN, an RST/ECONNRESET,
 *     or a remote destroy) destroys the other side;
 *   - an 'error' on either side tears both down.
 *
 * `pipe()` still carries the bytes and the half-close itself: a local FIN ends
 * the stream's writable side (END_STREAM on h2, CloseWrite on the SSH channel)
 * while the readable side keeps delivering the target's reply. Only the final
 * reclamation is ours. Both ends need the 'error' handler: a bare pipe chain
 * does not forward errors, so an ECONNRESET from either side (the tunnel
 * channel being torn down mid-transfer is the common case) becomes an UNCAUGHT
 * exception and takes the whole main process down with Electron's
 * "A JavaScript error occurred in the main process" dialog.
 */
function spliceDuplex(local: Duplex, remote: Duplex): void {
  local.on('close', () => { if (!remote.destroyed) remote.destroy() })
  remote.on('close', () => { if (!local.destroyed) local.destroy() })
  local.on('error', () => { local.destroy(); remote.destroy() })
  remote.on('error', () => { remote.destroy(); local.destroy() })
  local.pipe(remote).pipe(local)
}

/**
 * Bind localhost:localPort and pipe each accepted socket through the SSH
 * channel to host:targetPort.
 *
 * Per-port single-flight: `listen()` is asynchronous, so two concurrent binds
 * for the same port would both reach the OS; the loser's EADDRINUSE handler
 * used to `closeForwardServer(localPort)`, which destroyed the WINNER's entry
 * and left the port unreachable while `state.forwarded` still claimed it was
 * forwarded. Joining the in-flight bind instead makes repeated adds idempotent.
 */
const pendingBinds = new Map<number, Promise<boolean>>()

function listenForward(localPort: number, targetPort: number, host: string): Promise<boolean> {
  const inFlight = pendingBinds.get(localPort)
  if (inFlight) return inFlight
  closeForwardServer(localPort)
  const p = new Promise<boolean>((resolve) => {
    const t = transport
    if (!t || !t.isConnected()) { resolve(false); return }
    // allowHalfOpen: see spliceDuplex — without it the local FIN destroys the
    // socket and the target's response is lost.
    const server = net.createServer({ allowHalfOpen: true }, (socket) => {
      // Re-read the transport per connection: a reconnect may have replaced it
      // since the listener was created (the listener itself survives, since
      // only the SSH/h2 channel died).
      const cur = transport
      if (!cur || !cur.isConnected()) { socket.destroy(); return }
      cur.openStream(host, targetPort).then((stream) => {
        spliceDuplex(socket, stream)
      }).catch(() => {
        // The stream could not be opened (dial failed, port not allowed,
        // unauthenticated): drop the local socket instead of piping it into
        // nothing.
        socket.destroy()
      })
    })
    server.listen(localPort, '127.0.0.1', () => {
      // A removeForwardedPort() landing during listen() must win: publishing
      // here would revive a port the user just deleted, with no desired entry.
      if (!state.forwarded.has(localPort)) {
        try { server.close() } catch { /* ignore */ }
        resolve(false)
        return
      }
      forwardServers.set(localPort, server)
      resolve(true)
    })
    server.on('error', () => {
      // Release only THIS server, never a different (winning) one.
      if (forwardServers.get(localPort) === server) forwardServers.delete(localPort)
      try { server.close() } catch { /* ignore */ }
      resolve(false)
    })
  }).finally(() => { pendingBinds.delete(localPort) })
  pendingBinds.set(localPort, p)
  return p
}

/**
 * Ask the server to bind `serverPort` on its loopback and hand connections back
 * to us; each one is then spliced to `host:targetPort` on this machine.
 *
 * Unlike listenForward there is no local socket: the server owns the listener,
 * so success means the server acknowledged the bind.
 *
 * The two transports hand connections back differently. SSH pushes them (the
 * client's 'tcp connection' handler accepts the channel and dials the target
 * directly — see createSshTransport). h2 cannot open a server-initiated stream
 * at all (RFC 9113 §8.4), so the server parks each accepted connection and
 * sends `incoming` with a single-use token on the control stream; the client
 * must redeem it with a claim stream. `wireH2Incoming` below is that half.
 *
 * Per-port single-flight for the same reason as listenForward: two concurrent
 * requests for one port would race, and the loser would clobber the winner's
 * bookkeeping.
 */
const pendingReverseBinds = new Map<number, Promise<boolean>>()

function listenReverse(serverPort: number, targetPort: number, host: string): Promise<boolean> {
  const inFlight = pendingReverseBinds.get(serverPort)
  if (inFlight) return inFlight
  const p = new Promise<boolean>((resolve) => {
    const t = transport
    if (!t || !t.isConnected()) { resolve(false); return }
    // h2 must have the control stream (and its `incoming` handler) alive BEFORE
    // the bind is requested: a connection arriving right after `bound` is
    // parked and announced with a single-use token, and a token nobody is
    // listening for is simply lost. SSH has no control stream, so the prelude
    // is a no-op there and its behavior is untouched — `null` rather than a
    // resolved promise, so the SSH bind stays synchronous (deferring it by a
    // microtask would change when forwardIn() is issued).
    const pre = t === h2Transport ? ensureControlStream() : null
    const boundP = pre ? pre.then(() => t.bind(serverPort)) : t.bind(serverPort)
    boundP.then((bound) => {
      if (bound === null) {
        resolve(false)
        return
      }
      // A remove landing during the request must win: publishing here would
      // revive a mapping the user just deleted.
      if (!state.forwarded.has(serverPort)) {
        void t.unbind(bound)
        resolve(false)
        return
      }
      reverseForwards.set(serverPort, { targetPort, host, serverPort: bound })
      resolve(true)
    }).catch(() => resolve(false))
  }).finally(() => { pendingReverseBinds.delete(serverPort) })
  pendingReverseBinds.set(serverPort, p)
  return p
}

// ---------------------------------------------------------------------------
// h2 reverse data plane (-R)
// ---------------------------------------------------------------------------

/**
 * The single -R control stream, cached for the same reason the transport
 * adapter caches it: two control streams would mean two competing sets of
 * bind bookkeeping on the server.
 */
let controlStream: ControlStream | null = null

/**
 * The h2 `-R` data plane: an `incoming` control message names a server-bound
 * port and carries a single-use token; redeeming the token with a claim stream
 * yields the parked connection, which is then spliced to the local target
 * exactly like the SSH path's `accept()` branch.
 */
function wireH2Incoming(msg: ControlMessage): void {
  if (msg.type !== 'incoming' || typeof msg.port !== 'number' || !msg.token) return
  const entry = reverseForwards.get(msg.port)
  if (!entry) {
    // No matching forward: the token simply expires (the server closes the
    // parked connection on timeout). Nothing to reject — there is no stream.
    return
  }
  const t = transport
  if (!t) return

  const upstream = net.connect({ port: entry.targetPort, host: entry.host || '127.0.0.1', allowHalfOpen: true })
  upstream.on('connect', () => {
    t.openClaimStream(msg.token as string).then((stream) => {
      // Half-close-aware splice, same as the SSH accept() branch above.
      spliceDuplex(upstream, stream)
    }).catch(() => {
      // Claim failed (expired token, stream refused): drop the local dial.
      upstream.destroy()
    })
  })
  upstream.on('error', () => { /* target unreachable — nothing to splice */ })
}

/**
 * Ensure the control stream exists and route `incoming` notifications.
 *
 * Kept separate from `listenReverse` because a tunnel may carry several
 * reverse ports over ONE control stream, and re-registering the handler per
 * bind would deliver every `incoming` several times (one duplicate claim per
 * extra registration, each racing for the single-use token).
 */
function ensureControlStream(): Promise<ControlStream | null> {
  if (controlStream && !controlStream.closed) return Promise.resolve(controlStream)
  const t = transport
  if (!t) return Promise.resolve(null)
  return t.openControlStream().then((cs) => {
    controlStream = cs
    cs.onMessage(wireH2Incoming)
    cs.onClose(() => {
      if (controlStream === cs) controlStream = null
    })
    return cs
  }).catch(() => null)
}

/**
 * Re-bind every desired forward. Called after the client becomes ready.
 *
 * The snapshot is taken up front and the awaits yield, so a
 * removeForwardedPort() can land mid-rebuild; listenForward() re-checks
 * `state.forwarded` at publish time and refuses to bind a deleted port.
 */
async function rebuildAllForwards(): Promise<void> {
  for (const [key, fwd] of [...state.forwarded.entries()]) {
    if (fwd.direction === 'reverse') {
      await listenReverse(key, fwd.targetPort, fwd.host)
    } else {
      await listenForward(key, fwd.targetPort, fwd.host)
    }
  }
}

/** Add a local port forward: localhost:localPort → host:targetPort via the SSH channel. */
export async function addForwardedPort(localPort: number, targetPort: number, host: string): Promise<boolean> {
  if (!state.connected) {
    const ok = await ensureTunnel()
    if (!ok) return false
  }
  // Record the intent BEFORE binding, so a concurrent reconnect's
  // rebuildAllForwards() can pick it up even if this call loses the race.
  state.forwarded.set(localPort, { targetPort, host, direction: 'forward' })
  // There is now something worth keeping alive.
  syncMonitor()
  return listenForward(localPort, targetPort, host)
}

/**
 * Add a reverse forward: expose this machine's host:targetPort on the server's
 * loopback `serverPort`.
 */
export async function addReverseForwardedPort(serverPort: number, targetPort: number, host: string): Promise<boolean> {
  if (!state.connected) {
    const ok = await ensureTunnel()
    if (!ok) return false
  }
  state.forwarded.set(serverPort, { targetPort, host, direction: 'reverse' })
  syncMonitor()
  return listenReverse(serverPort, targetPort, host)
}

export function removeForwardedPort(localPort: number): void {
  const entry = state.forwarded.get(localPort)
  if (entry?.direction === 'reverse') {
    unforwardReverse(localPort)
  } else {
    closeForwardServer(localPort)
  }
  state.forwarded.delete(localPort)
  // Nothing left to maintain → stop polling.
  syncMonitor()
}

/**
 * Remove a reverse forward. `serverPort` is the port the SERVER bound, which is
 * the key the mapping was registered under.
 */
export function removeReverseForwardedPort(serverPort: number): void {
  removeForwardedPort(serverPort)
}

export function testPortReachable(localPort: number): Promise<boolean> {
  return new Promise((resolve) => {
    const sock = net.connect({ host: '127.0.0.1', port: localPort })
    sock.on('connect', () => { sock.destroy(); resolve(true) })
    sock.on('error', () => resolve(false))
    setTimeout(() => { sock.destroy(); resolve(false) }, 500)
  })
}

const DEFAULT_SSH_USER = 'clawbench'

interface SshInfo {
  enabled: boolean
  port: number
  username: string
}

/** Fetch SSH connection info from the server's public /api/ssh/info endpoint. */
function fetchSshInfo(serverUrl: string): Promise<SshInfo | null> {
  return new Promise((resolve) => {
    try {
      const url = new URL(serverUrl)
      const scheme = url.protocol.replace(':', '')
      const httpPort = url.port || (scheme === 'https' ? '443' : '80')
      const infoUrl = `${scheme}://${url.hostname}:${httpPort}/api/ssh/info`
      const lib = scheme === 'https' ? https : http
      const req = lib.get(infoUrl, { rejectUnauthorized: false }, (res) => {
        let data = ''
        res.on('data', c => { data += c })
        res.on('end', () => {
          try {
            const j = JSON.parse(data)
            resolve({ enabled: !!j.enabled, port: j.port || -1, username: j.username || DEFAULT_SSH_USER })
          } catch { resolve(null) }
        })
      })
      req.on('error', () => resolve(null))
    } catch { resolve(null) }
  })
}

/**
 * Try one transport, updating the shared connection state on success or
 * recording the failure. `applyError` lets each transport report a failure
 * through the shape it has: SSH already wrote `state.error`/`state.errorType`
 * in its own handlers (classifying there is the only place the ssh2 error
 * object exists), while h2 only hands back a message.
 */
async function attemptTransport(
  kind: 'ssh' | 'h2',
  opts: TransportConnectOptions,
  applyError: (t: TunnelTransport) => void,
): Promise<boolean> {
  const t = kind === 'h2' ? getH2Transport() : getSshTransport()
  transport = t
  const ok = await t.connect(opts)
  if (!ok) {
    state.connected = false
    applyError(t)
    // A failed attempt must not leave a half-open transport behind: the next
    // candidate replaces `transport`, and an unclosed h2 session would keep
    // its socket (and its server-side control stream) alive.
    try { t.close() } catch { /* ignore */ }
    transport = null
    return false
  }
  state.connected = true
  state.error = ''
  state.errorType = ''
  reconnectAttempt = 0
  activeTransport = kind
  if (kind === 'h2') {
    // Remember the wire kind that worked so a plaintext deployment does not
    // pay a TLS rejection on every reconnect (design doc §2.3).
    const k = h2Transport?.getKind()
    if (k) lastH2Kind = k
  }
  return true
}

/**
 * Establish the tunnel if not already connected. Returns true when connected.
 *
 * Single-flight: a second caller arriving during a connect joins the in-flight
 * attempt instead of starting its own. Two concurrent connects would otherwise
 * replace each other's transport, leaving the first caller's promise pending
 * forever (the original hang that killed every forward).
 *
 * Dispatch (design doc §2.3): SSH-only and h2-only are single attempts; `both`
 * probes h2 first and falls back to SSH. Only the SSH path needs the
 * `/api/ssh/info` prelude (the SSH port and username) — h2 uses the main URL's
 * own host and port.
 *
 * Rebuilding every desired forward is done HERE, once, for both transports
 * (see the success path below). It used to live in the ssh2 `'ready'` handler,
 * which meant h2 — whose `connect()` only flips `state.connected` — never
 * rebuilt: `reconnectTunnel()` (disconnect + reconnect) reported success and
 * `isTunnelConnected()` agreed, while every local listener stayed closed. The
 * UI's own `testPortReachable` follow-up then failed and told the user the
 * reconnect had failed, contradicting `ok === true`.
 */
export function ensureTunnel(): Promise<boolean> {
  if (state.connected && transport && transport.isConnected()) return Promise.resolve(true)
  if (connecting) return connecting
  const p = (async () => {
    const serverUrl = getStore().get('serverUrl')
    if (!serverUrl) return false
    let url: URL
    try { url = new URL(serverUrl) } catch { return false }

    const candidates = transportCandidates()

    // SSH connection info is fetched lazily: an h2-first attempt should not
    // pay for (or depend on) an endpoint only the SSH fallback needs.
    let sshOpts: TransportConnectOptions | null = null
    const loadSshOpts = async (): Promise<TransportConnectOptions> => {
      if (sshOpts) return sshOpts
      const info = await fetchSshInfo(serverUrl)
      const sshPort = info && info.enabled && info.port > 0 ? info.port : Number(url.port || 80) + 1
      sshOpts = { host: url.hostname, port: sshPort, username: info?.username || DEFAULT_SSH_USER }
      return sshOpts
    }

    for (const kind of candidates) {
      let ok = false
      if (kind === 'ssh') {
        const opts = await loadSshOpts()
        // openClient() populates state.error/errorType itself; only the
        // "no client was even created" path leaves them empty.
        ok = await attemptTransport('ssh', opts, () => {
          if (!state.error) {
            state.error = 'SSH connection failed'
            state.errorType = 'unknown'
          }
        })
      } else {
        const port = Number(url.port || (url.protocol === 'https:' ? 443 : 80))
        const opts: TransportConnectOptions = { host: url.hostname, port, ...(lastH2Kind ? { prefer: lastH2Kind } : {}) }
        ok = await attemptTransport('h2', opts, (t) => {
          state.error = t.getLastError() || 'h2 connection failed'
          state.errorType = classifyError(Object.assign(new Error(state.error), { code: h2ErrorCode(state.error) }))
        })
      }
      if (!ok) continue

      // Single success sink: rebuild the listeners on the fresh channel for
      // EVERY transport. This is what makes a reconnect (and a startup sync)
      // actually restore reachability — `listenForward`/`listenReverse` are
      // idempotent (single-flight + close-before-bind), so re-running this
      // never duplicates a listener or leaks one. Best-effort: a rebuild
      // failure must not turn a live connection into a reported failure, which
      // is what the ssh2 `'ready'` handler did with `.catch(() => done(true))`.
      try { await rebuildAllForwards() } catch { /* best effort — the tunnel is up */ }
      return true
    }

    return false
  })().finally(() => { connecting = null })
  connecting = p
  return p
}

/**
 * Best-effort extraction of a stable error code from an h2 failure message.
 *
 * T6's connect() reports a human-readable string (its internal `fail()` is
 * handed `err.code || err.message`), so the code is usually embedded verbatim.
 * Recovering it lets classifyError() use its exact-code branches instead of
 * having to pattern-match prose.
 */
function h2ErrorCode(message: string): string | undefined {
  const m = /\b(ERR_HTTP2_[A-Z_]+|ERR_TLS_[A-Z_]+|ECONNREFUSED|ECONNRESET|ETIMEDOUT|ENOTFOUND|EPIPE)\b/.exec(message)
  return m ? m[1] : undefined
}

/**
 * Reconnect the tunnel, preserving and rebuilding every desired forward.
 * Resolves true only once the forwards are bound again, so a caller that
 * immediately probes a local port sees it reachable.
 */
export async function reconnectTunnel(): Promise<boolean> {
  // Let an in-flight attempt finish first: disconnectTunnel() below would
  // otherwise strand it, and we would return that dead attempt's result.
  if (connecting) {
    try { await connecting } catch { /* ignore — we are reconnecting anyway */ }
  }
  disconnectTunnel()
  return ensureTunnel()
}

/**
 * Test helper — re-run the forward rebuild directly, so its idempotency (no
 * duplicate listeners, no leak) can be asserted without contriving a reconnect.
 */
export function _rebuildAllForwardsForTesting(): Promise<void> {
  return rebuildAllForwards()
}

/**
 * Test helper — reset the transport-selection state so each case starts from a
 * clean slate. Module-level state otherwise leaks across tests (the same reason
 * h2Transport has `_resetForTesting()`): a remembered h2 kind or a cached
 * adapter would make the next test's assertions order-dependent.
 */
export function _resetTransportForTesting(): void {
  disconnectTunnel()
  transportPreference = 'ssh'
  activeTransport = 'ssh'
  lastH2Kind = null
  sshTransport = null
  h2Transport = null
  controlStream = null
}
