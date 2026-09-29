import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Theme-safety guard for the two "shared …" drawers' destructive controls.
 *
 * Why this exists: the clear-all button and the revoke icon's hover shipped in
 * BOTH drawers with a hardcoded `#cf222e` (a GitHub-Primer-era red) plus a
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
 * The two files were byte-for-byte copies of the same block, so fixing one and
 * not the other is exactly how they drifted apart in the first place — the
 * guard therefore runs against both.
 *
 * The hover wash follows the app-wide convention (see GitTagList.vue,
 * TaskDetailPage.vue): `color-mix(in srgb, var(--color-red) 10%, transparent)`.
 *
 * jsdom has no CSS engine, so a mounted component cannot observe any of this —
 * it is read off the source, the same pattern as wideDockIconSize.css.test.ts.
 */

interface Drawer {
  name: string
  file: string
  clear: string
  dangerBtn: string
}

const DRAWERS: Drawer[] = [
  {
    name: 'shared sessions',
    file: join(__dirname, '..', 'SharedSessionsDrawer.vue'),
    clear: '.shared-sessions-clear',
    dangerBtn: '.shared-session-btn.danger:hover',
  },
  {
    name: 'shared files',
    file: join(__dirname, '..', '..', 'file', 'SharedFilesDrawer.vue'),
    clear: '.shared-files-clear',
    dangerBtn: '.shared-file-btn.danger:hover',
  },
]

function stripComments(css: string): string {
  return css.replace(/\/\*[\s\S]*?\*\//g, '')
}

/** Declaration block of the first rule whose selector is exactly `sel`.
 *  The boundary class includes `{` so rules nested in an at-rule (the
 *  `@media (hover: hover)` block) are found too. */
function ruleDecls(css: string, sel: string): string | null {
  const escaped = sel.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const m = css.match(new RegExp(`(?:^|[},{])\\s*${escaped}\\s*\\{([^}]*)\\}`))
  return m ? m[1] : null
}

for (const { name, file, clear, dangerBtn } of DRAWERS) {
  const css = stripComments(readFileSync(file, 'utf8'))

  describe(`${name} drawer destructive colours are themed`, () => {
    it('colours the clear-all button from the red token', () => {
      const decls = ruleDecls(css, clear)
      expect(decls, `${clear} rule must exist`).not.toBeNull()
      expect(decls).toMatch(/color:\s*var\(--color-red\)/)
    })

    it('colours the revoke hover from the red token', () => {
      const decls = ruleDecls(css, dangerBtn)
      expect(decls, `${dangerBtn} rule must exist`).not.toBeNull()
      expect(decls).toMatch(/color:\s*var\(--color-red\)/)
    })

    it('washes the hover surface from the same token', () => {
      // A literal hover background is the other half of the same defect: it
      // cannot adapt to a dark surface.
      for (const sel of [`${clear}:not(:disabled):hover`, dangerBtn]) {
        const decls = ruleDecls(css, sel)
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
      expect(css).not.toMatch(
        new RegExp(`\\[data-theme-base="dark"\\][^{]*${clear.replace('.', '\\.')}`),
      )
      expect(css).not.toMatch(
        new RegExp(
          `\\[data-theme-base="dark"\\][^{]*${dangerBtn.split(':')[0].replace('.', '\\.')}`,
        ),
      )
    })

    it('carries no bare red literal in its destructive rules', () => {
      // The error message and the deleted-file badge are intentionally left
      // alone (shared cross-component values), so only the destructive
      // selectors are scanned.
      for (const sel of [clear, `${clear}:not(:disabled):hover`, dangerBtn]) {
        const decls = ruleDecls(css, sel)!
        const bare = decls.replace(/var\([^()]*(?:\([^()]*\)[^()]*)*\)/g, '')
        expect(bare, `${sel} must not hardcode a colour`).not.toMatch(
          /#[0-9a-f]{3,8}\b|rgba?\(/i,
        )
      }
    })
  })
}
