import {
  applyUIScale,
  computeAutoUIScale,
  currentScreenShortEdge,
  readAppliedUIScale,
} from '@/utils/uiScale'

/**
 * Whether the reader turned "fit to screen" ON for this share page.
 *
 * Deliberately NOT the app's `uiScale` setting: that is a fixed factor the
 * reader chose in Settings, while this is a per-page toggle ("make this page
 * fit my screen"). Someone who set 150% in the app still wants the ability to
 * turn the share page back to its design size — and a reader who never ran the
 * app has no setting at all, only this switch.
 */
export const SHARE_FIT_KEY = 'clawbench-share-fit-to-screen'

/** Read the toggle. Unset / unreadable counts as OFF. */
export function isShareFitEnabled(): boolean {
  try {
    return localStorage.getItem(SHARE_FIT_KEY) === '1'
  } catch {
    // Private mode / blocked storage: treat as off (the design size), which is
    // always a legible page.
    return false
  }
}

/**
 * The scale the share page should use RIGHT NOW.
 *
 * Off → the design size (100%). On → fitted to the reader's screen, i.e. the
 * same computation the app's "auto fit" button performs. The app's own stored
 * factor is deliberately not consulted: it would make the switch appear to do
 * nothing for a reader who already set a factor (their page would be scaled
 * either way), and the switch has to have a visible effect in both directions.
 */
export function resolveShareUIScale(): number {
  if (!isShareFitEnabled()) return 1
  return computeAutoUIScale(currentScreenShortEdge())
}

/**
 * Apply the current scale. Called before `createApp` (so the first paint is
 * already correct — no reflow once the app mounts) and again on every toggle.
 *
 * Returns the applied factor (used by tests; the caller ignores it).
 */
export function applyShareUIScale(): number {
  const factor = resolveShareUIScale()
  applyUIScale(factor)
  return factor
}

/**
 * Whether fitting would change anything on THIS screen.
 *
 * False when the screen already maps to 100% (a phone, a 1080p monitor) — the
 * switch would be a control that visibly does nothing, and in a phone topbar it
 * would also eat the last of the file name's room. Hidden rather than disabled:
 * a greyed-out button invites the reader to wonder what is broken.
 *
 * Deliberately independent of the current ON/OFF state: a reader on a big
 * screen who turns fitting off must still see the switch to turn it back on.
 */
export function shareFitAvailable(): boolean {
  return computeAutoUIScale(currentScreenShortEdge()) !== 1
}

/**
 * Turn fitting on or off, persist the choice, and apply it immediately.
 *
 * Returns the factor now in effect, so the caller can report it (the toggle's
 * tooltip / aria-label names the resulting size).
 */
export function setShareFitEnabled(on: boolean): number {
  try {
    if (on) localStorage.setItem(SHARE_FIT_KEY, '1')
    else localStorage.removeItem(SHARE_FIT_KEY)
  } catch {
    // Storage unavailable: the choice still applies to this page view, it just
    // will not survive a reload.
  }
  return applyShareUIScale()
}

/**
 * Design-px width below which the outline moves from a side rail to a drawer.
 *
 * 900 design px, matching the reading column's own cap: narrower than that and
 * the rail would eat the text.
 */
export const SHARE_NARROW_MAX_DESIGN_PX = 899

/**
 * The media query the share views use for that decision, scaled.
 *
 * The threshold is a DESIGN width, and interface scaling makes every element
 * bigger without changing the window — so the same window holds fewer design
 * px. A fixed `(max-width: 899px)` would keep the rail on a window where the
 * scaled rail leaves the text too narrow (at 1.33× a 1200px window is really
 * ~900 design px, right at the edge). The app solves the same problem in
 * `computeIsWideScreen` by dividing the width by the scale; a media query
 * cannot do arithmetic on the viewport, so the threshold is multiplied instead.
 *
 * Media queries resolve `rem` against the INITIAL font size (16px), not the
 * root element's, so the unit cannot be reused here — the factor has to be
 * baked into the number.
 */
export function shareNarrowQuery(): string {
  const max = Math.round(SHARE_NARROW_MAX_DESIGN_PX * readAppliedUIScale())
  return `(max-width: ${max}px)`
}
