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
  row: string
  meta: string
  emptyIcon: string
}

const DRAWERS: Drawer[] = [
  {
    name: 'shared sessions',
    file: join(__dirname, '..', 'SharedSessionsDrawer.vue'),
    body: '.shared-sessions-body',
    row: '.shared-session-row',
    meta: '.shared-session-meta',
    emptyIcon: '.shared-sessions-empty-icon',
  },
  {
    name: 'shared files',
    file: join(__dirname, '..', '..', 'file', 'SharedFilesDrawer.vue'),
    body: '.shared-files-body',
    row: '.shared-file-row',
    meta: '.shared-file-meta',
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

for (const { name, file, body, row, meta, emptyIcon } of DRAWERS) {
  const css = stripComments(readFileSync(file, 'utf8'))

  describe(`${name} drawer list container has no surrounding padding`, () => {
    it(`keeps ${body} free of a padding declaration`, () => {
      const decls = ruleDecls(css, body)
      expect(decls, `${body} rule must exist`).not.toBeNull()
      expect(decls).not.toMatch(/(?:^|;|\{)\s*padding\s*:/)
      expect(decls).not.toMatch(/(?:^|;|\{)\s*padding-(?:top|right|bottom|left)\s*:/)
    })

    it(`gives ${row} its own horizontal inset`, () => {
      // With the container padding gone, each row owns its horizontal inset so
      // its content never touches the drawer edge. Vertical padding stays as
      // the literal-first token pair (`<N>px var(--space-*)`).
      const decls = ruleDecls(css, row)
      expect(decls, `${row} rule must exist`).not.toBeNull()
      const m = decls!.match(/padding:\s*\S+\s+var\(--space-(\d+)\)/)
      expect(m, `${row} must declare a vertical/horizontal padding pair`).not.toBeNull()
      // --space-4 is 8px; anything less re-hugs the edge.
      expect(Number(m![1]), `${row} horizontal padding must be at least --space-4`).toBeGreaterThanOrEqual(4)
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

    it(`lays out ${meta} like its sibling drawer's meta line`, () => {
      // Both drawers render "name / meta" as the same two-line row. The meta
      // line must therefore be constructed identically; a drift here is what
      // made the file row a taller three-line row before.
      const decls = ruleDecls(css, meta)
      expect(decls, `${meta} rule must exist`).not.toBeNull()
      expect(decls, `${meta} must be a flex row`).toMatch(/display:\s*flex/)
      expect(decls, `${meta} must use the muted token`).toMatch(/color:\s*var\(--text-muted/)
      expect(decls, `${meta} must be xs-sized`).toMatch(/font-size:\s*var\(--font-size-xs/)
    })
  })
}

// The two rows must share the same visual skeleton: a name row + a meta line,
// with a same-geometry badge. Compared field-by-field rather than as a whole
// block so the one allowed difference (the badge COLOUR, which carries state)
// does not mask a geometry drift.
describe('the two share-list drawers read as one family', () => {
  const sessionCss = stripComments(
    readFileSync(join(__dirname, '..', 'SharedSessionsDrawer.vue'), 'utf8'),
  )
  const fileCss = stripComments(
    readFileSync(join(__dirname, '..', '..', 'file', 'SharedFilesDrawer.vue'), 'utf8'),
  )

  const normalise = (decls: string) =>
    decls
      .split(';')
      .map((d) => d.trim().replace(/\s*:\s*/, ':').replace(/\s+/g, ' '))
      .filter(Boolean)
      .sort()
      .join(';')

  it('gives both meta lines the same declarations', () => {
    const a = ruleDecls(sessionCss, '.shared-session-meta')
    const b = ruleDecls(fileCss, '.shared-file-meta')
    expect(a, '.shared-session-meta rule must exist').not.toBeNull()
    expect(b, '.shared-file-meta rule must exist').not.toBeNull()
    expect(normalise(a!)).toBe(normalise(b!))
  })

  it('gives both badges the same geometry', () => {
    // Geometry (padding / radius / font-size) is the skeleton; colour is the
    // state signal and is deliberately allowed to differ (orange "archived"
    // vs red "deleted").
    const geom = (decls: string) =>
      decls
        .split(';')
        .map((d) => d.trim().replace(/\s*:\s*/, ':').replace(/\s+/g, ' '))
        .filter((d) => /^(padding|border-radius|font-size):/.test(d))
        .sort()
        .join(';')
    const a = ruleDecls(sessionCss, '.shared-session-badge')
    const b = ruleDecls(fileCss, '.shared-file-badge')
    expect(a, '.shared-session-badge rule must exist').not.toBeNull()
    expect(b, '.shared-file-badge rule must exist').not.toBeNull()
    expect(geom(a!), 'both badges must share padding/radius/font-size').toBe(geom(b!))
  })

  it('renders both meta lines with a separator dot and the shared time formatter', () => {
    for (const [css, sep, time] of [
      [sessionCss, 'shared-session-sep', 'shared-session-meta'],
      [fileCss, 'shared-file-sep', 'shared-file-meta'],
    ] as const) {
      // The separator only shows between the two meta parts.
      expect(css, `${sep} rule must exist`).toMatch(
        new RegExp(`\\.${sep}\\s*\\{`),
      )
      // Both drawers route createdAt through the single relative-time helper.
      expect(css, `${time} must use formatRelativeTime`).toMatch(/formatRelativeTime\(/)
    }
  })
})
