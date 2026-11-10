import { describe, expect, it } from 'vitest'
import xtermThemes from 'xterm-theme'
import type { ITheme } from '@xterm/xterm'
import {
  MIN_ANSI_CONTRAST,
  MIN_FOREGROUND_CONTRAST,
  THEME_IDS,
  contrastRatio,
  isReadableTheme,
} from '@/utils/terminalThemes'

/**
 * 主题列表的「能看清」守卫。
 *
 * 这里刻意不 mock xterm-theme：判据必须对着真实配色数据算，否则改了常量
 * 或换了包版本都发现不了。列表本身是人工精选的经典款（不是全量），但每一条
 * 都必须过对比度门槛——加回低对比度项、或包更新后某个主题变糊，都会失败。
 */

const themes = xtermThemes as unknown as Record<string, ITheme>

/**
 * THEME_IDS 里唯一不在 xterm-theme 包中的条目：`default` 是列表里的占位项
 * （解析时回落 Catppuccin），没有自己的配色数据，因此不参与对比度校验。
 */
const PACKAGE_LESS_IDS = new Set(['default'])

describe('terminal theme readability', () => {
  it('contrastRatio matches the WCAG reference values', () => {
    expect(contrastRatio('#000000', '#ffffff')).toBeCloseTo(21, 5)
    expect(contrastRatio('#ffffff', '#ffffff')).toBeCloseTo(1, 5)
    // #767676 on white 是 WCAG 常用的 4.5:1 边界色。
    expect(contrastRatio('#767676', '#ffffff')).toBeCloseTo(4.54, 1)
    expect(contrastRatio('nope', '#ffffff')).toBeNull()
  })

  it('isReadableTheme rejects low-contrast foreground and invisible ANSI colors', () => {
    expect(
      isReadableTheme({ background: '#101010', foreground: '#2a2a2a' } as ITheme),
    ).toBe(false)
    // 正文够亮，但红色几乎与背景同色（git diff 的删除行会消失）。
    expect(
      isReadableTheme({
        background: '#ffffff',
        foreground: '#222222',
        red: '#fdfdfd',
      } as ITheme),
    ).toBe(false)
    // 缺前景/背景时无从判断，按可读处理（不拦）。
    expect(isReadableTheme({ background: '#101010' } as ITheme)).toBe(true)
  })

  it('every listed theme clears both contrast thresholds', () => {
    for (const id of THEME_IDS) {
      const theme = themes[id]
      if (!theme) continue
      const fg = contrastRatio(theme.foreground, theme.background)
      expect(fg, `${id} 的正文对比度`).not.toBeNull()
      expect(fg!, `${id} 的正文对比度`).toBeGreaterThanOrEqual(MIN_FOREGROUND_CONTRAST)
      for (const key of ['red', 'green', 'yellow', 'blue', 'magenta', 'cyan'] as const) {
        const ratio = contrastRatio(theme[key], theme.background)
        if (ratio == null) continue
        expect(ratio, `${id} 的 ${key} 对比度`).toBeGreaterThanOrEqual(MIN_ANSI_CONTRAST)
      }
    }
  })

  it('every listed id exists in xterm-theme', () => {
    for (const id of THEME_IDS) {
      if (PACKAGE_LESS_IDS.has(id)) continue
      expect(themes[id], `${id} 不在 xterm-theme 里`).toBeDefined()
    }
  })

  it('keeps the curated list small', () => {
    // 包里 156 套全列出来对用户没有意义（挑不过来，且不少配色本身看不清）。
    // 上限用来拦住「把整包倒回列表」这种改动；加一两套经典款不会触发。
    expect(THEME_IDS.length).toBeLessThanOrEqual(20)
  })

  it('still lists the classics users expect', () => {
    for (const id of ['Dracula', 'Gruvbox_Dark', 'OneHalfDark', 'Monokai_Soda', 'Solarized_Dark_Higher_Contrast', 'OneHalfLight', 'Solarized_Light']) {
      expect(THEME_IDS).toContain(id)
    }
  })
})
