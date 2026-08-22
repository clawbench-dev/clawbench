/**
 * Theme metadata and helpers for the VSCode-style named theme system.
 * Each theme is a self-contained color scheme identified by a semantic ID.
 */

// ── Theme IDs ──────────────────────────────────────────────────────────────────

export const THEME_IDS = [
  'github-light', 'github-dark',
  'one-dark-pro',
  'catppuccin-mocha', 'catppuccin-latte',
  'dracula',
  'nord',
  'tokyo-night',
  'solarized-dark', 'solarized-light',
  'gruvbox-dark', 'gruvbox-light',
  'high-contrast-dark', 'high-contrast-light',
  'night-owl',
  'ayu-dark',
  'vitesse-dark',
] as const

export type ThemeId = typeof THEME_IDS[number]

// ── Dark / light classification ────────────────────────────────────────────────

const DARK_THEME_IDS = new Set<string>([
  'github-dark', 'one-dark-pro', 'catppuccin-mocha', 'dracula',
  'nord', 'tokyo-night', 'solarized-dark', 'gruvbox-dark',
  'high-contrast-dark',
  'night-owl', 'ayu-dark', 'vitesse-dark',
])

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

// ── Status bar color (for Android meta theme-color + native bridge) ──────────

const STATUS_BAR_COLORS: Record<string, string> = {
  'github-light':       '#f8f9fa',
  'github-dark':        '#161b22',
  'one-dark-pro':       '#21252b',
  'catppuccin-mocha':   '#181825',
  'catppuccin-latte':   '#e6e9ef',
  'dracula':            '#21222c',
  'nord':               '#3b4252',
  'tokyo-night':        '#16161e',
  'solarized-dark':     '#073642',
  'solarized-light':    '#eee8d5',
  'gruvbox-dark':       '#1d2021',
  'gruvbox-light':      '#f2e5bc',
  'high-contrast-dark': '#0a0a0a',
  'high-contrast-light':'#f5f5f5',
  'night-owl':          '#001122',
  'ayu-dark':           '#0d1017',
  'vitesse-dark':       '#181818',
}

/** Returns the status bar / navigation bar background color for a theme ID. */
export function getThemeStatusBarColor(themeId: string): string {
  return STATUS_BAR_COLORS[themeId] ?? '#161b22'
}
