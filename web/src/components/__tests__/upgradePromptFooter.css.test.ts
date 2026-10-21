import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Source-contract guard for the upgrade prompt's footer.
 *
 * Two rounds of this bug:
 *
 * 1. Three buttons in one row were squeezed until the CJK labels wrapped per
 *    character — "立即升级" rendered as a 4-line vertical column. A flex item
 *    that shrinks below its label simply wraps; the panel still "fits".
 * 2. The fix keyed the two-row layout to `@media (max-width: 400px)`, i.e. to
 *    the VIEWPORT. But the panel is capped at 380px, so the constraint is the
 *    panel's 348px content box, not the window. On a 1280px desktop the media
 *    query never fires: English labels (~470px of text) still had to share one
 *    row and the footer wrapped anyway, while Chinese (~230px) stayed on one
 *    row. The dialog laid out differently per language on the same screen.
 *
 * The invariant that fixes both is structural and language-independent: the
 * primary action ALWAYS owns its own row, so the two dismissals only ever
 * compete with each other. That is asserted unconditionally below — no media
 * query may reintroduce the viewport dependency.
 *
 * jsdom has no layout engine and never applies a scoped stylesheet, so this
 * cannot be observed from a mounted component. The geometry was verified in
 * Chromium against the real stylesheet: zh and en now produce identical row
 * counts and identical button widths at 1280 / 500 / 360 / 320px.
 */
const source = readWebFile('src/components/UpgradePromptOverlay.vue')

function stripComments(css: string): string {
  return css.replace(/\/\*[\s\S]*?\*\//g, '')
}

/**
 * Declaration block of the rule whose selector is EXACTLY `sel`.
 *
 * Not a prefix/regex-adjacent match: `.up-later` also appears as the second
 * half of the `.up-skip,\n.up-later` group, and a naive search resolves to that
 * block instead of the button's own base rule (which is what carries
 * `white-space`). Selectors are compared after whitespace normalisation.
 */
function ruleDecls(css: string, sel: string): string | null {
  const stripped = stripComments(css)
  const re = /\n([^{}]+?)\s*\{([^}]*)\}/g
  let m: RegExpExecArray | null
  while ((m = re.exec(stripped)) !== null) {
    const selector = m[1].replace(/\s+/g, ' ').trim()
    if (selector === sel) return m[2]
  }
  return null
}

describe('upgrade prompt footer', () => {
  it('lets the row wrap instead of squeezing its buttons', () => {
    const footer = ruleDecls(source, '.up-footer')
    expect(footer, '.up-footer rule must exist').not.toBeNull()
    expect(footer, 'a non-wrapping row is what squeezed the labels').toMatch(
      /flex-wrap:\s*wrap/,
    )
  })

  it.each(['.up-upgrade', '.up-skip', '.up-later'])(
    'keeps %s on one line',
    (selector) => {
      const decls = ruleDecls(source, selector)
      expect(decls, `${selector} rule must exist`).not.toBeNull()
      expect(decls, 'a wrapping label is the bug being fixed').toMatch(
        /white-space:\s*nowrap/,
      )
    },
  )

  it('gives the primary action its own row UNCONDITIONALLY', () => {
    // `flex-basis: 100%` in the base rule, not inside a media query. This is the
    // property that makes the layout language-independent: the primary button
    // never competes for width with the two labels beside it.
    const decls = ruleDecls(source, '.up-upgrade')
    expect(decls, '.up-upgrade rule must exist').not.toBeNull()
    expect(decls).toMatch(/flex:\s*1 1 100%/)
  })

  it('splits the two dismissals evenly on the second row', () => {
    for (const sel of ['.up-skip', '.up-later']) {
      const decls = ruleDecls(source, sel)
      expect(decls, `${sel} rule must exist`).not.toBeNull()
      expect(decls).toMatch(/flex:\s*1 1 0/)
      // Without this a long label sets the flex item's automatic minimum to its
      // full text width and the even split collapses.
      expect(decls).toMatch(/min-width:\s*0/)
    }
  })

  it('does not key the footer layout to the viewport', () => {
    // The regression: `@media (max-width: 400px)` decided the row structure, so
    // the desktop (where the panel is STILL 380px) kept the squeezed layout and
    // English wrapped while Chinese did not. The panel width is the real
    // constraint, and it does not change with the window.
    const stripped = stripComments(source)
    const footerLayoutDecl = /\.up-(?:upgrade|skip|later|footer)\s*\{[^}]*flex:/
    for (const media of stripped.matchAll(/@media[^{]*\{([\s\S]*?)\n\}/g)) {
      expect(
        media[1],
        'the footer row structure must not depend on a viewport breakpoint',
      ).not.toMatch(footerLayoutDecl)
    }
  })
})
