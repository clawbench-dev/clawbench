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

/**
 * 每个主题 id 对应的背景色（构建期从 xterm-theme 提取，硬编码避免触发懒加载）。
 * 用于主题选择列表按背景明暗从浅到深排序。`default` 是模块默认导出（无独立背景），为 null。
 */
const THEME_BACKGROUNDS: Record<string, string | null> = {
  AdventureTime: '#1f1d45',
  Afterglow: '#212121',
  AlienBlood: '#0f1610',
  Argonaut: '#0e1019',
  Arthur: '#1c1c1c',
  AtelierSulphurpool: '#202746',
  Atom: '#161719',
  Batman: '#1b1d1e',
  Belafonte_Night: '#20111b',
  BirdsOfParadise: '#2a1f1d',
  Blazer: '#0d1926',
  Borland: '#0000a4',
  Bright_Lights: '#191919',
  Broadcast: '#2b2b2b',
  Brogrammer: '#131313',
  C64: '#40318d',
  Chalk: '#2b2d2e',
  Chalkboard: '#29262f',
  Ciapre: '#191c27',
  Cobalt2: '#132738',
  Cobalt_Neon: '#142838',
  CrayonPonyFish: '#150707',
  Dark_Pastel: '#000000',
  Darkside: '#222324',
  Desert: '#333333',
  DimmedMonokai: '#1f1f1f',
  DotGov: '#262c35',
  Dracula: '#1e1f29',
  Duotone_Dark: '#1f1d27',
  ENCOM: '#000000',
  Earthsong: '#292520',
  Elemental: '#22211d',
  Elementary: '#181818',
  Espresso: '#323232',
  Espresso_Libre: '#2a211c',
  Fideloper: '#292f33',
  FirefoxDev: '#0e1011',
  Firewatch: '#1e2027',
  FishTank: '#232537',
  Flat: '#002240',
  Flatland: '#1d1f21',
  Floraverse: '#0e0d15',
  ForestBlue: '#051519',
  FrontEndDelight: '#1b1c1d',
  FunForrest: '#251200',
  Galaxy: '#1d2837',
  Github: '#f4f4f4',
  Glacier: '#0c1115',
  Grape: '#171423',
  Grass: '#13773d',
  Gruvbox_Dark: '#1e1e1e',
  Hardcore: '#121212',
  Harper: '#010101',
  Highway: '#222225',
  Hipster_Green: '#100b05',
  Homebrew: '#000000',
  Hurtado: '#000000',
  Hybrid: '#161719',
  IC_Green_PPL: '#3a3d3f',
  IC_Orange_PPL: '#262626',
  IR_Black: '#000000',
  Jackie_Brown: '#2c1d16',
  Japanesque: '#1e1e1e',
  Jellybeans: '#121212',
  JetBrains_Darcula: '#202020',
  Kibble: '#0e100a',
  Later_This_Evening: '#222222',
  Lavandula: '#050014',
  LiquidCarbon: '#303030',
  LiquidCarbonTransparent: '#000000',
  LiquidCarbonTransparentInverse: '#000000',
  Man_Page: '#fef49c',
  Material: '#eaeaea',
  MaterialDark: '#232322',
  Mathias: '#000000',
  Medallion: '#1d1908',
  Misterioso: '#2d3743',
  Molokai: '#121212',
  MonaLisa: '#120b0d',
  Monokai_Soda: '#1a1a1a',
  Monokai_Vivid: '#121212',
  N0tch2k: '#222222',
  Neopolitan: '#271f19',
  Neutron: '#1c1e22',
  NightLion_v1: '#000000',
  NightLion_v2: '#171717',
  Night_3024: '#090300',
  Novel: '#dfdbc3',
  Obsidian: '#283033',
  Ocean: '#224fbc',
  OceanicMaterial: '#1c262b',
  Ollie: '#222125',
  OneHalfDark: '#282c34',
  OneHalfLight: '#fafafa',
  Pandora: '#141e43',
  Paraiso_Dark: '#2f1e2e',
  Parasio_Dark: '#2f1e2e',
  PaulMillr: '#000000',
  PencilDark: '#212121',
  PencilLight: '#f1f1f1',
  Piatto_Light: '#ffffff',
  Pnevma: '#1c1c1c',
  Pro: '#000000',
  Red_Alert: '#762423',
  Red_Sands: '#7a251e',
  Rippedcasts: '#2b2b2b',
  Royal: '#100815',
  Ryuuko: '#2c3941',
  SeaShells: '#09141b',
  Seafoam_Pastel: '#243435',
  Seti: '#111213',
  Shaman: '#001015',
  Slate: '#222222',
  Smyck: '#1b1b1b',
  SoftServer: '#242626',
  Solarized_Darcula: '#3d3f41',
  Solarized_Dark: '#001e27',
  Solarized_Dark_Higher_Contrast: '#001e27',
  Solarized_Dark_Patched: '#001e27',
  Solarized_Light: '#fcf4dc',
  SpaceGray: '#20242d',
  SpaceGray_Eighties: '#222222',
  SpaceGray_Eighties_Dull: '#222222',
  Spacedust: '#0a1e24',
  Spiderman: '#1b1d1e',
  Spring: '#ffffff',
  Square: '#1a1a1a',
  Sundried: '#1a1818',
  Symfonic: '#000000',
  Teerb: '#262626',
  Terminal_Basic: '#ffffff',
  Thayer_Bright: '#1b1d1e',
  The_Hulk: '#1b1d1e',
  Tomorrow: '#ffffff',
  Tomorrow_Night: '#1d1f21',
  Tomorrow_Night_Blue: '#002451',
  Tomorrow_Night_Bright: '#000000',
  Tomorrow_Night_Eighties: '#2d2d2d',
  ToyChest: '#24364b',
  Treehouse: '#191919',
  Ubuntu: '#300a24',
  UnderTheSea: '#011116',
  Urple: '#1b1b23',
  Vaughn: '#25234f',
  VibrantInk: '#000000',
  Violet_Dark: '#1c1d1f',
  Violet_Light: '#fcf4dc',
  WarmNeon: '#404040',
  Wez: '#000000',
  WildCherry: '#1f1726',
  Wombat: '#171717',
  Wryan: '#101010',
  Zenburn: '#3f3f3f',
  ayu: '#0f1419',
  deep: '#000000',
  default: null,
  idleToes: '#323232',
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
