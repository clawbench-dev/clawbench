import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { copyDocumentStyles, useScreenWindow } from '../useScreenWindow'

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
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
  it('reports unsupported instead of throwing when the API is absent', async () => {
    const { target, open } = useScreenWindow()
    expect(await open()).toBe('unsupported')
    expect(target.value).toBeNull()
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
