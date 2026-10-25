/**
 * Placing a `position: fixed` element at an exact VIEWPORT position.
 *
 * Every floating surface in the app (the table-block copy menu, PopupMenu and
 * its 15 consumers) is `position: fixed` so that no `overflow: hidden` ancestor
 * can clip it. That buys escape from clipping, but it costs a naive assumption:
 * "fixed means the viewport is my containing block".
 *
 * Two things in this app break that assumption:
 *
 *  - `.chat-message` carries `will-change: transform` in app mode (the Android
 *    WebView GPU ghost-artifact fix). `will-change: transform` establishes a
 *    containing block for fixed descendants — measured: a menu written to
 *    `top: rect.bottom + 4` rendered 60px too low, and the error grew with the
 *    ancestor's own offset.
 *  - The UI scale is applied as a CSS `zoom` on `<html>` by older builds. The
 *    current build scales through the root FONT SIZE instead (see
 *    css/variables.css), which leaves the coordinate space intact — so for a
 *    current build this measurement is only needed for the transform case
 *    above. It is kept general (it solves for the scale rather than assuming 1)
 *    because a page can still carry a zoom: a user stylesheet, a browser
 *    extension, or a stale tab from before the upgrade.
 *
 * Neither is detectable from `getBoundingClientRect()` alone, so both are
 * MEASURED: write a known offset, observe where the element actually lands, and
 * solve for the origin and scale.
 *
 * Keep this the single implementation — a second copy is how the two surfaces
 * drift apart again.
 */

export interface FixedContainingBlock {
  /** Viewport-space position the element lands at when CSS `left/top` are 0. */
  left: number
  top: number
  /** Viewport-space position the element's RIGHT/BOTTOM edge lands at when CSS
   *  `right/bottom` are 0. Together with `left`/`top` this gives the block's
   *  extent — which is NOT the viewport's when a transformed ancestor sits in
   *  between. */
  right: number
  bottom: number
  /** Rect px per CSS px (1 unless CSS `zoom` / a scaling transform is in play). */
  scaleX: number
  scaleY: number
}

/** Distance used for the probe. Large enough that rounding cannot swamp the
 *  derived scale, small enough to stay off-screen during the synchronous
 *  layout reads (the element is already displayed and mid-open here). */
const PROBE_PX = 100

/**
 * Measure the containing block a fixed element resolves against: its origin
 * (from `left/top: 0`), its far edges (from `right/bottom: 0`), and the
 * CSS-px→rect-px scale.
 *
 * MEASURED WITH A CLONE, never by touching the live element.
 *
 * The probe has to write CSS offsets and read back where the box lands, so the
 * first implementation wrote them onto the real element and restored them
 * afterwards. That is unobservable in principle but not in practice: the browser
 * paints between the writes and the restore, so the menu was seen jumping to its
 * containing block's corners. Playwright refuses to click a moving element
 * ("element is not stable") and, on the very first open — where there is no
 * previous offset to restore — the element simply STAYED in the corner and
 * every click missed it ("element is outside of the viewport"). Both symptoms
 * were reproduced on the `#` completion menu.
 *
 * A clone costs one extra element and removes the whole class: the probe box is
 * `position: fixed` inside the same parent, so it inherits the identical
 * containing block, transform and zoom, and the live element is never touched.
 *
 * MUST be called while `el` is laid out (not `display: none`) — a display:none
 * element has no parent chain worth cloning into.
 */
export function measureFixedContainingBlock(el: HTMLElement): FixedContainingBlock | null {
  // A box that is not laid out yet (mid-Transition, or still `display: none`)
  // reports zeros for every rect, and the probe would then "measure" a
  // containing block of (0,0,0,0) — which the caller feeds straight back into
  // the offsets, placing the menu off-screen. Refuse instead: the caller's
  // `getClientRects().length > 0` gate cannot catch this, because a box in its
  // enter-transition DOES have client rects while still measuring 0x0.
  // Measured: the first open of the `#` completion menu landed at
  // `bottom: -596.6px` (off-screen), the second open was correct.
  const own = el.getBoundingClientRect()
  if (own.width === 0 && own.height === 0) return null

  const probe = el.cloneNode(false) as HTMLElement
  // Same containing block (the clone lives in the same parent), but invisible
  // and out of hit-testing. `visibility` rather than `display:none` because a
  // display:none box has no rect to measure.
  probe.style.visibility = 'hidden'
  probe.style.pointerEvents = 'none'
  probe.style.margin = '0'
  // `transform: none` is NOT enough on its own: the menu is wrapped in a
  // <Transition> whose `*-enter-active` class declares a `transform` TRANSITION,
  // and during that transition the element is its own containing block for
  // `position: fixed` descendants — including this clone, which inherited the
  // same class list. The probe then measured the menu's own box (380x59) as the
  // containing block instead of the viewport, so `right`/`bottom` came out
  // hundreds of px short and the menu landed off-screen. Dropping the transition
  // classes is what makes the probe measure the real block.
  probe.style.transition = 'none'
  // Strip the Transition/transition-group class names, NOT the whole class
  // attribute.
  //
  // Only the transition classes are a problem: `*-enter-active` declares a
  // `transform` TRANSITION, which makes the box its own containing block for
  // fixed descendants, so the clone measured itself instead of the real block.
  //
  // `probe.className = ''` also removed the element's OWN classes, and that is
  // a silent measurement bug: `position: fixed` on every floating surface comes
  // FROM a class (`.table-block-copy-menu`, `.popup-menu`), so a stripped clone
  // degrades to a static box in normal flow. Measured in Chromium against
  // `.table-block-copy-menu`: `probePosition: "static"`, and the "block" came
  // out as `{left: 30, top: 30, right: 830, bottom: 30}` — a zero-height line
  // box at the parent's content edge instead of the viewport. Every caller that
  // fed those numbers back placed its menu at `left: -140px, top: 27`.
  //
  // Match Vue's Transition naming (`name-enter-active`, `name-leave-to`, …) and
  // the `v-` prefixed defaults, and leave everything else alone.
  for (const cls of Array.from(probe.classList)) {
    if (/-enter(-active|-from|-to)?$/.test(cls) || /-leave(-active|-from|-to)?$/.test(cls) || cls.startsWith('v-')) {
      probe.classList.remove(cls)
    }
  }
  probe.style.transform = 'none'
  probe.removeAttribute('id')
  el.parentElement?.appendChild(probe)
  try {
    // 1. Origin: where does (0,0) land?
    probe.style.left = '0px'
    probe.style.top = '0px'
    probe.style.right = ''
    probe.style.bottom = ''
    const atZero = probe.getBoundingClientRect()

    // 2. Scale: how far does a known offset move it?
    probe.style.left = `${PROBE_PX}px`
    probe.style.top = `${PROBE_PX}px`
    const atProbe = probe.getBoundingClientRect()

    // 3. Far edges: pin BOTH pairs to 0 so the box stretches to the containing
    // block's own extent, and read where its far edge lands.
    //
    // `width`/`height` must be cleared for this step: a fixed box with both
    // `left` and `right` set is over-constrained when it also carries an
    // explicit width, and the cascade then ignores `right` entirely — the far
    // edge comes back as `left + width` instead of the block's edge.
    //
    // `max-width`/`max-height` must go too, and that is not obvious: the clone
    // carries the real menu's `max-width: 380px`, so with `left:0; right:0` the
    // box stretches only to 380px and the measured "far edge" is 380 instead of
    // the viewport's 1280. Feeding that back made `cb.right` 900px too small, so
    // every menu was placed ~600px BELOW the viewport (measured `bottom:
    // -596.6px` on the `#` completion menu — its rows were unreachable).
    probe.style.width = ''
    probe.style.height = ''
    probe.style.maxWidth = ''
    probe.style.maxHeight = ''
    probe.style.left = '0px'
    probe.style.top = '0px'
    probe.style.right = '0px'
    probe.style.bottom = '0px'
    const atFar = probe.getBoundingClientRect()

    // `|| 1` covers the degenerate case where the box has no extent at all.
    return {
      left: atZero.left,
      top: atZero.top,
      right: atFar.right,
      bottom: atFar.bottom,
      scaleX: (atProbe.left - atZero.left) / PROBE_PX || 1,
      scaleY: (atProbe.top - atZero.top) / PROBE_PX || 1,
    }
  } finally {
    probe.remove()
  }
}

/** Convert a desired viewport X (left edge) into the CSS `left` value. */
export function toFixedLeft(desiredLeft: number, cb: FixedContainingBlock): number {
  return (desiredLeft - cb.left) / (cb.scaleX || 1)
}

/** Convert a desired viewport Y (top edge) into the CSS `top` value. */
export function toFixedTop(desiredTop: number, cb: FixedContainingBlock): number {
  return (desiredTop - cb.top) / (cb.scaleY || 1)
}

/** Convert a desired viewport X (RIGHT edge) into the CSS `right` value. */
export function toFixedRight(desiredRight: number, cb: FixedContainingBlock): number {
  return (cb.right - desiredRight) / (cb.scaleX || 1)
}

/** Convert a desired viewport Y (BOTTOM edge) into the CSS `bottom` value. */
export function toFixedBottom(desiredBottom: number, cb: FixedContainingBlock): number {
  return (cb.bottom - desiredBottom) / (cb.scaleY || 1)
}

/** The containing block's extent in CSS px (for clamping `maxWidth`). */
export function containingBlockWidthCss(cb: FixedContainingBlock): number {
  return (cb.right - cb.left) / (cb.scaleX || 1)
}

/**
 * Place a fixed element so its top-left corner sits at the given viewport
 * position. Clears `right`/`bottom`, which are relative to the containing
 * block's edges and therefore unsafe to leave in play.
 */
export function placeFixedAt(el: HTMLElement, left: number, top: number): void {
  el.style.right = ''
  el.style.bottom = ''
  const cb = measureFixedContainingBlock(el)
  if (!cb) {
    // Not laid out (display:none, or a 0x0 box): there is no containing block to
    // invert, so write the viewport coordinates straight through. The caller
    // repositions on the next layout change anyway.
    el.style.left = `${left}px`
    el.style.top = `${top}px`
    return
  }
  el.style.left = `${toFixedLeft(left, cb)}px`
  el.style.top = `${toFixedTop(top, cb)}px`
}
