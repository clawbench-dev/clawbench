import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Source-contract guard for the upgrade prompt's footer on narrow screens.
 *
 * The prompt is the first thing a user sees over the home screen, and on a
 * 320px phone its three buttons used to be squeezed until the CJK labels
 * wrapped per character — "立即升级" rendered as a 4-line vertical column, and
 * "跳过此版本" / "稍后提醒" broke into two lines each. Nothing failed: a flex
 * item that shrinks below its label's width simply wraps, and the panel still
 * "fits".
 *
 * jsdom has no layout engine and never applies a scoped stylesheet, so the
 * invariant cannot be observed from a mounted component. It is read off the
 * source instead — the same pattern as wideDockIconSize.css.test.ts.
 *
 * Three things have to hold together, which is why they are pinned together:
 *   1. every label refuses to wrap (`white-space: nowrap`), which is what makes
 *      the label the item's minimum width instead of a soft preference;
 *   2. the row may wrap (`flex-wrap`), so an over-wide row becomes two rows
 *      rather than a squeeze;
 *   3. at narrow widths the primary action takes its own row and the two
 *      dismissals share the next one — otherwise `nowrap` alone would let the
 *      row overflow the panel.
 */
const source = readWebFile('src/components/UpgradePromptOverlay.vue')

function stripComments(css: string): string {
  return css.replace(/\/\*[\s\S]*?\*\//g, '')
}

/** Declaration block of the first rule whose selector is exactly `sel`. */
function ruleDecls(css: string, sel: string): string | null {
  const escaped = sel.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const m = stripComments(css).match(new RegExp(`(?:^|[},])\\s*${escaped}\\s*\\{([^}]*)\\}`))
  return m ? m[1] : null
}

/** Declarations of the narrow-width media block (its body is flat rules only). */
function narrowMediaBlock(css: string): string | null {
  const stripped = stripComments(css)
  const start = stripped.indexOf('@media (max-width: 400px)')
  if (start === -1) return null
  const open = stripped.indexOf('{', start)
  if (open === -1) return null
  let depth = 0
  for (let i = open; i < stripped.length; i++) {
    if (stripped[i] === '{') depth++
    else if (stripped[i] === '}') {
      depth--
      if (depth === 0) return stripped.slice(open + 1, i)
    }
  }
  return null
}

describe('upgrade prompt footer on narrow screens', () => {
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

  it('gives the primary action its own row and splits the dismissals below 400px', () => {
    const block = narrowMediaBlock(source)
    expect(block, 'the narrow-width media block must exist').not.toBeNull()
    expect(ruleDecls(block!, '.up-upgrade')).toMatch(/flex:\s*1 1 100%/)
    expect(ruleDecls(block!, '.up-skip,\n  .up-later')).toMatch(/flex:\s*1 1 0/)
  })

  it('declares the narrow override after the base button rules', () => {
    // Equal specificity, so the override only wins by source order. Moving it
    // above .up-upgrade / .up-skip would silently restore the squeezed row.
    const stripped = stripComments(source)
    const media = stripped.indexOf('@media (max-width: 400px)')
    expect(media).toBeGreaterThan(-1)
    expect(media).toBeGreaterThan(stripped.indexOf('.up-upgrade {'))
    expect(media).toBeGreaterThan(stripped.indexOf('.up-skip {'))
    expect(media).toBeGreaterThan(stripped.indexOf('.up-later {'))
  })
})
