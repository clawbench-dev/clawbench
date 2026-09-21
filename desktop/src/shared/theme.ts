/**
 * Theme ID handling for the desktop shell.
 *
 * The desktop shell persists the FULL theme ID (e.g. 'github-dark', 'nord')
 * because the login page resolves its colours from `[data-theme="<id>"]`
 * blocks. It has no rule for a bare 'dark', so collapsing the ID would make the
 * login page silently fall back to its defaults and never match the main UI.
 *
 * Android stores the raw ID for the same reason (see ThemePalette.java).
 */

/** Theme used when nothing is persisted or the ID is unknown. */
export const DEFAULT_THEME_ID = 'github-dark'

/**
 * Dark theme IDs, mirroring `web/src/utils/themeMeta.ts` (the authoritative
 * registry) and the `DARK_IDS` list inlined in `desktop/assets/login.html`.
 *
 * This list exists only because `nativeTheme.themeSource` understands just
 * 'light' | 'dark' | 'system'. A drift test
 * (`src/shared/theme.test.ts`) compares it against the login page's DARK_IDS so
 * a new theme cannot be added in one place only.
 */
export const DARK_THEME_IDS: readonly string[] = [
  'ayu-dark',
  'bluloco-dark',
  'catppuccin-mocha',
  'dark-plus',
  'dracula',
  'everforest-dark',
  'github-dark',
  'gruvbox-dark',
  'high-contrast-dark',
  'kanagawa',
  'material-darker',
  'monokai',
  'night-owl',
  'nord',
  'one-dark-pro',
  'rose-pine',
  'solarized-dark',
  'solarized-deep',
  'tokyo-night',
  'vitesse-dark',
]

/** Returns true when the theme ID denotes a dark colour scheme. */
export function isDarkThemeId(themeId: string): boolean {
  return DARK_THEME_IDS.includes(themeId)
}
