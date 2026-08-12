import type { ITheme } from '@xterm/xterm'

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

/** 把主题 id 转成展示名（下划线 → 空格）。 */
export function formatThemeName(id: string): string {
  return id.replace(/_/g, ' ')
}

/**
 * 解析最终主题。
 * - auto → 按 isAppDark 返回静态 darkTheme/lightTheme（不触发懒加载）。
 * - 固定 id → 懒加载后返回该主题；未知/缺失 → 回退到按 isAppDark 的自动主题。
 */
export async function resolveTheme(selection: string, isAppDark: boolean): Promise<ITheme> {
  if (selection === TERMINAL_THEME_AUTO) {
    return isAppDark ? darkTheme : lightTheme
  }
  try {
    const themes = await loadThemesModule()
    const theme = themes[selection]
    if (theme) return theme
  } catch {
    // 加载失败或 id 缺失 → 回退
  }
  return isAppDark ? darkTheme : lightTheme
}

/** 当前 App 是否为深色主题（同步判定）。 */
export function isAppDarkTheme(): boolean {
  return document.documentElement.getAttribute('data-theme') === 'dark'
}
