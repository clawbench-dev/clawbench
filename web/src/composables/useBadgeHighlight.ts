import { ref, type Ref } from 'vue'

/**
 * Badge capsule feedback — staged highlight/fill animation on a badge segment
 * content change (project name, current file, branch) inside the AppHeader
 * capsule.
 *
 * Timeline:
 *   1. HIGHLIGHT_PRE_MS — the changed segment highlights (accent background).
 *      This always happens.
 *   2. Only when the capsule is space-constrained (its natural content width
 *      overflows the available capsule width, i.e. text would be truncated)
 *      does the segment then FILL the capsule — other segments slide shut.
 *   3. FILL_MS / HIGHLIGHT_POST_MS — everything expands back.
 *   4. finally the highlight fades out.
 *
 * `highlightBadge` drives the accent background (always); `fillBadge` drives
 * the collapse/expand of the other segments (only on truncation).
 *
 * The overflow/shape measurement is deferred to a single requestAnimationFrame
 * slot (matching the caller's DOM-write frame) so the scrollWidth /
 * getBoundingClientRect reads never force a synchronous reflow. Tests drive the
 * same single-slot contract deterministically via `flushPendingMeasurement()`.
 *
 * Reduced-motion fast-path: when the user's OS/browser requests reduced
 * motion, skip the staged fill/expand and show a brief static highlight
 * instead of the layout animation. `prefersReducedMotion` is checked live so a
 * runtime change applies to the next pulse.
 */

/** Segment types that live inside the badge capsule. */
export type BadgeSegment = 'project' | 'branch' | 'file'

/** Shape of the highlighted segment when NOT filling. */
export type HighlightShape = 'left' | 'right' | 'none'

export const HIGHLIGHT_PRE_MS = 200
export const FILL_MS = 1000
export const HIGHLIGHT_POST_MS = 200
export const HIGHLIGHT_NO_FILL_MS = 400
const EDGE_THRESHOLD_PX = 2
// Longest possible fill timeline — kept in sync with the sum of the staged
// delays so the safety-net reset never fires before the fill path releases.
const TOTAL_MS = HIGHLIGHT_PRE_MS + FILL_MS + HIGHLIGHT_POST_MS

/**
 * Decide the highlighted segment's shape when it is NOT filling the capsule:
 * touching the capsule's left edge → round left side; right edge → round
 * right side; in the middle → rectangle. Positions are measured via
 * getBoundingClientRect and normalized into the capsule's coordinate space —
 * offsetLeft is relative to the offsetParent (the fixed <header>), which is a
 * different coordinate system than the capsule width.
 */
export function decideHighlightShape(left: number, right: number, capsuleWidth: number): HighlightShape {
  if (left <= EDGE_THRESHOLD_PX) return 'left'
  if (right >= capsuleWidth - EDGE_THRESHOLD_PX) return 'right'
  return 'none'
}

/**
 * True when any badge segment's text is truncated (ellipsis) in the capsule.
 * Detected via the text spans that have overflow:hidden + text-overflow:
 * ellipsis — a truncated span has scrollWidth > clientWidth. The capsule
 * itself is a flex container without overflow:hidden, so its own scrollWidth
 * equals clientWidth even when children are clipped.
 */
export function capsuleOverflowing(capsule: HTMLElement | null): boolean {
  const el = capsule
  if (!el) return false
  const textSpans = el.querySelectorAll<HTMLElement>('.project-name, .branch-name, .current-file-name')
  for (const span of textSpans) {
    if (span.scrollWidth > span.clientWidth + 1) return true // +1 for rounding
  }
  return false
}

/** Segment DOM node for the highlighted badge wrapper. */
export function highlightSegmentEl(capsule: HTMLElement | null, source: BadgeSegment): HTMLElement | null {
  const el = capsule
  if (!el) return null
  const sel = source === 'project' ? '.project-dropdown-wrapper'
    : source === 'branch' ? '.branch-badge' : '.current-file-badge'
  return el.querySelector<HTMLElement>(sel)
}

export function useBadgeHighlight(options: {
  /** The capsule container element (hosts the badge segments). */
  capsuleRef: Ref<HTMLElement | null>
  /** Whether reduced motion is currently requested (live-checked per pulse). */
  prefersReducedMotion?: Ref<boolean>
  /** Injectable measurement dependencies — for tests / environments without layout. */
  overflowing?: (capsule: HTMLElement | null, source: BadgeSegment) => boolean
  measureShape?: (capsule: HTMLElement | null, source: BadgeSegment) => HighlightShape
}) {
  const { capsuleRef } = options
  const prefersReducedMotion = options.prefersReducedMotion ?? ref(false)
  const overflowing = options.overflowing ?? capsuleOverflowing
  const measureShape = options.measureShape ?? ((capsule: HTMLElement | null, source: BadgeSegment): HighlightShape => {
    const seg = highlightSegmentEl(capsule, source)
    if (!capsule || !seg) return 'none'
    const cRect = capsule.getBoundingClientRect()
    const sRect = seg.getBoundingClientRect()
    // Normalize into the capsule coordinate space (capsule left edge = 0).
    const left = sRect.left - cRect.left
    const right = sRect.right - cRect.left
    return decideHighlightShape(left, right, cRect.width)
  })

  const highlightBadge = ref<BadgeSegment | null>(null)
  const fillBadge = ref<BadgeSegment | null>(null)
  const highlightRadius = ref<HighlightShape | null>(null)

  let highlightTimer: ReturnType<typeof setTimeout> | null = null
  let fillTimer: ReturnType<typeof setTimeout> | null = null
  let clearTimer: ReturnType<typeof setTimeout> | null = null
  // Single rAF handle for the deferred overflow/shape measurement (was nested
  // nextTick; rAF lands after layout of the content write has been computed,
  // so the read no longer forces a synchronous reflow in the same frame).
  let measureRaf: ReturnType<typeof requestAnimationFrame> | null = null
  // The pending deferred measurement, captured for the test-only flush hook.
  let pendingMeasurement: { source: BadgeSegment; seq: number } | null = null

  // Animation generation guard: each pulseBadge bumps the sequence; async
  // callbacks (rAF / timers) capture their own seq and bail out if a newer
  // change already superseded them. This prevents orphan fill timers and stale
  // measurements when two badge sources change within the same tick.
  let animSeq = 0

  function clearTimers() {
    if (highlightTimer) { clearTimeout(highlightTimer); highlightTimer = null }
    if (fillTimer) { clearTimeout(fillTimer); fillTimer = null }
    if (clearTimer) { clearTimeout(clearTimer); clearTimer = null }
    if (measureRaf !== null) { cancelAnimationFrame(measureRaf); measureRaf = null }
    pendingMeasurement = null
  }

  /** Non-fill highlight shape → border-radius: left edge → round-left pill,
      right edge → round-right pill, middle → rectangle. Ignored when the
      segment fills the capsule (`.badge-highlight--fill` overrides with a
      full pill). */
  function shapeStyle(shape: HighlightShape | null): Record<string, string> {
    switch (shape) {
      case 'left': return { borderRadius: '999px 0 0 999px' }
      case 'right': return { borderRadius: '0 999px 999px 0' }
      default: return { borderRadius: '0' }
    }
  }

  /**
   * Reactive class object for a badge segment (project / branch / file).
   * `fileVisible` reports whether the current-file segment is showing its
   * empty-state text (no file open) — only relevant for the 'file' source.
   * The `no-file` key carries no CSS styles (the empty-state look keys off
   * `.no-file-name` text styling), but the component's integration tests use
   * it as a state probe, so it is kept.
   */
  function segmentClass(source: BadgeSegment, fileVisible: boolean): Record<string, boolean> {
    return {
      'no-file': source === 'file' && !fileVisible,
      'badge-segment-hidden': fillBadge.value !== null && fillBadge.value !== source,
      'badge-highlight': highlightBadge.value === source,
      'badge-highlight--fill': fillBadge.value === source,
    }
  }

  /** Inline style for a highlighted segment: the position-dependent half-pill
      shape when highlighted but not filling. */
  function segmentStyle(source: BadgeSegment): Record<string, string> | undefined {
    return highlightBadge.value === source && fillBadge.value !== source
      ? shapeStyle(highlightRadius.value)
      : undefined
  }

  /** Deferred end-of-animation: drop the highlight (and any fill). Captures
      seq so a newer pulse supersedes it. Shared by every completion path. */
  function endHighlight(seq: number) {
    if (seq !== animSeq) return
    fillBadge.value = null
    highlightBadge.value = null
    highlightRadius.value = null
  }

  /** Drop the highlight HIGHLIGHT_NO_FILL_MS after the start (no-fill pulse)
      or HIGHLIGHT_POST_MS after the fill releases — whichever path schedules
      it. Reuses the highlightTimer slot for cleanup. */
  function scheduleHighlightEnd(seq: number, ms: number) {
    if (highlightTimer) clearTimeout(highlightTimer)
    highlightTimer = setTimeout(() => {
      endHighlight(seq)
      highlightTimer = null
    }, ms)
  }

  /** Run the deferred overflow/shape measurement for the current pulse. This
      is the body of the single rAF slot; tests call it directly. */
  function runPendingMeasurement() {
    if (pendingMeasurement === null) return
    const { source, seq } = pendingMeasurement
    pendingMeasurement = null
    if (seq !== animSeq) return // superseded while waiting
    highlightRadius.value = measureShape(capsuleRef.value, source)
    if (overflowing(capsuleRef.value, source)) {
      fillTimer = setTimeout(() => {
        fillTimer = null
        if (seq !== animSeq) return // superseded while waiting
        fillBadge.value = source

        // Expand back after the fill window...
        clearTimer = setTimeout(() => {
          clearTimer = null
          if (seq !== animSeq) return
          fillBadge.value = null
          // ...then, after HIGHLIGHT_POST_MS, drop the highlight.
          scheduleHighlightEnd(seq, HIGHLIGHT_POST_MS)
        }, FILL_MS)
      }, HIGHLIGHT_PRE_MS)
    } else {
      // No fill: keep the highlight briefly, then drop it. Short window
      // so a plain highlight doesn't feel stuck for the full fill length.
      scheduleHighlightEnd(seq, HIGHLIGHT_NO_FILL_MS)
    }
  }

  function pulseBadge(source: BadgeSegment) {
    clearTimers()
    const seq = ++animSeq

    // Reset any previous fill/highlight state so a mid-fill change doesn't
    // leave the old segment filling while the new one is highlighted.
    fillBadge.value = null
    highlightRadius.value = null

    // 1. Highlight first (accent background on the changed segment) — always.
    highlightBadge.value = source

    // Reduced motion (or a superseded pulse): static short highlight only —
    // no fill/expand layout animation, no overflow/shape measurement.
    if (prefersReducedMotion.value) {
      scheduleHighlightEnd(seq, HIGHLIGHT_NO_FILL_MS)
      return
    }

    // 2. Then, only if the capsule is space-constrained, fill it (collapse
    //    the other segments). Measurement is deferred to a
    //    requestAnimationFrame: rAF callbacks run AFTER the browser has
    //    computed layout for the new content, so the scrollWidth /
    //    getBoundingClientRect reads below never force a synchronous reflow
    //    in the frame that just wrote the DOM (nextTick ran synchronously
    //    with the flush and did force one). Also decide the non-fill
    //    highlight shape from the segment's position.
    pendingMeasurement = { source, seq }
    measureRaf = requestAnimationFrame(() => {
      measureRaf = null
      runPendingMeasurement()
    })

    // Safety net: ensure the highlight always resets, filled or not. Longest
    // possible window; overwritten by the branch-specific timer above when it
    // fires first.
    highlightTimer = setTimeout(() => {
      endHighlight(seq)
      highlightTimer = null
    }, TOTAL_MS)
  }

  /** Dispose: cancel the rAF slot and all timers (e.g. onUnmounted). */
  function dispose() {
    clearTimers()
  }

  /** @internal Run the deferred measurement immediately — for tests. */
  function flushPendingMeasurement() {
    runPendingMeasurement()
  }

  return {
    highlightBadge,
    fillBadge,
    highlightRadius,
    pulseBadge,
    segmentClass,
    segmentStyle,
    dispose,
    flushPendingMeasurement,
  }
}
