/**
 * Automatic UI scaling for high-resolution screens.
 *
 * The layout is designed against a 1080p reference (1920×1080): a larger
 * screen would otherwise render the same pixel sizes across more physical
 * space, making everything look small. So the UI is scaled up on screens
 * taller than the reference.
 *
 * `screenHeight` is deliberately read from `window.screen.height`, which
 * reports CSS pixels (DIP) — NOT the physical panel resolution. Verified in
 * Chromium: a 4K panel driven at 200% OS scaling reports height 1080 and
 * devicePixelRatio 2. This is exactly the "subtract the system scaling"
 * behaviour we want: a 4K screen already scaled by the OS gets factor 1.0
 * (no double scaling), while a 4K screen running at 100% gets factor 2.0.
 */

/** Height in CSS pixels the layout was designed against (1080p). */
export const UI_SCALE_BASE_HEIGHT = 1080

/**
 * Upper bound on the automatic factor. Matches the manual slider's ceiling
 * (`uiScale` max 1.5 is the stored value, but the CSS-zoom applier clamps at
 * 2) so both paths can never exceed what the layout is expected to survive.
 */
export const UI_SCALE_MAX = 2

/**
 * Never shrink automatically. The request was explicitly "only scale up for
 * higher resolutions" — a 768p laptop keeps factor 1.0 rather than being
 * squeezed to 0.71, which would make text too small to read comfortably.
 */
export const UI_SCALE_MIN = 1

/**
 * Granularity of the manual scale slider, in factor units (5%).
 *
 * The auto factor is snapped to this grid so it is always REPRESENTABLE on
 * that slider. A `<input type=range>` silently snaps an off-grid value to the
 * nearest step: 1.33 (1440p) lands on the 1.35 step, so the thumb would sit at
 * 135% while the label read "133%" — the two disagreeing is exactly the bug
 * this prevents. Keeping the step here (and using it for the slider's own
 * `step` in settingsFieldMap) means the grid has a single source of truth.
 */
export const UI_SCALE_STEP = 0.05

/** Round to 2 decimals so the applied factor is stable and displayable. */
function round2(n: number): number {
  return Math.round(n * 100) / 100
}

/**
 * Snap a factor to the slider's step grid.
 *
 * Snapped relative to 0, which matches the slider's grid because its minimum
 * (0.8) is itself a whole multiple of the step. `settingsFieldMap.test.ts`
 * guards that relationship so a future change to either value cannot silently
 * break the alignment.
 */
function snapToScaleStep(value: number): number {
  return round2(Math.round(value / UI_SCALE_STEP) * UI_SCALE_STEP)
}

/**
 * Factor derived purely from the screen height.
 *
 * Returns 1 for any unusable input (missing/zero/negative/NaN height) so a
 * headless or exotic environment degrades to "no scaling" rather than
 * producing a broken factor.
 */
export function computeAutoUIScale(
  screenHeight: number,
  base: number = UI_SCALE_BASE_HEIGHT,
  max: number = UI_SCALE_MAX,
): number {
  if (!Number.isFinite(screenHeight) || screenHeight <= 0) return 1
  if (!Number.isFinite(base) || base <= 0) return 1
  const raw = round2(screenHeight / base)
  const bounded = Math.min(Math.max(raw, UI_SCALE_MIN), max)
  // Snap, then re-bound: snapping moves by at most half a step, so it can in
  // principle cross the cap if the cap ever stopped being on the grid.
  return Math.min(Math.max(snapToScaleStep(bounded), UI_SCALE_MIN), max)
}

/**
 * The factor actually in effect, given the user's auto/manual preference.
 *
 * Auto ON  → derived from the screen, manual value ignored.
 * Auto OFF → the manual slider value, clamped to the same sane bounds.
 *
 * Kept pure so both the settings UI (which displays the effective value) and
 * the applier (which applies it) derive it the same way and cannot drift.
 */
export function resolveUIScale(
  auto: boolean,
  manual: number,
  screenHeight: number,
): number {
  if (auto) return computeAutoUIScale(screenHeight)
  const n = Number(manual)
  if (!Number.isFinite(n) || n <= 0) return 1
  // Snap for the same reason as the auto path: a value stored off the grid
  // (e.g. a legacy 0.82) would otherwise be displayed as 82% while the slider
  // rendered its thumb on the 0.8 step.
  const bounded = Math.min(Math.max(n, 0.5), UI_SCALE_MAX)
  return Math.min(Math.max(snapToScaleStep(bounded), 0.5), UI_SCALE_MAX)
}

/**
 * Current screen height in CSS pixels, or 0 when unavailable (SSR/tests).
 * Wrapped so callers do not each need the try/undefined dance.
 */
export function currentScreenHeight(): number {
  try {
    return Number(window.screen?.height) || 0
  } catch {
    return 0
  }
}
