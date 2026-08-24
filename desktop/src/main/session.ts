import { session } from 'electron'
import { getMainWindow } from './window'

/**
 * Clear the session cache/storage and hard-reload the window (Ctrl+F5).
 *
 * Used by the Ctrl+F5 shortcut and by `native:reload-app` (the bridge the web
 * frontend calls after a server upgrade completes). Clearing cookies here is
 * intentional: it guarantees fresh auth + static assets, matching the
 * Ctrl+F5 behavior. On the localhost setups ClawBench targets this only logs
 * the user back in automatically.
 */
export async function clearCacheAndReload(): Promise<void> {
  const ses = session.defaultSession
  await ses.clearCache()
  await ses.clearStorageData({ storages: ['localstorage', 'indexdb', 'cookies', 'cachestorage', 'serviceworkers'] })
  getMainWindow()?.webContents.reload()
}
