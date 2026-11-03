/**
 * Full-screen annotation overlay (desktop shell).
 *
 * A transparent, always-on-top window over the display being shared, so the
 * publisher draws straight onto the screen instead of onto a preview window.
 *
 * Whether the ink travels as control frames depends on whether the overlay can
 * be hidden from the capture:
 *
 *   - Windows (`SetWindowDisplayAffinity` / WDA_EXCLUDEFROMCAPTURE) and macOS
 *     (`NSWindowSharingNone`) can, through `BrowserWindow.setContentProtection`.
 *     The audience therefore cannot see the overlay, so every stroke is
 *     forwarded to the app renderer as a control frame — instant, and it costs
 *     no video bitrate.
 *   - Linux has no equivalent. The overlay IS part of the captured picture, so
 *     forwarding strokes would draw each one twice. Nothing is forwarded there:
 *     the ink reaches the audience inside the video.
 *
 * That decision is made here, in the main process, because it is the only place
 * that knows the platform — the renderer just broadcasts whatever it receives.
 */
import { BrowserWindow, ipcMain, screen } from 'electron'
import path from 'node:path'
import { record } from './clientLog'
import { currentCaptureDisplayId } from './capture'
import { getMainWindow } from './window'

const TAG = 'Annotate'

/** Whether a window can be kept out of the screen capture on this platform. */
export function annotateHiddenFromCapture(platform: NodeJS.Platform = process.platform): boolean {
  return platform === 'win32' || platform === 'darwin'
}

let win: BrowserWindow | null = null

function htmlPath(): string {
  return path.join(process.resourcesPath, 'annotate.html')
}

function preloadPath(): string {
  return path.join(__dirname, '../preload/annotate.js')
}

/**
 * Bounds of the display being shared.
 *
 * `currentCaptureDisplayId()` is empty on Wayland (the portal never names the
 * screen), so the primary display is the fallback — the only choice that is
 * still correct on a single-monitor host, which is the common case.
 */
function targetBounds(): Electron.Rectangle {
  const id = currentCaptureDisplayId()
  const display = id ? screen.getAllDisplays().find((d) => String(d.id) === id) : undefined
  return (display ?? screen.getPrimaryDisplay()).bounds
}

export interface AnnotateOverlayResult {
  opened: boolean
  /** True when strokes must be forwarded as control frames (see module doc). */
  inkViaControl: boolean
}

export async function openAnnotateOverlay(): Promise<AnnotateOverlayResult> {
  const inkViaControl = annotateHiddenFromCapture()
  if (win && !win.isDestroyed()) {
    // Already open: raise it AND ask it to show the toolbar again. The toolbar
    // fades out on its own where it would be part of the shared picture, and
    // "标注画面" is the only control the host has outside the overlay — a click
    // that only re-focuses an invisible bar looks like a dead button.
    win.focus()
    win.webContents.send('annotate:wake')
    return { opened: true, inkViaControl }
  }
  const b = targetBounds()
  win = new BrowserWindow({
    x: b.x,
    y: b.y,
    width: b.width,
    height: b.height,
    show: false,
    frame: false,
    transparent: true,
    // A transparent window still paints its background unless this is explicit;
    // the default white would hide the desktop underneath.
    backgroundColor: '#00000000',
    hasShadow: false,
    resizable: false,
    movable: false,
    minimizable: false,
    maximizable: false,
    fullscreenable: false,
    skipTaskbar: true,
    alwaysOnTop: true,
    // The publisher clicks straight from another app into the overlay; without
    // this the first click is swallowed as a focus click.
    acceptFirstMouse: true,
    webPreferences: {
      preload: preloadPath(),
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true,
      partition: 'clawbench-annotate',
    },
  })
  win.setMenuBarVisibility(false)
  // 'screen-saver' outranks other topmost windows, same as the status capsule.
  win.setAlwaysOnTop(true, 'screen-saver', 1)
  // No-op on Linux — which is exactly why the ink path differs there.
  win.setContentProtection(inkViaControl)
  if (process.platform === 'darwin') {
    win.setVisibleOnAllWorkspaces(true, { visibleOnFullScreen: true })
  }
  win.on('closed', () => {
    win = null
    getMainWindow()?.webContents.send('native:annotate-closed')
  })
  try {
    // The page hides its toolbar by itself so it does not sit in the middle of
    // the shared picture — but only where the overlay IS the picture (Linux).
    // Where the window is excluded from the capture the audience never sees the
    // bar, so hiding it only makes the tool disappear for the host.
    await win.loadFile(htmlPath(), { query: { hideBar: inkViaControl ? '0' : '1' } })
  } catch (e) {
    // A package without the document (or a dev run, where it is not in
    // resourcesPath) must fall back to the in-app surface instead of leaving
    // the button looking dead.
    record('E', TAG, `overlay failed to load: ${String(e)}`)
    closeAnnotateOverlay()
    return { opened: false, inkViaControl }
  }
  win.show()
  return { opened: true, inkViaControl }
}

export function closeAnnotateOverlay(): void {
  const w = win
  win = null
  if (!w || w.isDestroyed()) return
  try {
    w.close()
  } catch (e) {
    record('W', TAG, `close failed: ${String(e)}`)
  }
}

export function isAnnotateOverlayOpen(): boolean {
  return !!win && !win.isDestroyed()
}

/**
 * Whether an IPC message came from the app window.
 *
 * The same preload is loaded by the share page, which renders conference data —
 * exactly the kind of content that must not be able to open a full-screen,
 * always-on-top, click-swallowing overlay. `remoteControl` guards its channels
 * the same way and for the same reason.
 */
function isMainSender(sender: unknown): boolean {
  const main = getMainWindow()
  return !!main && sender === main.webContents
}

/** Whether an IPC message came from the annotation overlay document. */
function isOverlaySender(sender: unknown): boolean {
  return !!win && !win.isDestroyed() && sender === win.webContents
}

export function registerAnnotateIpc(): void {
  ipcMain.handle('native:annotate-open', (e) => {
    if (!isMainSender(e.sender)) return { opened: false, inkViaControl: annotateHiddenFromCapture() }
    return openAnnotateOverlay()
  })
  ipcMain.handle('native:annotate-close', (e) => {
    if (!isMainSender(e.sender)) return
    closeAnnotateOverlay()
  })
  // Stroke frames from the overlay document. Dropped when the overlay is part
  // of the picture: forwarding them would draw every stroke twice.
  ipcMain.on('annotate:ink', (e, msg: unknown) => {
    if (!isOverlaySender(e.sender)) return
    if (typeof msg !== 'string') return
    if (!annotateHiddenFromCapture()) return
    getMainWindow()?.webContents.send('native:annotate-ink', msg)
  })
  ipcMain.on('annotate:close', (e) => {
    if (!isOverlaySender(e.sender)) return
    closeAnnotateOverlay()
  })
}

/** Test seam: drop the window reference without touching Electron. */
export function _resetAnnotateOverlayForTest(): void {
  win = null
}
