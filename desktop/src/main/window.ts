import { BrowserWindow, Menu, MenuItem, clipboard, shell } from 'electron'
import path from 'node:path'
import { pathToFileURL } from 'node:url'
import { getStore } from './store'

let mainWindow: BrowserWindow | null = null

export function getMainWindow(): BrowserWindow | null { return mainWindow }

function loginPagePath(): string {
  return path.join(process.resourcesPath, 'login.html')
}

/** Register native context menu handlers for text selection, editable fields, links, and images. */
function registerContextMenu(webContents: Electron.WebContents): void {
  webContents.on('context-menu', (_e, params) => {
    const menu = new Menu()

    // 1. Editable inputs (input, textarea, contenteditable)
    if (params.isEditable) {
      const hasSelection = params.selectionText.trim().length > 0
      if (hasSelection) {
        menu.append(new MenuItem({ role: 'cut', label: '剪切', enabled: params.editFlags.canCut }))
        menu.append(new MenuItem({ role: 'copy', label: '复制', enabled: params.editFlags.canCopy }))
      }
      menu.append(new MenuItem({ role: 'paste', label: '粘贴', enabled: params.editFlags.canPaste }))
      if (params.linkURL) {
        menu.append(new MenuItem({
          label: '复制链接',
          click: () => { clipboard.writeText(params.linkURL) },
        }))
      }
    } else {
      // 2. Normal text selection outside editable inputs
      if (params.selectionText.trim().length > 0) {
        menu.append(new MenuItem({ role: 'copy', label: '复制', enabled: params.editFlags.canCopy }))
      }

      // 3. Link (supports both web URLs and in-app file/anchor links)
      if (params.linkURL) {
        menu.append(new MenuItem({
          label: '复制链接',
          click: () => { clipboard.writeText(params.linkURL) },
        }))
      }

      // 4. Image copying
      if ((params.mediaType === 'image' || params.hasImageContents) && !params.selectionText.trim()) {
        menu.append(new MenuItem({
          label: '复制图片',
          click: () => { webContents.copyImageAt(params.x, params.y) },
        }))
      }
    }

    if (menu.items.length > 0) {
      menu.popup()
    }
  })
}

/** Check if a target URL is an external link outside the configured ClawBench server. */
function isExternalUrl(targetUrl: string): boolean {
  try {
    const parsed = new URL(targetUrl)
    if (parsed.protocol === 'file:') return false
    if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:' && parsed.protocol !== 'mailto:') {
      return true
    }
    const serverUrl = getStore().get('serverUrl')
    if (!serverUrl) return true
    const serverOrigin = new URL(serverUrl).origin
    return parsed.origin !== serverOrigin
  } catch {
    return false
  }
}

export function createMainWindow(): BrowserWindow {
  mainWindow = new BrowserWindow({
    width: 1280, height: 800, show: false,
    webPreferences: { preload: path.join(__dirname, '../preload/index.js'), contextIsolation: true, nodeIntegration: false },
  })
  registerContextMenu(mainWindow.webContents)
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
  mainWindow.webContents.on('did-fail-load', (_e, _code, _desc, failedUrl) => {
    showWindow()
    // Server page failed to load (unreachable) — fall back to the server-selection
    // login page so the user can pick another server instead of a blank page.
    const loginUrl = pathToFileURL(loginPagePath()).toString()
    if (failedUrl && failedUrl !== loginUrl) {
      mainWindow?.loadFile(loginPagePath())
    }
  })
  setTimeout(showWindow, 2000)

  // Open external web links in the user's default browser instead of navigating within the app window.
  mainWindow.webContents.on('will-navigate', (event, url) => {
    if (isExternalUrl(url)) {
      event.preventDefault()
      void shell.openExternal(url)
    }
  })

  mainWindow.webContents.setWindowOpenHandler(({ url: target }) => {
    if (isExternalUrl(target)) {
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
