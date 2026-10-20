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
 *  - The UI scale applies CSS `zoom` on `<html>`, which both moves the origin
 *    and scales the offsets.
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
 * CSS-px→rect-px scale. Restores the element's previous inline offsets.
 *
 * The far edges are probed separately rather than derived from the element's
 * own width: `width` is the ELEMENT's box, not the containing block's, so
 * `origin + width` is only right by accident (it is wrong the moment the
 * containing block is a transformed ancestor narrower than the viewport).
 *
 * MUST be called while the element is laid out (not `display: none`) — a
 * display:none element reports a 0x0 rect and the probe silently yields
 * origin (0,0), scale 1.
 */
export function measureFixedContainingBlock(el: HTMLElement): FixedContainingBlock {
  const prev = {
    left: el.style.left,
    top: el.style.top,
    right: el.style.right,
    bottom: el.style.bottom,
  }

  // 1. Origin: where does (0,0) land?
  el.style.right = ''
  el.style.bottom = ''
  el.style.left = '0px'
  el.style.top = '0px'
  const atZero = el.getBoundingClientRect()

  // 2. Scale: how far does a known offset move it?
  el.style.left = `${PROBE_PX}px`
  el.style.top = `${PROBE_PX}px`
  const atProbe = el.getBoundingClientRect()

  // 3. Far edges: where does the element's right/bottom edge land at right:0?
  el.style.left = ''
  el.style.top = ''
  el.style.right = '0px'
  el.style.bottom = '0px'
  const atFar = el.getBoundingClientRect()

  el.style.left = prev.left
  el.style.top = prev.top
  el.style.right = prev.right
  el.style.bottom = prev.bottom

  // `|| 1` covers the degenerate case where the element has no box at all.
  return {
    left: atZero.left,
    top: atZero.top,
    right: atFar.right,
    bottom: atFar.bottom,
    scaleX: (atProbe.left - atZero.left) / PROBE_PX || 1,
    scaleY: (atProbe.top - atZero.top) / PROBE_PX || 1,
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
  el.style.left = `${toFixedLeft(left, cb)}px`
  el.style.top = `${toFixedTop(top, cb)}px`
}
