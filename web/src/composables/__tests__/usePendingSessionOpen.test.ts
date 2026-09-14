import { describe, expect, it, vi } from 'vitest'
import { nextTick, ref } from 'vue'
import { openPendingSessionWhenReady } from '@/composables/usePendingSessionOpen'

describe('openPendingSessionWhenReady', () => {
  it('opens the pending session immediately when identity is already resolved', () => {
    const currentSessionId = ref('recent-session')
    const switchTab = vi.fn()
    const switchSession = vi.fn()

    const stop = openPendingSessionWhenReady({
      currentSessionId,
      sessionId: 'target-session',
      projectPath: '/project/target',
      switchTab,
      switchSession,
    })

    // Phase 6 (initSessionFromAPI) already set identity, so Phase 7 must open
    // the pending session synchronously — this is the cross-project jump.
    expect(switchTab).toHaveBeenCalledWith('chat')
    expect(switchSession).toHaveBeenCalledWith('target-session', '/project/target')
    expect(typeof stop).toBe('function')
  })

  it('does not re-open the pending session when the user later switches away', async () => {
    // Regression: the old `watch(..., { immediate: true })` self-referencing
    // stop handle threw a TDZ ReferenceError on the immediate run, leaving a
    // stale watcher armed. The user's NEXT session click fired it, which yanked
    // the app back to the pending session (content flicker, click appeared not
    // to work, badge cleared only then). Opening directly must leave no watcher.
    const currentSessionId = ref('recent-session')
    const switchTab = vi.fn()
    const switchSession = vi.fn()

    openPendingSessionWhenReady({
      currentSessionId,
      sessionId: 'target-session',
      projectPath: '/project/target',
      switchTab,
      switchSession,
    })
    switchSession.mockClear()
    switchTab.mockClear()

    // User clicks another session in the now-current project.
    currentSessionId.value = 'other-session'
    await nextTick()

    expect(switchSession).not.toHaveBeenCalled()
    expect(switchTab).not.toHaveBeenCalled()
  })

  it('waits for identity, then opens once and stops watching', async () => {
    const currentSessionId = ref('')
    const switchTab = vi.fn()
    const switchSession = vi.fn()

    openPendingSessionWhenReady({
      currentSessionId,
      sessionId: 'target-session',
      projectPath: '/project/target',
      switchTab,
      switchSession,
    })

    // Identity not ready yet — nothing happens, and crucially no TDZ error.
    expect(switchSession).not.toHaveBeenCalled()

    currentSessionId.value = 'resolved-session'
    await nextTick()
    expect(switchSession).toHaveBeenCalledTimes(1)
    expect(switchSession).toHaveBeenCalledWith('target-session', '/project/target')
    expect(switchTab).toHaveBeenCalledWith('chat')

    // Watcher must have stopped itself: further identity changes are ignored.
    currentSessionId.value = 'another-session'
    await nextTick()
    expect(switchSession).toHaveBeenCalledTimes(1)
  })

  it('stays inert while identity remains empty', async () => {
    const currentSessionId = ref('')
    const switchSession = vi.fn()

    openPendingSessionWhenReady({
      currentSessionId,
      sessionId: 'target-session',
      switchTab: vi.fn(),
      switchSession,
    })

    currentSessionId.value = ''
    await nextTick()
    expect(switchSession).not.toHaveBeenCalled()
  })

  it('omits projectPath for same-project opens', () => {
    const currentSessionId = ref('recent-session')
    const switchSession = vi.fn()

    openPendingSessionWhenReady({
      currentSessionId,
      sessionId: 'target-session',
      switchTab: vi.fn(),
      switchSession,
    })

    expect(switchSession).toHaveBeenCalledWith('target-session', undefined)
  })

  it('opens a resolved session without surfacing a watcher error', () => {
    // Guards the exact defect this composable exists to avoid: a
    // self-referencing stop handle inside an `immediate` watcher throws
    // `ReferenceError: Cannot access 'stop' before initialization` on the first,
    // synchronous invocation. Vue routes that through callWithErrorHandling and
    // (in dev) rethrows after console.error, so the caller sees a crash instead
    // of the session opening. Identity is resolved here — the branch that the
    // cross-project jump actually takes.
    const currentSessionId = ref('resolved-session')
    const switchSession = vi.fn()
    const errors: unknown[] = []
    const spy = vi.spyOn(console, 'error').mockImplementation((...args) => { errors.push(args) })

    try {
      expect(() =>
        openPendingSessionWhenReady({
          currentSessionId,
          sessionId: 'target-session',
          switchTab: vi.fn(),
          switchSession,
        }),
      ).not.toThrow()
    } finally {
      spy.mockRestore()
    }

    expect(errors).toEqual([])
    expect(switchSession).toHaveBeenCalledWith('target-session', undefined)
  })
})
