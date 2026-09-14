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

  it('opens a resolved session without throwing a TDZ error', () => {
    // Guards the exact defect this composable exists to avoid: a
    // self-referencing stop handle inside an `immediate` watcher throws
    // `ReferenceError: Cannot access 'stop' before initialization` on the first,
    // synchronous invocation, so the session never opens. Identity is resolved
    // here — the branch the cross-project jump actually takes.
    //
    // Deliberately asserts on the throw only. An earlier revision also asserted
    // `console.error` was never called, which was vacuous: this path reaches no
    // console.error at all (measured 0 on the buggy shape too), so it could
    // never fail. The throw is the real, load-bearing signal.
    const currentSessionId = ref('resolved-session')
    const switchSession = vi.fn()

    expect(() =>
      openPendingSessionWhenReady({
        currentSessionId,
        sessionId: 'target-session',
        switchTab: vi.fn(),
        switchSession,
      }),
    ).not.toThrow()

    expect(switchSession).toHaveBeenCalledWith('target-session', undefined)
  })

  it('does not open a stale session after the user switches to an unrelated project', async () => {
    // Regression for a residual hazard: when identity is unresolved at Phase 7 a
    // fallback watcher must be armed (ChatPanel's loadHistory recovery resolves
    // identity slightly later). Without a relevance guard that watcher outlives
    // its navigation, so switching to an UNRELATED project and resolving ITS
    // identity would open the stale session there.
    const currentSessionId = ref('')   // initSessionFromAPI failed/aborted
    const switchSession = vi.fn()
    let project = '/project/B'

    openPendingSessionWhenReady({
      currentSessionId,
      sessionId: 'session-in-B',
      projectPath: '/project/B',
      switchTab: vi.fn(),
      switchSession,
      isStillRelevant: () => project === '/project/B',
    })

    // Nothing yet — identity is unresolved, as designed.
    expect(switchSession).not.toHaveBeenCalled()

    // User navigates to an unrelated project C, whose identity then resolves.
    project = '/project/C'
    currentSessionId.value = 'session-in-C'
    await nextTick()

    expect(switchSession).not.toHaveBeenCalled()
  })

  it('stops watching after being disarmed by the relevance guard', async () => {
    // Once the guard has fired, the watcher must be fully dead — not merely
    // inert for one tick.
    const currentSessionId = ref('')
    const switchSession = vi.fn()
    let project = '/project/B'

    openPendingSessionWhenReady({
      currentSessionId,
      sessionId: 'session-in-B',
      projectPath: '/project/B',
      switchTab: vi.fn(),
      switchSession,
      isStillRelevant: () => project === '/project/B',
    })

    project = '/project/C'
    currentSessionId.value = 'c-1'
    await nextTick()
    expect(switchSession).not.toHaveBeenCalled()

    // Even if the user comes back to project B and identity changes again, the
    // stale navigation must not resurrect.
    project = '/project/B'
    currentSessionId.value = 'b-2'
    await nextTick()
    expect(switchSession).not.toHaveBeenCalled()
  })

  it('still opens the pending session when the project is still relevant', async () => {
    // The guard must not break the legitimate path it wraps.
    const currentSessionId = ref('')
    const switchSession = vi.fn()

    openPendingSessionWhenReady({
      currentSessionId,
      sessionId: 'session-in-B',
      projectPath: '/project/B',
      switchTab: vi.fn(),
      switchSession,
      isStillRelevant: () => true,
    })

    currentSessionId.value = 'resolved-in-B'
    await nextTick()

    expect(switchSession).toHaveBeenCalledTimes(1)
    expect(switchSession).toHaveBeenCalledWith('session-in-B', '/project/B')
  })

  it('does not open when relevance already fails on the resolved path', () => {
    // The direct (identity-already-resolved) branch consults the guard too.
    const currentSessionId = ref('resolved-session')
    const switchSession = vi.fn()

    openPendingSessionWhenReady({
      currentSessionId,
      sessionId: 'session-in-B',
      projectPath: '/project/B',
      switchTab: vi.fn(),
      switchSession,
      isStillRelevant: () => false,
    })

    expect(switchSession).not.toHaveBeenCalled()
  })
})
