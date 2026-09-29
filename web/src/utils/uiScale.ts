/**
 * UI scale math for high-resolution screens.
 *
 * The layout is designed against a 1080p reference (1920×1080): a larger
 * screen would otherwise render the same pixel sizes across more physical
 * space, making everything look small. `computeAutoUIScale` derives a factor
 * that compensates, and the settings "auto fit" button applies it ONCE by
 * storing the result as the user's scale.
 *
 * Nothing here runs on startup any more. The applied factor is the stored
 * `uiScale` value (`resolveUIScale`), so it cannot change on its own — the
 * previous design recomputed from the screen on every applier call, which made
 * the UI jump.
 *
 * `screenHeight` is read from `window.screen.height`, which is supposed to be
 * in CSS pixels (DIP) — NOT the physical panel resolution. On Windows, macOS
 * and X11/XWayland that holds: a 4K panel driven at 200% OS scaling reports
 * height 1080 and devicePixelRatio 2, so `height / 1080` already subtracts the
 * system scaling (4K at 200% → factor 1.0, 4K at 100% → factor 2.0).
 *
 * Native Wayland (Ubuntu's default GNOME session) is the exception: Chromium
 * reports `screen.width/height` in DEVICE pixels there — the same 4K panel at
 * 200% reports height 2160 with devicePixelRatio 2. Feeding that straight in
 * yields factor 2.0 on top of the OS's 2×, i.e. a 4× UI. Measured with Chrome
 * 154 on a live GNOME 46 Wayland session (4K VX2880-4K-HDU at scale 2.0):
 * native Wayland → `screen.height === 2160`, XWayland on the same monitor →
 * `screen.height === 1080`.
 *
 * There is no browser API that reports which convention is in use (UA, screen
 * and media queries are identical), so `screenHeightToCssPixels` infers it from
 * an invariant: a window can never be taller than the screen it sits on.
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
 * Floor for the computed factor. The request was explicitly "only scale up for
 * higher resolutions" — a 768p laptop gets 1.0 rather than being squeezed to
 * 0.71, which would make text too small to read comfortably. Only
 * `computeAutoUIScale` (the auto-fit button) honours this; the manual slider
 * may go lower on purpose.
 */
export const UI_SCALE_MIN = 1

/**
 * Granularity of the manual scale slider, in factor units (5%).
 *
 * Both the auto-fit result and any stored value are snapped to this grid so
 * they are always REPRESENTABLE on that slider. A `<input type=range>`
 * silently snaps an off-grid value to the nearest step: 1.33 (1440p) lands on
 * the 1.35 step, so the thumb would sit at 135% while the label read "133%" —
 * the two disagreeing is exactly the bug this prevents. Keeping the step here
 * (and using it for the slider's own `step` in settingsFieldMap) means the
 * grid has a single source of truth.
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
 * The factor actually in effect: the stored value, clamped to sane bounds.
 *
 * There is deliberately no screen input. The scale is a plain stored value
 * now — the settings "auto fit" button computes one ONCE from the screen and
 * writes it to `uiScale` (see `autoFitUIScale`). Deriving it from the screen
 * on every startup/applier call is what used to make the UI jump, so this
 * function must stay screen-independent; narrowing the signature is the guard
 * that stops the dependency from creeping back in.
 *
 * Kept pure so both the settings UI (which displays the effective value) and
 * the applier (which applies it) derive it the same way and cannot drift.
 */
export function resolveUIScale(manual: number): number {
  const n = Number(manual)
  if (!Number.isFinite(n) || n <= 0) return 1
  // Snap so a value stored off the grid (e.g. a legacy 0.82) is not displayed
  // as 82% while the slider renders its thumb on the 0.8 step.
  const bounded = Math.min(Math.max(n, 0.5), UI_SCALE_MAX)
  return Math.min(Math.max(snapToScaleStep(bounded), 0.5), UI_SCALE_MAX)
}

/**
 * Convert a reported screen height into CSS pixels.
 *
 * `rawHeight` is `window.screen.height`. On Windows, macOS and X11/XWayland it
 * is already CSS pixels and is returned unchanged. On native Wayland Chromium
 * reports device pixels, so it is divided by `dpr`.
 *
 * No browser API says which convention is in effect, so the convention is
 * inferred from an invariant: a window can never be taller than the screen it
 * sits on. `windowDeviceHeight` is the window's own height converted to device
 * pixels (`outerHeight * dpr`). If that already exceeds `rawHeight`, then
 * `rawHeight` cannot be device pixels — it is the CSS-pixel convention, and
 * dividing would under-scale. Only when the window fits inside the reported
 * height is `rawHeight` treated as device pixels.
 *
 * Scoped to Linux desktop because `dpr` means different things elsewhere:
 * on macOS/Windows it is the panel density (a Retina 5K reports 1440 CSS
 * pixels at dpr 2 and must keep factor 1.35), not a user-chosen scale factor.
 *
 * The inference is genuinely ambiguous for a SMALL window (a 400px-tall window
 * fits inside both a 1080-device and a 1080-CSS screen), so it can only be
 * wrong in the harmless direction: treating an X11 CSS height as device pixels
 * halves it, which at worst under-scales a screen taller than 2160 logical
 * pixels. The opposite mistake is the 4× UI this function exists to prevent,
 * so an unmeasurable window (0/NaN) also divides rather than keeps.
 */
export function screenHeightToCssPixels(
  rawHeight: number,
  dpr: number,
  windowDeviceHeight: number,
  linuxDesktop: boolean,
): number {
  if (!linuxDesktop) return rawHeight
  if (!Number.isFinite(dpr) || dpr <= 1) return rawHeight
  if (!Number.isFinite(rawHeight) || rawHeight <= 0) return rawHeight
  // Unmeasurable window: prefer the device-pixel reading. On Linux a dpr > 1
  // means the OS already scaled, so dividing is the safer of the two errors.
  if (!Number.isFinite(windowDeviceHeight) || windowDeviceHeight <= 0) return rawHeight / dpr
  // The window fits inside the reported height only if that height is device
  // pixels; a CSS-pixel screen of the same size would be exceeded by the
  // window's own device height.
  if (windowDeviceHeight > rawHeight) return rawHeight
  return rawHeight / dpr
}

/** True on a Linux desktop browser. Excludes Android, whose UA also says Linux. */
function isLinuxDesktopBrowser(): boolean {
  try {
    const ua = navigator.userAgent || ''
    return /Linux/i.test(ua) && !/Android/i.test(ua)
  } catch {
    return false
  }
}

/**
 * Current screen height in CSS pixels, or 0 when unavailable (SSR/tests).
 * Wrapped so callers do not each need the try/undefined dance.
 *
 * The value is normalized by `screenHeightToCssPixels` so native Wayland's
 * device-pixel reading does not double up with the OS scale factor.
 */
export function currentScreenHeight(): number {
  try {
    const raw = Number(window.screen?.height) || 0
    const dpr = Number(window.devicePixelRatio) || 1
    const outer = Number(window.outerHeight) || 0
    const inner = Number(window.innerHeight) || 0
    const windowDeviceHeight = Math.max(outer, inner) * dpr
    return screenHeightToCssPixels(raw, dpr, windowDeviceHeight, isLinuxDesktopBrowser())
  } catch {
    return 0
  }
}
