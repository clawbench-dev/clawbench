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

  // ── hidden page ──
  //
  // #728: reply finalisation and the reconnect replay of the same completion
  // both used to hit a backgrounded page. Hidden means no speaker (the OS
  // notification is the alert there), but the chime is deferred rather than
  // dropped — one sound on return, covering a whole background burst.

  it('defers the chime while hidden and plays one on return', async () => {
    mockLocalConfig.notificationSound = true
    const chimes = stubAudioContext()
    const { playNotificationSound, _resetSoundThrottleForTesting } =
      await import('@/composables/useNotificationSound')
    _resetSoundThrottleForTesting()

    const vis = vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
    try {
      // Live finalisation + the replay of the same completion + another one.
      playNotificationSound()
      playNotificationSound()
      playNotificationSound()
      expect(chimes()).toBe(0)

      vis.mockReturnValue('visible')
      document.dispatchEvent(new Event('visibilitychange'))
      expect(chimes()).toBe(1)

      // Consumed: a second visibilitychange with nothing new must stay silent.
      document.dispatchEvent(new Event('visibilitychange'))
      expect(chimes()).toBe(1)
    } finally {
      vis.mockRestore()
      vi.unstubAllGlobals()
    }
  })

  it('chimes immediately when nothing was queued while visible', async () => {
    mockLocalConfig.notificationSound = true
    const chimes = stubAudioContext()
    const { playNotificationSound, _resetSoundThrottleForTesting } =
      await import('@/composables/useNotificationSound')
    _resetSoundThrottleForTesting()

    playNotificationSound()
    expect(chimes()).toBe(1)
    document.dispatchEvent(new Event('visibilitychange'))
    expect(chimes()).toBe(1)
    vi.unstubAllGlobals()
  })

  it('plays immediately while hidden when the caller asks for it', async () => {
    // Meeting control requests are the reason this exists: they are meant for
    // a host who is NOT on the meeting page, so deferring them to the next
    // return would be the opposite of the alert.
    mockLocalConfig.notificationSound = true
    const chimes = stubAudioContext()
    const { playImmediateNotificationSound, _resetSoundThrottleForTesting } =
      await import('@/composables/useNotificationSound')
    _resetSoundThrottleForTesting()

    const vis = vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
    try {
      playImmediateNotificationSound()
      expect(chimes()).toBe(1)
    } finally {
      vis.mockRestore()
      vi.unstubAllGlobals()
    }
  })

  it('does not play the deferred chime if the sound was muted meanwhile', async () => {
    mockLocalConfig.notificationSound = true
    const chimes = stubAudioContext()
    const { playNotificationSound, _resetSoundThrottleForTesting } =
      await import('@/composables/useNotificationSound')
    _resetSoundThrottleForTesting()

    const vis = vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
    try {
      playNotificationSound()
      mockLocalConfig.notificationSound = false
      vis.mockReturnValue('visible')
      document.dispatchEvent(new Event('visibilitychange'))
      expect(chimes()).toBe(0)
    } finally {
      vis.mockRestore()
      vi.unstubAllGlobals()
    }
  })

  // ── continuous-conversation cues ──
  //
  // "sent" and "closed" are part of the voice mode, not completion alerts, so
  // they are neither gated on `notificationSound` nor coalesced with the chime.

  it('plays a two-tone cue for each voice-mode event', async () => {
    const chimes = stubAudioContext()
    const { playVoiceModeCue, _resetSoundThrottleForTesting } =
      await import('@/composables/useNotificationSound')
    _resetSoundThrottleForTesting()

    playVoiceModeCue('send')
    expect(chimes()).toBe(1)
    // Immediately after — the notification coalescing window must not swallow
    // it. A cue that arrives right after a completion chime is still the
    // answer to "did it send?".
    playVoiceModeCue('exit')
    expect(chimes()).toBe(2)
    // The interruption cue is its own shape and must not be dropped as a
    // duplicate of either neighbour.
    playVoiceModeCue('interrupt')
    expect(chimes()).toBe(3)
    vi.unstubAllGlobals()
  })

  it('plays the cue even when the completion chime is turned off', async () => {
    // That switch is about task alerts ("avoid interrupting Bluetooth audio");
    // the cue is feedback for a mode the user just turned on, and dropping it
    // silently would leave the mode with no feedback at all.
    mockLocalConfig.notificationSound = false
    const audioCtxSpy = vi.fn()
    vi.stubGlobal('AudioContext', audioCtxSpy)

    const { playVoiceModeCue, _resetSoundThrottleForTesting } =
      await import('@/composables/useNotificationSound')
    _resetSoundThrottleForTesting()
    playVoiceModeCue('send')

    expect(audioCtxSpy).toHaveBeenCalled()
    vi.unstubAllGlobals()
  })

  it('never throws when audio is unavailable', async () => {
    const { playVoiceModeCue, _resetSoundThrottleForTesting } =
      await import('@/composables/useNotificationSound')
    _resetSoundThrottleForTesting()
    expect(() => playVoiceModeCue('exit')).not.toThrow()
  })

  // ── unlocking the cue context ──
  //
  // Cues fire from speech/turn events, never a tap, so a context first created
  // there can stay suspended under a strict autoplay policy — the whole mode
  // then has no sound at all while looking perfectly healthy.

  it('resumes a suspended context when the mode is entered', async () => {
    const resume = vi.fn(() => Promise.resolve())
    class SuspendedContext {
      state = 'suspended'
      currentTime = 0
      destination = {}
      resume = resume
    }
    vi.stubGlobal('AudioContext', SuspendedContext as unknown as typeof AudioContext)
    const { unlockVoiceAudio, _resetSoundThrottleForTesting } =
      await import('@/composables/useNotificationSound')
    _resetSoundThrottleForTesting()

    unlockVoiceAudio()

    expect(resume).toHaveBeenCalled()
    vi.unstubAllGlobals()
  })

  it('never throws when there is no Web Audio to unlock', async () => {
    const { unlockVoiceAudio } = await import('@/composables/useNotificationSound')
    expect(() => unlockVoiceAudio()).not.toThrow()
  })
})
