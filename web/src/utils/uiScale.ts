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
 * Granularity of the manual scale slider, in factor units (1%).
 *
 * The whole point of font-driven scaling is that it is continuous: the layout
 * is expressed in rem-derived units, so ANY factor renders correctly — there is
 * no need for a coarse grid to hide rounding. 1% is fine enough to read as
 * stepless while still producing a short, stable, displayable number (a raw
 * pointer position would give values like 1.0344827 that no one wants to see in
 * the label or store in localStorage).
 *
 * Both the auto-fit result and any stored value are snapped to this grid so
 * they are always REPRESENTABLE on that slider. A `<input type=range>`
 * silently snaps an off-grid value to the nearest step, and the thumb would
 * then sit somewhere the label does not say. Keeping the step here (and using
 * it for the slider's own `step` in settingsFieldMap) means the grid has a
 * single source of truth.
 */
export const UI_SCALE_STEP = 0.01

/** Lowest factor the slider offers. Matches the applier's own floor. */
export const UI_SCALE_MANUAL_MIN = 0.5

/** Highest factor the slider offers. Matches the applier's own ceiling. */
export const UI_SCALE_MANUAL_MAX = 2

/** Round to 2 decimals so the applied factor is stable and displayable. */
function round2(n: number): number {
  return Math.round(n * 100) / 100
}

/**
 * Snap a factor to the slider's step grid.
 *
 * Snapped relative to 0, which matches the slider's grid because its minimum
 * (0.5) is itself a whole multiple of the step. `settingsFieldMap.test.ts`
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
  const bounded = Math.min(Math.max(n, UI_SCALE_MANUAL_MIN), UI_SCALE_MAX)
  return Math.min(Math.max(snapToScaleStep(bounded), UI_SCALE_MANUAL_MIN), UI_SCALE_MAX)
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

/**
 * The scale factor currently applied to the document.
 *
 * Reads the `--ui-scale` custom property that `applyUIScale` writes, rather
 * than a CSS `zoom` style. Returns 1 when the property is absent (standalone
 * documents, first paint before the applier runs) or unparseable.
 *
 * Lives in this leaf module (zero imports) rather than in `useSettingsConfig`
 * on purpose: `useWideScreenLayout` needs it, and that module is imported by
 * ~20 components whose tests hand-pick a whitelist mock of
 * `@/composables/useSettingsConfig`. A new import from that module is
 * `undefined` in those mocks and crashes the whole file at import time — the
 * exact failure mode this repo has hit before.
 */
export function readAppliedUIScale(): number {
  try {
    const raw = getComputedStyle(document.documentElement).getPropertyValue('--ui-scale')
    const n = Number(raw)
    return Number.isFinite(n) && n > 0 ? n : 1
  } catch {
    return 1
  }
}

/** The design base the layout was drawn against, in px. `1rem` = this × scale. */
export const UI_BASE_FONT_PX = 15

/**
 * `--font-size-md` in design px — the size single-line form controls use.
 *
 * Deliberately not `UI_BASE_FONT_PX`: the line box that has to be an integer is
 * the control's own, and controls are sized `--font-size-md` (13 design px),
 * not at the document base (15). Pairing with the base would make the line box
 * 15% too tall at every scale.
 */
const INPUT_FONT_DESIGN_PX = 13

/** Line height multiplier for single-line form controls (--input-line-height). */
const INPUT_LINE_HEIGHT_RATIO = 1.4

/**
 * Event broadcast after the applied scale changes.
 *
 * Rem scaling resizes every element but never the viewport, so `resize` does
 * not fire and layout decisions derived from the scale have to be told.
 * Declared here (a leaf module) so both the applier and its consumers can
 * import it without creating an import cycle.
 */
export const UI_SCALE_EVENT = 'clawbench-ui-scale-change'

/**
 * Apply the global UI scale by resizing the root font.
 *
 * This is the ONLY scaling mechanism. The layout is expressed in rem-derived
 * units (`--dp` = `1rem / 15` in variables.css), so changing the root font size
 * resizes text and every box around it together, at any factor.
 *
 * It replaced `html { zoom }`, which was a dead end: CSS zoom does not scale
 * the layout viewport. At zoom 1.5 on a 1000px window, `innerWidth` stayed 1000
 * while `clientWidth` became 667 — so `100vw` drew 1500 visual px (floating
 * cards ran off-screen), `60dvh` grew from 60% to 90% of the screen, media
 * queries still believed there were 1000px, and canvases stretched their
 * backing store. Every consumer then needed a `toFixedCSS()` conversion to
 * translate between the two coordinate spaces. With rem scaling there is one
 * space, so all of that is gone.
 *
 * Also writes `--input-line-height` as a whole number of px: form controls
 * need an integer line box or Android WebView rounds the line box and the
 * content box independently and the caret drifts high (see variables.css).
 */
export function applyUIScale(scale: number): void {
  const el = document.documentElement
  const s = Math.max(UI_SCALE_MANUAL_MIN, Math.min(UI_SCALE_MANUAL_MAX, scale))
  // A pre-rem build may have left `zoom` on <html>; clear it unconditionally,
  // otherwise the two mechanisms compound into a tiny UI.
  el.style.zoom = ''
  el.style.setProperty('--ui-scale', String(s))
  el.style.setProperty(
    '--input-line-height',
    `${Math.round(INPUT_FONT_DESIGN_PX * s * INPUT_LINE_HEIGHT_RATIO)}px`,
  )
  try {
    window.dispatchEvent(new CustomEvent(UI_SCALE_EVENT, { detail: { scale: s } }))
  } catch {
    // No window (SSR / a bare unit test): nothing to notify.
  }
}

