import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import net from 'node:net'
import { PassThrough } from 'node:stream'

// Test-only knob: delays the `listen` callback so the rebuild window (the span
// between the desired-set snapshot and an actual bind) is observable. Real
// binds complete too fast to hit the mid-rebuild removal race deterministically.
const { listenDelay } = vi.hoisted(() => ({ listenDelay: { ms: 0 } }))

/**
 * Which (host, port) the net.connect mock should fake. Held on globalThis so the
 * vi.mock factory and the test body are guaranteed to observe the SAME object —
 * a hoisted object literal can end up duplicated across vitest's module
 * registries, which silently makes the factory read stale values.
 */
const REVERSE_DIAL_KEY = '__clawbenchTestReverseDial'
function reverseDialState(): { host: string; port: number; armed: boolean } {
  const g = globalThis as Record<string, unknown>
  if (!g[REVERSE_DIAL_KEY]) g[REVERSE_DIAL_KEY] = { host: '', port: 0, armed: false }
  return g[REVERSE_DIAL_KEY] as { host: string; port: number; armed: boolean }
}

vi.mock('node:net', async (importOriginal) => {
  const actual = await importOriginal<typeof import('node:net')>()
  const realCreateServer = actual.createServer as unknown as (...a: unknown[]) => net.Server
  const createServer = (...args: unknown[]) => {
    // net.createServer(cb) registers cb INTERNALLY, so overriding server.on()
    // cannot intercept it — wrap the callback argument instead. This lets a
    // test emit('error') on the REAL server-side socket tunnel.ts wired its
    // handlers onto; emitting on the test's own client socket would prove
    // nothing, since that socket is not the module's.
    const wrapped = args.map((a, i) =>
      i === 0 && typeof a === 'function'
        ? (socket: import('node:net').Socket) => {
            acceptedSockets.push(socket)
            ;(a as (s: unknown) => void)(socket)
          }
        : a,
    )
    const server = realCreateServer(...(wrapped as []))
    const realListen = server.listen.bind(server)
    server.listen = ((...largs: unknown[]) => {
      const cb = typeof largs[largs.length - 1] === 'function'
        ? (largs.pop() as () => void)
        : undefined
      return realListen(...(largs as []), () => {
        if (cb) setTimeout(cb, listenDelay.ms)
      })
    }) as typeof server.listen
    return server
  }
  // Record the outbound dials reverse forwarding makes, WITHOUT replacing real
  // sockets: testPortReachable() (and the tests' own probes) need genuine
  // connects. Only the reverse-forward target is faked, recognised by its
  // (host, port) pair, so everything else falls through to the real connect.
  const connect = ((...args: unknown[]) => {
    // net.connect accepts either (port, host) positionally or a single options
    // object — the module uses the positional form for reverse dials and the
    // object form in testPortReachable().
    let port = 0
    let host = ''
    if (typeof args[0] === 'object' && args[0] !== null) {
      const o = args[0] as { port?: number; host?: string }
      port = o.port ?? 0
      host = o.host ?? ''
    } else {
      port = typeof args[0] === 'number' ? args[0] : 0
      host = typeof args[1] === 'string' ? args[1] : ''
    }
    const dial = (globalThis as Record<string, any>)[REVERSE_DIAL_KEY] as { host: string; port: number; armed: boolean } | undefined
    if (dial?.armed && port === dial.port && host === dial.host) {
      dials.push({ port, host })
      const sock = new PassThrough() as unknown as net.Socket
      const handlers: Record<string, Array<() => void>> = {}
      const realOn = sock.on.bind(sock)
      ;(sock as unknown as { on: unknown }).on = (ev: string, cb: () => void) => {
        ;(handlers[ev] ||= []).push(cb)
        return realOn(ev as never, cb as never)
      }
      queueMicrotask(() => { for (const cb of handlers['connect'] || []) cb() })
      return sock
    }
    return (actual.connect as (...a: unknown[]) => net.Socket)(...args)
  }) as typeof actual.connect
  // `tunnel.ts` does `import net from 'node:net'`, so the default export is what
  // it reads — keep it in sync with the named one.
  return { ...actual, createServer, connect, default: { ...actual, createServer, connect } }
})

/**
 * Outbound dials made via net.connect, in order. Reverse forwarding dials the
 * local target for each connection the server hands over.
 */
const dials: Array<{ port: number; host: string }> = []

/**
 * Arms the fake reverse dial for one (host, port) pair. Reverse forwarding dials
 * the local target for each connection the server hands over; faking just that
 * dial keeps testPortReachable() (and the harness's own probes) on real sockets.
 */
function armFakeReverseDial(host: string, port: number): void {
  const d = reverseDialState()
  d.host = host
  d.port = port
  d.armed = true
}

function disarmFakeReverseDial(): void {
  reverseDialState().armed = false
}

// A controllable stand-in for ssh2's Client. Each instance records the
// handlers the module registered and exposes emit*() so a test can drive the
// connection lifecycle deterministically (no real SSH server involved).
//
// Declared via vi.hoisted because vi.mock factories are hoisted above ordinary
// top-level declarations — referencing a plain `class` here would throw
// "Cannot access 'FakeClient' before initialization".
const { FakeClient } = vi.hoisted(() => {
  class FakeClientImpl {
    static instances: FakeClientImpl[] = []
    static last(): FakeClientImpl { return FakeClientImpl.instances[FakeClientImpl.instances.length - 1] }

    handlers: Record<string, Array<(...a: unknown[]) => void>> = {}
    connectArgs: Record<string, unknown> | null = null
    ended = false
    /** targetPorts this client was asked to forward to, in order. */
    forwardedTo: number[] = []
    /**
     * Reverse-forward requests made via forwardIn(), in order. The module must
     * always ask for loopback — the server ignores the address and binds
     * 127.0.0.1 anyway, but the client contract is explicit.
     */
    reverseForwardedTo: Array<{ bindAddr: string; bindPort: number }> = []
    /** Ports passed to unforwardIn(), in order. */
    unforwarded: number[] = []
    /** When set, forwardIn() fails with this error instead of succeeding. */
    reverseForwardError: Error | null = null
    /** When set, forwardIn() reports this as the actually-allocated port. */
    reverseAllocatedPort: number | null = null
    /**
     * When true, forwardIn() defers its callback until releaseForwardIn() is
     * called, so a test can act inside the request window (real ssh2 does a
     * network round trip here; the fake would otherwise complete synchronously).
     */
    deferReverseForward = false
    private pendingReverseCallbacks: Array<() => void> = []

    /** Complete every deferred forwardIn() callback. */
    releaseForwardIn(): void {
      const cbs = this.pendingReverseCallbacks
      this.pendingReverseCallbacks = []
      for (const cb of cbs) cb()
    }
    /**
     * The channel streams handed back by forwardOut(), in order. A test can
     * emit('error') on one to simulate the SSH channel dying mid-transfer —
     * the case that used to crash the main process.
     */
    streams: import('node:stream').PassThrough[] = []

    constructor() { FakeClientImpl.instances.push(this) }

    on(event: string, cb: (...a: unknown[]) => void): this {
      ;(this.handlers[event] ||= []).push(cb)
      return this
    }

    connect(args: Record<string, unknown>): this {
      this.connectArgs = args
      return this
    }
    end(): void { this.ended = true }

    forwardOut(
      _srcHost: string, _srcPort: number, _dstHost: string, dstPort: number,
      cb: (err: Error | undefined, stream: unknown) => void,
    ): void {
      this.forwardedTo.push(dstPort)
      // A real Duplex so the module's `socket.pipe(stream).pipe(socket)` wiring
      // is structurally valid; the tests only assert on listener reachability,
      // not on bytes flowing through.
      const stream = new PassThrough()
      this.streams.push(stream)
      cb(undefined, stream)
    }

    forwardIn(bindAddr: string, bindPort: number, cb: (err?: Error, realPort?: number) => void): void {
      this.reverseForwardedTo.push({ bindAddr, bindPort })
      const fire = () => {
        if (this.reverseForwardError) {
          cb(this.reverseForwardError)
          return
        }
        cb(undefined, this.reverseAllocatedPort ?? bindPort)
      }
      if (this.deferReverseForward) this.pendingReverseCallbacks.push(fire)
      else fire()
    }

    unforwardIn(_bindAddr: string, bindPort: number, cb?: () => void): void {
      this.unforwarded.push(bindPort)
      cb?.()
    }

    /** Drive an incoming server-side connection for a reverse forward. */
    emitTcpConnection(destPort: number, accept: () => unknown, reject: () => void): void {
      this.emit('tcp connection', { destIP: '127.0.0.1', destPort, origIP: '127.0.0.1', origPort: 50000 }, accept, reject)
    }

    emit(event: string, ...args: unknown[]): void {
      for (const cb of this.handlers[event] || []) cb(...args)
    }
  }
  return { FakeClient: FakeClientImpl }
})

vi.mock('ssh2', () => ({ Client: FakeClient }))

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

// ssh/info is fetched over HTTP(S) before connecting. Stub BOTH schemes so the
// suite never touches a real server (and so a reachable local server cannot
// accidentally make assertions pass).
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
  addForwardedPort, removeForwardedPort, reconnectTunnel, ensureTunnel,
  getForwardedPorts, isTunnelConnected, testPortReachable, disconnectTunnel,
  addReverseForwardedPort,
} from './tunnel'

const PORT_A = 28901
const PORT_B = 28902

/**
 * Server-side sockets accepted by tunnel.ts's listeners, in order. Lets a test
 * drive an error on the socket the module actually wired up.
 */
const acceptedSockets: import('node:net').Socket[] = []

/** Bring the module to a connected state via a single fake client. */
async function connectOnce() {
  const p = ensureTunnel()
  await vi.waitFor(() => expect(FakeClient.instances.length).toBeGreaterThan(0))
  const c = FakeClient.last()
  c.emit('ready')
  await p
  return c
}

/**
 * The module keeps connection + desired-forward state in module scope, so each
 * test must start from a clean slate or `ensureTunnel()` short-circuits on the
 * previous test's still-"connected" client.
 */
function resetModule(): void {
  disconnectTunnel()
  for (const p of getForwardedPorts()) removeForwardedPort(p.port)
}

beforeEach(() => {
  initStore()
  resetModule()
  acceptedSockets.length = 0
  dials.length = 0
  disarmFakeReverseDial()
  FakeClient.instances = []
  storeData.serverUrl = 'https://127.0.0.1:20000'
  listenDelay.ms = 0
})

afterEach(() => {
  // Release any real listeners this test bound so ports do not leak across tests.
  for (const p of getForwardedPorts()) removeForwardedPort(p.port)
})

describe('tunnel: reconnect preserves and rebuilds forwards', () => {
  it('keeps the mapping across reconnectTunnel and makes the port reachable again', async () => {
    await connectOnce()
    expect(await addForwardedPort(PORT_A, 20000, '')).toBe(true)
    expect(await testPortReachable(PORT_A)).toBe(true)

    const reconnected = reconnectTunnel()
    // reconnectTunnel disconnects then reconnects: drive the new client ready.
    await vi.waitFor(() => expect(FakeClient.instances.length).toBeGreaterThan(1))
    FakeClient.last().emit('ready')
    expect(await reconnected).toBe(true)

    // Regression guard: the old code cleared state.forwarded in
    // disconnectTunnel(), so this came back as [] and the port was dead.
    expect(getForwardedPorts()).toEqual([{ port: PORT_A, host: '', direction: 'forward' }])
    expect(await testPortReachable(PORT_A)).toBe(true)
  })

  it('does not resurrect a forward removed DURING the rebuild window', async () => {
    await connectOnce()
    await addForwardedPort(PORT_A, 20000, '')
    await addForwardedPort(PORT_B, 20000, '')
    disconnectTunnel()
    expect(getForwardedPorts().length).toBe(2)

    // Widen the window between rebuildAllForwards() snapshotting the desired
    // set and each individual bind completing.
    listenDelay.ms = 60
    const reconnected = ensureTunnel()
    await vi.waitFor(() => expect(FakeClient.instances.length).toBeGreaterThan(1))
    FakeClient.last().emit('ready')
    // A's bind is now in flight; delete B while the rebuild still holds it.
    await new Promise(r => setTimeout(r, 30))
    removeForwardedPort(PORT_B)
    await reconnected
    listenDelay.ms = 0

    // Regression guard: the snapshot-only loop used to bind B anyway, leaving a
    // live listener for a port that no longer exists in the desired set.
    expect(getForwardedPorts()).toEqual([{ port: PORT_A, host: '', direction: 'forward' }])
    expect(await testPortReachable(PORT_B)).toBe(false)
    expect(await testPortReachable(PORT_A)).toBe(true)
  })

  it('a failed concurrent bind does not destroy the winning listener', async () => {
    await connectOnce()
    listenDelay.ms = 50
    // Two adds for the SAME local port. The loser used to hit EADDRINUSE and
    // call closeForwardServer(), which deleted the winner's entry — leaving the
    // port dead while state.forwarded still claimed it was forwarded.
    const [first, second] = await Promise.all([
      addForwardedPort(PORT_A, 20000, ''),
      addForwardedPort(PORT_A, 20000, ''),
    ])
    listenDelay.ms = 0

    expect(first).toBe(true)
    expect(second).toBe(true)
    expect(getForwardedPorts()).toEqual([{ port: PORT_A, host: '', direction: 'forward' }])
    expect(await testPortReachable(PORT_A)).toBe(true)
  })

  it('does not resurrect a forward removed before the reconnect', async () => {
    await connectOnce()
    await addForwardedPort(PORT_A, 20000, '')
    await addForwardedPort(PORT_B, 20000, '')
    removeForwardedPort(PORT_A)

    const reconnected = reconnectTunnel()
    await vi.waitFor(() => expect(FakeClient.instances.length).toBeGreaterThan(1))
    FakeClient.last().emit('ready')
    await reconnected

    expect(getForwardedPorts()).toEqual([{ port: PORT_B, host: '', direction: 'forward' }])
    expect(await testPortReachable(PORT_A)).toBe(false)
    expect(await testPortReachable(PORT_B)).toBe(true)
  })
})

describe('tunnel: concurrent callers share one connection', () => {
  it('ensureTunnel single-flights instead of cancelling the in-flight attempt', async () => {
    const p1 = ensureTunnel()
    const p2 = ensureTunnel()
    await vi.waitFor(() => expect(FakeClient.instances.length).toBe(1))

    const c = FakeClient.last()
    c.emit('ready')

    expect(await Promise.all([p1, p2])).toEqual([true, true])
    // Exactly one client: a second connect would have called end() on the first
    // and left p1 pending forever (the original hang).
    expect(FakeClient.instances.length).toBe(1)
    expect(isTunnelConnected()).toBe(true)
  })

  it('adds two forwards concurrently and both become reachable', async () => {
    const connecting = ensureTunnel()
    await vi.waitFor(() => expect(FakeClient.instances.length).toBe(1))
    FakeClient.last().emit('ready')
    await connecting

    // Mirrors syncToNative() fanning out over every enabled port.
    const results = await Promise.all([
      addForwardedPort(PORT_A, 20000, ''),
      addForwardedPort(PORT_B, 20000, ''),
    ])

    expect(results).toEqual([true, true])
    expect(await testPortReachable(PORT_A)).toBe(true)
    expect(await testPortReachable(PORT_B)).toBe(true)
  })
})

describe('tunnel: failure handling', () => {
  it('reports failure and can recover on a later connect', async () => {
    const first = ensureTunnel()
    await vi.waitFor(() => expect(FakeClient.instances.length).toBe(1))
    const err = Object.assign(new Error('auth failed'), { level: 'client-authentication' })
    FakeClient.last().emit('error', err)

    expect(await first).toBe(false)
    expect(isTunnelConnected()).toBe(false)

    // A subsequent attempt must actually try again, not return a cached failure.
    const second = ensureTunnel()
    await vi.waitFor(() => expect(FakeClient.instances.length).toBe(2))
    FakeClient.last().emit('ready')
    expect(await second).toBe(true)
    expect(isTunnelConnected()).toBe(true)
  })

  it('settles false when the client never becomes ready (timeout guard)', async () => {
    vi.useFakeTimers()
    try {
      const p = ensureTunnel()
      // ensureTunnel awaits the ssh/info prelude before openClient registers its
      // timer, so flush microtasks until the client exists — advancing the clock
      // before that would fire nothing and leave the promise pending forever
      // (which would also poison `connecting` for every later test).
      for (let i = 0; i < 20 && FakeClient.instances.length === 0; i++) {
        await vi.advanceTimersByTimeAsync(0)
      }
      expect(FakeClient.instances.length).toBe(1)

      // No 'ready' and no 'error' — without the timeout this promise would hang.
      await vi.advanceTimersByTimeAsync(20001)
      expect(await p).toBe(false)
      expect(isTunnelConnected()).toBe(false)
    } finally {
      vi.useRealTimers()
    }
  })

  it('a timed-out attempt is abandoned, so a late ready cannot flip the state', async () => {
    vi.useFakeTimers()
    try {
      const p = ensureTunnel()
      for (let i = 0; i < 20 && FakeClient.instances.length === 0; i++) {
        await vi.advanceTimersByTimeAsync(0)
      }
      const timedOut = FakeClient.last()
      await vi.advanceTimersByTimeAsync(20001)
      expect(await p).toBe(false)
      // The socket must be released, not left half-open.
      expect(timedOut.ended).toBe(true)

      // A connection completing after the deadline must NOT report the module as
      // connected — the caller was already told the attempt failed.
      timedOut.emit('ready')
      await vi.advanceTimersByTimeAsync(0)
      expect(isTunnelConnected()).toBe(false)
    } finally {
      vi.useRealTimers()
    }
  })

  it('keeps desired forwards but drops listeners when the client closes', async () => {
    const c = await connectOnce()
    await addForwardedPort(PORT_A, 20000, '')
    expect(await testPortReachable(PORT_A)).toBe(true)

    c.emit('close')
    expect(isTunnelConnected()).toBe(false)
    // Listener is gone, but the intent survives so a reconnect can restore it.
    expect(await testPortReachable(PORT_A)).toBe(false)
    expect(getForwardedPorts()).toEqual([{ port: PORT_A, host: '', direction: 'forward' }])

    const reconnected = ensureTunnel()
    await vi.waitFor(() => expect(FakeClient.instances.length).toBeGreaterThan(1))
    FakeClient.last().emit('ready')
    expect(await reconnected).toBe(true)
    expect(await testPortReachable(PORT_A)).toBe(true)
  })

  it('a superseded client closing does not tear down the new connection', async () => {
    const old = await connectOnce()
    await addForwardedPort(PORT_A, 20000, '')

    const reconnected = reconnectTunnel()
    await vi.waitFor(() => expect(FakeClient.instances.length).toBeGreaterThan(1))
    const fresh = FakeClient.last()
    fresh.emit('ready')
    await reconnected

    // The old client's close arrives late (real ssh2 emits it asynchronously).
    old.emit('close')

    expect(isTunnelConnected()).toBe(true)
    expect(await testPortReachable(PORT_A)).toBe(true)
  })
})

describe('tunnel: listener bookkeeping', () => {
  it('releases the local port when a forward is removed', async () => {
    await connectOnce()
    await addForwardedPort(PORT_A, 20000, '')
    expect(await testPortReachable(PORT_A)).toBe(true)

    removeForwardedPort(PORT_A)
    expect(await testPortReachable(PORT_A)).toBe(false)
    expect(getForwardedPorts()).toEqual([])
  })

  it('re-adding the same local port does not fail with EADDRINUSE', async () => {
    await connectOnce()
    expect(await addForwardedPort(PORT_A, 20000, '')).toBe(true)
    expect(await addForwardedPort(PORT_A, 30000, '')).toBe(true)
    expect(await testPortReachable(PORT_A)).toBe(true)
  })

  it('testPortReachable reports false for a port nothing listens on', async () => {
    expect(await testPortReachable(28999)).toBe(false)
  })

  it('exposes the target host it was registered with', async () => {
    await connectOnce()
    await addForwardedPort(PORT_A, 20000, '192.168.1.10')
    expect(getForwardedPorts()).toEqual([{ port: PORT_A, host: '192.168.1.10', direction: 'forward' }])
  })
})

describe('tunnel: a dying forwarded socket must not crash the process', () => {
  /**
   * An 'error' event with no listener is re-thrown as an UNCAUGHT exception on
   * the next tick, so `expect(fn).not.toThrow()` cannot see it — the emit
   * returns normally and the process blows up afterwards. That is exactly how
   * this bug reached production, so the assertion has to observe the same
   * channel the process does: a real uncaughtException.
   *
   * Returns the errors that escaped during `action`.
   */
  async function collectUncaught(action: () => Promise<void> | void): Promise<Error[]> {
    const seen: Error[] = []
    const handler = (e: Error) => { seen.push(e) }
    process.on('uncaughtException', handler)
    try {
      await action()
      // The throw is scheduled on a later tick; give it a chance to land.
      await new Promise((r) => setTimeout(r, 20))
    } finally {
      process.off('uncaughtException', handler)
    }
    return seen
  }

  /** Bring a forward up and open a real client so the pipe chain is live. */
  async function openLiveForward() {
    await connectOnce()
    await addForwardedPort(PORT_A, 20000, '')
    const client = net.connect(PORT_A, '127.0.0.1')
    await new Promise<void>((r) => client.on('connect', () => r()))
    await vi.waitFor(() => expect(FakeClient.last().streams.length).toBeGreaterThan(0))
    await vi.waitFor(() => expect(acceptedSockets.length).toBeGreaterThan(0))
    return client
  }

  it('handles ECONNRESET on the SSH channel instead of crashing', async () => {
    // Regression: the wiring was a bare `socket.pipe(stream).pipe(socket)`.
    // pipe() does not forward errors, so an ECONNRESET from either end (the
    // SSH channel being torn down mid-transfer) surfaced as an UNCAUGHT
    // exception. Electron's default handler then showed the modal
    // "A JavaScript error occurred in the main process" dialog and the app was
    // stuck on an error the user could do nothing about.
    const client = await openLiveForward()
    const stream = FakeClient.last().streams[0]

    const escaped = await collectUncaught(() => {
      stream.emit('error', Object.assign(new Error('read ECONNRESET'), { code: 'ECONNRESET' }))
    })

    expect(escaped.map((e) => e.message)).toEqual([])
    client.destroy()
  })

  it('handles an error on the local socket instead of crashing', async () => {
    // The reverse direction: the browser drops the connection (its own RST)
    // while the SSH channel is still open. The module's socket is the one the
    // server accepted — emitting on the test's client would prove nothing.
    const client = await openLiveForward()

    const escaped = await collectUncaught(() => {
      acceptedSockets[0].emit('error', Object.assign(new Error('write EPIPE'), { code: 'EPIPE' }))
    })

    expect(escaped.map((e) => e.message)).toEqual([])
    client.destroy()
  })
})

// Sanity check that the fake really does back a real listener, so the
// reachability assertions above are not vacuous.
describe('tunnel: test harness sanity', () => {
  it('a plain net server is detectable by testPortReachable', async () => {
    const server = net.createServer()
    await new Promise<void>(r => server.listen(PORT_B, '127.0.0.1', () => r()))
    expect(await testPortReachable(PORT_B)).toBe(true)
    await new Promise<void>(r => server.close(() => r()))
    expect(await testPortReachable(PORT_B)).toBe(false)
  })
})

describe('tunnel: keepalive (parity with Android JSch setServerAliveInterval)', () => {
  it('enables SSH keepalive on connect', async () => {
    await connectOnce()
    const args = FakeClient.last().connectArgs as Record<string, unknown>
    // Without this the tunnel is silently dropped by idle NAT/firewall timeouts
    // and nothing restores it — the reported "PC port mapping does not work".
    expect(args.keepaliveInterval).toBe(30000)
    expect(args.keepaliveCountMax).toBe(3)
  })
})

describe('tunnel: reverse port forwarding (ssh -R)', () => {
  it('asks the server to bind loopback on the requested port', async () => {
    await connectOnce()
    expect(await addReverseForwardedPort(PORT_A, 3000, '')).toBe(true)

    const req = FakeClient.last().reverseForwardedTo
    expect(req).toEqual([{ bindAddr: '127.0.0.1', bindPort: PORT_A }])
    // The server ignores the address and binds 127.0.0.1 regardless, but the
    // client must never ask for a wider bind.
    expect(req[0].bindAddr).toBe('127.0.0.1')
    expect(getForwardedPorts()).toEqual([{ port: PORT_A, host: '', direction: 'reverse' }])
  })

  it('records the port the server actually allocated', async () => {
    await connectOnce()
    const c = FakeClient.last()
    // Ask for 0; the server picks one and reports it back in the reply.
    c.reverseAllocatedPort = PORT_B
    expect(await addReverseForwardedPort(0, 3000, '')).toBe(true)

    // Removal must unforward the REAL port, not the requested 0.
    removeForwardedPort(0)
    expect(c.unforwarded).toEqual([PORT_B])
  })

  it('reports failure when the server rejects the request', async () => {
    await connectOnce()
    FakeClient.last().reverseForwardError = new Error('request denied')
    expect(await addReverseForwardedPort(PORT_A, 3000, '')).toBe(false)
  })

  it('does not register a reverse forward removed during the request', async () => {
    await connectOnce()
    const c = FakeClient.last()
    // Hold the forwardIn callback open so the removal lands inside the request
    // window, which is what a real (network round trip) forwardIn looks like.
    c.deferReverseForward = true
    const p = addReverseForwardedPort(PORT_A, 3000, '')
    removeForwardedPort(PORT_A)
    c.releaseForwardIn()

    expect(await p).toBe(false)
    expect(c.unforwarded).toEqual([PORT_A])
    expect(getForwardedPorts()).toEqual([])
  })

  it('unforwards on removeForwardedPort', async () => {
    await connectOnce()
    await addReverseForwardedPort(PORT_A, 3000, '')
    const c = FakeClient.last()

    removeForwardedPort(PORT_A)
    expect(c.unforwarded).toEqual([PORT_A])
    expect(getForwardedPorts()).toEqual([])
  })

  it('unforwards on disconnectTunnel so the server releases promptly', async () => {
    await connectOnce()
    await addReverseForwardedPort(PORT_A, 3000, '')
    const c = FakeClient.last()

    disconnectTunnel()
    expect(c.unforwarded).toEqual([PORT_A])
    // Intent survives so a reconnect restores the mapping.
    expect(getForwardedPorts()).toEqual([{ port: PORT_A, host: '', direction: 'reverse' }])
  })

  it('re-registers the reverse forward after a reconnect', async () => {
    await connectOnce()
    await addReverseForwardedPort(PORT_A, 3000, '')

    const reconnected = reconnectTunnel()
    await vi.waitFor(() => expect(FakeClient.instances.length).toBeGreaterThan(1))
    const fresh = FakeClient.last()
    fresh.emit('ready')
    await reconnected

    // Regression guard: a reconnect that skipped reverse entries would leave the
    // server-side port unbound while the UI still showed it as configured.
    expect(fresh.reverseForwardedTo).toEqual([{ bindAddr: '127.0.0.1', bindPort: PORT_A }])
    expect(getForwardedPorts()).toEqual([{ port: PORT_A, host: '', direction: 'reverse' }])
  })

  it('clears stale reverse bookkeeping when the connection drops', async () => {
    const c = await connectOnce()
    await addReverseForwardedPort(PORT_A, 3000, '')

    c.emit('close')

    // The server freed its listeners with the connection, so reconnecting must
    // re-register cleanly rather than trying to unforward dead ports.
    const reconnected = ensureTunnel()
    await vi.waitFor(() => expect(FakeClient.instances.length).toBeGreaterThan(1))
    const fresh = FakeClient.last()
    fresh.emit('ready')
    await reconnected
    expect(fresh.reverseForwardedTo).toEqual([{ bindAddr: '127.0.0.1', bindPort: PORT_A }])
  })

  it('pipes an incoming server connection to the local target', async () => {
    armFakeReverseDial('127.0.0.1', 3000)
    await connectOnce()
    await addReverseForwardedPort(PORT_A, 3000, '127.0.0.1')
    const c = FakeClient.last()

    const stream = new PassThrough()
    const accept = vi.fn(() => stream)
    const reject = vi.fn()
    c.emitTcpConnection(PORT_A, accept, reject)

    // The module must dial the registered target and splice the two ends.
    await vi.waitFor(() => expect(accept).toHaveBeenCalled())
    expect(dials).toEqual([{ port: 3000, host: '127.0.0.1' }])
    expect(reject).not.toHaveBeenCalled()
    stream.destroy()
  })

  it('rejects a connection for an unknown server port', async () => {
    await connectOnce()
    const c = FakeClient.last()

    const accept = vi.fn(() => new PassThrough())
    const reject = vi.fn()
    c.emitTcpConnection(28999, accept, reject)

    // A spurious incoming connection must be refused, not accepted.
    expect(reject).toHaveBeenCalled()
    expect(accept).not.toHaveBeenCalled()
  })

  it('keeps the monitor armed for reverse-only forwards', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    try {
      const c = await connectOnce()
      await addReverseForwardedPort(PORT_A, 3000, '')
      expect(getForwardedPorts()).toEqual([{ port: PORT_A, host: '', direction: 'reverse' }])

      c.emit('close')
      await vi.advanceTimersByTimeAsync(15001)
      // The monitor must treat a reverse-only desired set as worth maintaining.
      expect(FakeClient.instances.length).toBeGreaterThan(1)
      FakeClient.last().emit('ready')
      await vi.advanceTimersByTimeAsync(0)
      expect(isTunnelConnected()).toBe(true)
    } finally {
      vi.useRealTimers()
    }
  })
})

describe('tunnel: connection monitor auto-reconnects', () => {
  // The monitor interval must be CREATED while fake timers are installed, or it
  // is a real timer that advanceTimersByTimeAsync() cannot reach. Enabling
  // shouldAdvanceTime keeps libuv socket I/O (the real bind + probe) moving.
  async function connectAndMap() {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const c = await connectOnce()
    await addForwardedPort(PORT_A, 20000, '')
    expect(await testPortReachable(PORT_A)).toBe(true)
    return c
  }

  it('re-establishes the tunnel and its forwards after an unexpected drop', async () => {
    const c = await connectAndMap()
    try {
      // Simulate the SSH connection dying (network blip / idle timeout).
      c.emit('close')
      expect(isTunnelConnected()).toBe(false)

      // The monitor polls every 15s and reconnects on its own.
      await vi.advanceTimersByTimeAsync(15001)
      expect(FakeClient.instances.length).toBeGreaterThan(1)
      FakeClient.last().emit('ready')
      await vi.advanceTimersByTimeAsync(0)
      vi.useRealTimers()

      // Both the tunnel and the forward are back without any user action.
      expect(isTunnelConnected()).toBe(true)
      expect(getForwardedPorts()).toEqual([{ port: PORT_A, host: '', direction: 'forward' }])
      expect(await testPortReachable(PORT_A)).toBe(true)
    } finally {
      vi.useRealTimers()
    }
  })

  it('does not reconnect after an explicit disconnectTunnel()', async () => {
    await connectAndMap()
    try {
      const before = FakeClient.instances.length
      // An explicit teardown means "stop maintaining this".
      disconnectTunnel()
      await vi.advanceTimersByTimeAsync(300001)
      expect(FakeClient.instances.length).toBe(before)
      expect(isTunnelConnected()).toBe(false)
    } finally {
      vi.useRealTimers()
    }
  })

  it('stops monitoring once the last forward is removed', async () => {
    const c = await connectAndMap()
    try {
      removeForwardedPort(PORT_A)
      c.emit('close')
      const before = FakeClient.instances.length
      await vi.advanceTimersByTimeAsync(300001)
      // Nothing left to maintain → no reconnect attempts.
      expect(FakeClient.instances.length).toBe(before)
    } finally {
      vi.useRealTimers()
    }
  })

  it('backs off instead of hammering the server on repeated failures', async () => {
    const c = await connectAndMap()
    try {
      c.emit('close')
      const before = FakeClient.instances.length

      // First tick fires attempt #1.
      await vi.advanceTimersByTimeAsync(15001)
      const afterFirst = FakeClient.instances.length
      expect(afterFirst).toBe(before + 1)

      // Fail it so the backoff gate applies...
      FakeClient.last().emit('error', new Error('ECONNREFUSED'))
      // ...and a tick arriving 15s later (> 5s backoff) makes exactly one more.
      await vi.advanceTimersByTimeAsync(15001)
      expect(FakeClient.instances.length).toBe(afterFirst + 1)
    } finally {
      vi.useRealTimers()
    }
  })
})
