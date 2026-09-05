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

describe('ChatMessageList — session switching indicator (replaces full-area overlay)', () => {
  it('renders an in-list LoadingIndicator while switching and messages are empty', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    // The spinner is gated on switching + empty message list — no full-area mask.
    expect(source).toContain('v-if="props.switching && messages.length === 0"')
    expect(source).toContain('class="chat-switching-indicator"')
  })

  it('defines the switching prop and forwards it from the panel', async () => {
    const listSource = await import('@/components/chat/ChatMessageList.vue?raw')
    expect(String(listSource.default)).toContain('switching: { type: Boolean, default: false }')

    const panelSource = await import('@/components/chat/ChatPanelContent.vue?raw')
    expect(String(panelSource.default)).toContain(':switching="session.switching.value"')
    // The old full-area overlay mask must be gone.
    expect(String(panelSource.default)).not.toContain('Session switching overlay')
  })
})

describe('ChatMessageList — message jump flash is accent background (not border)', () => {
  it('animates the bubble background with theme accent, leaving text color untouched', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    // No inset box-shadow border highlight.
    expect(source).not.toContain('box-shadow: inset 0 0 0 2px var(--accent-color)')
    expect(source).not.toContain('msg-highlight-flash 1.5s')
    // Flash is role-specific and blends the accent over each bubble's own
    // theme background (user white text and assistant text stay unchanged).
    expect(source).toContain('--msg-base-bg: var(--user-msg-color)')
    expect(source).toContain('--msg-base-bg: var(--bg-tertiary)')
    expect(source).toContain('color-mix(in srgb, var(--accent-color) 65%, var(--msg-base-bg))')
    expect(source).toContain('animation: msg-highlight-flash 1.2s ease-out 1')
    // The background only animates — the keyframes block contains no color:
    // property (only background-color), so text color never changes.
    const kfStart = source.indexOf('@keyframes msg-highlight-flash')
    const kfEnd = source.indexOf('}', source.indexOf('color-mix(in srgb, var(--accent-color) 25%, var(--msg-base-bg))'))
    const keyframes = source.slice(kfStart, kfEnd > -1 ? kfEnd + 1 : undefined)
    expect(keyframes).toContain('background-color')
    // `background-color:` contains the substring "color:", so match a standalone
    // `color:` property (declaration start) rather than a bare substring.
    expect(keyframes).not.toMatch(/(?:^|[;{])\s*color:/)
  })

  it('keeps removing the highlight class after the animation window', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    expect(source).toContain("setTimeout(() => el.classList.remove('chat-message-highlight'), 1500)")
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
    expect(source).toContain('if (isUserScrolling(buildScrollState())) return')
    // …and must not follow once the user has scrolled away (non-force).
    expect(source).toContain('shouldPin(buildScrollState(), force)')
  })

  it('scrollToBottom returns early when the user is scrolling (touch drag)', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    // The guard is the unified isUserScrolling check, not a raw userTouching flag.
    expect(source).toContain('if (isUserScrolling(buildScrollState()))')
    expect(source).not.toContain('if (userTouching && !force) return')
  })

  it('scrollToBottom with force=true defers the pin while the user is scrolling', async () => {
    // New semantic: force=true no longer overrides an active user scroll.
    // The pin is deferred (pendingFollow) and flushed only after the scroll
    // stops — never while the user's finger is on the screen.
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    // scrollToBottom must consult isUserScrolling before pinning
    expect(source).toContain('isUserScrolling(buildScrollState())')
    // A force pin during a user scroll is deferred, not applied
    expect(source).toMatch(/if \(isUserScrolling\(buildScrollState\(\)\)\) \{\s*if \(force\) pendingFollow = true/)
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
  it('scrollToBottom consults the scroll-state guards', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    // Guards imported from the pure module, fed by the shared state builder
    expect(source).toContain('function buildScrollState()')
    expect(source).toContain('if (isUserScrolling(buildScrollState()))')
    expect(source).toContain('shouldPin(buildScrollState(), force)')  })
  it('force pin is deferred (pendingFollow) while the user is scrolling, not applied', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    // A force pin during a user scroll is deferred to pendingFollow and never
    // applied while the user is still scrolling (the appLog diagnostic line
    // sits between the two statements).
    expect(source).toMatch(/if \(isUserScrolling\(buildScrollState\(\)\)\) \{\s*if \(force\) pendingFollow = true[\s\S]*?return\s*\}/)
  })

  it('onScrollStopped clears pendingFollow unconditionally and always flushes a deferred force pin', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    // onScrollStopped resets ownership and clears the deferred flag no matter what
    expect(source).toContain('function onScrollStopped()')
    // pendingFollow is always cleared here — stale pins never fire later.
    // A deferred force pin is ALWAYS flushed: pendingFollow is only ever set by
    // explicit user-intent pins (sending a message, answering a question card,
    // switching sessions), and the user took an action expecting to see the
    // bottom. The old RESUME_FOLLOW_PX gate dropped the pin when the user had
    // scrolled far up (e.g. answered a card while reading earlier context), so
    // their answer appeared out of view — exactly the "AskUserQuestion answer
    // doesn't scroll to bottom" bug.
    expect(source).toMatch(/if \(pendingFollow\) \{\s*pendingFollow = false[\s\S]*?scrollToBottom\(true\)/)
    expect(source).not.toMatch(/if \(dist <= RESUME_FOLLOW_PX\) \{\s*scrollToBottom\(true\)/)
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

/**
 * Tests for the DOM reconciliation key fix (listKey).
 *
 * Root cause: when a transient message's id changes from string (pending-xxx)
 * to numeric (DB id) — e.g. after loadHistory or queue_drain — the v-for key
 * changes but Vue's patch may leave a stale DOM node behind in certain WebView
 * /GPU compositor states. This produces the "duplicate message" visual artifact
 * that survives refresh (because the data layer is clean) and only clears on
 * app restart (because restart recreates the DOM from scratch).
 *
 * Fix: the .chat-messages-list container now uses a structural key
 * (listKey) that changes whenever the message array is replaced or reshuffled
 * by rebuildFromDb, forcing Vue to unmount and remount the entire list.
 */
describe('ChatMessageList — DOM reconciliation key (listKey)', () => {
  it('uses a structural listKey instead of bare session id', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    // The container key must reference listKey, not the raw session id
    expect(source).toContain(':key="listKey"')
    expect(source).not.toContain(":key=\"currentSessionId || 'no-session'\"")
  })

  it('listKey includes session id, message count, and first/last message id', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    // listKey must be a computed that concatenates these segments
    expect(source).toContain('const listKey = computed')
    expect(source).toContain('props.currentSessionId')
    expect(source).toContain('msgs.length')
    expect(source).toContain('msgs[0]?.id')
    expect(source).toContain('msgs[msgs.length - 1]?.id')
  })
})

/**
 * Tests for the stream-follow persistence fix.
 *
 * Root cause: a single throttled render flush (ContentBlocks.vue, 300ms) can
 * grow scrollHeight by a large amount in one frame when a burst of tokens
 * arrives at once. A distance-based follow check then rejects the follow
 * (gap too big) and the viewport is never pulled down again — every later
 * flush re-reads an even larger gap, so follow is lost permanently.
 *
 * Fix: follow is decided ONLY by "did the user scroll away?" — no distance or
 * grace-band heuristic.
 * - As long as the user has not deliberately left the bottom, content growth
 *   (streaming, render flush, lazy load) always re-pins to the bottom.
 * - The moment the user scrolls away from the bottom (past NEAR_BOTTOM_PX),
 *   userLeftBottom latches on and ALL follow is suppressed — a user reading
 *   older content is never yanked back, regardless of how much arrives.
 * - userLeftBottom clears when the user scrolls back near the bottom, switches
 *   session, or taps the bottom FAB.
 */
describe('ChatMessageList — stream-follow persistence', () => {
  it('scrollToBottom consults the geometry + userLeftBottom state', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    // The follow decision feeds the latched "user left" flag
    expect(source).toContain('userLeftBottom,')
  })

  it('a user who scrolls away from the bottom is never yanked back (userLeftBottom)', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    // Leaving the bottom is DIRECTION-driven, not distance-driven: any upward
    // drag latches the "left" flag immediately — a user who stops mid-drag
    // inside the near-bottom band must stay locked, or the next streamed pin
    // yanks them (the snap-back jitter bug).
    // Direction uses prevScrollTop — captured before any branch so programmatic
    // stream pins (which return early) cannot freeze lastScrollTop at a stale
    // pre-stream value and poison upward-drag detection.
    expect(source).toContain('scrollingUp: el.scrollTop < prevScrollTop')
    expect(source).toContain('updateUserLeftBottom(userLeftBottom, {')
    // Returning to the bottom (within RESUME_FOLLOW_PX) clears it
    expect(source).toContain('updateUserLeftBottom')
  })

  it('session switch resets the follow latch', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    expect(source).toContain('userLeftBottom = false')
  })

  it('the bottom FAB (scrollToBottomSmooth) clears the follow latch', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    // A user who scrolled up earlier and then taps the bottom FAB has
    // explicitly asked to return to the bottom — the "left the bottom" latch
    // must clear so streaming follow resumes. Without it the next streamed
    // content that briefly pushes the gap past the edge is rejected and the
    // list appears to stop auto-scrolling despite the user being at the bottom.
    expect(source).toContain('function scrollToBottomSmooth()')
    expect(source).toContain('userLeftBottom = false')
    // The clearing must live INSIDE scrollToBottomSmooth (not merely anywhere)
    expect(source).toMatch(/scrollToBottomSmooth\(\)[\s\S]*?userLeftBottom = false/)
  })

  it('an upward drag inside the near-bottom band latches userLeftBottom immediately', async () => {
    // Root cause of "很难拖上去、抽搐" (snap-back jitter): the old latch only
    // fired past NEAR_BOTTOM_PX, so a user who stopped mid-drag inside the
    // band stayed "at the bottom" and the next streamed pin yanked them back.
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    // The latch decision delegates to the direction-driven pure function
    expect(source).toContain('userLeftBottom = updateUserLeftBottom(')
    expect(source).toContain('scrollingUp: el.scrollTop < prevScrollTop')
    // The old distance-only latch must be gone
    expect(source).not.toContain('if (distFromBottom > NEAR_BOTTOM_PX) {')
  })

  it('streamed pin paths skip the write when already glued to the bottom (gap <= 0)', async () => {
    // followToBottom's rAF correction and the content-growth observer both
    // re-pin on every streamed frame; writing the same scrollTop emits an
    // unnecessary scroll event that restarts the 250ms user-scroll window.
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    // rAF correction: no write when the gap is already <= 0
    expect(source).toMatch(/const gap = el2\.scrollHeight - el2\.scrollTop - el2\.clientHeight[\s\S]*?if \(gap <= 0\) return/)
    // Content-growth observer: same skip
    expect(source).toContain('if (el.scrollHeight - el.scrollTop - el.clientHeight <= 0) return')
  })

  it('the latch block is NOT gated on !programmaticScrolling', async () => {
    // Regression fix: during a stream, followToBottom re-arms setProgrammatic(true)
    // every frame, so programmaticScrolling stays true for the whole stream.
    // Gating the user-scroll latch on `!programmaticScrolling` blocked it entirely —
    // the user could scroll far away and still get yanked back to the bottom.
    // User drags are distinguished by the input flags alone (which programmatic
    // pins never set).
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    expect(source).toContain('if (userTouching || wheelActive || mouseDownActive) {')
    expect(source).not.toContain('if (!programmaticScrolling && (userTouching || wheelActive || mouseDownActive)) {')
  })

  it('lastScrollTop is captured before any branch so programmatic pins cannot freeze it', async () => {
    // Regression: during streaming, every stream-pin scroll event takes the
    // `if (programmaticScrolling)` early-return path. The old code updated
    // lastScrollTop only AFTER that branch, so it froze at a stale pre-stream
    // value (typically 0). Every subsequent upward drag then read
    // `el.scrollTop < lastScrollTop` as false → the userLeftBottom latch never
    // fired → streamed pins yanked the user back to the bottom no matter how
    // far they dragged up ("无论如何向上拖拽都会被拽回到底部" bug; refresh fixed
    // it only by resetting the frozen value).
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    // The previous position must be captured BEFORE the programmatic branch.
    const capture = source.indexOf('const prevScrollTop = lastScrollTop')
    expect(capture).toBeGreaterThan(-1)
    expect(source.indexOf('lastScrollTop = el.scrollTop')).toBeGreaterThan(capture)
    // Both the user-scroll latch and the FAB direction logic consume the
    // pre-branch capture, not the (possibly frozen) module-level variable.
    expect(source).toContain('scrollingUp: el.scrollTop < prevScrollTop')
    expect(source).toContain('const scrollDelta = el.scrollTop - prevScrollTop')
  })

  it('a force pin (send message / answer card) clears the userLeftBottom latch', async () => {
    // Regression: sending a message while streaming force-pins the viewport to
    // the bottom, but the one-way userLeftBottom latch (tripped by an earlier
    // upward scroll while reading context during a long tool call) was never
    // cleared by the force pin. The AI reply then streams BELOW the just-sent
    // message and every subsequent non-force pin is rejected — the view stays
    // stuck at the user bubble and the streamed reply is never followed.
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    // The latch clear must live INSIDE followToBottom, gated on force.
    expect(source).toMatch(/function followToBottom\(force\) \{\s*setProgrammatic\(true\)[\s\S]*?if \(force\) userLeftBottom = false/)
  })

  it('a non-force stream pin does NOT clear the userLeftBottom latch', async () => {
    // The "user reading history is never yanked back" guarantee must survive:
    // only explicit force pins (user action expecting the bottom) clear the
    // latch — ordinary streamed content growth must not.
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    // followToBottom must only clear under `if (force)` — no unconditional clear.
    const fnStart = source.indexOf('function followToBottom(')
    expect(fnStart).toBeGreaterThan(-1)
    const fnEnd = source.indexOf('\n}', fnStart)
    const fnBody = source.slice(fnStart, fnEnd)
    expect(fnBody).toMatch(/if \(force\) userLeftBottom = false/)
    expect(fnBody).not.toMatch(/userLeftBottom = false[\s\S]*?if \(force\)/)
  })
})

/**
 * Load-more must also fire when the TOP FAB programmatically scrolls to the
 * top — the programmatic branch of handleScroll used to `return` before the
 * load-more check, so only a subsequent manual scroll triggered history load.
 */
describe('ChatMessageList — programmatic scroll-to-top triggers load-more', () => {
  it('load-more check runs before the programmatic return', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    expect(source).toContain('function scrollToTop()')
    // The programmatic-scroll branch must run the load-more check BEFORE its
    // return, not after (the manual-scroll path).
    expect(source).toMatch(/if \(programmaticScrolling\) \{[\s\S]*?emit\('load-more'\)[\s\S]*?return\n  \}/)
    expect(source).toContain("emit('load-more')")
  })
})

/**
 * Content-growth observer: async rendering (Mermaid deferred, throttled flush,
 * lazy original text) can grow the list height AFTER the initial pin, with no
 * dedicated scroll call. ResizeObserver is the universal backstop that re-pins
 * whenever content grows while the user has NOT scrolled away.
 */
describe('ChatMessageList — content-growth observer backstop', () => {
  it('observes the content wrapper and re-pins on growth unless the user left', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    // Observe the .chat-messages-list wrapper (its box = content height)
    expect(source).toContain('new ResizeObserver(() => onContentGrown())')
    expect(source).toContain('contentResizeObserver.observe(inner)')
    // Re-pin guard: unified pin decision — never pull back a user who scrolled
    // away, never fight an active scroll.
    expect(source).toContain('function onContentGrown()')
    expect(source).toContain('if (!shouldPin(buildScrollState(), false)) return')
    // Re-observe when listKey rebuilds the DOM (session switch / load-more)
    expect(source).toContain('watch(listKey')
    expect(source).toContain('observeContentGrowth()')
  })
})

/**
 * Session switches always land at the bottom — no per-session scroll position
 * memory (chatScrollMemory was removed). The currentSessionId watcher only
 * resets the scroll state machine for the freshly rebuilt list; the actual
 * force-scroll-to-bottom is driven by switchSession's loadHistory(true).
 *
 * The messages watcher keeps ONLY the array-replacement anchor (captureAnchor /
 * restoreAnchor) for when content is prepended/loaded while the user is NOT at
 * the bottom — a same-session, mid-reading reload must not jump the view.
 */
describe('ChatMessageList — session switch resets scroll state, no position memory', () => {
  it('resets the full scroll state machine on session switch', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    // State machine reset on currentSessionId change
    expect(source).toContain('watch(() => props.currentSessionId')
    expect(source).toContain('userLeftBottom = false')
    expect(source).toContain('pendingFollow = false')
    // No position memory left behind
    expect(source).not.toContain('saveChatScrollPosition')
    expect(source).not.toContain('clearChatScrollPosition')
    expect(source).not.toContain('getChatScrollPosition')
    expect(source).not.toContain('pendingRestoreSessionId')
    expect(source).not.toContain('savePositionNow')
  })

  it('keeps the array-replacement anchor for mid-reading reloads', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    // The messages watcher still anchors the first visible message when the
    // array is replaced (loadHistory / prepend) while not at the bottom.
    expect(source).toContain('function captureAnchor(el)')
    expect(source).toContain('function restoreAnchor(el, anchor)')
    expect(source).toContain('scrollAnchor = captureAnchor(el)')
  })
})

/**
 * Lazy-load hint floating overlay.
 *
 * The "还有 N 条更早消息 / 加载中 / 已加载全部" pill must float above the top of
 * the message area, not live inside the scrolling message flow. It was moved
 * out of .chat-messages (the scroll container) into .chat-messages-wrapper and
 * positioned absolutely, so it:
 *   - never scrolls with the message flow,
 *   - takes no layout space (does not push messages down),
 *   - renders with a backdrop background so it reads as a floating pill.
 */
describe('ChatMessageList — floating lazy-load hint overlay', () => {
  it('chat-load-area lives outside the scroll container (absolute overlay)', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    // .chat-load-area must be a sibling of .chat-messages, not its child.
    expect(source).toContain('class="chat-messages-wrapper">')
    expect(source).toContain('class="chat-load-area"')
    // The scroll container must open after the load area closes.
    const loadAreaIdx = source.indexOf('class="chat-load-area"')
    const messagesIdx = source.indexOf('class="chat-messages"')
    expect(loadAreaIdx).toBeGreaterThan(-1)
    expect(messagesIdx).toBeGreaterThan(loadAreaIdx)
    // The load area must be absolutely positioned (no layout footprint).
    expect(source).toMatch(/\.chat-load-area \{[^}]*position: absolute/s)
  })

  it('the pill states carry a backdrop background so they read as floating', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    expect(source).toMatch(/\.chat-load-more,\s*\.chat-load-hint,\s*\.chat-load-done \{/)
    expect(source).toContain('border-radius: 999px')
    // backdrop background: the pill is not transparent text in the flow anymore
    expect(source).toContain('background: color-mix')
  })
})

/**
 * Transient "more older messages" hint.
 *
 * The "还有 N 条更早消息" pill must NOT be a persistent resident of the message
 * area. Whenever older messages remain it briefly appears (including on first
 * render of a session that still has history to load) then auto-hides after a
 * timeout. Once all history is loaded it hides immediately so the "all loaded"
 * hint can take over.
 */
describe('ChatMessageList — transient more-messages hint', () => {
  it('the more-messages hint is gated by a showMoreHint state, not hasMore alone', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    // The hint branch must be driven by the transient showMoreHint flag —
    // hasMore must no longer be the standalone gate that keeps it resident.
    expect(source).toMatch(/v-else-if="showMoreHint"/)
    expect(source).not.toMatch(/v-else-if="hasMore && remainingCount > 0"/)
  })

  it('showMoreHint is armed whenever older messages remain and auto-hides on a timer', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    // Armed from a watch over (hasMore && remainingCount > 0), so it announces
    // remaining history on first render too — not just after an explicit load.
    expect(source).toMatch(/watch\(\(\) => props\.hasMore && remainingCount\.value > 0/)
    expect(source).toContain("{ immediate: true }")
    // Auto-hide via a timeout (2.5s); re-arming clears the in-flight timer.
    expect(source).toContain('moreHintTimer = setTimeout')
    expect(source).toMatch(/clearTimeout\(moreHintTimer\)/)
    expect(source).toMatch(/showMoreHint\.value = false/)
  })

  it('hides immediately when all history is loaded (lets the all-loaded hint show)', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    // The watch else-branch hides the hint once remaining count drops to zero.
    expect(source).toMatch(/if \(hasRemaining\) \{[\s\S]*?showMoreHint\.value = true/)
    expect(source).toMatch(/else \{[\s\S]*?showMoreHint\.value = false/)
  })
})

describe('ChatMessageList — CodeLinkPreview integration', () => {
  it('imports CodeLinkPreview and useCodeLinkPreview', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    expect(source).toContain("import CodeLinkPreview from '@/components/file/CodeLinkPreview.vue'")
    expect(source).toContain("import { useCodeLinkPreview } from '@/composables/useCodeLinkPreview.ts'")
  })

  it('instantiates useCodeLinkPreview with containerRef bound to messagesRef', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    expect(source).toContain('const codeLinkPreview = useCodeLinkPreview({ containerRef: messagesRef })')
  })

  it('renders CodeLinkPreview conditioned on codeLinkPreview.enabled.value', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    expect(source).toContain('<CodeLinkPreview')
    expect(source).toContain('v-if="codeLinkPreview.enabled.value"')
    expect(source).toContain(':preview="codeLinkPreview"')
  })

  it('handles modifier click or touch tap for in-place code preview in handleChatClick', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    expect(source).toContain('if (codeLinkPreview.enabled.value)')
    expect(source).toContain('codeLinkPreview.handleClick(event)')
  })

  it('only intercepts verified file paths so dirs/unverified fall through to original handlers', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    // Guard must exist so directories and not-yet-verified paths are NOT
    // swallowed by the preview interceptor (they keep navigating as before).
    expect(source).toContain("const isVerifiedFile = linkOrBtn?.getAttribute('data-path-type') === 'file'")
    expect(source).toContain('isVerifiedFile && ((isModifier && linkOrBtn) || (!isTouch && pathEl))')
    expect(source).toContain('isVerifiedFile && isTouch && pathEl')
  })

  it('closes preview when clicking file-open button or double clicking', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    // File-open button handler
    const btnSection = source.slice(source.indexOf("closest('.chat-file-open-btn')"))
    expect(btnSection.slice(0, 300)).toContain('codeLinkPreview.close()')

    // Double click handler
    const dblSection = source.slice(source.indexOf('handleDblClick(event'))
    expect(dblSection.slice(0, 200)).toContain('codeLinkPreview.close()')
  })

  it('closes preview on session switch', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    const sessionWatch = source.slice(source.indexOf('watch(() => props.currentSessionId'))
    expect(sessionWatch.slice(0, 1000)).toContain('codeLinkPreview.close()')
  })

  it('exposes closeCodePreview in defineExpose', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    const exposeSection = source.slice(source.indexOf('defineExpose({'))
    expect(exposeSection).toContain('closeCodePreview: () => codeLinkPreview.close()')
  })
})

