import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import {
  DARK_THEME_IDS,
  DEFAULT_LIGHT_THEME_ID,
  DEFAULT_THEME_ID,
  isDarkThemeId,
  normalizeThemeId,
} from './theme'

/**
 * The desktop shell cannot import `web/src/utils/themeMeta.ts`, and the login
 * page cannot import either (it is a standalone HTML file that must resolve
 * colours before any module loads). So the dark-theme list is duplicated in
 * three places and can silently drift.
 *
 * Drift is user-visible: a dark theme missing from a login page's DARK_IDS
 * resolves to `data-theme-base="light"`, so the page paints light-theme
 * colours for a dark theme.
 */

const repoRoot = resolve(__dirname, '../../..')

function readRepoFile(rel: string): string {
  return readFileSync(resolve(repoRoot, rel), 'utf8')
}

/** Extract the inline `var DARK_IDS = [...]` literal from a login page. */
function readLoginDarkIds(rel: string): string[] {
  const html = readRepoFile(rel)
  const m = html.match(/var DARK_IDS = \[([^\]]*)\]/)
  if (!m) throw new Error(`${rel}: no DARK_IDS literal found`)
  return [...m[1].matchAll(/'([^']+)'/g)].map((x) => x[1])
}

/** Extract the inline `var LIGHT_IDS = [...]` literal from a login page. */
function readLoginLightIds(rel: string): string[] {
  const html = readRepoFile(rel)
  const m = html.match(/var LIGHT_IDS = \[([^\]]*)\]/)
  if (!m) throw new Error(`${rel}: no LIGHT_IDS literal found`)
  return [...m[1].matchAll(/'([^']+)'/g)].map((x) => x[1])
}

/**
 * The light theme IDs from the authoritative registry, derived from
 * web/src/utils/themeMeta.ts so this test does not hardcode a second copy.
 */
function registryLightThemeIds(): string[] {
  const src = readRepoFile('web/src/utils/themeMeta.ts')
  const body = src.match(/export const THEMES[^=]*=\s*\[(.*?)\n\]/s)?.[1]
  if (!body) throw new Error('themeMeta.ts: THEMES array not found')
  return [...body.matchAll(/id:\s*'([^']+)',\s*dark:\s*false/g)].map((x) => x[1])
}

describe('DARK_THEME_IDS registry', () => {
  it('contains no duplicates', () => {
    expect(new Set(DARK_THEME_IDS).size).toBe(DARK_THEME_IDS.length)
  })

  it('includes the default theme, and the default is dark', () => {
    // If the default ever became a light theme, a first launch would paint a
    // light login page while the app defaults to dark.
    expect(DARK_THEME_IDS).toContain(DEFAULT_THEME_ID)
    expect(isDarkThemeId(DEFAULT_THEME_ID)).toBe(true)
  })

  it('classifies an unknown ID as light rather than throwing', () => {
    expect(isDarkThemeId('not-a-theme')).toBe(false)
    expect(isDarkThemeId('')).toBe(false)
  })
})

describe('normalizeThemeId', () => {
  it('maps legacy collapsed values onto a real theme ID', () => {
    // Older builds persisted 'dark'/'light'. Those match no [data-theme] rule,
    // so every colour variable stayed undefined and the login page rendered
    // with transparent inputs and buttons.
    expect(normalizeThemeId('dark')).toBe(DEFAULT_THEME_ID)
    expect(normalizeThemeId('light')).toBe(DEFAULT_LIGHT_THEME_ID)
  })

  it('falls back to the default for empty/missing values', () => {
    // Fresh install: nothing has called setTheme, so the stored value is unset.
    expect(normalizeThemeId(undefined)).toBe(DEFAULT_THEME_ID)
    expect(normalizeThemeId(null)).toBe(DEFAULT_THEME_ID)
    expect(normalizeThemeId('')).toBe(DEFAULT_THEME_ID)
  })

  it('passes real theme IDs through unchanged', () => {
    expect(normalizeThemeId('github-dark')).toBe('github-dark')
    expect(normalizeThemeId('nord')).toBe('nord')
    expect(normalizeThemeId('catppuccin-latte')).toBe('catppuccin-latte')
  })

  it('produces a dark default for the dark legacy value', () => {
    // A dark-mode user must not be flipped to a light login page.
    expect(isDarkThemeId(normalizeThemeId('dark'))).toBe(true)
    expect(isDarkThemeId(normalizeThemeId(undefined))).toBe(true)
  })

  it('produces a light default for the light legacy value', () => {
    expect(isDarkThemeId(normalizeThemeId('light'))).toBe(false)
  })
})

describe('login page DARK_IDS stays in sync', () => {
  // Both login pages inline this list; neither can import the registry.
  const pages = [
    'desktop/assets/login.html',
    'android/app/src/main/assets/login.html',
  ]

  for (const page of pages) {
    it(`${page} lists exactly the registry's dark themes`, () => {
      const ids = readLoginDarkIds(page)
      const missing = DARK_THEME_IDS.filter((id) => !ids.includes(id))
      const extra = ids.filter((id) => !DARK_THEME_IDS.includes(id))
      expect(missing, `${page} is missing dark themes`).toEqual([])
      expect(extra, `${page} lists themes not in the registry`).toEqual([])
      expect(ids.length).toBe(DARK_THEME_IDS.length)
    })

    it(`${page} agrees on the light/dark split for every ID`, () => {
      const ids = new Set(readLoginDarkIds(page))
      // A theme is dark in the page iff it is dark in the registry.
      for (const id of DARK_THEME_IDS) {
        expect(ids.has(id), `${page} disagrees for dark theme ${id}`).toBe(true)
      }
      // Spot-check a light theme is absent from DARK_IDS.
      expect(ids.has('github-light')).toBe(false)
      expect(ids.has('catppuccin-latte')).toBe(false)
    })

    it(`${page} LIGHT_IDS lists exactly the registry's light themes`, () => {
      // The page validates the incoming theme against DARK_IDS ∪ LIGHT_IDS and
      // substitutes a default when neither matches. A light theme missing from
      // LIGHT_IDS would therefore be silently replaced by the dark default,
      // flipping a light-mode user's login page to dark.
      const expected = registryLightThemeIds()
      const ids = readLoginLightIds(page)
      const missing = expected.filter((id) => !ids.includes(id))
      const extra = ids.filter((id) => !expected.includes(id))
      expect(missing, `${page} LIGHT_IDS is missing light themes`).toEqual([])
      expect(extra, `${page} LIGHT_IDS lists themes not in the registry`).toEqual([])
      expect(ids.length).toBe(expected.length)
    })

    it(`${page} DARK_IDS and LIGHT_IDS do not overlap`, () => {
      const dark = new Set(readLoginDarkIds(page))
      const overlap = readLoginLightIds(page).filter((id) => dark.has(id))
      expect(overlap, `${page} lists an ID as both dark and light`).toEqual([])
    })
  }
})

/**
 * Parse the `[data-theme="<id>"] { ... }` blocks of a login page into
 * `{ themeId: { varName: value } }`, preserving declaration order.
 *
 * Both login pages inline the whole palette (they must paint before any module
 * loads), so the two copies can drift. This is the parser both drift checks
 * below share.
 */
function parseLoginThemes(rel: string): Map<string, Array<[string, string]>> {
  const css = readRepoFile(rel).match(/<style>([\s\S]*?)<\/style>/)?.[1]
  if (!css) throw new Error(`${rel}: no <style> block found`)
  const out = new Map<string, Array<[string, string]>>()
  const blockRe = /\[data-theme="([^"]+)"\]\s*\{([\s\S]*?)\n\s*\}/g
  for (const m of css.matchAll(blockRe)) {
    const decls: Array<[string, string]> = []
    for (const d of m[2].matchAll(/--([a-z0-9-]+)\s*:\s*([^;]+);/g)) {
      decls.push([d[1], d[2].trim().toLowerCase()])
    }
    out.set(m[1], decls)
  }
  return out
}

/** `#rrggbb` -> `"r, g, b"`, matching the --accent-rgb literal format. */
function hexToRgbTriplet(hex: string): string {
  const h = hex.replace('#', '')
  return `${parseInt(h.slice(0, 2), 16)}, ${parseInt(h.slice(2, 4), 16)}, ${parseInt(h.slice(4, 6), 16)}`
}

// The two login pages duplicate the entire 36-theme palette. They are separate
// files that cannot share code (each must resolve colours before any module
// loads), so the copies can drift silently — and did: six themes had an
// --accent-rgb that belonged to a DIFFERENT theme, and dracula had none at all.
// Because --accent-rgb drives the focus ring and button glow, the symptom was a
// subtly wrong accent on those themes rather than an obvious break.
describe('login page palettes agree between desktop and Android', () => {
  const DESKTOP = 'desktop/assets/login.html'
  const ANDROID = 'android/app/src/main/assets/login.html'

  it('declares the same theme IDs', () => {
    const d = [...parseLoginThemes(DESKTOP).keys()].sort()
    const a = [...parseLoginThemes(ANDROID).keys()].sort()
    expect(d).toEqual(a)
  })

  it('declares identical values for every variable in every theme', () => {
    const d = parseLoginThemes(DESKTOP)
    const a = parseLoginThemes(ANDROID)
    const mismatches: string[] = []
    for (const [theme, decls] of d) {
      const other = new Map(a.get(theme) ?? [])
      for (const [name, value] of decls) {
        if (other.get(name) !== value) {
          mismatches.push(`${theme} ${name}: desktop=${value} android=${other.get(name)}`)
        }
      }
    }
    expect(mismatches).toEqual([])
  })

  it('declares the variables in the same order', () => {
    // Order is not cosmetic here: the six-theme drift happened because an
    // --accent-rgb line was moved next to the wrong neighbour while editing.
    const d = parseLoginThemes(DESKTOP)
    const a = parseLoginThemes(ANDROID)
    const orderDiffs: string[] = []
    for (const [theme, decls] of d) {
      const mine = decls.map(([n]) => n).join(',')
      const theirs = (a.get(theme) ?? []).map(([n]) => n).join(',')
      if (mine !== theirs) orderDiffs.push(`${theme}: desktop=[${mine}] android=[${theirs}]`)
    }
    expect(orderDiffs).toEqual([])
  })

  it('declares --accent-rgb exactly once per theme, matching that theme accent', () => {
    // The value must be the theme's own accent-color in rgb triplet form. This
    // is the invariant the drift violated: a copied-from-a-neighbour value
    // still LOOKS plausible, so nothing else would catch it.
    const problems: string[] = []
    for (const page of [DESKTOP, ANDROID]) {
      for (const [theme, decls] of parseLoginThemes(page)) {
        const map = new Map(decls)
        const count = decls.filter(([n]) => n === 'accent-rgb').length
        if (count !== 1) {
          problems.push(`${page} ${theme}: --accent-rgb declared ${count} time(s)`)
          continue
        }
        const accent = map.get('accent-color')
        if (!accent) {
          problems.push(`${page} ${theme}: no --accent-color to check against`)
          continue
        }
        // dark-plus intentionally uses the brand colour, not the accent.
        if (theme === 'dark-plus') continue
        const expected = hexToRgbTriplet(accent)
        if (map.get('accent-rgb') !== expected) {
          problems.push(`${page} ${theme}: --accent-rgb=${map.get('accent-rgb')} but --accent-color=${accent} (expected ${expected})`)
        }
      }
    }
    expect(problems).toEqual([])
  })

  it('declares --accent-rgb inside a theme block, never on a shared rule', () => {
    // A stray declaration on `body` inherits into every element and silently
    // overrides the per-theme value for anything that does not set its own.
    const problems: string[] = []
    for (const page of [DESKTOP, ANDROID]) {
      const css = readRepoFile(page).match(/<style>([\s\S]*?)<\/style>/)?.[1] ?? ''
      const withoutThemes = css.replace(/\[data-theme="[^"]+"\]\s*\{[\s\S]*?\n\s*\}/g, '')
      for (const line of withoutThemes.split('\n')) {
        if (line.trim().startsWith('--accent-rgb')) problems.push(`${page}: ${line.trim()}`)
      }
    }
    expect(problems).toEqual([])
  })
})
