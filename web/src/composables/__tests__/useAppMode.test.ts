import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'

// Must mock before import
const originalWindowTop = window.top

describe('useAppMode', () => {
  beforeEach(() => {
    // Reset module cache between tests
    vi.resetModules()
    // Reset document attribute
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
