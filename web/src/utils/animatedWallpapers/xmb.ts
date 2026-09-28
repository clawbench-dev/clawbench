/**
 * "XMB 波浪" — the original animated wallpaper, ported from the tuned
 * implementation that already shipped (and from `test/psp-wave/index.html`).
 *
 * Three crisp bands (solid fill + bright crest stroke) over a shallow vertical
 * stage gradient. The shape math lives in `../waveMath.ts`; this module only
 * wires the parameters to it and paints.
 *
 * Parameter keys mirror the prototype's `ui` object so the two stay comparable.
 */

import {
  BLACK,
  WHITE,
  luminance,
  mixRgb,
  parseAccentColor,
  parseBackgroundColor,
  parseHexColorOr,
  rgba,
} from '../canvasMath'
import { WAVE_LAYERS, centerline, type WaveLayer, type WaveParams } from '../waveMath'
import type { AnimatedStyle, FrameContext, ParamSpec, ParamValue } from './types'

/** Sampling step, in BACKING pixels (not CSS px). */
const STEP_BACKING = 1.5

/**
 * Parameter specs. Slider ranges are the prototype's own (a 0–250 scale around
 * a 100 = 1x baseline), so the tuned defaults keep the shipped look exactly.
 */
const PARAMS: ParamSpec[] = [
  { kind: 'slider', key: 'lam', labelKey: 'settings.items.wallpaperStyleXmbLam', min: 50, max: 200, step: 5, defaultValue: 100, format: 'multiplier' },
  { kind: 'slider', key: 'amp', labelKey: 'settings.items.wallpaperStyleXmbAmp', min: 0, max: 250, step: 5, defaultValue: 100, format: 'multiplier' },
  { kind: 'slider', key: 'band', labelKey: 'settings.items.wallpaperStyleXmbBand', min: 20, max: 260, step: 5, defaultValue: 100, format: 'multiplier' },
  { kind: 'slider', key: 'edge', labelKey: 'settings.items.wallpaperStyleXmbEdge', min: 0, max: 250, step: 5, defaultValue: 100, format: 'multiplier' },
  { kind: 'slider', key: 'tilt', labelKey: 'settings.items.wallpaperStyleXmbTilt', min: -250, max: 250, step: 5, defaultValue: 100, format: 'multiplier' },
  { kind: 'slider', key: 'irr', labelKey: 'settings.items.wallpaperStyleXmbIrr', min: 0, max: 250, step: 5, defaultValue: 100, format: 'multiplier' },
  { kind: 'slider', key: 'contrast', labelKey: 'settings.items.wallpaperStyleXmbContrast', min: 0, max: 250, step: 5, defaultValue: 100, format: 'multiplier' },
  // Named `fadeEdges`, NOT `edgeFade`: the image wallpaper already has an
  // "edge fade" (wallpaperEdgeFade, a CSS radial mask). Different mechanism,
  // different setting — the two are mode-exclusive but must not share a key.
  { kind: 'switch', key: 'fadeEdges', labelKey: 'settings.items.wallpaperStyleXmbFadeEdges', descriptionKey: 'settings.items.wallpaperStyleXmbFadeEdgesDesc', defaultValue: true },
]

/**
 * Palette derived the same way the shipped wave does it.
 *
 * The crest is pushed AWAY from the stage gradient rather than always toward
 * white: the wave paints its own opaque stage, so on a light theme "toward
 * white" makes it a barely-visible ghost (measured 2.37 contrast on ayu-light).
 * See `waveMath.buildWavePalette` for the full rationale and measurements.
 */
function readPalette() {
  const cs = getComputedStyle(document.documentElement)
  const accent = parseAccentColor(cs.getPropertyValue('--accent-color'))
  const bg = parseBackgroundColor(cs.getPropertyValue('--bg-primary'))

  const stageBottom = mixRgb(mixRgb(bg, accent, 0.38), WHITE, 0.16)
  const crestTarget = luminance(stageBottom) < 0.5 ? WHITE : BLACK

  return {
    stageBottom,
    rim: parseHexColorOr(cs.getPropertyValue('--wave-band'), mixRgb(stageBottom, crestTarget, 0.55)),
    body: parseHexColorOr(cs.getPropertyValue('--wave-body'), mixRgb(stageBottom, accent, 0.20)),
  }
}

/** Slider value → multiplier around the 100 baseline. */
function scale(v: ParamValue | undefined): number {
  return typeof v === 'number' ? v / 100 : 1
}

export const xmbStyle: AnimatedStyle = {
  id: 'xmb',
  labelKey: 'settings.items.wallpaperStyleXmb',
  params: PARAMS,
  // 0.2–2.0x, matching the shipped wave. Do NOT change to the prototype's
  // 0.25–2.5 without accepting a feel change for existing users.
  speedRange: [0.2, 2.0],

  draw(frame: FrameContext) {
    const { ctx, cssW, cssH, scale: backingScale, time, speed, params } = frame
    // Guard before any arithmetic: cssW = 0 makes u = x/cssW NaN, and
    // createLinearGradient throws on a non-finite stop, aborting the frame.
    if (cssW <= 0 || cssH <= 0) return

    const pal = readPalette()
    const contrast = scale(params.contrast)
    const stepCss = STEP_BACKING / backingScale
    const t = time * speed

    const shape: WaveParams = {
      time: t,
      lamScale: scale(params.lam),
      ampScale: scale(params.amp),
      irregularity: scale(params.irr),
      tiltScale: scale(params.tilt),
    }
    const bandScale = scale(params.band)
    const edgeScale = scale(params.edge)

    // ① Stage: a shallow vertical gradient the bands sit on.
    const bg = ctx.createLinearGradient(0, 0, 0, cssH)
    bg.addColorStop(0, rgba(mixRgb(pal.stageBottom, BLACK, 0.34 * contrast), 1))
    bg.addColorStop(1, rgba(pal.stageBottom, 1))
    ctx.fillStyle = bg
    ctx.fillRect(0, 0, cssW, cssH)

    // ② Bands, back to front. Each is a solid fill plus a bright crest stroke.
    for (let li = 0; li < WAVE_LAYERS.length; li++) {
      const layer: WaveLayer = WAVE_LAYERS[li]

      const ys: [number, number][] = []
      for (let x = 0; x <= cssW; x += stepCss) {
        ys.push([x, centerline(x / cssW, layer, shape) * cssH])
      }
      if (ys.length === 0 || ys[ys.length - 1][0] < cssW) {
        ys.push([cssW, centerline(1, layer, shape) * cssH])
      }

      // Canvas silently drops a path containing NaN — no throw, no warning — so a
      // malformed layer presents as "the wave vanished" with a clean console.
      let bad = false
      for (const p of ys) if (!Number.isFinite(p[1])) { bad = true; break }
      if (bad) continue

      const a = Math.min(0.95, layer.alpha * contrast)

      // Anchor the fade to the layer's HIGHEST crest, not a per-column value, so
      // the band's tint stays consistent instead of banding along x.
      let yPeak = Infinity
      for (const p of ys) if (p[1] < yPeak) yPeak = p[1]
      const fadeEnd = yPeak + layer.band * cssH * bandScale

      const grad = ctx.createLinearGradient(0, yPeak, 0, fadeEnd)
      grad.addColorStop(0, rgba(pal.rim, a))
      grad.addColorStop(0.02, rgba(pal.rim, a * 0.88))
      grad.addColorStop(0.28, rgba(pal.body, a * 0.50))
      grad.addColorStop(1, rgba(pal.body, 0))

      ctx.beginPath()
      ctx.moveTo(ys[0][0], ys[0][1])
      for (let j = 1; j < ys.length; j++) ctx.lineTo(ys[j][0], ys[j][1])
      ctx.lineTo(cssW, fadeEnd)
      ctx.lineTo(0, fadeEnd)
      ctx.closePath()
      ctx.fillStyle = grad
      ctx.fill()

      // ③ Crest stroke — the crispness comes from this, not from blur.
      const edgeA = Math.min(1, a * 1.25) * (layer.edge * edgeScale)
      if (edgeA > 0.01) {
        ctx.beginPath()
        for (let j = 0; j < ys.length; j++) {
          if (j === 0) ctx.moveTo(ys[j][0], ys[j][1])
          else ctx.lineTo(ys[j][0], ys[j][1])
        }
        ctx.strokeStyle = rgba(pal.rim, Math.min(1, edgeA))
        ctx.lineWidth = 1.5
        ctx.lineJoin = 'round'
        ctx.lineCap = 'round'
        ctx.stroke()
      }
    }

    // ④ Optional left/right edge fade, over the whole frame.
    applyEdgeFade(ctx, cssW, cssH, params.fadeEdges !== false)
  },
}

/**
 * The left/right edge fade, as a separate pass over the whole frame.
 *
 * A style-level toggle rather than a slider, and deliberately owned by the style
 * (not the shared component) because a style that fills its frame differently
 * may not want it. Named `fadeEdges` to avoid colliding with the IMAGE
 * wallpaper's "edge fade" (`wallpaperEdgeFade`), which is a CSS radial mask —
 * different mechanism, different setting, mode-exclusive.
 */
function applyEdgeFade(ctx: CanvasRenderingContext2D, cssW: number, cssH: number, enabled: boolean): void {
  if (!enabled) return
  ctx.globalCompositeOperation = 'destination-in'
  const mask = ctx.createLinearGradient(0, 0, cssW, 0)
  mask.addColorStop(0, 'rgba(0,0,0,0)')
  mask.addColorStop(0.06, 'rgba(0,0,0,1)')
  mask.addColorStop(0.94, 'rgba(0,0,0,1)')
  mask.addColorStop(1, 'rgba(0,0,0,0)')
  ctx.fillStyle = mask
  ctx.fillRect(0, 0, cssW, cssH)
  ctx.globalCompositeOperation = 'source-over'
}
