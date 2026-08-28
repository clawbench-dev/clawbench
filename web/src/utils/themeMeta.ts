import { appLog } from '@/utils/appLog'

/**
 * Theme metadata and helpers for the VSCode-style named theme system.
 * Each theme is a self-contained color scheme identified by a semantic ID.
 *
 * `THEMES` is the single source of truth for the theme system. All other
 * theme data (`THEME_IDS`, dark classification, status-bar colors, preview
 * colors, i18n label keys) is derived from it, so adding/removing a theme only
 * requires editing this array (plus the matching `variables.css` block and any
 * native-side mapping in `MainActivity.java` / `login.html`).
 */

export interface ThemePreview {
  bg: string
  text: string
  accent: string
}

export interface ThemeMeta {
  id: string
  dark: boolean
  labelKey: string
  statusBar: string
  preview: ThemePreview
}

export const THEMES: ThemeMeta[] = [
  // 按背景亮度从浅到深排列
  { id: 'one-light',          dark: false, labelKey: 'settings.items.themeOneLight',          statusBar: '#f0f0f0', preview: { bg: '#fafafa', text: '#383a42', accent: '#4078f2' } },
  { id: 'ayu-light',          dark: false, labelKey: 'settings.items.themeAyuLight',          statusBar: '#f3f3f3', preview: { bg: '#fafafa', text: '#5c6166', accent: '#ff9940' } },
  { id: 'github-light',       dark: false, labelKey: 'settings.items.themeGithubLight',       statusBar: '#f8f9fa', preview: { bg: '#f8f9fa', text: '#212529', accent: '#4a90d9' } },
  { id: 'everforest-light',   dark: false, labelKey: 'settings.items.themeEverforestLight',   statusBar: '#f2e9d0', preview: { bg: '#fdf6e3', text: '#5c6a72', accent: '#7a8478' } },
  { id: 'high-contrast-light',dark: false, labelKey: 'settings.items.themeHighContrastLight', statusBar: '#f5f5f5', preview: { bg: '#f5f5f5', text: '#000000', accent: '#0055cc' } },
  { id: 'mint-light',         dark: false, labelKey: 'settings.items.themeMintLight',         statusBar: '#dcf0dd', preview: { bg: '#e8f5e9', text: '#21362a', accent: '#00897b' } },
  { id: 'sky-light',          dark: false, labelKey: 'settings.items.themeSkyLight',          statusBar: '#d9edf9', preview: { bg: '#e3f2fd', text: '#24364a', accent: '#3949ab' } },
  { id: 'nord-light',         dark: false, labelKey: 'settings.items.themeNordLight',         statusBar: '#e5e9f0', preview: { bg: '#eceff4', text: '#2e3440', accent: '#5e81ac' } },
  { id: 'catppuccin-latte',   dark: false, labelKey: 'settings.items.themeCatppuccinLatte',   statusBar: '#e6e9ef', preview: { bg: '#e6e9ef', text: '#4c4f69', accent: '#1e66f5' } },
  { id: 'solarized-light',    dark: false, labelKey: 'settings.items.themeSolarizedLight',    statusBar: '#eee8d5', preview: { bg: '#eee8d5', text: '#657b83', accent: '#268bd2' } },
  { id: 'gruvbox-light',      dark: false, labelKey: 'settings.items.themeGruvboxLight',      statusBar: '#f2e5bc', preview: { bg: '#f2e5bc', text: '#3c3836', accent: '#af3a03' } },
  { id: 'solarized-dark',     dark: true,  labelKey: 'settings.items.themeSolarizedDark',     statusBar: '#0a3541', preview: { bg: '#0a3541', text: '#a0b0b4', accent: '#2e9fd8' } },
  { id: 'nord',               dark: true,  labelKey: 'settings.items.themeNord',              statusBar: '#202833', preview: { bg: '#202833', text: '#e6ecf4', accent: '#6cb2f0' } },
  { id: 'everforest-dark',    dark: true,  labelKey: 'settings.items.themeEverforestDark',    statusBar: '#22282b', preview: { bg: '#22282b', text: '#d3c6aa', accent: '#a7c080' } },
  { id: 'one-dark-pro',       dark: true,  labelKey: 'settings.items.themeOneDarkPro',        statusBar: '#21252b', preview: { bg: '#21252b', text: '#abb2bf', accent: '#61afef' } },
  { id: 'dracula',            dark: true,  labelKey: 'settings.items.themeDracula',           statusBar: '#21222c', preview: { bg: '#21222c', text: '#f8f8f2', accent: '#bd93f9' } },
  { id: 'rose-pine',          dark: true,  labelKey: 'settings.items.themeRosePine',          statusBar: '#1f1d2e', preview: { bg: '#1f1d2e', text: '#e0def4', accent: '#ebbcba' } },
  { id: 'gruvbox-dark',       dark: true,  labelKey: 'settings.items.themeGruvboxDark',       statusBar: '#1d2021', preview: { bg: '#1d2021', text: '#ebdbb2', accent: '#fe8019' } },
  { id: 'solarized-deep',     dark: true,  labelKey: 'settings.items.themeSolarizedDeep',     statusBar: '#15212b', preview: { bg: '#15212b', text: '#dce5ec', accent: '#3bb8e0' } },
  { id: 'github-dark',        dark: true,  labelKey: 'settings.items.themeGithubDark',        statusBar: '#161b22', preview: { bg: '#161b22', text: '#c9d1d9', accent: '#58a6ff' } },
  { id: 'catppuccin-mocha',   dark: true,  labelKey: 'settings.items.themeCatppuccinMocha',   statusBar: '#181825', preview: { bg: '#181825', text: '#cdd6f4', accent: '#89b4fa' } },
  { id: 'vitesse-dark',       dark: true,  labelKey: 'settings.items.themeVitesseDark',       statusBar: '#181818', preview: { bg: '#181818', text: '#dbd7ca', accent: '#4d9375' } },
  { id: 'tokyo-night',        dark: true,  labelKey: 'settings.items.themeTokyoNight',        statusBar: '#16161e', preview: { bg: '#16161e', text: '#c0caf5', accent: '#7aa2f7' } },
  { id: 'kanagawa',           dark: true,  labelKey: 'settings.items.themeKanagawa',          statusBar: '#16161d', preview: { bg: '#16161d', text: '#dcd7ba', accent: '#7e9cd8' } },
  { id: 'ayu-dark',           dark: true,  labelKey: 'settings.items.themeAyuDark',           statusBar: '#0d1017', preview: { bg: '#0d1017', text: '#b3b1ad', accent: '#e6b450' } },
  { id: 'night-owl',          dark: true,  labelKey: 'settings.items.themeNightOwl',          statusBar: '#001122', preview: { bg: '#001122', text: '#d6deeb', accent: '#82aaff' } },
  { id: 'high-contrast-dark', dark: true,  labelKey: 'settings.items.themeHighContrastDark',  statusBar: '#0a0a0a', preview: { bg: '#0a0a0a', text: '#ffffff', accent: '#00ccff' } },
]

// ── Theme IDs ──────────────────────────────────────────────────────────────────

export const THEME_IDS: readonly string[] = THEMES.map(t => t.id)

export type ThemeId = (typeof THEMES)[number]['id']

// ── Dark / light classification ────────────────────────────────────────────────

const DARK_THEME_IDS = new Set<string>(THEMES.filter(t => t.dark).map(t => t.id))

/** Returns `true` if the given theme ID is a dark-color-scheme theme. */
export function isDarkTheme(themeId: string): boolean {
  return DARK_THEME_IDS.has(themeId)
}

// ── Defaults ───────────────────────────────────────────────────────────────────

export function getDefaultDarkTheme(): string { return 'github-dark' }
export function getDefaultLightTheme(): string { return 'github-light' }

// ── Resolution ─────────────────────────────────────────────────────────────────

/** Resolve a stored setting value (which may be `'auto'`) to a concrete theme ID. */
export function resolveThemeId(value: string): string {
  if (value === 'auto') {
    return window.matchMedia('(prefers-color-scheme: dark)').matches
      ? getDefaultDarkTheme()
      : getDefaultLightTheme()
  }
  return value
}

// ── i18n label keys ────────────────────────────────────────────────────────────

/** Derive the i18n key for a theme's display label (e.g. 'github-light' → 'settings.items.themeGithubLight'). */
export function getThemeLabelKey(themeId: string): string {
  return 'settings.items.theme' + themeId
    .split('-')
    .map(s => s.charAt(0).toUpperCase() + s.slice(1))
    .join('')
}

// ── Status bar color (for Android meta theme-color + native bridge) ──────────

export const STATUS_BAR_COLORS: Record<string, string> = Object.fromEntries(
  THEMES.map(t => [t.id, t.statusBar]),
)

/** Returns the status bar / navigation bar background color for a theme ID. */
export function getThemeStatusBarColor(themeId: string): string {
  const color = STATUS_BAR_COLORS[themeId]
  if (!color) {
    appLog.w('ThemeMeta', `getThemeStatusBarColor: unknown theme '${themeId}', falling back to github-dark`)
    return '#161b22'
  }
  return color
}

// ── Theme preview colors (for theme picker) ──────────────────────────────────

export const THEME_PREVIEW_COLORS: Record<string, ThemePreview> = Object.fromEntries(
  THEMES.map(t => [t.id, t.preview]),
)

/** Returns the preview colors (bg/text/accent) for a theme ID, or null if unknown. */
export function getThemePreviewColor(themeId: string): ThemePreview | null {
  const preview = THEME_PREVIEW_COLORS[themeId]
  if (!preview) {
    appLog.w('ThemeMeta', `getThemePreviewColor: unknown theme '${themeId}'`)
    return null
  }
  return preview
}

// ── Full theme palette (for native floating window) ──────────────────────────

export interface ThemePalette {
  bg: string
  text: string
  textSecondary: string
  accent: string
}

/** Default palette for github-dark, used when no color has been persisted. */
export const DEFAULT_THEME_PALETTE: ThemePalette = {
  bg: '#161b22',
  text: '#c9d1d9',
  textSecondary: '#8b949e',
  accent: '#58a6ff',
}

/**
 * Build the full palette (bg/text/textSecondary/accent) for a resolved theme.
 * bg/text/accent come from THEMES metadata; textSecondary is read from the
 * live `--text-secondary` CSS variable when available (falls back to the
 * github-dark secondary color). Unknown theme ids fall back to github-dark.
 */
export function buildThemePalette(themeId: string): ThemePalette {
  const meta = THEMES.find(t => t.id === themeId)
  if (!meta) {
    appLog.w('ThemeMeta', `buildThemePalette: unknown theme '${themeId}', falling back to github-dark`)
    const fallback = THEMES.find(t => t.id === 'github-dark')!
    return { bg: fallback.preview.bg, text: fallback.preview.text, textSecondary: '#8b949e', accent: fallback.preview.accent }
  }
  let textSecondary = '#8b949e'
  try {
    const cs = getComputedStyle(document.documentElement).getPropertyValue('--text-secondary').trim()
    if (cs) textSecondary = cs
  } catch { /* not in browser */ }
  return { bg: meta.preview.bg, text: meta.preview.text, textSecondary, accent: meta.preview.accent }
}

// ── Apply to DOM ──────────────────────────────────────────────────────────────

/**
 * Apply the given resolved theme ID to the document: sets `data-theme`,
 * `data-theme-base`, `data-hljs-theme` and the `meta[name="theme-color"]`
 * content. Idempotent — safe to call multiple times with the same theme.
 */
export function applyThemeAttributes(resolved: string): void {
  const base = isDarkTheme(resolved) ? 'dark' : 'light'
  const el = document.documentElement
  el.setAttribute('data-theme', resolved)
  el.setAttribute('data-theme-base', base)
  el.setAttribute('data-hljs-theme', base)
  const metaTC = document.querySelector('meta[name="theme-color"]')
  if (metaTC) metaTC.setAttribute('content', getThemeStatusBarColor(resolved))
}