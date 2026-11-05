import { describe, it, expect } from 'vitest'
import { existsSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'

/**
 * Guard: the annotation overlay must scale its stroke the way viewers do.
 *
 * The overlay document (`assets/annotate.html`) is a standalone page with an
 * inline script, so it cannot import the renderer's `LiveInkLayer`. Its line
 * width used to be `width * dpr` — a fixed 4 CSS pixels — while every viewer
 * (and the in-app drawing window) scales by `canvas.width / 800`. On a
 * 1920-wide display the host therefore drew a 4px line and the audience saw a
 * 9.6px one: the same stroke had two thicknesses depending on which end you
 * looked at.
 *
 * The two implementations are now required to share the reference width, and
 * the overlay is required to derive its line width from the canvas size rather
 * than from dpr alone.
 */

function findFile(candidates: string[]): string {
  for (const p of candidates) {
    if (existsSync(p)) return p
  }
  throw new Error(`none of these exist (cwd=${process.cwd()}):\n${candidates.join('\n')}`)
}

const overlayPath = findFile([
  resolve(process.cwd(), 'desktop/assets/annotate.html'),
  resolve(process.cwd(), 'assets/annotate.html'),
])
const inkLayerPath = findFile([
  resolve(process.cwd(), 'web/src/share/LiveInkLayer.vue'),
  resolve(process.cwd(), '../web/src/share/LiveInkLayer.vue'),
])

const overlay = readFileSync(overlayPath, 'utf8')
const inkLayer = readFileSync(inkLayerPath, 'utf8')

/** The numeric reference width a surface is normalised against, e.g. 800. */
function referenceWidth(src: string, label: string): number {
  // Both sides express it as `Math.max(1, <surfaceWidth> / N)` — the renderer
  // binds the width to `w` first, the overlay spells out `canvas.width`.
  const m = src.match(/Math\.max\(1,\s*(?:canvas\.)?\w+\s*\/\s*(\d+)\)/)
  if (!m) throw new Error(`${label}: no "<surface width> / <reference>" scaling found`)
  return Number(m[1])
}

/** The expression assigned to `ctx.lineWidth`, whitespace-collapsed. */
function lineWidthExpr(src: string, label: string): string {
  const m = src.match(/ctx\.lineWidth\s*=\s*([^\n]+)/)
  if (!m) throw new Error(`${label}: no ctx.lineWidth assignment found`)
  return m[1].trim()
}

describe('annotation stroke thickness', () => {
  it('uses the same reference width as the renderer ink layer', () => {
    expect(referenceWidth(overlay, 'annotate.html')).toBe(referenceWidth(inkLayer, 'LiveInkLayer.vue'))
  })

  it('scales the overlay line width by the surface, not by dpr alone', () => {
    const expr = lineWidthExpr(overlay, 'annotate.html')
    // A fixed `width * dpr` is what made the host's line thinner than the
    // audience's; the width must come from the scaled helper.
    expect(expr).not.toMatch(/^width\s*\*\s*dpr$/)
    expect(expr).toMatch(/inkScale\(\)/)
  })

  it('keeps the scale helper derived from the canvas backing size', () => {
    // The helper's body must divide by the reference width; this is what makes
    // dpr cancel out (lineWidth and canvas.width are both device pixels).
    expect(overlay).toMatch(/function inkScale\(\)\s*\{\s*return Math\.max\(1, canvas\.width \/ \d+\)/)
  })
})
