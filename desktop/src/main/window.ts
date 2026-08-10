import { BrowserWindow, shell } from 'electron'
import path from 'node:path'
import { getStore } from './store'

let mainWindow: BrowserWindow | null = null

export function getMainWindow(): BrowserWindow | null { return mainWindow }

export function createMainWindow(): BrowserWindow {
  mainWindow = new BrowserWindow({
    width: 1280, height: 800, show: false,
    webPreferences: { preload: path.join(__dirname, '../preload/index.js'), contextIsolation: true, nodeIntegration: false },
  })
  const url = getStore().get('serverUrl') || 'http://localhost:20000'
  mainWindow.loadURL(url)
  mainWindow.once('ready-to-show', () => mainWindow?.show())
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
