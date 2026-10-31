import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

/**
 * Annotation overlay (desktop shell).
 *
 * The invariant that matters is the platform split: the audience must never see
 * a stroke twice. On Windows/macOS the overlay is excluded from the capture, so
 * strokes have to travel as control frames; on Linux it is part of the picture,
 * so forwarding them would double every line — they must be dropped there.
 *
 * Everything else (window flags, bounds, lifecycle) is asserted because a wrong
 * flag makes the overlay either invisible or unusable, and neither shows up in
 * a type check.
 */

interface FakeWin {
  options: Record<string, unknown>
  setMenuBarVisibility: ReturnType<typeof vi.fn>
  setAlwaysOnTop: ReturnType<typeof vi.fn>
  setContentProtection: ReturnType<typeof vi.fn>
  setVisibleOnAllWorkspaces: ReturnType<typeof vi.fn>
  loadFile: ReturnType<typeof vi.fn>
  show: ReturnType<typeof vi.fn>
  focus: ReturnType<typeof vi.fn>
  close: ReturnType<typeof vi.fn>
  isDestroyed: () => boolean
  once: (ev: string, cb: () => void) => void
  on: (ev: string, cb: () => void) => void
  fire: (ev: string) => void
}

const state = vi.hoisted(() => ({
  windows: [] as FakeWin[],
  displays: [] as Array<{ id: number; bounds: { x: number; y: number; width: number; height: number } }>,
  captureDisplayId: '',
  handlers: {} as Record<string, (...a: unknown[]) => unknown>,
  events: {} as Record<string, (...a: unknown[]) => void>,
  sent: [] as Array<{ channel: string; payload: unknown }>,
  loadFileRejects: false,
}))

vi.mock('electron', () => ({
  BrowserWindow: class {
    options: Record<string, unknown>
    private listeners: Record<string, Array<() => void>> = {}
    setMenuBarVisibility = vi.fn()
    setAlwaysOnTop = vi.fn()
    setContentProtection = vi.fn()
    setVisibleOnAllWorkspaces = vi.fn()
    loadFile = vi.fn(async () => {
      if (state.loadFileRejects) throw new Error('ENOENT: annotate.html')
    })
    show = vi.fn()
    focus = vi.fn()
    close = vi.fn()
    isDestroyed = () => false
    constructor(options: Record<string, unknown>) {
      this.options = options
      state.windows.push(this as unknown as FakeWin)
    }
    once(ev: string, cb: () => void) {
      ;(this.listeners[ev] ??= []).push(cb)
    }
    on(ev: string, cb: () => void) {
      ;(this.listeners[ev] ??= []).push(cb)
    }
    fire(ev: string) {
      for (const cb of this.listeners[ev] ?? []) cb()
    }
  },
  screen: {
    getAllDisplays: () => state.displays,
    getPrimaryDisplay: () => state.displays[0] ?? { id: 1, bounds: { x: 0, y: 0, width: 1920, height: 1080 } },
  },
  ipcMain: {
    handle: (channel: string, fn: (...a: unknown[]) => unknown) => {
      state.handlers[channel] = fn
    },
    on: (channel: string, fn: (...a: unknown[]) => void) => {
      state.events[channel] = fn
    },
  },
}))

vi.mock('./clientLog', () => ({ record: () => undefined }))

vi.mock('./capture', () => ({
  currentCaptureDisplayId: () => state.captureDisplayId,
}))

vi.mock('./window', () => ({
  getMainWindow: () => ({ webContents: { send: (channel: string, payload: unknown) => state.sent.push({ channel, payload }) } }),
}))

/** The real platform, restored after every test. */
const REAL_PLATFORM = process.platform

/**
 * Import a fresh copy of the module with `process.platform` forced.
 *
 * The override stays in place for the whole test: the platform decides whether
 * strokes are forwarded at CALL time, not at import time.
 */
async function load(platform: NodeJS.Platform = 'linux') {
  vi.resetModules()
  Object.defineProperty(process, 'platform', { value: platform, configurable: true })
  return await import('./annotateOverlay')
}

afterEach(() => {
  Object.defineProperty(process, 'platform', { value: REAL_PLATFORM, configurable: true })
})

beforeEach(() => {
  // Electron-only globals the module reads while building the window.
  Object.defineProperty(process, 'resourcesPath', { value: '/res', configurable: true })
  state.windows = []
  state.displays = [
    { id: 1, bounds: { x: 0, y: 0, width: 1920, height: 1080 } },
    { id: 2, bounds: { x: 1920, y: 0, width: 2560, height: 1440 } },
  ]
  state.captureDisplayId = ''
  state.handlers = {}
  state.events = {}
  state.sent = []
  state.loadFileRejects = false
})

describe('annotateHiddenFromCapture', () => {
  it('is true only where a window can be kept out of the capture', async () => {
    const mod = await load()
    expect(mod.annotateHiddenFromCapture('win32')).toBe(true)
    expect(mod.annotateHiddenFromCapture('darwin')).toBe(true)
    expect(mod.annotateHiddenFromCapture('linux')).toBe(false)
  })
})

describe('openAnnotateOverlay', () => {
  it('covers the display being captured, transparent and on top', async () => {
    const mod = await load()
    state.captureDisplayId = '2'
    const res = await mod.openAnnotateOverlay()

    expect(res).toEqual({ opened: true, inkViaControl: false })
    const win = state.windows[0]
    // The second monitor, not the primary: drawing must land on the shared one.
    expect(win.options).toMatchObject({ x: 1920, y: 0, width: 2560, height: 1440 })
    // A transparent window that is not explicitly transparent-background hides
    // the desktop underneath.
    expect(win.options).toMatchObject({ transparent: true, backgroundColor: '#00000000', frame: false })
    expect(win.options).toMatchObject({ alwaysOnTop: true, skipTaskbar: true, hasShadow: false })
    expect(win.options).toMatchObject({ resizable: false, movable: false })
    expect(win.setAlwaysOnTop).toHaveBeenCalledWith(true, 'screen-saver', 1)
    // Linux cannot hide it, so content protection stays off and the ink travels
    // inside the video instead of as control frames.
    expect(win.setContentProtection).toHaveBeenCalledWith(false)
  })

  it('falls back to the primary display when the platform does not name one', async () => {
    const mod = await load()
    state.captureDisplayId = ''
    await mod.openAnnotateOverlay()
    expect(state.windows[0].options).toMatchObject({ x: 0, y: 0, width: 1920, height: 1080 })
  })

  it('hides the overlay from the capture on windows and asks for control frames', async () => {
    const mod = await load('win32')
    const res = await mod.openAnnotateOverlay()
    expect(res).toEqual({ opened: true, inkViaControl: true })
    expect(state.windows[0].setContentProtection).toHaveBeenCalledWith(true)
  })

  it('reuses the open window instead of stacking a second one', async () => {
    const mod = await load()
    await mod.openAnnotateOverlay()
    await mod.openAnnotateOverlay()
    expect(state.windows).toHaveLength(1)
    expect(state.windows[0].focus).toHaveBeenCalled()
  })

  it('reports failure when the document is missing, so the caller can fall back', async () => {
    const mod = await load()
    // A dev run has no resourcesPath copy of the document; leaving the caller
    // with "opened" would turn the button into a no-op.
    state.loadFileRejects = true
    const res = await mod.openAnnotateOverlay()

    expect(res.opened).toBe(false)
    expect(state.windows[0].close).toHaveBeenCalled()
    expect(state.windows[0].show).not.toHaveBeenCalled()
    expect(mod.isAnnotateOverlayOpen()).toBe(false)
  })

  it('tells the renderer when the window goes away', async () => {
    const mod = await load()
    await mod.openAnnotateOverlay()
    state.windows[0].fire('closed')
    expect(state.sent).toEqual([{ channel: 'native:annotate-closed', payload: undefined }])
    expect(mod.isAnnotateOverlayOpen()).toBe(false)
  })
})

describe('ink forwarding', () => {
  it('forwards strokes to the renderer when the overlay is hidden from capture', async () => {
    const mod = await load('win32')
    mod.registerAnnotateIpc()
    state.events['annotate:ink']?.({}, '{"t":"ink","phase":"move","x":0.5,"y":0.5}')
    expect(state.sent).toEqual([
      { channel: 'native:annotate-ink', payload: '{"t":"ink","phase":"move","x":0.5,"y":0.5}' },
    ])
  })

  it('drops strokes on Linux, where the ink is already in the picture', async () => {
    const mod = await load('linux')
    mod.registerAnnotateIpc()
    state.events['annotate:ink']?.({}, '{"t":"ink","phase":"move","x":0.5,"y":0.5}')
    expect(state.sent).toEqual([])
  })

  it('ignores a non-string frame instead of forwarding it', async () => {
    const mod = await load('win32')
    mod.registerAnnotateIpc()
    state.events['annotate:ink']?.({}, { t: 'ink' })
    expect(state.sent).toEqual([])
  })
})
