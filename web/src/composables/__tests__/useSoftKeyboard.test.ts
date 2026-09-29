import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'

/**
 * useSoftKeyboard holds module-level state (a singleton ref + listener/timer
 * handles), so each test re-imports the module with vi.resetModules() to get a
 * clean instance — same pattern as useTerminalKeyboard.test.ts.
 */

type SoftKeyboard = typeof import('@/composables/useSoftKeyboard')['useSoftKeyboard']

let originalInnerHeight: number
let originalInnerWidth: number
let originalVisualViewport: VisualViewport | undefined

// A single mutable visualViewport stub, shared across a test so the listeners
// the composable registered on it stay attached when we change the height.
let vv: (EventTarget & { width: number; height: number; offsetTop: number }) | null = null

function makeVisualViewport(width: number, height: number) {
  const target = new EventTarget() as EventTarget & { width: number; height: number; offsetTop: number }
  target.width = width
  target.height = height
  target.offsetTop = 0
  Object.defineProperty(window, 'visualViewport', { value: target, writable: true, configurable: true })
  return target
}

/** Resize the visual viewport in place and notify listeners (as a browser does). */
function resizeVisualViewport(width: number, height: number) {
  if (!vv) throw new Error('visualViewport stub not installed')
  vv.width = width
  vv.height = height
  vv.dispatchEvent(new Event('resize'))
}

function setInnerHeight(h: number) {
  Object.defineProperty(window, 'innerHeight', { value: h, writable: true, configurable: true })
}

function setInnerWidth(w: number) {
  Object.defineProperty(window, 'innerWidth', { value: w, writable: true, configurable: true })
}

/** Focus a freshly created <input> and notify the detector via a bubbling focusin. */
function focusInput(): HTMLInputElement {
  const input = document.createElement('input')
  document.body.appendChild(input)
  input.focus()
  input.dispatchEvent(new FocusEvent('focusin', { bubbles: true }))
  return input
}

async function fresh(): Promise<ReturnType<SoftKeyboard>> {
  vi.resetModules()
  const { useSoftKeyboard } = await import('@/composables/useSoftKeyboard')
  return useSoftKeyboard()
}

describe('useSoftKeyboard', () => {
  beforeEach(() => {
    originalInnerHeight = window.innerHeight
    originalInnerWidth = window.innerWidth
    originalVisualViewport = window.visualViewport
    setInnerHeight(800)
    setInnerWidth(400)
    vv = makeVisualViewport(400, 800)
  })

  afterEach(() => {
    document.body.innerHTML = ''
    setInnerHeight(originalInnerHeight)
    setInnerWidth(originalInnerWidth)
    Object.defineProperty(window, 'visualViewport', {
      value: originalVisualViewport,
      writable: true,
      configurable: true,
    })
    vv = null
    vi.restoreAllMocks()
    vi.useRealTimers()
  })

  it('starts closed and exposes the same singleton to every caller', async () => {
    const a = await fresh()
    const b = (await import('@/composables/useSoftKeyboard')).useSoftKeyboard()
    expect(a.isSoftKeyboardOpen.value).toBe(false)
    expect(a.isSoftKeyboardOpen).toBe(b.isSoftKeyboardOpen)
  })

  it('reports open on Android adjustResize, where innerHeight shrinks', async () => {
    const { isSoftKeyboardOpen, install, uninstall } = await fresh()
    install()
    focusInput()
    // Focus alone is not a keyboard — the viewport must actually shrink.
    expect(isSoftKeyboardOpen.value).toBe(false)

    setInnerHeight(500)
    resizeVisualViewport(400, 500)
    expect(isSoftKeyboardOpen.value).toBe(true)

    uninstall()
  })

  it('reports open on a mobile browser, where only the visual viewport shrinks', async () => {
    const { isSoftKeyboardOpen, install, uninstall } = await fresh()
    install()
    focusInput()

    // innerHeight is untouched (no adjustResize); only vv.height drops.
    resizeVisualViewport(400, 500)
    expect(isSoftKeyboardOpen.value).toBe(true)

    uninstall()
  })

  it('ignores viewport shrink while no editable has focus', async () => {
    // URL-bar show/hide, page scroll and rotation all change the viewport
    // without a keyboard — the dock must not blink out for those.
    const { isSoftKeyboardOpen, install, uninstall } = await fresh()
    install()
    expect(document.activeElement).toBe(document.body)

    setInnerHeight(500)
    resizeVisualViewport(400, 500)
    expect(isSoftKeyboardOpen.value).toBe(false)

    uninstall()
  })

  it('does not treat browser chrome as a keyboard', async () => {
    const { isSoftKeyboardOpen, install, uninstall } = await fresh()
    install()
    focusInput()

    // 60px of shrink is chrome, below the 120px threshold.
    setInnerHeight(740)
    resizeVisualViewport(400, 740)
    expect(isSoftKeyboardOpen.value).toBe(false)

    uninstall()
  })

  it('closes after focus leaves the editable', async () => {
    vi.useFakeTimers()
    const { isSoftKeyboardOpen, install, uninstall } = await fresh()
    install()
    const input = focusInput()
    setInnerHeight(500)
    resizeVisualViewport(400, 500)
    expect(isSoftKeyboardOpen.value).toBe(true)

    // Blur, then let the grace window elapse. document.activeElement must be
    // the body for the re-check to conclude the keyboard closed.
    input.blur()
    document.dispatchEvent(new FocusEvent('focusout', { bubbles: true }))
    setInnerHeight(800)
    resizeVisualViewport(400, 800)
    vi.advanceTimersByTime(200)
    expect(isSoftKeyboardOpen.value).toBe(false)

    uninstall()
  })

  it('stays open when focus moves straight into another editable', async () => {
    vi.useFakeTimers()
    const { isSoftKeyboardOpen, install, uninstall } = await fresh()
    install()
    const first = focusInput()
    setInnerHeight(500)
    resizeVisualViewport(400, 500)
    expect(isSoftKeyboardOpen.value).toBe(true)

    // focusout fires, then the next field is focused before the grace elapses.
    first.blur()
    document.dispatchEvent(new FocusEvent('focusout', { bubbles: true }))
    const second = document.createElement('textarea')
    document.body.appendChild(second)
    second.focus()
    second.dispatchEvent(new FocusEvent('focusin', { bubbles: true }))
    vi.advanceTimersByTime(200)
    expect(isSoftKeyboardOpen.value).toBe(true)

    uninstall()
  })

  it('uninstall clears the flag and stops observing', async () => {
    const { isSoftKeyboardOpen, install, uninstall } = await fresh()
    install()
    focusInput()
    setInnerHeight(500)
    resizeVisualViewport(400, 500)
    expect(isSoftKeyboardOpen.value).toBe(true)

    uninstall()
    expect(isSoftKeyboardOpen.value).toBe(false)

    // A later shrink must not flip it back — listeners are gone.
    setInnerHeight(400)
    resizeVisualViewport(400, 400)
    expect(isSoftKeyboardOpen.value).toBe(false)
  })

  it('is idempotent on repeated install', async () => {
    const { isSoftKeyboardOpen, install, uninstall } = await fresh()
    install()
    install()
    focusInput()
    setInnerHeight(500)
    resizeVisualViewport(400, 500)
    expect(isSoftKeyboardOpen.value).toBe(true)
    uninstall()
    // One uninstall must fully tear down (no leaked second registration).
    setInnerHeight(300)
    resizeVisualViewport(400, 300)
    expect(isSoftKeyboardOpen.value).toBe(false)
  })
})
