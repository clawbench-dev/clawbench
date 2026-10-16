import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * The notification filter row must not overflow its panel.
 *
 * The panel lives in the left column, whose width comes from the split ratio —
 * it can be dragged to 320px (MIN_PANEL_WIDTH) and is also the whole screen on
 * a phone. Three content-sized dropdowns plus the count exceed that, and the
 * original non-wrapping row overflowed: measured scrollWidth 355 vs clientWidth
 * 320, with the third dropdown clipped and the count pushed entirely off-screen.
 *
 * The fix wraps the row and keeps the triggers at natural width, moving the
 * count to a second line instead of clipping it. A viewport @media query cannot
 * express this — on wide screens the panel is narrower than the viewport, so the
 * constraint is container-relative.
 *
 * jsdom has no layout engine, so this is a source-sniffing check (the pattern
 * used by settingsHeaderAlignment.css.test.ts). cwd differs between a bare
 * `vitest` run (web/) and scripts/vitest-run.sh (repo root), so probe both.
 */

function readWebFile(relPath: string): string {
  for (const base of [process.cwd(), join(process.cwd(), 'web')]) {
    try {
      return readFileSync(join(base, relPath), 'utf8')
    } catch {
      // try the next candidate root
    }
  }
  throw new Error(`${relPath} not found from cwd: ` + process.cwd())
}

function stripComments(css: string): string {
  return css.replace(/\/\*[\s\S]*?\*\//g, '')
}

/** The `<style>` block of a .vue source (the template has braces that would confuse a rule regex). */
function styleBlock(sfc: string): string {
  const m = sfc.match(/<style[^>]*>([\s\S]*?)<\/style>/)
  return m ? m[1] : ''
}

/** The declaration block of the first rule whose selector is exactly `.cls`. */
function baseRule(sfc: string, cls: string): string | null {
  const target = `.${cls}`
  for (const m of stripComments(styleBlock(sfc)).matchAll(/([^{}]+)\{([^}]*)\}/g)) {
    if (m[1].trim() === target) return m[2]
  }
  return null
}

const panel = readWebFile('src/components/notifications/NotificationsPanelContent.vue')

describe('notification filter row survives the narrowest panel', () => {
  it('wraps instead of overflowing', () => {
    const decls = baseRule(panel, 'notif-filters')
    expect(decls, '.notif-filters rule must exist').not.toBeNull()
    expect(decls, 'the filter row must wrap so the count cannot be clipped').toMatch(/flex-wrap:\s*wrap/)
  })

  it('keeps the count from being pushed off-screen', () => {
    const decls = baseRule(panel, 'filter-count')
    expect(decls, '.filter-count rule must exist').not.toBeNull()
    // `margin-left:auto` right-aligns the count on the dropdowns' line and,
    // once they fill it, wraps it to its own line. `white-space:nowrap` stops
    // the label itself breaking mid-word.
    expect(decls).toMatch(/margin-left:\s*auto/)
    expect(decls).toMatch(/white-space:\s*nowrap/)
  })

  it('does not shrink the dropdowns into ellipsised labels', () => {
    // Wrapping is chosen over shrinking on purpose: a shrunken trigger would
    // render "项目: 全…", which is worse than moving the count down a line.
    const css = stripComments(styleBlock(panel))
    const m = css.match(/\.notif-filters\s+:deep\(\.menu-select\)\s*\{([^}]*)\}/)
    expect(m, 'the filter row must pin its dropdown flex-shrink').not.toBeNull()
    expect(m![1]).toMatch(/flex-shrink:\s*0/)
  })

  it('has no leftover spacer element (the auto margin replaced it)', () => {
    // The old row used an explicit .filter-spacer div for the same job. Keeping
    // both would be two mechanisms for one gap.
    expect(panel).not.toContain('filter-spacer')
    expect(baseRule(panel, 'filter-spacer')).toBeNull()
  })
})

/**
 * The list must SCROLL, not compress its cards.
 *
 * `.notif-body` is a flex column (so the empty state can centre via flex:1),
 * which makes every card a flex item with the default `flex-shrink:1`. Once the
 * rows exceeded the panel height they were compressed to fit instead of
 * scrolling: measured with 31 rows, each card collapsed to 22px (should be
 * ~74-98) while scrollHeight 938 > clientHeight 670. The sibling TaskListPage
 * only escapes this because its scroll container is a plain block and the flex
 * column lives on an inner element.
 */
describe('notification list scrolls instead of squashing its cards', () => {
  it('pins the cards against flex shrink', () => {
    const decls = baseRule(panel, 'notif-card')
    expect(decls, '.notif-card rule must exist').not.toBeNull()
    expect(decls, 'cards must not shrink — the body scrolls instead').toMatch(/flex-shrink:\s*0/)
  })

  it('pins the load-more button against flex shrink', () => {
    // Same trap: the button is also a direct child of the flex-column body.
    const decls = baseRule(panel, 'load-more')
    expect(decls, '.load-more rule must exist').not.toBeNull()
    expect(decls).toMatch(/flex-shrink:\s*0/)
  })

  it('keeps the body as the scroll container', () => {
    const decls = baseRule(panel, 'notif-body')
    expect(decls, '.notif-body rule must exist').not.toBeNull()
    expect(decls).toMatch(/overflow-y:\s*auto/)
    expect(decls).toMatch(/min-height:\s*0/)
  })
})

/**
 * The project identifier is right-aligned within the card's meta row.
 *
 * It is context ABOUT the row (which project this happened in), not another
 * event attribute, so it reads as a right-edge affordance rather than one more
 * chip in the left-hand run. `margin-left: auto` does the work — and it also
 * pins the DOM order requirement below: with an auto margin the right-aligned
 * element must be LAST, or a later sibling would sit to its right.
 */
describe('the project identifier sits at the right edge of the meta row', () => {
  it('right-aligns the project chip with an auto margin', () => {
    const decls = baseRule(panel, 'nc-proj')
    expect(decls, '.nc-proj rule must exist').not.toBeNull()
    expect(decls, 'the project chip must be pushed to the right edge').toMatch(/margin-left:\s*auto/)
  })

  it('puts the project chip LAST in the meta row', () => {
    // An auto left margin only reaches the right edge if nothing follows it.
    // The source-deleted chip used to come after, which would have left the
    // project floating mid-row. Assert the template order directly.
    const meta = panel.match(/<div class="nc-meta">([\s\S]*?)<\/div>/)
    expect(meta, 'the meta row must exist').not.toBeNull()
    const body = meta![1]
    const projAt = body.indexOf('nc-proj')
    const goneAt = body.indexOf('nc-gone')
    expect(projAt, 'the project chip must be in the meta row').toBeGreaterThan(-1)
    expect(goneAt, 'the source-deleted chip must still be in the meta row').toBeGreaterThan(-1)
    expect(projAt, 'the project chip must come AFTER the source-deleted chip').toBeGreaterThan(goneAt)
  })
})
