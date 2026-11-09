import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Source-contract guard: the "archived" badge must look the same in the share
 * centre and in the session-search result list.
 *
 * Both badges mark the same server state, so a divergence is a bug: the user
 * sees one archived conversation rendered as a neutral gray chip and another as
 * an orange status chip, with no signal that they mean the same thing.
 *
 * jsdom has no CSS engine and never applies a component's scoped stylesheet, so
 * this invariant cannot be observed from a mounted component — it is read off
 * the source, the same pattern as wideDockIconSize.css.test.ts.
 */

const shareCenter = readFileSync(
  join(__dirname, '..', '..', 'share', 'ShareCenterPanel.vue'),
  'utf8',
)
const searchDrawer = readFileSync(
  join(__dirname, '..', 'SessionSearchDrawer.vue'),
  'utf8',
)
// The label key is chosen where the row is normalised, not in the template.
const shareCenterComposable = readFileSync(
  join(__dirname, '..', '..', '..', 'composables', 'useShareCenter.ts'),
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
  it('renders the share-centre badge with the session-search styling', () => {
    const shared = ruleDecls(shareCenter, '.share-badge--warn')
    const search = ruleDecls(searchDrawer, '.session-search-item-archived')

    expect(shared, '.share-badge--warn rule must exist').not.toBeNull()
    expect(
      search,
      '.session-search-item-archived rule must exist',
    ).not.toBeNull()

    expect(
      normalise(shared!),
      'the two archived badges must use identical visual declarations',
    ).toBe(normalise(search!))
  })

  it('labels both badges with the same text in both locales', () => {
    // The share centre reads its label from a key, so the guard is on the
    // RESOLVED string: the two keys live in different namespaces and would
    // otherwise drift into different words for the same state.
    const sharedKey = shareCenterComposable.match(
      /goneLabelKey:\s*archived\s*\?\s*'([^']+)'/,
    )?.[1]
    expect(sharedKey, 'the share centre must name an archived label key').toBe('shareCenter.sessionArchived')

    const searchText = searchDrawer.match(
      /class="session-search-item-archived"[^>]*>\{\{\s*t\('([^']+)'\)/,
    )?.[1]
    expect(searchText).toBe('sessionSearch.archived')

    const localeRoot = join(__dirname, '..', '..', '..', 'i18n', 'locales')
    const label = (file: string, ns: string, key: string) =>
      readFileSync(join(localeRoot, file), 'utf8').match(
        new RegExp(`^\\s*${ns}:\\s*\\{[\\s\\S]*?${key}:\\s*'([^']+)'`, 'm'),
      )?.[1]

    for (const file of ['zh.ts', 'en.ts']) {
      const sharedLabel = label(file, 'shareCenter', 'sessionArchived')
      const searchLabel = label(file, 'sessionSearch', 'archived')
      expect(sharedLabel, `${file}: shareCenter.sessionArchived`).toBeTruthy()
      expect(searchLabel, `${file}: sessionSearch.archived`).toBe(sharedLabel)
    }
  })
})
