import { describe, it, expect, vi } from 'vitest'
import { guardStartupWithSplash } from '../useStartupGuard'

/**
 * Regression tests for issue #449.
 *
 * The native Android splash overlay is dismissed by JS via
 * ClawBenchNative.dismissSplash(). It used to be called only *after*
 * `await initializeApp()`, so an exception during initialization rejected the
 * async handler before reaching the call and the user was stranded on
 * "正在初始化应用…" with no way forward. The guard must dismiss the splash on
 * every exit path.
 */
describe('guardStartupWithSplash', () => {
  it('dismisses the splash and returns true when initialization succeeds', async () => {
    const dismissSplash = vi.fn()
    const onError = vi.fn()

    const ok = await guardStartupWithSplash({
      initialize: async () => true,
      dismissSplash,
      onError,
    })

    expect(ok).toBe(true)
    expect(dismissSplash).toHaveBeenCalledTimes(1)
    expect(onError).not.toHaveBeenCalled()
  })

  it('dismisses the splash and returns false when initialization reports a handled failure', async () => {
    const dismissSplash = vi.fn()
    const onError = vi.fn()

    const ok = await guardStartupWithSplash({
      initialize: async () => false,
      dismissSplash,
      onError,
    })

    expect(ok).toBe(false)
    // The splash must come down even though init bailed out.
    expect(dismissSplash).toHaveBeenCalledTimes(1)
    // initialize() already reported its own specific error — no double report.
    expect(onError).not.toHaveBeenCalled()
  })

  it('dismisses the splash, reports the error, and returns false when initialization throws', async () => {
    const dismissSplash = vi.fn()
    const onError = vi.fn()
    const boom = new Error('init exploded')

    const ok = await guardStartupWithSplash({
      initialize: async () => { throw boom },
      dismissSplash,
      onError,
    })

    expect(ok).toBe(false)
    // This is the exact regression: the throw must not skip dismissSplash().
    expect(dismissSplash).toHaveBeenCalledTimes(1)
    expect(onError).toHaveBeenCalledWith(boom)
  })

  it('does not reject the caller when initialization throws (never throws itself)', async () => {
    // A rejected promise here is what previously killed onMounted before
    // dismissSplash(), so the guard must swallow it.
    await expect(guardStartupWithSplash({
      initialize: async () => { throw new Error('boom') },
      dismissSplash: vi.fn(),
      onError: vi.fn(),
    })).resolves.toBe(false)
  })

  it('dismisses the splash when initialize rejects asynchronously after a tick', async () => {
    const dismissSplash = vi.fn()
    const onError = vi.fn()

    const ok = await guardStartupWithSplash({
      initialize: async () => {
        await Promise.resolve()
        throw new Error('late rejection')
      },
      dismissSplash,
      onError,
    })

    expect(ok).toBe(false)
    expect(dismissSplash).toHaveBeenCalledTimes(1)
    expect(onError).toHaveBeenCalledTimes(1)
  })

  describe('onReady ordering', () => {
    it('runs onReady before dismissing the splash on success', async () => {
      const order: string[] = []

      const ok = await guardStartupWithSplash({
        initialize: async () => { order.push('init'); return true },
        onReady: () => { order.push('ready') },
        dismissSplash: () => { order.push('dismiss') },
        onError: vi.fn(),
      })

      expect(ok).toBe(true)
      // The app UI must be mounted (onReady) before the splash fades, so the
      // login view is never exposed during the fade-out.
      expect(order).toEqual(['init', 'ready', 'dismiss'])
    })

    it('does not run onReady when initialization reports a failure', async () => {
      const onReady = vi.fn()

      const ok = await guardStartupWithSplash({
        initialize: async () => false,
        onReady,
        dismissSplash: vi.fn(),
        onError: vi.fn(),
      })

      expect(ok).toBe(false)
      expect(onReady).not.toHaveBeenCalled()
    })

    it('does not run onReady when initialization throws', async () => {
      const onReady = vi.fn()

      const ok = await guardStartupWithSplash({
        initialize: async () => { throw new Error('boom') },
        onReady,
        dismissSplash: vi.fn(),
        onError: vi.fn(),
      })

      expect(ok).toBe(false)
      expect(onReady).not.toHaveBeenCalled()
    })

    it('still dismisses the splash when onReady itself throws', async () => {
      // onReady flips isAuthenticated; a throw there must not strand the splash.
      const dismissSplash = vi.fn()
      const onError = vi.fn()

      const ok = await guardStartupWithSplash({
        initialize: async () => true,
        onReady: () => { throw new Error('render failed') },
        dismissSplash,
        onError,
      })

      expect(ok).toBe(false)
      expect(onError).toHaveBeenCalledTimes(1)
      expect(dismissSplash).toHaveBeenCalledTimes(1)
    })
  })
})
