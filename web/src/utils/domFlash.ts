/**
 * Unified DOM "flash" helper — the single place that applies a transient
 * highlight class to an element and removes it when the highlight is done.
 *
 * The app's jump/localization feedback (TOC / file-line / search-match /
 * chat-message jumps) all animate a short accent blink on the target element.
 * Historically every call site inlined the same `add class → wait for
 * animationend → remove class` boilerplate (each with its own cleanup path
 * and timing constant). This module collects that logic once.
 *
 * Cleanup is double-guaranteed (pattern borrowed from RefreshButton):
 *   - `animationend` removes the class right when the CSS animation finishes
 *     (real browsers), AND
 *   - a fallback `setTimeout(duration + pad)` removes it in environments where
 *     the animation never fires — jsdom (no CSS engine), OS-level
 *     prefers-reduced-motion (this helper intentionally still adds the class
 *     so the reduced-motion static tint shows, see the CSS media queries), or
 *     any other reason the animation event is lost.
 *
 * The helper also forces a clean re-play when the same element is flashed
 * again while a flash is still pending (class removed first + reflow), so a
 * rapid sequence of jumps to the same target is one crisp pulse instead of a
 * no-op or an overlapping double blink.
 *
 * Timings: the CSS animation durations live in the stylesheets
 * (`var(--flash-duration, 0.7s)` in code-viewer.css / search-bar.css /
 * share-chrome.css / ChatMessageList.vue scoped). KEEP LINE_FLASH_MS in sync
 * with that CSS variable — change both together.
 */

/** Duration of the canonical line-flash animation. Must match
 *  `--flash-duration` in code-viewer.css / search-bar.css / share-chrome.css
 *  / ChatMessageList.vue. */
export const LINE_FLASH_MS = 700

/** Duration used when the user prefers reduced motion: the class is still
 *  added so the reduced-motion static tint (CSS media query) shows, but the
 *  highlight is held only briefly before removal. */
export const REDUCED_FLASH_MS = 250

/** Extra time past the CSS animation before the fallback timer removes the
 *  class, in case `animationend` never fires. */
export const LINE_FLASH_FALLBACK_PAD_MS = 80

/** Default highlight class applied to jump targets. */
export const DEFAULT_FLASH_CLASS = 'line-flash'

export interface FlashElementOptions {
  /** Highlight class to add. Defaults to 'line-flash'. */
  className?: string
  /** How long to hold the class when the animation runs normally.
   *  Defaults to LINE_FLASH_MS. */
  durationMs?: number
  /** Live reduced-motion check (injectable for tests). Defaults to the
   *  module-level matchMedia probe. */
  prefersReducedMotion?: () => boolean
  /** Called exactly once after the class has been removed (or immediately
   *  when the element is already disconnected). Used e.g. by SearchDrawer to
   *  unwrap a temporary anchor after its flash ends. */
  onDone?: () => void
}

interface InFlightFlash {
  timer: ReturnType<typeof setTimeout> | null
  className: string
  animationEndHandler: () => void
}

const inFlight = new WeakMap<Element, InFlightFlash>()

/** Live probe — do not cache, the OS setting can change at runtime. */
export function prefersReducedMotion(): boolean {
  return (
    typeof window !== 'undefined' &&
    typeof window.matchMedia === 'function' &&
    window.matchMedia('(prefers-reduced-motion: reduce)').matches === true
  )
}

/** Remove the class and cancel any in-flight cleanup for an element right
 *  away (also safe when nothing is flashing). */
export function clearFlash(el: Element): void {
  const existing = inFlight.get(el)
  if (existing) {
    if (existing.timer !== null) clearTimeout(existing.timer)
    el.removeEventListener('animationend', existing.animationEndHandler)
    inFlight.delete(el)
    el.classList.remove(existing.className)
  }
}

/**
 * Flash a DOM element: add the highlight class, then remove it once the
 * animation finishes (or after a fallback timer). Repeated calls on the same
 * element restart the animation cleanly.
 */
export function flashElement(el: Element, opts?: FlashElementOptions): void {
  const className = opts?.className ?? DEFAULT_FLASH_CLASS
  const reduced = opts?.prefersReducedMotion?.() ?? prefersReducedMotion()
  const duration = reduced ? REDUCED_FLASH_MS : (opts?.durationMs ?? LINE_FLASH_MS)

  // Already-disconnected elements cannot animate and are often about to be
  // removed (SearchDrawer unwraps its temp anchor after the layout settles);
  // still report completion so callers don't leave dangling work.
  if (!el.isConnected) {
    opts?.onDone?.()
    return
  }

  // Restart cleanly: cancel any pending flash on this element and remove the
  // class first, then force a reflow when the class was already present.
  // Removing + re-adding within the same frame is what restarts a completed
  // animation; a running animation is cancelled by the removal itself.
  const existing = inFlight.get(el)
  if (existing) {
    if (existing.timer !== null) clearTimeout(existing.timer)
    el.removeEventListener('animationend', existing.animationEndHandler)
    inFlight.delete(el)
  }
  const hadClass = el.classList.contains(className)
  if (hadClass) {
    el.classList.remove(className)
    // Force layout so the re-add below restarts the animation even when the
    // removal and re-add land in the same frame. jsdom tolerates this (the
    // offsetWidth accessor exists and returns 0).
    void (el as HTMLElement).offsetWidth
  }

  let done = false
  const finish = () => {
    if (done) return
    done = true
    const current = inFlight.get(el)
    if (current && current.className === className) {
      if (current.timer !== null) clearTimeout(current.timer)
      inFlight.delete(el)
    }
    el.classList.remove(className)
    opts?.onDone?.()
  }

  const animationEndHandler = () => finish()
  el.classList.add(className)
  el.addEventListener('animationend', animationEndHandler, { once: true })
  const timer = setTimeout(finish, duration + LINE_FLASH_FALLBACK_PAD_MS)
  inFlight.set(el, { timer, className, animationEndHandler })
}
