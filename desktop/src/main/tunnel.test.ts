import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import net from 'node:net'
import { PassThrough } from 'node:stream'

// Test-only knob: delays the `listen` callback so the rebuild window (the span
// between the desired-set snapshot and an actual bind) is observable. Real
// binds complete too fast to hit the mid-rebuild removal race deterministically.
const { listenDelay } = vi.hoisted(() => ({ listenDelay: { ms: 0 } }))

vi.mock('node:net', async (importOriginal) => {
  const actual = await importOriginal<typeof import('node:net')>()
  const realCreateServer = actual.createServer as unknown as (...a: unknown[]) => net.Server
  const createServer = (...args: unknown[]) => {
    const server = realCreateServer(...args)
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
  // `tunnel.ts` does `import net from 'node:net'`, so the default export is what
  // it reads — keep it in sync with the named one.
  return { ...actual, createServer, default: { ...actual, createServer } }
})

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
      cb(undefined, new PassThrough())
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
} from './tunnel'

const PORT_A = 28901
const PORT_B = 28902

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
    expect(getForwardedPorts()).toEqual([{ port: PORT_A, host: '' }])
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
    expect(getForwardedPorts()).toEqual([{ port: PORT_A, host: '' }])
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
    expect(getForwardedPorts()).toEqual([{ port: PORT_A, host: '' }])
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

    expect(getForwardedPorts()).toEqual([{ port: PORT_B, host: '' }])
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
    expect(getForwardedPorts()).toEqual([{ port: PORT_A, host: '' }])

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
    expect(getForwardedPorts()).toEqual([{ port: PORT_A, host: '192.168.1.10' }])
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
      expect(getForwardedPorts()).toEqual([{ port: PORT_A, host: '' }])
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
