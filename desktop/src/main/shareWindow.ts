/**
 * Opening a share link in the desktop client.
 *
 * The browser can watch, but it cannot share a screen without HTTPS, cannot
 * capture system audio, and can never be driven by the host. The desktop shell
 * has all three — the only missing piece was a way to OPEN the link inside it.
 *
 * The window reuses the same preload as the app, so the share page sees
 * `window.ClawBenchNative` and switches itself into client mode.
 */
import { BrowserWindow, shell } from 'electron'
import path from 'node:path'
import { isShareUrl } from '../shared/shareLink'

let shareWindow: BrowserWindow | null = null

/**
 * Opens the share page in its own window.
 *
 * A separate window rather than navigating the main one: the member is still
 * using the app (chat, sessions) while watching, and navigating away would also
 * destroy the renderer's state.
 */
export function openShareWindow(raw: string): boolean {
  if (!isShareUrl(raw)) return false

  if (shareWindow && !shareWindow.isDestroyed()) {
    shareWindow.loadURL(raw)
    shareWindow.focus()
    return true
  }

  shareWindow = new BrowserWindow({
    width: 1280,
    height: 800,
    minWidth: 720,
    minHeight: 480,
    title: 'ClawBench',
    backgroundColor: '#0d1117',
    webPreferences: {
      preload: path.join(__dirname, '../preload/index.js'),
      contextIsolation: true,
      nodeIntegration: false,
    },
  })

  // The share page must not be able to navigate this window anywhere else: it
  // is a capability window (it can capture the screen and be remote-controlled).
  shareWindow.webContents.on('will-navigate', (event, target) => {
    if (target === raw) return
    event.preventDefault()
    if (/^https?:/i.test(target)) void shell.openExternal(target)
  })
  shareWindow.webContents.setWindowOpenHandler(({ url }) => {
    void shell.openExternal(url)
    return { action: 'deny' }
  })

  shareWindow.on('closed', () => {
    shareWindow = null
  })

  void shareWindow.loadURL(raw)
  return true
}
