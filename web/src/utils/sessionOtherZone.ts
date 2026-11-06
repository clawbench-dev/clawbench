/**
 * Geometry for the "other projects" strip that sits under the project list.
 *
 * The strip has to answer two competing demands:
 *   - the current project's sessions must stay fully visible (no scrolling)
 *     whenever that is possible at all;
 *   - whatever height is left over should go to other projects, so more of
 *     their active sessions are on screen at once.
 *
 * So the strip gets `availH - currentContentH` (the space the project list
 * does not need), clamped from below by a two-row floor and from above by both
 * its own content and a share of the pane. When the project list alone already
 * overflows, the floor wins and the project list scrolls — the priority rule
 * cannot be honoured, so the strip must not shrink to a sliver.
 *
 * Pure function on purpose: jsdom reports every height as 0, so the component
 * cannot be tested for this. The caller measures and passes real pixels.
 */

/** Hard cap on the strip, as a share of the whole list area. */
export const OTHER_ZONE_CAP_RATIO = 0.6

/** Minimum number of other-project rows kept visible. */
export const OTHER_ZONE_MIN_ROWS = 2

export interface OtherZoneMetrics {
  /** Height of the whole list area (project pane + strip), in CSS px. */
  availH: number
  /** Full content height of the project pane, in CSS px. */
  currentContentH: number
  /** Full content height of the other-project rows, in CSS px. */
  otherContentH: number
  /** Rendered height of ONE other-project row, in CSS px. */
  rowH: number
  capRatio?: number
  minRows?: number
}

/**
 * Body height (rows only — the header is rendered separately) for the strip.
 *
 * Returns 0 when there is nothing to show, and the full content height when
 * the host gives no measurable constraint (the mobile sheet sizes itself to
 * content, so `availH` is 0 there and the strip must simply show everything).
 */
export function computeOtherZoneBodyHeight(metrics: OtherZoneMetrics): number {
  const { availH, currentContentH, otherContentH, rowH } = metrics
  if (!(otherContentH > 0)) return 0
  // A host with no fixed height (auto-sized sheet) — show all of it.
  if (!(availH > 0)) return Math.round(otherContentH)
  // Row measurement not available yet (first paint, fonts loading). Falling
  // back to "show everything" is safe: the next measure pass corrects it.
  if (!(rowH > 0)) return Math.round(otherContentH)

  const minRows = metrics.minRows ?? OTHER_ZONE_MIN_ROWS
  const capRatio = metrics.capRatio ?? OTHER_ZONE_CAP_RATIO

  const floor = rowH * minRows
  const cap = Math.max(floor, availH * capRatio)
  const leftover = availH - currentContentH
  const wanted = Math.max(floor, leftover)

  return Math.max(0, Math.round(Math.min(wanted, otherContentH, cap)))
}

/**
 * Stable tint for a project, used by the strip's per-row project chip.
 *
 * Deliberately theme tokens rather than fixed hex values: the palette follows
 * all 36 themes and stays legible in both light and dark. Collisions between
 * two projects are acceptable (the chip also carries the project name).
 */
const PROJECT_TINT_VARS = ['--color-purple', '--color-orange', '--color-green', '--color-info']

export function projectTintVar(projectPath: string): string {
  let hash = 0
  for (let i = 0; i < projectPath.length; i++) {
    hash = (hash * 31 + projectPath.charCodeAt(i)) | 0
  }
  const index = Math.abs(hash) % PROJECT_TINT_VARS.length
  return `var(${PROJECT_TINT_VARS[index]})`
}
