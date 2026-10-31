/**
 * Opening a share link in the desktop client.
 *
 * The browser can watch, but it cannot share a screen without HTTPS, cannot
 * capture system audio, and can never be driven by the host. The desktop shell
 * has all three — the only missing piece was a way to OPEN the link inside it.
 *
 * The window reuses the same preload as the app, so the share page sees
 * `window.ClawBenchNative` and switches itself into client mode. That makes it
 * a capability window: it may only ever show pages from the configured server
 * (see `isShareUrl`), and it may never navigate away from them.
 */
import { BrowserWindow, shell } from 'electron'
import path from 'node:path'
import { isShareUrl } from '../shared/shareLink'
import { classifyUrl } from './urlPolicy'
import { getStore } from './store'

let shareWindow: BrowserWindow | null = null

/**
 * Opens the share page in its own window.
 *
 * A separate window rather than navigating the main one: the member is still
 * using the app (chat, sessions) while watching, and navigating away would also
 * destroy the renderer's state.
 */
export function openShareWindow(raw: string): boolean {
  const serverUrl = getStore().get('serverUrl')
  if (!isShareUrl(raw, serverUrl)) return false

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
  // is a capability window (it can capture the screen and be remote-controlled),
  // and the preload survives a navigation. Same-origin targets are the app
  // itself; everything else goes to the browser or nowhere.
  shareWindow.webContents.on('will-navigate', (event, target) => {
    if (classifyUrl(target, serverUrl) === 'internal') return
    event.preventDefault()
    if (classifyUrl(target, serverUrl) === 'external') void shell.openExternal(target)
  })
  shareWindow.webContents.setWindowOpenHandler(({ url }) => {
    // `openExternal` hands the URL to the OS protocol handler, so it needs the
    // same scheme allow-list as the main window — `file:`/`ms-settings:` here
    // would launch local programs.
    if (classifyUrl(url, serverUrl) === 'external') void shell.openExternal(url)
    return { action: 'deny' }
  })

  shareWindow.on('closed', () => {
    shareWindow = null
  })

  void shareWindow.loadURL(raw)
  return true
}
