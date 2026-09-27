/**
 * v-running-sweep directive — phase-locked travelling comet for running sessions.
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
 * comets are out of step with each other.
 *
 * ── The fix ────────────────────────────────────────────────────────────────
 * Drive the animation through the Web Animations API and pin its `startTime` to
 * the timeline origin. Every comet in the document then reads its progress
 * straight from the shared document timeline, so two comets created seconds
 * apart are at the *same* phase, by construction rather than by luck:
 *
 *     phase = (document.timeline.currentTime - startTime) / duration
 *
 * With `startTime` a shared constant (0), the phase is a pure function of the
 * shared clock. The same holds across the sidebar and the drawer, the project
 * and cross-project panes, and — unlike the custom-property clock this replaced
 * — it costs nothing on the main thread: the transform stays compositor-driven.
 *
 * A WAAPI animation is also *not* restarted by a DOM move or a `display:none`
 * toggle (both re-create a CSS animation), so the phase survives list reorders
 * and tab switches for free.
 */

/** One full pass. */
const SWEEP_DURATION_MS = 1500

/**
 * Travel of one comet, as a percentage of the comet's OWN width.
 *
 * The travel is deliberately expressed against the comet rather than the track,
 * so it can ride on `transform` (compositor-only) instead of `left` (which
 * relayouts every frame).
 *
 * The design specifies the travel as a fraction of the TRACK: the comet starts
 * with its left edge at -40% of the track and ends with it at +102%, so it is
 * fully clear of both ends at the extremes and the wrap is invisible — the next
 * pass starts the instant the previous one leaves, with no overlap and no gap.
 *
 * The comet is 38% of the track wide (`.session-running-band` in
 * SessionList.vue), so those two positions become:
 *
 *     start: -40  / 38 * 100 = -105.26%
 *     end:   102  / 38 * 100 =  268.42%
 *
 * These three numbers — 38% width, -40%, 102% — are one design and must move
 * together. Change the width and both keyframes must be recomputed.
 */
const SWEEP_FROM = 'translateX(-105.26%)'
const SWEEP_TO = 'translateX(268.42%)'

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
      // A gentle S-curve, not `linear`: the comet eases in and out of each pass.
      // Safe across the wrap because the curve's slope at t=0 and t=1 are equal
      // (both ≈0.111), so the velocity does not jump when one pass becomes the
      // next. The slowed ends are off-screen anyway, so all this does is soften
      // the moment the comet enters and leaves.
      easing: 'cubic-bezier(.45,.05,.55,.95)',
      // Apply the first keyframe while the animation is still pending, so the
      // comet never flashes at translateX(0) (its left shoulder at the row edge)
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
