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

/** Light counterpart of {@link DEFAULT_THEME_ID}, used for legacy 'light'. */
export const DEFAULT_LIGHT_THEME_ID = 'github-light'

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

/**
 * Normalise a stored theme value to a real theme ID.
 *
 * Older builds persisted a collapsed 'dark' / 'light' instead of the full ID.
 * Such a value matches no `[data-theme="..."]` rule, so the login page renders
 * with every colour variable undefined (transparent inputs and buttons). Map
 * those two legacy values onto the default for their colour scheme so an
 * existing install recovers without the user touching settings.
 */
export function normalizeThemeId(stored: string | undefined | null): string {
  if (!stored) return DEFAULT_THEME_ID
  if (stored === 'dark') return DEFAULT_THEME_ID
  if (stored === 'light') return DEFAULT_LIGHT_THEME_ID
  return stored
}
