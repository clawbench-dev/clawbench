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
    OneHalfLight: {
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
  SORTED_THEME_IDS,
  getThemeBackground,
  formatThemeName,
  parseHexColor,
  colorDistance,
  findClosestThemeByBackground,
  resolveAutoTheme,
  resolveAutoThemeSync,
  resolveTheme,
  resolveThemeSync,
  getAppThemeBg,
  loadThemesModule,
  resetThemesCache,
  buildTerminalThemePreviews,
  ensureVisibleSelection,
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

  it('exports the curated 16 theme ids and the auto/storage constants', () => {
    expect(THEME_IDS.length).toBe(16)
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

  it('resolveAutoTheme only picks themes that are in the curated list', () => {
    // 包里还有 140 多套没收录的配色，「跟随应用」不能自己用上去——否则
    // 用户在主题菜单里根本看不到、也选不回当前这套颜色。
    const themes = {
      NotListed: { background: '#0f0f0f', foreground: '#eeeeee' },
      Dracula: { background: '#1e1f29', foreground: '#f8f8f2' },
    } as unknown as Record<string, import('@xterm/xterm').ITheme>
    // NotListed 的背景离目标更近，但不是精选主题 → 让位给 Dracula。
    expect(resolveAutoTheme('#0f0f0f', themes, true).background).toBe('#1e1f29')
  })

  it('resolveAutoTheme skips a listed theme whose text is unreadable', () => {
    const themes = {
      // 正文与背景几乎同色 → 不能选，回落到 Catppuccin。
      Dracula: { background: '#1e1f29', foreground: '#1e1f2a' },
    } as unknown as Record<string, import('@xterm/xterm').ITheme>
    const picked = resolveAutoTheme('#1e1f29', themes, true).background
    expect(picked).toBe(darkTheme.background)
    expect(picked).not.toBe('#1e1f29')
  })

  it('resolveAutoTheme keeps a listed theme that only declares a background', () => {
    // 只给背景色时无从判断可读性，按原样参与匹配（测试夹具常见形态）。
    const theme = resolveAutoTheme('#101010', { Dracula: { background: '#101010' } }, true)
    expect(theme.background).toBe('#101010')
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

  it('resolveAutoThemeSync matches closest background once themes are loaded', async () => {
    document.documentElement.setAttribute('data-theme', 'one-dark-pro')
    document.documentElement.setAttribute('data-theme-base', 'dark')
    // Warm the module cache (mimics onMounted applyTheme).
    await loadThemesModule()
    // one-dark-pro preview bg is #21252b → Dracula (#1e1f29) is nearest.
    expect(resolveAutoThemeSync(true).background).toBe('#1e1f29')
  })

  it('resolveAutoThemeSync falls back to Catppuccin when themes not loaded', () => {
    document.documentElement.setAttribute('data-theme', 'one-dark-pro')
    document.documentElement.setAttribute('data-theme-base', 'dark')
    // Cache is empty (reset in beforeEach) → cannot match synchronously.
    expect(resolveAutoThemeSync(true).background).toBe(darkTheme.background)
  })

  it('resolveAutoThemeSync falls back to Catppuccin when no App theme set', async () => {
    // No data-theme → getAppThemeBg() returns null → fallback by isAppDark.
    await loadThemesModule()
    expect(resolveAutoThemeSync(true).background).toBe(darkTheme.background)
    expect(resolveAutoThemeSync(false).background).toBe(lightTheme.background)
  })

  describe('ensureVisibleSelection', () => {
    /**
     * 复刻 xterm ThemeService 的选区上色：selectionBackground 与 background
     * blend 后写入 CSS。用于断言「选区与背景是否真的分得开」，而不是只断言字段被赋值。
     */
    function blendedSelectionCss(theme: import('@xterm/xterm').ITheme): string {
      const bg = parseHexColor(theme.background!)!
      const raw = theme.selectionBackground!.replace('#', '')
      const fg = [
        parseInt(raw.slice(0, 2), 16),
        parseInt(raw.slice(2, 4), 16),
        parseInt(raw.slice(4, 6), 16),
      ]
      const a = raw.length === 8 ? parseInt(raw.slice(6, 8), 16) / 255 : 1
      const mixed = bg.map((c, i) => Math.round(c + (fg[i] - c) * a))
      return '#' + mixed.map(c => c.toString(16).padStart(2, '0')).join('')
    }

    it('injects a dark selection on light backgrounds so it is visible', () => {
      const theme = ensureVisibleSelection({ background: '#fafafa', foreground: '#383a42' })
      expect(theme.selectionBackground).toBe('#00000040')
      // 核心诉求：叠色后必须与背景有可见差异（回归前的 xterm 默认是 #fcfcfc）。
      const blended = blendedSelectionCss(theme)
      expect(colorDistance(blended, '#fafafa')).toBeGreaterThan(40)
    })

    it('injects a light selection on dark backgrounds (matches xterm default)', () => {
      const theme = ensureVisibleSelection({ background: '#1e1f29', foreground: '#f8f8f2' })
      expect(theme.selectionBackground).toBe('#ffffff4d')
      expect(colorDistance(blendedSelectionCss(theme), '#1e1f29')).toBeGreaterThan(40)
    })

    it('keeps an existing selectionBackground untouched', () => {
      const original = { background: '#eff1f5', selectionBackground: '#acb0be66' }
      const theme = ensureVisibleSelection(original)
      expect(theme.selectionBackground).toBe('#acb0be66')
      expect(theme).toBe(original)
    })

    it('does not mutate the input theme (module cache must stay clean)', () => {
      const original: import('@xterm/xterm').ITheme = { background: '#fafafa' }
      ensureVisibleSelection(original)
      expect(original.selectionBackground).toBeUndefined()
    })

    it('treats a missing/unparseable background as dark', () => {
      expect(ensureVisibleSelection({}).selectionBackground).toBe('#ffffff4d')
      expect(ensureVisibleSelection({ background: 'nope' }).selectionBackground).toBe('#ffffff4d')
    })

    it('is idempotent for the same input', () => {
      const original = { background: '#fafafa' }
      expect(ensureVisibleSelection(original)).toEqual(ensureVisibleSelection(original))
    })
  })

  describe('selection visibility through the resolve paths', () => {
    it('resolveAutoTheme injects a visible selection on the matched light preset', () => {
      // OneHalfLight (#ffffff) is nearest to a white app background and, like all
      // xterm-theme presets, carries no selectionBackground.
      const theme = resolveAutoTheme('#ffffff', { OneHalfLight: { background: '#ffffff' } }, false)
      expect(theme.selectionBackground).toBe('#00000040')
    })

    it('resolveAutoTheme fallback constants keep their own visible selection', () => {
      expect(resolveAutoTheme('#ffffff', null, false).selectionBackground).toBe(lightTheme.selectionBackground)
      expect(resolveAutoTheme('#ffffff', null, true).selectionBackground).toBe(darkTheme.selectionBackground)
    })

    it('resolveThemeSync injects a selection for a fixed preset', async () => {
      await loadThemesModule()
      expect(resolveThemeSync('Dracula', true).selectionBackground).toBe('#ffffff4d')
    })

    it('resolveTheme injects a selection for a fixed preset', async () => {
      const theme = await resolveTheme('OneHalfLight', false)
      expect(theme.selectionBackground).toBe('#00000040')
    })

    it('does not pollute the module cache with injected values', async () => {
      const themes = await loadThemesModule()
      resolveThemeSync('Dracula', true)
      expect(themes.Dracula.selectionBackground).toBeUndefined()
    })
  })

  describe('SORTED_THEME_IDS', () => {
    it('contains the same ids as THEME_IDS without duplicates', () => {
      expect(SORTED_THEME_IDS).toHaveLength(THEME_IDS.length)
      expect([...SORTED_THEME_IDS].sort()).toEqual([...THEME_IDS].sort())
      expect(new Set(SORTED_THEME_IDS).size).toBe(SORTED_THEME_IDS.length)
    })

    it('orders ids from light background to dark background', () => {
      // Known light themes must appear before known dark themes.
      const lightOnes = ['Terminal_Basic', 'OneHalfLight', 'Solarized_Light', 'Material']
      const darkOnes = ['Dracula', 'Gruvbox_Dark', 'ayu', 'SpaceGray']
      const lightIdx = lightOnes.map(id => SORTED_THEME_IDS.indexOf(id))
      const darkIdx = darkOnes.map(id => SORTED_THEME_IDS.indexOf(id))
      for (const li of lightIdx) {
        for (const di of darkIdx) {
          expect(li).toBeGreaterThanOrEqual(0)
          expect(di).toBeGreaterThanOrEqual(0)
          expect(li).toBeLessThan(di)
        }
      }
    })

    it('is monotonically non-increasing in background luminance', () => {
      const luminance = (hex: string): number => {
        const [r, g, b] = parseHexColor(hex)!
        return 0.299 * r + 0.587 * g + 0.114 * b
      }
      // Every known-background id must be in non-increasing luminance order.
      for (let i = 1; i < SORTED_THEME_IDS.length; i++) {
        const prev = getThemeBackground(SORTED_THEME_IDS[i - 1])
        const cur = getThemeBackground(SORTED_THEME_IDS[i])
        // `default` (no background) is always last — once reached, the rest are nulls.
        if (cur == null) {
          expect(prev).not.toBeNull()
          continue
        }
        if (prev == null) continue
        expect(luminance(cur)).toBeLessThanOrEqual(luminance(prev))
      }
    })
  })

  describe('buildTerminalThemePreviews', () => {
    it('includes an auto entry without a theme', () => {
      const map = buildTerminalThemePreviews(null)
      expect(map[TERMINAL_THEME_AUTO]).toEqual({ type: 'terminal', themeId: TERMINAL_THEME_AUTO })
      expect(map[TERMINAL_THEME_AUTO].theme).toBeUndefined()
    })

    it('returns auto + one key per sorted theme id', () => {
      const map = buildTerminalThemePreviews(null)
      expect(Object.keys(map)).toHaveLength(SORTED_THEME_IDS.length + 1)
      for (const id of SORTED_THEME_IDS) {
        expect(map[id]).toEqual({ type: 'terminal', themeId: id, theme: undefined })
      }
    })

    it('wires loaded themes to each entry and none to auto', () => {
      const themes = {
        Dracula: { background: '#1e1f29' },
        OneHalfLight: { background: '#fafafa' },
      } as unknown as Record<string, import('@xterm/xterm').ITheme>
      const map = buildTerminalThemePreviews(themes)
      expect(map.Dracula.theme).toEqual({ background: '#1e1f29' })
      expect(map.OneHalfLight.theme).toEqual({ background: '#fafafa' })
      expect(map[TERMINAL_THEME_AUTO].theme).toBeUndefined()
    })

    it('leaves theme undefined for ids missing from the loaded map', () => {
      const map = buildTerminalThemePreviews({})
      expect(map.Dracula.theme).toBeUndefined()
    })
  })
})
