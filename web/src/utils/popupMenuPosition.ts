/**
 * PopupMenu positioning utilities — pure functions for computing
 * the fixed-position style of a popup menu relative to an anchor element.
 *
 * Extracted from PopupMenu.vue for testability.
 *
 * Positioning strategy (simplified):
 * - Menu always appears above the anchor (mobile-first: bottom bar).
 * - Flips below only when there isn't enough space above.
 * - Horizontal alignment is **auto-detected** based on the anchor's
 *   position in the viewport: right side → right-aligned, left side → left-aligned.
 *   Callers can override with `anchor: 'left' | 'right'` to force alignment
 *   (useful for autocomplete menus that logically start at the left of the input).
 */

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
    maxHeight?: number
    edgeMargin?: number
    menuItemsCount?: number
    viewportWidth?: number
    viewportHeight?: number
    anchor?: 'left' | 'right' | 'auto'
  } = {}
): Record<string, string> {
  const {
    maxWidth = 220,
    maxHeight = 320,
    edgeMargin = 6,
    menuItemsCount = 10,
    viewportWidth = typeof window !== 'undefined' ? window.innerWidth : 1024,
    viewportHeight = typeof window !== 'undefined' ? window.innerHeight : 768,
    anchor = 'auto',
  } = opts

  const gap = 4

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
    ? computeRight(rect, maxWidth, edgeMargin, viewportWidth)
    : computeLeft(rect, maxWidth, edgeMargin, viewportWidth)

  if (goBelow) {
    // Menu appears BELOW the anchor
    const top = rect.bottom + gap
    const availableBelow = viewportHeight - top - edgeMargin
    return {
      position: 'fixed',
      top: `${top}px`,
      ...horizontal,
      maxWidth: `${maxWidth}px`,
      maxHeight: `min(${maxHeight}px, ${availableBelow}px)`,
      overflowY: 'auto',
    }
  }

  // Menu appears ABOVE the anchor (preferred)
  const bottom = viewportHeight - rect.top + gap
  const availableAbove = rect.top - gap - edgeMargin
  return {
    position: 'fixed',
    bottom: `${bottom}px`,
    ...horizontal,
    maxWidth: `${maxWidth}px`,
    maxHeight: `min(${maxHeight}px, ${availableAbove}px)`,
    overflowY: 'auto',
  }
}

/** Right-align the menu to the anchor's right edge. */
function computeRight(
  rect: DOMRect,
  maxWidth: number,
  edgeMargin: number,
  viewportWidth: number,
): Record<string, string> {
  let right = viewportWidth - rect.right
  if (right + maxWidth + edgeMargin > viewportWidth) {
    right = viewportWidth - maxWidth - edgeMargin
  }
  right = Math.max(edgeMargin, right)
  return { right: `${right}px` }
}

/** Left-align the menu to the anchor's left edge. */
function computeLeft(
  rect: DOMRect,
  maxWidth: number,
  edgeMargin: number,
  viewportWidth: number,
): Record<string, string> {
  let left = rect.left
  if (left + maxWidth + edgeMargin > viewportWidth) {
    left = viewportWidth - maxWidth - edgeMargin
  }
  left = Math.max(edgeMargin, left)
  return { left: `${left}px` }
}
