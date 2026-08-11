import { BrowserWindow, shell } from 'electron'
import path from 'node:path'
import { getStore } from './store'

let mainWindow: BrowserWindow | null = null

export function getMainWindow(): BrowserWindow | null { return mainWindow }

function loginPagePath(): string {
  return path.join(process.resourcesPath, 'login.html')
}

export function createMainWindow(): BrowserWindow {
  mainWindow = new BrowserWindow({
    width: 1280, height: 800, show: false,
    frame: false, // frameless: fully show the app UI, no OS title/menu bar
    webPreferences: { preload: path.join(__dirname, '../preload/index.js'), contextIsolation: true, nodeIntegration: false },
  })
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
  mainWindow.webContents.on('did-fail-load', () => { if (!mainWindow?.isVisible()) showWindow() })
  setTimeout(showWindow, 2000)

  mainWindow.webContents.setWindowOpenHandler(({ url: target }) => {
    shell.openExternal(target)
    return { action: 'deny' }
  })
  mainWindow.on('closed', () => { mainWindow = null })
  return mainWindow
}

export function openSandboxWindow(port: number, protocol: string, host: string, path: string): void {
  const win = new BrowserWindow({
    width: 1000, height: 720,
    webPreferences: { partition: `sandbox-${port}`, contextIsolation: true },
  })
  win.loadURL(`${protocol}://localhost:${port}${path || '/'}`)
}
