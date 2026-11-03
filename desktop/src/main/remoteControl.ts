/**
 * Remote control: apply a viewer's pointer/keyboard events to this machine.
 *
 * WHY A CHILD PROCESS AND NOT A NATIVE MODULE
 * -------------------------------------------
 * Electron cannot move the OS pointer — `sendInputEvent` only reaches our own
 * windows. The usual answer is a native npm module (robotjs / nut.js), which
 * means a compiled dependency in every desktop build and something the user has
 * to trust. Instead the app ships a tiny Go helper (`input-helper.exe`, pure
 * syscall, no CGO) and spawns it only while someone actually holds control.
 *
 * The helper speaks newline-delimited JSON: one request line in, one
 * `{ok,error}` line out. A long-lived process matters — spawning one per mouse
 * move would cap the pointer at a few updates per second.
 *
 * SECURITY
 * --------
 * This is the most dangerous code in the app: whoever holds control can drive
 * the host's mouse and keyboard. The renderer only calls it after the publisher
 * granted control to that specific viewer, and `revoke` clears the grant on the
 * main side too (see bridge.ts), so a stale renderer cannot keep driving.
 */
import { app, screen } from 'electron'
import { spawn, type ChildProcessWithoutNullStreams } from 'node:child_process'
import fs from 'node:fs'
import path from 'node:path'
import { getMainWindow } from './window'
import { currentCaptureDisplayId } from './capture'
import { record } from './clientLog'

/** Physical-pixel rectangle of the captured display on the virtual desktop. */
export interface DisplayBounds {
  left: number
  top: number
  width: number
  height: number
}

export interface RemoteControlEvent {
  kind: 'move' | 'down' | 'up' | 'wheel' | 'key' | 'text'
  x?: number
  y?: number
  button?: number
  delta?: number
  key?: string
  modifiers?: string[]
  text?: string
  /** Added by the main process; the renderer never sends it. */
  bounds?: DisplayBounds
}

/**
 * The viewer currently allowed to drive this machine, '' when nobody is.
 *
 * Held in the MAIN process on purpose: the renderer's check is the first line of
 * defence, not the only one. Anything that can run script in the window (a
 * remote server's page, an XSS) can call the IPC, so the capability has to be
 * armed here before an event is forwarded.
 */
let grantedViewer = ''
/** Where the granted viewer's picture sits on the virtual desktop (null = unknown). */
let grantBounds: DisplayBounds | null = null

/**
 * The captured display's rectangle in physical pixels.
 *
 * A share captures ONE monitor, but the pointer is positioned across the whole
 * virtual desktop; without this the fraction of the picture is applied to every
 * monitor at once and clicks land on the wrong screen. Electron reports display
 * bounds in DIP, while the injector works in physical pixels, so they are
 * scaled by the display's factor (exact on a uniform-DPI setup, which is the
 * common case; a mixed-DPI desktop is at worst off by the scale difference
 * rather than by a whole monitor).
 */
function capturedDisplayBounds(): DisplayBounds | null {
  const id = currentCaptureDisplayId()
  if (!id) return null
  const display = screen.getAllDisplays().find((d) => String(d.id) === id)
  if (!display) return null
  const scale = display.scaleFactor || 1
  return {
    left: Math.round(display.bounds.x * scale),
    top: Math.round(display.bounds.y * scale),
    width: Math.round(display.bounds.width * scale),
    height: Math.round(display.bounds.height * scale),
  }
}

/** Test seam: the rectangle attached to events, if any. */
export function _grantBoundsForTest(): DisplayBounds | null {
  return grantBounds
}

let child: ChildProcessWithoutNullStreams | null = null
/** Replies arrive in request order, so a FIFO is enough to correlate them. */
let pending: Array<(ok: boolean) => void> = []
let buffer = ''

const TAG = 'RemoteControl'

/**
 * How long one injected event may wait for the helper's reply.
 *
 * The helper can be alive but unable to answer — blocked behind an elevated
 * (UIPI) window, or with its stdin pipe wedged. Pointer moves arrive at IPC
 * rate, so without a deadline every move would leave a promise and a queue
 * entry behind for as long as the wedge lasts.
 */
const EVENT_TIMEOUT_MS = 2000

/** Where the helper lives, packaged and in development. */
function helperPath(): string {
  const name = process.platform === 'win32' ? 'input-helper.exe' : 'input-helper'
  return app.isPackaged
    ? path.join(process.resourcesPath, name)
    : path.join(app.getAppPath(), 'assets', name)
}

/** Whether this install can inject input at all. */
export function remoteControlSupported(): boolean {
  // Only Windows has an injector; the helper is still built elsewhere so the
  // build stays uniform, but it refuses every event there.
  if (process.platform !== 'win32') return false
  return fs.existsSync(helperPath())
}

function ensureHelper(): ChildProcessWithoutNullStreams | null {
  if (child) return child
  const exe = helperPath()
  if (!fs.existsSync(exe)) return null

  const proc = spawn(exe, [], { stdio: ['pipe', 'pipe', 'pipe'], windowsHide: true })
  proc.stdout.setEncoding('utf8')
  proc.stdout.on('data', (chunk: string) => {
    buffer += chunk
    let nl = buffer.indexOf('\n')
    while (nl >= 0) {
      const line = buffer.slice(0, nl)
      buffer = buffer.slice(nl + 1)
      const resolve = pending.shift()
      if (resolve) {
        try {
          const res = JSON.parse(line) as { ok?: boolean }
          resolve(res.ok === true)
        } catch {
          resolve(false)
        }
      }
      nl = buffer.indexOf('\n')
    }
  })
  proc.on('exit', () => {
    // Drain waiters: an event sent to a dead helper must not leave a promise
    // hanging forever.
    const waiters = pending
    pending = []
    for (const w of waiters) w(false)
    child = null
    buffer = ''
  })
  // A write after the process died would throw EPIPE; the exit handler covers
  // the bookkeeping, this just keeps the error out of the console.
  proc.stdin.on('error', () => undefined)
  proc.stderr.resume()

  child = proc
  return child
}

/** Stops the helper. Called when the app quits. */
export function stopRemoteControlHelper(): void {
  resetHelper()
}

/**
 * Kill the helper and fail every request still waiting on it.
 *
 * A wedged helper cannot be recovered in place: replies are correlated by
 * POSITION, so a request that leaves the queue without its reply would shift
 * every later reply onto the wrong event — the pointer would keep working, but
 * driven by stale coordinates. Dropping the whole pipeline and spawning a fresh
 * process is the only state that is known to be consistent, and it costs one
 * spawn.
 */
function resetHelper(): void {
  const proc = child
  child = null
  buffer = ''
  const waiters = pending
  pending = []
  for (const w of waiters) w(false)
  if (!proc) return
  try {
    proc.stdin.end()
    proc.kill()
  } catch {
    /* already gone */
  }
}

/**
 * Applies one event.
 *
 * Returns false when the helper is missing or rejected the event (typically
 * UIPI: the focused window runs elevated and we do not), and when the helper
 * stops answering (see EVENT_TIMEOUT_MS).
 */
export async function applyRemoteControlEvent(ev: RemoteControlEvent): Promise<boolean> {
  const proc = ensureHelper()
  if (!proc) return false
  const line = JSON.stringify(ev)
  return new Promise<boolean>((resolve) => {
    let settled = false
    const finish = (ok: boolean) => {
      if (settled) return
      settled = true
      clearTimeout(timer)
      resolve(ok)
    }
    const timer = setTimeout(() => {
      record('W', TAG, `input helper did not answer within ${EVENT_TIMEOUT_MS}ms — restarting it`)
      resetHelper()
    }, EVENT_TIMEOUT_MS)
    pending.push(finish)
    try {
      proc.stdin.write(line + '\n')
    } catch {
      const i = pending.indexOf(finish)
      if (i >= 0) pending.splice(i, 1)
      finish(false)
    }
  })
}

/**
 * Whether an IPC came from the app's own window.
 *
 * The capability is armed and driven only by the publishing panel, which lives
 * in the main window. A share window (a different renderer with the same
 * preload) must never be able to grant itself control, so the check is on the
 * webContents identity rather than on the page's origin — both windows share an
 * origin, and a share page is exactly the one that could be talked into it.
 */
function isPublisherSender(sender: unknown): boolean {
  const main = getMainWindow()
  return !!main && sender === main.webContents
}

/** Test seam: who currently holds control ('' when nobody does). */
export function _grantedViewerForTest(): string {
  return grantedViewer
}

/**
 * Registers the IPC the renderer calls while it holds remote control.
 *
 * Always registered: the answer carries `supported`, so a missing helper shows
 * up as a clear "not available" instead of a dead button.
 */
export function registerRemoteControlHandlers(ipcMain: Electron.IpcMain): void {
  ipcMain.handle('native:remote-control', async (e, ev: RemoteControlEvent) => {
    if (!isPublisherSender(e.sender)) return { ok: false, supported: true, reason: 'untrusted' }
    if (!remoteControlSupported()) return { ok: false, supported: false }
    // No active grant: the share ended, control was revoked, or nothing ever
    // granted it. Drop the event instead of moving the user's mouse.
    if (!grantedViewer) return { ok: false, supported: true, reason: 'no-grant' }
    // The renderer's coordinates are a fraction of the picture it captured; the
    // injector needs to know which part of the desktop that picture covers.
    const payload = grantBounds ? { ...ev, bounds: grantBounds } : ev
    const ok = await applyRemoteControlEvent(payload)
    return { ok, supported: true }
  })
  ipcMain.handle('native:remote-control-supported', () => remoteControlSupported())
  ipcMain.handle('native:remote-control-grant', (e, viewerId: string) => {
    if (!isPublisherSender(e.sender)) return
    grantedViewer = typeof viewerId === 'string' ? viewerId : ''
    // Resolved at grant time: the capture is already running, so this is the
    // display the viewer is actually looking at.
    grantBounds = grantedViewer ? capturedDisplayBounds() : null
  })
  // Revoking is allowed from any renderer: it only ever removes a capability.
  ipcMain.handle('native:remote-control-revoke', () => {
    grantedViewer = ''
    grantBounds = null
    // Kill the helper too: a live process is a capability that outlives the
    // grant, and restarting it costs one spawn.
    stopRemoteControlHelper()
  })
}
