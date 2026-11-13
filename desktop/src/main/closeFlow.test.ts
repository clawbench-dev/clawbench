import { describe, it, expect, vi, beforeEach } from 'vitest'
import type { BrowserWindow } from 'electron'

/**
 * The close flow's side effects.
 *
 * `closePolicy` covers the decision table; this file covers what the flow then
 * does with it — hide, quit, or nothing — and the guards around it: no second
 * dialog while one is up, no re-entry once a quit is under way, and no hiding
 * the window when there is no tray icon to get it back.
 */

const store = vi.hoisted(() => ({
  value: {} as Record<string, unknown>,
  sets: [] as [string, unknown][],
}))
const appQuit = vi.hoisted(() => vi.fn())
const showMessageBox = vi.hoisted(() => vi.fn())
const ensureAppTray = vi.hoisted(() => vi.fn())
const hasAppTray = vi.hoisted(() => ({ value: true }))
const syncTrayForCloseAction = vi.hoisted(() => vi.fn())
const notifyCloseActionChanged = vi.hoisted(() => vi.fn())

vi.mock('electron', () => ({
  app: {
    on: () => undefined,
    quit: appQuit,
    getLocale: () => 'en-US',
  },
  dialog: { showMessageBox },
}))

vi.mock('./store', () => ({
  getStore: () => ({
    get: (key: string) => store.value[key],
    set: (key: string, value: unknown) => {
      store.value[key] = value
      store.sets.push([key, value])
    },
  }),
}))

// The tray hand-off is a lazy import precisely so this module stays testable
// without Electron's Tray. A dynamic import resolves to this mock too.
vi.mock('./floating', () => ({
  ensureAppTray,
  hasAppTray: () => hasAppTray.value,
  syncTrayForCloseAction,
  notifyCloseActionChanged,
}))

import {
  cancelPendingQuit,
  getCloseAction,
  handleCloseRequest,
  isAppQuitting,
  noteOsShutdown,
  setCloseAction,
} from './closeFlow'

function fakeWindow() {
  return {
    isDestroyed: () => false,
    hide: vi.fn(),
  } as unknown as BrowserWindow & { hide: ReturnType<typeof vi.fn> }
}

beforeEach(() => {
  store.value = {}
  store.sets = []
  appQuit.mockClear()
  showMessageBox.mockReset()
  ensureAppTray.mockClear()
  hasAppTray.value = true
  syncTrayForCloseAction.mockClear()
  notifyCloseActionChanged.mockClear()
  // `quitting` is module state: a previous "exit" case would otherwise make
  // every later close a no-op.
  cancelPendingQuit()
})

describe('getCloseAction / setCloseAction', () => {
  it('defaults to asking and validates what it reads', () => {
    expect(getCloseAction()).toBe('ask')
    store.value.closeAction = 'nonsense'
    expect(getCloseAction()).toBe('ask')
  })

  it('stores the normalized value', () => {
    expect(setCloseAction('tray')).toBe('tray')
    expect(store.value.closeAction).toBe('tray')
  })

  it('only tells the shell when the value actually changed', () => {
    // The settings row pushes on every load; an unconditional notification would
    // bounce the value back through the renderer forever.
    store.value.closeAction = 'tray'
    setCloseAction('tray')
    expect(notifyCloseActionChanged).not.toHaveBeenCalled()
  })
})

describe('a remembered action is carried out without asking', () => {
  it('"minimize to tray" hides the window and keeps the app alive', async () => {
    store.value.closeAction = 'tray'
    const win = fakeWindow()
    await handleCloseRequest(win)

    expect(ensureAppTray).toHaveBeenCalledTimes(1)
    expect(win.hide).toHaveBeenCalledTimes(1)
    expect(appQuit).not.toHaveBeenCalled()
    expect(showMessageBox).not.toHaveBeenCalled()
  })

  it('refuses to hide the window when no tray icon could be created', async () => {
    // Hiding it would leave the app running with nothing on screen and nothing
    // to click — it would look closed and be unreachable.
    store.value.closeAction = 'tray'
    hasAppTray.value = false
    const win = fakeWindow()
    await handleCloseRequest(win)

    expect(win.hide).not.toHaveBeenCalled()
    expect(showMessageBox).toHaveBeenCalledTimes(1)
    expect(appQuit).not.toHaveBeenCalled()
  })

  it('lets an OS shutdown through without asking', async () => {
    // Windows ends a session by closing every window; a dialog there would block
    // a machine that is already going down.
    noteOsShutdown()
    const win = fakeWindow()
    await handleCloseRequest(win)

    expect(showMessageBox).not.toHaveBeenCalled()
    expect(win.hide).not.toHaveBeenCalled()
    expect(appQuit).not.toHaveBeenCalled()
  })

  it('"exit" quits and stops intercepting further closes', async () => {
    store.value.closeAction = 'exit'
    const win = fakeWindow()
    await handleCloseRequest(win)

    expect(appQuit).toHaveBeenCalledTimes(1)
    expect(isAppQuitting()).toBe(true)

    // The quit re-enters the window's close handler; the flow must step aside.
    appQuit.mockClear()
    await handleCloseRequest(fakeWindow())
    expect(appQuit).not.toHaveBeenCalled()
  })
})

describe('the dialog', () => {
  it('hides to the tray on "minimize" without remembering', async () => {
    showMessageBox.mockResolvedValue({ response: 0, checkboxChecked: false })
    const win = fakeWindow()
    await handleCloseRequest(win)

    expect(win.hide).toHaveBeenCalledTimes(1)
    expect(store.value.closeAction).toBeUndefined()
    expect(appQuit).not.toHaveBeenCalled()
  })

  it('quits on "exit" without remembering', async () => {
    showMessageBox.mockResolvedValue({ response: 1, checkboxChecked: false })
    await handleCloseRequest(fakeWindow())

    expect(appQuit).toHaveBeenCalledTimes(1)
    expect(store.value.closeAction).toBeUndefined()
  })

  it('does nothing on cancel', async () => {
    showMessageBox.mockResolvedValue({ response: 2, checkboxChecked: false })
    const win = fakeWindow()
    await handleCloseRequest(win)

    expect(win.hide).not.toHaveBeenCalled()
    expect(appQuit).not.toHaveBeenCalled()
    expect(store.sets).toEqual([])
  })

  it('remembers the picked action and tells the settings row', async () => {
    showMessageBox.mockResolvedValue({ response: 0, checkboxChecked: true })
    await handleCloseRequest(fakeWindow())

    expect(store.value.closeAction).toBe('tray')
    await vi.waitFor(() => expect(notifyCloseActionChanged).toHaveBeenCalledWith('tray'))
    // The tray has to exist for the choice to be usable next time.
    await vi.waitFor(() => expect(syncTrayForCloseAction).toHaveBeenCalled())
  })

  it('does not stack a second dialog while one is up', async () => {
    // Alt+F4 twice: the dialog is modal to the window, not to the process.
    let resolveBox: (value: { response: number; checkboxChecked: boolean }) => void = () => undefined
    showMessageBox.mockImplementation(() => new Promise((resolve) => { resolveBox = resolve }))

    const win = fakeWindow()
    const first = handleCloseRequest(win)
    const second = handleCloseRequest(win)
    expect(showMessageBox).toHaveBeenCalledTimes(1)

    resolveBox({ response: 2, checkboxChecked: false })
    await Promise.all([first, second])
  })
})
