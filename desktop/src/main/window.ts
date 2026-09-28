import { app, BrowserWindow, Menu, MenuItem, clipboard, shell } from 'electron'
import path from 'node:path'
import { pathToFileURL } from 'node:url'
import { getStore } from './store'
import { contextMenuLabels } from './contextMenu'
import { classifyUrl } from './urlPolicy'
import { markRendererLoading } from './navReady'
import { handleShortcut } from './shortcuts'
import { shouldFallBackToLogin, buildConnectErrorScript } from './loadFailure'
import { createSplashController, type SplashController } from './splash'
import {
  beginNavigation,
  checkVersionGate,
  getActiveGate,
  installServerVersion,
  setActiveGate,
} from './versionGate'
import { nextZoomFactor, type ZoomAction } from './zoom'
import { shouldUseFramelessWindow } from './windowChrome'
import { WINDOW_STATE_CHANNEL, type WindowState } from '../shared/types'

let mainWindow: BrowserWindow | null = null

/**
 * The native loading overlay for the main window, if one is up.
 *
 * Kept at module scope because the bridge (`native:dismiss-splash` from the
 * app, `native:splash-cancel` from the overlay's cancel button) and the window
 * lifecycle both need it, and they reach it through the exported helpers below
 * rather than each holding their own reference.
 */
let splash: SplashController | null = null

/** Show the loading overlay for a navigation to `url`, if it warrants one. */
export function showSplashFor(url: string): void {
  splash?.show(url)
}

/** Hide the loading overlay. Idempotent; safe when none is up. */
export function dismissSplash(): void {
  splash?.dismiss()
}

/**
 * Abort the in-flight connection from the overlay's cancel button: stop the
 * navigation, hide the overlay, and return to the server-selection page.
 */
export function cancelSplash(): void {
  splash?.cancel()
}

/**
 * Check the desktop/server version consistency for a navigation to `url` and,
 * on a mismatch, raise the blocking gate. Fire-and-forget: the check is
 * asynchronous and its result is applied only if the navigation is still the
 * current one.
 */
export function checkVersionGateFor(url: string): void {
  const gen = beginNavigation()
  void checkVersionGate(url, gen)
    .then((info) => {
      if (!info) {
        // No mismatch (or nothing to compare): make sure a gate from an earlier
        // navigation cannot be acted on.
        setActiveGate(null)
        return
      }
      setActiveGate(info)
      splash?.showVersionMismatch(info)
    })
    .catch(() => { /* a gate check must never break a connection */ })
}

/** Dismiss the version gate (the overlay's "continue" action). */
export function continueVersionGate(): void {
  setActiveGate(null)
  splash?.continueGate()
}

/**
 * Abandon any version-gate work for the current navigation.
 *
 * Bumps the navigation generation so an in-flight check cannot raise the gate
 * afterwards, and clears any gate already up. Used when the connection fails or
 * the user returns to the login page, where a gate would be meaningless.
 */
export function abortVersionGate(): void {
  beginNavigation()
  setActiveGate(null)
  splash?.continueGate()
}

/**
 * Install the server's version from the gate ("download" action). The gate
 * stays up on failure; on success the app restarts into the new version (or the
 * gate closes when the user defers the restart, so it stops offering a download
 * of the version already on disk).
 */
export async function downloadVersionFromGate(): Promise<void> {
  const info = getActiveGate()
  if (!info) return
  if (await installServerVersion(info, mainWindow)) continueVersionGate()
}

export function getMainWindow(): BrowserWindow | null { return mainWindow }

function loginPagePath(): string {
  return path.join(process.resourcesPath, 'login.html')
}

/**
 * Claim the app-level shortcuts (page zoom, hard reload, DevTools) on this window.
 *
 * Uses `before-input-event` rather than `globalShortcut`: a global shortcut is
 * captured by the OS and never reaches the renderer, which would break the
 * page's own use of the same keys — the terminal sends F5/F12 to the running
 * TUI and the file manager refreshes on F5. This runs in the window's own
 * event path, so unclaimed keys fall through to the page untouched.
 *
 * Also gives the window Ctrl+Wheel page zoom. `webContents` announces that
 * gesture with `zoom-changed` but does not apply it (measured on Electron 44 —
 * the built-in handling is gone now that the app has no menu bar), so the shell
 * has to set the factor itself.
 *
 * Keys over an element that calls `preventDefault()` on Ctrl+Wheel never reach
 * this event at all, which is what keeps the terminal, PDF and office previews
 * on their own Ctrl+Wheel zoom instead of zooming the whole page. Verified
 * against real input: three events over a plain area, zero over a
 * `preventDefault` region.
 *
 * The zoom mode is intentionally left at Chromium's `default`. `manual` reads
 * as the tidier option but is not: it neither applies the factor live nor
 * persists it, so it would silently break both the visual result and the
 * per-origin memory across restarts.
 */
function registerKeyboardShortcuts(webContents: Electron.WebContents): void {
  const isMac = process.platform === 'darwin'

  const applyZoom = (action: ZoomAction) => {
    webContents.setZoomFactor(nextZoomFactor(webContents.getZoomFactor(), action))
  }

  webContents.on('zoom-changed', (_event, direction) => {
    applyZoom(direction === 'in' ? 'in' : 'out')
  })

  webContents.on('before-input-event', (event, input) => {
    const claimed = handleShortcut(input, isMac, {
      // Imported lazily: session.ts imports getMainWindow() from this module,
      // so a top-level import here would be a cycle.
      onHardReload: () => { void import('./session').then((m) => m.clearCacheAndReload()) },
      onToggleDevTools: () => {
        const wc = webContents
        if (wc.isDevToolsOpened()) wc.closeDevTools()
        else wc.openDevTools({ mode: 'bottom' })
      },
      onZoom: applyZoom,
    })
    // preventDefault stops the renderer from also seeing a key we handled.
    if (claimed) event.preventDefault()
  })
}

/** Register native context menu handlers for text selection, editable fields, links, and images. */
function registerContextMenu(webContents: Electron.WebContents): void {
  // cut/copy/paste use Electron roles (auto-localized by the OS); only the
  // custom items are translated from the current app locale.
  const labels = contextMenuLabels(app.getLocale())

  webContents.on('context-menu', (_e, params) => {
    const menu = new Menu()

    // 1. Editable inputs (input, textarea, contenteditable)
    if (params.isEditable) {
      const hasSelection = params.selectionText.trim().length > 0
      if (hasSelection) {
        menu.append(new MenuItem({ role: 'cut', enabled: params.editFlags.canCut }))
        menu.append(new MenuItem({ role: 'copy', enabled: params.editFlags.canCopy }))
      }
      menu.append(new MenuItem({ role: 'paste', enabled: params.editFlags.canPaste }))
      if (params.linkURL) {
        menu.append(new MenuItem({
          label: labels.copyLink,
          click: () => { clipboard.writeText(params.linkURL) },
        }))
      }
    } else {
      // 2. Normal text selection outside editable inputs
      if (params.selectionText.trim().length > 0) {
        menu.append(new MenuItem({ role: 'copy', enabled: params.editFlags.canCopy }))
      }

      // 3. Link (supports both web URLs and in-app file/anchor links)
      if (params.linkURL) {
        menu.append(new MenuItem({
          label: labels.copyLink,
          click: () => { clipboard.writeText(params.linkURL) },
        }))
      }

      // 4. Image copying
      if ((params.mediaType === 'image' || params.hasImageContents) && !params.selectionText.trim()) {
        menu.append(new MenuItem({
          label: labels.copyImage,
          click: () => { webContents.copyImageAt(params.x, params.y) },
        }))
      }
    }

    if (menu.items.length > 0) {
      menu.popup()
    }
  })
}

/**
 * Keep the renderer's view of the maximize state in step with the window's.
 *
 * The header's maximize/restore button must show the right glyph, but the state
 * can change from places the renderer cannot see: our own IPC toggle, an OS
 * snap/tile, or a double-click on the drag region (Electron's built-in
 * behaviour for a draggable region, which does NOT go through our IPC at all).
 * Listening to the window events therefore covers every path, whereas mirroring
 * state in the renderer after each click would miss the others.
 *
 * The initial value is NOT pushed here: a push on load would race the
 * renderer's own subscription (the preload's listener fires before Vue mounts),
 * so the renderer instead queries the current state once when it mounts. That
 * makes the initial read deterministic instead of order-dependent.
 */
function registerWindowStateReporting(win: BrowserWindow): void {
  const send = () => {
    if (win.isDestroyed()) return
    const state: WindowState = { maximized: win.isMaximized() }
    win.webContents.send(WINDOW_STATE_CHANNEL, state)
  }
  win.on('maximize', send)
  win.on('unmaximize', send)
}

/** Minimize the main window (frameless header control). */
export function minimizeMainWindow(): void {
  if (mainWindow && !mainWindow.isDestroyed()) mainWindow.minimize()
}

/**
 * Toggle the main window between maximized and restored (frameless header
 * control). Toggling rather than setting avoids the two sides disagreeing about
 * the current state — the window is the authority.
 */
export function toggleMaximizeMainWindow(): void {
  if (!mainWindow || mainWindow.isDestroyed()) return
  if (mainWindow.isMaximized()) mainWindow.unmaximize()
  else mainWindow.maximize()
}

/** Close the main window (frameless header control). */
export function closeMainWindow(): void {
  if (mainWindow && !mainWindow.isDestroyed()) mainWindow.close()
}

/** Whether the main window is currently maximized (frameless header control). */
export function isMainWindowMaximized(): boolean {
  return !!mainWindow && !mainWindow.isDestroyed() && mainWindow.isMaximized()
}

export function createMainWindow(): BrowserWindow {
  mainWindow = new BrowserWindow({
    width: 1280, height: 800, show: false,
    // Windows/Linux get a frameless window and draw their own controls in the
    // header (see windowChrome.ts). macOS keeps the native frame so its
    // top-left traffic lights stay where users expect them. A frameless window
    // is NOT draggable by default — the header supplies the drag region — and
    // is still resizable on Windows/Linux; on Wayland Electron gives frameless
    // windows GTK shadow plus an extended resize border.
    frame: !shouldUseFramelessWindow(process.platform),
    webPreferences: { preload: path.join(__dirname, '../preload/index.js'), contextIsolation: true, nodeIntegration: false },
  })
  registerContextMenu(mainWindow.webContents)
  registerKeyboardShortcuts(mainWindow.webContents)
  registerWindowStateReporting(mainWindow)
  // A (re)load tears down the renderer's listeners, so anything clicked before
  // it re-registers must be deferred rather than sent into the void.
  mainWindow.webContents.on('did-start-loading', () => markRendererLoading())

  // Native loading overlay, floating above the window's own page. Created before
  // the first navigation so a cold start can cover it too.
  splash?.destroy()
  splash = createSplashController(mainWindow, {
    onAbort: (reason) => {
      // Both paths land on the login page. A timeout also reports why, through
      // the same onConnectError hook the did-fail-load fallback uses — without
      // it the user would be returned to the login page with no explanation.
      void mainWindow?.loadFile(loginPagePath())
        .then(() => {
          if (reason !== 'timeout') return
          const wc = mainWindow?.webContents
          if (!wc) return
          void wc.executeJavaScript(buildConnectErrorScript('Connection timed out'), true)
        })
        .catch(() => { /* login page itself failed to load; nothing to report to */ })
    },
  })

  const serverUrl = getStore().get('serverUrl')
  if (serverUrl) {
    // Cold start with a saved server: same gap as connecting from the login
    // page, so the overlay covers it here too.
    showSplashFor(serverUrl)
    mainWindow.loadURL(serverUrl)
    // The version gate runs alongside the load; on a mismatch it replaces the
    // loading overlay with the blocking gate.
    checkVersionGateFor(serverUrl)
  } else {
    // First run: no server configured — show a built-in login page to enter the server URL.
    // No overlay: the login page is a local document with nothing to wait for.
    mainWindow.loadFile(loginPagePath())
  }

  // Ensure the window is always shown, even if the page fails to load (e.g. no
  // server at the configured URL), so the user is never left with a hidden window.
  const showWindow = () => { if (mainWindow && !mainWindow.isDestroyed()) mainWindow.show() }
  mainWindow.once('ready-to-show', showWindow)
  mainWindow.webContents.on('did-fail-load', (_e, errorCode, errorDesc, failedUrl, isMainFrame) => {
    showWindow()
    const loginUrl = pathToFileURL(loginPagePath()).toString()
    // Subframe failures, cancelled navigations and a failure of the login page
    // itself must not hijack the window — see shouldFallBackToLogin.
    if (!shouldFallBackToLogin({ errorCode, failedUrl, loginUrl, isMainFrame })) return
    // The server is unreachable, so the overlay's premise (something is
    // loading) no longer holds — drop it before showing the login page.
    dismissSplash()
    // And abandon any version-gate check for this navigation: a mismatch against
    // an unreachable server is not actionable, and its result must not cover the
    // login page the user is about to see.
    abortVersionGate()
    // Server page failed to load (unreachable) — fall back to the server-selection
    // login page so the user can pick another server instead of a blank page.
    // loadFile resolves once the page is ready, so the failure can then be
    // handed to the page's onConnectError() — the same hook Android calls.
    // Without it the fallback was silent: the user got the login page back
    // with no indication of why the connection failed.
    void mainWindow?.loadFile(loginPagePath())
      .then(() => {
        const wc = mainWindow?.webContents
        if (!wc) return
        void wc.executeJavaScript(buildConnectErrorScript(errorDesc || ''), true)
      })
      .catch(() => { /* login page itself failed to load; nothing to report to */ })
  })
  setTimeout(showWindow, 2000)

  // Open external web links in the user's default browser instead of navigating within the app window.
  mainWindow.webContents.on('will-navigate', (event, url) => {
    const disposition = classifyUrl(url, getStore().get('serverUrl'))
    if (disposition === 'external') {
      event.preventDefault()
      void shell.openExternal(url)
    } else if (disposition === 'block') {
      // Block file:, javascript:, data:, and unknown schemes rather than
      // letting the window navigate to (or the OS handle) arbitrary content.
      event.preventDefault()
    }
  })

  mainWindow.webContents.setWindowOpenHandler(({ url: target }) => {
    if (classifyUrl(target, getStore().get('serverUrl')) === 'external') {
      void shell.openExternal(target)
    }
    return { action: 'deny' }
  })
  mainWindow.on('closed', () => {
    splash?.destroy()
    splash = null
    mainWindow = null
  })
  return mainWindow
}

/** Navigate the main window back to the server-selection login page. */
export function showLoginPage(): void {
  if (mainWindow && !mainWindow.isDestroyed()) {
    // Reached from the settings "reconfigure server" action and from the
    // notification deep-link fallback. The overlay must come down, or it would
    // sit on top of the login page the user just asked for.
    dismissSplash()
    // A version gate is meaningless on the login page; drop it and invalidate
    // any in-flight check so it cannot reappear here.
    abortVersionGate()
    mainWindow.loadFile(loginPagePath())
  }
}

export function openSandboxWindow(port: number, protocol: string, host: string, path: string): void {
  const win = new BrowserWindow({
    width: 1000, height: 720,
    webPreferences: { partition: `sandbox-${port}`, contextIsolation: true },
  })
  registerContextMenu(win.webContents)
  win.loadURL(`${protocol}://localhost:${port}${path || '/'}`)
}
