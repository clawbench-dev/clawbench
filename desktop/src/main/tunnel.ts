import net from 'node:net'
import http from 'node:http'
import https from 'node:https'
import { Client } from 'ssh2'
import { getPassword } from './secrets'
import { getStore } from './store'

export type TunnelErrorType = 'auth' | 'network' | 'hostkey' | 'unknown' | ''

export interface TunnelState {
  connected: boolean
  error: string
  errorType: TunnelErrorType
  /**
   * The DESIRED set of forwards, keyed by local port. This is intent, not
   * runtime state: it survives a disconnect so a reconnect can rebuild every
   * listener (Android's BackgroundService does the same — its
   * `disconnectInternal()` deliberately keeps `forwardedPorts`).
   *
   * The live listeners live in `forwardServers` below.
   */
  forwarded: Map<number, { targetPort: number; host: string }>
}

const state: TunnelState = { connected: false, error: '', errorType: '', forwarded: new Map() }
let client: Client | null = null

// Upper bound on a single connect attempt. Without it a connect that never
// emits `ready` or `error` (dropped SYN, half-open peer) would leave the
// returned promise pending forever, and every caller awaiting it — notably
// addForwardedPort — would hang with no listener ever bound.
const CONNECT_TIMEOUT_MS = 20000

// The listening net.Server for each forwarded local port. Kept alongside
// `state.forwarded` because removing a forward must CLOSE the listener — the
// map entry alone is bookkeeping, and dropping it without closing leaves the
// local port bound forever (a leak that also makes re-adding the same port
// fail with EADDRINUSE).
const forwardServers = new Map<number, net.Server>()

// In-flight connect attempt, if any. Callers that arrive while a connect is
// running must join it instead of starting a second one: openClient replaces
// the module's client, so a second attempt would strand the first caller's
// promise (see ensureTunnel).
let connecting: Promise<boolean> | null = null

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

export function isTunnelConnected(): boolean { return state.connected }
export function getTunnelError(): string { return state.error }
export function getTunnelErrorType(): TunnelErrorType { return state.errorType }
export function getForwardedPorts(): Array<{ port: number; host: string }> {
  return [...state.forwarded.entries()].map(([localPort, v]) => ({ port: localPort, host: v.host }))
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
      if (client === c) {
        state.connected = false
        state.error = state.error || 'connection timed out'
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
      // Rebuild every desired forward on the fresh channel. This is what makes
      // a reconnect (and a startup sync) actually restore reachability.
      rebuildAllForwards()
        .then(() => done(true))
        .catch(() => done(true))
    })
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
          // next ensureTunnel() can rebuild it.
          closeAllForwardServers()
        }
        done(false)
      })

    try {
      c.connect({ host, port, username, password: getPassword() })
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
 * channel to host:targetPort. Idempotent per local port: an existing listener
 * is released first, or listen() fails with EADDRINUSE.
 */
function listenForward(localPort: number, targetPort: number, host: string): Promise<boolean> {
  closeForwardServer(localPort)
  return new Promise((resolve) => {
    if (!client || !state.connected) { resolve(false); return }
    const server = net.createServer((socket) => {
      const c = client
      if (!c) { socket.destroy(); return }
      c.forwardOut('127.0.0.1', 0, host || 'localhost', targetPort, (err, stream) => {
        if (err) { socket.destroy(); return }
        socket.pipe(stream).pipe(socket)
      })
    })
    server.listen(localPort, '127.0.0.1', () => {
      forwardServers.set(localPort, server)
      resolve(true)
    })
    server.on('error', () => {
      // A failed listen must not leave a half-registered listener behind.
      closeForwardServer(localPort)
      resolve(false)
    })
  })
}

/** Re-bind every desired forward. Called after the client becomes ready. */
async function rebuildAllForwards(): Promise<void> {
  for (const [localPort, fwd] of [...state.forwarded.entries()]) {
    await listenForward(localPort, fwd.targetPort, fwd.host)
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
  state.forwarded.set(localPort, { targetPort, host })
  return listenForward(localPort, targetPort, host)
}

export function removeForwardedPort(localPort: number): void {
  closeForwardServer(localPort)
  state.forwarded.delete(localPort)
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
