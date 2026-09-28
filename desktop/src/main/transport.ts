import type { Duplex } from 'node:stream'
import * as h2 from './h2Transport'
import type { ControlMessage, ControlStream, H2TransportKind } from './h2Transport'

export type { ControlMessage, ControlStream, H2TransportKind } from './h2Transport'

/**
 * The tunnel transport abstraction (design doc §7.1).
 *
 * The desktop tunnel used to be hard-wired to ssh2: every forwarded TCP
 * connection was an SSH channel, and the -R data plane arrived on the client's
 * `'tcp connection'` event. The HTTP/2 stream tunnel carries the same traffic
 * as one h2 stream per connection, over the main port only. Both are driven
 * through this shape so `tunnel.ts` keeps its bookkeeping, listeners, single
 * flight and reconnect monitor exactly as they were.
 *
 * Two implementations exist:
 *   - the ssh2 one, which lives in `tunnel.ts` because it is a thin adapter
 *     over that module's own state (client, error classification, rebuild);
 *   - the h2 one, `createH2TunnelTransport()` below, wrapping `./h2Transport`.
 *
 * The interface is defined here (rather than inside `tunnel.ts`) so the h2
 * adapter — which must not import `tunnel.ts`, or the two would form an import
 * cycle — can share the type. The interface is type-only: this module has no
 * dependency on `tunnel.ts`.
 */

/**
 * Which transport the tunnel prefers. The three values are kept for the
 * `both` fallback (`['h2', 'ssh']`, design doc §2.3) and for the tests that
 * exercise it; in production, however, Electron is hard-wired to SSH —
 * `bridge.ts` only lets the literal `'ssh'` through to
 * `setTransportPreference()`, so the server's `port_forward.transport` value is
 * no longer consumed here.
 */
export type TransportPreference = 'ssh' | 'h2' | 'both'

export interface TransportConnectOptions {
  /** Server host, e.g. `127.0.0.1`. */
  host: string
  /**
   * Server port. The SSH transport uses the SSH port (from `/api/ssh/info`,
   * defaulting to mainPort+1); h2 uses the main HTTP port.
   */
  port: number
  /** SSH only: the username advertised by `/api/ssh/info`. */
  username?: string
  /**
   * h2 only: which wire transport to try first (h2-over-TLS vs h2c). The
   * caller remembers the kind that worked last and passes it back, so a
   * plaintext deployment does not pay a TLS rejection on every reconnect.
   */
  prefer?: H2TransportKind
}

/** One pluggable tunnel transport. */
export interface TunnelTransport {
  /** Establish (or reuse) the underlying connection. Resolves true on success. */
  connect(opts: TransportConnectOptions): Promise<boolean>
  /** `-L`: dial one stream to `host:port` on the server side. */
  openStream(host: string, port: number): Promise<Duplex>
  /** `-R`: open the data stream that redeems an `incoming` claim token. */
  openClaimStream(token: string): Promise<Duplex>
  /** `-R`: the long-lived NDJSON control stream. */
  openControlStream(): Promise<ControlStream>
  /** `-R`: ask the server to bind `serverPort`; resolves the actual port, or null. */
  bind(serverPort: number): Promise<number | null>
  /** `-R`: release a server-side bind. Best-effort, mirrors ssh2's unforwardIn. */
  unbind(serverPort: number): Promise<void>
  /** Tear the connection down. Idempotent. */
  close(): void
  isConnected(): boolean
  /**
   * Human-readable reason the last `connect()` failed ('' when it succeeded).
   *
   * `connect()` reports success as a boolean, so without this the caller has no
   * way to populate `state.error` / `state.errorType` for the UI. The ssh2
   * implementation answers with `state.error`, which its own handlers already
   * maintain; the h2 adapter records T6's `{ ok, kind, error }` message.
   */
  getLastError(): string
}

/** The h2 transport additionally reports which wire kind carried the session. */
export interface H2TunnelTransport extends TunnelTransport {
  getKind(): H2TransportKind | null
}

/**
 * Wrap the node:http2 transport (`./h2Transport`) in the `TunnelTransport`
 * shape.
 *
 * The adapter owns the two pieces of state that must outlive a single call but
 * do not belong in `tunnel.ts`:
 *
 *   - the single -R control stream. `h2Transport.openControlStream()` opens a
 *     NEW stream on every call, and two control streams would mean two
 *     competing sets of bind bookkeeping, so it is cached here. The caller may
 *     subscribe to the returned stream as well — this adapter's own
 *     subscription consumes only `bound`/`bind_err`.
 *   - the queue of in-flight bind replies.
 */
export function createH2TunnelTransport(): H2TunnelTransport {
  let control: ControlStream | null = null
  let lastError = ''
  /**
   * Bind replies, oldest first. The server's control loop is sequential and
   * writes `bound`/`bind_err` before reading the next command, so replies
   * arrive in request order. A queue (rather than a port-keyed map) is
   * mandatory: a `bind(0)` is answered with the OS-assigned port, so that reply
   * carries no key to correlate on.
   */
  const bindWaiters: Array<(port: number | null) => void> = []

  function settleBind(port: number | null): void {
    bindWaiters.shift()?.(port)
  }

  function route(msg: ControlMessage): void {
    if (msg.type === 'bound') settleBind(typeof msg.port === 'number' ? msg.port : null)
    else if (msg.type === 'bind_err') settleBind(null)
  }

  async function openControlStream(): Promise<ControlStream> {
    if (control && !control.closed) return control
    const cs = await h2.openControlStream()
    control = cs
    cs.onMessage(route)
    cs.onClose(() => {
      if (control === cs) control = null
      // A control stream that ended can never answer a pending bind; settling
      // with null keeps listenReverse() from hanging on a dead tunnel (which
      // would also poison pendingReverseBinds for that port forever).
      while (bindWaiters.length > 0) bindWaiters.shift()!(null)
    })
    return cs
  }

  return {
    async connect(opts: TransportConnectOptions): Promise<boolean> {
      lastError = ''
      // T6's connect() owns the TLS -> h2c fallback and the single-flight guard.
      const res = await h2.connect({ host: opts.host, port: opts.port, prefer: opts.prefer })
      if (!res.ok) {
        lastError = res.error
        return false
      }
      return true
    },
    openStream(host: string, port: number): Promise<Duplex> {
      return h2.openStream(host, port)
    },
    openClaimStream(token: string): Promise<Duplex> {
      return h2.openClaimStream(token)
    },
    openControlStream,
    bind(serverPort: number): Promise<number | null> {
      return openControlStream().then((cs) => new Promise<number | null>((resolve) => {
        bindWaiters.push(resolve)
        cs.send({ type: 'bind', port: serverPort })
      }))
    },
    unbind(serverPort: number): Promise<void> {
      // Fire-and-forget, mirroring ssh2's unforwardIn callback: the caller
      // (removeForwardedPort) is synchronous, and the server's `unbound`
      // acknowledgement carries nothing the caller needs to wait for.
      const cs = control
      if (cs && !cs.closed) cs.send({ type: 'unbind', port: serverPort })
      return Promise.resolve()
    },
    close(): void {
      while (bindWaiters.length > 0) bindWaiters.shift()!(null)
      const cs = control
      control = null
      if (cs) {
        try { cs.close() } catch { /* already gone */ }
      }
      h2.close()
    },
    isConnected(): boolean {
      return h2.isConnected()
    },
    getLastError(): string {
      return lastError
    },
    getKind(): H2TransportKind | null {
      return h2.getTransportKind()
    },
  }
}
