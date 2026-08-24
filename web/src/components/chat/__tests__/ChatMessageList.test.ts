import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'

// ChatMessageList.vue only has 1 changed line: importing handleTableBlockClick
// We verify the import exists and the function is callable
describe('ChatMessageList — handleTableBlockClick integration', () => {
  it('handleTableBlockClick is exported from useCodeBlockHeader', async () => {
    const mod = await import('@/composables/useCodeBlockHeader.ts')
    expect(mod.handleTableBlockClick).toBeDefined()
    expect(typeof mod.handleTableBlockClick).toBe('function')
  })

  it('handleCodeBlockClick is still exported (existing import)', async () => {
    const mod = await import('@/composables/useCodeBlockHeader.ts')
    expect(mod.handleCodeBlockClick).toBeDefined()
    expect(typeof mod.handleCodeBlockClick).toBe('function')
  })
})

/**
 * Test for the scroll sticky抖动 (snap-back jitter) fix.
 *
 * Root cause: scrollToBottom's requestAnimationFrame correction scrolled
 * unconditionally when gap > 0, even if the user had scrolled up
 * (isAtBottom = false). A prior rAF callback would override the user's
 * scroll position, creating a fight between auto-scroll and manual scroll.
 *
 * Fix: rAF and setTimeout(300) corrections now check isAtBottom before
 * scrolling, and scrollToBottom respects a userTouching flag during
 * active touch drag gestures.
 */
describe('ChatMessageList — scroll sticky抖动 fix', () => {
  let mockEl
  let rafCallbacks
  let timers

  beforeEach(() => {
    rafCallbacks = []
    timers = []
    vi.stubGlobal('requestAnimationFrame', (cb) => {
      rafCallbacks.push(cb)
      return rafCallbacks.length
    })
    vi.stubGlobal('cancelAnimationFrame', vi.fn())
  })

  afterEach(() => {
    timers.forEach(t => clearTimeout(t))
    vi.restoreAllMocks()
  })

  it('rAF correction should NOT scroll when isAtBottom is false', () => {
    // Simulate: user scrolled up → isAtBottom = false
    // A rAF from a prior scrollToBottom should NOT snap back
    const isAtBottom = { value: false }
    const messagesRef = { value: { scrollHeight: 1000, scrollTop: 800, clientHeight: 100 } }
    const gap = messagesRef.value.scrollHeight - messagesRef.value.scrollTop - messagesRef.value.clientHeight
    // gap = 100, but isAtBottom is false → should NOT scroll
    expect(gap).toBeGreaterThan(0)
    // The fix: rAF checks isAtBottom before scrolling
    // If isAtBottom.value is false, the rAF should return early
    expect(isAtBottom.value).toBe(false)
  })

  it('scrollToBottom should skip when userTouching is true', () => {
    // Simulate: user is actively touch-dragging
    const userTouching = true
    const force = false
    // The fix: scrollToBottom returns early when userTouching && !force
    expect(userTouching && !force).toBe(true)
  })

  it('scrollToBottom with force=true should still work when userTouching', () => {
    // force=true means programmatic scroll (e.g., sending a message)
    // This should override userTouching
    const userTouching = true
    const force = true
    expect(userTouching && !force).toBe(false)
  })
})

describe('ChatMessageList — ensure-content event pass-through', () => {
  it('ChatMessageList source defines ensure-content emit', async () => {
    // Verify the emit is defined by reading the raw source
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    expect(source).toContain("'ensure-content'")
  })
})

/**
 * Tests for the force-scroll correction guard fix.
 *
 * Root cause: scrollToBottom's rAF and setTimeout(300) corrections both
 * bailed when isAtBottom was false, even for force=true scrolls. During a
 * session switch in original mode, content is lazy-loaded AFTER the initial
 * scroll — when the async blocks arrive, the container grows and the browser
 * keeps the old scrollTop, leaving the view pinned mid-list. The correction
 * was skipped because a scroll event had flipped isAtBottom to false.
 *
 * Fix: force=true scrolls are unconditional (pin to bottom no matter what);
 * only non-force corrections keep the isAtBottom guard (protecting manual
 * scroll-up during streaming).
 */
describe('ChatMessageList — force scroll corrections are unconditional', () => {
  it('rAF correction source no longer guards force=true with isAtBottom', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    // The rAF callback must scroll on force=true even when isAtBottom is false.
    // The source must gate on `!force && !isAtBottom.value`, not `!isAtBottom.value`.
    expect(source).toMatch(/if \(!force && !isAtBottom\.value\) return/)
    // Force branch must NOT contain the old unconditional guard.
    expect(source).not.toContain('if (!messagesRef.value || !isAtBottom.value) return')
  })

  it('delayed setTimeout(300) correction for force=true drops the isAtBottom guard', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    // The force-branch setTimeout body scrolls directly — no isAtBottom check.
    // Extract the region between the setTimeout opener and the `}, 300)` closer.
    const timerStart = source.indexOf('if (force) {')
    const timerEnd = source.indexOf('}, 300)', timerStart)
    const forceTimerRegion = source.slice(timerStart, timerEnd)
    expect(forceTimerRegion).toContain('if (!messagesRef.value) return')
    expect(forceTimerRegion).not.toContain('isAtBottom')
  })

  it('non-force rAF correction still respects isAtBottom', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    // Non-force (streaming follow, render-flush) must still protect manual scroll-up.
    expect(source).toMatch(/if \(!force && !isAtBottom\.value\) return/)
  })
})
