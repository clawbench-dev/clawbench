/**
 * PopupMenu positioning utilities — pure functions for computing
 * the fixed-position style of a popup menu relative to an anchor element.
 *
 * Extracted from PopupMenu.vue for testability.
 *
 * Positioning strategy (simplified):
 * - Menu always appears above the anchor (mobile-adapted: bottom bar).
 * - Flips below only when there isn't enough space above.
 * - Horizontal alignment is **auto-detected** based on the anchor's
 *   position in the viewport: right side → right-aligned, left side → left-aligned.
 *   Callers can override with `anchor: 'left' | 'right'` to force alignment
 *   (useful for autocomplete menus that logically start at the left of the input).
 *
 * Coordinate space:
 *   The UI scale is applied by resizing the root font (css/variables.css), which
 *   leaves `getBoundingClientRect()`, `innerWidth` and `position: fixed` in ONE
 *   space — so no conversion is needed for scaling. `toFixedCSS` is retained as
 *   an identity for the fallback path (it still neutralises a legacy CSS `zoom`
 *   left on `<html>` by an older build or a user stylesheet).
 *
 * The conversion that IS needed is for the containing block. When an ancestor
 * establishes one for fixed descendants (`.chat-message` does, via
 * `will-change: transform` in app mode), both the origin and the scale differ
 * from the viewport's — callers pass a `containingBlock` measured by
 * `measureFixedContainingBlock` (utils/fixedPosition.ts) and every coordinate
 * is converted through it instead.
 */

import { getZoomedViewport, toFixedCSS } from '@/utils/viewportCoords'
import {
  toFixedLeft,
  toFixedTop,
  toFixedRight,
  toFixedBottom,
  containingBlockWidthCss,
  type FixedContainingBlock,
} from '@/utils/fixedPosition'

/**
 * Compute the CSS style object for a popup menu's fixed position.
 *
 * @param rect - The getBoundingClientRect() of the anchor element
 * @param opts - Positioning options
 * @returns A CSS style object suitable for binding to the menu element
 */
export function computeMenuStyle(
  rect: DOMRect,
  opts: {
    maxWidth?: number
    /**
     * Floor for the menu's own width (CSS px). Consumers whose popup should at
     * least line up with the control it drops out of pass the trigger's width
     * here. It belongs on the ROOT, not on the slot content: the root is the
     * scroll container, so a child wider than the root's content box paints a
     * horizontal scrollbar under the menu. Clamped to the effective max width
     * so a narrow viewport cannot push the menu past the screen edge.
     */
    minWidth?: number
    maxHeight?: number
    edgeMargin?: number
    menuItemsCount?: number
    viewportWidth?: number
    viewportHeight?: number
    anchor?: 'left' | 'right' | 'auto'
    /**
     * When false, the menu root does NOT scroll itself (`overflowY` is omitted).
     * Used by menus whose inner content scrolls independently so a header /
     * footer can stay pinned (e.g. the app-menu-styled theme picker).
     * Default: true
     */
    scrollable?: boolean
    /**
     * The element's ACTUAL containing block, from
     * `measureFixedContainingBlock` (utils/fixedPosition.ts).
     *
     * Omitting it falls back to the CSS-zoom assumption (`toFixedCSS`), which
     * is wrong whenever an ancestor establishes a containing block for fixed
     * descendants — `.chat-message` does so via `will-change: transform` in app
     * mode, and the menu then lands tens of pixels away from its anchor.
     * PopupMenu always passes it; the fallback exists for pure-function tests
     * and callers that have no element to measure.
     */
    containingBlock?: FixedContainingBlock
  } = {}
): Record<string, string> {
  const defaultVp = typeof window !== 'undefined' ? getZoomedViewport() : { width: 1024, height: 768 }
  const {
    maxWidth = 220,
    minWidth = 0,
    maxHeight = 320,
    edgeMargin = 6,
    viewportWidth = defaultVp.width,
    viewportHeight = defaultVp.height,
    anchor = 'auto',
    scrollable = true,
    containingBlock,
  } = opts

  const gap = 4

  // Viewport coordinate → CSS value. With a measured containing block the
  // conversion also accounts for a transformed ancestor (which shifts the
  // origin) and for the real scale; without one, fall back to CSS zoom.
  const toCssX = (v: number) => containingBlock ? toFixedLeft(v, containingBlock) : toFixedCSS(v)
  const toCssY = (v: number) => containingBlock ? toFixedTop(v, containingBlock) : toFixedCSS(v)
  const toCssRight = (v: number) => containingBlock ? toFixedRight(v, containingBlock) : toFixedCSS(viewportWidth - v)
  const toCssBottom = (v: number) => containingBlock ? toFixedBottom(v, containingBlock) : toFixedCSS(viewportHeight - v)
  /** CSS px available across the viewport (a CSS-px quantity, like maxWidth). */
  const cssViewportWidth = containingBlock
    ? containingBlockWidthCss(containingBlock)
    : toFixedCSS(viewportWidth)

  // --- Effective width: never let the menu overflow the viewport ---
  // maxWidth is a CSS (pre-zoom) value, so compare it against the viewport
  // width expressed in the same CSS space. On narrow viewports (mobile) or
  // under CSS zoom, the requested maxWidth can exceed the space between the
  // two edge margins; shrink it so left/right clamping alone cannot push the
  // menu past the screen edge.
  const availableWidth = Math.max(0, cssViewportWidth - edgeMargin * 2)
  const effectiveMaxWidth = Math.min(maxWidth, availableWidth)
  // Same border-box space as effectiveMaxWidth; 0 disables the floor.
  const effectiveMinWidth = Math.min(minWidth, effectiveMaxWidth)

  // --- Vertical positioning: prefer above anchor, flip below when near top ---
  const spaceAbove = rect.top - edgeMargin
  const spaceBelow = viewportHeight - rect.bottom - edgeMargin
  const goBelow = spaceAbove < spaceBelow

  // --- Horizontal positioning ---
  // When anchor is 'left' or 'right', force that alignment regardless of
  // the anchor element's position in the viewport.  'auto' uses the old
  // center-based heuristic.
  let alignRight: boolean
  if (anchor === 'left') {
    alignRight = false
  } else if (anchor === 'right') {
    alignRight = true
  } else {
    // auto: right side → right-aligned, left side → left-aligned
    const anchorCenterX = (rect.left + rect.right) / 2
    alignRight = anchorCenterX > viewportWidth / 2
  }

  const horizontal = alignRight
    ? computeRight(rect, effectiveMaxWidth, edgeMargin, cssViewportWidth, toCssRight)
    : computeLeft(rect, effectiveMaxWidth, edgeMargin, toCssX, cssViewportWidth)

  // --- Overflow: vertical only ---
  // The menu is a scroll container (overflowY below), so its `overflow-x`
  // computes to `auto` as well. Anything wider than the clamp — a wide word in
  // a label, a `min-width` written in unscaled design px, an inline row that
  // refuses to wrap — then paints a HORIZONTAL scrollbar under the menu even
  // though the box is already as wide as it may be. Measured: the context-usage
  // popup showed one at a non-1.0 UI scale. Max width is the ceiling, so the
  // content must adapt instead of scrolling sideways; `clip` also stops an
  // over-wide child from painting outside the rounded box.
  const overflow = { overflowX: 'clip' as const }

  // --- Reserve a lane for the vertical scrollbar ---
  // The app's global scrollbar is 10px wide (web/css/scrollbar.css) and, on a
  // desktop client, it is a CLASSIC bar: it takes its width out of the box and
  // paints over the last pixels of the content — right-aligned numbers (the
  // context-usage popup's values, 1,000,000 in the screenshot) end up with
  // their last digits cut away.
  //
  // `scrollbar-gutter` cannot fix this reliably: it is a no-op where the bar
  // overlays content (headless Chromium, macOS/iOS defaults) and, worse, its
  // `stable` value reserves the lane even when nothing scrolls — a dead strip
  // the user explicitly did not want. So the lane is reserved unconditionally
  // as padding instead: predictable on every engine, and the cost is only that
  // a short menu keeps a 10px gap on its right edge.
  //
  // It is applied ONLY when the menu scrolls itself (`scrollable`). With
  // `scrollable: false` the root never scrolls (the slot pins a header/footer
  // and scrolls a region of its own), so there is no bar here to dodge and an
  // unconditional pad would just make every app-header dropdown 10px wider.
  const lane = scrollable ? { paddingRight: '10px' as const } : {}

  if (goBelow) {
    // Menu appears BELOW the anchor
    const top = toCssY(rect.bottom + gap)
    const availableBelow = viewportHeight - (rect.bottom + gap) - edgeMargin
    const style: Record<string, string> = {
      position: 'fixed',
      top: `${top}px`,
      ...horizontal,
      ...overflow,
      ...lane,
      maxWidth: `${effectiveMaxWidth}px`,
      maxHeight: `min(${maxHeight}px, ${availableBelow / (containingBlock?.scaleY || 1)}px)`,
    }
    if (effectiveMinWidth > 0) style.minWidth = `${effectiveMinWidth}px`
    // When the menu doesn't scroll itself the caller handles overflow
    // internally (pinned header/footer + scrollable middle).
    if (scrollable) style.overflowY = 'auto'
    return style
  }

  // Menu appears ABOVE the anchor (preferred)
  const bottom = toCssBottom(rect.top - gap)
  const availableAbove = rect.top - gap - edgeMargin
  const style: Record<string, string> = {
    position: 'fixed',
    bottom: `${bottom}px`,
    ...horizontal,
    ...overflow,
    ...lane,
    maxWidth: `${effectiveMaxWidth}px`,
    maxHeight: `min(${maxHeight}px, ${availableAbove / (containingBlock?.scaleY || 1)}px)`,
  }
  if (effectiveMinWidth > 0) style.minWidth = `${effectiveMinWidth}px`
  if (scrollable) style.overflowY = 'auto'
  return style
}

/** Right-align the menu to the anchor's right edge. */
function computeRight(
  rect: DOMRect,
  maxWidth: number,
  edgeMargin: number,
  cssViewportWidth: number,
  toCssRightDist: (viewportRight: number) => number,
): Record<string, string> {
  // `right` is a CSS-px distance from the containing block's right edge, which
  // is why it is compared against the CSS-px viewport width (not the rect one).
  let right = toCssRightDist(rect.right)
  if (right + maxWidth + edgeMargin > cssViewportWidth) {
    right = cssViewportWidth - maxWidth - edgeMargin
  }
  right = Math.max(edgeMargin, right)
  return { right: `${right}px` }
}

/** Left-align the menu to the anchor's left edge. */
function computeLeft(
  rect: DOMRect,
  maxWidth: number,
  edgeMargin: number,
  toCssX: (viewportLeft: number) => number,
  cssViewportWidth: number,
): Record<string, string> {
  let left = toCssX(rect.left)
  if (left + maxWidth + edgeMargin > cssViewportWidth) {
    left = cssViewportWidth - maxWidth - edgeMargin
  }
  left = Math.max(edgeMargin, left)
  return { left: `${left}px` }
}
