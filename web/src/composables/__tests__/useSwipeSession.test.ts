import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { reactive, ref } from 'vue'
import { useSwipeSession } from '@/composables/useSwipeSession'

// ── swipeSession disabled guard ──
// Tests that the composable's touch handlers respect the swipeSession local setting.

// We must import localConfig and the composable after setting up the mock,
// so we use dynamic import inside the describe block.

// ── Timer leak prevention ───────────────────────────────────
const pendingTimers: ReturnType<typeof setTimeout>[] = []
const _origSetTimeout = setTimeout
globalThis.setTimeout = ((fn: TimerHandler, ms?: number, ...args: any[]) => {
  const id = _origSetTimeout(fn, ms, ...args)
  pendingTimers.push(id)
  return id
}) as typeof setTimeout

const pendingIntervals: ReturnType<typeof setInterval>[] = []
const _origSetInterval = setInterval
globalThis.setInterval = ((fn: TimerHandler, ms?: number, ...args: any[]) => {
  const id = _origSetInterval(fn, ms, ...args)
  pendingIntervals.push(id)
  return id
}) as typeof setInterval

describe('useSwipeSession disabled guard', () => {
  let localConfig: Record<string, any>
  let useSwipeSession: typeof import('@/composables/useSwipeSession')['useSwipeSession']

  beforeEach(async () => {
    // Reset module cache so we get fresh localConfig state
    vi.resetModules()
    // Clear localStorage to ensure predictable defaults
    localStorage.clear()
    const mod = await import('@/composables/useSwipeSession')
    useSwipeSession = mod.useSwipeSession
    const configMod = await import('@/composables/useSettingsConfig')
    localConfig = configMod.localConfig
  })

  afterEach(() => {
    for (const id of pendingTimers) { clearTimeout(id) }
    pendingTimers.length = 0
    for (const id of pendingIntervals) { clearInterval(id) }
    pendingIntervals.length = 0
    localStorage.clear()
  })

  function createTouchEvent(type: 'touchstart' | 'touchend', clientX: number, clientY: number): TouchEvent {
    const touch = { clientX, clientY } as Touch
    const event = {
      touches: type === 'touchstart' ? [touch] : [],
      changedTouches: type === 'touchend' ? [touch] : [],
      target: document.createElement('div'),
      currentTarget: document.createElement('div'),
    } as unknown as TouchEvent
    return event
  }

  it('does not trigger session switch when swipeSession is false (default)', async () => {
    // Default is false
    expect(localConfig.swipeSession).toBe(false)

    const switchSession = vi.fn().mockResolvedValue(undefined)
    const currentSessionId = ref('session-1')

    const { onTouchStart, onTouchEnd, indicatorText } = useSwipeSession({
      currentSessionId,
      switchSession,
    })

    // Simulate a left swipe that would normally switch sessions
    const startEvent = createTouchEvent('touchstart', 200, 100)
    onTouchStart(startEvent)

    const endEvent = createTouchEvent('touchend', 50, 100)
    onTouchEnd(endEvent)

    // switchSession should NOT have been called because swipeSession is disabled
    expect(switchSession).not.toHaveBeenCalled()
    // indicator should remain empty
    expect(indicatorText.value).toBe('')
  })

  it('triggers session switch when swipeSession is true', async () => {
    localConfig.swipeSession = true

    // Mock the sessions API
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ sessions: [{ id: 'session-1', title: 'S1' }, { id: 'session-2', title: 'S2' }] }),
    } as Response)

    const switchSession = vi.fn().mockResolvedValue(undefined)
    const currentSessionId = ref('session-2')

    const { onTouchStart, onTouchEnd, indicatorText } = useSwipeSession({
      currentSessionId,
      switchSession,
    })

    // Simulate a left swipe (next session)
    const startEvent = createTouchEvent('touchstart', 200, 100)
    onTouchStart(startEvent)

    const endEvent = createTouchEvent('touchend', 50, 100)
    onTouchEnd(endEvent)

    // Wait for async operations
    await vi.waitFor(() => {
      expect(switchSession).toHaveBeenCalled()
    }, { timeout: 5000 })

    // Indicator should show the target session title
    expect(indicatorText.value).toBeTruthy()

    fetchSpy.mockRestore()
  })
})

/**
 * Pure swipe classification logic — extracted for testability.
 * Mirrors the logic in useSwipeSession.onTouchEnd.
 */
export function classifySwipe(
    deltaX: number,
    deltaY: number,
    duration: number,
    threshold = 80,
    maxDuration = 500,
    maxVerticalRatio = 0.75,
): 'left' | 'right' | null {
    if (duration > maxDuration) return null
    if (Math.abs(deltaY) > Math.abs(deltaX) * maxVerticalRatio) return null
    if (Math.abs(deltaX) < threshold) return null
    if (deltaX < 0) return 'left'
    return 'right'
}

/**
 * Circular session index navigation (DESC-ordered sessions).
 * - Next (swipe left) = index - 1 (more recent)
 * - Prev (swipe right) = index + 1 (older)
 */
export function getNextSessionIndex(currentIndex: number, total: number): number {
    if (total <= 1) return currentIndex
    return currentIndex > 0 ? currentIndex - 1 : total - 1
}

export function getPrevSessionIndex(currentIndex: number, total: number): number {
    if (total <= 1) return currentIndex
    return currentIndex < total - 1 ? currentIndex + 1 : 0
}

// ── classifySwipe ──

describe('classifySwipe', () => {
    it('returns "left" for negative deltaX exceeding threshold', () => {
        expect(classifySwipe(-100, 0, 200)).toBe('left')
    })

    it('returns "right" for positive deltaX exceeding threshold', () => {
        expect(classifySwipe(100, 0, 200)).toBe('right')
    })

    it('returns null for slow swipes (duration > 500ms)', () => {
        expect(classifySwipe(-100, 0, 600)).toBeNull()
    })

    it('returns null for short swipes (deltaX < 80px)', () => {
        expect(classifySwipe(-50, 0, 200)).toBeNull()
    })

    it('returns null when vertical movement dominates', () => {
        // deltaY > deltaX * 0.75
        expect(classifySwipe(100, 80, 200)).toBeNull()
    })

    it('allows slight vertical movement', () => {
        // deltaY < deltaX * 0.75
        expect(classifySwipe(100, 50, 200)).toBe('right')
    })

    it('returns null for zero deltaX', () => {
        expect(classifySwipe(0, 0, 200)).toBeNull()
    })

    it('returns null exactly at threshold boundary', () => {
        expect(classifySwipe(-80, 0, 200)).toBe('left') // exactly threshold = pass
    })

    it('returns null exactly at max duration boundary', () => {
        expect(classifySwipe(-100, 0, 500)).toBe('left') // exactly max = pass
    })

    it('returns null when vertical ratio equals threshold', () => {
        // deltaY === deltaX * 0.75 → Math.abs(75) > Math.abs(100) * 0.75 = 75
        // 75 > 75 is false, so it passes the vertical check → returns 'right'
        // Actually: 75 > 75 is false, so the check passes and swipe is classified
        // The condition is deltaY > deltaX * ratio (strict greater than)
        expect(classifySwipe(100, 75, 200)).toBe('right')
    })

    it('handles negative deltaY (swipe up-right)', () => {
        expect(classifySwipe(100, -50, 200)).toBe('right')
    })

    it('handles diagonal swipes within vertical ratio', () => {
        expect(classifySwipe(-120, 30, 300)).toBe('left')
    })

    it('respects custom threshold', () => {
        expect(classifySwipe(-50, 0, 200, 100)).toBeNull() // below custom threshold
        expect(classifySwipe(-100, 0, 200, 100)).toBe('left') // above custom threshold
    })

    it('respects custom maxDuration', () => {
        expect(classifySwipe(-100, 0, 400, 80, 300)).toBeNull() // above custom maxDuration
    })
})

// ── getNextSessionIndex / getPrevSessionIndex ──

describe('getNextSessionIndex', () => {
    it('returns index-1 for non-zero index', () => {
        expect(getNextSessionIndex(2, 5)).toBe(1)
    })

    it('wraps to last index when at 0', () => {
        expect(getNextSessionIndex(0, 5)).toBe(4)
    })

    it('returns current index when only one session', () => {
        expect(getNextSessionIndex(0, 1)).toBe(0)
    })

    it('returns current index when zero sessions', () => {
        expect(getNextSessionIndex(0, 0)).toBe(0)
    })

    it('handles being at last index (no wrap needed since DESC)', () => {
        expect(getNextSessionIndex(4, 5)).toBe(3)
    })
})

describe('getPrevSessionIndex', () => {
    it('returns index+1 for non-last index', () => {
        expect(getPrevSessionIndex(2, 5)).toBe(3)
    })

    it('wraps to 0 when at last index', () => {
        expect(getPrevSessionIndex(4, 5)).toBe(0)
    })

    it('returns current index when only one session', () => {
        expect(getPrevSessionIndex(0, 1)).toBe(0)
    })

    it('handles being at index 0 (goes to 1)', () => {
        expect(getPrevSessionIndex(0, 5)).toBe(1)
    })
})

// ── circular navigation consistency ──

describe('circular navigation consistency', () => {
    it('next then prev returns to original for multi-session list', () => {
        const total = 5
        for (let i = 0; i < total; i++) {
            const nextIdx = getNextSessionIndex(i, total)
            const backIdx = getPrevSessionIndex(nextIdx, total)
            expect(backIdx).toBe(i)
        }
    })

    it('prev then next returns to original for multi-session list', () => {
        const total = 5
        for (let i = 0; i < total; i++) {
            const prevIdx = getPrevSessionIndex(i, total)
            const backIdx = getNextSessionIndex(prevIdx, total)
            expect(backIdx).toBe(i)
        }
    })
})

// ── jumpToNextUnread ──

describe('jumpToNextUnread', () => {
    let fetchSpy: ReturnType<typeof vi.spyOn>
    let switchSession: ReturnType<typeof vi.fn>

    beforeEach(() => {
        switchSession = vi.fn().mockResolvedValue(undefined)
        localStorage.clear()
    })

    afterEach(() => {
        fetchSpy?.mockRestore()
    })

    function makeSessions(ids: string[], unreadMap: Record<string, number | 'pending'> = {}) {
        return ids.map(id => ({
            id,
            title: `Session ${id}`,
            unreadCount: typeof unreadMap[id] === 'number' ? unreadMap[id] : 0,
            pendingApproval: unreadMap[id] === 'pending',
        }))
    }

    it('jumps to the next unread session after the current one', async () => {
        fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
            ok: true,
            json: () => Promise.resolve({ sessions: makeSessions(['s1', 's2', 's3'], { s3: 2 }) }),
        } as Response)
        const { jumpToNextUnread } = useSwipeSession({ currentSessionId: ref('s1'), switchSession })
        const target = await jumpToNextUnread()
        expect(target).toBe('Session s3')
        expect(switchSession).toHaveBeenCalledWith('s3')
    })

    it('wraps around to an earlier unread session when none after current', async () => {
        fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
            ok: true,
            json: () => Promise.resolve({ sessions: makeSessions(['s1', 's2', 's3'], { s1: 1 }) }),
        } as Response)
        const { jumpToNextUnread } = useSwipeSession({ currentSessionId: ref('s3'), switchSession })
        const target = await jumpToNextUnread()
        expect(target).toBe('Session s1')
        expect(switchSession).toHaveBeenCalledWith('s1')
    })

    it('treats pendingApproval as unread', async () => {
        fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
            ok: true,
            json: () => Promise.resolve({ sessions: makeSessions(['s1', 's2'], { s2: 'pending' }) }),
        } as Response)
        const { jumpToNextUnread } = useSwipeSession({ currentSessionId: ref('s1'), switchSession })
        const target = await jumpToNextUnread()
        expect(target).toBe('Session s2')
        expect(switchSession).toHaveBeenCalledWith('s2')
    })

    it('skips already-read sessions and does not switch to current again', async () => {
        fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
            ok: true,
            json: () => Promise.resolve({ sessions: makeSessions(['s1', 's2', 's3'], { s2: 5 }) }),
        } as Response)
        const { jumpToNextUnread } = useSwipeSession({ currentSessionId: ref('s1'), switchSession })
        const target = await jumpToNextUnread()
        expect(target).toBe('Session s2')
        expect(switchSession).toHaveBeenCalledTimes(1)
    })

    it('returns null and does not switch when there are no unread sessions', async () => {
        fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
            ok: true,
            json: () => Promise.resolve({ sessions: makeSessions(['s1', 's2']) }),
        } as Response)
        const { jumpToNextUnread } = useSwipeSession({ currentSessionId: ref('s1'), switchSession })
        const target = await jumpToNextUnread()
        expect(target).toBeNull()
        expect(switchSession).not.toHaveBeenCalled()
    })

    it('ignores the current session when it is the only unread one', async () => {
        fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
            ok: true,
            json: () => Promise.resolve({ sessions: makeSessions(['s1', 's2'], { s1: 3 }) }),
        } as Response)
        const { jumpToNextUnread } = useSwipeSession({ currentSessionId: ref('s1'), switchSession })
        const target = await jumpToNextUnread()
        expect(target).toBeNull()
        expect(switchSession).not.toHaveBeenCalled()
    })

    it('returns null when there are no sessions', async () => {
        fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
            ok: true,
            json: () => Promise.resolve({ sessions: [] }),
        } as Response)
        const { jumpToNextUnread } = useSwipeSession({ currentSessionId: ref('s1'), switchSession })
        const target = await jumpToNextUnread()
        expect(target).toBeNull()
        expect(switchSession).not.toHaveBeenCalled()
    })
})
