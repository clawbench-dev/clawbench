import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Source-contract guard for the completion menu's skill-row layout.
 *
 * jsdom has no CSS engine, so "the name row and the description have breathing
 * room" and "the backend icon replaces the source icon" cannot be observed from
 * a mounted component. The icon swap IS covered behaviorally by
 * CompletionMenu.test.ts; the gap is not observable at all, so it is pinned
 * here off the source (same pattern as wideDockIconSize.css.test.ts).
 */

const menuVue = readFileSync(
  join(__dirname, '..', 'CompletionMenu.vue'),
  'utf8',
)

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

describe('completion menu skill-row layout', () => {
  it('separates the name row from the description with a gap', () => {
    // The two stacked lines are flex children of .completion-text; without a
    // gap they sit flush and read as one cramped block.
    const decls = ruleDecls(menuVue, '.completion-text')
    expect(decls, '.completion-text rule must exist').not.toBeNull()
    expect(decls).toMatch(/flex-direction:\s*column/)
    expect(decls).toMatch(/gap:\s*var\(--space-2\)/)
  })

  it('clamps skill descriptions to two lines keeping the prefix', () => {
    const decls = ruleDecls(menuVue, '.completion-desc--skill')
    expect(decls, '.completion-desc--skill rule must exist').not.toBeNull()
    expect(decls).toMatch(/-webkit-line-clamp:\s*2/)
    expect(decls).toMatch(/white-space:\s*normal/)
  })
})
