import { describe, expect, it, vi, beforeEach } from 'vitest'

// Mock useSettingsConfig before importing the module under test
vi.mock('@/composables/useSettingsConfig', () => ({
  get localConfig() { return mockLocalConfig },
}))

const mockLocalConfig: Record<string, unknown> = { notificationSound: true }

describe('useNotificationSound', () => {
  beforeEach(async () => {
    mockLocalConfig.notificationSound = true
    // The coalescing window is module-level state that survives between tests.
    // Without this reset, whichever case plays first suppresses the rest.
    const { _resetSoundThrottleForTesting } = await import('@/composables/useNotificationSound')
    _resetSoundThrottleForTesting()
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

  // ── burst coalescing ──
  //
  // One forge poll derives events in a loop, so a batch sync used to fire one
  // chime per item. The chime is ~350ms long, so overlapping calls turned the
  // alert into noise. Notifications themselves are deduped per item; this only
  // stops the SOUND from stacking.
  //
  // The AudioContext is cached across calls, so counting constructor calls
  // measures "first use", not "how many chimes played". Count oscillators
  // instead — each chime creates exactly two.

  /** Install a stub AudioContext and return a getter for chimes actually played. */
  function stubAudioContext() {
    const oscillators: unknown[] = []
    class FakeAudioContext {
      state = 'running'
      currentTime = 0
      destination = {}
      resume() { /* no-op */ }
      createOscillator() {
        const osc = {
          type: '', frequency: { value: 0 },
          connect() { /* no-op */ },
          start() { oscillators.push(osc) },
          stop() { /* no-op */ },
        }
        return osc
      }
      createGain() {
        return { gain: { setValueAtTime() {}, exponentialRampToValueAtTime() {} }, connect() {} }
      }
    }
    vi.stubGlobal('AudioContext', FakeAudioContext)
    // Two oscillators per chime.
    return () => oscillators.length / 2
  }

  it('plays only one chime for a burst of calls inside the window', async () => {
    mockLocalConfig.notificationSound = true
    const chimes = stubAudioContext()

    const { playNotificationSound, _resetSoundThrottleForTesting } =
      await import('@/composables/useNotificationSound')
    _resetSoundThrottleForTesting()

    // A batch sync: 20 derived events in quick succession.
    for (let i = 0; i < 20; i++) playNotificationSound()

    expect(chimes()).toBe(1)
    vi.unstubAllGlobals()
  })

  it('plays again once the coalescing window has elapsed', async () => {
    mockLocalConfig.notificationSound = true
    const chimes = stubAudioContext()

    const { playNotificationSound, _resetSoundThrottleForTesting } =
      await import('@/composables/useNotificationSound')
    _resetSoundThrottleForTesting()

    const realNow = Date.now
    let clock = 1_000_000
    vi.spyOn(Date, 'now').mockImplementation(() => clock)
    try {
      playNotificationSound()
      // Still inside the window — suppressed.
      clock += 100
      playNotificationSound()
      expect(chimes()).toBe(1)

      // Past the window — a genuinely separate notification must still chime.
      clock += 1000
      playNotificationSound()
      expect(chimes()).toBe(2)
    } finally {
      vi.spyOn(Date, 'now').mockImplementation(realNow)
      vi.unstubAllGlobals()
    }
  })
})
