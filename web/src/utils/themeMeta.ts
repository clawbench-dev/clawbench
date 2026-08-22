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
] as const

export type ThemeId = typeof THEME_IDS[number]

// ── Dark / light classification ────────────────────────────────────────────────

const DARK_THEME_IDS = new Set<string>([
  'github-dark', 'one-dark-pro', 'catppuccin-mocha', 'dracula',
  'nord', 'tokyo-night', 'solarized-dark', 'gruvbox-dark',
  'high-contrast-dark',
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
