import { ref, type Ref } from 'vue'

/**
 * Badge capsule feedback — a short accent highlight on a badge segment
 * (project name, current file, branch) inside the AppHeader capsule when its
 * content changes.
 *
 * The former fill/collapse machinery (overflow measurement → other segments
 * slide shut → expand back) is gone: on narrow screens even a full capsule
 * could not show a very long name, and the layout animation churned every
 * frame. Now a change simply flashes the changed segment's accent background
 * for HIGHLIGHT_MS and the caller shows a floating "reveal card" under the
 * segment with the complete text (see AppHeader.vue).
 *
 * Reduced motion uses a shorter flash; the highlight is a plain background
 * color, so no geometry animates regardless.
 *
 * Timing is seq-guarded: a new pulse while one is active cancels the pending
 * clear of the previous one (a fast sequence of changes leaves only the last
 * segment highlighted).
 */

/** Segment types that live inside the badge capsule. */
export type BadgeSegment = 'project' | 'branch' | 'file'

/** How long the accent flash stays on for a normal pulse. */
export const HIGHLIGHT_MS = 650

/** Shorter flash under prefers-reduced-motion. */
export const REDUCED_HIGHLIGHT_MS = 300

export function useBadgeHighlight(options: {
  /** Whether reduced motion is currently requested (live-checked per pulse). */
  prefersReducedMotion?: Ref<boolean>
}) {
  const prefersReducedMotion = options.prefersReducedMotion ?? ref(false)

  const highlightBadge = ref<BadgeSegment | null>(null)

  let highlightClearTimer: ReturnType<typeof setTimeout> | null = null
  // Animation generation guard: each pulseBadge bumps the sequence; the
  // pending clear captures its own seq and bails out if a newer change
  // already superseded it. This prevents a stale clear from wiping the
  // highlight of a newer pulse.
  let animSeq = 0

  function cancelPendingClear() {
    if (highlightClearTimer) { clearTimeout(highlightClearTimer); highlightClearTimer = null }
  }

  /** Reactive class object for a badge segment (project / branch / file).
      `fileVisible` reports whether the current-file segment is showing its
      empty-state text (no file open) — only relevant for the 'file' source.
      The `no-file` key carries no CSS styles (the empty-state look keys off
      `.no-file-name` text styling), but the component's integration tests use
      it as a state probe, so it is kept. */
  function segmentClass(source: BadgeSegment, fileVisible: boolean): Record<string, boolean> {
    return {
      'no-file': source === 'file' && !fileVisible,
      'badge-highlight': highlightBadge.value === source,
    }
  }

  /** Flash the changed segment's accent background for HIGHLIGHT_MS (or
      REDUCED_HIGHLIGHT_MS under reduced motion). A newer pulse supersedes the
      current one — the previous clear is cancelled and the sequence restarts. */
  function pulseBadge(source: BadgeSegment) {
    cancelPendingClear()
    const seq = ++animSeq
    highlightBadge.value = source
    const ms = prefersReducedMotion.value ? REDUCED_HIGHLIGHT_MS : HIGHLIGHT_MS
    highlightClearTimer = setTimeout(() => {
      if (seq !== animSeq) return // superseded by a newer pulse
      highlightBadge.value = null
      highlightClearTimer = null
    }, ms)
  }

  /** Dispose: cancel the pending clear (e.g. onUnmounted). */
  function dispose() {
    cancelPendingClear()
  }

  return {
    highlightBadge,
    pulseBadge,
    segmentClass,
    dispose,
  }
}
