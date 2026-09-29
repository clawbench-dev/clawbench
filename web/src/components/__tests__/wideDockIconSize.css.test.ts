import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Source-contract guard for the wide-screen (left) dock glyph size.
 *
 * jsdom has no CSS engine and never applies App.vue's scoped stylesheet, so the
 * "icons are bigger, rail unchanged" invariant cannot be observed from a
 * mounted component — it is read off the source, the same pattern as
 * dockBadgeAnim.test.ts / taskAndStatsGlyphs.css.test.ts.
 *
 * The rail is a fixed 48px and every dock button is 34px; the active-indicator
 * height (34px) and the JS step constant DOCK_STEP (34 + 12 gap = 46) are both
 * pinned to that button geometry. So enlarging the glyphs must be done purely
 * with an svg size override — bumping the button instead would desync the
 * sliding indicator from the buttons it highlights.
 */

const appVue = readFileSync(join(__dirname, '..', '..', 'App.vue'), 'utf8')

function stripComments(css: string): string {
  return css.replace(/\/\*[\s\S]*?\*\//g, '')
}

/** Declaration block of the first rule whose selector is exactly `sel`. */
function ruleDecls(css: string, sel: string): string | null {
  const escaped = sel.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const m = stripComments(css).match(
    new RegExp(`(?:^|[},])\\s*${escaped}\\s*\\{([^}]*)\\}`),
  )
  return m ? m[1] : null
}

describe('wide dock glyph size', () => {
  it('renders the dock glyphs at 20px inside the 48px rail', () => {
    const decls = ruleDecls(appVue, '.wide-dock .dock-btn svg')
    expect(decls, '.wide-dock .dock-btn svg rule must exist').not.toBeNull()
    expect(decls).toMatch(/width:\s*20px/)
    expect(decls).toMatch(/height:\s*20px/)
  })

  it('keeps the rail width and button geometry untouched', () => {
    // The rail width, the button box, and the active-indicator height all have
    // to agree with DOCK_STEP; enlarging any of them would drift the sliding
    // highlight off the buttons.
    expect(ruleDecls(appVue, '.wide-dock')).toMatch(/width:\s*48px/)
    expect(ruleDecls(appVue, '.dock-btn')).toMatch(/width:\s*34px/)
    expect(ruleDecls(appVue, '.dock-btn')).toMatch(/height:\s*34px/)
    expect(ruleDecls(appVue, '.wide-dock .wide-dock-active-indicator')).toMatch(
      /height:\s*34px/,
    )
  })

  it('keeps the base bottom-dock glyph at 16px', () => {
    // The override is scoped under .wide-dock on purpose: the horizontal bottom
    // dock must keep its original 16px glyphs.
    expect(ruleDecls(appVue, '.dock-btn svg')).toMatch(/width:\s*16px/)
    expect(ruleDecls(appVue, '.dock-btn svg')).toMatch(/height:\s*16px/)
  })
})
