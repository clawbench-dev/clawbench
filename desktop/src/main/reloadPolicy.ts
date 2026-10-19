/**
 * What an in-app reload is allowed to erase.
 *
 * There are two very different reasons the shell reloads the page, and they
 * must NOT share one storage policy:
 *
 *   - `cache` — the page is reloaded because the *server* was upgraded
 *     (`native:reload-app`, called by `useUpgrade` once the new binary is
 *     running). Only stale assets need to go. Every per-device preference the
 *     web app owns — theme, wallpaper, fonts, panel layout, language, UI scale
 *     — lives in `localStorage` on this origin, and wiping it here made every
 *     upgrade silently reset all of them (issue #559). The session cookie must
 *     survive too: the server persists its cookie token in the data directory
 *     (`model.PersistCookieToken` / `LoadCookieToken`), so the existing cookie
 *     is still valid after the restart.
 *
 *   - `everything` — the user explicitly asked for a hard reload
 *     (Ctrl+Shift+R), which is documented in `docs/user-guide/shortcuts.md` as
 *     clearing cookies and local storage, i.e. signing out and starting from
 *     factory defaults.
 *
 * Kept as a pure module (no electron import) so the policy is unit-testable.
 */

export type ReloadScope = 'cache' | 'everything'

/**
 * The subset of Electron's `Storages` this policy uses, declared locally so the
 * module stays importable without Electron. The values must stay a subset of
 * Electron's union — `tsc` enforces that at the `clearStorageData` call site.
 */
export type ReloadStorage = 'localstorage' | 'indexdb' | 'cookies' | 'cachestorage' | 'serviceworkers'

/**
 * The `storages` list for `session.clearStorageData`.
 *
 * `localstorage`, `indexdb` and `cookies` are deliberately absent from the
 * `cache` scope — see the module comment. `cachestorage` and `serviceworkers`
 * stay: they can hold cached app-shell/asset copies, and leaving a service
 * worker registered across a rebuild is exactly the stale-chunk failure the
 * upgrade reload exists to prevent.
 */
export function storagesToClear(scope: ReloadScope): ReloadStorage[] {
  if (scope === 'everything') {
    return ['localstorage', 'indexdb', 'cookies', 'cachestorage', 'serviceworkers']
  }
  return ['cachestorage', 'serviceworkers']
}
