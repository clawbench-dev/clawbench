/**
 * Dropdown panel positioning for the header's quick-index dropdowns.
 *
 * Each dropdown (project / recent files / branches) is teleported to body and
 * needs fixed positioning centered under its anchor badge button. Positioning
 * is called twice per open:
 *  1. Synchronously, with an estimated width, so the panel never renders at the
 *     browser's default (far-left) position — this is what caused the visible
 *     flash.
 *  2. After a double rAF (see deferPosition), with the measured real width, for
 *     a precise fit.
 *
 * All functions are pure (no DOM reads beyond the caller-supplied geometry),
 * which keeps them unit-testable.
 */

import { toFixedCSS } from '@/composables/useSettingsConfig'

const PANEL_MARGIN = 8
const PANEL_MAX_WIDTH = 320
/** Panel width must never fall below the anchor button's width, but at least 180. */
const PANEL_MIN_WIDTH = 180
/** Default panel height used when the panel isn't laid out yet. */
const DEFAULT_PANEL_HEIGHT = 320

export type DropdownStyle = Record<string, string>

/** Estimated initial width — matches the minWidth set in positionDropdown. */
export function estimatePanelWidth(anchorRect: { width: number } | null, viewportWidth: number): number {
  if (!anchorRect) return 200
  const maxPanelWidth = Math.min(PANEL_MAX_WIDTH, viewportWidth - 16)
  return Math.min(Math.max(PANEL_MIN_WIDTH, anchorRect.width), maxPanelWidth)
}

/**
 * Compute the fixed-position style for a dropdown panel centered under its
 * anchor, clamped to the viewport (with a margin). Flips upward when there
 * isn't room below.
 *
 * @param anchorRect    getBoundingClientRect() of the anchor button
 * @param panelWidth    measured panel width (falls back to an estimate)
 * @param panelHeight   measured panel height (falls back to a default)
 * @param viewportWidth / Height — window.innerWidth/innerHeight
 * @returns CSS style for the fixed-position panel
 */
export function computeDropdownStyle(
  anchorRect: { left: number; right: number; bottom: number; top: number; width: number },
  panelWidth: number,
  panelHeight: number,
  viewportWidth: number,
  viewportHeight: number,
): DropdownStyle {
  const margin = PANEL_MARGIN
  // Panel width must never exceed the viewport (accounting for margins).
  const maxPanelWidth = Math.min(PANEL_MAX_WIDTH, viewportWidth - 2 * margin)
  const width = Math.min(panelWidth || maxPanelWidth, maxPanelWidth)
  const height = panelHeight || DEFAULT_PANEL_HEIGHT

  // Center horizontally on the anchor, then clamp so the panel stays in view.
  let left = anchorRect.left + anchorRect.width / 2 - width / 2
  left = Math.max(margin, Math.min(viewportWidth - width - margin, left))

  let top = anchorRect.bottom + 4
  // Flip upward if there isn't room below.
  if (top + height > viewportHeight - margin) {
    top = Math.max(margin, anchorRect.top - height - 4)
  }
  top = Math.max(margin, Math.min(viewportHeight - margin - height, top))

  return {
    position: 'fixed',
    top: `${toFixedCSS(top)}px`,
    left: `${toFixedCSS(left)}px`,
    minWidth: `${Math.min(Math.max(PANEL_MIN_WIDTH, anchorRect.width), maxPanelWidth)}px`,
    maxWidth: `${toFixedCSS(maxPanelWidth)}px`,
  }
}

/**
 * Position a dropdown after the current render + a double rAF so that the
 * panel's content width/layout is stable before measuring it. Without this,
 * the panel width read on first open (e.g. while branch/project lists are
 * still loading) differs from the settled width, causing the horizontally
 * centered position to jump between opens.
 */
export function deferPosition(update: () => void) {
  requestAnimationFrame(() => {
    requestAnimationFrame(() => {
      update()
    })
  })
}

/**
 * Position a dropdown centered under its anchor button, writing the computed
 * style into `styleRef.value`.
 */
export function positionDropdown(
  anchorRect: { left: number; right: number; bottom: number; top: number; width: number } | null | undefined,
  panel: { offsetWidth: number; offsetHeight: number } | null | undefined,
  styleRef: { value: Record<string, string> },
  estimatedWidth?: number,
  viewport?: { width: number; height: number },
) {
  if (!anchorRect) return
  const vp = viewport ?? { width: window.innerWidth, height: window.innerHeight }
  const maxPanelWidth = Math.min(PANEL_MAX_WIDTH, vp.width - 2 * PANEL_MARGIN)
  const measured = panel?.offsetWidth || 0
  const panelWidth = Math.min(measured || estimatedWidth || maxPanelWidth, maxPanelWidth)
  const panelHeight = panel?.offsetHeight || DEFAULT_PANEL_HEIGHT
  styleRef.value = computeDropdownStyle(anchorRect, panelWidth, panelHeight, vp.width, vp.height)
}
