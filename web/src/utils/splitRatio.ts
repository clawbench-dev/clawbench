export const MIN_PANEL_WIDTH = 320
/**
 * Default left-pane share of the wide-screen split (4:6, left slightly
 * narrower than chat). Used both as the composable's initial value and as the
 * fallback when a ratio can't be used.
 */
export const DEFAULT_RATIO = 0.4

/** Coerce an unknown value into a ratio in [0, 1]; defaults to DEFAULT_RATIO. */
export function normalizeRatio(raw: unknown): number {
  if (typeof raw !== 'number' || !Number.isFinite(raw)) return DEFAULT_RATIO
  return Math.min(1, Math.max(0, raw))
}

/**
 * Clamp a ratio so the left panel stays within [minLeft, containerWidth - minRight].
 * Returns DEFAULT_RATIO when the container can't hold two min-width panels.
 */
export function clampRatio(
  ratio: number,
  containerWidth: number,
  minLeft = MIN_PANEL_WIDTH,
  minRight = MIN_PANEL_WIDTH,
): number {
  if (!Number.isFinite(containerWidth) || containerWidth <= 0) return DEFAULT_RATIO
  if (containerWidth <= minLeft + minRight) return DEFAULT_RATIO
  const maxLeft = containerWidth - minRight
  const leftPx = Math.min(maxLeft, Math.max(minLeft, ratio * containerWidth))
  return leftPx / containerWidth
}
