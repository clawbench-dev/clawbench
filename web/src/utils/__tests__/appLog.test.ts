import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

// We must import appLog after setting up window mocks
describe('appLog', () => {
  const origWindow = globalThis.window
  let logSpy: ReturnType<typeof vi.fn>

  beforeEach(() => {
    logSpy = vi.fn()
  })

  afterEach(() => {
    vi.restoreAllMocks()
    // @ts-expect-error test cleanup
    delete globalThis.window
    globalThis.window = origWindow
  })

  function mockWindow(androidNative?: any) {
    // Create a minimal window mock with window === window.top
    const w = { top: null as any } as any
    w.top = w // window === window.top (not iframe)
    w.AndroidNative = androidNative
    globalThis.window = w
  }

  it('appLog.d calls console.log with [tag] prefix', async () => {
    const logFn = vi.spyOn(console, 'log').mockImplementation(() => {})
    mockWindow() // no AndroidNative
    const { appLog } = await import('@/utils/appLog')
    appLog.d('Test', 'hello')
    expect(logFn).toHaveBeenCalledWith('[Test]', 'hello')
  })

  it('appLog.w calls console.warn with [tag] prefix', async () => {
    const warnFn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    mockWindow()
    const { appLog } = await import('@/utils/appLog')
    appLog.w('Test', 'warning msg')
    expect(warnFn).toHaveBeenCalledWith('[Test]', 'warning msg')
  })

  it('appLog.e calls console.error with [tag] prefix', async () => {
    const errFn = vi.spyOn(console, 'error').mockImplementation(() => {})
    mockWindow()
    const { appLog } = await import('@/utils/appLog')
    appLog.e('Test', 'error msg')
    expect(errFn).toHaveBeenCalledWith('[Test]', 'error msg')
  })

  it('appLog.i calls console.info with [tag] prefix', async () => {
    const infoFn = vi.spyOn(console, 'info').mockImplementation(() => {})
    mockWindow()
    const { appLog } = await import('@/utils/appLog')
    appLog.i('Test', 'info msg')
    expect(infoFn).toHaveBeenCalledWith('[Test]', 'info msg')
  })

  it('relays via AndroidNative.log in app mode', async () => {
    vi.spyOn(console, 'log').mockImplementation(() => {})
    mockWindow({
      log: logSpy,
      isNativeApp: () => true,
    })
    const { appLog } = await import('@/utils/appLog')
    appLog.d('MyTag', 'hello', 'world')
    expect(logSpy).toHaveBeenCalledWith('D', 'MyTag', 'hello world')
  })

  it('relays error level correctly', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    mockWindow({
      log: logSpy,
      isNativeApp: () => true,
    })
    const { appLog } = await import('@/utils/appLog')
    appLog.e('MyTag', 'fail:', 'code')
    expect(logSpy).toHaveBeenCalledWith('E', 'MyTag', 'fail: code')
  })

  it('skips relay when AndroidNative is absent', async () => {
    vi.spyOn(console, 'log').mockImplementation(() => {})
    mockWindow() // no AndroidNative
    const { appLog } = await import('@/utils/appLog')
    appLog.d('Test', 'hello')
    expect(logSpy).not.toHaveBeenCalled()
  })

  it('skips relay when isNativeApp returns false', async () => {
    vi.spyOn(console, 'log').mockImplementation(() => {})
    mockWindow({
      log: logSpy,
      isNativeApp: () => false,
    })
    const { appLog } = await import('@/utils/appLog')
    appLog.d('Test', 'hello')
    expect(logSpy).not.toHaveBeenCalled()
  })

  it('skips relay in iframe (window !== window.top)', async () => {
    vi.spyOn(console, 'log').mockImplementation(() => {})
    const w = { top: {} } as any // window.top is different object → iframe
    w.AndroidNative = { log: logSpy, isNativeApp: () => true }
    globalThis.window = w
    const { appLog } = await import('@/utils/appLog')
    appLog.d('Test', 'hello')
    expect(logSpy).not.toHaveBeenCalled()
  })

  it('JSON-serializes object arguments in relay', async () => {
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    mockWindow({
      log: logSpy,
      isNativeApp: () => true,
    })
    const { appLog } = await import('@/utils/appLog')
    appLog.w('Test', 'err:', { code: 404 })
    expect(logSpy).toHaveBeenCalledWith('W', 'Test', 'err: {"code":404}')
  })

  it('handles circular references safely in relay', async () => {
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    mockWindow({
      log: logSpy,
      isNativeApp: () => true,
    })
    const { appLog } = await import('@/utils/appLog')
    const circular: any = { name: 'test' }
    circular.self = circular
    appLog.w('Test', 'circular:', circular)
    // Should not throw — uses safeStringify which falls back to String()
    expect(logSpy).toHaveBeenCalled()
  })
})
