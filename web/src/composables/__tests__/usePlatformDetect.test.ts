import { describe, expect, it, beforeEach, vi } from 'vitest'
import { _setPlatformForTest, _resetPlatformForTest, usePlatformDetect, isAndroidUA, isIOSUA, isIPadOSUA, isWindowsUA, isMacDesktopUA, isLinuxDesktopUA, isMobileOSUA } from '@/composables/usePlatformDetect'

// Mock useAppMode to control the host axis in tests
vi.mock('@/composables/useAppMode', () => ({
  useAppMode: () => ({ isAppMode: { value: false }, isDesktopApp: { value: false } }),
}))

beforeEach(() => {
  _resetPlatformForTest()
})

describe('UA detection constants', () => {
  it('isAndroidUA detects Android browser UA', () => {
    // jsdom default UA contains no "Android"
    expect(isAndroidUA).toBe(false)
  })

  it('isIOSUA detects iOS UA', () => {
    // jsdom default UA contains no "iPhone/iPad/iPod"
    expect(isIOSUA).toBe(false)
  })

  it('isIPadOSUA detects iPadOS desktop-mode UA', () => {
    // jsdom UA contains "Macintosh" but maxTouchPoints defaults to 0
    expect(isIPadOSUA).toBe(false)
  })
})

/**
 * The module exposes three ORTHOGONAL axes, replacing the old conflated `isPC`:
 *   HOST     — isElectron / isAndroidApp / isWebApp (+ isNativeApp rollup)
 *   INPUT    — isTouchPrimary
 *   VIEWPORT — isWideScreen (lives in useWideScreenLayout, not here)
 *
 * These tests pin the host/input axes and, crucially, their INDEPENDENCE: a
 * consumer that picks the wrong axis must fail here rather than silently
 * inheriting the old conflated behaviour.
 */
describe('usePlatformDetect axes', () => {
  it('defaults to the web host with a fine pointer (jsdom)', () => {
    const { isElectron, isAndroidApp, isWebApp, isNativeApp, isTouchPrimary } = usePlatformDetect()
    expect(isWebApp.value).toBe(true)
    expect(isElectron.value).toBe(false)
    expect(isAndroidApp.value).toBe(false)
    expect(isNativeApp.value).toBe(false)
    // jsdom has no matchMedia; the UA fallback sees a desktop UA → not touch.
    expect(isTouchPrimary.value).toBe(false)
  })

  it('_setPlatformForTest drives each axis independently', () => {
    // A touchscreen laptop: web host, but a coarse primary pointer.
    _setPlatformForTest({ isElectron: false, isAndroidApp: false, isWebApp: true, isTouchPrimary: true })
    const a = usePlatformDetect()
    expect(a.isWebApp.value).toBe(true)
    expect(a.isTouchPrimary.value).toBe(true)
    expect(a.isElectron.value).toBe(false)
    expect(a.isNativeApp.value).toBe(false)
  })

  it('_setPlatformForTest keeps isNativeApp consistent with the host axes', () => {
    _setPlatformForTest({ isElectron: true, isAndroidApp: false, isWebApp: false })
    expect(usePlatformDetect().isNativeApp.value).toBe(true)
    _resetPlatformForTest()
    _setPlatformForTest({ isElectron: false, isAndroidApp: true, isWebApp: false })
    expect(usePlatformDetect().isNativeApp.value).toBe(true)
  })

  it('_resetPlatformForTest resets every axis', () => {
    _setPlatformForTest({ isElectron: true, isAndroidApp: true, isWebApp: true, isTouchPrimary: true })
    _resetPlatformForTest()
    const a = usePlatformDetect()
    // Re-initialized from the (mocked) web host.
    expect(a.isWebApp.value).toBe(true)
    expect(a.isElectron.value).toBe(false)
    expect(a.isAndroidApp.value).toBe(false)
    expect(a.isNativeApp.value).toBe(false)
    expect(a.isTouchPrimary.value).toBe(false)
  })
})

describe('host axis from useAppMode', () => {
  it('Android WebView: isAndroidApp true, isElectron false, isNativeApp true', async () => {
    vi.doMock('@/composables/useAppMode', () => ({
      useAppMode: () => ({ isAppMode: { value: true }, isDesktopApp: { value: false } }),
    }))
    vi.resetModules()
    const mod = await import('@/composables/usePlatformDetect')
    const { isElectron, isAndroidApp, isWebApp, isNativeApp } = mod.usePlatformDetect()
    expect(isAndroidApp.value).toBe(true)
    expect(isElectron.value).toBe(false)
    expect(isWebApp.value).toBe(false)
    expect(isNativeApp.value).toBe(true)
    vi.doUnmock('@/composables/useAppMode')
  })

  it('Electron shell: isElectron true, isAndroidApp false', async () => {
    // Electron is a native host too, so a consumer that tests only "native"
    // cannot tell the two apart — that is why the axes are separate.
    vi.doMock('@/composables/useAppMode', () => ({
      useAppMode: () => ({ isAppMode: { value: true }, isDesktopApp: { value: true } }),
    }))
    vi.resetModules()
    const mod = await import('@/composables/usePlatformDetect')
    const { isElectron, isAndroidApp, isWebApp, isNativeApp } = mod.usePlatformDetect()
    expect(isElectron.value).toBe(true)
    expect(isAndroidApp.value).toBe(false)
    expect(isWebApp.value).toBe(false)
    expect(isNativeApp.value).toBe(true)
    // doMock registrations outlive resetModules and would otherwise leak this
    // isDesktopApp=true into every later case.
    vi.doUnmock('@/composables/useAppMode')
  })

  it('plain browser: web host, neither native', async () => {
    vi.doMock('@/composables/useAppMode', () => ({
      useAppMode: () => ({ isAppMode: { value: false }, isDesktopApp: { value: false } }),
    }))
    vi.resetModules()
    const mod = await import('@/composables/usePlatformDetect')
    const { isElectron, isAndroidApp, isWebApp, isNativeApp } = mod.usePlatformDetect()
    expect(isWebApp.value).toBe(true)
    expect(isElectron.value).toBe(false)
    expect(isAndroidApp.value).toBe(false)
    expect(isNativeApp.value).toBe(false)
  })
})

describe('input axis falls back to the UA when matchMedia is unavailable', () => {
  it('Android browser UA → isTouchPrimary true', async () => {
    const androidUA = 'Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 Chrome/120.0.0.0 Mobile Safari/537.36'
    Object.defineProperty(navigator, 'userAgent', { configurable: true, value: androidUA })
    vi.resetModules()
    const mod = await import('@/composables/usePlatformDetect')
    expect(mod.isAndroidUA).toBe(true)
    expect(mod.isIOSUA).toBe(false)
    expect(mod.isIPadOSUA).toBe(false)
    expect(mod.usePlatformDetect().isTouchPrimary.value).toBe(true)
  })

  it('iOS Safari UA → isTouchPrimary true', async () => {
    const iosUA = 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1'
    Object.defineProperty(navigator, 'userAgent', { configurable: true, value: iosUA })
    vi.resetModules()
    const mod = await import('@/composables/usePlatformDetect')
    expect(mod.isAndroidUA).toBe(false)
    expect(mod.isIOSUA).toBe(true)
    expect(mod.isIPadOSUA).toBe(false)
    expect(mod.usePlatformDetect().isTouchPrimary.value).toBe(true)
  })

  it('iPadOS 13+ desktop-mode UA (maxTouchPoints > 0) → isTouchPrimary true', async () => {
    const ipadUA = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15'
    Object.defineProperty(navigator, 'userAgent', { configurable: true, value: ipadUA })
    Object.defineProperty(navigator, 'maxTouchPoints', { configurable: true, value: 5 })
    vi.resetModules()
    const mod = await import('@/composables/usePlatformDetect')
    expect(mod.isAndroidUA).toBe(false)
    expect(mod.isIOSUA).toBe(false)
    expect(mod.isIPadOSUA).toBe(true)
    expect(mod.usePlatformDetect().isTouchPrimary.value).toBe(true)
  })

  it('real Mac desktop UA (maxTouchPoints = 0) → isTouchPrimary false', async () => {
    const macUA = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36'
    Object.defineProperty(navigator, 'userAgent', { configurable: true, value: macUA })
    Object.defineProperty(navigator, 'maxTouchPoints', { configurable: true, value: 0 })
    vi.doMock('@/composables/useAppMode', () => ({
      useAppMode: () => ({ isAppMode: { value: false }, isDesktopApp: { value: false } }),
    }))
    vi.resetModules()
    const mod = await import('@/composables/usePlatformDetect')
    expect(mod.isAndroidUA).toBe(false)
    expect(mod.isIOSUA).toBe(false)
    expect(mod.isIPadOSUA).toBe(false)
    expect(mod.usePlatformDetect().isTouchPrimary.value).toBe(false)
  })

  it('Windows desktop UA → isTouchPrimary false', async () => {
    const winUA = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36'
    Object.defineProperty(navigator, 'userAgent', { configurable: true, value: winUA })
    vi.doMock('@/composables/useAppMode', () => ({
      useAppMode: () => ({ isAppMode: { value: false }, isDesktopApp: { value: false } }),
    }))
    vi.resetModules()
    const mod = await import('@/composables/usePlatformDetect')
    expect(mod.isAndroidUA).toBe(false)
    expect(mod.isIOSUA).toBe(false)
    expect(mod.isIPadOSUA).toBe(false)
    expect(mod.usePlatformDetect().isTouchPrimary.value).toBe(false)
  })
})

/**
 * `isMobileOSUA` exists because iPadOS 13+ sends a macOS UA. A caller asking
 * "is this a desktop computer?" that tests only Android/iOS hands an iPad the
 * macOS build — the regression this constant prevents.
 */
describe('isMobileOSUA covers the iPadOS desktop-mode UA', () => {
  it('is true for the iPadOS desktop-mode UA', async () => {
    const ipadUA = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15'
    Object.defineProperty(navigator, 'userAgent', { configurable: true, value: ipadUA })
    Object.defineProperty(navigator, 'maxTouchPoints', { configurable: true, value: 5 })
    vi.resetModules()
    const mod = await import('@/composables/usePlatformDetect')
    // The individual predicates disagree — this is exactly the trap.
    expect(mod.isIOSUA).toBe(false)
    expect(mod.isAndroidUA).toBe(false)
    expect(mod.isIPadOSUA).toBe(true)
    expect(mod.isMobileOSUA).toBe(true)
  })

  it('is false for a real Mac desktop UA', async () => {
    const macUA = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36'
    Object.defineProperty(navigator, 'userAgent', { configurable: true, value: macUA })
    Object.defineProperty(navigator, 'maxTouchPoints', { configurable: true, value: 0 })
    vi.resetModules()
    const mod = await import('@/composables/usePlatformDetect')
    expect(mod.isMobileOSUA).toBe(false)
  })
})

describe('exported UA constants stay available', () => {
  it('exposes the desktop-OS predicates used by the download / proxy copy', () => {
    expect(typeof isWindowsUA).toBe('boolean')
    expect(typeof isMacDesktopUA).toBe('boolean')
    expect(typeof isLinuxDesktopUA).toBe('boolean')
    expect(typeof isMobileOSUA).toBe('boolean')
  })
})
