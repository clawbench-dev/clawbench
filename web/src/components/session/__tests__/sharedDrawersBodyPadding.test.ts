import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Guard: the two "shared …" drawers' list containers must have NO padding.
 *
 * The bodies previously carried `padding: var(--space-2) var(--space-7)
 * var(--space-7)`, which inset the whole list from the sheet's edges. The rows
 * are meant to read as a flat, edge-to-edge list (each row already owns its own
 * horizontal inset), so the container-level padding is the thing to keep out.
 * The empty/loading state now carries its own padding + margin instead, so it
 * still never hugs the drawer edge.
 *
 * jsdom has no CSS engine, so a mounted component cannot observe this — the
 * value is read off the source, the same pattern as sharedDrawerDestructiveColor
 * .test.ts / sessionShareDialogTheme.css.test.ts.
 */

interface Drawer {
  name: string
  file: string
  body: string
  emptyIcon: string
}

const DRAWERS: Drawer[] = [
  {
    name: 'shared sessions',
    file: join(__dirname, '..', 'SharedSessionsDrawer.vue'),
    body: '.shared-sessions-body',
    emptyIcon: '.shared-sessions-empty-icon',
  },
  {
    name: 'shared files',
    file: join(__dirname, '..', '..', 'file', 'SharedFilesDrawer.vue'),
    body: '.shared-files-body',
    emptyIcon: '.shared-files-empty-icon',
  },
]

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

for (const { name, file, body, emptyIcon } of DRAWERS) {
  const css = stripComments(readFileSync(file, 'utf8'))

  describe(`${name} drawer list container has no surrounding padding`, () => {
    it(`keeps ${body} free of a padding declaration`, () => {
      const decls = ruleDecls(css, body)
      expect(decls, `${body} rule must exist`).not.toBeNull()
      expect(decls).not.toMatch(/(?:^|;|\{)\s*padding\s*:/)
      expect(decls).not.toMatch(/(?:^|;|\{)\s*padding-(?:top|right|bottom|left)\s*:/)
    })

    it(`keeps ${emptyIcon} tinted from the muted token`, () => {
      // The empty state must not hardcode a colour — the whole point of the
      // icon is that it adapts to every theme. A `var(--token, #fallback)` pair
      // is the house convention, so var() calls are stripped before the scan.
      const decls = ruleDecls(css, emptyIcon)
      expect(decls, `${emptyIcon} rule must exist`).not.toBeNull()
      expect(decls).toMatch(/color:\s*var\(--text-muted/)
      const bare = decls!.replace(/var\([^()]*(?:\([^()]*\)[^()]*)*\)/g, '')
      expect(bare, `${emptyIcon} must not hardcode a colour`).not.toMatch(/#[0-9a-f]{3,8}\b/i)
    })
  })
}
