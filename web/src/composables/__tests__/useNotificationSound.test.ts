import { describe, expect, it, vi, beforeEach } from 'vitest'

// Mock useSettingsConfig before importing the module under test
vi.mock('@/composables/useSettingsConfig', () => ({
  get localConfig() { return mockLocalConfig },
}))

const mockLocalConfig: Record<string, unknown> = { notificationSound: true }

describe('useNotificationSound', () => {
  beforeEach(() => {
    mockLocalConfig.notificationSound = true
  })

  it('exports play function without throwing', async () => {
    const { useNotificationSound } = await import('@/composables/useNotificationSound')
    const { play } = useNotificationSound()
    expect(typeof play).toBe('function')
  })

  it('playNotificationSound catches AudioContext errors gracefully', async () => {
    const { playNotificationSound } = await import('@/composables/useNotificationSound')
    // In test environment, AudioContext is not available
    // The function should not throw — it catches errors internally
    expect(() => playNotificationSound()).not.toThrow()
  })

  it('playNotificationSound skips AudioContext when notificationSound is false', async () => {
    mockLocalConfig.notificationSound = false
    const audioCtxSpy = vi.fn()
    vi.stubGlobal('AudioContext', audioCtxSpy)

    const { playNotificationSound } = await import('@/composables/useNotificationSound')
    playNotificationSound()

    // AudioContext constructor should never be called when setting is off
    expect(audioCtxSpy).not.toHaveBeenCalled()
    vi.unstubAllGlobals()
  })

  it('playNotificationSound attempts AudioContext when notificationSound is true', async () => {
    mockLocalConfig.notificationSound = true
    const audioCtxSpy = vi.fn()
    vi.stubGlobal('AudioContext', audioCtxSpy)

    const { playNotificationSound } = await import('@/composables/useNotificationSound')
    playNotificationSound()

    // AudioContext constructor should be called when setting is on
    expect(audioCtxSpy).toHaveBeenCalled()
    vi.unstubAllGlobals()
  })
})
