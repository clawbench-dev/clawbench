import { app, BrowserWindow, Menu, dialog, session } from 'electron'
import { initStore } from './store'
import { createMainWindow, getMainWindow } from './window'
import { registerBridge } from './bridge'
import { checkForUpdate } from './updater'
import { downloadAndInstall, restartInto } from './install'
import { recordError, flushOnShutdown } from './clientLog'
import { APP_USER_MODEL_ID } from './identity'

/**
 * Last-resort safety net for the main process.
 *
 * Electron's default behaviour for an uncaught exception here is to show a
 * modal "A JavaScript error occurred in the main process" dialog. That blocks
 * the app on an error the user can do nothing about, and hides the real one.
 * The shell is long-lived and its network/SSH plumbing can fail at any time
 * (an ECONNRESET from a forwarded socket reaching a bare `pipe()` chain is the
 * case that prompted this), so a stray error must degrade rather than take the
 * app down.
 *
 * Per-operation handlers are still required — this only catches what they miss,
 * and must not be used to swallow errors deliberately. Registered at module
 * scope so a failure during startup is covered too.
 */
process.on('uncaughtException', (err) => {
  console.error('[main] uncaught exception:', err)
  recordError('Main', err)
})
process.on('unhandledRejection', (reason) => {
  console.error('[main] unhandled rejection:', reason)
  recordError('Main', reason)
})

/**
 * Desktop self-upgrade.
 *
 * The shell owns this end to end (check → confirm → download → install →
 * confirm → restart) rather than delegating to the web UI, because the web UI
 * is served by the SERVER: it has no way to replace the desktop binary, and it
 * would be unavailable exactly when the shell is pointed at an unreachable or
 * outdated server.
 *
 * Nothing happens without an explicit confirmation. Restarting the app is
 * disruptive — it would drop whatever the user is doing — so the download and
 * the restart are each gated on a native dialog.
 */
async function promptAndInstallUpdate(): Promise<void> {
  let info
  try {
    info = await checkForUpdate()
  } catch {
    // A registry that is unreachable or slow must never block startup.
    return
  }
  if (!info.hasUpdate || info.urls.length === 0) return

  const parent = getMainWindow() ?? undefined
  const { response } = await dialog.showMessageBox(parent as BrowserWindow, {
    type: 'info',
    buttons: ['下载并安装', '稍后'],
    defaultId: 0,
    cancelId: 1,
    title: '发现新版本',
    message: `ClawBench 桌面版 ${info.version} 可用（当前 ${app.getVersion()}）`,
    detail: '安装完成后需要重启应用。',
  })
  if (response !== 0) return

  try {
    await downloadAndInstall(info.urls, info.version, '')
  } catch (err) {
    await dialog.showMessageBox(parent as BrowserWindow, {
      type: 'error',
      buttons: ['确定'],
      title: '更新失败',
      message: '下载或安装新版本失败，当前版本不受影响。',
      detail: String((err as Error)?.message || err),
    })
    return
  }

  const { response: restart } = await dialog.showMessageBox(parent as BrowserWindow, {
    type: 'info',
    buttons: ['立即重启', '稍后'],
    defaultId: 0,
    cancelId: 1,
    title: '更新已就绪',
    message: `ClawBench 桌面版 ${info.version} 已安装。`,
    // The pointer is already flipped, so "later" still lands on the new
    // version the next time the app starts via the launcher.
    detail: '重启后生效。选择“稍后”也可在下次启动时自动使用新版本。',
  })
  if (restart !== 0) return

  try {
    restartInto(info.version)
  } catch (err) {
    await dialog.showMessageBox(parent as BrowserWindow, {
      type: 'error',
      buttons: ['确定'],
      title: '重启失败',
      message: '无法启动新版本，请手动重启应用。',
      detail: String((err as Error)?.message || err),
    })
  }
}

app.whenReady().then(() => {
  // Windows toast notifications are addressed by Application User Model ID, and
  // without an explicit one Electron falls back to the generic
  // `electron.app.Electron`. The consequences on Windows are that the
  // notification is attributed to "Electron" rather than ClawBench (or does not
  // appear at all, because Windows only shows toasts for an AUMID it can
  // resolve to an installed shortcut).
  //
  // Must be set before any Notification is constructed, hence at the top of the
  // ready handler rather than lazily. The value matches electron-builder's
  // `appId` (desktop/electron-builder.yml) so the toast, the Start Menu
  // shortcut and the version resource all agree on one identity. It is
  // duplicated here because electron-builder.yml is a build-time file that is
  // NOT shipped inside app.asar, so the runtime has no way to read it.
  //
  // Documented as win32-only, but verified safe to call on Linux/macOS (no-op,
  // does not throw) — so this needs no platform guard.
  app.setAppUserModelId(APP_USER_MODEL_ID)

  // No OS menu bar — the app is fully UI-driven.
  Menu.setApplicationMenu(null)
  initStore()
  registerBridge()
  createMainWindow()

  // Grant microphone access so voice input (getUserMedia) works in the
  // desktop shell when the server is served over a secure context
  // (localhost or HTTPS). Deny other permission requests by default.
  session.defaultSession.setPermissionRequestHandler((_wc, permission, callback) => {
    callback(permission === 'media')
  })

  // Give the window a moment to appear before a modal dialog can steal focus.
  setTimeout(() => { void promptAndInstallUpdate() }, 3000)

  app.on('activate', () => { if (BrowserWindow.getAllWindows().length === 0) createMainWindow() })
})

app.on('window-all-closed', () => { if (process.platform !== 'darwin') app.quit() })

// Last chance to ship buffered shell logs before the process goes away.
// `before-quit` is async-unfriendly (Electron does not await handlers), so the
// flush races the exit; the renderer relay has the same property. Worth doing
// because the entries most likely still buffered are the ones around whatever
// caused the quit.
app.on('before-quit', () => { void flushOnShutdown() })
