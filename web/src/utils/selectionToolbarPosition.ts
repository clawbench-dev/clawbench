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
}

/**
 * Compute the `position: fixed` style placing the toolbar next to `rect`.
 *
 * Vertical: prefers ABOVE the selection (the pointer is usually below the text
 * the user just dragged over). Flips BELOW only when there is not enough room
 * above — an unconditional "above" would push the bar off-screen for a
 * selection near the top of the viewport.
 *
 * Horizontal: centres on the selection, then clamps into the viewport so a
 * selection at either edge cannot push the bar past it.
 *
 * Returns `position`/`left` plus exactly one of `top`/`bottom` (anchoring to
 * `bottom` when above keeps the gap correct regardless of the bar's height).
 */
export function computeSelectionToolbarStyle(
  rect: SelectionAnchorRect,
  opts: SelectionToolbarOptions,
): Record<string, string> {
  const { barWidth, barHeight, gap = 8, edgeMargin = 8 } = opts
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
  // `fitsAbove` and both emitted values work entirely in pre-zoom CSS space
  // (gap / edgeMargin are design tokens, not rect-space measurements), so the
  // visual gap stays constant under the app's `html { zoom }` setting.
  const cssTop = toFixedCSS(rect.top)
  const fitsAbove = cssTop - edgeMargin >= barHeight + gap
  if (fitsAbove) {
    // Anchor to the bottom edge so the gap holds whatever the bar's height is.
    style.bottom = `${toFixedCSS(vp.height - rect.top) + gap}px`
  } else {
    style.top = `${toFixedCSS(rect.bottom) + gap}px`
  }

  return style
}
