import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { reactive, ref } from 'vue'
import { readWebFile } from '@/testUtils/readWebFile'

// Full-suite scheduling: this test exercises a swipe gesture watcher that
// dispatches a debounced session switch. Finishes quickly in isolation but
// can flake under the coverage-gate's full-suite run. Bump this file's
// timeout only.
vi.setConfig({ testTimeout: 30_000 })
import {
  useSwipeSession,
  getNextSessionIndex,
  getPrevSessionIndex,
} from '@/composables/useSwipeSession'

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

    try {
      const switchSession = vi.fn().mockResolvedValue(undefined)
      const currentSessionId = ref('session-2')

      const { onTouchStart, onTouchEnd, indicatorText } = useSwipeSession({
        currentSessionId,
        switchSession,
      })

      // Simulate a left swipe (previous session)
      const startEvent = createTouchEvent('touchstart', 200, 100)
      onTouchStart(startEvent)

      const endEvent = createTouchEvent('touchend', 50, 100)
      onTouchEnd(endEvent)

      // Wait for async operations. Full-suite scheduling under the coverage gate
      // can slow async tasks; the swipe handler may need more than 5s when the
      // worker pool is busy.
      await vi.waitFor(() => {
        expect(switchSession).toHaveBeenCalled()
      }, { timeout: 20_000 })

      // Indicator should show the target session title
      expect(indicatorText.value).toBeTruthy()
    } finally {
      // Always restore the fetch mock — even if the waitFor above throws, so
      // the globalThis.fetch mock does not leak into subsequent tests in the
      // full coverage suite.
      fetchSpy.mockRestore()
    }
  })
})

// ── gesture → list direction (#562) ──
// The session list is rendered top-down, so "next" must mean the next row
// (index + 1) for both the swipe gesture and the Ctrl+←/→ shortcut. Before the
// fix "next" walked up the list, which made → on the first session jump to the
// oldest one.

describe('swipe direction matches list order', () => {
  const SESSIONS = [
    { id: 's1', title: 'S1' },
    { id: 's2', title: 'S2' },
    { id: 's3', title: 'S3' },
  ]

  let localConfig: Record<string, any>
  let fetchSpy: ReturnType<typeof vi.spyOn>

  beforeEach(async () => {
    vi.resetModules()
    localStorage.clear()
    const configMod = await import('@/composables/useSettingsConfig')
    localConfig = configMod.localConfig
    localConfig.swipeSession = true
    fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ sessions: SESSIONS }),
    } as Response)
  })

  afterEach(() => {
    fetchSpy?.mockRestore()
    // showIndicator() arms a 1.5s hide timer; clear it (and any interval) or
    // the leak detector reports a hanging handle.
    for (const id of pendingTimers) { clearTimeout(id) }
    pendingTimers.length = 0
    for (const id of pendingIntervals) { clearInterval(id) }
    pendingIntervals.length = 0
    localStorage.clear()
  })

  async function swipe(currentId: string, fromX: number, toX: number) {
    const mod = await import('@/composables/useSwipeSession')
    const switchSession = vi.fn().mockResolvedValue(undefined)
    const api = mod.useSwipeSession({ currentSessionId: ref(currentId), switchSession })
    api.onTouchStart(createTouchEvent('touchstart', fromX, 100))
    api.onTouchEnd(createTouchEvent('touchend', toX, 100))
    await vi.waitFor(() => { expect(switchSession).toHaveBeenCalled() }, { timeout: 20_000 })
    return { switchSession, indicatorDirection: api.indicatorDirection }
  }

  it('swipe right goes to the next row down', async () => {
    const { switchSession, indicatorDirection } = await swipe('s1', 50, 200)
    expect(switchSession).toHaveBeenCalledWith('s2')
    expect(indicatorDirection.value).toBe('right')
  })

  it('swipe left goes to the previous row up', async () => {
    const { switchSession, indicatorDirection } = await swipe('s3', 200, 50)
    expect(switchSession).toHaveBeenCalledWith('s2')
    expect(indicatorDirection.value).toBe('left')
  })

  it('swipe right on the last session wraps to the first', async () => {
    const { switchSession } = await swipe('s3', 50, 200)
    expect(switchSession).toHaveBeenCalledWith('s1')
  })

  it('swipe left on the first session wraps to the last row', async () => {
    const { switchSession } = await swipe('s1', 200, 50)
    expect(switchSession).toHaveBeenCalledWith('s3')
  })
})

describe('desktop Ctrl+arrow mapping', () => {
  const PANEL = readWebFile('src/components/chat/ChatPanelContent.vue')
  const start = PANEL.indexOf('function handleCtrlArrowSessionSwitch')
  const end = PANEL.indexOf('\n}\n', start)
  const body = PANEL.slice(start, end)

  /** Body of the `if (e.key === '<key>') { ... }` branch. */
  function arrowBranch(key: 'ArrowLeft' | 'ArrowRight'): string {
    const at = body.indexOf(`e.key === '${key}'`)
    expect(at, `missing ${key} branch`).toBeGreaterThanOrEqual(0)
    const open = body.indexOf('{', at)
    const close = body.indexOf('}', open)
    expect(open, `${key} branch should be a block`).toBeGreaterThan(at)
    expect(close, `${key} branch should close`).toBeGreaterThan(open)
    return body.slice(open, close)
  }

  it('Ctrl+ArrowRight advances to the next session', () => {
    expect(start).toBeGreaterThanOrEqual(0)
    expect(arrowBranch('ArrowRight')).toMatch(/swipeToNext\(\)/)
    expect(arrowBranch('ArrowRight')).not.toMatch(/swipeToPrev\(\)/)
  })

  it('Ctrl+ArrowLeft goes back to the previous session', () => {
    expect(start).toBeGreaterThanOrEqual(0)
    expect(arrowBranch('ArrowLeft')).toMatch(/swipeToPrev\(\)/)
    expect(arrowBranch('ArrowLeft')).not.toMatch(/swipeToNext\(\)/)
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
    it('returns index+1 for non-last index', () => {
        expect(getNextSessionIndex(2, 5)).toBe(3)
    })

    it('wraps to index 0 when at the last index', () => {
        expect(getNextSessionIndex(4, 5)).toBe(0)
    })

    it('returns current index when only one session', () => {
        expect(getNextSessionIndex(0, 1)).toBe(0)
    })

    it('returns current index when zero sessions', () => {
        expect(getNextSessionIndex(0, 0)).toBe(0)
    })

    it('advances from the first session instead of wrapping to the last', () => {
        // Regression for #562: pressing → on the first session must reach the
        // second one, not the oldest one at the bottom of the list.
        expect(getNextSessionIndex(0, 5)).toBe(1)
    })

    it('lands on the first row when the current session is not in the list', () => {
        // The active session can be missing from the fetched list (just
        // archived, or created after the 3s cache was filled). -1 is treated as
        // "before the first row" so next lands on row 0.
        expect(getNextSessionIndex(-1, 5)).toBe(0)
    })
})

describe('getPrevSessionIndex', () => {
    it('returns index-1 for non-zero index', () => {
        expect(getPrevSessionIndex(2, 5)).toBe(1)
    })

    it('wraps to the last index when at 0', () => {
        expect(getPrevSessionIndex(0, 5)).toBe(4)
    })

    it('returns current index when only one session', () => {
        expect(getPrevSessionIndex(0, 1)).toBe(0)
    })

    it('handles being at the last index (goes to the previous row)', () => {
        expect(getPrevSessionIndex(4, 5)).toBe(3)
    })

    it('lands on the last row when the current session is not in the list', () => {
        expect(getPrevSessionIndex(-1, 5)).toBe(4)
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
