import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { appLog, setLogCaptureEnabled, stopFlushTimer, _clearBuffer } from '@/utils/appLog'

describe('appLog console output', () => {
  it('appLog.d calls console.log with [tag] prefix', () => {
    const logFn = vi.spyOn(console, 'log').mockImplementation(() => {})
    appLog.d('Test', 'hello')
    expect(logFn).toHaveBeenCalledWith('[Test]', 'hello')
  })

  it('appLog.w calls console.warn with [tag] prefix', () => {
    const warnFn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    appLog.w('Test', 'warning msg')
    expect(warnFn).toHaveBeenCalledWith('[Test]', 'warning msg')
  })

  it('appLog.e calls console.error with [tag] prefix', () => {
    const errFn = vi.spyOn(console, 'error').mockImplementation(() => {})
    appLog.e('Test', 'error msg')
    expect(errFn).toHaveBeenCalledWith('[Test]', 'error msg')
  })

  it('appLog.i calls console.info with [tag] prefix', () => {
    const infoFn = vi.spyOn(console, 'info').mockImplementation(() => {})
    appLog.i('Test', 'info msg')
    expect(infoFn).toHaveBeenCalledWith('[Test]', 'info msg')
  })
})

describe('appLog native relay', () => {
  let logSpy: ReturnType<typeof vi.fn>
  const origClawBenchNative = (window as any).ClawBenchNative

  beforeEach(() => {
    _clearBuffer()
    logSpy = vi.fn()
  })

  afterEach(() => {
    (window as any).ClawBenchNative = origClawBenchNative
    vi.restoreAllMocks()
    _clearBuffer()
  })

  it('relays via ClawBenchNative.log in app mode', () => {
    vi.spyOn(console, 'log').mockImplementation(() => {})
    ;(window as any).ClawBenchNative = { log: logSpy, isNativeApp: () => true }
    appLog.d('MyTag', 'hello', 'world')
    expect(logSpy).toHaveBeenCalledWith('D', 'MyTag', 'hello world')
  })

  it('relays error level correctly', () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    ;(window as any).ClawBenchNative = { log: logSpy, isNativeApp: () => true }
    appLog.e('MyTag', 'fail:', 'code')
    expect(logSpy).toHaveBeenCalledWith('E', 'MyTag', 'fail: code')
  })

  it('skips native relay when ClawBenchNative is absent', () => {
    vi.spyOn(console, 'log').mockImplementation(() => {})
    delete (window as any).ClawBenchNative
    appLog.d('Test', 'hello')
    expect(logSpy).not.toHaveBeenCalled()
  })

  it('skips native relay when isNativeApp returns false', () => {
    vi.spyOn(console, 'log').mockImplementation(() => {})
    ;(window as any).ClawBenchNative = { log: logSpy, isNativeApp: () => false }
    appLog.d('Test', 'hello')
    expect(logSpy).not.toHaveBeenCalled()
  })

  it('JSON-serializes object arguments in native relay', () => {
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    ;(window as any).ClawBenchNative = { log: logSpy, isNativeApp: () => true }
    appLog.w('Test', 'err:', { code: 404 })
    expect(logSpy).toHaveBeenCalledWith('W', 'Test', 'err: {"code":404}')
  })

  it('handles circular references safely in native relay', () => {
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    ;(window as any).ClawBenchNative = { log: logSpy, isNativeApp: () => true }
    const circular: any = { name: 'test' }
    circular.self = circular
    appLog.w('Test', 'circular:', circular)
    expect(logSpy).toHaveBeenCalled()
  })
})

describe('appLog HTTP relay', () => {
  let fetchSpy: ReturnType<typeof vi.fn>
  const origClawBenchNative = (window as any).ClawBenchNative

  beforeEach(() => {
    _clearBuffer()
    fetchSpy = vi.fn().mockResolvedValue({ ok: true })
    vi.stubGlobal('fetch', fetchSpy)
    // Non-app mode window (no ClawBenchNative)
    delete (window as any).ClawBenchNative
    // Stand down the relay from any previous test; tests arm it explicitly.
    setLogCaptureEnabled(false)
  })

  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
    ;(window as any).ClawBenchNative = origClawBenchNative
    stopFlushTimer()
    _clearBuffer()
  })

  it('does NOT flush while the relay is disabled (Debug Log Capture off)', async () => {
    vi.spyOn(console, 'log').mockImplementation(() => {})
    vi.spyOn(console, 'info').mockImplementation(() => {})

    appLog.d('ChatStream', 'SSE connected')
    appLog.i('Store', 'state loaded')

    // Even after the periodic flush window elapses, no request may fire.
    await new Promise(r => setTimeout(r, 2500))
    expect(fetchSpy).not.toHaveBeenCalled()
  })

  it('visibilitychange(hidden) does NOT flush while disabled, but DOES when armed', async () => {
    vi.spyOn(console, 'log').mockImplementation(() => {})

    // Disabled: tab-hide must not POST the ring buffer.
    appLog.d('Test', 'off-window entry')
    Object.defineProperty(document, 'visibilityState', { value: 'hidden', configurable: true })
    document.dispatchEvent(new Event('visibilitychange'))
    await new Promise(r => setTimeout(r, 50))
    expect(fetchSpy).not.toHaveBeenCalled()

    // Armed: tab-hide flushes immediately.
    setLogCaptureEnabled(true)
    appLog.d('Test', 'armed entry')
    document.dispatchEvent(new Event('visibilitychange'))
    await vi.waitFor(() => expect(fetchSpy).toHaveBeenCalled(), { timeout: 2000 })
    const body = JSON.parse(fetchSpy.mock.calls[0][1].body)
    expect(body.entries.map((e: { msg: string }) => e.msg)).toEqual(['off-window entry', 'armed entry'])
  })

  it('enqueues entries and flushes them via HTTP POST when relay is armed', async () => {
    vi.spyOn(console, 'log').mockImplementation(() => {})
    vi.spyOn(console, 'info').mockImplementation(() => {})

    appLog.d('ChatStream', 'SSE connected')
    appLog.i('Store', 'state loaded')

    setLogCaptureEnabled(true)
    await vi.waitFor(() => expect(fetchSpy).toHaveBeenCalled(), { timeout: 3000 })

    expect(fetchSpy).toHaveBeenCalledWith('/api/client-log', expect.objectContaining({
      method: 'POST',
      keepalive: true,
    }))

    const body = JSON.parse(fetchSpy.mock.calls[0][1].body)
    expect(body.entries).toHaveLength(2)
    expect(body.entries[0]).toMatchObject({
      level: 'D',
      tag: 'ChatStream',
      msg: 'SSE connected',
      source: 'js',
    })
    expect(body.entries[1]).toMatchObject({
      level: 'I',
      tag: 'Store',
      msg: 'state loaded',
      source: 'js',
    })
  })

  it('high log volume does NOT fire requests between interval ticks (timer-only relay)', async () => {
    vi.spyOn(console, 'log').mockImplementation(() => {})
    setLogCaptureEnabled(true)

    // Simulate a streaming burst: 120 entries, well past the old 50-entry
    // early-flush threshold. With the timer-only relay, NO request may fire
    // until the next 2s interval tick.
    for (let i = 0; i < 120; i++) appLog.d('ChatStream', `token ${i}`)
    expect(fetchSpy).not.toHaveBeenCalled()

    // All 120 arrive in a single batched POST on the next tick.
    await vi.waitFor(() => expect(fetchSpy).toHaveBeenCalledTimes(1), { timeout: 4000 })
    const body = JSON.parse(fetchSpy.mock.calls[0][1].body)
    expect(body.entries).toHaveLength(120)
  })

  it('sends HTTP in App mode when capture armed, skipping console and native bridge', async () => {
    const logSpy = vi.fn()
    vi.spyOn(console, 'log').mockImplementation(() => {})
    ;(window as any).ClawBenchNative = { log: logSpy, isNativeApp: () => true }

    setLogCaptureEnabled(true)
    appLog.d('Test', 'hello')
    // Single HTTP copy — console and native bridge must both be skipped.
    await vi.waitFor(() => expect(fetchSpy).toHaveBeenCalled(), { timeout: 3000 })
    expect(logSpy).not.toHaveBeenCalled()
    expect(console.log).not.toHaveBeenCalled()
    const body = JSON.parse(fetchSpy.mock.calls[0][1].body)
    expect(body.entries[0]).toMatchObject({ level: 'D', tag: 'Test', msg: 'hello', source: 'js' })
  })

  it('App mode with capture OFF keeps console + native bridge and sends no HTTP', async () => {
    const logSpy = vi.fn()
    const cLog = vi.spyOn(console, 'log').mockImplementation(() => {})
    ;(window as any).ClawBenchNative = { log: logSpy, isNativeApp: () => true }

    setLogCaptureEnabled(false)
    appLog.d('Test', 'hello')
    expect(logSpy).toHaveBeenCalledWith('D', 'Test', 'hello')
    expect(cLog).toHaveBeenCalledWith('[Test]', 'hello')
    await new Promise(r => setTimeout(r, 2500))
    expect(fetchSpy).not.toHaveBeenCalled()
  })

  it('discards entries when server is unreachable', async () => {
    vi.spyOn(console, 'log').mockImplementation(() => {})
    fetchSpy.mockRejectedValue(new Error('NetworkError'))

    appLog.e('Test', 'fail msg')
    setLogCaptureEnabled(true)
    await vi.waitFor(() => expect(fetchSpy).toHaveBeenCalled(), { timeout: 3000 })
    // Should not throw — silently discards
  })

  it('does not latch isFlushing when fetch rejects (rejected flush frees the relay)', async () => {
    vi.spyOn(console, 'log').mockImplementation(() => {})
    fetchSpy
      .mockRejectedValueOnce(new Error('NetworkError'))
      .mockResolvedValueOnce({ ok: true })

    appLog.d('Test', 'first')
    setLogCaptureEnabled(true)
    // First flush (2s tick) rejects; the catch + finally must release the lock.
    await vi.waitFor(() => expect(fetchSpy).toHaveBeenCalledTimes(1), { timeout: 5000 })

    // A later flush must still fire — the lock was released in the finally.
    appLog.d('Test', 'second')
    await vi.waitFor(() => expect(fetchSpy).toHaveBeenCalledTimes(2), { timeout: 6000 })
  })

  it('aborts a hung fetch via AbortSignal so subsequent flushes are not blocked', async () => {
    vi.useFakeTimers()
    try {
      vi.spyOn(console, 'log').mockImplementation(() => {})
      // Never-settling fetch — the real-world dead keep-alive connection.
      // Only the AbortController signal can release it.
      fetchSpy.mockImplementation((_url: string, init?: RequestInit) => {
        return new Promise((_resolve, reject) => {
          init?.signal?.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')))
        })
      })

      appLog.d('Test', 'first')
      setLogCaptureEnabled(true)
      // Interval tick at t=2000 fires the first flush, which hangs awaiting fetch.
      await vi.advanceTimersByTimeAsync(2000)
      expect(fetchSpy).toHaveBeenCalledTimes(1)
      expect(fetchSpy.mock.calls[0][1].signal).toBeInstanceOf(AbortSignal)

      // Queue another entry while the first flush is still hung.
      appLog.d('Test', 'second')

      // Advance past the 5s abort timeout: the hung fetch aborts and doFlush's
      // finally releases the lock. The queued entry stays buffered — with the
      // timer-only relay it is sent on the NEXT 2s interval tick, not instantly.
      await vi.advanceTimersByTimeAsync(5000) // t=7000: abort fires, lock released
      await vi.advanceTimersByTimeAsync(2000) // t=9000: next tick sends entry 2
      expect(fetchSpy).toHaveBeenCalledTimes(2)

      // The second flush is also hung (same never-settling mock). Let its own
      // abort timer fire so doFlush releases the module-level lock before the
      // test ends — otherwise the latched flush leaks into later tests.
      await vi.advanceTimersByTimeAsync(5000)
    } finally {
      vi.useRealTimers()
    }
  })

  it('passes the abort signal through to fetch', async () => {
    vi.spyOn(console, 'log').mockImplementation(() => {})
    fetchSpy.mockResolvedValue({ ok: true })

    appLog.d('Test', 'hello')
    setLogCaptureEnabled(true)
    await vi.waitFor(() => expect(fetchSpy).toHaveBeenCalled(), { timeout: 3000 })

    expect(fetchSpy.mock.calls[0][1]).toMatchObject({ method: 'POST', keepalive: true })
    expect(fetchSpy.mock.calls[0][1].signal).toBeInstanceOf(AbortSignal)
  })

  it('disabling the relay mid-stream stops further requests', async () => {
    vi.spyOn(console, 'log').mockImplementation(() => {})
    fetchSpy.mockResolvedValue({ ok: true })

    setLogCaptureEnabled(true)
    appLog.d('Test', 'first')
    await vi.waitFor(() => expect(fetchSpy).toHaveBeenCalled(), { timeout: 3000 })

    // Switch off: pending entries dropped, timer stopped, no further fetches.
    setLogCaptureEnabled(false)
    appLog.d('Test', 'second')
    const countAfterDisable = fetchSpy.mock.calls.length
    await new Promise(r => setTimeout(r, 2500))
    expect(fetchSpy.mock.calls.length).toBe(countAfterDisable)
  })
})
