/**
 * XMB wave style: pure shape math and palette derivation.
 *
 * Kept separate from the rendering component so the wave shape, the speed
 * mapping and the palette can be unit-tested without a canvas. jsdom has no 2D
 * context, so anything that needs one is untestable there — the split is what
 * makes these functions verifiable.
 *
 * The style-agnostic primitives (hex parsing, luminance, `mixRgb`, `rgba`,
 * `noise2`, `TAU`) now live in `canvasMath.ts` and are re-exported here so
 * existing importers keep working unchanged.
 *
 * Nothing in this module touches the DOM.
 */

import {
  BLACK,
  TAU,
  WHITE,
  luminance,
  mixRgb,
  noise2,
  parseAccentColor,
  parseBackgroundColor,
  parseHexColorOr,
  type Rgb,
} from './canvasMath'

// Re-exported for backward compatibility: these used to be defined here and are
// imported from this module across the codebase and its tests.
export {
  BLACK,
  WHITE,
  luminance,
  mixRgb,
  noise2,
  parseAccentColor,
  parseBackgroundColor,
  parseHexColor,
  parseHexColorOr,
  rgba,
  TAU,
  type Rgb,
} from './canvasMath'

/** The four colours a wave frame is drawn from. */
export interface WavePalette {
  /** Background gradient top (darker). */
  bgTop: Rgb
  /** Background gradient bottom (lighter). */
  bgBottom: Rgb
  /** Wave crest highlight — the brightest colour, which is what reads as a crisp edge. */
  waveRim: Rgb
  /** Wave body, fading downward from the crest. */
  waveBody: Rgb
}

/**
 * Derive the wave palette from the theme's accent + background.
 *
 * Explicit overrides win when provided (a theme may ship a hand-tuned wave
 * palette); otherwise the colours are derived so any theme gets a coherent
 * wave without extra configuration.
 *
 * ── Why the crest is pushed away from the stage, not always toward white ──
 * The wave paints its OWN opaque stage gradient (see animatedWallpapers/xmb.ts),
 * so what the bands must contrast against is that stage — not the page. Pushing
 * both toward WHITE (the original) makes the crest a lighter tint of the stage
 * on every theme: on a dark theme that reads as a glow, but on a light theme the
 * stage is already near-white, so the wave becomes a barely-visible ghost.
 *
 * Measured on the real rendered frame across all 36 themes (front band crest vs
 * the stage under it): light themes sat at 2.37–2.54, dark at 3.15–3.37. Pushing
 * the crest AWAY from the stage lifts every theme and hurts none —
 * light 2.78–2.99 (+0.23..+0.62), dark 3.56–3.97 (+0.41..+0.60).
 *
 * The consequence is deliberate: on light themes the wave is a DARK silhouette
 * rather than a bright glow. That is forced by physics — you cannot draw
 * anything brighter than a near-white stage.
 */
export function buildWavePalette(
  accent: string | null | undefined,
  bg: string | null | undefined,
  override?: Partial<Record<'top' | 'bottom' | 'rim' | 'body', string>>,
): WavePalette {
  const accentRgb = parseAccentColor(accent)
  const bgRgb = parseBackgroundColor(bg)

  const derivedBottom = mixRgb(mixRgb(bgRgb, accentRgb, 0.38), WHITE, 0.16)
  // Contrast direction is decided from the STAGE the bands sit on, since that is
  // what they are actually drawn over (the page behind it is fully covered).
  const bandTarget = luminance(derivedBottom) < 0.5 ? WHITE : BLACK
  const derived: WavePalette = {
    bgTop: mixRgb(bgRgb, BLACK, 0.28),
    bgBottom: derivedBottom,
    waveRim: mixRgb(derivedBottom, bandTarget, 0.55),
    waveBody: mixRgb(derivedBottom, accentRgb, 0.20),
  }

  // Each override is applied independently: a theme can pin just the crest
  // colour without having to restate the rest.
  return {
    bgTop: parseHexColorOr(override?.top, derived.bgTop),
    bgBottom: parseHexColorOr(override?.bottom, derived.bgBottom),
    waveRim: parseHexColorOr(override?.rim, derived.waveRim),
    waveBody: parseHexColorOr(override?.body, derived.waveBody),
  }
}

// ── Speed ────────────────────────────────────────────────────────────────────

/** Slider midpoint: the value that maps to 1x. */
export const WAVE_SPEED_MID = 50
/** Slider range, mirrored by the speed control (10–100). */
export const WAVE_SPEED_MIN = 10
export const WAVE_SPEED_MAX = 100
/**
 * Time-scale bounds, matching the slider's own range exactly (10/50 = 0.2x,
 * 100/50 = 2x). Wider bounds would make part of the slider dead (several
 * positions clamping to the same value); narrower ones would leave the extremes
 * unreachable. These exist only to contain a corrupt persisted value.
 */
export const WAVE_TIME_SCALE_MIN = WAVE_SPEED_MIN / WAVE_SPEED_MID
export const WAVE_TIME_SCALE_MAX = WAVE_SPEED_MAX / WAVE_SPEED_MID

/**
 * Map the 10–100 speed slider to a time-scale multiplier.
 *
 * The slider is a percentage around 50 = 1x. Clamped so a persisted outlier
 * (hand-edited localStorage, a legacy value) cannot make the wave appear frozen
 * or jitter.
 */
export function waveTimeScale(sliderValue: number): number {
  const v = Number.isFinite(sliderValue) ? sliderValue : WAVE_SPEED_MID
  const raw = v / WAVE_SPEED_MID
  return Math.min(WAVE_TIME_SCALE_MAX, Math.max(WAVE_TIME_SCALE_MIN, raw))
}

// ── Wave shape ───────────────────────────────────────────────────────────────

/**
 * One wave band.
 *
 * The centre line is:
 *
 *   y(x) = base + amp * h(x) + tilt * (u - 0.5)
 *
 * where `u = x / width`. `base`/`amp`/`tilt` are fractions of the canvas
 * height, so a layer definition is resolution-independent.
 *
 * `base` is required: omitting it yields NaN, and **canvas silently discards a
 * path with NaN coordinates** (no throw), which presents as "the wave vanished"
 * with a clean console. renderWave() guards against this.
 */
export interface WaveLayer {
  /** Centre-line height as a fraction of canvas height. Required. */
  base: number
  /** Wavelength in page-widths: 2.4 means one full period spans 2.4 screens. */
  lam: number
  /** Amplitude as a fraction of canvas height. */
  amp: number
  /** How far below the crest the fill fades out, as a fraction of height. */
  band: number
  /** Linear tilt across the width; negative rises to the right. */
  tilt: number
  /** Overall opacity of this band. */
  alpha: number
  /** Crest stroke strength — the main contributor to edge crispness. */
  edge: number
  /** Phase offset, so bands do not line up. */
  phase0: number
  /** Phase-warp strength (radians): makes the slope vary along x. */
  warp: number
  /** Second-harmonic amount: sharper crests, flatter troughs. */
  harm: number
  /** Amplitude-envelope strength: local fat/thin variation. */
  env: number
  /** Horizontal drift speed (periods/second). */
  travel: number
  /** Shape-evolution speed — what makes it morph rather than just translate. */
  evolve: number
}

/**
 * The three visible bands, back to front.
 *
 * Deliberately only three: the brief is "about two or three waves at once".
 * `lam` sits near 2–2.7 page-widths so a single screen shows roughly half a
 * period — one long swell rather than a field of ripples.
 */
export const WAVE_LAYERS: readonly WaveLayer[] = [
  { base: 0.34, lam: 2.70, amp: 0.090, band: 0.42, tilt: -0.10, alpha: 0.34, edge: 0.24, phase0: 0.0, warp: 0.50, harm: 0.10, env: 0.20, travel: 0.024, evolve: 0.026 },
  { base: 0.55, lam: 2.40, amp: 0.110, band: 0.50, tilt: -0.14, alpha: 0.52, edge: 0.34, phase0: 1.1, warp: 0.66, harm: 0.14, env: 0.24, travel: 0.032, evolve: 0.034 },
  { base: 0.76, lam: 2.10, amp: 0.130, band: 0.58, tilt: -0.18, alpha: 0.72, edge: 0.40, phase0: 2.2, warp: 0.82, harm: 0.18, env: 0.28, travel: 0.042, evolve: 0.044 },
]

/** Knobs shared by every band in a frame. */
export interface WaveParams {
  /** Current animation time in seconds. */
  time: number
  /**
   * Wavelength multiplier from the slider (1 = the layer's own `lam`).
   * Multiplies, so a larger value means a *longer* wavelength and therefore
   * fewer visible periods — matching the "wavelength" label. (Dividing here
   * inverts the control: the slider would shorten the wave as you turn it up.)
   */
  lamScale?: number
  /** Amplitude multiplier. */
  ampScale?: number
  /** Irregularity multiplier; 0 gives a pure sine. */
  irregularity?: number
  /** Tilt multiplier. */
  tiltScale?: number
}

/**
 * Centre-line height for one band at normalised x, as a fraction of height.
 *
 * The base is a very low-frequency sine; three small perturbations remove the
 * mechanical regularity:
 *   1. phase warp   — the slope varies slowly along x, so curvature is not constant
 *   2. second harmonic — crests slightly sharper than troughs (real water is asymmetric)
 *   3. amplitude envelope — some stretches fuller, others thinner
 * The perturbation field advances along its own axis (`evolve`), so the shape
 * genuinely morphs over time instead of sliding rigidly.
 *
 * Returns a fraction of canvas height. Multiply by height to get pixels.
 */
export function centerline(u: number, layer: WaveLayer, params: WaveParams): number {
  const {
    time,
    lamScale = 1,
    ampScale = 1,
    irregularity = 1,
    tiltScale = 1,
  } = params

  const y = time * layer.evolve
  const lam = layer.lam * lamScale
  const drift = TAU * time * layer.travel

  let phase = TAU * (u / lam) + drift + layer.phase0
  phase += layer.warp * irregularity * (noise2(u * 1.1 + drift * 0.05, y) * 2 - 1)

  const h = layer.harm * irregularity
  let n = (Math.sin(phase) + h * Math.sin(2 * phase + 1.1)) / (1 + h)
  n *= 1 + layer.env * irregularity * (noise2(u * 0.8 + 4.7 + drift * 0.04, y * 0.6) * 2 - 1)

  const tilt = layer.tilt * tiltScale * (u - 0.5)
  return layer.base + layer.amp * ampScale * n + tilt
}

/** Visible fraction of one period for the front band, given the slider scale. */
export function visibleCycles(lamScale = 1): number {
  const front = WAVE_LAYERS[WAVE_LAYERS.length - 1]
  return 1 / (front.lam * lamScale)
}
