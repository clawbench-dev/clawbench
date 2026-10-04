/**
 * Circular "reveal" transition for theme switching.
 *
 * Uses the View Transitions API to snapshot the old theme, apply the new one,
 * then wipe it in with a `clip-path: circle()` growing from the toggle button —
 * the new theme inside the circle, the old theme outside it.
 *
 * Everything here degrades to an instant switch when the API is unavailable
 * (older browsers, jsdom) or the user prefers reduced motion. The colour change
 * itself is the information; the animation is decoration, so opting out of it
 * loses nothing.
 */

/** Minimal shape of the bits of `ViewTransition` we use. */
export interface ThemeRevealTransition {
  ready: Promise<unknown>
  finished: Promise<unknown>
}

export interface ThemeRevealOrigin {
  x: number
  y: number
}

export interface ThemeRevealOptions {
  /** Centre of the expanding circle, in viewport (client) coordinates. */
  origin?: ThemeRevealOrigin | null
  /** Animation length in ms. */
  durationMs?: number
}

/** Matches the 0.2s root fade in base.css, scaled up for a full-screen wipe. */
const DEFAULT_DURATION_MS = 400

/** Class that suppresses CSS transitions while the snapshots are captured. */
const REVEAL_ACTIVE_CLASS = 'theme-reveal-active'

// A second switch while one is still animating would be skipped by the browser
// (the callback might not run), which would silently drop the theme change.
// Fall back to an instant switch instead.
let revealInFlight = false

/** True when the user has asked the OS/browser to minimise animation. */
export function prefersReducedMotion(): boolean {
  return typeof window !== 'undefined'
    && typeof window.matchMedia === 'function'
    && window.matchMedia('(prefers-reduced-motion: reduce)').matches
}

/**
 * Convert a viewport-space origin into the percentage form used by the reveal
 * `clip-path`, plus the percentage radius that reaches the farthest corner.
 *
 * Percentages are the whole point here: the View Transitions pseudo-element's
 * `clip-path` is resolved against the *snapshot box*, whose size can differ from
 * `window.innerWidth/Height` — the app's UI-scale setting applies CSS `zoom` on
 * `<html>`, and Android WebView additionally scales the layout viewport. A
 * pixel-based circle computed from `getBoundingClientRect()` is therefore
 * misplaced and under-sized on those clients (it drifts toward the top-left and
 * stops short of the corners). A percentage centre and radius both resolve
 * against the same box, so any uniform scale cancels out and the wipe is correct
 * everywhere.
 *
 * `circle()`'s percentage radius resolves against `sqrt(w² + h²) / sqrt(2)`, so
 * the radius fraction is the corner distance over that reference.
 */
export function revealClipPercent(
  x: number,
  y: number,
  width: number,
  height: number,
): { cx: number; cy: number; radius: number } {
  const w = width > 0 ? width : 1
  const h = height > 0 ? height : 1
  const distance = Math.hypot(Math.max(x, w - x), Math.max(y, h - y))
  const reference = Math.hypot(w, h) / Math.SQRT2
  return {
    cx: (x / w) * 100,
    cy: (y / h) * 100,
    radius: (distance / reference) * 100,
  }
}

/** Centre point of an element, or null when it has no box (unmounted/hidden). */
export function originFromElement(el: Element | null | undefined): ThemeRevealOrigin | null {
  if (!el || typeof el.getBoundingClientRect !== 'function') return null
  const rect = el.getBoundingClientRect()
  if (rect.width === 0 && rect.height === 0) return null
  return { x: rect.left + rect.width / 2, y: rect.top + rect.height / 2 }
}

/**
 * Origin for the theme reveal: the app-header quick toggle, so EVERY entry
 * point (the header menu and the settings theme grid) wipes from the identical
 * top-right spot. Falls back to the top-right corner when the button has no box
 * (e.g. a layout without the header).
 */
export function headerThemeOrigin(): ThemeRevealOrigin | null {
  if (typeof document === 'undefined') return null
  const fromButton = originFromElement(document.querySelector('.theme-quick-toggle'))
  if (fromButton) return fromButton
  if (typeof window === 'undefined') return null
  return { x: window.innerWidth, y: 0 }
}

/**
 * Apply a theme change, revealing it with a circular wipe from `origin` when
 * possible.
 *
 * `applyChange` runs inside the transition callback and may return a promise:
 * the browser waits for it to settle before capturing the "new" snapshot. This
 * matters for frameworks — Vue applies DOM updates on the next microtask, so a
 * synchronous callback would snapshot the OLD DOM and reveal nothing. Callers
 * should therefore `await nextTick()` inside `applyChange`.
 *
 * Returns the transition so callers/tests can await it, or null when the change
 * was applied instantly (unsupported environment, reduced motion, no origin, or
 * a reveal already running).
 */
export function applyThemeWithReveal(
  applyChange: () => void | Promise<void>,
  options: ThemeRevealOptions = {},
): ThemeRevealTransition | null {
  // lib.dom types `startViewTransition` as always present, but it is genuinely
  // absent in older browsers and jsdom, so re-widen it to optional.
  const start = (document as { startViewTransition?: Document['startViewTransition'] }).startViewTransition
  const origin = options.origin ?? null

  if (revealInFlight || !origin || typeof start !== 'function' || prefersReducedMotion()) {
    void applyChange()
    return null
  }

  const root = document.documentElement
  // Freeze every CSS transition for the duration so both snapshots capture the
  // final colours: without this, the "new" snapshot is taken at the start of
  // each element's own transition and shows the old theme.
  root.classList.add(REVEAL_ACTIVE_CLASS)

  let transition: ThemeRevealTransition
  try {
    transition = start.call(document, () => applyChange()) as unknown as ThemeRevealTransition
  } catch {
    root.classList.remove(REVEAL_ACTIVE_CLASS)
    void applyChange()
    return null
  }

  revealInFlight = true
  const cleanup = () => {
    revealInFlight = false
    root.classList.remove(REVEAL_ACTIVE_CLASS)
  }

  // Percentages (not pixels) so the circle is correct regardless of how the
  // snapshot box is scaled — see revealClipPercent.
  const clip = revealClipPercent(origin.x, origin.y, window.innerWidth, window.innerHeight)
  const cx = `${clip.cx}%`
  const cy = `${clip.cy}%`

  if (transition.ready && typeof transition.ready.then === 'function') {
    transition.ready
      .then(() => {
        root.animate(
          { clipPath: [`circle(0% at ${cx} ${cy})`, `circle(${clip.radius}% at ${cx} ${cy})`] },
          {
            duration: options.durationMs ?? DEFAULT_DURATION_MS,
            easing: 'ease-in-out',
            pseudoElement: '::view-transition-new(root)',
          },
        )
      })
      // `ready` rejects when the transition is skipped (e.g. a new navigation);
      // the theme change itself already happened, so there is nothing to do.
      .catch(() => {})
  }

  if (transition.finished && typeof transition.finished.then === 'function') {
    transition.finished.then(cleanup, cleanup)
  } else {
    cleanup()
  }

  return transition
}

/** Test-only: clear the in-flight guard left behind by an unfinished reveal. */
export function _resetThemeRevealForTest(): void {
  revealInFlight = false
  document.documentElement.classList.remove(REVEAL_ACTIVE_CLASS)
}
