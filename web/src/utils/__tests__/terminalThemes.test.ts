import { describe, expect, it, vi, beforeEach } from 'vitest'

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
  },
}))

import {
  TERMINAL_THEME_AUTO,
  TERMINAL_THEME_STORAGE_KEY,
  THEME_IDS,
  formatThemeName,
  resolveTheme,
  resolveThemeSync,
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

  it('resolveTheme("auto") returns the app theme without loading xterm-theme', async () => {
    // assert auto path returns static theme; lazy load module is not called because
    // the 'xterm-theme' import is mocked but we assert via the returned value:
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
})
