import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'

// Must mock before import
const originalWindowTop = window.top

describe('useAppMode', () => {
  beforeEach(() => {
    // Reset module cache between tests
    vi.resetModules()
    // Reset document attribute. Both names: `data-app-mode` is the old spelling
    // and removing it too keeps a stale attribute from a previous test file
    // from making the "not set" assertions pass for the wrong reason.
    document.documentElement.removeAttribute('data-webview-mode')
    document.documentElement.removeAttribute('data-app-mode')
  })

  afterEach(() => {
    // Clean up
    vi.restoreAllMocks()
  })

  it('detects web mode when ClawBenchNative is not defined', async () => {
    const { useAppMode } = await import('@/composables/useAppMode')
    const { isAppMode } = useAppMode()
    expect(isAppMode.value).toBe(false)
  })

  it('detects app mode when ClawBenchNative.isNativeApp() returns true', async () => {
    // Set up the mock before importing
    ;(window as any).ClawBenchNative = {
      isNativeApp: () => true,
    }

    const { useAppMode } = await import('@/composables/useAppMode')
    const { isAppMode } = useAppMode()

    expect(isAppMode.value).toBe(true)

    // Clean up
    delete (window as any).ClawBenchNative
  })

  it('reports isDesktopApp for the Electron shell (isDesktopApp() === true)', async () => {
    ;(window as any).ClawBenchNative = {
      isNativeApp: () => true,
      isDesktopApp: () => true,
    }

    const { useAppMode } = await import('@/composables/useAppMode')
    const { isAppMode, isDesktopApp } = useAppMode()

    expect(isAppMode.value).toBe(true)
    expect(isDesktopApp.value).toBe(true)

    delete (window as any).ClawBenchNative
  })

  /**
   * The root attribute gates ~16 Android-only CSS compensations, several of
   * which are actively harmful on a desktop engine:
   *
   *   - `will-change: transform` on `.chat-panel-content` + `contain: paint` on
   *     every `.chat-message` made the whole scroller one composited layer
   *     sliced into per-row paint chunks. On the desktop shell that produced a
   *     mis-painted row — a single line belonging to a LATER thinking block
   *     drawn at the top of the message, and gone again after a session switch
   *     (which rebuilds the DOM). Reported 2026-10-29.
   *   - `-webkit-text-stroke` bold compensation double-darkens glyphs the
   *     desktop engine already renders with a real bold face.
   *
   * The JS side already excluded the desktop shell everywhere it mattered; the
   * CSS hook was the one place that did not. These assertions pin the split so
   * it cannot silently widen back to "any native host".
   */
  describe('the WebView-only attribute', () => {
    it('is set for the Android WebView', async () => {
      ;(window as any).ClawBenchNative = { isNativeApp: () => true }

      const { useAppMode } = await import('@/composables/useAppMode')
      useAppMode()

      expect(document.documentElement.hasAttribute('data-webview-mode')).toBe(true)

      delete (window as any).ClawBenchNative
    })

    it('is NOT set for the Electron desktop shell', async () => {
      // The bridge reports isNativeApp() === true here too (preload/index.ts),
      // so a gate on isAppMode alone would set it.
      ;(window as any).ClawBenchNative = {
        isNativeApp: () => true,
        isDesktopApp: () => true,
      }

      const { useAppMode } = await import('@/composables/useAppMode')
      useAppMode()

      expect(
        document.documentElement.hasAttribute('data-webview-mode'),
        'the desktop shell must not get the Android compositing/bold ' +
        'compensations — they caused the chat-row ghost and double-darken text',
      ).toBe(false)

      delete (window as any).ClawBenchNative
    })

    it('is NOT set in web mode', async () => {
      const { useAppMode } = await import('@/composables/useAppMode')
      useAppMode()

      expect(document.documentElement.hasAttribute('data-webview-mode')).toBe(false)
    })

    it('is no longer named data-app-mode', async () => {
      // The old name read as "any app shell", which is how the desktop shell
      // ended up opted into Android workarounds. The rename is part of the fix.
      ;(window as any).ClawBenchNative = { isNativeApp: () => true }

      const { useAppMode } = await import('@/composables/useAppMode')
      useAppMode()

      expect(
        document.documentElement.hasAttribute('data-app-mode'),
        'the attribute was renamed to data-webview-mode',
      ).toBe(false)

      delete (window as any).ClawBenchNative
    })
  })

  it('does not report isDesktopApp for Android (no isDesktopApp on the bridge)', async () => {
    // Android's bridge has no isDesktopApp(); the optional call must resolve
    // to false so Android keeps its background-suspension behaviour.
    ;(window as any).ClawBenchNative = {
      isNativeApp: () => true,
    }

    const { useAppMode } = await import('@/composables/useAppMode')
    const { isAppMode, isDesktopApp } = useAppMode()

    expect(isAppMode.value).toBe(true)
    expect(isDesktopApp.value).toBe(false)

    delete (window as any).ClawBenchNative
  })

  it('never reports isDesktopApp in web mode', async () => {
    ;(window as any).ClawBenchNative = {
      isNativeApp: () => false,
      isDesktopApp: () => true, // inconsistent bridge must not leak through
    }

    const { useAppMode } = await import('@/composables/useAppMode')
    const { isAppMode, isDesktopApp } = useAppMode()

    expect(isAppMode.value).toBe(false)
    expect(isDesktopApp.value).toBe(false)

    delete (window as any).ClawBenchNative
  })

  it('detects web mode when ClawBenchNative.isNativeApp() returns false', async () => {
    ;(window as any).ClawBenchNative = {
      isNativeApp: () => false,
    }

    const { useAppMode } = await import('@/composables/useAppMode')
    const { isAppMode } = useAppMode()

    expect(isAppMode.value).toBe(false)

    delete (window as any).ClawBenchNative
  })

  it('returns singleton state across multiple calls', async () => {
    const { useAppMode } = await import('@/composables/useAppMode')
    const instance1 = useAppMode()
    const instance2 = useAppMode()

    expect(instance1.isAppMode).toBe(instance2.isAppMode)
  })
})
