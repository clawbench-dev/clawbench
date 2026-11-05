/**
 * The drawing surface's toolbar must stay clickable.
 *
 * The annotator (its own window, used whenever the publisher shares a single
 * window instead of the whole screen) is one big pointer target: the root turns
 * every pointerdown into a stroke. The ink canvas inside it is also an active
 * pointer target — it is the same component the viewers use, where `active`
 * opts it back into hit-testing — and it carries `z-index: 4` for that layout.
 *
 * A z-index on a child whose positioned parent has no z-index is resolved
 * against the OUTER stacking context, so the canvas outranked the toolbar
 * (`z-index: auto`). The bar was drawn but never hit: clicks fell through to
 * the canvas, and every attempt to pick the laser, a colour, undo or close
 * simply started a stroke. It only showed up on window shares because a
 * full-screen share on the desktop goes to the native overlay instead.
 *
 * jsdom neither paints nor hit-tests, so the ordering is read from the sources.
 */
import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

const OVERLAY = 'src/share/LiveAnnotateOverlay.vue'
const INK = 'src/share/LiveInkLayer.vue'

/** Comments mention the properties they explain; drop them before parsing. */
function stripComments(s: string): string {
  return s.replace(/\/\*[\s\S]*?\*\//g, '')
}

function styleOf(file: string): string {
  const src = readWebFile(file)
  const start = src.indexOf('<style')
  if (start < 0) throw new Error(`${file} has no <style> block`)
  return stripComments(src.slice(start))
}

function ruleDecls(css: string, selector: string): Record<string, string> {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const m = css.match(new RegExp(`\\n${escaped}\\s*\\{([^}]*)\\}`))
  if (!m) throw new Error(`rule ${selector} not found`)
  const out: Record<string, string> = {}
  for (const decl of m[1].split(';')) {
    const i = decl.indexOf(':')
    if (i < 0) continue
    const prop = decl.slice(0, i).trim()
    const value = decl.slice(i + 1).trim().replace(/\s+/g, ' ')
    if (prop) out[prop] = value
  }
  return out
}

function zIndexOf(decls: Record<string, string>, selector: string): number | null {
  const raw = decls['z-index']
  if (raw === undefined) return null
  const n = Number(raw)
  if (!Number.isFinite(n)) throw new Error(`${selector} has a non-numeric z-index: ${raw}`)
  return n
}

describe('annotate overlay: the toolbar sits above the drawing surface', () => {
  const overlay = styleOf(OVERLAY)
  const ink = styleOf(INK)

  it('keeps the ink canvas a live pointer target (the premise of this contract)', () => {
    // If the canvas ever stops taking pointer events the ordering below stops
    // mattering — and the drawing surface itself stops working. Pin both.
    expect(ruleDecls(ink, '.live-ink-layer')['pointer-events']).toBe('none')
    expect(ruleDecls(ink, '.live-ink-layer--active')['pointer-events']).toBe('auto')
    expect(zIndexOf(ruleDecls(ink, '.live-ink-layer'), '.live-ink-layer')).toBeGreaterThan(0)
  })

  it('gives the ink wrapper its own stacking context', () => {
    // Without a z-index here the canvas's own z-index escapes into the
    // overlay's stacking context and competes with the toolbar directly.
    const inkWrap = ruleDecls(overlay, '.anno__ink')
    expect(inkWrap['position']).toBe('absolute')
    expect(zIndexOf(inkWrap, '.anno__ink')).not.toBeNull()
  })

  it('puts the bar above the ink', () => {
    const inkWrapZ = zIndexOf(ruleDecls(overlay, '.anno__ink'), '.anno__ink') as number
    const barZ = zIndexOf(ruleDecls(overlay, '.anno__bar'), '.anno__bar')
    expect(barZ, '.anno__bar needs a z-index of its own').not.toBeNull()
    expect(barZ as number).toBeGreaterThan(inkWrapZ)
  })

  it('keeps a press on the bar from becoming a stroke', () => {
    // Once the click lands, the handlers still have to stop it: the root turns
    // any pointerdown it sees into ink.
    const src = readWebFile(OVERLAY)
    expect(src).toMatch(/class="anno__bar"[^>]*@pointerdown\.stop/)
    expect(src).toMatch(/class="anno__bar"[^>]*@pointermove\.stop/)
  })
})
