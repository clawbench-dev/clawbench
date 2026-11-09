/**
 * Where per-device settings live.
 *
 * One prefix, one place. `useSettingsConfig` owns the settings panel and keeps
 * a reactive copy of every value, but a few leaf modules (picture quality, for
 * one) are read from code paths that must not import that composable — it
 * pulls in i18n, the API client and the agent store, and some of those call
 * sites sit inside the share page's own entry point. Those modules read the
 * same keys straight from localStorage instead, so the two views can never
 * disagree about what is stored.
 *
 * Values are JSON: the panel writes strings, numbers and booleans through
 * `JSON.stringify`, so a reader that parses JSON sees the original type.
 */
export const SETTINGS_KEY_PREFIX = 'clawbench-settings-'

/** Read one stored setting, or the fallback when it is absent or unreadable. */
export function readStoredSetting(key: string, fallback: unknown): unknown {
  try {
    const raw = localStorage.getItem(SETTINGS_KEY_PREFIX + key)
    if (raw === null) return fallback
    return JSON.parse(raw)
  } catch {
    return fallback
  }
}
