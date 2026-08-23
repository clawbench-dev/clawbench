import { describe, expect, it, vi, afterEach } from 'vitest'
import {
  THEMES,
  THEME_IDS,
  STATUS_BAR_COLORS,
  THEME_PREVIEW_COLORS,
  isDarkTheme,
  getDefaultDarkTheme,
  getDefaultLightTheme,
  resolveThemeId,
  getThemeLabelKey,
  getThemeStatusBarColor,
  getThemePreviewColor,
  applyThemeAttributes,
} from '@/utils/themeMeta'

function stubMatchMedia(matches: boolean): void {
  vi.stubGlobal('matchMedia', (query: string) => ({
    matches,
    media: query,
    onchange: null,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }))
}

afterEach(() => {
  vi.unstubAllGlobals()
  document.documentElement.removeAttribute('data-theme')
  document.documentElement.removeAttribute('data-theme-base')
  document.documentElement.removeAttribute('data-hljs-theme')
})

describe('themeMeta registry', () => {
  it('derives every THEME_ID an entry in STATUS_BAR_COLORS and THEME_PREVIEW_COLORS', () => {
    for (const id of THEME_IDS) {
      expect(STATUS_BAR_COLORS[id], `missing status bar color for ${id}`).toBeTruthy()
      expect(THEME_PREVIEW_COLORS[id], `missing preview for ${id}`).toBeTruthy()
      const p = THEME_PREVIEW_COLORS[id]
      expect(p.bg, `${id} preview.bg`).toMatch(/^#[0-9a-fA-F]{6}$/)
      expect(p.text, `${id} preview.text`).toMatch(/^#[0-9a-fA-F]{6}$/)
      expect(p.accent, `${id} preview.accent`).toMatch(/^#[0-9a-fA-F]{6}$/)
    }
  })

  it('keeps THEME_IDS in sync with the registry order and count', () => {
    expect(THEME_IDS.length).toBe(THEMES.length)
    expect(THEME_IDS).toEqual(THEMES.map(t => t.id))
    expect(STATUS_BAR_COLORS).toEqual(Object.fromEntries(THEMES.map(t => [t.id, t.statusBar])))
    expect(THEME_PREVIEW_COLORS).toEqual(Object.fromEntries(THEMES.map(t => [t.id, t.preview])))
  })

  it('THEME_IDS contains no duplicates', () => {
    expect(new Set(THEME_IDS).size).toBe(THEME_IDS.length)
  })
})

describe('dark / light classification', () => {
  it('is exhaustive and mutually exclusive over all THEME_IDS', () => {
    const dark = THEME_IDS.filter(id => isDarkTheme(id))
    const light = THEME_IDS.filter(id => !isDarkTheme(id))
    expect(dark.length + light.length).toBe(THEME_IDS.length)
    expect(new Set(dark).has(getDefaultLightTheme())).toBe(false)
    expect(new Set(light).has(getDefaultDarkTheme())).toBe(false)
    // Every registry entry's dark flag agrees with isDarkTheme
    for (const t of THEMES) {
      expect(isDarkTheme(t.id)).toBe(t.dark)
    }
  })

  it('classifies the known defaults correctly', () => {
    expect(isDarkTheme('github-dark')).toBe(true)
    expect(isDarkTheme('github-light')).toBe(false)
    expect(isDarkTheme('ayu-dark')).toBe(true)
    expect(isDarkTheme('solarized-light')).toBe(false)
  })
})

describe('resolveThemeId', () => {
  it('resolves auto to the dark default when system prefers dark', () => {
    stubMatchMedia(true)
    expect(resolveThemeId('auto')).toBe(getDefaultDarkTheme())
  })

  it('resolves auto to the light default when system prefers light', () => {
    stubMatchMedia(false)
    expect(resolveThemeId('auto')).toBe(getDefaultLightTheme())
  })

  it('passes through a concrete theme id unchanged', () => {
    stubMatchMedia(true)
    expect(resolveThemeId('nord')).toBe('nord')
    expect(resolveThemeId('github-light')).toBe('github-light')
  })
})

describe('getThemeLabelKey', () => {
  it('derives the i18n key from the theme id', () => {
    expect(getThemeLabelKey('github-light')).toBe('settings.items.themeGithubLight')
    expect(getThemeLabelKey('one-dark-pro')).toBe('settings.items.themeOneDarkPro')
    expect(getThemeLabelKey('solarized-deep')).toBe('settings.items.themeSolarizedDeep')
    expect(getThemeLabelKey('high-contrast-dark')).toBe('settings.items.themeHighContrastDark')
  })

  it('matches every registry entry labelKey and is unique', () => {
    for (const t of THEMES) {
      expect(getThemeLabelKey(t.id)).toBe(t.labelKey)
    }
    expect(new Set(THEMES.map(t => t.labelKey)).size).toBe(THEMES.length)
  })
})

describe('status bar & preview lookups', () => {
  it('returns known colors for a real theme', () => {
    expect(getThemeStatusBarColor('github-dark')).toBe('#161b22')
    expect(getThemePreviewColor('dracula')).toEqual({ bg: '#21222c', text: '#f8f8f2', accent: '#bd93f9' })
  })

  it('falls back to github-dark status bar for unknown themes', () => {
    expect(getThemeStatusBarColor('not-a-theme')).toBe('#161b22')
  })

  it('returns null preview for unknown themes', () => {
    expect(getThemePreviewColor('not-a-theme')).toBeNull()
  })
})

describe('applyThemeAttributes', () => {
  it('sets data-theme, data-theme-base, data-hljs-theme and meta theme-color', () => {
    document.head.innerHTML = '<meta name="theme-color" content="">'
    applyThemeAttributes('github-dark')
    expect(document.documentElement.getAttribute('data-theme')).toBe('github-dark')
    expect(document.documentElement.getAttribute('data-theme-base')).toBe('dark')
    expect(document.documentElement.getAttribute('data-hljs-theme')).toBe('dark')
    expect(document.querySelector('meta[name="theme-color"]')?.getAttribute('content')).toBe('#161b22')
  })

  it('is idempotent across repeated calls', () => {
    document.head.innerHTML = '<meta name="theme-color" content="">'
    applyThemeAttributes('catppuccin-latte')
    applyThemeAttributes('catppuccin-latte')
    expect(document.documentElement.getAttribute('data-theme')).toBe('catppuccin-latte')
    expect(document.documentElement.getAttribute('data-theme-base')).toBe('light')
    expect(document.querySelector('meta[name="theme-color"]')?.getAttribute('content')).toBe('#e6e9ef')
  })
})