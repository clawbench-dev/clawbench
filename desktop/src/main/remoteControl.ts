/**
 * Remote control: inject pointer events into this machine.
 *
 * WHY A SEPARATE MODULE
 * ---------------------
 * Input injection needs a native module. Making it a hard dependency would put
 * a compiled binary into every desktop build (and break any environment that
 * cannot build it), for a feature most sessions never use. So it is optional:
 * the module is loaded on first use, and when it is missing the IPC answers
 * `false` — the UI then says "this client cannot inject input" instead of
 * silently swallowing clicks.
 *
 * To enable it: `cd desktop && npm i @nut-tree/nut-js`.
 *
 * SCOPE
 * -----
 * Pointer move / button / wheel only. Typing is deliberately not injected: it
 * is the highest-risk part (a stray keystroke can run anything on the host) and
 * it needs keyboard layout handling that this module has no business guessing.
 */
import { ipcMain, screen } from 'electron'

type NutModule = {
  mouse: {
    setPosition(p: { x: number; y: number }): Promise<void>
    pressButton(b: unknown): Promise<void>
    releaseButton(b: unknown): Promise<void>
    scrollUp(n: number): Promise<void>
    scrollDown(n: number): Promise<void>
  }
  Button: { LEFT: unknown }
}

export interface RemoteControlEvent {
  kind: 'move' | 'down' | 'up' | 'wheel' | 'key'
  x?: number
  y?: number
  button?: number
  key?: string
  delta?: number
}

let nut: NutModule | null | undefined
let loadError = ''

/**
 * Loads the optional module once.
 *
 * `eval('require')` keeps TypeScript from resolving a package that may not be
 * installed — a static import would fail the build for everyone who does not
 * want the feature.
 */
function loadNut(): NutModule | null {
  if (nut !== undefined) return nut
  try {
    const req = eval('require') as NodeRequire
    nut = req('@nut-tree/nut-js') as NutModule
    return nut
  } catch (e) {
    loadError = String(e)
    nut = null
    return null
  }
}

/** Whether input injection is available on this install. */
export function remoteControlSupported(): boolean {
  return loadNut() !== null
}

/**
 * Applies one event.
 *
 * Coordinates arrive normalised to the shared picture (0..1) and are mapped
 * onto the primary display. A multi-monitor capture of a secondary screen would
 * need that screen's bounds, which the capture path does not report yet — a
 * known limitation, not a silent one: the pointer lands on the primary screen.
 */
export async function applyRemoteControlEvent(ev: RemoteControlEvent): Promise<boolean> {
  const mod = loadNut()
  if (!mod) {
    if (loadError) {
      // Logged once per process by the caller; keep this cheap.
      loadError = ''
    }
    return false
  }
  const { width, height } = screen.getPrimaryDisplay().bounds
  const px = Math.round((ev.x ?? 0) * width)
  const py = Math.round((ev.y ?? 0) * height)

  try {
    switch (ev.kind) {
      case 'move':
        await mod.mouse.setPosition({ x: px, y: py })
        return true
      case 'down':
        await mod.mouse.setPosition({ x: px, y: py })
        await mod.mouse.pressButton(mod.Button.LEFT)
        return true
      case 'up':
        await mod.mouse.releaseButton(mod.Button.LEFT)
        return true
      case 'wheel':
        if ((ev.delta ?? 0) > 0) await mod.mouse.scrollDown(1)
        else await mod.mouse.scrollUp(1)
        return true
      case 'key':
        // Deliberately unsupported; see the header.
        return false
      default:
        return false
    }
  } catch {
    return false
  }
}


/**
 * Registers the IPC the renderer calls while it holds remote control.
 *
 * The handler is always registered; `supported` in the answer tells the UI
 * whether anything actually happened, so a missing module surfaces as a clear
 * "not available" instead of a dead button.
 */
export function registerRemoteControlHandlers(): void {
  ipcMain.handle('native:remote-control', async (_e, ev: RemoteControlEvent) => {
    if (!remoteControlSupported()) return { ok: false, supported: false }
    const ok = await applyRemoteControlEvent(ev)
    return { ok, supported: true }
  })
  ipcMain.handle('native:remote-control-supported', () => remoteControlSupported())
}
