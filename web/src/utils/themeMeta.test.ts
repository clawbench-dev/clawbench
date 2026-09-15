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
  buildThemePalette,
  onSystemColorSchemeChange,
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
  // The visibilitychange tests redefine this; drop the override so later
  // files/describe blocks see jsdom's real value.
  delete (document as unknown as Record<string, unknown>).visibilityState
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

describe('onSystemColorSchemeChange', () => {
  /** A matchMedia stub whose listeners can be fired and whose removal is observable. */
  function installControllableMatchMedia(opts: { legacyAddListener?: boolean } = {}) {
    const listeners = new Set<() => void>()
    const removed: Array<() => void> = []
    let addedViaLegacy = 0
    const mql: Record<string, unknown> = {
      matches: false,
      media: '(prefers-color-scheme: dark)',
      onchange: null,
      addEventListener: (_: string, cb: () => void) => { listeners.add(cb) },
      removeEventListener: (_: string, cb: () => void) => { listeners.delete(cb); removed.push(cb) },
      dispatchEvent: vi.fn(),
    }
    if (opts.legacyAddListener) {
      delete mql.addEventListener
      delete mql.removeEventListener
      mql.addListener = (cb: () => void) => { listeners.add(cb); addedViaLegacy++ }
      mql.removeListener = (cb: () => void) => { listeners.delete(cb); removed.push(cb) }
    }
    vi.stubGlobal('matchMedia', vi.fn(() => mql))
    return {
      /** Simulate the OS flipping light/dark. */
      fireChange: () => { for (const cb of [...listeners]) cb() },
      listenerCount: () => listeners.size,
      removedCount: () => removed.length,
      legacyAdds: () => addedViaLegacy,
    }
  }

  it('invokes the callback on a prefers-color-scheme change', () => {
    const mm = installControllableMatchMedia()
    const cb = vi.fn()
    onSystemColorSchemeChange(cb)
    expect(mm.listenerCount()).toBe(1)

    mm.fireChange()
    expect(cb).toHaveBeenCalledTimes(1)
  })

  it('falls back to the legacy addListener API (Safari < 14)', () => {
    const mm = installControllableMatchMedia({ legacyAddListener: true })
    const cb = vi.fn()
    onSystemColorSchemeChange(cb)

    expect(mm.legacyAdds()).toBe(1)
    mm.fireChange()
    expect(cb).toHaveBeenCalledTimes(1)
  })

  it('re-checks on visibilitychange only when the page becomes visible', () => {
    installControllableMatchMedia()
    const cb = vi.fn()
    onSystemColorSchemeChange(cb)

    // Hiding the page is not a signal — the OS value may not have changed yet.
    Object.defineProperty(document, 'visibilityState', { value: 'hidden', configurable: true })
    document.dispatchEvent(new Event('visibilitychange'))
    expect(cb).not.toHaveBeenCalled()

    // Resuming is: iOS PWAs never get a matchMedia change event for a scheme
    // flip that happened while suspended.
    Object.defineProperty(document, 'visibilityState', { value: 'visible', configurable: true })
    document.dispatchEvent(new Event('visibilitychange'))
    expect(cb).toHaveBeenCalledTimes(1)
  })

  it('re-checks on pageshow (bfcache restore skips visibilitychange)', () => {
    installControllableMatchMedia()
    const cb = vi.fn()
    onSystemColorSchemeChange(cb)

    window.dispatchEvent(new Event('pageshow'))
    expect(cb).toHaveBeenCalledTimes(1)
  })

  it('unsubscribe detaches the matchMedia, visibility and pageshow listeners', () => {
    const mm = installControllableMatchMedia()
    const cb = vi.fn()
    const stop = onSystemColorSchemeChange(cb)
    stop()

    expect(mm.listenerCount()).toBe(0)
    mm.fireChange()
    window.dispatchEvent(new Event('pageshow'))
    Object.defineProperty(document, 'visibilityState', { value: 'visible', configurable: true })
    document.dispatchEvent(new Event('visibilitychange'))
    expect(cb).not.toHaveBeenCalled()
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

describe('buildThemePalette', () => {
  it('builds theme palette from THEMES metadata', () => {
    const p = buildThemePalette('github-dark')
    expect(p.bg).toBe('#161b22')
    expect(p.text).toBe('#c9d1d9')
    expect(p.textSecondary).toBe('#8b949e')
    expect(p.accent).toBe('#58a6ff')
  })

  it('falls back to github-dark for unknown theme', () => {
    const p = buildThemePalette('not-a-theme')
    expect(p.bg).toBe('#161b22')
    expect(p.text).toBe('#c9d1d9')
    expect(p.textSecondary).toBe('#8b949e')
    expect(p.accent).toBe('#58a6ff')
  })

  it('reads textSecondary from the --text-secondary CSS variable when available', () => {
    const stub = vi.spyOn(window, 'getComputedStyle').mockReturnValue({
      getPropertyValue: (name: string) => (name === '--text-secondary' ? '#123456' : ''),
    } as CSSStyleDeclaration)
    try {
      expect(buildThemePalette('github-light').textSecondary).toBe('#123456')
    } finally {
      stub.mockRestore()
    }
  })
})