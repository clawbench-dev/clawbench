import { session } from 'electron'
import { getMainWindow } from './window'
import { performReload, type ReloadScope } from './reloadPolicy'

/**
 * Clear caches (or everything, for an explicit hard reload) and reload the
 * window.
 *
 * Two callers with two different needs — see `reloadPolicy.ts`:
 *
 *   - Ctrl+Shift+R passes `'everything'`: the documented hard reload that
 *     clears cookies and local storage, i.e. signs out and resets every
 *     per-device preference.
 *   - `native:reload-app` (the bridge the web frontend calls after a server
 *     upgrade completes) passes `'cache'`: only stale assets go. It used to
 *     clear local storage too, which silently reset the theme, wallpaper,
 *     fonts and panel layout on every upgrade (issue #559) — the server's
 *     upgrade only replaces the binary, so nothing about this browser's
 *     preferences became invalid.
 *
 * `scope` is required on purpose. A default would let a future third call site
 * pick a policy silently, which is exactly how the two opposite semantics got
 * merged in the first place; requiring it makes the compiler ask.
 *
 * The work itself lives in `reloadPolicy.performReload` because this file
 * cannot be loaded without the desktop dependencies installed — CI runs the
 * root vitest suite with only the root `node_modules`, where importing
 * `electron` fails to resolve. Keeping the decision here would mean it ships
 * untested.
 */
export async function clearCacheAndReload(scope: ReloadScope): Promise<void> {
  const ses = session.defaultSession
  await performReload(
    {
      clearCache: () => ses.clearCache(),
      clearStorageData: (options) => ses.clearStorageData(options),
      reload: () => { getMainWindow()?.webContents.reload() },
    },
    scope,
  )
}
