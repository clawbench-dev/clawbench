/**
 * Turn a vertical mouse wheel into horizontal scrolling on an overflow-x
 * container.
 *
 * WHY THIS EXISTS
 *
 * A horizontal strip (the chat input's attachment/quote cards) hides its
 * scrollbar on purpose — a visible one inside a compact input row is noise. But
 * with the bar hidden, a plain mouse wheel over the strip scrolls the PAGE, not
 * the strip: the browser only maps wheel to the nearest vertically-scrollable
 * ancestor. On a trackpad a horizontal two-finger swipe works (it produces
 * deltaX), which is why this only bites PC users with a plain wheel.
 *
 * So: while the strip can still scroll in the wheel's direction, consume the
 * event and apply the delta to scrollLeft. When it is already at the end, do
 * NOT consume — the page keeps scrolling normally, which is what the user
 * expects when the strip has nothing left to give.
 *
 * The decision is a pure function (`resolveHorizontalWheel`) so the boundary
 * behaviour is unit-testable without a DOM.
 */

/**
 * Normalise a wheel event's delta to pixels.
 *
 * `deltaY` is used when the gesture is vertical (a plain mouse wheel), `deltaX`
 * when it is already horizontal (trackpad swipe) — preferring the axis the user
 * actually moved, so a diagonal trackpad gesture is not fought.
 */
export function wheelDeltaPx(e: Pick<WheelEvent, 'deltaX' | 'deltaY' | 'deltaMode'>): number {
  const raw = e.deltaX !== 0 ? e.deltaX : e.deltaY
  // deltaMode 1 = lines, 2 = pages. Firefox reports lines for a plain wheel, so
  // without this the strip would scroll a few px per notch and feel broken.
  if (e.deltaMode === 1) return raw * 40
  if (e.deltaMode === 2) return raw * 800
  return raw
}

export interface HorizontalWheelDecision {
  /** Whether the handler should preventDefault and apply `nextScrollLeft`. */
  consume: boolean
  /** The scrollLeft to apply (clamped into range). Meaningless when !consume. */
  nextScrollLeft: number
}

/**
 * Decide whether a wheel event should be turned into a horizontal scroll.
 *
 * `consume` is false when:
 *   - the container cannot scroll at all (nothing to do — let the page have it);
 *   - the delta is 0;
 *   - the container is already at the boundary the wheel is pushing toward
 *     (scrolling up at scrollLeft 0, or down at the end). Consuming here would
 *     trap the page: the user scrolls, nothing moves, and the page never
 *     scrolls either.
 */
export function resolveHorizontalWheel(
  scrollLeft: number,
  scrollWidth: number,
  clientWidth: number,
  deltaPx: number,
): HorizontalWheelDecision {
  const maxScrollLeft = scrollWidth - clientWidth
  if (maxScrollLeft <= 0 || deltaPx === 0) {
    return { consume: false, nextScrollLeft: scrollLeft }
  }

  const target = scrollLeft + deltaPx
  // Sub-pixel rounding means scrollLeft can sit a hair inside the bounds while
  // visually being at the end; a 1px tolerance avoids a dead notch.
  const atStart = deltaPx < 0 && scrollLeft <= 1
  const atEnd = deltaPx > 0 && scrollLeft >= maxScrollLeft - 1
  if (atStart || atEnd) {
    return { consume: false, nextScrollLeft: scrollLeft }
  }

  return {
    consume: true,
    nextScrollLeft: Math.max(0, Math.min(maxScrollLeft, target)),
  }
}

/**
 * Vue `@wheel` handler for an overflow-x strip.
 *
 * Must be bound as a NON-passive listener (`@wheel="..."`, no `.passive`) —
 * preventing the default is the entire point. `passiveScrollListeners.test.ts`
 * enforces the pairing (a non-passive wheel handler must call preventDefault).
 *
 * NESTED STRIPS: the chat input's strips nest (the outer one holds quote cards,
 * the inner one file cards), and a wheel event bubbles from the inner to the
 * outer. Without the `defaultPrevented` check below, one wheel notch would be
 * applied to BOTH strips at once. The check gives scroll chaining instead: the
 * inner strip scrolls until it reaches the boundary, then stops consuming and
 * the outer strip picks the gesture up — the same handoff the browser performs
 * natively for nested vertical scrollers.
 */
export function onHorizontalWheel(e: WheelEvent): void {
  // An inner strip already handled this notch — let it own the gesture.
  if (e.defaultPrevented) return

  const el = e.currentTarget as HTMLElement | null
  if (!el) return

  const decision = resolveHorizontalWheel(el.scrollLeft, el.scrollWidth, el.clientWidth, wheelDeltaPx(e))
  if (!decision.consume) return

  e.preventDefault()
  el.scrollLeft = decision.nextScrollLeft
}
