/**
 * "丝雾星轨" — the PS3 XMB silk-haze style, ported from
 * `test/ps3-wave/index.html`.
 *
 * One soft band drawn as 38 extremely faint filaments offset along the normal
 * (dense at the centre, sparse at the edges), plus a forked sub-band and a
 * starfield. The filaments are what give it the silk/smoke texture — a single
 * wide stroke reads as a blurry smear instead.
 *
 * Parameter keys mirror the prototype's `ui` object so the two stay comparable.
 */

import {
  BLACK,
  TAU,
  WHITE,
  luminance,
  makeRng,
  mixRgb,
  noise2,
  parseAccentColor,
  parseBackgroundColor,
  rgba,
} from '../canvasMath'
import type { AnimatedStyle, FrameContext, ParamSpec, ParamValue } from './types'

/** Sampling step, in BACKING pixels (not CSS px). */
const STEP_BACKING = 1.5

/**
 * How many filaments the main band is built from. More is finer and costlier.
 *
 * The per-filament alpha is budgeted against this number (see FIL_A), so
 * changing it without re-tuning FIL_A changes the band's overall brightness.
 */
const FILAMENTS = 38

/**
 * Base alpha of a single filament.
 *
 * Budget: the centre should accumulate to ≈0.30 (the reference frames measure
 * the band at ~+70/230 over the background). 38 filaments, bell mean ~0.5,
 * brightness jitter mean ~0.72 →
 *   total ≈ 38 · FIL_A · 0.5 · 0.72 = 13.7·FIL_A  ⇒  FIL_A ≈ 0.022
 */
const FIL_A = 0.022

/**
 * Band definitions.
 *
 * Centre line: y = base + amp·e^(−decay·u), plus two low-frequency sines at
 * different frequencies AND different phase speeds — the beat between them is
 * what makes the shape flow and morph rather than rigidly translate.
 *
 * The second band subtracts a Gaussian bump, so the middle of the main band
 * splits into a forked sub-band (matching the reference frames).
 */
interface SilkBand {
  base: number
  amp: number
  decay: number
  wob: number
  wobF: number
  wobSpeed: number
  wob2: number
  wobF2: number
  wobSpeed2: number
  half: number
  envDecay: number
  forkAmp: number
  forkC: number
  forkW: number
  filaments: number
  speed: number
}

const BANDS: readonly SilkBand[] = [
  {
    base: 0.355, amp: 0.20, decay: 1.5,
    wob: 0.048, wobF: 1.25, wobSpeed: 0.55,
    wob2: 0.030, wobF2: 2.30, wobSpeed2: 0.90,
    half: 0.030, envDecay: 1.05,
    forkAmp: 0, forkC: 0, forkW: 1,
    filaments: FILAMENTS, speed: 0.55,
  },
  {
    base: 0.355, amp: 0.20, decay: 1.5,
    wob: 0.038, wobF: 1.60, wobSpeed: 0.42,
    wob2: 0.024, wobF2: 2.75, wobSpeed2: 0.72,
    half: 0.022, envDecay: 1.55,
    forkAmp: 0.105, forkC: 0.56, forkW: 0.24,
    filaments: Math.round(FILAMENTS * 0.7), speed: 0.42,
  },
]

const PARAMS: ParamSpec[] = [
  { kind: 'slider', key: 'thick', labelKey: 'settings.items.wallpaperStyleSilkThick', min: 30, max: 260, step: 5, defaultValue: 100, format: 'multiplier' },
  { kind: 'slider', key: 'amp', labelKey: 'settings.items.wallpaperStyleSilkAmp', min: 0, max: 220, step: 5, defaultValue: 100, format: 'multiplier' },
  { kind: 'slider', key: 'lam', labelKey: 'settings.items.wallpaperStyleSilkLam', min: 40, max: 260, step: 5, defaultValue: 100, format: 'multiplier' },
  { kind: 'slider', key: 'haze', labelKey: 'settings.items.wallpaperStyleSilkHaze', min: 0, max: 220, step: 5, defaultValue: 100, format: 'multiplier' },
  { kind: 'slider', key: 'fil', labelKey: 'settings.items.wallpaperStyleSilkFil', min: 0, max: 220, step: 5, defaultValue: 100, format: 'multiplier' },
  { kind: 'slider', key: 'spread', labelKey: 'settings.items.wallpaperStyleSilkSpread', min: 40, max: 200, step: 5, defaultValue: 100, format: 'multiplier' },
  { kind: 'slider', key: 'gain', labelKey: 'settings.items.wallpaperStyleSilkGain', min: 20, max: 200, step: 5, defaultValue: 100, format: 'multiplier' },
  { kind: 'slider', key: 'density', labelKey: 'settings.items.wallpaperStyleSilkDensity', min: 0, max: 250, step: 5, defaultValue: 100, format: 'multiplier' },
  { kind: 'slider', key: 'psize', labelKey: 'settings.items.wallpaperStyleSilkPsize', min: 30, max: 250, step: 5, defaultValue: 100, format: 'multiplier' },
  { kind: 'slider', key: 'pglow', labelKey: 'settings.items.wallpaperStyleSilkPglow', min: 0, max: 250, step: 5, defaultValue: 100, format: 'multiplier' },
]

/** Slider value → multiplier around the 100 baseline. */
function scale(v: ParamValue | undefined): number {
  return typeof v === 'number' ? v / 100 : 1
}

/**
 * Palette. The haze/wisp/star colours are near-white tints of the accent, so
 * they read as pale blue smoke over the stage rather than a hard white core.
 *
 * Unlike the XMB style these do NOT flip with the theme's light/dark character:
 * the reference look is an additive glow (`globalCompositeOperation:'lighter'`),
 * which is only meaningful on a dark stage. On a light theme the stage is pulled
 * dark enough by the accent blend for the glow to stay readable — see the
 * `stageBottom` mix below, which is stronger than XMB's.
 */
function readPalette() {
  const cs = getComputedStyle(document.documentElement)
  const accent = parseAccentColor(cs.getPropertyValue('--accent-color'))
  const bg = parseBackgroundColor(cs.getPropertyValue('--bg-primary'))

  // The additive glow needs a stage that is meaningfully darker than the haze.
  // Blend the accent in, then pull toward black so even a near-white page theme
  // yields a dark stage (a light stage + 'lighter' compositing = a white blob).
  const stageBottom = mixRgb(mixRgb(bg, accent, 0.30), BLACK, luminance(bg) > 0.5 ? 0.62 : 0.10)
  const stageTop = mixRgb(stageBottom, BLACK, 0.42)

  return {
    stageTop,
    stageBottom,
    haze: mixRgb(accent, WHITE, 0.85),
    wisp: mixRgb(accent, WHITE, 0.93),
    star: mixRgb(accent, WHITE, 0.90),
  }
}

/** One star. Position is re-derived per frame so follow-stars track the band. */
interface Star {
  kind: 'follow' | 'scatter' | 'cluster'
  u: number
  off: number
  y: number
  size: number
  alpha: number
  drift: number
  tw: number
  twS: number
}

/**
 * Build the starfield once, deterministically.
 *
 * Three groups: stars hugging the band, a wide scatter for depth, and a bright
 * cluster at the band's left "source". A seeded PRNG keeps the layout identical
 * across reloads and theme changes — a fresh random field each mount would make
 * the wallpaper visibly reshuffle.
 */
function buildStars(): Star[] {
  const rnd = makeRng(987654321)
  const stars: Star[] = []
  const s = (kind: Star['kind'], u: number, off: number, y: number, size: number, alpha: number, drift: number): Star =>
    ({ kind, u, off, y, size, alpha, drift, tw: rnd() * TAU, twS: 0.5 + rnd() * 1.8 })

  for (let i = 0; i < 170; i++) {
    stars.push(s('follow', rnd(), (rnd() * 2 - 1) * (rnd() * 2 - 1) * 3.0, 0,
      0.5 + rnd() * rnd() * 2.0, 0.30 + rnd() * 0.70, (rnd() * 2 - 1) * 0.010))
  }
  for (let i = 0; i < 110; i++) {
    stars.push(s('scatter', rnd(), 0, 0.06 + rnd() * 0.86,
      0.4 + rnd() * rnd() * 1.8, 0.20 + rnd() * 0.65, (rnd() * 2 - 1) * 0.006))
  }
  for (let i = 0; i < 46; i++) {
    stars.push(s('cluster', 0.012 + rnd() * rnd() * 0.10, (rnd() * 2 - 1) * (rnd() * 2 - 1) * 2.2, 0,
      0.5 + rnd() * rnd() * 2.4, 0.45 + rnd() * 0.55, (rnd() * 2 - 1) * 0.004))
  }
  return stars
}

const STARS = buildStars()

/** Band centre line at normalised x, as a fraction of height. */
function bandY(band: SilkBand, bi: number, u: number, t: number, ampScale: number, lamScale: number): number {
  const uu = Math.min(1, Math.max(0, u / lamScale))
  const phase = TAU * t * 0.11 * band.speed
  let y = band.base + band.amp * Math.exp(-band.decay * uu)
  // Two sines at different frequencies AND phase speeds → the beat makes the
  // shape flow/morph. A single phase offset would only translate it.
  y += band.wob * Math.sin(TAU * band.wobF * uu + phase)
  y += band.wob2 * Math.sin(TAU * band.wobF2 * uu + phase * band.wobSpeed2)
  y += (noise2(u * 1.5 + bi * 9.1, t * 0.02 + bi * 3.3) * 2 - 1) * 0.014

  if (band.forkAmp > 0) {
    const g = Math.exp(-(((u - band.forkC) / band.forkW) ** 2))
    y -= band.forkAmp * ampScale * g
  }
  return band.base + (y - band.base) * ampScale
}

/** Centre-line slope, for normal-offset thickness (perpendicular to the band). */
function bandSlope(band: SilkBand, bi: number, u: number, t: number, ampScale: number, lamScale: number): number {
  const d = 0.004
  const lo = Math.max(0, u - d), hi = Math.min(1, u + d)
  const y0 = bandY(band, bi, lo, t, ampScale, lamScale)
  const y1 = bandY(band, bi, hi, t, ampScale, lamScale)
  return (y1 - y0) / (hi - lo)
}

/** Along-track envelope: brighter at the left "source", fading right. */
function envAt(band: SilkBand, u: number): number {
  return 0.58 + 0.42 * Math.exp(-band.envDecay * u)
}

interface Path {
  pts: [number, number][]
  norm: [number, number][]
}

function buildPath(band: SilkBand, bi: number, t: number, cssW: number, cssH: number, scaleBacking: number, ampScale: number, lamScale: number): Path {
  const stepCss = STEP_BACKING / scaleBacking
  const pts: [number, number][] = []
  const norm: [number, number][] = []
  const push = (x: number, u: number) => {
    const s = bandSlope(band, bi, u, t, ampScale, lamScale)
    const len = Math.hypot(1, s)
    pts.push([x, bandY(band, bi, u, t, ampScale, lamScale) * cssH])
    norm.push([-s / len, 1 / len])
  }
  let x = 0
  for (; x <= cssW; x += stepCss) push(x, x / cssW)
  if (pts.length === 0 || pts[pts.length - 1][0] < cssW) push(cssW, 1)
  return { pts, norm }
}

/** Stroke a path offset along its normal, with an optional per-sample extra offset. */
function strokePath(
  ctx: CanvasRenderingContext2D,
  path: Path,
  off: number,
  extraFn?: (i: number) => number,
): void {
  ctx.beginPath()
  for (let i = 0; i < path.pts.length; i++) {
    const [nx, ny] = path.norm[i]
    const e = extraFn ? extraFn(i) : 0
    const x = path.pts[i][0] + nx * (off + e)
    const y = path.pts[i][1] + ny * (off + e)
    if (i === 0) ctx.moveTo(x, y)
    else ctx.lineTo(x, y)
  }
  ctx.stroke()
}

export const silkStyle: AnimatedStyle = {
  id: 'silk',
  labelKey: 'settings.items.wallpaperStyleSilk',
  params: PARAMS,
  // The prototype's own bounds (0.25–2.5x), unlike XMB's shipped 0.2–2.0x.
  speedRange: [0.25, 2.5],

  draw(frame: FrameContext) {
    const { ctx, cssW, cssH, scale: backingScale, time, speed, params } = frame
    // Guard before any arithmetic: cssW = 0 makes u = x/cssW NaN, and
    // createLinearGradient throws on a non-finite stop, aborting the frame.
    if (cssW <= 0 || cssH <= 0) return

    const pal = readPalette()
    const t = time * speed
    const gain = scale(params.gain)
    const thickScale = scale(params.thick)
    const hazeAmt = scale(params.haze)
    const filAmt = scale(params.fil)
    const spreadAmt = scale(params.spread)
    const ampScale = scale(params.amp)
    const lamScale = scale(params.lam)

    // ① Stage: vertical gradient, darker at the top.
    const bg = ctx.createLinearGradient(0, 0, 0, cssH)
    bg.addColorStop(0, rgba(pal.stageTop, 1))
    bg.addColorStop(0.55, rgba(mixRgb(pal.stageTop, pal.stageBottom, 0.35), 1))
    bg.addColorStop(1, rgba(pal.stageBottom, 1))
    ctx.fillStyle = bg
    ctx.fillRect(0, 0, cssW, cssH)

    ctx.save()
    // Additive: overlapping filaments accumulate into a glow instead of the last
    // one painting over the others.
    ctx.globalCompositeOperation = 'lighter'
    ctx.lineJoin = 'round'
    ctx.lineCap = 'round'

    for (let bi = 0; bi < BANDS.length; bi++) {
      const band = BANDS[bi]
      const path = buildPath(band, bi, t, cssW, cssH, backingScale, ampScale, lamScale)

      // Canvas silently drops a path containing NaN — no throw, no warning — so a
      // malformed band presents as "the band vanished" with a clean console.
      let bad = false
      for (const p of path.pts) if (!Number.isFinite(p[1])) { bad = true; break }
      if (bad) continue

      const half = band.half * cssH * thickScale * spreadAmt

      // ② Wide soft underlay: a few strokes far wider than the filaments, to lay
      //    down a cloud before the texture.
      for (const [wf, a0] of [[9.0, 0.016], [4.6, 0.028], [2.4, 0.042]] as const) {
        ctx.strokeStyle = rgba(pal.haze, a0 * hazeAmt * gain)
        ctx.lineWidth = half * wf
        strokePath(ctx, path, 0)
      }

      // ③ Filaments: dense at the centre, sparse at the edges. Per-filament
      //    brightness/width jitter is what reads as silk rather than a blur.
      const nF = band.filaments
      for (let f = 0; f < nF; f++) {
        const k = nF > 1 ? (f / (nF - 1)) * 2 - 1 : 0
        const offK = Math.sign(k) * Math.abs(k) ** 1.5
        const bell = Math.cos((k * Math.PI) / 2) ** 2

        const nA = noise2(f * 3.7 + bi * 17.1, 0.5)
        const nW = noise2(f * 5.9 + bi * 23.3, 1.7)
        const bright = 0.55 + 0.45 * nA * (0.55 + 0.9 * filAmt)
        const wCss = 0.6 + 1.2 * nW

        const ph = f * 0.9 + bi * 2.0
        const wobA = half * 0.11
        const wobF = 1.0 + 0.8 * nW

        const a = FIL_A * hazeAmt * gain * bell * Math.min(1.6, bright)
        if (a < 0.002) continue

        const env = envAt(band, 0.5)
        ctx.strokeStyle = rgba(bright > 1.05 ? pal.wisp : pal.haze, Math.min(1, a * env))
        ctx.lineWidth = wCss
        strokePath(ctx, path, offK * half, (i) => {
          const u = path.pts[i][0] / cssW
          return Math.sin(TAU * (u * wobF + ph) + t * 0.35) * wobA
        })
      }
    }

    // ④ Starfield.
    const dens = scale(params.density)
    if (dens > 0.01) {
      const sizeScale = scale(params.psize)
      const glowScale = scale(params.pglow)
      const main = BANDS[0]
      const path = buildPath(main, 0, t, cssW, cssH, backingScale, ampScale, lamScale)
      const half = main.half * cssH * thickScale * spreadAmt
      const stepCss = STEP_BACKING / backingScale

      // The follow/cluster stars are positioned FROM this path, so it needs the
      // same finite check the band loop does: canvas silently drops a NaN path,
      // and a NaN centre would make every attached star disappear too.
      let pathOk = true
      for (const p of path.pts) if (!Number.isFinite(p[1])) { pathOk = false; break }

      for (const s of STARS) {
        const u = (s.u + t * s.drift + 1) % 1
        let cx: number, cy: number
        if (s.kind === 'scatter' || !pathOk) {
          // Scatter stars do not depend on the band, so they keep rendering even
          // if the band's geometry is unusable.
          cx = u * cssW
          cy = (s.kind === 'scatter' ? s.y : 0.5) * cssH
        } else {
          const idx = Math.min(path.pts.length - 1, Math.max(0, Math.round((u * cssW) / stepCss)))
          const [nx, ny] = path.norm[idx]
          cx = path.pts[idx][0] + nx * s.off * half
          cy = path.pts[idx][1] + ny * s.off * half
        }

        const tw = 0.65 + 0.35 * Math.sin(t * s.twS + s.tw)
        const a = Math.min(1, s.alpha * tw * gain * dens)
        const r = Math.max(0.35, s.size * sizeScale)

        // Belt-and-braces: a non-finite centre or radius makes canvas silently
        // skip the arc, which would look like randomly missing stars.
        if (!Number.isFinite(cx) || !Number.isFinite(cy) || !Number.isFinite(r)) continue

        if (glowScale > 0.01 && r > 1.1) {
          const gr = r * (2.6 + 1.6 * glowScale)
          const g = ctx.createRadialGradient(cx, cy, 0, cx, cy, gr)
          g.addColorStop(0, rgba(pal.star, 0.36 * a * glowScale))
          g.addColorStop(1, rgba(pal.star, 0))
          ctx.fillStyle = g
          ctx.beginPath()
          ctx.arc(cx, cy, gr, 0, TAU)
          ctx.fill()
        }
        ctx.fillStyle = rgba(pal.star, Math.min(1, a))
        ctx.beginPath()
        ctx.arc(cx, cy, r, 0, TAU)
        ctx.fill()
      }
    }

    ctx.restore()
  },
}
