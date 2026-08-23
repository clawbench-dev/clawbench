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

export const STATUS_BAR_COLORS: Record<string, string> = {
  'github-light':       '#f8f9fa',
  'github-dark':        '#161b22',
  'one-dark-pro':       '#21252b',
  'catppuccin-mocha':   '#181825',
  'catppuccin-latte':   '#e6e9ef',
  'dracula':            '#21222c',
  'nord':               '#202833',
  'tokyo-night':        '#16161e',
  'solarized-dark':     '#0a3541',
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

// ── Theme preview colors (for theme picker) ──────────────────────────────────

export const THEME_PREVIEW_COLORS: Record<string, { bg: string; text: string; accent: string }> = {
  'github-light':       { bg: '#f8f9fa', text: '#212529', accent: '#4a90d9' },
  'github-dark':        { bg: '#161b22', text: '#c9d1d9', accent: '#58a6ff' },
  'one-dark-pro':       { bg: '#21252b', text: '#abb2bf', accent: '#61afef' },
  'catppuccin-mocha':   { bg: '#181825', text: '#cdd6f4', accent: '#89b4fa' },
  'catppuccin-latte':   { bg: '#e6e9ef', text: '#4c4f69', accent: '#1e66f5' },
  'dracula':            { bg: '#21222c', text: '#f8f8f2', accent: '#bd93f9' },
  'nord':               { bg: '#202833', text: '#e6ecf4', accent: '#6cb2f0' },
  'tokyo-night':        { bg: '#16161e', text: '#c0caf5', accent: '#7aa2f7' },
  'solarized-dark':     { bg: '#0a3541', text: '#a0b0b4', accent: '#2e9fd8' },
  'solarized-light':    { bg: '#eee8d5', text: '#657b83', accent: '#268bd2' },
  'gruvbox-dark':       { bg: '#1d2021', text: '#ebdbb2', accent: '#fe8019' },
  'gruvbox-light':      { bg: '#f2e5bc', text: '#3c3836', accent: '#af3a03' },
  'high-contrast-dark': { bg: '#0a0a0a', text: '#ffffff', accent: '#00ccff' },
  'high-contrast-light':{ bg: '#f5f5f5', text: '#000000', accent: '#0055cc' },
  'night-owl':          { bg: '#001122', text: '#d6deeb', accent: '#82aaff' },
  'ayu-dark':           { bg: '#0d1017', text: '#b3b1ad', accent: '#e6b450' },
  'vitesse-dark':       { bg: '#181818', text: '#dbd7ca', accent: '#4d9375' },
}
