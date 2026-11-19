import { dialog, type BrowserWindow } from 'electron'
import { downloadAndInstall, restartInto } from './install'
import { hasEmbeddedServerBinary } from './localServer'
import { platformKey } from './updater'
import { recordError } from './clientLog'
import { resolveNativeLanguage, versionGateLabels } from './labels'

export interface FusedUpdateResult {
  ok: boolean
  /** Why the install failed; empty when ok. */
  error: string
}

/**
 * Install the all-in-one ("fused") package for `version`.
 *
 * A fused install carries its own server next to the executable, so it cannot
 * be upgraded by the two paths every other install uses:
 *
 *   - the THIN package would replace the client and leave the next launch
 *     without `server/`, i.e. without the server the app is built around;
 *   - the payload archive is `resources/` only, so it would replace the app
 *     and leave the bundled server at the old version.
 *
 * The all-in-one package is the only unit that updates both halves together,
 * which is why the server hands out its URLs (fused_downloads in
 * /api/upgrade/check) instead of the client reconstructing asset names.
 *
 * The install goes through the normal side-by-side path: the archive is
 * unpacked into `app-<version>/` and the `current` pointer is flipped. A
 * release that reused the previous desktop shell ships a package whose app
 * reports that older version while the directory is named after the release
 * tag — the startup handoff keeps the pointer in that case, so the new
 * directory (with its new server) is what every later launch runs.
 *
 * Restarting is explicit: the user is asked, never interrupted mid-session.
 */
export async function installFusedUpdate(
  version: string,
  downloads: Record<string, string[]>,
  parent: BrowserWindow | null,
): Promise<FusedUpdateResult> {
  const labels = versionGateLabels(resolveNativeLanguage())
  const parentArg = parent ?? undefined

  if (!hasEmbeddedServerBinary()) {
    // Only a fused install has something to update this way. Reaching here
    // means the UI offered an action this build cannot perform.
    recordError('FusedUpdate', 'not a fused install')
    return { ok: false, error: 'this build does not carry its own server' }
  }

  const key = platformKey(process.platform, process.arch)
  const urls = key ? (downloads?.[key] ?? []) : []
  if (!version || urls.length === 0) {
    await dialog.showMessageBox(parentArg as BrowserWindow, {
      type: 'error',
      buttons: [labels.ok],
      title: labels.downloadFailedTitle,
      message: labels.fusedPackageMissing,
    })
    return { ok: false, error: `no all-in-one package for ${key || 'this platform'}` }
  }

  try {
    await downloadAndInstall(urls, version, '')
  } catch (err) {
    recordError('FusedUpdate', err)
    await dialog.showMessageBox(parentArg as BrowserWindow, {
      type: 'error',
      buttons: [labels.ok],
      title: labels.downloadFailedTitle,
      message: labels.downloadFailedMessage,
      detail: String((err as Error)?.message || err),
    })
    return { ok: false, error: String((err as Error)?.message || err) }
  }

  const { response } = await dialog.showMessageBox(parentArg as BrowserWindow, {
    type: 'info',
    buttons: [labels.restartNow, labels.later],
    defaultId: 0,
    cancelId: 1,
    title: labels.versionReadyTitle,
    message: labels.versionReadyMessage.replace('{version}', version),
    detail: labels.versionReadyDetail,
  })
  if (response === 0) {
    try {
      restartInto(version)
    } catch (err) {
      recordError('FusedUpdate', err)
      await dialog.showMessageBox(parentArg as BrowserWindow, {
        type: 'error',
        buttons: [labels.ok],
        title: labels.restartFailedTitle,
        message: labels.restartFailedMessage,
        detail: String((err as Error)?.message || err),
      })
      return { ok: false, error: String((err as Error)?.message || err) }
    }
  }
  return { ok: true, error: '' }
}
