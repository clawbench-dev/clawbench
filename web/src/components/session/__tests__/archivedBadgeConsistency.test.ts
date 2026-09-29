import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Source-contract guard: the "archived" badge must look the same in the shared
 * sessions drawer and in the session-search result list.
 *
 * Both badges mark the same server state, so a divergence is a bug: the user
 * sees one archived conversation rendered as a neutral gray chip and another as
 * an orange status chip, with no signal that they mean the same thing.
 *
 * jsdom has no CSS engine and never applies a component's scoped stylesheet, so
 * this invariant cannot be observed from a mounted component — it is read off
 * the source, the same pattern as wideDockIconSize.css.test.ts.
 */

const sharedDrawer = readFileSync(
  join(__dirname, '..', 'SharedSessionsDrawer.vue'),
  'utf8',
)
const searchDrawer = readFileSync(
  join(__dirname, '..', 'SessionSearchDrawer.vue'),
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

/** Normalise a declaration block so formatting differences do not matter. */
function normalise(decls: string): string {
  return decls
    .split(';')
    .map((d) => d.trim().replace(/\s*:\s*/, ':').replace(/\s+/g, ' '))
    .filter(Boolean)
    .sort()
    .join(';')
}

describe('archived badge consistency', () => {
  it('renders the shared-sessions badge with the session-search styling', () => {
    const shared = ruleDecls(sharedDrawer, '.shared-session-badge')
    const search = ruleDecls(searchDrawer, '.session-search-item-archived')

    expect(shared, '.shared-session-badge rule must exist').not.toBeNull()
    expect(
      search,
      '.session-search-item-archived rule must exist',
    ).not.toBeNull()

    // `flex-shrink: 0` is a layout concern of the shared row (the search list
    // does not set it), so it is the one declaration allowed to differ.
    const withoutLayout = (d: string) =>
      normalise(d)
        .split(';')
        .filter((x) => !x.startsWith('flex-shrink'))
        .join(';')

    expect(
      withoutLayout(shared!),
      'the two archived badges must use identical visual declarations',
    ).toBe(withoutLayout(search!))
  })

  it('labels both badges with the same i18n text', () => {
    const sharedText = sharedDrawer.match(
      /class="shared-session-badge"[^>]*>\s*\{\{\s*t\('([^']+)'\)/,
    )?.[1]
    const searchText = searchDrawer.match(
      /class="session-search-item-archived"[^>]*>\{\{\s*t\('([^']+)'\)/,
    )?.[1]

    expect(sharedText).toBe('sharedSessions.archived')
    expect(searchText).toBe('sessionSearch.archived')

    // The keys differ (different namespaces) but the resolved strings must be
    // the same label in every locale, otherwise the same state reads
    // differently. Both locales are checked: zh is the primary UI language.
    const localeRoot = join(__dirname, '..', '..', '..', 'i18n', 'locales')
    const label = (file: string, ns: string) =>
      readFileSync(join(localeRoot, file), 'utf8').match(
        new RegExp(`^\\s*${ns}:\\s*\\{[\\s\\S]*?archived:\\s*'([^']+)'`, 'm'),
      )?.[1]

    for (const file of ['zh.ts', 'en.ts']) {
      const sharedLabel = label(file, 'sharedSessions')
      const searchLabel = label(file, 'sessionSearch')
      expect(sharedLabel, `${file}: sharedSessions.archived`).toBeTruthy()
      expect(searchLabel, `${file}: sessionSearch.archived`).toBe(sharedLabel)
    }
  })
})
