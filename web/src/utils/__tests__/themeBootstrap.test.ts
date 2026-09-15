import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { THEMES, isDarkTheme, getThemeStatusBarColor } from '@/utils/themeMeta'

/**
 * Drift guards for the pre-CSS theme bootstrap scripts.
 *
 * `web/index.html` and `web/share.html` each inline a `DARK_IDS` list (and
 * index.html a `SB_COLORS` map) because they must resolve the theme
 * synchronously before any module — or CSS — loads. They cannot import
 * themeMeta.ts, so the data is duplicated and can silently drift.
 *
 * Drift is user-visible: a dark theme missing from DARK_IDS resolves to
 * `data-theme-base="light"`, so the first paint uses the light hljs palette
 * and the wrong background until App.vue mounts (issue #458). An entry for a
 * removed theme is dead weight and a sign the list was not maintained.
 *
 * These are source-sniffing tests (the scripts run in the browser, not jsdom),
 * the same pattern fontConfig.test.ts uses for its default-stack guard.
 */

const repoRoot = resolve(__dirname, '../../..')

/** Extract a `var NAME = <literal>;` initializer and evaluate it. */
function readBootstrapLiteral(html: string, name: string): unknown {
  const m = html.match(new RegExp(`var ${name} = (\\[[^\\]]*\\]|\\{[^}]*\\});`))
  if (!m) throw new Error(`var ${name} not found`)
  // The literals are plain JSON-ish (single-quoted strings), so swap the quotes
  // and parse rather than eval'ing a page-authored string.
  return JSON.parse(m[1].replace(/'/g, '"'))
}

function readHtml(file: string): string {
  return readFileSync(resolve(repoRoot, file), 'utf8')
}

const REGISTRY_DARK_IDS = THEMES.filter(t => t.dark).map(t => t.id)
const REGISTRY_IDS = THEMES.map(t => t.id)

describe.each(['index.html', 'share.html'])('%s theme bootstrap', (file) => {
  it('DARK_IDS exactly matches the dark themes in the registry', () => {
    const html = readHtml(file)
    const darkIds = readBootstrapLiteral(html, 'DARK_IDS') as string[]

    // Both directions matter: missing → light base for a dark theme (wrong
    // first paint); extra → stale entry for a theme that no longer exists.
    const missing = REGISTRY_DARK_IDS.filter(id => !darkIds.includes(id))
    const extra = darkIds.filter(id => !REGISTRY_IDS.includes(id))
    expect(missing, `${file} DARK_IDS is missing dark themes`).toEqual([])
    expect(extra, `${file} DARK_IDS lists unknown themes`).toEqual([])
    expect(darkIds.length).toBe(REGISTRY_DARK_IDS.length)
  })

  it('agrees with isDarkTheme for every registered theme', () => {
    const html = readHtml(file)
    const darkIds = new Set(readBootstrapLiteral(html, 'DARK_IDS') as string[])

    for (const theme of THEMES) {
      expect(darkIds.has(theme.id), `${file} DARK_IDS disagrees for ${theme.id}`).toBe(isDarkTheme(theme.id))
    }
  })
})

describe('index.html SB_COLORS', () => {
  it('maps every registered theme to its registry status-bar color', () => {
    const sbColors = readBootstrapLiteral(readHtml('index.html'), 'SB_COLORS') as Record<string, string>

    const missing = REGISTRY_IDS.filter(id => !(id in sbColors))
    const extra = Object.keys(sbColors).filter(id => !REGISTRY_IDS.includes(id))
    expect(missing, 'SB_COLORS is missing themes').toEqual([])
    expect(extra, 'SB_COLORS lists unknown themes').toEqual([])

    // A stale color is as bad as a missing one: the meta theme-color drives
    // the Android status bar before the app mounts.
    for (const theme of THEMES) {
      expect(sbColors[theme.id], `SB_COLORS mismatch for ${theme.id}`).toBe(getThemeStatusBarColor(theme.id))
    }
  })
})
