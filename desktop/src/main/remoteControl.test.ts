import { describe, expect, it, vi, beforeEach } from 'vitest'
import { registerRemoteControlHandlers, _grantedViewerForTest, _grantBoundsForTest } from './remoteControl'

/**
 * Who may arm and drive remote control, and which part of the desktop the
 * viewer's coordinates refer to.
 *
 * The capability is granted by the publishing panel in the main window. The
 * share window is a different renderer with the SAME preload and the SAME
 * origin, so an origin check would let it grant itself control — the check has
 * to be on the webContents identity.
 */
const handlers = vi.hoisted(() => new Map<string, (e: unknown, ...args: unknown[]) => unknown>())
const mainWebContents = vi.hoisted(() => ({ kind: 'main' }))
const foreignWebContents = vi.hoisted(() => ({ kind: 'share-window' }))
const getMainWindow = vi.hoisted(() => vi.fn(() => ({ webContents: mainWebContents })))
/** What capture.ts reports as the display being shared, and the screens it maps to. */
const capture = vi.hoisted(() => ({
  displayId: '',
  displays: [] as Array<{ id: number; bounds: { x: number; y: number; width: number; height: number }; scaleFactor: number }>,
}))

vi.mock('electron', () => ({
  app: { isPackaged: false, getAppPath: () => '/tmp/clawbench-test' },
  screen: { getAllDisplays: () => capture.displays },
  ipcMain: {
    handle: (channel: string, fn: (e: unknown, ...args: unknown[]) => unknown) => {
      handlers.set(channel, fn)
    },
  },
}))
vi.mock('./window', () => ({ getMainWindow }))
vi.mock('./capture', () => ({ currentCaptureDisplayId: () => capture.displayId }))

registerRemoteControlHandlers({
  handle: (channel: string, fn: (e: unknown, ...args: unknown[]) => unknown) => {
    handlers.set(channel, fn)
  },
} as unknown as Electron.IpcMain)

const grant = (sender: unknown, viewerId: string) =>
  handlers.get('native:remote-control-grant')!({ sender }, viewerId)
const revoke = () => handlers.get('native:remote-control-revoke')!({ sender: foreignWebContents })
const event = (sender: unknown) =>
  handlers.get('native:remote-control')!({ sender }, { kind: 'move', x: 0.5, y: 0.5 })

beforeEach(() => {
  revoke()
  capture.displayId = ''
  capture.displays = []
})

describe('remote control IPC', () => {
  it('lets the main window grant control', () => {
    grant(mainWebContents, 'viewer-1')
    expect(_grantedViewerForTest()).toBe('viewer-1')
  })

  it('ignores a grant from any other renderer', () => {
    // A share window (or anything else with the preload) must not be able to
    // authorize itself: it would then drive this machine's mouse and keyboard.
    grant(foreignWebContents, 'viewer-1')
    expect(_grantedViewerForTest()).toBe('')
  })

  it('drops events that do not come from the main window', async () => {
    grant(mainWebContents, 'viewer-1')
    await expect(event(foreignWebContents)).resolves.toMatchObject({ ok: false, reason: 'untrusted' })
  })

  it('does not reject the main window as untrusted', async () => {
    // Without a grant the event is still refused — but for the capability
    // reason, not the sender one. (Which reason depends on the platform: the
    // injector only exists on Windows.)
    const res = (await event(mainWebContents)) as { ok: boolean; reason?: string }
    expect(res.ok).toBe(false)
    expect(res.reason).not.toBe('untrusted')
  })

  it('allows revoking from any renderer', () => {
    grant(mainWebContents, 'viewer-1')
    revoke()
    expect(_grantedViewerForTest()).toBe('')
  })
})

describe('captured display bounds', () => {
  it('maps the shared display, in physical pixels, at grant time', () => {
    // Screen 2 at 200%: DIP 1920..3840 becomes physical 3840..7680.
    capture.displayId = '42'
    capture.displays = [
      { id: 7, bounds: { x: 0, y: 0, width: 1920, height: 1080 }, scaleFactor: 1 },
      { id: 42, bounds: { x: 1920, y: 0, width: 1920, height: 1080 }, scaleFactor: 2 },
    ]

    grant(mainWebContents, 'viewer-1')

    expect(_grantBoundsForTest()).toEqual({ left: 3840, top: 0, width: 3840, height: 2160 })
  })

  it('leaves bounds unset when the capture does not name its display', () => {
    // Wayland hands out a nameless placeholder; the injector then falls back to
    // mapping over the whole virtual desktop instead of guessing a monitor.
    capture.displayId = ''
    grant(mainWebContents, 'viewer-1')
    expect(_grantBoundsForTest()).toBeNull()
  })

  it('leaves bounds unset when the reported id matches no display', () => {
    capture.displayId = '99'
    capture.displays = [{ id: 7, bounds: { x: 0, y: 0, width: 1920, height: 1080 }, scaleFactor: 1 }]
    grant(mainWebContents, 'viewer-1')
    expect(_grantBoundsForTest()).toBeNull()
  })

  it('clears the rectangle when control is revoked', () => {
    capture.displayId = '7'
    capture.displays = [{ id: 7, bounds: { x: 0, y: 0, width: 1920, height: 1080 }, scaleFactor: 1 }]
    grant(mainWebContents, 'viewer-1')
    expect(_grantBoundsForTest()).not.toBeNull()

    revoke()
    expect(_grantBoundsForTest()).toBeNull()
  })
})
