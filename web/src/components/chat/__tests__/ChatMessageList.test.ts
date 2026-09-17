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
    // The bubble flash follows the canonical flash timing (--flash-duration,
    // kept in sync with domFlash.ts LINE_FLASH_MS).
    expect(source).toContain('animation: msg-highlight-flash var(--flash-duration, 0.7s) ease-out 1')
    // The background only animates — the keyframes block contains no color:
    // property (only background-color), so text color never changes.
    const kfStart = source.indexOf('@keyframes msg-highlight-flash')
    const kfEnd = source.indexOf('}', source.indexOf('color-mix(in srgb, var(--accent-color) 35%, var(--msg-base-bg))'))
    const keyframes = source.slice(kfStart, kfEnd > -1 ? kfEnd + 1 : undefined)
    expect(keyframes).toContain('background-color')
    // `background-color:` contains the substring "color:", so match a standalone
    // `color:` property (declaration start) rather than a bare substring.
    expect(keyframes).not.toMatch(/(?:^|[;{])\s*color:/)
  })

  it('removes the highlight class through the shared domFlash helper', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    // The class removal is delegated to flashElement (domFlash) instead of an
    // inline setTimeout — cleanup now survives environments where the CSS
    // animation never fires (jsdom / reduced-motion).
    expect(source).toContain("flashElement(el, { className: 'chat-message-highlight' })")
    expect(source).toContain("import { flashElement } from '@/utils/domFlash'")
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
    // The rAF correction must not scroll while the user is scrolling. That is
    // now expressed by the single shouldPin decision point, which rejects a
    // held touch before anything else — so no separate isUserScrolling check is
    // needed (and having one would be a second, divergent guard).
    expect(source).toContain('shouldPin(buildScrollState(), force)')
    // …and must not follow once the user has scrolled away (non-force).
    expect(source).toMatch(/if \(shouldPin\(buildScrollState\(\), force\)\) \{\s*el2\.scrollTop = el2\.scrollHeight/)
  })

  it('scrollToBottom returns early when the user is scrolling (touch drag)', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    // The guard is the unified isUserScrolling check, not a raw userTouching flag.
    expect(source).toContain('if (isUserScrolling(buildScrollState()))')
    expect(source).not.toContain('if (userTouching && !force) return')
  })

  it('a force pin goes through the single shouldPin decision point', async () => {
    // force=true no longer has its own deferral branch. Every pin (force or
    // not) is decided by shouldPin, which blocks only on a held touch or on the
    // "user is away" latch for non-force pins. The old pendingFollow queue was
    // removed because it depended on a "scroll stopped" signal that a streaming
    // turn never emits, so queued pins were silently lost.
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    expect(source).toMatch(/function scrollToBottom\(force = false\) \{[\s\S]*?if \(shouldPin\(buildScrollState\(\), force\)\) \{\s*followToBottom\(force\)/)
    // No deferral queue anywhere
    expect(source).not.toContain('pendingFollow')
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

describe('ChatMessageList — rewind event pass-through', () => {
  it('re-emits rewind-from-message from ChatMessageItem and declares the emit', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    expect(source).toContain("@rewind-from-message=\"$emit('rewind-from-message', $event)\"")
    expect(source).toContain("'rewind-from-message'")
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
 * - Every pin (force or not) goes through the single shouldPin decision point.
 *   A held touch always blocks; the "user is away" latch blocks only non-force
 *   pins. Nothing is queued for later.
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
    expect(source).toContain('shouldPin(buildScrollState(), force)')
    // The latch is re-sampled under the user-input predicate
    expect(source).toContain('if (isUserScrolling(buildScrollState())) {')
  })

  it('a force pin is never queued — it runs or is rejected in the same tick', async () => {
    // The removed deferral was the bug: a queued force pin was flushed only by
    // onScrollStopped, and a streaming turn keeps emitting scroll events, so the
    // flush never ran. A send whose reply arrived a few seconds later (slow
    // network) therefore never got pinned.
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    expect(source).not.toContain('pendingFollow')
    expect(source).not.toMatch(/if \(force\)\s*pendingFollow/)
  })

  it('onScrollStopped no longer flushes a queued pin', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    // onScrollStopped still flushes the jump highlight and releases programmatic
    // ownership, and still does a distance-gated safety clear of the latch.
    expect(source).toContain('function onScrollStopped()')
    expect(source).toContain('flushMessageHighlight()')
    expect(source).toContain('setProgrammatic(false)')
    expect(source).toMatch(/if \(dist <= RESUME_FOLLOW_PX\) \{\s*isAtBottom\.value = true\s*userLeftBottom = false/)
    // …but it must not re-pin on behalf of a queued request.
    const fnStart = source.indexOf('function onScrollStopped()')
    const fnEnd = source.indexOf('\n}', fnStart)
    const fnBody = source.slice(fnStart, fnEnd)
    expect(fnBody).not.toContain('scrollToBottom(true)')
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

  it('programmatic scrolling is a single boolean flag (no owner channel)', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    // The scrollOwner channel was removed with the rest of the follow memory:
    // it existed only to feed the follow decision, which no longer reads it.
    expect(source).toContain('function setProgrammatic(val)')
    expect(source).not.toContain('scrollOwner')
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
 * Tests for the follow latch.
 *
 * The latch answers one question: "when the user last drove the scroll
 * surface, were they at the bottom?" It is sampled ONLY inside a user-input
 * window, from the distance to the bottom. Content-driven scroll events never
 * touch it.
 *
 * Root cause of the "stuck mid-conversation" bug this replaced: the old latch
 * was direction-driven (any 1px upward movement = "user scrolled up") and was
 * evaluated on EVERY scroll event. During streaming the browser nudges
 * scrollTop by a pixel (scroll anchoring / clamping), so the latch flipped on
 * while the user sat at the very bottom — after which every follow pin was
 * rejected and the content kept growing below the viewport.
 */
describe('ChatMessageList — stream-follow persistence', () => {
  it('samples the latch only while the user is driving the scroll surface', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    // The re-sample must be gated on the user-input predicate, so a
    // content-growth scroll event (no input flag set) leaves the latch alone.
    expect(source).toMatch(/if \(isUserScrolling\(buildScrollState\(\)\)\) \{\s*userLeftBottom = isUserAwayFromBottom\(distFromBottom\)/)
  })

  it('the latch is distance-only — no direction test anywhere', async () => {
    // A direction test cannot distinguish a deliberate drag from a 1px layout
    // nudge, and its unconditional "any upward pixel = left" branch is exactly
    // what let a layout nudge masquerade as a scroll away.
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    expect(source).toContain('userLeftBottom = isUserAwayFromBottom(distFromBottom)')
    expect(source).not.toContain('scrollingUp')
    expect(source).not.toContain('updateUserLeftBottom')
  })

  it('the follow decision no longer depends on scroll-stop detection', async () => {
    // The old force pin was queued as `pendingFollow` and flushed only by
    // onScrollStopped. A streaming turn never lets the scroll stream stop, so
    // the queued pin never ran — the "sent a message but the reply is never
    // followed" bug. The queue is gone; scroll-stop now only serves the jump
    // highlight and releasing programmatic ownership.
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    expect(source).not.toContain('pendingFollow')
    expect(source).not.toContain('deferred (user scrolling)')
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

  it('every user-input flag is bounded by its own end signal', async () => {
    // The root of the "clicked once, then the reply never followed" bug:
    // mouseDownActive had NO release path (mouseup was never listened), so a
    // single click left it set forever and every later content-growth scroll
    // was misread as a user gesture. Each flag must have a real end signal.
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    // touch → touchend/touchcancel (template), mouse → document mouseup,
    // wheel → decay timer.
    expect(source).toContain('function onDocumentMouseUp()')
    expect(source).toContain("document.addEventListener('mouseup', onDocumentMouseUp)")
    expect(source).toMatch(/function onWheelScroll\(\) \{\s*wheelActive = true[\s\S]*?wheelDecayTimer = setTimeout/)
    // …and no flag is refreshed by scroll events (handleScroll never assigns them)
    const handleScroll = source.slice(source.indexOf('function handleScroll()'), source.indexOf('// Touch tracking:'))
    expect(handleScroll).not.toMatch(/userTouching = true/)
    expect(handleScroll).not.toMatch(/wheelActive = true/)
    expect(handleScroll).not.toMatch(/mouseDownActive = true/)
  })

  it('streamed pin paths skip the write when already glued to the bottom (gap <= 0)', async () => {
    // followToBottom's rAF correction and the content-growth observer both
    // re-pin on every streamed frame; writing the same scrollTop emits an
    // unnecessary scroll event.
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
    expect(source).toContain('if (isUserScrolling(buildScrollState())) {')
    expect(source).not.toContain('if (!programmaticScrolling && isUserScrolling(buildScrollState())) {')
  })

  it('lastScrollTop is captured before any branch so programmatic pins cannot freeze it', async () => {
    // Regression: during streaming, every stream-pin scroll event takes the
    // `if (programmaticScrolling)` early-return path. The old code updated
    // lastScrollTop only AFTER that branch, so it froze at a stale pre-stream
    // value (typically 0) and the FAB direction detection never fired.
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    // The previous position must be captured BEFORE the programmatic branch.
    const capture = source.indexOf('const prevScrollTop = lastScrollTop')
    expect(capture).toBeGreaterThan(-1)
    expect(source.indexOf('lastScrollTop = el.scrollTop')).toBeGreaterThan(capture)
    expect(source).toContain('const scrollDelta = el.scrollTop - prevScrollTop')
  })

  it('a force pin (send message / answer card) clears the userLeftBottom latch', async () => {
    // Regression: sending a message while streaming force-pins the viewport to
    // the bottom, but a one-way latch (tripped by an earlier scroll while
    // reading context during a long tool call) was never cleared by the force
    // pin. The AI reply then streams BELOW the just-sent message and every
    // subsequent non-force pin is rejected — the view stays stuck at the user
    // bubble and the streamed reply is never followed.
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

  it('a real user scroll during a stream releases programmatic ownership so the FAB can appear', async () => {
    // Regression: during a running session followToBottom re-arms
    // setProgrammatic(true) on every pin frame. handleScroll's programmatic
    // branch returns early BEFORE the scrolledUp/scrolledDown logic, so a real
    // upward/downward drag while the stream is active never flipped either flag
    // — the scroll-jump FAB required a huge scroll (or waiting for the stream
    // to go quiet + another scroll) before it appeared.
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    // The user-input latch block must release programmatic ownership first so
    // the FAB logic on this same event is not skipped by the early return.
    expect(source).toMatch(/if \(isUserScrolling\(buildScrollState\(\)\)\) \{[\s\S]*?if \(programmaticScrolling\) setProgrammatic\(false\)/)
  })
})

/**
 * Regressions for the two user-reported symptoms. Both had the same root
 * cause: a follow latch that could latch ON without any user gesture and then
 * never release.
 *
 * 1. "Sometimes after sending, the page stops at some middle position instead
 *    of the bottom."
 * 2. "After sending, when the assistant reply appears a few seconds late
 *    (slow network), it is not followed."
 */
describe('ChatMessageList — follow latch cannot latch on without a gesture', () => {
  it('a click (mousedown without scroll) does not leave a permanent user-scroll marker', async () => {
    // Root cause of both symptoms: mouseDownActive was set by ANY mousedown and
    // had no release path (mouseup was never listened). A single click in the
    // message area therefore left it set forever, so every later
    // content-growth scroll event was misread as a user gesture and latched the
    // follow latch off while the user sat at the bottom.
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    // mouseup is listened on the document (the button is often released outside
    // the list) and clears the flag.
    expect(source).toContain("document.addEventListener('mouseup', onDocumentMouseUp)")
    expect(source).toMatch(/function onDocumentMouseUp\(\) \{\s*mouseDownActive = false\s*\}/)
    // and the listener is removed on unmount (no leak across sessions)
    expect(source).toContain("document.removeEventListener('mouseup', onDocumentMouseUp)")
  })

  it('a content-driven scroll event cannot latch follow off', async () => {
    // A slow reply means the assistant message appears seconds after the send.
    // In that window the list keeps growing, and the browser nudges scrollTop.
    // Those scroll events arrive with NO input flag set, so the latch must not
    // be re-sampled — otherwise the delayed reply lands while follow is off.
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    // The only assignment to the latch in handleScroll is inside the
    // user-input-gated block.
    const handleScroll = source.slice(source.indexOf('function handleScroll()'), source.indexOf('// Touch tracking:'))
    const latchWrites = handleScroll.match(/userLeftBottom\s*=/g) || []
    expect(latchWrites.length).toBe(1)
    const gateIdx = handleScroll.indexOf('if (isUserScrolling(buildScrollState())) {')
    const writeIdx = handleScroll.indexOf('userLeftBottom = isUserAwayFromBottom(distFromBottom)')
    expect(gateIdx).toBeGreaterThan(-1)
    expect(writeIdx).toBeGreaterThan(gateIdx)
  })

  it('a delayed assistant reply still gets pinned (no queued pin to lose)', async () => {
    // With the queue removed, a force pin runs (or is rejected) in the same
    // tick — there is no "wait for scroll to stop" signal that a streaming turn
    // never emits. The late reply's own content growth is then caught by the
    // content-growth observer.
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    expect(source).not.toContain('pendingFollow')
    // The observer re-pins on growth whenever the user has not scrolled away.
    expect(source).toContain('if (!shouldPin(buildScrollState(), false)) return')
    expect(source).toContain('el.scrollTop = el.scrollHeight')
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
    // Every user-input flag is cleared too, so the rebuilt list starts clean.
    expect(source).toMatch(/userTouching = false\s*wheelActive = false\s*mouseDownActive = false/)
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
    expect(source).toContain('border-radius: var(--radius-full)')
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
    expect(source).toContain('import { useCodeLinkPreview, handleVerifiedFilePathClick } from')
  })

  it('instantiates useCodeLinkPreview with containerRef bound to messagesRef', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    expect(source).toContain("const codeLinkPreview = useCodeLinkPreview({ containerRef: messagesRef, source: 'chat' })")
  })

  it('renders CodeLinkPreview conditioned on codeLinkPreview.enabled.value', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    expect(source).toContain('<CodeLinkPreview')
    expect(source).toContain('v-if="codeLinkPreview.enabled.value"')
    expect(source).toContain(':preview="codeLinkPreview"')
  })

  it('delegates verified file-path clicks to the shared interceptor in handleChatClick', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    expect(source).toContain('if (handleVerifiedFilePathClick(event, codeLinkPreview)) return')
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

  it('handles clicking both file-open button and directory chat-file-path text', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    const openSection = source.slice(source.indexOf("closest('.chat-file-open-btn')"))
    expect(openSection).toContain("closest('.chat-file-path[data-path-type=\"dir\"]')")
    expect(openSection.slice(0, 500)).toContain('openFilePath(filePath')
  })

  it('exposes closeCodePreview in defineExpose', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    const exposeSection = source.slice(source.indexOf('defineExpose({'))
    expect(exposeSection).toContain('closeCodePreview: () => codeLinkPreview.close()')
  })
})

describe('ChatMessageList — scroll FAB is fully opaque', () => {
  it('does not dim the floating scroll buttons with --opacity-muted', async () => {
    // Regression: the FAB used `opacity: var(--opacity-muted)` (0.5), which made
    // the icons hard to read against busy chat content. The buttons must render
    // fully opaque at rest; only the enter/leave transitions animate opacity.
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    const block = source.slice(
      source.indexOf('.scroll-fab-round {'),
      source.indexOf('.scroll-fab-enter-active'),
    )
    expect(block).toContain('opacity: 1')
    expect(block).not.toContain('--opacity-muted')
    // The opacity transition was only there for the muted resting state.
    expect(block).not.toContain('opacity var(--duration-base)')
  })
})

// ── send-message forwards the ask-card key ──
//
// An AskUserQuestion answer carries the card's key so a FAILED send can revert
// the submitted flag and leave the card answerable (see revertAskSubmission).
// The key travels through every layer between the card and ChatPanelContent —
// dropping it at any hop silently strands a failed answer with no way to retry.
describe('ChatMessageList — ask-card key forwarding', () => {
  async function source(component: string): Promise<string> {
    const mod = await import(/* @vite-ignore */ `@/components/chat/${component}?raw`)
    return typeof mod.default === 'string' ? mod.default : ''
  }

  it('forwards both the text and the card key from the inner list', async () => {
    const src = await source('ChatMessageList.vue')
    // The old single-argument form dropped the key.
    expect(src).not.toMatch(/@send-message="\$emit\('send-message', \$event\)"/)
    expect(src).toMatch(/@send-message="\(text, cardKey\) => \$emit\('send-message', text, cardKey\)"/)
  })

  it('forwards both the text and the card key from the message item', async () => {
    const src = await source('ChatMessageItem.vue')
    expect(src).not.toMatch(/@send-message="\$emit\('send-message', \$event\)"/)
    expect(src).toMatch(/@send-message="\(text, cardKey\) => \$emit\('send-message', text, cardKey\)"/)
  })

  it('forwards both the text and the card key from ContentBlocks', async () => {
    const src = await source('ContentBlocks.vue')
    expect(src).toMatch(/@send-message="\(text, cardKey\) => \$emit\('send-message', text, cardKey\)"/)
  })
})
