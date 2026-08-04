import { describe, expect, it, beforeEach, vi } from 'vitest'
import { _setIsPCForTest, _resetPlatformForTest, usePlatformDetect, isAndroidUA, isIOSUA, isIPadOSUA } from '@/composables/usePlatformDetect'

// Mock useAppMode to control isAppMode in tests
vi.mock('@/composables/useAppMode', () => ({
  useAppMode: () => ({ isAppMode: { value: false } }),
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

describe('usePlatformDetect', () => {
  it('returns isPC = true for desktop browser (jsdom default UA)', () => {
    const { isPC } = usePlatformDetect()
    expect(isPC.value).toBe(true)
  })

  it('_setIsPCForTest overrides isPC value', () => {
    _setIsPCForTest(false)
    const { isPC } = usePlatformDetect()
    expect(isPC.value).toBe(false)

    _setIsPCForTest(true)
    expect(isPC.value).toBe(true)
  })

  it('_resetPlatformForTest resets to uninitialized state', () => {
    const { isPC } = usePlatformDetect()
    expect(isPC.value).toBe(true)
    _resetPlatformForTest()
    expect(isPC.value).toBe(false)
  })
})

describe('isPC logic', () => {
  it('isPC = false when isAppMode is true (Android native app)', async () => {
    vi.doMock('@/composables/useAppMode', () => ({
      useAppMode: () => ({ isAppMode: { value: true } }),
    }))
    vi.resetModules()
    const mod = await import('@/composables/usePlatformDetect')
    // Note: module-level UA constants are already computed with jsdom UA,
    // so isAndroidUA/isIOSUA/isIPadOSUA are all false. Only isAppMode blocks isPC.
    const { isPC } = mod.usePlatformDetect()
    expect(isPC.value).toBe(false)
  })

  it('isPC = false for Android browser UA', async () => {
    const androidUA = 'Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 Chrome/120.0.0.0 Mobile Safari/537.36'
    Object.defineProperty(navigator, 'userAgent', { configurable: true, value: androidUA })
    vi.resetModules()
    const mod = await import('@/composables/usePlatformDetect')
    expect(mod.isAndroidUA).toBe(true)
    expect(mod.isIOSUA).toBe(false)
    expect(mod.isIPadOSUA).toBe(false)
    const { isPC } = mod.usePlatformDetect()
    expect(isPC.value).toBe(false)
  })

  it('isPC = false for iOS Safari UA', async () => {
    const iosUA = 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1'
    Object.defineProperty(navigator, 'userAgent', { configurable: true, value: iosUA })
    vi.resetModules()
    const mod = await import('@/composables/usePlatformDetect')
    expect(mod.isAndroidUA).toBe(false)
    expect(mod.isIOSUA).toBe(true)
    expect(mod.isIPadOSUA).toBe(false)
    const { isPC } = mod.usePlatformDetect()
    expect(isPC.value).toBe(false)
  })

  it('isPC = false for iPadOS 13+ desktop-mode UA with maxTouchPoints > 0', async () => {
    const ipadUA = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15'
    Object.defineProperty(navigator, 'userAgent', { configurable: true, value: ipadUA })
    Object.defineProperty(navigator, 'maxTouchPoints', { configurable: true, value: 5 })
    vi.resetModules()
    const mod = await import('@/composables/usePlatformDetect')
    expect(mod.isAndroidUA).toBe(false)
    expect(mod.isIOSUA).toBe(false)
    expect(mod.isIPadOSUA).toBe(true)
    const { isPC } = mod.usePlatformDetect()
    expect(isPC.value).toBe(false)
  })

  it('isPC = true for real Mac desktop UA (Macintosh + maxTouchPoints = 0)', async () => {
    const macUA = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36'
    Object.defineProperty(navigator, 'userAgent', { configurable: true, value: macUA })
    Object.defineProperty(navigator, 'maxTouchPoints', { configurable: true, value: 0 })
    vi.doMock('@/composables/useAppMode', () => ({
      useAppMode: () => ({ isAppMode: { value: false } }),
    }))
    vi.resetModules()
    const mod = await import('@/composables/usePlatformDetect')
    expect(mod.isAndroidUA).toBe(false)
    expect(mod.isIOSUA).toBe(false)
    expect(mod.isIPadOSUA).toBe(false)
    const { isPC } = mod.usePlatformDetect()
    expect(isPC.value).toBe(true)
  })

  it('isPC = true for Windows desktop UA', async () => {
    const winUA = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36'
    Object.defineProperty(navigator, 'userAgent', { configurable: true, value: winUA })
    vi.doMock('@/composables/useAppMode', () => ({
      useAppMode: () => ({ isAppMode: { value: false } }),
    }))
    vi.resetModules()
    const mod = await import('@/composables/usePlatformDetect')
    expect(mod.isAndroidUA).toBe(false)
    expect(mod.isIOSUA).toBe(false)
    expect(mod.isIPadOSUA).toBe(false)
    const { isPC } = mod.usePlatformDetect()
    expect(isPC.value).toBe(true)
  })
})
