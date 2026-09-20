import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import net from 'node:net'
import { PassThrough } from 'node:stream'

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
