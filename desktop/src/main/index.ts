import { app, BrowserWindow, Menu } from 'electron'
import { initStore } from './store'
import { createMainWindow, getMainWindow } from './window'
import { registerBridge } from './bridge'
import { checkForUpdate } from './updater'

app.whenReady().then(() => {
  // No OS menu bar — the app is fully UI-driven (frameless).
  Menu.setApplicationMenu(null)
  initStore()
  registerBridge()
  createMainWindow()
  checkForUpdate().then(info => {
    if (info.hasUpdate) getMainWindow()?.webContents.send('clawbench-update-available', info)
  }).catch(() => {})
  app.on('activate', () => { if (BrowserWindow.getAllWindows().length === 0) createMainWindow() })
})

app.on('window-all-closed', () => { if (process.platform !== 'darwin') app.quit() })
