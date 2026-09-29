import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

/**
 * Fake `node:http2` + `./clientLog` for the h2 transport.
 *
 * The fake session/stream are deliberately hand-rolled emitters rather than
 * subclasses of `node:stream`'s Duplex: `vi.mock` factories run before the test
 * module's imports, so a hoisted class cannot extend an imported base without
 * hitting the temporal dead zone. h2Transport only ever drives a duck-typed
 * surface (`on/once/emit/write/end/destroy/pause/resume/request`), so the
 * minimal emitter below is both sufficient and fully controllable.
 *
 * The REAL Duplex that h2Transport builds around the fake is what the tests
 * assert on for backpressure/half-close — the wrapper is production code, only
 * its wire peer is faked.
 */
const { h2State } = vi.hoisted(() => {
  class Emitter {
    handlers: Record<string, Array<(...a: unknown[]) => void>> = {}
    on(ev: string, cb: (...a: unknown[]) => void): this {
      ;(this.handlers[ev] ||= []).push(cb)
      return this
    }
    once(ev: string, cb: (...a: unknown[]) => void): this {
      const wrap = (...a: unknown[]) => { this.off(ev, wrap); cb(...a) }
      return this.on(ev, wrap)
    }
    off(ev: string, cb: (...a: unknown[]) => void): this {
      const arr = this.handlers[ev]
      if (arr) {
        const i = arr.indexOf(cb)
        if (i >= 0) arr.splice(i, 1)
      }
      return this
    }
    removeListener(ev: string, cb: (...a: unknown[]) => void): this { return this.off(ev, cb) }
    listenerCount(ev: string): number { return (this.handlers[ev] || []).length }
    emit(ev: string, ...a: unknown[]): void {
      for (const cb of [...(this.handlers[ev] || [])]) cb(...a)
    }
  }

  class FakeStream extends Emitter {
    headers: Record<string, unknown>
    /** Every chunk the transport wrote into the request body, in order. */
    writes: Buffer[] = []
    ended = false
    destroyed = false
    paused = false
    resumes = 0
    /** When true, write() returns false and the caller must wait for 'drain'. */
    holdWrites = false
    status = 200

    constructor(headers: Record<string, unknown>) {
      super()
      this.headers = headers
    }

    write(chunk: unknown): boolean {
      this.writes.push(Buffer.from(chunk as Buffer))
      return !this.holdWrites
    }
    end(): void { this.ended = true }
    destroy(err?: Error): void {
      this.destroyed = true
      if (err) this.emit('error', err)
      // Real streams emit 'close' on destroy (no 'end'), which the transport
      // relies on to finish the readable side after an RST.
      this.emit('close')
    }
    pause(): void { this.paused = true }
    resume(): void { this.paused = false; this.resumes++ }
    /** Simulate the server writing a chunk of the response body. */
    feed(chunk: string): void { this.emit('data', Buffer.from(chunk)) }
    /** Simulate the server half-closing (response END_STREAM). */
    finishRemote(): void { this.emit('end') }
    /** Simulate the response headers arriving. */
    respond(): void { this.emit('response', { ':status': this.status }) }
  }

  class FakeSession extends Emitter {
    authority: string
    options: Record<string, unknown> | undefined
    kind: 'tls' | 'h2c'
    requests: FakeStream[] = []
    closed = false
    destroyed = false
    closeCalls = 0
    destroyCalls = 0
    /** Every keepalive PING issued on this session. */
    pingCalls: Array<{ answered: boolean }> = []
    /**
     * When true, `ping()` never invokes its callback — exactly what a
     * black-holed (half-open) peer does: no PONG, and no 'error'/'close' either.
     */
    dropPings = false

    constructor(authority: string, options?: Record<string, unknown>) {
      super()
      this.authority = authority
      this.options = options
      this.kind = authority.startsWith('https:') ? 'tls' : 'h2c'
    }

    /**
     * Mirror `session.ping(callback)`: the callback runs on the next tick when
     * the PONG arrives, and NEVER runs when it does not (the transport's own
     * timeout is what must catch that — see startHeartbeat).
     */
    ping(cb?: (err: Error | null) => void): void {
      const entry = { answered: false }
      this.pingCalls.push(entry)
      if (this.dropPings) return
      entry.answered = true
      queueMicrotask(() => cb?.(null))
    }

    request(headers: Record<string, unknown>): FakeStream {
      const st = new FakeStream(headers)
      this.requests.push(st)
      const behavior = h2State.behavior[this.kind]
      if (behavior === 'error') {
        // Reject the way a wrong-version TLS peer / non-h2 server does.
        queueMicrotask(() => this.emit('error', Object.assign(new Error('h2 error'), { code: h2State.errorCode })))
        return st
      }
      if (behavior === 'hang') return st
      queueMicrotask(() => st.respond())
      return st
    }

    close(): void {
      this.closed = true
      this.closeCalls++
    }

    /**
     * Real `destroy()` tears the session down immediately (and emits 'close'),
     * unlike the graceful `close()` which waits for open streams. The
     * transport uses destroy() for teardown, so the fake must mirror it.
     */
    destroy(): void {
      this.destroyed = true
      this.destroyCalls++
      // Every stream on a torn-down session is closed. Without this the
      // transport's pending verify request would dangle (and the harness's
      // async-leak detector would flag it).
      for (const st of this.requests) st.emit('close')
      this.emit('close')
    }
  }

  const h2State = {
    /** Per-transport behaviour for the next session created. */
    behavior: { tls: 'ok', h2c: 'ok' } as Record<'tls' | 'h2c', 'ok' | 'error' | 'hang'>,
    errorCode: 'ERR_HTTP2_ERROR',
    sessions: [] as FakeSession[],
    connectCalls: [] as Array<{ authority: string; options?: Record<string, unknown> }>,
    cookie: null as string | null,
    logs: [] as unknown[][],
    connect(authority: string, options?: Record<string, unknown>): FakeSession {
      h2State.connectCalls.push({ authority, options })
      const s = new FakeSession(authority, options)
      h2State.sessions.push(s)
      return s
    },
    lastSession(): FakeSession { return h2State.sessions[h2State.sessions.length - 1] },
    reset(): void {
      h2State.behavior = { tls: 'ok', h2c: 'ok' }
      h2State.errorCode = 'ERR_HTTP2_ERROR'
      h2State.sessions = []
      h2State.connectCalls = []
      h2State.cookie = null
      h2State.logs = []
    },
  }

  return { h2State }
})

vi.mock('node:http2', () => ({
  default: {
    connect: (authority: string, options?: Record<string, unknown>) => h2State.connect(authority, options),
  },
}))

vi.mock('./clientLog', () => ({
  getSessionCookie: async () => h2State.cookie,
  record: (...args: unknown[]) => { h2State.logs.push(args) },
}))

import {
  connect, openStream, openClaimStream, openControlStream, close,
  isConnected, getTransportKind, _resetForTesting, _hasHeartbeatForTesting,
} from './h2Transport'

/** Let queued microtasks (cookie read, response event) run. */
const tick = (): Promise<void> => new Promise((r) => setImmediate(r))

beforeEach(() => {
  h2State.reset()
  _resetForTesting()
})

// `_resetForTesting()` tears the session (and thus the keepalive) down after
// every test, so the heartbeat timer a connecting test arms does not outlive it
// and trip the harness's async-leak detector.
afterEach(() => { _resetForTesting() })

describe('h2Transport: connect', () => {
  it('connects over h2c with prior knowledge (plain http authority)', async () => {
    const res = await connect({ host: '127.0.0.1', port: 20000, prefer: 'h2c' })

    expect(res).toEqual({ ok: true, kind: 'h2c', error: '' })
    expect(h2State.connectCalls.map(c => c.authority)).toEqual(['http://127.0.0.1:20000'])
    expect(isConnected()).toBe(true)
    expect(getTransportKind()).toBe('h2c')
  })

  it('connects over h2-over-TLS with rejectUnauthorized:false', async () => {
    const res = await connect({ host: '127.0.0.1', port: 20000, prefer: 'tls' })

    expect(res.ok).toBe(true)
    expect(res.kind).toBe('tls')
    const call = h2State.connectCalls[0]
    expect(call.authority).toBe('https://127.0.0.1:20000')
    // A self-hosted instance commonly uses a self-signed cert; refusing it
    // would drop the tunnel on exactly those installs.
    expect(call.options).toMatchObject({ rejectUnauthorized: false })
  })

  it('falls back tls -> h2c when the TLS attempt fails', async () => {
    h2State.behavior.tls = 'error'
    h2State.errorCode = 'ERR_SSL_WRONG_VERSION_NUMBER'

    const res = await connect({ host: '127.0.0.1', port: 20000, prefer: 'tls' })

    expect(res).toEqual({ ok: true, kind: 'h2c', error: '' })
    expect(h2State.connectCalls.map(c => c.authority)).toEqual([
      'https://127.0.0.1:20000',
      'http://127.0.0.1:20000',
    ])
  })

  it('falls back h2c -> tls when h2c is preferred but fails', async () => {
    h2State.behavior.h2c = 'error'

    const res = await connect({ host: '127.0.0.1', port: 20000, prefer: 'h2c' })

    expect(res.ok).toBe(true)
    expect(res.kind).toBe('tls')
    expect(h2State.connectCalls.map(c => c.authority)).toEqual([
      'http://127.0.0.1:20000',
      'https://127.0.0.1:20000',
    ])
  })

  it('reports failure when every transport fails', async () => {
    h2State.behavior.tls = 'error'
    h2State.behavior.h2c = 'error'

    const res = await connect({ host: '127.0.0.1', port: 20000, prefer: 'tls' })

    expect(res.ok).toBe(false)
    expect(res.error).not.toBe('')
    expect(isConnected()).toBe(false)
    expect(getTransportKind()).toBeNull()
  })

  it('releases the readiness-probe stream once it has the status line', async () => {
    await connect({ host: '127.0.0.1', port: 20000, prefer: 'h2c' })

    // requests[0] is the /api/ssh/info probe. Its body is never read, so
    // without an explicit release it would stay open for the life of the
    // session and a later teardown would wait on it (measured against a real
    // server: the session never closed).
    const probe = h2State.sessions[0].requests[0]
    expect(probe.headers[':path']).toBe('/api/ssh/info')
    expect(probe.destroyed).toBe(true)
  })

  it('reuses the existing session instead of reconnecting', async () => {
    const first = await connect({ host: '127.0.0.1', port: 20000, prefer: 'h2c' })
    const second = await connect({ host: '127.0.0.1', port: 20000, prefer: 'tls' })

    expect(first.kind).toBe('h2c')
    // Reconnecting just to honour `prefer` would drop every live stream.
    expect(second.kind).toBe('h2c')
    expect(h2State.connectCalls.length).toBe(1)
  })

  it('brackets an IPv6 literal in the authority', async () => {
    await connect({ host: '::1', port: 20000, prefer: 'h2c' })
    expect(h2State.connectCalls[0].authority).toBe('http://[::1]:20000')
  })

  it('times out instead of hanging forever when the peer never answers', async () => {
    h2State.behavior.tls = 'hang'
    h2State.behavior.h2c = 'hang'

    const res = await connect({ host: '127.0.0.1', port: 20000, prefer: 'h2c', timeoutMs: 10 })

    expect(res.ok).toBe(false)
    expect(res.error).toBe('connect timed out')
  })
})

describe('h2Transport: openStream', () => {
  beforeEach(async () => {
    await connect({ host: '127.0.0.1', port: 20000, prefer: 'h2c' })
  })

  it('POSTs to the stream endpoint with host/port query and octet-stream body', async () => {
    const p = openStream('localhost', 3000)
    await tick()
    await p

    // request[0] is connect()'s /api/ssh/info probe; request[1] is the data stream.
    const req = h2State.lastSession().requests[1]
    expect(req.headers[':method']).toBe('POST')
    expect(req.headers[':path']).toBe('/api/tunnel/stream?host=localhost&port=3000')
    expect(req.headers['content-type']).toBe('application/octet-stream')
    // Streaming body: a Content-Length would freeze the request at one size.
    expect(req.headers['content-length']).toBeUndefined()
  })

  it('carries the session cookie as a plain header', async () => {
    h2State.cookie = 'clawbench_session=abc123'
    const p = openStream('127.0.0.1', 8080)
    await tick()
    await p

    expect(h2State.lastSession().requests[1].headers['cookie']).toBe('clawbench_session=abc123')
  })

  it('omits the cookie header when not logged in', async () => {
    h2State.cookie = null
    const p = openStream('127.0.0.1', 8080)
    await tick()
    await p

    expect('cookie' in h2State.lastSession().requests[1].headers).toBe(false)
  })

  it('rejects and destroys the stream when the server answers 502', async () => {
    const session = h2State.lastSession()
    // Make the NEXT data stream answer 502 (dial failure).
    const origRequest = session.request.bind(session)
    session.request = (headers: Record<string, unknown>) => {
      const st = origRequest(headers)
      st.status = 502
      return st
    }

    await expect(openStream('127.0.0.1', 9999)).rejects.toThrow(/HTTP 502/)
    const st = session.requests[session.requests.length - 1]
    expect(st.destroyed).toBe(true)
  })

  it('rejects when there is no live session', async () => {
    close()
    await expect(openStream('127.0.0.1', 3000)).rejects.toThrow('not connected')
  })

  it('rejects a pending stream when the session dies before the response', async () => {
    const session = h2State.lastSession()
    // Make the next request hang (never respond), then kill the session. A
    // real stream emits 'aborted'/'close' (no 'error'), so without handling
    // them the caller would await a promise that never settles.
    h2State.behavior.h2c = 'hang'

    const p = openStream('127.0.0.1', 3000)
    await tick()
    session.destroy()

    await expect(p).rejects.toThrow(/closed before response|aborted/)
  })

  it('ends the readable side when the wire closes without END_STREAM (RST)', async () => {
    const p = openStream('127.0.0.1', 3000)
    await tick()
    const stream = await p
    const raw = h2State.lastSession().requests[1]

    let ended = false
    stream.on('end', () => { ended = true })
    // Flowing mode, exactly as `socket.pipe(stream)` puts it: without a
    // consumer, EOF is recorded but 'end' only fires once the buffer drains.
    stream.on('data', () => { /* drain */ })
    // An RST_STREAM closes the stream without 'end'; the readable must still
    // see EOF or a local socket piped from it would hang forever.
    raw.destroy()
    await tick()
    expect(ended).toBe(true)
  })
})

describe('h2Transport: duplex data flow', () => {
  it('relays bytes in both directions', async () => {
    await connect({ host: '127.0.0.1', port: 20000, prefer: 'h2c' })
    const p = openStream('127.0.0.1', 3000)
    await tick()
    const stream = await p
    const raw = h2State.lastSession().requests[1]

    // client -> server
    stream.write('ping')
    expect(Buffer.concat(raw.writes).toString()).toBe('ping')

    // server -> client
    const received: string[] = []
    stream.on('data', (c: Buffer) => received.push(c.toString()))
    raw.feed('pong')
    await tick()
    expect(received).toEqual(['pong'])
  })

  it('waits for drain when the underlying write applies backpressure', async () => {
    await connect({ host: '127.0.0.1', port: 20000, prefer: 'h2c' })
    const p = openStream('127.0.0.1', 3000)
    await tick()
    const stream = await p
    const raw = h2State.lastSession().requests[1]

    raw.holdWrites = true
    stream.write('first')
    stream.write('second')
    await tick()

    // Only the first chunk reached the wire, and the transport is parked on a
    // drain listener rather than dropping the second.
    expect(raw.writes.map(b => b.toString())).toEqual(['first'])
    expect(raw.listenerCount('drain')).toBe(1)

    raw.holdWrites = false
    raw.emit('drain')
    await tick()
    expect(raw.writes.map(b => b.toString())).toEqual(['first', 'second'])
  })

  it('pauses the underlying stream when the consumer stops reading', async () => {
    await connect({ host: '127.0.0.1', port: 20000, prefer: 'h2c' })
    const p = openStream('127.0.0.1', 3000)
    await tick()
    const stream = await p
    const raw = h2State.lastSession().requests[1]

    // No 'data' listener, so the wrapper's readable buffer fills and push()
    // returns false — the transport must pause the wire rather than buffer.
    // The h2 wire is faked with an unbounded emitter, so feed enough to
    // exceed the wrapper's highWaterMark deterministically.
    raw.feed('x'.repeat(1024 * 1024))
    expect(raw.paused).toBe(true)

    // Consuming resumes the wire (the read side drains on the next tick).
    stream.on('data', () => { /* drain it */ })
    await tick()
    expect(raw.paused).toBe(false)
    expect(raw.resumes).toBeGreaterThan(0)
  })
})

describe('h2Transport: half-close and write-after-end', () => {
  async function openRaw() {
    await connect({ host: '127.0.0.1', port: 20000, prefer: 'h2c' })
    const p = openStream('127.0.0.1', 3000)
    await tick()
    const stream = await p
    return { stream, raw: h2State.lastSession().requests[1] }
  }

  it('end() half-closes the request body without closing the response side', async () => {
    const { stream, raw } = await openRaw()

    stream.end()
    await tick()
    expect(raw.ended).toBe(true)          // END_STREAM went out
    expect(stream.writableEnded).toBe(true)
    expect(stream.readableEnded).toBe(false) // still open for replies

    const received: string[] = []
    stream.on('data', (c: Buffer) => received.push(c.toString()))
    raw.feed('reply-after-eof')
    await tick()
    expect(received).toEqual(['reply-after-eof'])
  })

  it('drops a write after end() instead of emitting ERR_STREAM_WRITE_AFTER_END', async () => {
    const { stream, raw } = await openRaw()
    const errors: NodeJS.ErrnoException[] = []
    stream.on('error', (e: NodeJS.ErrnoException) => errors.push(e))

    stream.end()
    await tick()
    const before = raw.writes.length

    // A final chunk racing the local socket's EOF is the real-world case.
    expect(() => stream.write('late')).not.toThrow()
    await tick()

    expect(raw.writes.length).toBe(before)
    expect(errors.map(e => e.code)).not.toContain('ERR_STREAM_WRITE_AFTER_END')
  })

  it('forwards a wire error to the duplex so the local socket can be torn down', async () => {
    const { stream, raw } = await openRaw()
    const errors: NodeJS.ErrnoException[] = []
    stream.on('error', (e: NodeJS.ErrnoException) => errors.push(e))

    raw.emit('error', Object.assign(new Error('boom'), { code: 'ECONNRESET' }))
    await tick()
    expect(errors.map(e => e.code)).toEqual(['ECONNRESET'])
  })
})

describe('h2Transport: session lifecycle', () => {
  it('reports disconnected after a session error', async () => {
    await connect({ host: '127.0.0.1', port: 20000, prefer: 'h2c' })
    h2State.lastSession().emit('error', Object.assign(new Error('reset'), { code: 'ECONNRESET' }))
    expect(isConnected()).toBe(false)
  })

  it('reports disconnected after a session close', async () => {
    await connect({ host: '127.0.0.1', port: 20000, prefer: 'h2c' })
    h2State.lastSession().emit('close')
    expect(isConnected()).toBe(false)
    expect(getTransportKind()).toBeNull()
  })

  it('reports disconnected after a GOAWAY', async () => {
    await connect({ host: '127.0.0.1', port: 20000, prefer: 'h2c' })
    h2State.lastSession().emit('goaway', 0, 0)
    expect(isConnected()).toBe(false)
  })

  it('close() tears the session down', async () => {
    await connect({ host: '127.0.0.1', port: 20000, prefer: 'h2c' })
    const s = h2State.lastSession()
    close()
    // destroy(), not close(): a graceful close would wait for open streams,
    // leaving the session (and its socket) alive after a disconnect.
    expect(s.destroyCalls).toBe(1)
    expect(isConnected()).toBe(false)
    // Idempotent: a second close must not throw.
    expect(() => close()).not.toThrow()
  })
})

/**
 * The desktop half of the Android liveness defect (cb693c7c).
 *
 * A half-open session — NAT rebind, server restart — emits neither 'error' nor
 * 'close' and never answers a PING. Before the heartbeat, `isConnected()` stayed
 * true forever, so tunnel.ts's monitor skipped it and every `-R` mapping stayed
 * dead until a manual reconnect. These tests pin the two mechanisms that make it
 * observable: the periodic PING, and the session-level `dead` flag it sets.
 */
describe('h2Transport: session liveness', () => {
  it('arms the keepalive after connect and PINGs the session on the interval', async () => {
    vi.useFakeTimers()
    try {
      await connect({ host: '127.0.0.1', port: 20000, prefer: 'h2c' })
      const s = h2State.lastSession()

      // Nothing yet: the first PING is one interval away.
      expect(s.pingCalls.length).toBe(0)
      await vi.advanceTimersByTimeAsync(30001)
      // PING sent, PONG came back (fake answers in a microtask), re-armed.
      expect(s.pingCalls.length).toBe(1)
      expect(isConnected()).toBe(true)
      // The re-arm is real: a second interval produces a second PING.
      await vi.advanceTimersByTimeAsync(30001)
      expect(s.pingCalls.length).toBe(2)
    } finally {
      vi.useRealTimers()
    }
  })

  it('marks the session dead when a PING is never answered (half-open peer)', async () => {
    vi.useFakeTimers()
    try {
      await connect({ host: '127.0.0.1', port: 20000, prefer: 'h2c' })
      const s = h2State.lastSession()
      // The peer goes silent: no PONG, and (as measured against a real
      // black hole) no 'error'/'close' either.
      s.dropPings = true

      // The PING itself fires at 30s and, unanswered, must time out 30s later.
      await vi.advanceTimersByTimeAsync(30001)
      expect(isConnected()).toBe(true)   // still inside the PING deadline
      await vi.advanceTimersByTimeAsync(30001)
      expect(isConnected()).toBe(false)  // PING timed out -> dead
    } finally {
      vi.useRealTimers()
    }
  })

  it('marks the session dead on a session error', async () => {
    await connect({ host: '127.0.0.1', port: 20000, prefer: 'h2c' })
    h2State.lastSession().emit('error', Object.assign(new Error('reset'), { code: 'ECONNRESET' }))
    expect(isConnected()).toBe(false)
  })

  it('marks the session dead on a session close', async () => {
    await connect({ host: '127.0.0.1', port: 20000, prefer: 'h2c' })
    h2State.lastSession().emit('close')
    expect(isConnected()).toBe(false)
  })

  it('marks the session dead on a GOAWAY', async () => {
    await connect({ host: '127.0.0.1', port: 20000, prefer: 'h2c' })
    h2State.lastSession().emit('goaway', 0, 0)
    expect(isConnected()).toBe(false)
  })

  it('clears the keepalive timer on closeSession (no leak)', async () => {
    vi.useFakeTimers()
    try {
      await connect({ host: '127.0.0.1', port: 20000, prefer: 'h2c' })
      const s = h2State.lastSession()
      expect(_hasHeartbeatForTesting()).toBe(true)

      close()
      // The clear is what stops the timer outliving its session. The stale tick
      // would be a silent no-op (identity guard), so assert the handle itself.
      expect(_hasHeartbeatForTesting()).toBe(false)

      await vi.advanceTimersByTimeAsync(60002)
      // The heartbeat belonged to the torn-down session: it must not PING the
      // old one (nor keep the timer alive past close).
      expect(s.pingCalls.length).toBe(0)
      expect(isConnected()).toBe(false)
    } finally {
      vi.useRealTimers()
    }
  })

  it('does not mark the session dead when a single stream fails (no false positive)', async () => {
    await connect({ host: '127.0.0.1', port: 20000, prefer: 'h2c' })
    const session = h2State.lastSession()
    // The next data stream answers 502 — the server answered over a perfectly
    // healthy h2 session, so the session must stay connected. Marking it dead
    // here would reconnect on every refused target.
    const origRequest = session.request.bind(session)
    session.request = (headers: Record<string, unknown>) => {
      const st = origRequest(headers)
      st.status = 502
      return st
    }

    await expect(openStream('127.0.0.1', 9999)).rejects.toThrow(/HTTP 502/)
    expect(isConnected()).toBe(true)
  })
})

describe('h2Transport: claim stream', () => {
  it('opens a stream carrying the claim token', async () => {
    await connect({ host: '127.0.0.1', port: 20000, prefer: 'h2c' })
    const p = openClaimStream('tok-abc')
    await tick()
    await p

    const req = h2State.lastSession().requests[1]
    expect(req.headers[':method']).toBe('POST')
    expect(req.headers[':path']).toBe('/api/tunnel/stream?claim=tok-abc')
  })
})

describe('h2Transport: control stream', () => {
  async function openControl() {
    await connect({ host: '127.0.0.1', port: 20000, prefer: 'h2c' })
    const p = openControlStream()
    await tick()
    const control = await p
    return { control, raw: h2State.lastSession().requests[1] }
  }

  it('POSTs the control endpoint', async () => {
    const { raw } = await openControl()
    expect(raw.headers[':method']).toBe('POST')
    expect(raw.headers[':path']).toBe('/api/tunnel/control')
  })

  it('writes one NDJSON line per message', async () => {
    const { control, raw } = await openControl()

    control.send({ type: 'bind', port: 0 })
    control.send({ type: 'unbind', port: 4000 })

    expect(Buffer.concat(raw.writes).toString()).toBe(
      '{"type":"bind","port":0}\n{"type":"unbind","port":4000}\n',
    )
  })

  it('parses newline-delimited messages, including across chunk boundaries', async () => {
    const { control, raw } = await openControl()
    const seen: unknown[] = []
    control.onMessage((m) => seen.push(m))

    raw.feed('{"type":"bound","port":4000}\n{"type":"inco')
    raw.feed('ming","port":4000,"token":"t1"}\n')
    await tick()

    expect(seen).toEqual([
      { type: 'bound', port: 4000 },
      { type: 'incoming', port: 4000, token: 't1' },
    ])
  })

  it('skips a malformed line without dropping the stream', async () => {
    const { control, raw } = await openControl()
    const seen: unknown[] = []
    control.onMessage((m) => seen.push(m))

    raw.feed('not json\n{"type":"pong"}\n')
    await tick()

    expect(seen).toEqual([{ type: 'pong' }])
    expect(control.closed).toBe(false)
  })

  it('ignores a JSON line that is not an object with a type', async () => {
    const { control, raw } = await openControl()
    const seen: unknown[] = []
    control.onMessage((m) => seen.push(m))

    raw.feed('42\n{"type":"pong"}\n')
    await tick()

    expect(seen).toEqual([{ type: 'pong' }])
  })

  it('fires onClose when the server half-closes', async () => {
    const { control, raw } = await openControl()
    let closed = 0
    control.onClose(() => { closed++ })

    raw.finishRemote()
    await tick()

    expect(closed).toBe(1)
    expect(control.closed).toBe(true)
  })

  it('close() ends the underlying stream and fires onClose', async () => {
    const { control, raw } = await openControl()
    let closed = 0
    control.onClose(() => { closed++ })

    control.close()
    await tick()

    expect(raw.ended).toBe(true)
    expect(control.closed).toBe(true)
    expect(closed).toBe(1)
  })

  it('drops send() after close', async () => {
    const { control, raw } = await openControl()
    control.close()
    await tick()
    const before = raw.writes.length

    control.send({ type: 'ping' })
    expect(raw.writes.length).toBe(before)
  })
})
