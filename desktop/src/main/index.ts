import { app, BrowserWindow, Menu, globalShortcut, session } from 'electron'
import { initStore } from './store'
import { createMainWindow, getMainWindow } from './window'
import { registerBridge } from './bridge'
import { checkForUpdate } from './updater'
import { clearCacheAndReload } from './session'

app.whenReady().then(() => {
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

  // Ctrl+F5 (Cmd+Shift+R on macOS): hard refresh clearing cached resources.
  const accelerator = process.platform === 'darwin' ? 'CommandOrControl+Shift+R' : 'Control+F5'
  globalShortcut.register(accelerator, () => { void clearCacheAndReload() })

  checkForUpdate().then(info => {
    if (info.hasUpdate) getMainWindow()?.webContents.send('clawbench-update-available', info)
  }).catch(() => {})
  app.on('activate', () => { if (BrowserWindow.getAllWindows().length === 0) createMainWindow() })
})

app.on('will-quit', () => {
  globalShortcut.unregisterAll()
})

app.on('window-all-closed', () => { if (process.platform !== 'darwin') app.quit() })
