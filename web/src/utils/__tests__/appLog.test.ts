import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { appLog, startFlushTimer, stopFlushTimer, _clearBuffer } from '@/utils/appLog'

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
  })

  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
    ;(window as any).ClawBenchNative = origClawBenchNative
    stopFlushTimer()
    _clearBuffer()
  })

  it('enqueues entries and flushes them via HTTP POST', async () => {
    vi.spyOn(console, 'log').mockImplementation(() => {})
    vi.spyOn(console, 'info').mockImplementation(() => {})

    appLog.d('ChatStream', 'SSE connected')
    appLog.i('Store', 'state loaded')

    startFlushTimer()
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

  it('skips HTTP relay in Android app mode (native bridge handles it)', async () => {
    vi.spyOn(console, 'log').mockImplementation(() => {})
    ;(window as any).ClawBenchNative = { log: vi.fn(), isNativeApp: () => true }

    appLog.d('Test', 'hello')
    // Give a tick for any async operations
    await new Promise(r => setTimeout(r, 100))
    expect(fetchSpy).not.toHaveBeenCalled()
  })

  it('discards entries when server is unreachable', async () => {
    vi.spyOn(console, 'log').mockImplementation(() => {})
    fetchSpy.mockRejectedValue(new Error('NetworkError'))

    appLog.e('Test', 'fail msg')
    startFlushTimer()
    await vi.waitFor(() => expect(fetchSpy).toHaveBeenCalled(), { timeout: 3000 })
    // Should not throw — silently discards
  })
})
