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
import { app } from 'electron'
import { spawn, type ChildProcessWithoutNullStreams } from 'node:child_process'
import fs from 'node:fs'
import path from 'node:path'

export interface RemoteControlEvent {
  kind: 'move' | 'down' | 'up' | 'wheel' | 'key' | 'text'
  x?: number
  y?: number
  button?: number
  delta?: number
  key?: string
  modifiers?: string[]
  text?: string
}

let child: ChildProcessWithoutNullStreams | null = null
/** Replies arrive in request order, so a FIFO is enough to correlate them. */
let pending: Array<(ok: boolean) => void> = []
let buffer = ''

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
  if (!child) return
  try {
    child.stdin.end()
    child.kill()
  } catch {
    /* already gone */
  }
  child = null
}

/**
 * Applies one event.
 *
 * Returns false when the helper is missing or rejected the event (typically
 * UIPI: the focused window runs elevated and we do not).
 */
export async function applyRemoteControlEvent(ev: RemoteControlEvent): Promise<boolean> {
  const proc = ensureHelper()
  if (!proc) return false
  const line = JSON.stringify(ev)
  return new Promise<boolean>((resolve) => {
    pending.push(resolve)
    try {
      proc.stdin.write(line + '\n')
    } catch {
      pending.pop()
      resolve(false)
    }
  })
}

/**
 * Registers the IPC the renderer calls while it holds remote control.
 *
 * Always registered: the answer carries `supported`, so a missing helper shows
 * up as a clear "not available" instead of a dead button.
 */
export function registerRemoteControlHandlers(ipcMain: Electron.IpcMain): void {
  ipcMain.handle('native:remote-control', async (_e, ev: RemoteControlEvent) => {
    if (!remoteControlSupported()) return { ok: false, supported: false }
    const ok = await applyRemoteControlEvent(ev)
    return { ok, supported: true }
  })
  ipcMain.handle('native:remote-control-supported', () => remoteControlSupported())
}
