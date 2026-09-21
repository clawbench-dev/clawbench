/**
 * v-running-sweep directive — phase-locked travelling band for running sessions.
 *
 * Usage: <i v-running-sweep class="session-running-band"></i>
 *
 * Put it on an element that exists ONLY while the session is running (the call
 * sites use `v-if="session.running"`); it starts the sweep on mount and cancels
 * it on unmount, so there is no state to keep in sync by hand.
 *
 * ── Why not a plain CSS `animation:` ────────────────────────────────────────
 * A CSS keyframes animation starts at the moment its element first matches the
 * rule, so each row's phase is its own mount time. Rows that begin running at
 * different moments then sweep at different phases — and the same row loses its
 * phase whenever the browser re-creates the animation (Vue's TransitionGroup
 * reorders rows on every list refresh, which restarts a CSS animation). The
 * visible symptom is exactly what users report: several running sessions whose
 * bands are out of step with each other.
 *
 * ── The fix ────────────────────────────────────────────────────────────────
 * Drive the animation through the Web Animations API and pin its `startTime` to
 * the timeline origin. Every band in the document then reads its progress
 * straight from the shared document timeline, so two bands created seconds
 * apart are at the *same* phase, by construction rather than by luck:
 *
 *     phase = (document.timeline.currentTime - startTime) / duration
 *
 * With `startTime` a shared constant (0), the phase is a pure function of the
 * shared clock. The same holds across the sidebar and the drawer, the project
 * and cross-project panes, and — unlike the custom-property clock this replaced
 * — it costs nothing on the main thread: the transform stays compositor-driven
 * (~0.16ms/frame, measured equal to the old CSS animation, versus ~1.3ms/frame
 * for an animated custom property).
 *
 * A WAAPI animation is also *not* restarted by a DOM move or a `display:none`
 * toggle (both re-create a CSS animation), so the phase survives list reorders
 * and tab switches for free.
 */

/** One full pass. Mirrors the 2s the band has always used. */
const SWEEP_DURATION_MS = 2000

/**
 * Travel of one band, as a percentage of the band's OWN width.
 *
 * The band is 80% of the row wide (`.session-running-band`), so -100% parks it
 * just off the left edge and 125% (= 100% / 0.8) carries it just off the right
 * edge. Both ends are fully outside the row, which makes the wrap from 125%
 * back to -100% invisible: the next pass starts the instant the previous one
 * leaves, with no overlap and no gap. These two numbers are therefore tied to
 * the 80% width in SessionList.vue — change one and the other must follow.
 */
const SWEEP_FROM = 'translateX(-100%)'
const SWEEP_TO = 'translateX(125%)'

interface SweepElement extends HTMLElement {
  _sweepAnim?: Animation
}

function stop(el: SweepElement) {
  el._sweepAnim?.cancel()
  delete el._sweepAnim
}

function mounted(el: SweepElement) {
  // jsdom (unit tests) and pre-WAAPI engines have no Element.animate. The
  // sweep is decoration, so skip it rather than throw.
  if (typeof el.animate !== 'function') return

  const anim = el.animate(
    [{ transform: SWEEP_FROM }, { transform: SWEEP_TO }],
    {
      duration: SWEEP_DURATION_MS,
      iterations: Infinity,
      easing: 'linear',
      // Apply the first keyframe while the animation is still pending, so the
      // band never flashes at translateX(0) (its left shoulder at the row edge)
      // for one frame before the clock takes over.
      fill: 'both',
    },
  )

  // Pin to the timeline origin: every band shares this anchor, so they all read
  // the same phase off the shared clock. This is the whole point of the
  // directive — see the header.
  anim.startTime = 0

  el._sweepAnim = anim
}

function unmounted(el: SweepElement) {
  stop(el)
}

export const RunningSweepDirective = { mounted, unmounted }
