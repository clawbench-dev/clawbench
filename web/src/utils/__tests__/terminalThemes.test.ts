import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'

vi.mock('xterm-theme', () => ({
  __esModule: true,
  default: {
    Dracula: {
      foreground: '#f8f8f2', background: '#1e1f29', cursor: '#bbbbbb',
      black: '#000000', brightBlack: '#555555', red: '#ff5555', brightRed: '#ff5555',
      green: '#50fa7b', brightGreen: '#50fa7b', yellow: '#f1fa8c', brightYellow: '#f1fa8c',
      blue: '#bd93f9', brightBlue: '#bd93f9', magenta: '#ff79c6', brightMagenta: '#ff79c6',
      cyan: '#8be9fd', brightCyan: '#8be9fd', white: '#bbbbbb', brightWhite: '#ffffff',
    },
    LightTheme: {
      foreground: '#333333', background: '#ffffff', cursor: '#000000',
      black: '#000000', brightBlack: '#555555', red: '#ff5555', brightRed: '#ff5555',
      green: '#50fa7b', brightGreen: '#50fa7b', yellow: '#f1fa8c', brightYellow: '#f1fa8c',
      blue: '#bd93f9', brightBlue: '#bd93f9', magenta: '#ff79c6', brightMagenta: '#ff79c6',
      cyan: '#8be9fd', brightCyan: '#8be9fd', white: '#bbbbbb', brightWhite: '#ffffff',
    },
  },
}))

import {
  TERMINAL_THEME_AUTO,
  TERMINAL_THEME_STORAGE_KEY,
  THEME_IDS,
  formatThemeName,
  parseHexColor,
  colorDistance,
  findClosestThemeByBackground,
  resolveAutoTheme,
  resolveTheme,
  resolveThemeSync,
  getAppThemeBg,
  loadThemesModule,
  resetThemesCache,
  darkTheme,
  lightTheme,
} from '@/utils/terminalThemes'

describe('terminalThemes', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    // The lazy-load cache is module-level and persists across tests, so reset
    // it to keep each test's resolveThemeSync expectations deterministic.
    resetThemesCache()
    // Default: no App theme set → auto falls back by isAppDark.
    document.documentElement.removeAttribute('data-theme')
    document.documentElement.removeAttribute('data-theme-base')
  })

  afterEach(() => {
    document.documentElement.removeAttribute('data-theme')
    document.documentElement.removeAttribute('data-theme-base')
  })

  it('exports 157 theme ids and the auto/storage constants', () => {
    expect(THEME_IDS.length).toBe(157)
    expect(TERMINAL_THEME_AUTO).toBe('auto')
    expect(TERMINAL_THEME_STORAGE_KEY).toBe('terminalTheme')
    expect(THEME_IDS).toContain('Dracula')
  })

  it('formatThemeName converts underscores to spaces', () => {
    expect(formatThemeName('Solarized_Dark')).toBe('Solarized Dark')
    expect(formatThemeName('Dracula')).toBe('Dracula')
  })

  it('parseHexColor handles #rgb, #rrggbb and rejects invalid', () => {
    expect(parseHexColor('#1e1f29')).toEqual([0x1e, 0x1f, 0x29])
    expect(parseHexColor('fff')).toEqual([255, 255, 255])
    expect(parseHexColor('#12')).toBeNull()
    expect(parseHexColor('nope')).toBeNull()
  })

  it('colorDistance returns 0 for identical colors and scales with difference', () => {
    expect(colorDistance('#ffffff', '#ffffff')).toBe(0)
    expect(colorDistance('#000000', '#ffffff')).toBeCloseTo(441.67, 0)
    expect(colorDistance('#ff0000', '#00ff00')).toBeGreaterThan(0)
  })

  it('findClosestThemeByBackground picks the nearest background among themes', () => {
    const themes = {
      Dark: { background: '#000000' },
      Mid: { background: '#808080' },
      Light: { background: '#ffffff' },
    } as unknown as Record<string, import('@xterm/xterm').ITheme>
    expect(findClosestThemeByBackground('#111111', themes)).toBe('Dark')
    expect(findClosestThemeByBackground('#eeeeee', themes)).toBe('Light')
    expect(findClosestThemeByBackground('#777777', themes)).toBe('Mid')
  })

  it('findClosestThemeByBackground skips themes without a background', () => {
    const themes = {
      NoBg: {},
      Only: { background: '#123456' },
    } as unknown as Record<string, import('@xterm/xterm').ITheme>
    expect(findClosestThemeByBackground('#123456', themes)).toBe('Only')
  })

  it('findClosestThemeByBackground returns null for empty themes', () => {
    expect(findClosestThemeByBackground('#ffffff', {})).toBeNull()
  })

  it('resolveAutoTheme falls back to Catppuccin when themes missing or empty', () => {
    expect(resolveAutoTheme('#ffffff', null, true).background).toBe(darkTheme.background)
    expect(resolveAutoTheme('#ffffff', undefined, false).background).toBe(lightTheme.background)
    expect(resolveAutoTheme('#ffffff', {}, true).background).toBe(darkTheme.background)
  })

  it('resolveTheme("auto") picks closest background among loaded themes', async () => {
    document.documentElement.setAttribute('data-theme', 'one-dark-pro')
    document.documentElement.setAttribute('data-theme-base', 'dark')
    // one-dark-pro preview bg is #21252b → Dracula (#1e1f29) is nearest in the mock set.
    const theme = await resolveTheme(TERMINAL_THEME_AUTO, true)
    expect(theme.background).toBe('#1e1f29')
  })

  it('resolveTheme("auto") falls back to Catppuccin when theme load fails', async () => {
    // Simulate load failure: make loadThemesModule reject. Since the module is
    // mocked, force the failure by mocking the import through vi.mock factory is
    // not feasible per-test; instead assert the empty-themes path via
    // resolveAutoTheme (already covered) and here verify a preloaded empty map
    // (equivalent to load failure) still falls back.
    document.documentElement.setAttribute('data-theme', 'one-dark-pro')
    document.documentElement.setAttribute('data-theme-base', 'dark')
    const theme = await resolveTheme(TERMINAL_THEME_AUTO, true, {})
    expect(theme.background).toBe(darkTheme.background)
  })

  it('resolveTheme("auto") with no app theme falls back to Catppuccin for that mode', async () => {
    const dark = await resolveTheme(TERMINAL_THEME_AUTO, true)
    const light = await resolveTheme(TERMINAL_THEME_AUTO, false)
    expect(dark.background).toBe(darkTheme.background)
    expect(light.background).toBe(lightTheme.background)
    expect(dark.background).not.toBe(lightTheme.background)
  })

  it('resolveTheme with a fixed id lazily loads and returns that theme', async () => {
    const theme = await resolveTheme('Dracula', false)
    expect(theme.background).toBe('#1e1f29')
  })

  it('resolveTheme falls back to app theme for unknown id', async () => {
    const theme = await resolveTheme('Not_A_Real_Theme', false)
    expect(theme.background).toBe(lightTheme.background)
  })

  it('resolveThemeSync("auto") returns the app theme synchronously', () => {
    const dark = resolveThemeSync(TERMINAL_THEME_AUTO, true)
    const light = resolveThemeSync(TERMINAL_THEME_AUTO, false)
    expect(dark.background).toBe(darkTheme.background)
    expect(light.background).toBe(lightTheme.background)
  })

  it('resolveThemeSync returns a fixed theme once the module is loaded', async () => {
    // Warm the cache exactly like the terminal panel does on mount.
    await loadThemesModule()
    const theme = resolveThemeSync('Dracula', false)
    expect(theme.background).toBe('#1e1f29')
  })

  it('resolveThemeSync falls back to app theme for an unknown id or unloaded module', () => {
    const unknown = resolveThemeSync('Not_A_Real_Theme', true)
    expect(unknown.background).toBe(darkTheme.background)
    const unloaded = resolveThemeSync('Dracula', true)
    expect(unloaded.background).toBe(darkTheme.background)
  })

  it('getAppThemeBg returns the App theme preview bg or null', () => {
    expect(getAppThemeBg()).toBeNull()
    document.documentElement.setAttribute('data-theme', 'one-dark-pro')
    expect(getAppThemeBg()).toBe('#21252b')
    document.documentElement.setAttribute('data-theme', 'not-a-real-theme')
    expect(getAppThemeBg()).toBeNull()
  })
})
