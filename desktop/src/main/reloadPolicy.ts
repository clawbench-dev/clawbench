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
 * This module has no imports on purpose. `session.ts` cannot be unit-tested in
 * CI (the desktop package's dependencies are not installed there, so importing
 * Electron fails to resolve), which is why the decision AND the ordering live
 * here, taking the Electron session as an injected dependency.
 */

export type ReloadScope = 'cache' | 'everything'

/**
 * The subset of Electron's `Storages` this policy uses. The values must stay a
 * subset of Electron's union — `tsc` enforces that at the `clearStorageData`
 * call site.
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

/** The three Electron calls a reload needs, injected so this stays testable. */
export interface ReloadDeps {
  clearCache(): Promise<void> | void
  clearStorageData(options: { storages: ReloadStorage[] }): Promise<void> | void
  reload(): void
}

/**
 * Drop the caches the scope allows, then reload the window.
 *
 * The order is load-bearing: reloading first would let the outgoing page write
 * its state straight back into the storage this call is trying to clear. It is
 * asserted by the test, because "clear then reload" reads like an obvious
 * detail right up until someone reorders it while tidying up.
 */
export async function performReload(deps: ReloadDeps, scope: ReloadScope): Promise<void> {
  await deps.clearCache()
  await deps.clearStorageData({ storages: storagesToClear(scope) })
  deps.reload()
}
