import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Guard: the shared-conversations drawer's list container must have NO padding.
 *
 * The body previously carried `padding: var(--space-2) var(--space-7)
 * var(--space-7)`, which inset the whole list from the sheet's edges. The rows
 * are meant to read as a flat, edge-to-edge list (each row already owns its own
 * horizontal inset via `.shared-session-row`), so the container-level padding is
 * the thing to keep out.
 *
 * jsdom has no CSS engine, so a mounted component cannot observe this — the
 * value is read off the source, the same pattern as sharedDrawerDestructiveColor
 * .test.ts / sessionShareDialogTheme.css.test.ts.
 */

const DRAWER = join(__dirname, '..', 'SharedSessionsDrawer.vue')

function stripComments(css: string): string {
  return css.replace(/\/\*[\s\S]*?\*\//g, '')
}

/** Declaration block of the first rule whose selector is exactly `sel`.
 *  Anchored on a preceding newline (or start of input) so a selector is never
 *  matched as a substring of another — the CSS is pretty-printed, one rule per
 *  line, so a real rule always begins a line. */
function ruleDecls(css: string, sel: string): string | null {
  const escaped = sel.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const m = css.match(new RegExp(`(?:^|\\n)\\s*${escaped}\\s*\\{([^}]*)\\}`))
  return m ? m[1] : null
}

describe('SharedSessionsDrawer list container has no surrounding padding', () => {
  const css = stripComments(readFileSync(DRAWER, 'utf8'))

  it('keeps .shared-sessions-body free of a padding declaration', () => {
    const decls = ruleDecls(css, '.shared-sessions-body')
    expect(decls, '.shared-sessions-body rule must exist').not.toBeNull()
    expect(decls).not.toMatch(/(?:^|;|\{)\s*padding\s*:/)
    expect(decls).not.toMatch(/(?:^|;|\{)\s*padding-(?:top|right|bottom|left)\s*:/)
  })
})
