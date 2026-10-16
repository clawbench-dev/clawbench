/**
 * Positioning for the selection toolbar (the floating quote/comment/copy bar
 * that follows a text selection — issue #544).
 *
 * Kept separate from `quoteQuestionUtils.ts` because that module is deliberately
 * free of any dependency (its docblock promises "no Vue reactivity
 * dependencies"); this one needs the UI-scale helpers to survive the app's
 * `html { zoom }` setting, exactly like `popupMenuPosition.ts`.
 *
 * Coordinate spaces — the whole point of this module:
 *   - `getBoundingClientRect()` returns ZOOM-SCALED coordinates.
 *   - `position: fixed` CSS values are PRE-zoom layout pixels.
 *   - `offsetWidth` / `offsetHeight` are also PRE-zoom layout pixels.
 * Everything below is therefore normalised into pre-zoom CSS space before it
 * is emitted, and the caller must pass the bar's `offsetWidth`/`offsetHeight`
 * (not its bounding rect) so the two sides agree.
 */

import { getZoomedViewport, toFixedCSS } from '@/composables/useSettingsConfig'

/** The parts of a DOMRect this module needs. Kept structural for testability. */
export interface SelectionAnchorRect {
  top: number
  bottom: number
  left: number
  right: number
}

export interface SelectionToolbarOptions {
  /** Bar width in pre-zoom layout px (pass `element.offsetWidth`). */
  barWidth: number
  /** Bar height in pre-zoom layout px (pass `element.offsetHeight`). */
  barHeight: number
  /** Gap between the selection and the bar, pre-zoom px. */
  gap?: number
  /** Minimum distance kept from every viewport edge, pre-zoom px. */
  edgeMargin?: number
  /**
   * Viewport-space px hidden at the BOTTOM of the layout viewport by the
   * on-screen keyboard (0 when there is none). The visible region is therefore
   * `[topInset, vp.height - bottomInset]`.
   *
   * Needed because this module's vertical math otherwise assumes the layout
   * viewport is fully visible. That holds on Android (`adjustResize` shrinks
   * the layout viewport, so the keyboard is already excluded), but NOT on a
   * mobile browser, where the keyboard shrinks only the VISUAL viewport — a bar
   * placed against the layout bottom then sits behind the keyboard.
   */
  bottomInset?: number
  /** Viewport-space px hidden at the TOP (visualViewport.offsetTop). */
  topInset?: number
}

/**
 * Compute the `position: fixed` style placing the toolbar next to `rect`.
 *
 * Vertical: prefers ABOVE the selection (the pointer is usually below the text
 * the user just dragged over). Flips BELOW only when there is not enough room
 * above — an unconditional "above" would push the bar off-screen for a
 * selection near the top of the viewport.
 *
 * A candidate side is only usable when BOTH of its edges fall inside the
 * VISIBLE region (`[topInset, height - bottomInset]`). With the soft keyboard
 * open that region is short, and frequently neither side fits; the bar is then
 * clamped into the region rather than left hanging behind the keyboard (see
 * `clampIntoVisible`). When the bar is taller than the region it is pinned to
 * the region's BOTTOM, which keeps the input row — the bottom of the panel —
 * next to the keyboard where the user is looking.
 *
 * Horizontal: centres on the selection, then clamps into the viewport so a
 * selection at either edge cannot push the bar past it.
 *
 * Returns `position`/`left` plus exactly one of `top`/`bottom` (anchoring to
 * `bottom` when above keeps the gap correct regardless of the bar's height).
 * Deliberately no `maxHeight`: see `clampIntoVisible` for why capping the height
 * would oscillate against the component's own measurement.
 */
export function computeSelectionToolbarStyle(
  rect: SelectionAnchorRect,
  opts: SelectionToolbarOptions,
): Record<string, string> {
  const { barWidth, barHeight, gap = 8, edgeMargin = 8, bottomInset = 0, topInset = 0 } = opts
  const vp = getZoomedViewport()

  // Pre-zoom viewport width; both operands below are pre-zoom, so the clamp
  // compares like with like.
  const cssVpWidth = toFixedCSS(vp.width)

  // Never let the bar be wider than the space between the two edge margins.
  // On a narrow viewport (or under `html { zoom }`, which shrinks the pre-zoom
  // viewport) the measured width can exceed it — clamping `left` alone would
  // then pin the bar to the left edge while it still overflowed the right one.
  const availableWidth = Math.max(0, cssVpWidth - edgeMargin * 2)
  const effectiveWidth = Math.min(barWidth, availableWidth)

  // --- Horizontal: centre on the selection, then clamp into the viewport ---
  const centerX = (rect.left + rect.right) / 2
  let left = toFixedCSS(centerX) - effectiveWidth / 2
  left = Math.max(edgeMargin, Math.min(left, cssVpWidth - effectiveWidth - edgeMargin))

  const style: Record<string, string> = {
    position: 'fixed',
    left: `${left}px`,
    maxWidth: `${availableWidth}px`,
  }

  // --- Vertical: above by default, flip below when it would not fit ---
  // Everything below works in pre-zoom CSS space (gap / edgeMargin / the insets
  // are design tokens or viewport measurements, not rect-space values), so the
  // visual gap stays constant under the app's `html { zoom }` setting.
  const cssTop = toFixedCSS(rect.top)
  const cssBottom = toFixedCSS(rect.bottom)
  // Visible vertical band in pre-zoom CSS space.
  const visTop = toFixedCSS(topInset)
  const visBottom = toFixedCSS(vp.height) - toFixedCSS(bottomInset)
  const visibleHeight = Math.max(0, visBottom - visTop)

  // A side is usable only when the bar's RESULTING BOX lies inside the visible
  // band. Checking "is there room above the selection" is not enough: when the
  // keyboard covers the selection itself, the space above it is inside the
  // visible band but the bar placed there is still behind the keyboard (the
  // selection, and therefore the bar above it, sits below `visBottom`).
  //
  // The edge margin is required on both edges, which also keeps the bar clear
  // of a browser-chrome or keyboard boundary it would otherwise touch.
  const aboveBottom = cssTop - gap
  const aboveTop = aboveBottom - barHeight
  const fitsAbove = aboveTop >= visTop + edgeMargin && aboveBottom <= visBottom - edgeMargin

  const belowTop = cssBottom + gap
  const belowBottom = belowTop + barHeight
  const fitsBelow = belowTop >= visTop + edgeMargin && belowBottom <= visBottom - edgeMargin

  if (fitsAbove) {
    // Anchor to the bottom edge so the gap holds whatever the bar's height is.
    style.bottom = `${toFixedCSS(vp.height - rect.top) + gap}px`
    return style
  }
  if (fitsBelow) {
    style.top = `${toFixedCSS(rect.bottom) + gap}px`
    return style
  }

  // Neither side fits — the keyboard has left too little room beside the
  // selection. Clamp into the visible band instead of leaving the bar behind
  // the keyboard.
  return clampIntoVisible(style, {
    visTop,
    visibleHeight,
    barHeight,
    gap,
    cssTop,
    // `bottom` is measured from the layout bottom, so the distance down to the
    // visible bottom IS the inset (already pre-zoom here).
    bottomToVisible: toFixedCSS(bottomInset),
  })
}

/**
 * Place the bar inside the visible band when it does not fit beside the
 * selection.
 *
 * Two cases, both deliberately free of a height cap:
 *
 * - It FITS the band: sit as close to the selection as the band allows,
 *   without crossing either visible edge.
 * - It is TALLER than the band: pin the BOTTOM edge to the visible bottom. The
 *   expanded panel's last child is the input row, so anchoring the bottom keeps
 *   the textarea and its send button just above the keyboard — the part the
 *   user is actually typing into — and lets the quoted snippet overflow above
 *   the visible area, which is the harmless half to lose.
 *
 * No `maxHeight` is emitted on purpose. The component feeds its own measured
 * `offsetHeight` back in as `barHeight`; capping the element would shrink that
 * measurement, which could then satisfy the "fits" test and drop the cap again
 * — an oscillation that would visibly resize the bar every frame. Growing
 * upward from a bottom anchor avoids the feedback entirely.
 */
function clampIntoVisible(
  style: Record<string, string>,
  ctx: {
    visTop: number
    visibleHeight: number
    barHeight: number
    gap: number
    cssTop: number
    bottomToVisible: number
  },
): Record<string, string> {
  const { visTop, visibleHeight, barHeight, gap, cssTop, bottomToVisible } = ctx

  if (barHeight <= visibleHeight) {
    const lowestTop = visTop + visibleHeight - barHeight
    const desiredTop = Math.min(Math.max(cssTop - gap - barHeight, visTop), lowestTop)
    style.top = `${desiredTop}px`
    return style
  }

  style.bottom = `${bottomToVisible}px`
  return style
}
