import { describe, it, expect } from 'vitest'

/**
 * Regression guard for the selected-session-row tint.
 *
 * Bug: the tint was declared on `.session-item` (and, for pinned rows, only on
 * `.session-row.pinned.active .session-item`). `.session-item` is the flexible
 * cell to the LEFT of the fixed-width `.session-archive-btn`, so the tint stopped
 * 34px short of the right edge. On a running (green) row the archive cell kept
 * showing the row's green fill, making a selected pinned+running row look like it
 * was only partially highlighted.
 *
 * Fix: declare the tint on `.session-row.active` so it spans the archive button
 * too, and paint it via `background-image` (not the `background` shorthand or
 * `background-color`) so a running row's green `background-color` still shows
 * through underneath instead of being replaced.
 *
 * jsdom has no CSS engine, so — like flashReducedMotion.css.test.ts — this is a
 * source-sniffing test against the raw SFC.
 */

async function sessionListSource(): Promise<string> {
  const mod = await import('@/components/session/SessionList.vue?raw')
  return String(mod.default)
}

describe('SessionList selected-row tint covers the archive button', () => {
  it('declares the selected tint on the row, not on the inner .session-item cell', async () => {
    const src = await sessionListSource()
    // The row owns the tint...
    expect(src).toMatch(/\.session-row\.active\s*\{[\s\S]*?background-image:/)
    // ...and no rule tints a bare `.session-item.active` background, which would
    // reintroduce the 34px gap in front of the archive button.
    expect(src).not.toMatch(/\.session-item\.active\s*\{[^}]*background/)
  })

  it('has no pinned-only item tint that would leave the archive cell unstyled', async () => {
    const src = await sessionListSource()
    expect(src).not.toContain('.session-row.pinned.active .session-item')
  })

  it('keeps running fill in background-color so the active background-image survives', async () => {
    const src = await sessionListSource()
    // A `background:` shorthand here would reset background-image and erase the
    // selection tint on running rows.
    expect(src).toMatch(/\.session-row\.running\s*\{[\s\S]*?background-color:\s*rgba\(34,\s*197,\s*94,\s*0\.05\)/)
    expect(src).toMatch(/\.session-row\.active\.running\s*\{[\s\S]*?background-color:/)
  })

  it('does not clobber the tint with a hover background shorthand', async () => {
    const src = await sessionListSource()
    // Every hover rule must set background-color (never the shorthand) so the
    // selected row's background-image layer is preserved while hovering. There
    // are several `@media (hover: hover)` blocks in the file; pick the one that
    // holds the `.session-row:hover` rule.
    const blocks = src.match(/@media \(hover: hover\)\s*\{[\s\S]*?\n\}/g) || []
    const hoverBlock = blocks.find(b => b.includes('.session-row:hover'))
    expect(hoverBlock, 'a hover block containing .session-row:hover should exist').toBeTruthy()
    const hoverRules = hoverBlock!.match(/\.session-row[^{]*\{[^}]*\}/g) || []
    expect(hoverRules.length).toBeGreaterThan(0)
    for (const rule of hoverRules) {
      expect(rule).toMatch(/background-color:/)
      expect(rule).not.toMatch(/[^-]background:/)
    }
  })
})

describe('SessionList pinned marker', () => {
  it('paints the corner wedge from the .pinned row modifier', async () => {
    const src = await sessionListSource()
    // The wedge is a border-triangle pseudo-element on the row. jsdom has no CSS
    // engine, so this asserts the declaration survives — a `width`/`height` of
    // non-zero (or a missing border) would render a box instead of a wedge.
    const rule = src.match(/\.session-row\.pinned::after\s*\{[^}]*\}/)?.[0]
    expect(rule, '.session-row.pinned::after should exist').toBeTruthy()
    expect(rule).toMatch(/border-top:\s*8px solid/)
    expect(rule).toMatch(/border-left:\s*8px solid transparent/)
    expect(rule).toMatch(/width:\s*0/)
    expect(rule).toMatch(/height:\s*0/)
    // Anchored to the row's own top-right corner.
    expect(rule).toMatch(/top:\s*0/)
    expect(rule).toMatch(/right:\s*0/)
    // Decorative only — must not swallow clicks meant for the archive button.
    expect(rule).toMatch(/pointer-events:\s*none/)
  })

  it('has no leftover pinned/recent section wrapper styles', async () => {
    const src = await sessionListSource()
    // The grouping is gone; a stray .session-section rule would mean the layout
    // wrapper outlived its markup.
    expect(src).not.toMatch(/\.session-section\s*\{/)
    expect(src).not.toMatch(/\.session-group-pin-icon/)
  })
})
