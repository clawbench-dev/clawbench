import net from 'node:net'
import http from 'node:http'
import https from 'node:https'
import { Client } from 'ssh2'
import { getPassword } from './secrets'
import { getStore } from './store'

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
 * reconnect (with backoff) so `openClient`'s ready handler rebuilds them.
 */
async function monitorTick(): Promise<void> {
  if (state.forwarded.size === 0) { stopMonitor(); return }
  if (state.connected && client) { reconnectAttempt = 0; return }
  if (connecting) return // an attempt is already in flight; let it finish

  if (reconnectAttempt > 0) {
    const delay = RECONNECT_DELAYS_MS[Math.min(reconnectAttempt - 1, RECONNECT_DELAYS_MS.length - 1)]
    if (Date.now() - lastAttemptAt < delay) return
  }
  reconnectAttempt++
  lastAttemptAt = Date.now()
  // On success the ready handler runs rebuildAllForwards(), restoring every
  // desired port; failures are retried on subsequent ticks with longer delays.
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
 * differ (the server allocated one when we asked for 0), so unforwardIn must use
 * the recorded `serverPort`.
 */
function unforwardReverse(key: number): void {
  const entry = reverseForwards.get(key)
  reverseForwards.delete(key)
  if (!entry || !client || !state.connected) return
  try {
    client.unforwardIn('127.0.0.1', entry.serverPort, () => { /* best effort */ })
  } catch { /* connection already gone */ }
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

function classifyError(err: Error & { level?: string; code?: string }): TunnelErrorType {
  if (err.level === 'client-authentication') return 'auth'
  if (err.code === 'ENOTFOUND' || err.code === 'ECONNREFUSED' || err.code === 'ETIMEDOUT') return 'network'
  if (err.level === 'client-timeout') return 'hostkey'
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
      // Rebuild every desired forward on the fresh channel. This is what makes
      // a reconnect (and a startup sync) actually restore reachability.
      rebuildAllForwards()
        .then(() => done(true))
        .catch(() => done(true))
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
        const upstream = net.connect(entry.targetPort, entry.host || '127.0.0.1')
        upstream.on('connect', () => {
          const stream = accept()
          // Both ends need an 'error' handler: a bare pipe() chain does not
          // forward errors, so a reset would become an uncaught exception in the
          // main process (same trap as listenForward).
          upstream.on('error', () => { upstream.destroy(); stream.destroy() })
          stream.on('error', () => { stream.destroy(); upstream.destroy() })
          upstream.pipe(stream).pipe(upstream)
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
 * Tear down the SSH client and every live listener, but KEEP the desired
 * forward set. Keeping it is what lets a reconnect restore the user's ports —
 * clearing it here silently dropped every mapping.
 */
export function disconnectTunnel(): void {
  // An explicit teardown means "stop maintaining this" — only an unexpected
  // close should leave the monitor running to recover.
  stopMonitor()
  // Unforward reverse mappings while the client is still alive, so the server
  // releases its listeners promptly. Dropping the connection alone would also
  // free them, but only once the server notices.
  unforwardAllReverse()
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
    if (!client || !state.connected) { resolve(false); return }
    const server = net.createServer((socket) => {
      const c = client
      if (!c) { socket.destroy(); return }
      c.forwardOut('127.0.0.1', 0, host || 'localhost', targetPort, (err, stream) => {
        if (err) { socket.destroy(); return }
        // Both ends need an 'error' handler. A bare `pipe()` chain does not
        // forward errors, so an ECONNRESET from either side (the SSH channel
        // being torn down mid-transfer is the common case) becomes an
        // UNCAUGHT exception and takes the whole main process down with
        // Electron's "A JavaScript error occurred in the main process" dialog.
        // Destroying the peer on error is also what makes the pipe clean up.
        socket.on('error', () => { socket.destroy(); stream.destroy() })
        stream.on('error', () => { stream.destroy(); socket.destroy() })
        socket.pipe(stream).pipe(socket)
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
 * so success means the server acknowledged the tcpip-forward request.
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
    const c = client
    if (!c || !state.connected) { resolve(false); return }
    c.forwardIn('127.0.0.1', serverPort, (err: Error | undefined, realPort?: number) => {
      if (err) {
        resolve(false)
        return
      }
      // The server may allocate a different port (we asked for 0, or it
      // remapped a privileged one), and unforwardIn must use the real one.
      const bound = realPort || serverPort
      // A remove landing during the request must win: publishing here would
      // revive a mapping the user just deleted.
      if (!state.forwarded.has(serverPort)) {
        try { c.unforwardIn('127.0.0.1', bound, () => { /* ignore */ }) } catch { /* ignore */ }
        resolve(false)
        return
      }
      reverseForwards.set(serverPort, { targetPort, host, serverPort: bound })
      resolve(true)
    })
  }).finally(() => { pendingReverseBinds.delete(serverPort) })
  pendingReverseBinds.set(serverPort, p)
  return p
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
 * Establish the SSH tunnel if not already connected. Returns true when connected.
 *
 * Single-flight: a second caller arriving during a connect joins the in-flight
 * attempt instead of starting its own. Two concurrent connects would otherwise
 * replace each other's client, leaving the first caller's promise pending
 * forever (the original hang that killed every forward).
 */
export function ensureTunnel(): Promise<boolean> {
  if (state.connected && client) return Promise.resolve(true)
  if (connecting) return connecting
  const p = (async () => {
    const serverUrl = getStore().get('serverUrl')
    if (!serverUrl) return false
    let url: URL
    try { url = new URL(serverUrl) } catch { return false }

    const info = await fetchSshInfo(serverUrl)
    const sshPort = info && info.enabled && info.port > 0 ? info.port : Number(url.port || 80) + 1
    const username = info?.username || DEFAULT_SSH_USER
    return openClient(url.hostname, sshPort, username)
  })().finally(() => { connecting = null })
  connecting = p
  return p
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
