import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const hostWindow = window as unknown as {
  ClawBenchNative?: unknown
  ClawBenchHarmony?: unknown
}

beforeEach(() => {
  vi.resetModules()
  delete hostWindow.ClawBenchNative
  delete hostWindow.ClawBenchHarmony
  document.documentElement.removeAttribute('data-webview-mode')
})

afterEach(() => {
  delete hostWindow.ClawBenchNative
  delete hostWindow.ClawBenchHarmony
  document.documentElement.removeAttribute('data-webview-mode')
})

describe('HarmonyOS host compatibility', () => {
  it('recognizes HarmonyOS without Android rendering compensation', async () => {
    hostWindow.ClawBenchHarmony = {
      isNativeApp: () => true,
      isDesktopApp: () => false,
      getHostPlatform: () => 'harmonyos',
    }
    const { usePlatformDetect } = await import('../usePlatformDetect')
    const platform = usePlatformDetect()
    expect(platform.isHarmonyApp.value).toBe(true)
    expect(platform.isAndroidApp.value).toBe(false)
    expect(platform.isElectron.value).toBe(false)
    expect(platform.isWebApp.value).toBe(false)
    expect(platform.isNativeApp.value).toBe(true)
    expect(document.documentElement.hasAttribute('data-webview-mode')).toBe(false)
  })

  it('keeps the legacy Android bridge contract', async () => {
    hostWindow.ClawBenchNative = { isNativeApp: () => true }
    const { usePlatformDetect } = await import('../usePlatformDetect')
    const platform = usePlatformDetect()
    expect(platform.isAndroidApp.value).toBe(true)
    expect(platform.isHarmonyApp.value).toBe(false)
    expect(document.documentElement.hasAttribute('data-webview-mode')).toBe(true)
  })

  it('does not trust a platform label without native identity', async () => {
    hostWindow.ClawBenchHarmony = {
      isNativeApp: () => false,
      getHostPlatform: () => 'harmonyos',
    }
    const { getNativeHostPlatform } = await import('@/utils/clawbenchNative')
    expect(getNativeHostPlatform()).toBe('web')
  })

  it('does not offer a tunnel when the server supports it but the host does not', async () => {
    const { portForwardUnavailable } = await import('@/utils/portForwardUtils')
    expect(portForwardUnavailable(true, true, false)).toBe(true)
    expect(portForwardUnavailable(true, true, true)).toBe(false)
    expect(portForwardUnavailable(false, true)).toBe(false)
  })
})
