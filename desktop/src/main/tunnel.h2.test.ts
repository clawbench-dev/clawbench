import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import net from 'node:net'

/**
 * The h2 transport suite for `tunnel.ts` (design doc §10.2 / implementation T7).
 *
 * `./h2Transport` is mocked (T6's own suite covers its wire behavior); the REAL
 * `./transport` adapter runs on top of it, so these tests exercise the adapter's
 * control-stream caching, bind correlation and teardown as well as tunnel.ts's
 * dispatch. SSH stays available through a fake `ssh2` client so the `both`
 * fallback and the "SSH error types are untouched" cases are real.
 *
 * The data/claim streams handed back are real `node:stream` Duplexes: tunnel.ts
 * pipes a real local socket into them, so byte-level assertions only mean
 * something if the stream is a genuine duplex.
 */
const { h2State, FakeSshClient } = vi.hoisted(() => {
  const h2State = {
    connected: false,
    kind: 'h2c' as 'tls' | 'h2c' | null,
    /** Every connect() the module issued, in order. */
    connectCalls: [] as Array<{ host: string; port: number; prefer?: 'tls' | 'h2c' }>,
    /**
     * Canned connect() results, shifted per call. When empty the fake succeeds
     * with `prefer` (or h2c) so the happy path needs no setup.
     */
    connectResults: [] as Array<{ ok: boolean; kind: 'tls' | 'h2c'; error: string }>,
    /** -L streams returned by openStream(), in order. */
    streams: [] as any[],
    /** Claim streams returned by openClaimStream(), in order. */
    claimStreams: [] as any[],
    /** Tokens passed to openClaimStream(), in order. */
    claimTokens: [] as string[],
    openStreamCalls: [] as Array<{ host: string; port: number }>,
    openStreamError: null as Error | null,
    claimError: null as Error | null,
    /** The single control stream the adapter caches. */
    control: null as any,
    closeCalls: 0,
  }

  class FakeSshClientImpl {
    static instances: FakeSshClientImpl[] = []
    static last(): FakeSshClientImpl { return FakeSshClientImpl.instances[FakeSshClientImpl.instances.length - 1] }
    handlers: Record<string, Array<(...a: unknown[]) => void>> = {}
    connectArgs: Record<string, unknown> | null = null
    ended = false
    constructor() { FakeSshClientImpl.instances.push(this) }
    on(ev: string, cb: (...a: unknown[]) => void): this { (this.handlers[ev] ||= []).push(cb); return this }
    connect(args: Record<string, unknown>): this { this.connectArgs = args; return this }
    end(): void { this.ended = true }
    forwardOut(_sh: string, _sp: number, _dh: string, _dp: number, cb: (e?: Error, s?: unknown) => void): void {
      cb(undefined, { destroy: () => undefined, on: () => undefined, pipe: () => undefined })
    }
    forwardIn(_a: string, p: number, cb: (e?: Error, r?: number) => void): void { cb(undefined, p) }
    unforwardIn(): void { /* no-op */ }
    emit(ev: string, ...args: unknown[]): void { for (const cb of this.handlers[ev] || []) cb(...args) }
  }

  return { h2State, FakeSshClient: FakeSshClientImpl }
})

vi.mock('./h2Transport', async () => {
  const { Duplex } = await import('node:stream')

  /** A real duplex whose writable side records every chunk the module writes. */
  function makeStream(): any {
    const written: Buffer[] = []
    // allowHalfOpen mirrors the REAL tunnel stream (h2Transport.wrapStream sets
    // it): ending the writable side must NOT auto-end the readable side, or a
    // half-close test could not push the target's trailing response.
    const s: any = new Duplex({
      allowHalfOpen: true,
      read() { /* the test pushes explicitly */ },
      write(chunk: Buffer, _enc: BufferEncoding, cb: (e?: Error | null) => void) {
        written.push(Buffer.from(chunk))
        cb()
      },
    })
    s.written = written
    return s
  }

  function makeControl(): any {
    const msgHandlers: Array<(m: any) => void> = []
    const closeHandlers: Array<() => void> = []
    const cs: any = {
      sent: [] as any[],
      closed: false,
      send(msg: any) { if (!cs.closed) cs.sent.push(msg) },
      onMessage(h: (m: any) => void) { msgHandlers.push(h) },
      onClose(h: () => void) { closeHandlers.push(h) },
      close() { cs.end() },
      /** Test-driven: deliver a server -> client control line. */
      emit(msg: any) { for (const h of [...msgHandlers]) h(msg) },
      /** Test-driven: the server ended the control stream. */
      end() {
        if (cs.closed) return
        cs.closed = true
        for (const h of [...closeHandlers]) h()
      },
    }
    return cs
  }

  return {
    connect: async (opts: { host: string; port: number; prefer?: 'tls' | 'h2c' }) => {
      h2State.connectCalls.push({ ...opts })
      const canned = h2State.connectResults.shift()
      if (canned) {
        h2State.connected = canned.ok
        if (canned.ok) h2State.kind = canned.kind
        return canned
      }
      h2State.connected = true
      h2State.kind = opts.prefer ?? 'h2c'
      return { ok: true, kind: h2State.kind, error: '' }
    },
    openStream: (host: string, port: number) => {
      h2State.openStreamCalls.push({ host, port })
      if (h2State.openStreamError) return Promise.reject(h2State.openStreamError)
      const s = makeStream()
      h2State.streams.push(s)
      return Promise.resolve(s)
    },
    openClaimStream: (token: string) => {
      h2State.claimTokens.push(token)
      if (h2State.claimError) return Promise.reject(h2State.claimError)
      const s = makeStream()
      h2State.claimStreams.push(s)
      return Promise.resolve(s)
    },
    openControlStream: () => {
      if (!h2State.control) h2State.control = makeControl()
      return Promise.resolve(h2State.control)
    },
    close: () => {
      h2State.closeCalls++
      h2State.connected = false
      const c = h2State.control
      h2State.control = null
      if (c) c.end()
    },
    isConnected: () => h2State.connected,
    getTransportKind: () => (h2State.connected ? h2State.kind : null),
  }
})

vi.mock('ssh2', () => ({ Client: FakeSshClient }))

/**
 * Real `net` — every socket below is genuine, so a local port that the module
 * fails to re-bind is actually unreachable. The mock only OBSERVES: it wraps
 * `createServer` to record each server and the sockets it accepts, which is how
 * the fd-reclamation and "exactly one listener" assertions get a handle on the
 * module's listener without exporting it.
 */
const { netState } = vi.hoisted(() => ({
  netState: {
    /** Every server the module (or a test helper) created, with its accepts. */
    servers: [] as Array<{ server: any; accepted: any[] }>,
  },
}))

vi.mock('node:net', async (importOriginal) => {
  const actual = await importOriginal<typeof import('node:net')>()
  const createServer = (...args: unknown[]) => {
    const entry = { server: null as any, accepted: [] as any[] }
    const wrapped = args.map((a) =>
      typeof a === 'function'
        ? (socket: import('node:net').Socket) => {
            entry.accepted.push(socket)
            ;(a as (s: unknown) => void)(socket)
          }
        : a,
    )
    const server = (actual.createServer as (...a: unknown[]) => net.Server)(...wrapped)
    entry.server = server
    netState.servers.push(entry)
    return server
  }
  // `tunnel.ts` does `import net from 'node:net'`, so keep the default export in
  // sync with the named one.
  return { ...actual, createServer, default: { ...actual, createServer } }
})

/** The entry whose server is CURRENTLY bound to `port`, or undefined. */
function listeningServer(port: number): { server: any; accepted: any[] } | undefined {
  return netState.servers.find((e) => {
    const a = e.server.address()
    return !!a && typeof a === 'object' && a.port === port
  })
}

/** How many of the recorded servers are still listening on `port`. */
function liveListeners(port: number): number {
  return netState.servers.filter((e) => {
    const a = e.server.address()
    return !!a && typeof a === 'object' && a.port === port
  }).length
}

vi.mock('electron', () => ({
  safeStorage: { isEncryptionAvailable: () => false, encryptString: (s: string) => Buffer.from(s), decryptString: (b: Buffer) => b.toString() },
}))

const storeData: Record<string, unknown> = { serverUrl: 'https://127.0.0.1:20000' }
vi.mock('electron-store', () => ({
  default: class {
    get(k: string) { return storeData[k] }
    set(k: string, v: unknown) { storeData[k] = v }
  },
}))

vi.mock('./secrets', () => ({
  savePassword: () => undefined,
  getPassword: () => 'test-password',
}))

const { fakeHttpGet } = vi.hoisted(() => ({
  fakeHttpGet: (_url: string, _opts: unknown, cb: (res: unknown) => void) => {
    const handlers: Record<string, (d?: string) => void> = {}
    const res = {
      statusCode: 200,
      on(ev: string, h: (d?: string) => void) { handlers[ev] = h; return res },
    }
    queueMicrotask(() => {
      handlers['data']?.('{"enabled":true,"port":20001,"username":"clawbench"}')
      handlers['end']?.()
    })
    cb(res)
    return { on: () => undefined }
  },
}))

vi.mock('node:http', () => ({ default: { get: fakeHttpGet } }))
vi.mock('node:https', () => ({ default: { get: fakeHttpGet } }))

import { initStore } from './store'
import {
  addForwardedPort, addReverseForwardedPort, removeForwardedPort, ensureTunnel,
  getForwardedPorts, isTunnelConnected, disconnectTunnel, getTunnelErrorType,
  getActiveTransport, getTunnelTransportKind, setTransportPreference,
  testPortReachable, reconnectTunnel, _resetTransportForTesting,
  _rebuildAllForwardsForTesting,
} from './tunnel'

const PORT_A = 28941
const PORT_TARGET = 28942

/**
 * Start a real loopback echo server, so -R splicing is proven with real bytes.
 * Returns the server plus a teardown that cannot hang (see stopEcho).
 */
function startEcho(port: number): Promise<{ srv: net.Server; stop: () => Promise<void> }> {
  const conns = new Set<net.Socket>()
  const srv = net.createServer((sock) => {
    conns.add(sock)
    sock.on('close', () => conns.delete(sock))
    sock.pipe(sock)
  })
  return new Promise((resolve) => srv.listen(port, '127.0.0.1', () => resolve({
    srv,
    stop: async () => {
      // net.Server has no closeAllConnections (that is an http.Server method,
      // and this Node is 20.x), so the accepted sockets are tracked here.
      // close() alone waits for them, and the spliced upstream stays open for
      // the life of the test — without destroying them the teardown hangs.
      for (const s of conns) s.destroy()
      await new Promise<void>((r) => srv.close(() => r()))
    },
  })))
}

/** Connect a real client to a local forwarded port and wait for the accept. */
async function dialLocal(port: number): Promise<net.Socket> {
  const sock = net.connect(port, '127.0.0.1')
  await new Promise<void>((resolve, reject) => {
    sock.once('connect', () => resolve())
    sock.once('error', reject)
  })
  return sock
}

/** Bring the module to a connected state over the h2 transport. */
async function connectH2(): Promise<void> {
  setTransportPreference('h2')
  const p = ensureTunnel()
  await vi.waitFor(() => expect(h2State.connectCalls.length).toBeGreaterThan(0))
  expect(await p).toBe(true)
  expect(isTunnelConnected()).toBe(true)
}

/** Bind a reverse port over h2 by answering the control stream's `bind`. */
async function bindReverse(serverPort: number, targetPort: number, host: string, allocated?: number): Promise<boolean> {
  const p = addReverseForwardedPort(serverPort, targetPort, host)
  await vi.waitFor(() => expect(h2State.control?.sent.length).toBeGreaterThan(0))
  h2State.control.emit({ type: 'bound', port: allocated ?? serverPort })
  return p
}

/**
 * Errors that escaped as uncaughtException during `action`. An 'error' with no
 * listener is re-thrown on a later tick, so a try/catch around the call cannot
 * see it — this is the same channel the process sees.
 */
async function collectUncaught(action: () => Promise<void> | void): Promise<Error[]> {
  const seen: Error[] = []
  const handler = (e: Error) => { seen.push(e) }
  process.on('uncaughtException', handler)
  try {
    await action()
    await new Promise((r) => setTimeout(r, 20))
  } finally {
    process.off('uncaughtException', handler)
  }
  return seen
}

beforeEach(() => {
  initStore()
  _resetTransportForTesting()
  h2State.connected = false
  h2State.kind = 'h2c'
  h2State.connectCalls.length = 0
  h2State.connectResults.length = 0
  h2State.streams.length = 0
  h2State.claimStreams.length = 0
  h2State.claimTokens.length = 0
  h2State.openStreamCalls.length = 0
  h2State.openStreamError = null
  h2State.claimError = null
  h2State.control = null
  h2State.closeCalls = 0
  netState.servers.length = 0
  FakeSshClient.instances = []
  storeData.serverUrl = 'https://127.0.0.1:20000'
})

afterEach(() => {
  for (const p of getForwardedPorts()) removeForwardedPort(p.port)
  _resetTransportForTesting()
})

describe('tunnel/h2: transport dispatch', () => {
  it('ensureTunnel connects over h2 when the preference is h2', async () => {
    await connectH2()

    expect(h2State.connectCalls).toEqual([{ host: '127.0.0.1', port: 20000 }])
    // SSH must not be touched at all in h2-only mode.
    expect(FakeSshClient.instances.length).toBe(0)
    expect(getActiveTransport()).toBe('h2')
    expect(getTunnelTransportKind()).toBe('h2c')
  })

  it('prefers h2 and falls back to ssh under the "both" preference', async () => {
    // h2 unreachable (no h2 listener on the main port) — the probe fails.
    h2State.connectResults.push({ ok: false, kind: 'tls', error: 'ERR_HTTP2_ERROR' })
    setTransportPreference('both')

    const p = ensureTunnel()
    await vi.waitFor(() => expect(FakeSshClient.instances.length).toBe(1))
    FakeSshClient.last().emit('ready')
    expect(await p).toBe(true)

    // h2 was probed FIRST (design doc §2.3), then SSH carried the tunnel.
    expect(h2State.connectCalls.length).toBe(1)
    expect(h2State.connectCalls[0].host).toBe('127.0.0.1')
    expect(getActiveTransport()).toBe('ssh')
    expect(getTunnelTransportKind()).toBeNull()
  })

  it('remembering the last successful h2 kind skips the wasted TLS probe', async () => {
    // A plaintext deployment: the first connect lands on h2c.
    await connectH2()
    expect(h2State.connectCalls[0].prefer).toBeUndefined()
    expect(h2State.kind).toBe('h2c')

    disconnectTunnel()
    const p = ensureTunnel()
    await vi.waitFor(() => expect(h2State.connectCalls.length).toBe(2))
    await p

    // Regression guard: without the memory, every reconnect pays one rejected
    // TLS handshake before h2c is tried.
    expect(h2State.connectCalls[1].prefer).toBe('h2c')
  })

  it('remembers h2-over-TLS when that is what worked', async () => {
    h2State.connectResults.push({ ok: true, kind: 'tls', error: '' })
    await connectH2()
    expect(getTunnelTransportKind()).toBe('tls')

    disconnectTunnel()
    const p = ensureTunnel()
    await vi.waitFor(() => expect(h2State.connectCalls.length).toBe(2))
    await p
    expect(h2State.connectCalls[1].prefer).toBe('tls')
  })

  it('does not report connected when every candidate fails', async () => {
    h2State.connectResults.push({ ok: false, kind: 'tls', error: 'ECONNREFUSED' })
    setTransportPreference('h2')

    expect(await ensureTunnel()).toBe(false)
    expect(isTunnelConnected()).toBe(false)
    expect(getTunnelErrorType()).toBe('network')
  })
})

describe('tunnel/h2: -L forwarding', () => {
  it('opens one h2 stream per accepted connection and passes bytes both ways', async () => {
    await connectH2()
    expect(await addForwardedPort(PORT_A, 20000, '')).toBe(true)

    const client = await dialLocal(PORT_A)
    await vi.waitFor(() => expect(h2State.streams.length).toBe(1))
    // The dial carries the registered target host/port, not a hardcoded one.
    expect(h2State.openStreamCalls).toEqual([{ host: '', port: 20000 }])

    // local -> target (the module pipes the accepted socket into the stream)
    client.write('ping')
    await vi.waitFor(() => expect(h2State.streams[0].written.map((b: Buffer) => b.toString()).join('')).toBe('ping'))

    // target -> local
    const got: Buffer[] = []
    client.on('data', (c: Buffer) => got.push(c))
    h2State.streams[0].push(Buffer.from('pong'))
    await vi.waitFor(() => expect(got.map((b) => b.toString()).join('')).toBe('pong'))

    client.destroy()
  })

  it('destroys the local socket when the stream cannot be opened', async () => {
    // A 502 from the server (target unreachable) must not leave the local
    // socket spliced to nothing, nor crash the main process.
    h2State.openStreamError = new Error('stream rejected with HTTP 502')
    await connectH2()
    await addForwardedPort(PORT_A, 20000, '')

    const client = net.connect(PORT_A, '127.0.0.1')
    client.on('error', () => { /* the reset is expected */ })
    const closed = new Promise<void>((r) => client.on('close', () => r()))

    const escaped = await collectUncaught(async () => {
      await vi.waitFor(() => expect(h2State.openStreamCalls.length).toBe(1))
      await closed
    })
    expect(escaped.map((e) => e.message)).toEqual([])
  })

  it('re-checks the transport per connection so a reconnect does not strand the listener', async () => {
    await connectH2()
    await addForwardedPort(PORT_A, 20000, '')

    // The tunnel drops: the listener survives, the transport does not.
    disconnectTunnel()
    const client = net.connect(PORT_A, '127.0.0.1')
    client.on('error', () => { /* expected reset */ })
    await new Promise((r) => setTimeout(r, 50))

    // No stream may be opened on a dead transport.
    expect(h2State.streams.length).toBe(0)
    client.destroy()
  })
})

describe('tunnel/h2: -R (reverse) forwarding', () => {
  it('binds over the control stream, claims an incoming connection and splices it', async () => {
    const echo = await startEcho(PORT_TARGET)
    try {
      await connectH2()
      const p = addReverseForwardedPort(PORT_A, PORT_TARGET, '127.0.0.1')
      await vi.waitFor(() => expect(h2State.control?.sent.length).toBe(1))
      expect(h2State.control.sent[0]).toEqual({ type: 'bind', port: PORT_A })

      h2State.control.emit({ type: 'bound', port: PORT_A })
      expect(await p).toBe(true)
      expect(getForwardedPorts()).toEqual([{ port: PORT_A, host: '127.0.0.1', direction: 'reverse' }])

      // The server parked a connection and announced it with a single-use token.
      h2State.control.emit({ type: 'incoming', port: PORT_A, token: 'tok-1' })
      await vi.waitFor(() => expect(h2State.claimStreams.length).toBe(1))
      expect(h2State.claimTokens).toEqual(['tok-1'])

      // The claim stream is spliced to the local target: a byte written by the
      // server side reaches the echo server and comes back.
      const claim = h2State.claimStreams[0]
      claim.push(Buffer.from('hello'))
      await vi.waitFor(() => expect(claim.written.map((b: Buffer) => b.toString()).join('')).toBe('hello'))
      claim.destroy()
    } finally {
      await echo.stop()
    }
  })

  it('reports failure when the server rejects the bind', async () => {
    await connectH2()
    const p = addReverseForwardedPort(PORT_A, 3000, '')
    await vi.waitFor(() => expect(h2State.control?.sent.length).toBe(1))
    h2State.control.emit({ type: 'bind_err', port: PORT_A, code: 3, msg: 'reserved' })

    expect(await p).toBe(false)
    // The intent is kept (only removeForwardedPort clears it), so a reconnect
    // retries the bind; the listener itself was never registered.
    expect(getForwardedPorts()).toEqual([{ port: PORT_A, host: '', direction: 'reverse' }])
  })

  it('records the port the server actually allocated', async () => {
    await connectH2()
    // Ask for 0; the server picks one and reports it in `bound`.
    expect(await bindReverse(0, 3000, '', PORT_TARGET)).toBe(true)
    expect(getForwardedPorts()).toEqual([{ port: 0, host: '', direction: 'reverse' }])

    // Removal must unbind the REAL port, not the requested 0.
    removeForwardedPort(0)
    expect(h2State.control.sent).toContainEqual({ type: 'unbind', port: PORT_TARGET })
  })

  it('does not register a reverse forward removed during the request', async () => {
    await connectH2()
    const p = addReverseForwardedPort(PORT_A, 3000, '')
    await vi.waitFor(() => expect(h2State.control?.sent.length).toBe(1))
    // The removal lands inside the request window (before `bound` arrives).
    removeForwardedPort(PORT_A)
    h2State.control.emit({ type: 'bound', port: PORT_A })

    // Regression guard: publishing here would revive a deleted mapping.
    expect(await p).toBe(false)
    expect(getForwardedPorts()).toEqual([])
    // The server-side listener must be released, not left orphaned.
    expect(h2State.control.sent).toContainEqual({ type: 'unbind', port: PORT_A })
  })

  it('fails an in-flight bind instead of hanging when the control stream ends', async () => {
    await connectH2()
    const p = addReverseForwardedPort(PORT_A, 3000, '')
    await vi.waitFor(() => expect(h2State.control?.sent.length).toBe(1))

    // The tunnel died before answering. A bind that never settles would also
    // poison pendingReverseBinds for that port forever.
    h2State.control.end()
    expect(await p).toBe(false)
  })

  it('sends unbind on removeForwardedPort', async () => {
    await connectH2()
    expect(await bindReverse(PORT_A, 3000, '')).toBe(true)

    removeForwardedPort(PORT_A)
    expect(h2State.control.sent).toContainEqual({ type: 'unbind', port: PORT_A })
    expect(getForwardedPorts()).toEqual([])
  })

  it('ignores an incoming connection for an unknown port', async () => {
    await connectH2()
    expect(await bindReverse(PORT_A, 3000, '')).toBe(true)

    // A spurious announcement must not open a claim stream.
    h2State.control.emit({ type: 'incoming', port: 28999, token: 'tok-x' })
    await new Promise((r) => setTimeout(r, 20))
    expect(h2State.claimStreams.length).toBe(0)
  })

  it('tears the control stream and session down on disconnectTunnel', async () => {
    await connectH2()
    expect(await bindReverse(PORT_A, 3000, '')).toBe(true)
    const control = h2State.control

    disconnectTunnel()

    expect(control.closed).toBe(true)
    expect(h2State.closeCalls).toBe(1)
    expect(isTunnelConnected()).toBe(false)
    // The intent survives so a reconnect restores the mapping.
    expect(getForwardedPorts()).toEqual([{ port: PORT_A, host: '', direction: 'reverse' }])
  })

  it('drops a stale claim stream when the local target is unreachable', async () => {
    await connectH2()
    expect(await bindReverse(PORT_A, PORT_TARGET, '127.0.0.1')).toBe(true)

    // Nothing listens on PORT_TARGET: the dial fails, so no claim stream may be
    // opened (which would otherwise leak a parked server-side connection).
    const escaped = await collectUncaught(async () => {
      h2State.control.emit({ type: 'incoming', port: PORT_A, token: 'tok-1' })
      await new Promise((r) => setTimeout(r, 30))
    })
    expect(escaped.map((e) => e.message)).toEqual([])
    expect(h2State.claimStreams.length).toBe(0)
  })
})

describe('tunnel/h2: connection monitor', () => {
  it('does not back off while a healthy h2 tunnel is up, and rebuilds after a drop', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    try {
      await connectH2()
      await addForwardedPort(PORT_A, 20000, '')
      expect(await testPortReachable(PORT_A)).toBe(true)
      const callsBefore = h2State.connectCalls.length

      // A healthy h2 tunnel has no ssh2 `client`. An SSH-only liveness check
      // would treat every tick as a failure and inflate the backoff counter, so
      // a later real drop would wait out the 120s delay instead of reconnecting.
      await vi.advanceTimersByTimeAsync(15001)
      expect(h2State.connectCalls.length).toBe(callsBefore)

      // Now drop the session: the monitor must reconnect on its own.
      h2State.connected = false
      await vi.advanceTimersByTimeAsync(15001)
      vi.useRealTimers()

      expect(h2State.connectCalls.length).toBeGreaterThan(callsBefore)
      expect(isTunnelConnected()).toBe(true)
      expect(await testPortReachable(PORT_A)).toBe(true)
    } finally {
      vi.useRealTimers()
    }
  })
})

describe('tunnel/h2: error classification', () => {
  it('maps an h2 protocol failure to a network error', async () => {
    h2State.connectResults.push({ ok: false, kind: 'h2c', error: 'ERR_HTTP2_ERROR' })
    setTransportPreference('h2')
    expect(await ensureTunnel()).toBe(false)
    expect(getTunnelErrorType()).toBe('network')
  })

  it('maps a TLS handshake failure to a network error', async () => {
    h2State.connectResults.push({ ok: false, kind: 'tls', error: 'self-signed certificate' })
    setTransportPreference('h2')
    expect(await ensureTunnel()).toBe(false)
    expect(getTunnelErrorType()).toBe('network')
  })

  it('leaves an SSH authentication failure classified as auth', async () => {
    // h2 fails, then the SSH fallback reports a credential problem. The ssh2
    // branches must still win: the h2 additions only cover connectivity.
    h2State.connectResults.push({ ok: false, kind: 'tls', error: 'ERR_HTTP2_ERROR' })
    setTransportPreference('both')

    const p = ensureTunnel()
    await vi.waitFor(() => expect(FakeSshClient.instances.length).toBe(1))
    FakeSshClient.last().emit('error', Object.assign(new Error('auth failed'), { level: 'client-authentication' }))

    expect(await p).toBe(false)
    expect(getTunnelErrorType()).toBe('auth')
  })
})

/**
 * These cases exist because the original h2 suite mocked `openStream` and never
 * dialled the local port, which hid two real defects:
 *
 *  - `reconnectTunnel()` returned true under h2 while every listener stayed
 *    closed (the rebuild only ran in ssh2's `'ready'` handler);
 *  - the local listener used the default `allowHalfOpen: false`, so a local
 *    half-close destroyed the socket and the target's reply was lost.
 *
 * Every assertion below therefore dials a REAL local port; a listener the
 * module failed to restore is genuinely unreachable, not merely unbookkept.
 */
describe('tunnel/h2: reconnect really rebinds the local listener', () => {
  it('makes the port reachable again after reconnectTunnel', async () => {
    await connectH2()
    expect(await addForwardedPort(PORT_A, 20000, '')).toBe(true)
    expect(await testPortReachable(PORT_A)).toBe(true)

    // reconnectTunnel = disconnectTunnel (closes every listener) + ensureTunnel.
    // Under h2 the old code only flipped state.connected, so the port came back
    // dead while this call still resolved true.
    expect(await reconnectTunnel()).toBe(true)

    expect(isTunnelConnected()).toBe(true)
    expect(getForwardedPorts()).toEqual([{ port: PORT_A, host: '', direction: 'forward' }])
    // The load-bearing assertion: a real dial, not a bookkeeping check.
    expect(await testPortReachable(PORT_A)).toBe(true)
  })

  it('keeps exactly one listener bound for the port after a reconnect', async () => {
    await connectH2()
    await addForwardedPort(PORT_A, 20000, '')
    expect(await reconnectTunnel()).toBe(true)

    // A rebuild that forgot to close the previous listener would leave two
    // servers fighting for the port; `closeForwardServer` runs first so exactly
    // one is left.
    expect(liveListeners(PORT_A)).toBe(1)
    expect(await testPortReachable(PORT_A)).toBe(true)
  })

  it('restores a reverse-only mapping on reconnect without a local listener', async () => {
    await connectH2()
    expect(await bindReverse(PORT_A, 3000, '')).toBe(true)

    // The reconnect re-issues the bind on the FRESH control stream, so the
    // server's reply has to be driven for it to complete.
    const p = reconnectTunnel()
    await vi.waitFor(() => expect(h2State.control?.sent.length).toBeGreaterThan(0))
    expect(h2State.control.sent[0]).toEqual({ type: 'bind', port: PORT_A })
    h2State.control.emit({ type: 'bound', port: PORT_A })
    expect(await p).toBe(true)

    expect(getForwardedPorts()).toEqual([{ port: PORT_A, host: '', direction: 'reverse' }])
  })

  it('the monitor path keeps the port reachable after a session drop', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    try {
      await connectH2()
      await addForwardedPort(PORT_A, 20000, '')
      expect(await testPortReachable(PORT_A)).toBe(true)

      // The session dies without an explicit teardown; only the monitor runs.
      h2State.connected = false
      await vi.advanceTimersByTimeAsync(15001)
      vi.useRealTimers()

      expect(isTunnelConnected()).toBe(true)
      expect(await testPortReachable(PORT_A)).toBe(true)
      expect(liveListeners(PORT_A)).toBe(1)
    } finally {
      vi.useRealTimers()
    }
  })

  it('rebuildAllForwards is idempotent (no duplicate listeners)', async () => {
    await connectH2()
    await addForwardedPort(PORT_A, 20000, '')
    expect(liveListeners(PORT_A)).toBe(1)

    // Two extra rebuilds, as a reconnect plus a monitor tick could produce.
    await _rebuildAllForwardsForTesting()
    await _rebuildAllForwardsForTesting()

    expect(liveListeners(PORT_A)).toBe(1)
    expect(await testPortReachable(PORT_A)).toBe(true)
  })
})

describe('tunnel/h2: local listener preserves TCP half-close', () => {
  /** Dial the local forwarded port, collecting bytes and watching for EOF. */
  async function dialHalfOpen(port: number): Promise<{ client: net.Socket; chunks: Buffer[]; ended: () => boolean }> {
    const client = net.connect({ host: '127.0.0.1', port, allowHalfOpen: true })
    await new Promise<void>((resolve, reject) => {
      client.once('connect', () => resolve())
      client.once('error', reject)
    })
    const chunks: Buffer[] = []
    let ended = false
    client.on('data', (c: Buffer) => chunks.push(c))
    client.on('end', () => { ended = true })
    return { client, chunks, ended: () => ended }
  }

  it('delivers the target response after the local socket half-closes', async () => {
    await connectH2()
    await addForwardedPort(PORT_A, 20000, '')

    const { client, chunks, ended } = await dialHalfOpen(PORT_A)
    await vi.waitFor(() => expect(h2State.streams.length).toBe(1))
    const stream = h2State.streams[0]

    // The request is written, then the local side half-closes (FIN). This is
    // the request/response shape a client that closes its write side uses.
    client.write('request-body')
    client.end()
    // The half-close must reach the tunnel as an END_STREAM / stream.end(),
    // NOT as a socket teardown.
    await vi.waitFor(() => expect(stream.writableEnded).toBe(true))

    // The target's reply arrives after our FIN and must still be delivered.
    stream.push(Buffer.from('TRAILER:request-body'))
    stream.push(null)

    await vi.waitFor(() => expect(ended()).toBe(true))
    expect(chunks.map((b) => b.toString()).join('')).toBe('TRAILER:request-body')
    client.destroy()
  })

  it('does not auto-destroy the socket when the local side half-closes', async () => {
    await connectH2()
    await addForwardedPort(PORT_A, 20000, '')

    const { client } = await dialHalfOpen(PORT_A)
    await vi.waitFor(() => expect(netState.servers.length).toBeGreaterThan(0))
    const entry = listeningServer(PORT_A)
    expect(entry).toBeDefined()

    client.write('ping')
    client.end()
    await vi.waitFor(() => expect(entry!.accepted.length).toBe(1))

    // allowHalfOpen:false would have destroyed the module's socket on the FIN.
    await vi.waitFor(() => expect(entry!.accepted[0].readableEnded).toBe(true))
    expect(entry!.accepted[0].destroyed).toBe(false)

    // Clean up: end the tunnel side so the splice releases the socket.
    h2State.streams[0].push(null)
    await vi.waitFor(() => expect(entry!.accepted[0].destroyed).toBe(true))
    client.destroy()
  })

  it('reclaims sockets after repeated half-close and full-close cycles', async () => {
    await connectH2()
    await addForwardedPort(PORT_A, 20000, '')
    const entry = () => listeningServer(PORT_A)!

    for (let i = 0; i < 6; i++) {
      const { client, chunks, ended } = await dialHalfOpen(PORT_A)
      await vi.waitFor(() => expect(h2State.streams.length).toBe(i + 1))
      const stream = h2State.streams[i]

      if (i % 2 === 0) {
        // Half-close first, target replies, then the response ends. Both
        // directions finish, so the splice must reclaim the socket.
        client.write('request-body')
        client.end()
        await vi.waitFor(() => expect(stream.writableEnded).toBe(true))
        stream.push(Buffer.from('TRAILER:request-body'))
        stream.push(null)
        await vi.waitFor(() => expect(ended()).toBe(true))
        expect(chunks.map((b) => b.toString()).join('')).toBe('TRAILER:request-body')
      } else {
        // The target closes first; the local side then finishes its write.
        stream.push(Buffer.from('TRAILER:request-body'))
        stream.push(null)
        await vi.waitFor(() => expect(ended()).toBe(true))
        client.write('request-body')
        client.end()
        await vi.waitFor(() => expect(stream.writableEnded).toBe(true))
      }

      // Each cycle's socket must be gone before the next one starts.
      await vi.waitFor(() => {
        expect(entry().accepted[i].destroyed).toBe(true)
      })
    }

    // The classic allowHalfOpen trap: without explicit reclamation these would
    // all still be open. Both the accepted sockets and the server's connection
    // count must drain to zero.
    await vi.waitFor(() => {
      expect(entry().accepted.filter((s: net.Socket) => !s.destroyed).length).toBe(0)
    })
    const conns = await new Promise<number>((r) => entry().server.getConnections((_e: unknown, c: number) => r(c)))
    expect(conns).toBe(0)
  })

  it('reclaims the socket on an abrupt client reset', async () => {
    await connectH2()
    await addForwardedPort(PORT_A, 20000, '')
    const entry = () => listeningServer(PORT_A)!

    const { client } = await dialHalfOpen(PORT_A)
    await vi.waitFor(() => expect(h2State.streams.length).toBe(1))
    const stream = h2State.streams[0]
    client.write('request-body')
    await vi.waitFor(() => expect(stream.written.length).toBeGreaterThan(0))

    // resetAndDestroy() forces an RST (destroy() alone is a clean FIN, which
    // legitimately leaves the socket waiting for the target's reply).
    client.resetAndDestroy()

    await vi.waitFor(() => expect(entry().accepted[0].destroyed).toBe(true))
    expect(stream.destroyed).toBe(true)
  })

  it('releases the socket when the tunnel stream errors mid-transfer', async () => {
    await connectH2()
    await addForwardedPort(PORT_A, 20000, '')

    const { client } = await dialHalfOpen(PORT_A)
    await vi.waitFor(() => expect(h2State.streams.length).toBe(1))
    const stream = h2State.streams[0]
    client.write('request-body')
    await vi.waitFor(() => expect(stream.written.length).toBeGreaterThan(0))

    const escaped = await collectUncaught(async () => {
      stream.emit('error', Object.assign(new Error('read ECONNRESET'), { code: 'ECONNRESET' }))
      await vi.waitFor(() => expect(listeningServer(PORT_A)!.accepted[0].destroyed).toBe(true))
    })
    expect(escaped.map((e) => e.message)).toEqual([])
    client.destroy()
  })
})
