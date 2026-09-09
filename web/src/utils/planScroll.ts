/**
 * Centering logic for the plan timeline (PlanPanel). While an agent runs, the
 * currently executing (in_progress) plan entry should stay visible in the
 * middle of the panel — whenever the panel opens/expands, or when execution
 * advances to a new step. When no step is in_progress the list is never
 * force-scrolled (the user keeps their place).
 *
 * All decision logic is pure (no Vue/DOM) so it is unit-testable.
 */

import type { PlanEntry } from '@/composables/usePlanProgress'

/** Index of the currently executing (in_progress) entry, or -1 when none. */
export function activeEntryIndex(entries: PlanEntry[]): number {
  return entries.findIndex(e => e.status === 'in_progress')
}

export interface CenterScrollArgs {
  /** The container's current scrollTop. */
  scrollTop: number
  /**
   * Vertical offset (px) of the target row's center relative to the container's
   * visible top edge.
   */
  rowCenter: number
  /** Visible height (px) of the scroll container. */
  containerHeight: number
  /** Largest legal scrollTop (scrollHeight - clientHeight), possibly 0. */
  maxScrollTop: number
}

/**
 * Scroll position that places a row whose center sits `rowCenter` px below the
 * container's top edge at the vertical center of the container, clamped into
 * the scrollable range. Pure so geometry callers only measure and write.
 */
export function centeredScrollTop({
  scrollTop,
  rowCenter,
  containerHeight,
  maxScrollTop,
}: CenterScrollArgs): number {
  const target = scrollTop + rowCenter - containerHeight / 2
  return Math.min(Math.max(target, 0), Math.max(maxScrollTop, 0))
}
