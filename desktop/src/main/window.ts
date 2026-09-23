import { app, BrowserWindow, Menu, MenuItem, clipboard, shell } from 'electron'
import path from 'node:path'
import { pathToFileURL } from 'node:url'
import { getStore } from './store'
import { contextMenuLabels } from './contextMenu'
import { classifyUrl } from './urlPolicy'
import { markRendererLoading } from './navReady'
import { handleShortcut } from './shortcuts'
import { shouldFallBackToLogin, buildConnectErrorScript } from './loadFailure'
import { nextZoomFactor, type ZoomAction } from './zoom'

let mainWindow: BrowserWindow | null = null

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

export function createMainWindow(): BrowserWindow {
  mainWindow = new BrowserWindow({
    width: 1280, height: 800, show: false,
    webPreferences: { preload: path.join(__dirname, '../preload/index.js'), contextIsolation: true, nodeIntegration: false },
  })
  registerContextMenu(mainWindow.webContents)
  registerKeyboardShortcuts(mainWindow.webContents)
  // A (re)load tears down the renderer's listeners, so anything clicked before
  // it re-registers must be deferred rather than sent into the void.
  mainWindow.webContents.on('did-start-loading', () => markRendererLoading())
  const serverUrl = getStore().get('serverUrl')
  if (serverUrl) {
    mainWindow.loadURL(serverUrl)
  } else {
    // First run: no server configured — show a built-in login page to enter the server URL.
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
  mainWindow.on('closed', () => { mainWindow = null })
  return mainWindow
}

/** Navigate the main window back to the server-selection login page. */
export function showLoginPage(): void {
  if (mainWindow && !mainWindow.isDestroyed()) {
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
