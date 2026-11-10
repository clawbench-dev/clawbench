import type { ITheme } from '@xterm/xterm'
import { getThemePreviewColor } from '@/utils/themeMeta'

export const TERMINAL_THEME_AUTO = 'auto'
export const TERMINAL_THEME_STORAGE_KEY = 'terminalTheme'

/**
 * 精选后的 16 个主题 id（构建期硬编码，不触发 xterm-theme 加载）。
 * 用于渲染选择列表。包里共有 156 套，全量列表太长且不少配色本身看不清，
 * 这里只留各家的经典款（都已通过 isReadableTheme 的对比度门槛）：
 *   深色：Dracula、Gruvbox Dark、One Half Dark、Monokai Soda、
 *         Solarized Dark（高对比版）、Tomorrow Night、ayu、Cobalt2、
 *         JetBrains Darcula、SpaceGray
 *   浅色：Material、One Half Light、Solarized Light、Tomorrow、Terminal Basic
 *   default：占位项，实际解析时回落到 Catppuccin（随 App 深/浅色）
 * 想加回某个主题：确认它能过 isReadableTheme，再补进 THEME_IDS 与
 * THEME_BACKGROUNDS 两处（顺序由背景亮度自动排，不用手排）。
 */
export const THEME_IDS: string[] = [
  // 深色
  'Dracula',
  'Gruvbox_Dark',
  'OneHalfDark',
  'Monokai_Soda',
  'Solarized_Dark_Higher_Contrast',
  'Tomorrow_Night',
  'ayu',
  'Cobalt2',
  'JetBrains_Darcula',
  'SpaceGray',
  // 浅色
  'Material',
  'OneHalfLight',
  'Solarized_Light',
  'Tomorrow',
  'Terminal_Basic',
  // 占位（回落 Catppuccin）
  'default',
]

/** THEME_IDS 的查找版，供 auto 模式判断「这个 id 是不是精选主题」。 */
const CURATED_THEME_IDS = new Set(THEME_IDS)

/** App 深色主题默认值（Catppuccin Mocha，迁移自 TerminalPanelContent.vue）。 */
export const darkTheme: ITheme = {
  background: '#1e1e2e', foreground: '#cdd6f4', cursor: '#f5e0dc', cursorAccent: '#1e1e2e',
  selectionBackground: '#585b7066', black: '#45475a', red: '#f38ba8', green: '#a6e3a1',
  yellow: '#f9e2af', blue: '#89b4fa', magenta: '#f5c2e7', cyan: '#94e2d5', white: '#bac2de',
  brightBlack: '#585b70', brightRed: '#f38ba8', brightGreen: '#a6e3a1', brightYellow: '#f9e2af',
  brightBlue: '#89b4fa', brightMagenta: '#f5c2e7', brightCyan: '#94e2d5', brightWhite: '#a6adc8',
}

/** App 浅色主题默认值（Catppuccin Latte，迁移自 TerminalPanelContent.vue）。 */
export const lightTheme: ITheme = {
  background: '#eff1f5', foreground: '#4c4f69', cursor: '#dc8a78', cursorAccent: '#eff1f5',
  selectionBackground: '#acb0be66', black: '#bcc0cc', red: '#d20f39', green: '#40a02b',
  yellow: '#df8e1d', blue: '#1e66f5', magenta: '#ea76cb', cyan: '#179299', white: '#4c4f69',
  brightBlack: '#9ca0b0', brightRed: '#d20f39', brightGreen: '#40a02b', brightYellow: '#df8e1d',
  brightBlue: '#1e66f5', brightMagenta: '#ea76cb', brightCyan: '#179299', brightWhite: '#6c6f85',
}

let cachedThemes: Record<string, ITheme> | null = null
let cachedThemesPromise: Promise<Record<string, ITheme>> | null = null

/** 动态 import 偶发失败（chunk 网络/缓存）时的最大重试次数。 */
const THEMES_LOAD_MAX_RETRIES = 3
/** 重试间隔。 */
const THEMES_LOAD_RETRY_DELAY_MS = 800

/**
 * 动态加载 xterm-theme 全部主题（懒加载，仅在使用时触发）。缓存结果避免重复 import。
 *
 * 加载失败时自动延迟重试（最多 THEMES_LOAD_MAX_RETRIES 次），让偶发的 chunk
 * 加载失败自愈——所有调用方（配置面板主题网格、终端调色板菜单）无需各自处理。
 */
export async function loadThemesModule(): Promise<Record<string, ITheme>> {
  if (cachedThemes) return cachedThemes
  if (cachedThemesPromise) return cachedThemesPromise
  cachedThemesPromise = doLoadThemes()
  try {
    return await cachedThemesPromise
  } finally {
    // 失败时清空 promise，允许下一次调用重新尝试
    cachedThemesPromise = null
  }
}

async function doLoadThemes(): Promise<Record<string, ITheme>> {
  let lastErr: unknown = null
  for (let attempt = 0; attempt <= THEMES_LOAD_MAX_RETRIES; attempt++) {
    try {
      const mod = await import('xterm-theme')
      cachedThemes = (mod.default ?? mod) as Record<string, ITheme>
      return cachedThemes
    } catch (e) {
      lastErr = e
      if (attempt < THEMES_LOAD_MAX_RETRIES) {
        await new Promise(r => setTimeout(r, THEMES_LOAD_RETRY_DELAY_MS))
      }
    }
  }
  throw lastErr
}

/** 清空已加载主题缓存（仅供测试使用）。 */
export function resetThemesCache(): void {
  cachedThemes = null
  cachedThemesPromise = null
}

// ── 背景色匹配（auto 主题跟随 App） ─────────────────────────────────────────

/** 解析 hex 颜色（#rgb / #rrggbb）为 [r, g, b]，无法解析返回 null。 */
export function parseHexColor(hex: string): [number, number, number] | null {
  let h = hex.trim().replace(/^#/, '')
  if (h.length === 3) h = h.split('').map(c => c + c).join('')
  if (h.length !== 6) return null
  const r = parseInt(h.slice(0, 2), 16)
  const g = parseInt(h.slice(2, 4), 16)
  const b = parseInt(h.slice(4, 6), 16)
  if ([r, g, b].some(Number.isNaN)) return null
  return [r, g, b]
}

/** RGB 欧氏距离（0~441，越小越接近）。 */
export function colorDistance(a: string, b: string): number {
  const ca = parseHexColor(a)
  const cb = parseHexColor(b)
  if (!ca || !cb) return Number.POSITIVE_INFINITY
  return Math.sqrt(
    (ca[0] - cb[0]) ** 2 + (ca[1] - cb[1]) ** 2 + (ca[2] - cb[2]) ** 2,
  )
}

// ── 可读性（对比度）────────────────────────────────────────────────────────

/** 正文与背景的最小对比度（WCAG AA 正文标准 4.5:1）。低于此值在 12px 等宽字下糊成一片。 */
export const MIN_FOREGROUND_CONTRAST = 4.5

/**
 * 常用 ANSI 色（红/绿/黄/蓝/紫/青）与背景的最小对比度。
 * 低于此值时 `ls`、`git diff` 这类彩色输出直接看不见，等同于没有输出。
 * 不检查 black / brightBlack：它们在多数主题里本就按背景色设计（注释、暗色块）。
 */
export const MIN_ANSI_CONTRAST = 1.6

/** 参与「彩色输出能不能看见」检查的 ANSI 槽位。 */
const READABLE_ANSI_KEYS = ['red', 'green', 'yellow', 'blue', 'magenta', 'cyan'] as const

/** 计算 WCAG 相对亮度（0 黑 ~ 1 白）。 */
function relativeLuminance(rgb: [number, number, number]): number {
  const [r, g, b] = rgb.map((v) => {
    const s = v / 255
    return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4
  })
  return 0.2126 * r + 0.7152 * g + 0.0722 * b
}

/**
 * 两色的 WCAG 对比度（1:1 ~ 21:1）。任一颜色不可解析 → null（无法判断）。
 */
export function contrastRatio(fg: string, bg: string): number | null {
  const a = parseHexColor(fg)
  const b = parseHexColor(bg)
  if (!a || !b) return null
  const la = relativeLuminance(a)
  const lb = relativeLuminance(b)
  const hi = Math.max(la, lb)
  const lo = Math.min(la, lb)
  return (hi + 0.05) / (lo + 0.05)
}

/**
 * 这套配色能不能看清：正文够不够亮，以及常用彩色输出会不会消失。
 *
 * 缺 foreground / background 时返回 true——判断不了就不拦（xterm 自带的
 * 默认黑白配色本身是可读的，而测试里也常只给背景色做匹配）。
 *
 * 同一个判据被两处共用：THEME_IDS 的筛选口径，以及 auto 模式挑主题时
 * 排除不可读项（否则「跟随应用」仍可能自动选到已从列表里删掉的那几个）。
 */
export function isReadableTheme(theme: ITheme): boolean {
  const bg = theme.background
  const fg = theme.foreground
  if (!bg || !fg) return true
  const fgRatio = contrastRatio(fg, bg)
  if (fgRatio == null) return true
  if (fgRatio < MIN_FOREGROUND_CONTRAST) return false
  for (const key of READABLE_ANSI_KEYS) {
    const color = theme[key]
    if (!color) continue
    const ratio = contrastRatio(color, bg)
    if (ratio != null && ratio < MIN_ANSI_CONTRAST) return false
  }
  return true
}

// ── 选区底色补齐 ────────────────────────────────────────────────────────────

/** 浅色背景下的选区底色（半透明黑，叠在 #fafafa 上约 #bbbbbb）。 */
const LIGHT_SELECTION_BACKGROUND = '#00000040'
/** 深色背景下的选区底色（半透明白，与 xterm 内置默认 rgba(255,255,255,0.3) 一致）。 */
const DARK_SELECTION_BACKGROUND = '#ffffff4d'
/** 判定背景为浅色的亮度阈值（0~255，与 isTerminalThemeDark 的 0.5 对齐）。 */
const LIGHT_BACKGROUND_MIN_LUMINANCE = 128

/**
 * 补齐选区底色：xterm-theme 的 156 个预设均未定义 selectionBackground，
 * 缺该字段时 xterm 回退到内置 rgba(255,255,255,0.3)——半透明白叠在浅色
 * 背景上（OneHalfLight #fafafa → #fcfcfc）与背景几乎同色，划选完全不可见。
 * 这里按背景明暗极性注入可见的选区色：浅底用半透明黑，深底保持半透明白。
 *
 * 已定义 selectionBackground 的主题（如 Catppuccin 常量）原样返回。
 * 返回浅拷贝，避免把注入值写进模块级 cachedThemes 污染缓存。
 */
export function ensureVisibleSelection(theme: ITheme): ITheme {
  if (theme.selectionBackground) return theme
  const isLight = backgroundLuminance(theme.background || '') >= LIGHT_BACKGROUND_MIN_LUMINANCE
  return {
    ...theme,
    selectionBackground: isLight ? LIGHT_SELECTION_BACKGROUND : DARK_SELECTION_BACKGROUND,
  }
}

/**
 * 在所有终端主题里找背景色与 targetBg 最接近的一个，返回其 id。
 * 主题无 background 或不可解析 → 跳过；themes 为空 → 返回 null。
 * accept 可进一步过滤候选（auto 用它排除不可读主题）。
 */
export function findClosestThemeByBackground(
  targetBg: string,
  themes: Record<string, ITheme>,
  accept?: (id: string, theme: ITheme) => boolean,
): string | null {
  let best: string | null = null
  let bestDist = Number.POSITIVE_INFINITY
  for (const [id, theme] of Object.entries(themes)) {
    const bg = theme.background
    if (!bg) continue
    if (accept && !accept(id, theme)) continue
    const dist = colorDistance(targetBg, bg)
    if (dist < bestDist) {
      bestDist = dist
      best = id
    }
  }
  return best
}

/**
 * 解析 auto 主题（跟随 App）：在精选主题里找背景色与当前 App 主题最接近的
 * 一个；themes 未加载或为空（懒加载失败）→ 回退 Catppuccin。
 *
 * 候选必须是 THEME_IDS 里的（包里还有 140 多套没收录的配色，若按背景色全量
 * 匹配，「跟随应用」会用上用户在菜单里看不见、也选不回来的颜色），并且要过
 * 可读性门槛。
 */
export function resolveAutoTheme(
  appThemeBg: string,
  themes: Record<string, ITheme> | null | undefined,
  isAppDark: boolean,
): ITheme {
  const id = themes
    ? findClosestThemeByBackground(
        appThemeBg,
        themes,
        (themeId, theme) => CURATED_THEME_IDS.has(themeId) && isReadableTheme(theme),
      )
    : null
  if (!id) return ensureVisibleSelection(isAppDark ? darkTheme : lightTheme)
  return ensureVisibleSelection(themes![id])
}

/** 把主题 id 转成展示名（下划线 → 空格）。 */
export function formatThemeName(id: string): string {
  return id.replace(/_/g, ' ')
}

/**
 * 解析最终主题。
 * - auto → 背景色最接近 App 主题的终端主题（优先用传入的 themes 匹配；
 *   未传入或为空 → 懒加载后匹配；加载失败 → 回退 Catppuccin）。
 * - 固定 id → 懒加载后返回该主题；未知/缺失 → 回退到按 isAppDark 的自动主题。
 */
export async function resolveTheme(
  selection: string,
  isAppDark: boolean,
  preloaded?: Record<string, ITheme> | null,
): Promise<ITheme> {
  if (selection === TERMINAL_THEME_AUTO) {
    const themes = preloaded ?? (await safeLoadThemes())
    const appThemeBg = getAppThemeBg()
    // 无 App 主题或主题未加载 → 回退 Catppuccin；否则匹配背景色最近的终端主题。
    if (!appThemeBg || !themes) return ensureVisibleSelection(isAppDark ? darkTheme : lightTheme)
    return resolveAutoTheme(appThemeBg, themes, isAppDark)
  }
  try {
    const themes = preloaded ?? (await loadThemesModule())
    const theme = themes[selection]
    if (theme) return ensureVisibleSelection(theme)
  } catch {
    // 加载失败或 id 缺失 → 回退
  }
  return ensureVisibleSelection(isAppDark ? darkTheme : lightTheme)
}

/** 懒加载 xterm-theme，失败返回 null（供 auto 匹配回退用）。 */
async function safeLoadThemes(): Promise<Record<string, ITheme> | null> {
  try {
    return await loadThemesModule()
  } catch {
    return null
  }
}

/** 当前 App 主题的背景色；无 data-theme 时返回 null（无法匹配）。 */
export function getAppThemeBg(): string | null {
  const appThemeId = document.documentElement.getAttribute('data-theme') || ''
  if (!appThemeId) return null
  const preview = getThemePreviewColor(appThemeId)
  return preview ? preview.bg : null
}

/**
 * 同步解析 auto 主题（跟随 App）：在已加载的终端主题里找背景色与当前 App
 * 主题最接近的一个；主题未加载（懒加载尚未完成/失败）或无 App 主题 →
 * 回退 Catppuccin。
 *
 * 用途：新建会话（xterm 实例创建是同步的）必须在打开瞬间拿到主题，
 * 不能 await 懒加载。与 resolveThemeSync 不同，auto 模式下已加载的主题
 * 也能被同步使用（模块级 cachedThemes），不依赖组件侧的 allThemes ref。
 */
export function resolveAutoThemeSync(isAppDark: boolean): ITheme {
  const appThemeBg = getAppThemeBg()
  if (!appThemeBg || !cachedThemes) return ensureVisibleSelection(isAppDark ? darkTheme : lightTheme)
  return resolveAutoTheme(appThemeBg, cachedThemes, isAppDark)
}

/**
 * 同步解析最终主题。仅在 xterm-theme 已加载（缓存命中）时可拿到固定主题；
 * 否则固定 id 回退到按 isAppDark 的自动主题。
 *
 * 用途：新建会话（xterm 实例创建是同步的）必须在打开瞬间拿到主题，
 * 不能 await 懒加载。配合 resolveTheme 在组件挂载时预热缓存即可。
 */
export function resolveThemeSync(selection: string, isAppDark: boolean): ITheme {
  if (selection === TERMINAL_THEME_AUTO) {
    return ensureVisibleSelection(isAppDark ? darkTheme : lightTheme)
  }
  const theme = cachedThemes?.[selection]
  if (theme) return ensureVisibleSelection(theme)
  return ensureVisibleSelection(isAppDark ? darkTheme : lightTheme)
}

/** 当前 App 是否为深色主题（同步判定）。 */
export function isAppDarkTheme(): boolean {
  return document.documentElement.getAttribute('data-theme-base') === 'dark'
}

/**
 * 每个主题 id 对应的背景色（构建期从 xterm-theme 提取，硬编码避免触发懒加载）。
 * 用于主题选择列表按背景明暗从浅到深排序。`default` 是占位项（包里没有对应
 * 配色数据），故意不在这里登记，排序时按「无背景」落到末尾。
 */
const THEME_BACKGROUNDS: Record<string, string | null> = {
  Dracula: '#1e1f29',
  Gruvbox_Dark: '#1e1e1e',
  OneHalfDark: '#282c34',
  Monokai_Soda: '#1a1a1a',
  Solarized_Dark_Higher_Contrast: '#001e27',
  Tomorrow_Night: '#1d1f21',
  ayu: '#0f1419',
  Cobalt2: '#132738',
  JetBrains_Darcula: '#202020',
  SpaceGray: '#20242d',
  Material: '#eaeaea',
  OneHalfLight: '#fafafa',
  Solarized_Light: '#fcf4dc',
  Tomorrow: '#ffffff',
  Terminal_Basic: '#ffffff',
}

/**
 * 按背景色从浅到深排序的主题 id 列表（用于选择菜单）。
 * 无背景色（default）排在最末尾。排序稳定，仅此一次。
 */
export const SORTED_THEME_IDS: readonly string[] = (() => {
  const withBg = THEME_IDS.filter(id => THEME_BACKGROUNDS[id] != null)
  const noBg = THEME_IDS.filter(id => THEME_BACKGROUNDS[id] == null)
  const sorted = [...withBg].sort((a, b) => {
    const la = backgroundLuminance(THEME_BACKGROUNDS[a]!)
    const lb = backgroundLuminance(THEME_BACKGROUNDS[b]!)
    return lb - la // 浅（高亮度）在前
  })
  return [...sorted, ...noBg]
})()

/** 返回主题 id 的背景色，未知或 `default` 返回 null。 */
export function getThemeBackground(id: string): string | null {
  return THEME_BACKGROUNDS[id] ?? null
}

/** 计算背景色感知亮度（0 深 ~ 255 浅），不可解析返回 0。 */
function backgroundLuminance(hex: string): number {
  const rgb = parseHexColor(hex)
  if (!rgb) return 0
  return 0.299 * rgb[0] + 0.587 * rgb[1] + 0.114 * rgb[2]
}

/**
 * 构造终端主题选择网格的 previews map（含 auto 条目）。
 * theme 未加载时为 undefined。显式过滤 auto，避免未来 SORTED_THEME_IDS
 * 加入 'auto' 时循环覆盖掉先插入的 auto 条目。
 */
export function buildTerminalThemePreviews(
  themes: Record<string, ITheme> | null,
): Record<string, { type: 'terminal'; themeId: string; theme?: ITheme }> {
  const map: Record<string, { type: 'terminal'; themeId: string; theme?: ITheme }> = {}
  map[TERMINAL_THEME_AUTO] = { type: 'terminal', themeId: TERMINAL_THEME_AUTO }
  for (const id of SORTED_THEME_IDS) {
    if (id === TERMINAL_THEME_AUTO) continue
    map[id] = { type: 'terminal', themeId: id, theme: themes?.[id] }
  }
  return map
}
