import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Theme-safety guard for the shared-sessions drawer's destructive controls.
 *
 * Why this exists: the clear-all button and the revoke icon's hover shipped
 * with a hardcoded `#cf222e` (a GitHub-Primer-era red) plus a
 * `[data-theme-base="dark"]` override that repainted it `#fca5a5`. That is
 * off-convention twice over:
 *
 *  1. `#cf222e` matches no theme token, so on the 35 non-default themes the
 *     button kept a light-theme red. The app's status colours are hue-named
 *     tokens (`--color-red`, 36 definitions in variables.css) precisely so
 *     each theme can retune them.
 *  2. The hand-written dark override existed only because the literal could
 *     not theme itself. Once the colour comes from `var(--color-red)` the
 *     override is dead weight that can silently drift from the token.
 *
 * The hover wash follows the app-wide convention (see GitTagList.vue,
 * TaskDetailPage.vue): `color-mix(in srgb, var(--color-red) 10%, transparent)`.
 *
 * jsdom has no CSS engine, so a mounted component cannot observe any of this —
 * it is read off the source, the same pattern as wideDockIconSize.css.test.ts.
 */

const DRAWER = join(__dirname, '..', 'SharedSessionsDrawer.vue')
const source = readFileSync(DRAWER, 'utf8')

function stripComments(css: string): string {
  return css.replace(/\/\*[\s\S]*?\*\//g, '')
}

const css = stripComments(source)

/** Declaration block of the first rule whose selector is exactly `sel`.
 *  The boundary class includes `{` so rules nested in an at-rule (the
 *  `@media (hover: hover)` block) are found too. */
function ruleDecls(sel: string): string | null {
  const escaped = sel.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const m = css.match(new RegExp(`(?:^|[},{])\\s*${escaped}\\s*\\{([^}]*)\\}`))
  return m ? m[1] : null
}

describe('shared-sessions drawer destructive colours are themed', () => {
  it('colours the clear-all button from the red token', () => {
    const decls = ruleDecls('.shared-sessions-clear')
    expect(decls, '.shared-sessions-clear rule must exist').not.toBeNull()
    expect(decls).toMatch(/color:\s*var\(--color-red\)/)
  })

  it('colours the revoke hover from the red token', () => {
    const decls = ruleDecls('.shared-session-btn.danger:hover')
    expect(decls, '.shared-session-btn.danger:hover rule must exist').not.toBeNull()
    expect(decls).toMatch(/color:\s*var\(--color-red\)/)
  })

  it('washes the hover surface from the same token', () => {
    // A literal hover background is the other half of the same defect: it
    // cannot adapt to a dark surface.
    for (const sel of [
      '.shared-sessions-clear:not(:disabled):hover',
      '.shared-session-btn.danger:hover',
    ]) {
      const decls = ruleDecls(sel)
      expect(decls, `${sel} rule must exist`).not.toBeNull()
      expect(decls, `${sel} must tint from the token`).toMatch(
        /background:\s*color-mix\(in srgb,\s*var\(--color-red\)/,
      )
    }
  })

  it('has no hand-written dark override left to drift from the token', () => {
    // These existed only to rescue the hardcoded literal. With a themed token
    // they are redundant, and a stale copy would win over the token's own
    // dark value.
    expect(css).not.toMatch(/\[data-theme-base="dark"\][^{]*shared-sessions-clear/)
    expect(css).not.toMatch(/\[data-theme-base="dark"\][^{]*shared-session-btn/)
  })

  it('carries no bare red literal in its destructive rules', () => {
    // The error message is intentionally left alone (it is a shared
    // cross-component value), so only the destructive selectors are scanned.
    for (const sel of [
      '.shared-sessions-clear',
      '.shared-sessions-clear:not(:disabled):hover',
      '.shared-session-btn.danger:hover',
    ]) {
      const decls = ruleDecls(sel)!
      const bare = decls.replace(/var\([^()]*(?:\([^()]*\)[^()]*)*\)/g, '')
      expect(bare, `${sel} must not hardcode a colour`).not.toMatch(
        /#[0-9a-f]{3,8}\b|rgba?\(/i,
      )
    }
  })
})
