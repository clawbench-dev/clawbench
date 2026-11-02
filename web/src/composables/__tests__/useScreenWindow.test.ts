import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { copyDocumentStyles, useScreenWindow } from '../useScreenWindow'

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

/** Which host the code under test believes it is running in. */
const host = vi.hoisted(() => ({ desktop: false }))
vi.mock('@/utils/clawbenchNative', () => ({
  isDesktopApp: () => host.desktop,
}))

interface FakeWindow extends Window {
  firePagehide: () => void
  close: ReturnType<typeof vi.fn>
  focus: ReturnType<typeof vi.fn>
}

/** A window-ish object whose document is a real (jsdom) Document. */
function fakeWindow(): FakeWindow {
  const doc = document.implementation.createHTMLDocument('pip')
  const listeners: Array<() => void> = []
  const win = {
    document: doc,
    addEventListener: (type: string, cb: () => void) => {
      if (type === 'pagehide') listeners.push(cb)
    },
    close: vi.fn(),
    focus: vi.fn(),
    firePagehide: () => listeners.forEach((f) => f()),
  }
  return win as unknown as FakeWindow
}

function installApi(requestWindow: ReturnType<typeof vi.fn>) {
  vi.stubGlobal('documentPictureInPicture', { requestWindow })
}

beforeEach(() => {
  host.desktop = false
  // A stylesheet must exist for the copy to have anything to move.
  const style = document.createElement('style')
  style.textContent = '.from-app { color: red }'
  document.head.appendChild(style)
  document.documentElement.setAttribute('data-theme', 'dark')
  document.documentElement.setAttribute('style', '--ui-scale: 1.25')
  document.body.className = 'app-body'
})

afterEach(() => {
  vi.unstubAllGlobals()
  document.querySelectorAll('style').forEach((s) => {
    if (s.textContent === '.from-app { color: red }') s.remove()
  })
  document.documentElement.removeAttribute('data-theme')
  document.documentElement.removeAttribute('style')
  document.body.className = ''
})

describe('useScreenWindow', () => {
  it('falls back to a popup window when the host has no Document PiP', async () => {
    const popup = fakeWindow()
    const openSpy = vi.spyOn(window, 'open').mockReturnValue(popup as unknown as Window)
    const { target, open } = useScreenWindow()

    expect(await open()).toBe('opened')
    expect(openSpy).toHaveBeenCalledTimes(1)
    // Same contract as PiP: the caller Teleports into this body.
    expect(target.value).toBe(popup.document.body)
    expect(popup.document.title).toBe('ClawBench')
    openSpy.mockRestore()
  })

  it('opens a real window in the desktop shell even when Document PiP exists', async () => {
    const popup = fakeWindow()
    const openSpy = vi.spyOn(window, 'open').mockReturnValue(popup as unknown as Window)
    const requestWindow = vi.fn(async () => fakeWindow() as unknown as Window)
    installApi(requestWindow)
    host.desktop = true
    const { target, open } = useScreenWindow()

    // The desktop shell gives the host a window of the app, not a floating PiP
    // pane: that is what "open in a window" means there.
    expect(await open()).toBe('opened')
    expect(openSpy).toHaveBeenCalledTimes(1)
    expect(requestWindow).not.toHaveBeenCalled()
    expect(target.value).toBe(popup.document.body)
    openSpy.mockRestore()
  })

  it('reports failure when neither a popup nor a PiP window can be opened', async () => {
    const openSpy = vi.spyOn(window, 'open').mockReturnValue(null)
    const { target, open } = useScreenWindow()
    expect(await open()).toBe('failed')
    expect(target.value).toBeNull()
    openSpy.mockRestore()
  })

  it('forgets a popup the user closed', async () => {
    vi.useFakeTimers()
    const popup = fakeWindow()
    const openSpy = vi.spyOn(window, 'open').mockReturnValue(popup as unknown as Window)
    const { target, open } = useScreenWindow()
    await open()
    expect(target.value).not.toBeNull()

    // A popup reports its own close only through `closed`; there is no event
    // in the opener.
    Object.defineProperty(popup, 'closed', { value: true, configurable: true })
    vi.advanceTimersByTime(500)
    expect(target.value).toBeNull()
    openSpy.mockRestore()
    vi.useRealTimers()
  })

  it('renders into a new document that carries the app styles and theme', async () => {
    const win = fakeWindow()
    installApi(vi.fn(async () => win))
    const { target, open } = useScreenWindow()

    expect(await open({ width: 800, height: 600 })).toBe('opened')
    expect(target.value).toBe(win.document.body)

    // Theme + UI scale live on <html>, so they have to travel explicitly.
    expect(win.document.documentElement.getAttribute('data-theme')).toBe('dark')
    expect(win.document.documentElement.getAttribute('style')).toContain('--ui-scale: 1.25')
    expect(win.document.body.className).toBe('app-body')
    const copied = Array.from(win.document.querySelectorAll('style')).map((s) => s.textContent)
    expect(copied.some((css) => css?.includes('.from-app'))).toBe(true)
  })

  it('focuses the existing window instead of opening a second one', async () => {
    const win = fakeWindow()
    const requestWindow = vi.fn(async () => win)
    installApi(requestWindow)
    const { open } = useScreenWindow()

    await open()
    expect(await open()).toBe('opened')
    expect(requestWindow).toHaveBeenCalledTimes(1)
    expect(win.focus).toHaveBeenCalled()
  })

  it('clears the target when the user closes the window themselves', async () => {
    const win = fakeWindow()
    installApi(vi.fn(async () => win))
    const { target, open } = useScreenWindow()
    await open()

    win.firePagehide()
    expect(target.value).toBeNull()
  })

  it('closes the window and forgets it', async () => {
    const win = fakeWindow()
    installApi(vi.fn(async () => win))
    const { target, open, close } = useScreenWindow()
    await open()

    close()
    expect(win.close).toHaveBeenCalled()
    expect(target.value).toBeNull()
  })

  it('reopens instead of answering "opened" when the window is gone', async () => {
    const first = fakeWindow()
    const second = fakeWindow()
    const openSpy = vi
      .spyOn(window, 'open')
      .mockReturnValueOnce(first as unknown as Window)
      .mockReturnValueOnce(second as unknown as Window)
    const { target, open, close } = useScreenWindow()

    await open()
    expect(target.value).toBe(first.document.body)

    // The close never reached us: a popup whose poll has not run yet, or one
    // that navigated away from the document we render into. The ref still
    // points at it, and treating that as "already open" is what made the
    // caller's button a silent no-op — the surface was gone for good.
    Object.defineProperty(first, 'closed', { value: true, configurable: true })
    expect(await open()).toBe('opened')
    expect(openSpy).toHaveBeenCalledTimes(2)
    expect(target.value).toBe(second.document.body)

    close()
    openSpy.mockRestore()
  })

  it('gives each surface its own window name', async () => {
    // Two surfaces sharing one name would be handed the SAME window by
    // `window.open`: they would render on top of each other and closing either
    // would take the other one down.
    const openSpy = vi.spyOn(window, 'open').mockReturnValue(fakeWindow() as unknown as Window)
    const first = useScreenWindow('surface-a')
    const second = useScreenWindow('surface-b')

    await first.open()
    await second.open()
    expect(openSpy.mock.calls.map((call) => call[1])).toEqual(['surface-a', 'surface-b'])

    first.close()
    second.close()
    openSpy.mockRestore()
  })

  it('reports failure when the popup is blocked', async () => {
    installApi(vi.fn(async () => Promise.reject(new Error('blocked'))))
    const { target, open } = useScreenWindow()
    expect(await open()).toBe('failed')
    expect(target.value).toBeNull()
  })
})

describe('copyDocumentStyles', () => {
  it('re-links a sheet whose rules cannot be read', () => {
    const to = document.implementation.createHTMLDocument('pip')
    const from = {
      documentElement: { getAttribute: () => null },
      body: { className: '' },
      styleSheets: [{ href: 'https://cdn.example/x.css', cssRules: [] }],
    } as unknown as Document
    // Reading cssRules throws for cross-origin sheets; the helper must fall
    // back to a <link> rather than dropping the styling entirely.
    Object.defineProperty(from.styleSheets, 0, {
      value: {
        href: 'https://cdn.example/x.css',
        get cssRules(): never {
          throw new Error('cross-origin')
        },
      },
    })

    copyDocumentStyles(from, to)
    const link = to.querySelector('link')
    expect(link?.getAttribute('href')).toBe('https://cdn.example/x.css')
  })
})
