import type { ITheme } from '@xterm/xterm'
import { getThemePreviewColor } from '@/utils/themeMeta'

export const TERMINAL_THEME_AUTO = 'auto'
export const TERMINAL_THEME_STORAGE_KEY = 'terminalTheme'

/**
 * 157 个主题 id 的静态列表（构建期硬编码，不触发 xterm-theme 加载）。
 * 用于渲染选择列表。来源：node_modules/xterm-theme/index.js 的所有具名导出。
 */
export const THEME_IDS: string[] = [
  'AdventureTime', 'Afterglow', 'AlienBlood', 'Argonaut', 'Arthur', 'AtelierSulphurpool',
  'Atom', 'Batman', 'Belafonte_Night', 'BirdsOfParadise', 'Blazer', 'Borland',
  'Bright_Lights', 'Broadcast', 'Brogrammer', 'C64', 'Chalk', 'Chalkboard', 'Ciapre',
  'Cobalt2', 'Cobalt_Neon', 'CrayonPonyFish', 'Dark_Pastel', 'Darkside', 'Desert',
  'DimmedMonokai', 'DotGov', 'Dracula', 'Duotone_Dark', 'ENCOM', 'Earthsong', 'Elemental',
  'Elementary', 'Espresso', 'Espresso_Libre', 'Fideloper', 'FirefoxDev', 'Firewatch',
  'FishTank', 'Flat', 'Flatland', 'Floraverse', 'ForestBlue', 'FrontEndDelight',
  'FunForrest', 'Galaxy', 'Github', 'Glacier', 'Grape', 'Grass', 'Gruvbox_Dark',
  'Hardcore', 'Harper', 'Highway', 'Hipster_Green', 'Homebrew', 'Hurtado', 'Hybrid',
  'IC_Green_PPL', 'IC_Orange_PPL', 'IR_Black', 'Jackie_Brown', 'Japanesque', 'Jellybeans',
  'JetBrains_Darcula', 'Kibble', 'Later_This_Evening', 'Lavandula', 'LiquidCarbon',
  'LiquidCarbonTransparent', 'LiquidCarbonTransparentInverse', 'Man_Page', 'Material',
  'MaterialDark', 'Mathias', 'Medallion', 'Misterioso', 'Molokai', 'MonaLisa',
  'Monokai_Soda', 'Monokai_Vivid', 'N0tch2k', 'Neopolitan', 'Neutron', 'NightLion_v1',
  'NightLion_v2', 'Night_3024', 'Novel', 'Obsidian', 'Ocean', 'OceanicMaterial', 'Ollie',
  'OneHalfDark', 'OneHalfLight', 'Pandora', 'Paraiso_Dark', 'Parasio_Dark', 'PaulMillr',
  'PencilDark', 'PencilLight', 'Piatto_Light', 'Pnevma', 'Pro', 'Red_Alert', 'Red_Sands',
  'Rippedcasts', 'Royal', 'Ryuuko', 'SeaShells', 'Seafoam_Pastel', 'Seti', 'Shaman',
  'Slate', 'Smyck', 'SoftServer', 'Solarized_Darcula', 'Solarized_Dark',
  'Solarized_Dark_Higher_Contrast', 'Solarized_Dark_Patched', 'Solarized_Light', 'SpaceGray',
  'SpaceGray_Eighties', 'SpaceGray_Eighties_Dull', 'Spacedust', 'Spiderman', 'Spring',
  'Square', 'Sundried', 'Symfonic', 'Teerb', 'Terminal_Basic', 'Thayer_Bright', 'The_Hulk',
  'Tomorrow', 'Tomorrow_Night', 'Tomorrow_Night_Blue', 'Tomorrow_Night_Bright',
  'Tomorrow_Night_Eighties', 'ToyChest', 'Treehouse', 'Ubuntu', 'UnderTheSea', 'Urple',
  'Vaughn', 'VibrantInk', 'Violet_Dark', 'Violet_Light', 'WarmNeon', 'Wez', 'WildCherry',
  'Wombat', 'Wryan', 'Zenburn', 'ayu', 'deep', 'default', 'idleToes',
]

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

/** 动态加载 xterm-theme 全部主题（懒加载，仅在使用时触发）。缓存结果避免重复 import。 */
export async function loadThemesModule(): Promise<Record<string, ITheme>> {
  if (cachedThemes) return cachedThemes
  const mod = await import('xterm-theme')
  cachedThemes = (mod.default ?? mod) as Record<string, ITheme>
  return cachedThemes
}

/** 清空已加载主题缓存（仅供测试使用）。 */
export function resetThemesCache(): void {
  cachedThemes = null
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

/**
 * 在所有终端主题里找背景色与 targetBg 最接近的一个，返回其 id。
 * 主题无 background 或不可解析 → 跳过；themes 为空 → 返回 null。
 */
export function findClosestThemeByBackground(
  targetBg: string,
  themes: Record<string, ITheme>,
): string | null {
  let best: string | null = null
  let bestDist = Number.POSITIVE_INFINITY
  for (const [id, theme] of Object.entries(themes)) {
    const bg = theme.background
    if (!bg) continue
    const dist = colorDistance(targetBg, bg)
    if (dist < bestDist) {
      bestDist = dist
      best = id
    }
  }
  return best
}

/**
 * 解析 auto 主题（跟随 App）：在全部终端主题里找背景色与当前 App 主题
 * 最接近的一个；themes 未加载或为空（懒加载失败）→ 回退 Catppuccin。
 */
export function resolveAutoTheme(
  appThemeBg: string,
  themes: Record<string, ITheme> | null | undefined,
  isAppDark: boolean,
): ITheme {
  const id = themes ? findClosestThemeByBackground(appThemeBg, themes) : null
  if (!id) return isAppDark ? darkTheme : lightTheme
  return themes![id]
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
    if (!appThemeBg || !themes) return isAppDark ? darkTheme : lightTheme
    return resolveAutoTheme(appThemeBg, themes, isAppDark)
  }
  try {
    const themes = preloaded ?? (await loadThemesModule())
    const theme = themes[selection]
    if (theme) return theme
  } catch {
    // 加载失败或 id 缺失 → 回退
  }
  return isAppDark ? darkTheme : lightTheme
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
  if (!appThemeBg || !cachedThemes) return isAppDark ? darkTheme : lightTheme
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
    return isAppDark ? darkTheme : lightTheme
  }
  const theme = cachedThemes?.[selection]
  if (theme) return theme
  return isAppDark ? darkTheme : lightTheme
}

/** 当前 App 是否为深色主题（同步判定）。 */
export function isAppDarkTheme(): boolean {
  return document.documentElement.getAttribute('data-theme-base') === 'dark'
}
