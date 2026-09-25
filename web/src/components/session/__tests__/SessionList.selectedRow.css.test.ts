import { describe, it, expect } from 'vitest'

/**
 * Regression guard for the selected-session-row tint.
 *
 * Bug: the tint was declared on `.session-item` (and, for pinned rows, only on
 * `.session-row.pinned.active .session-item`). `.session-item` is the flexible
 * cell to the LEFT of the fixed-width trailing action button, so the tint stopped
 * 34px short of the right edge. On a running (green) row that cell kept showing
 * the row's green fill, making a selected pinned+running row look like it was
 * only partially highlighted.
 *
 * Fix: declare the tint on `.session-row.active` so it spans the trailing button
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
    // reintroduce the 34px gap in front of the trailing action button.
    expect(src).not.toMatch(/\.session-item\.active\s*\{[^}]*background/)
  })

  it('has no pinned-only item tint that would leave the trailing cell unstyled', async () => {
    const src = await sessionListSource()
    expect(src).not.toContain('.session-row.pinned.active .session-item')
  })

  it('leaves the row background to the selection tint alone', async () => {
    const src = await sessionListSource()
    // The running state used to add a tinted background, which had to be
    // declared in the `background-color` slot so the active row's
    // `background-image` tint survived. The running signal is now a bottom
    // band and paints no row background at all, so the two can no longer
    // collide — assert the row keeps its own background free.
    const runningRule = src.match(/\.session-row\.running\s*\{[^}]*\}/)?.[0]
    expect(runningRule, '.session-row.running should exist').toBeTruthy()
    expect(runningRule, 'the running row must not paint a background').not.toMatch(
      /background(-color)?\s*:/,
    )
    // The selection tint is still declared on the row, not the inner cell.
    expect(src).toMatch(/\.session-row\.active\s*\{[\s\S]*?background-image:/)
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

describe('SessionList context-menu row tint', () => {
  // Bug: the row under an open context menu LOST the hover tint it had a moment
  // earlier. Opening the menu paints a full-viewport .ctx-overlay, which
  // swallows :hover (measured in the browser: the row stops matching :hover),
  // and although the `menu-open` class was already being computed and applied,
  // no rule ever styled it — so the row went plain at exactly the moment the
  // user needed to see which row the menu targeted. It read as worse than not
  // opening the menu at all.
  //
  // The existing class-application test could not catch this: the class was
  // always applied correctly. Only a source check can.
  it('styles the .menu-open class instead of leaving it dead', async () => {
    const src = await sessionListSource()
    const rule = src.match(/\.session-row\.menu-open\s*\{[^}]*\}/)?.[0]
    expect(rule, '.session-row.menu-open must have a style rule').toBeTruthy()
    expect(rule).toMatch(/background-color:/)
  })

  it('matches the hover tint so the row does not jump when the menu opens', async () => {
    const src = await sessionListSource()
    // Compare the two declared values rather than hardcoding 6%: if hover is
    // retuned, menu-open must follow, or the row flickers brighter as the menu
    // opens. A stronger value would also blur the distinction from .active,
    // which means "this is the open conversation".
    const hoverRule = src.match(/\.session-row:hover\s*\{[^}]*\}/)?.[0]
    const menuRule = src.match(/\.session-row\.menu-open\s*\{[^}]*\}/)?.[0]
    expect(hoverRule, '.session-row:hover should exist').toBeTruthy()
    expect(menuRule, '.session-row.menu-open should exist').toBeTruthy()
    const value = (r: string) => r.match(/background-color:\s*([^;]+);/)?.[1]?.trim()
    expect(value(menuRule!)).toBe(value(hoverRule!))
  })

  it('keeps the rule outside the hover media query so touch gets the cue', async () => {
    const src = await sessionListSource()
    // Touch has no hover to lose, and the ⋮ button opens this same menu there,
    // so for touch this rule is the ONLY indication of which row is targeted.
    // Inside `@media (hover: hover)` it would never apply there.
    const blocks = src.match(/@media \(hover: hover\)\s*\{[\s\S]*?\n\}/g) || []
    for (const b of blocks) {
      expect(b).not.toContain('.session-row.menu-open')
    }
  })
})

describe('SessionList pinned marker', () => {
  it('paints the corner wedge from the .pinned row modifier', async () => {
    const src = await sessionListSource()
    // The wedge is a clipped square on the row. jsdom has no CSS engine, so this
    // asserts the declarations survive: without the clip-path the box renders as
    // a full square instead of a triangle, and a zero width/height would make it
    // vanish entirely.
    const rule = src.match(/\.session-row\.pinned::after\s*\{[^}]*\}/)?.[0]
    expect(rule, '.session-row.pinned::after should exist').toBeTruthy()
    // Three vertices, one of them the top-right corner the box is anchored to.
    expect(rule).toMatch(/clip-path:\s*polygon\(\s*0\s+0\s*,\s*100%\s+0\s*,\s*100%\s+100%\s*\)/)
    expect(rule).toMatch(/width:\s*12px/)
    expect(rule).toMatch(/height:\s*12px/)
    // Anchored to the row's own top-right corner.
    expect(rule).toMatch(/top:\s*0/)
    expect(rule).toMatch(/right:\s*0/)
    // Decorative only — must not swallow clicks meant for the trailing button.
    expect(rule).toMatch(/pointer-events:\s*none/)
  })

  it('keeps the wedge on the theme accent and shades it for depth', async () => {
    const src = await sessionListSource()
    const rule = src.match(/\.session-row\.pinned::after\s*\{[^}]*\}/)?.[0]
    expect(rule).toBeTruthy()
    // Colour must follow the theme (a hardcoded amber ignored the user's accent).
    expect(rule).toMatch(/var\(--accent-color/)
    expect(rule).not.toMatch(/#f59e0b/)
    // The gradient + drop shadow are what give the flat triangle its depth;
    // both shades are mixed from the accent so they track the theme too.
    expect(rule).toMatch(/background:\s*linear-gradient\(/)
    expect(rule).toMatch(/color-mix\(in srgb, var\(--accent-color[^)]*\)/)
    expect(rule).toMatch(/filter:\s*drop-shadow\(/)
  })

  it('has no leftover pinned/recent section wrapper styles or inline pin glyph', async () => {
    const src = await sessionListSource()
    // The grouping is gone; a stray .session-section rule would mean the layout
    // wrapper outlived its markup.
    expect(src).not.toMatch(/\.session-section\s*\{/)
    expect(src).not.toMatch(/\.session-group-pin-icon/)
    // The pin glyph was dropped in favour of the wedge alone — a leftover class
    // rule would mean a dead style outlived the markup.
    expect(src).not.toMatch(/\.session-pin-icon\s*\{/)
  })
})

describe('SessionList row action button', () => {
  it('styles the trailing action cell under its current class name only', async () => {
    const src = await sessionListSource()
    // The standalone archive button became the grip menu/drag handle; a leftover
    // `.session-archive-btn` rule would mean dead CSS outlived the markup.
    expect(src).toMatch(/\.session-more-btn\s*\{/)
    expect(src).not.toMatch(/\.session-archive-btn/)
  })

  it('insets the trailing button slightly from the panel edge', async () => {
    const src = await sessionListSource()
    // Flush against the right edge the tap target merged into the panel border
    // on mobile; the fix is a small inset, not a wide gutter — the icon stays on
    // the edge it has always sat on. jsdom has no layout engine, so assert the
    // declaration survives rather than the measured pixels.
    // Anchored at line start so the sortable-chosen descendant rule
    // (`.session-row.sortable-chosen .session-more-btn`) cannot match first.
    const rule = src.match(/(?:^|\n)\.session-more-btn\s*\{[^}]*\}/)?.[0]
    expect(rule, '.session-more-btn should exist').toBeTruthy()
    expect(rule).toMatch(/margin-right:\s*var\(--space-3\)/)
    // Guard the "small" half: a wide gutter (space-6/7) is the opposite mistake,
    // so pin the scale step rather than just "has a margin-right".
    expect(rule).not.toMatch(/margin-right:\s*var\(--space-[678]\)/)
  })
})
