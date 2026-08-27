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
 * Fix (evolved): all scroll decisions now go through the pure scroll-state
 * guards (isUserScrolling / shouldFollowStream). Force pins never override an
 * active user scroll — they are deferred until the scroll stops.
 */
describe('ChatMessageList — scroll sticky抖动 fix', () => {
  it('rAF correction is guarded against an active user scroll', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    // The rAF correction must not scroll while the user is scrolling.
    expect(source).toContain('if (isUserScrolling(state2)) return')
    // …and must not follow once the user has scrolled away (non-force).
    expect(source).toContain('shouldFollowStream(state2, force)')
  })

  it('scrollToBottom returns early when the user is scrolling (touch drag)', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    // The guard is the unified isUserScrolling check, not a raw userTouching flag.
    expect(source).toContain('if (isUserScrolling(state()))')
    expect(source).not.toContain('if (userTouching && !force) return')
  })

  it('scrollToBottom with force=true defers the pin while the user is scrolling', async () => {
    // New semantic: force=true no longer overrides an active user scroll.
    // The pin is deferred (pendingFollow) and flushed only after the scroll
    // stops — never while the user's finger is on the screen.
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    // scrollToBottom must consult isUserScrolling before pinning
    expect(source).toContain('isUserScrolling(state())')
    // A force pin during a user scroll is deferred, not applied
    expect(source).toMatch(/if \(isUserScrolling\(state\(\)\)\) \{\s*if \(force\) pendingFollow = true/)
    // The old "force overrides userTouching" check must be gone
    expect(source).not.toContain('if (userTouching && !force) return')
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
 * Tests for the unified scroll-state refactor.
 *
 * Old behavior: force=true pins unconditionally (rAF + setTimeout(300)
 * corrections had no user-scrolling guard) — on touch devices a force pin
 * during a fling yanked the view back to the bottom ("弹回" snap-back).
 *
 * New behavior:
 * - force=true means "content grew, pin to bottom", but NEVER overrides an
 *   active user scroll — the pin is deferred (pendingFollow) and flushed by
 *   onScrollStopped only if the user is still near the bottom.
 * - All decisions read live container geometry instead of the cached
 *   isAtBottom ref.
 * - The unconditional setTimeout(300) force pin is removed.
 * - Array replacement (loadHistory) anchors the viewport to the first visible
 *   message when the user is not at the bottom.
 */
describe('ChatMessageList — force pin is guarded by user scrolling', () => {
  it('scrollToBottom computes distance live and consults the scroll-state guards', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    // Live geometry, not the cached isAtBottom ref
    expect(source).toContain('const dist = el.scrollHeight - el.scrollTop - el.clientHeight')
    // Guards imported from the pure module
    expect(source).toContain('isUserScrolling(state())')
    expect(source).toContain('shouldFollowStream(state(), force)')
  })

  it('force pin is deferred (pendingFollow) while the user is scrolling, not applied', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    expect(source).toMatch(/if \(isUserScrolling\(state\(\)\)\) \{\s*if \(force\) pendingFollow = true\s*return\s*\}/)
  })

  it('onScrollStopped clears pendingFollow unconditionally and flushes only near the bottom', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    // onScrollStopped resets ownership and clears the deferred flag no matter what
    expect(source).toContain('function onScrollStopped()')
    // pendingFollow is always cleared here — stale pins never fire later
    expect(source).toMatch(/if \(pendingFollow\) \{\s*pendingFollow = false\s*if \(dist <= NEAR_EDGE_THRESHOLD\) \{\s*scrollToBottom\(true\)/)
    expect(source).toContain('setProgrammatic(false)')
  })

  it('the unconditional force setTimeout(300) pin is removed', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    // No 300ms force pin timer anywhere (the old `}, 300)` was too loose)
    expect(source).not.toMatch(/setTimeout\([^)]*300\)/)
  })

  it('scroll-stop detection replaces the fixed 150ms touchend window', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    expect(source).toContain('setTimeout(onScrollStopped, SCROLL_STOP_MS)')
    expect(source).not.toContain('setTimeout(() => { userTouching = false }, 150)')
  })

  it('message array replacement anchors the viewport when not at the bottom', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    expect(source).toContain('captureAnchor(el)')
    expect(source).toContain('restoreAnchor(messagesRef.value, scrollAnchor)')
  })

  it('programmatic scrolling maps to the programmatic owner', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    expect(source).toContain("scrollOwner.value = val ? 'programmatic' : 'idle'")
  })
})
