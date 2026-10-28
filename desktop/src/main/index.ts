import { app, BrowserWindow, Menu, session } from 'electron'
import { initStore, getStore } from './store'
import { installDisplayMediaHandler, registerCaptureHandlers } from './capture'
import { createMainWindow, getMainWindow } from './window'
import { registerBridge } from './bridge'
import { registerCertificateHandler } from './certificate'
import { handOffToPointedVersion } from './install'
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

// The desktop shell loads the server over plain HTTP on a LAN address, which
// Chromium does NOT treat as a secure context. Without a secure context
// `navigator.mediaDevices` is undefined, so microphone capture, screen capture
// and WebCodecs are all unreachable — voice input has been broken in this shell
// for exactly that reason.
//
// `unsafely-treat-insecure-origin-as-secure` promotes ONE origin to secure-context
// treatment. It is deliberately scoped to the server the user configured, not
// applied globally, and it must be set before the app is ready (Chromium reads
// command-line switches during startup).
//
// Consequence to be aware of: this is a process-level switch, so switching to a
// different server needs a restart before capture works against it. The renderer
// surfaces that in the connect flow.
function applySecureContextSwitch(): void {
  try {
    const url = getStore().get('serverUrl')
    if (!url) return
    const origin = new URL(url).origin
    if (!origin.startsWith('http://')) return // https is already secure
    app.commandLine.appendSwitch('unsafely-treat-insecure-origin-as-secure', origin)
  } catch {
    // A malformed stored URL is reported by the connect flow; nothing to do here.
  }
}

// The store must exist before the switch can read serverUrl, and the switch must
// run before `whenReady` — hence the early init.
initStore()
applySecureContextSwitch()

app.whenReady().then(() => {
  // Self-upgrade handoff. `downloadAndInstall` unpacks the new version and
  // writes the `current` pointer, but a running process cannot replace its own
  // executable — so the switch happens HERE, on the next start: if the pointer
  // names a different installed version, start that one and quit this process.
  //
  // This used to be an npm launcher's job, but desktop distribution moved to
  // GitHub Releases (and npm publishing was removed), so users run the unpacked
  // binary directly. Without resolving the pointer here it would be written but
  // never read, and an upgrade would silently revert on the next cold start.
  //
  // Runs before any window exists: showing a window and then quitting would
  // flash a window on every upgrade launch.
  if (handOffToPointedVersion(app.getVersion())) {
    app.exit(0)
    return
  }

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
  registerBridge()
  // Prompt (once per connection) when a server's TLS certificate cannot be
  // verified, instead of failing the navigation outright. Mirrors Android's
  // "Trust and continue" dialog; without this a self-hosted server with a
  // self-signed certificate was unreachable from the desktop shell.
  registerCertificateHandler()
  // The version-consistency gate runs as part of connecting (see window.ts and
  // bridge.ts), where the server URL is known. It used to run here on a 3s timer
  // as an OS dialog; that path is gone so the blocking overlay is the only
  // prompt.
  createMainWindow()

  // Grant microphone/camera ('media') and screen capture ('display-capture') so
  // voice input and live screen sharing work in the desktop shell. These are two
  // distinct permission values: allowing only 'media' makes getDisplayMedia fail
  // with NotAllowedError even though getUserMedia succeeds. Everything else is
  // denied by default.
  const allowedPermissions = new Set(['media', 'display-capture'])
  session.defaultSession.setPermissionRequestHandler((_wc, permission, callback) => {
    callback(allowedPermissions.has(permission))
  })
  // Electron asks for BOTH handlers to be implemented: most web APIs run a
  // permission CHECK before making a request, and a missing check handler makes
  // that check fail — which shows up as `enumerateDevices()` returning devices
  // with empty labels, and as some capture paths never reaching the request.
  session.defaultSession.setPermissionCheckHandler((_wc, permission) => {
    return allowedPermissions.has(permission)
  })
  // Answer getDisplayMedia from the source the renderer declared. Installed
  // here (after ready) because the default session does not exist earlier.
  installDisplayMediaHandler()
  // Source listing / declaration / throttling IPC.
  registerCaptureHandlers(() => getMainWindow()?.webContents ?? null)

  app.on('activate', () => { if (BrowserWindow.getAllWindows().length === 0) createMainWindow() })
})

app.on('window-all-closed', () => { if (process.platform !== 'darwin') app.quit() })

// Last chance to ship buffered shell logs before the process goes away.
// `before-quit` is async-unfriendly (Electron does not await handlers), so the
// flush races the exit; the renderer relay has the same property. Worth doing
// because the entries most likely still buffered are the ones around whatever
// caused the quit.
app.on('before-quit', () => { void flushOnShutdown() })
