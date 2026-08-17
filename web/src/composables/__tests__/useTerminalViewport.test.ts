import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { ref, nextTick } from 'vue'
import { useTerminalViewport } from '@/composables/useTerminalViewport'

// Shared refs for the mocked module — exposed so tests can assert on the
// module-level singleton state that App.vue reads.
const mockShared = vi.hoisted(() => {
  // eslint-disable-next-line @typescript-eslint/no-var-requires
  const { ref } = require('vue') as typeof import('vue')
  return { keyboardHeight: ref(0), isAdjustResize: ref(false) }
})

vi.mock('@/composables/useTerminalKeyboard', () => {
  const fullScreenHeight = { value: 800 }
  return {
    useTerminalKeyboard: () => ({
      keyboardHeight: mockShared.keyboardHeight,
      setKeyboardHeight: (h: number) => { mockShared.keyboardHeight.value = h },
      isAdjustResize: mockShared.isAdjustResize,
      setAdjustResize: (v: boolean) => { mockShared.isAdjustResize.value = v },
      getFullScreenHeight: () => fullScreenHeight.value,
      noteFullScreenHeight: (h: number) => { if (h > fullScreenHeight.value) fullScreenHeight.value = h },
    }),
  }
})

function createMockTerminal() {
  return {
    fitAddon: {
      fit: vi.fn(),
    },
  }
}

describe('useTerminalViewport', () => {
  let container: HTMLElement
  let originalVisualViewport: VisualViewport | null
  let originalInnerHeight: number

  beforeEach(() => {
    container = document.createElement('div')
    document.body.appendChild(container)

    mockShared.isAdjustResize.value = false
    mockShared.keyboardHeight.value = 0

    originalInnerHeight = window.innerHeight
    originalVisualViewport = window.visualViewport
  })

  afterEach(() => {
    document.body.removeChild(container)
    vi.restoreAllMocks()

    Object.defineProperty(window, 'innerHeight', {
      value: originalInnerHeight,
      writable: true,
      configurable: true,
    })
    if (originalVisualViewport) {
      Object.defineProperty(window, 'visualViewport', {
        value: originalVisualViewport,
        writable: true,
        configurable: true,
      })
    }
  })

  it('initializes with zero viewport and keyboard heights', () => {
    const terminal = ref(null)
    const containerRef = ref<HTMLElement | null>(container)
    const viewport = useTerminalViewport(terminal, containerRef)

    expect(viewport.viewportHeight.value).toBe(0)
    expect(viewport.keyboardHeight.value).toBe(0)
  })

  it('calculates viewport height and keyboard height from visualViewport when available', () => {
    const terminal = ref(null)
    const containerRef = ref<HTMLElement | null>(container)
    const viewport = useTerminalViewport(terminal, containerRef)

    Object.defineProperty(window, 'visualViewport', {
      value: {
        height: 600,
        offsetTop: 0,
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
      },
      writable: true,
      configurable: true,
    })
    Object.defineProperty(window, 'innerHeight', {
      value: 800,
      writable: true,
      configurable: true,
    })

    viewport.startWatching()

    expect(viewport.viewportHeight.value).toBe(600)
    // keyboardHeight = innerHeight - vv.height - offsetTop = 800 - 600 - 0 = 200
    expect(viewport.keyboardHeight.value).toBe(200)
    // And it propagates to the shared singleton App.vue reads.
    expect(mockShared.keyboardHeight.value).toBe(200)

    viewport.stopWatching()
  })

  it('detects keyboard via the container ResizeObserver (Android adjustResize)', async () => {
    let resizeCb: ResizeObserverCallback | null = null
    const mockObserve = vi.fn((el: Element) => { /* store nothing */ })
    const mockRO = class {
      static cb: ResizeObserverCallback | null = null
      constructor(cb: ResizeObserverCallback) { mockRO.cb = cb }
      observe(el: Element) { mockObserve(el) }
      unobserve() {}
      disconnect() {}
    }
    // @ts-expect-error override global
    globalThis.ResizeObserver = mockRO as unknown as typeof ResizeObserver

    const terminal = ref(null)
    const containerRef = ref<HTMLElement | null>(container)
    const viewport = useTerminalViewport(terminal, containerRef)

    Object.defineProperty(window, 'visualViewport', {
      value: {
        height: 800,
        offsetTop: 0,
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
      },
      writable: true,
      configurable: true,
    })
    Object.defineProperty(window, 'innerHeight', {
      value: 800,
      writable: true,
      configurable: true,
    })

    viewport.startWatching()
    expect(mockShared.keyboardHeight.value).toBe(0)

    // Keyboard opens → innerHeight shrinks → container resizes → ResizeObserver fires.
    Object.defineProperty(window, 'innerHeight', {
      value: 500,
      writable: true,
      configurable: true,
    })
    mockRO.cb?.([], mockRO as unknown as ResizeObserver)
    await nextTick()

    // resizeKeyboard = fullScreenHeight(800) - innerHeight(500) = 300
    expect(mockShared.keyboardHeight.value).toBe(300)
    expect(mockShared.isAdjustResize.value).toBe(true)

    viewport.stopWatching()
  })

  it('clears the shared keyboard height when the active container becomes null', () => {
    let resizeHandler: (() => void) | null = null
    const terminal = ref(null)
    const containerRef = ref<HTMLElement | null>(container)
    const viewport = useTerminalViewport(terminal, containerRef)

    Object.defineProperty(window, 'visualViewport', {
      value: {
        height: 600,
        offsetTop: 0,
        addEventListener: (_type: string, cb: () => void) => { resizeHandler = cb },
        removeEventListener: vi.fn(),
      },
      writable: true,
      configurable: true,
    })
    Object.defineProperty(window, 'innerHeight', {
      value: 800,
      writable: true,
      configurable: true,
    })

    viewport.startWatching()
    expect(mockShared.keyboardHeight.value).toBe(200)

    // Closing the last terminal session tab detaches the active container; the
    // following resize must clear the stale shared height so the Dock is not
    // left hidden forever.
    containerRef.value = null
    resizeHandler!()
    expect(mockShared.keyboardHeight.value).toBe(0)
  })

  it('detects keyboard via polling when no resize event fires (Android WebView)', () => {
    vi.useFakeTimers()
    const terminal = ref(null)
    const containerRef = ref<HTMLElement | null>(container)
    const viewport = useTerminalViewport(terminal, containerRef)

    // visualViewport present but its resize listener is a no-op (WebView quirk:
    // no event dispatched even though height changes). window.resize also not fired.
    Object.defineProperty(window, 'visualViewport', {
      value: {
        height: 800,
        offsetTop: 0,
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
      },
      writable: true,
      configurable: true,
    })
    Object.defineProperty(window, 'innerHeight', {
      value: 800,
      writable: true,
      configurable: true,
    })

    viewport.startWatching()
    expect(mockShared.keyboardHeight.value).toBe(0)

    // Keyboard opens on Android adjustResize: innerHeight + visualViewport both
    // shrink, but no event fires. The 300ms poll must detect it.
    Object.defineProperty(window, 'innerHeight', {
      value: 500,
      writable: true,
      configurable: true,
    })
    Object.defineProperty(window, 'visualViewport', {
      value: {
        height: 500,
        offsetTop: 0,
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
      },
      writable: true,
      configurable: true,
    })

    vi.advanceTimersByTime(300)
    // resizeKeyboard = fullScreenHeight(800) - innerHeight(500) = 300
    expect(mockShared.keyboardHeight.value).toBe(300)
    expect(mockShared.isAdjustResize.value).toBe(true)

    // Keyboard closes silently: both restore to 800.
    Object.defineProperty(window, 'innerHeight', {
      value: 800,
      writable: true,
      configurable: true,
    })
    Object.defineProperty(window, 'visualViewport', {
      value: {
        height: 800,
        offsetTop: 0,
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
      },
      writable: true,
      configurable: true,
    })
    vi.advanceTimersByTime(300)
    expect(mockShared.keyboardHeight.value).toBe(0)

    viewport.stopWatching()
    vi.useRealTimers()
  })

  it('detects keyboard from Android adjustResize (innerHeight shrinks)', () => {
    const terminal = ref(null)
    const containerRef = ref<HTMLElement | null>(container)
    const viewport = useTerminalViewport(terminal, containerRef)

    Object.defineProperty(window, 'visualViewport', {
      value: {
        height: 500,
        offsetTop: 0,
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
      },
      writable: true,
      configurable: true,
    })
    Object.defineProperty(window, 'innerHeight', {
      value: 500, // shrunk due to adjustResize
      writable: true,
      configurable: true,
    })

    viewport.startWatching()

    // keyboardHeight = max(vvKeyboard, resizeKeyboard, 0)
    // vvKeyboard = 500 - 500 - 0 = 0; resizeKeyboard = 800 - 500 = 300
    expect(viewport.keyboardHeight.value).toBe(300)

    viewport.stopWatching()
  })

  it('uses container clientHeight when visualViewport is not available', () => {
    const terminal = ref(null)
    const containerRef = ref<HTMLElement | null>(container)
    const viewport = useTerminalViewport(terminal, containerRef)

    Object.defineProperty(window, 'visualViewport', {
      value: undefined,
      writable: true,
      configurable: true,
    })

    Object.defineProperty(container, 'clientHeight', {
      value: 450,
      writable: true,
      configurable: true,
    })

    viewport.startWatching()

    expect(viewport.viewportHeight.value).toBe(450)
    expect(viewport.keyboardHeight.value).toBe(0)

    viewport.stopWatching()
  })

  it('does nothing on updateViewport when containerRef is null', () => {
    const terminal = ref(null)
    const containerRef = ref<HTMLElement | null>(null)
    const viewport = useTerminalViewport(terminal, containerRef)

    viewport.startWatching()

    expect(viewport.viewportHeight.value).toBe(0)
    expect(viewport.keyboardHeight.value).toBe(0)

    viewport.stopWatching()
  })

  it('fitTerminal calls fitAddon.fit() when terminal is available', () => {
    const mockTerminal = createMockTerminal()
    const terminal = ref(mockTerminal)
    const containerRef = ref<HTMLElement | null>(container)
    const viewport = useTerminalViewport(terminal, containerRef)

    viewport.fitTerminal()

    expect(mockTerminal.fitAddon.fit).toHaveBeenCalled()
  })

  it('fitTerminal does not throw when terminal is null', () => {
    const terminal = ref(null)
    const containerRef = ref<HTMLElement | null>(container)
    const viewport = useTerminalViewport(terminal, containerRef)

    expect(() => viewport.fitTerminal()).not.toThrow()
  })

  it('fitTerminal catches fit() errors', () => {
    const mockTerminal = createMockTerminal()
    mockTerminal.fitAddon.fit.mockImplementation(() => {
      throw new Error('Terminal not visible')
    })
    const terminal = ref(mockTerminal)
    const containerRef = ref<HTMLElement | null>(container)
    const viewport = useTerminalViewport(terminal, containerRef)

    expect(() => viewport.fitTerminal()).not.toThrow()
  })

  it('uses the larger of vvKeyboard and resizeKeyboard for keyboard height', () => {
    const terminal = ref(null)
    const containerRef = ref<HTMLElement | null>(container)
    const viewport = useTerminalViewport(terminal, containerRef)

    Object.defineProperty(window, 'visualViewport', {
      value: {
        height: 700,
        offsetTop: 0,
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
      },
      writable: true,
      configurable: true,
    })
    Object.defineProperty(window, 'innerHeight', {
      value: 600, // shrunk more than vv suggests
      writable: true,
      configurable: true,
    })

    viewport.startWatching()

    // vvKeyboard = 600 - 700 - 0 = -100 → clamped; resizeKeyboard = 800 - 600 = 200
    expect(viewport.keyboardHeight.value).toBe(200)

    viewport.stopWatching()
  })

  it('clamps keyboard height to 0 minimum', () => {
    const terminal = ref(null)
    const containerRef = ref<HTMLElement | null>(container)
    const viewport = useTerminalViewport(terminal, containerRef)

    Object.defineProperty(window, 'visualViewport', {
      value: {
        height: 800,
        offsetTop: 0,
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
      },
      writable: true,
      configurable: true,
    })
    Object.defineProperty(window, 'innerHeight', {
      value: 800,
      writable: true,
      configurable: true,
    })

    viewport.startWatching()

    expect(viewport.keyboardHeight.value).toBe(0)

    viewport.stopWatching()
  })

  it('accounts for visualViewport offsetTop in keyboard height calculation', () => {
    const terminal = ref(null)
    const containerRef = ref<HTMLElement | null>(container)
    const viewport = useTerminalViewport(terminal, containerRef)

    Object.defineProperty(window, 'visualViewport', {
      value: {
        height: 700,
        offsetTop: 50,
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
      },
      writable: true,
      configurable: true,
    })
    Object.defineProperty(window, 'innerHeight', {
      value: 800,
      writable: true,
      configurable: true,
    })

    viewport.startWatching()

    // vvKeyboard = 800 - 700 - 50 = 50
    expect(viewport.keyboardHeight.value).toBe(50)

    viewport.stopWatching()
  })

  it('debounces fit() calls during viewport updates', () => {
    vi.useFakeTimers()
    const mockTerminal = createMockTerminal()
    const terminal = ref(mockTerminal)
    const containerRef = ref<HTMLElement | null>(container)
    const viewport = useTerminalViewport(terminal, containerRef)

    Object.defineProperty(window, 'visualViewport', {
      value: {
        height: 800,
        offsetTop: 0,
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
      },
      writable: true,
      configurable: true,
    })

    viewport.startWatching()

    expect(mockTerminal.fitAddon.fit).not.toHaveBeenCalled()
    vi.advanceTimersByTime(100)
    expect(mockTerminal.fitAddon.fit).toHaveBeenCalledTimes(1)

    viewport.stopWatching()
    vi.useRealTimers()
  })

  it('cancels pending fit debounce on stopWatching', () => {
    vi.useFakeTimers()
    const mockTerminal = createMockTerminal()
    const terminal = ref(mockTerminal)
    const containerRef = ref<HTMLElement | null>(container)
    const viewport = useTerminalViewport(terminal, containerRef)

    Object.defineProperty(window, 'visualViewport', {
      value: {
        height: 800,
        offsetTop: 0,
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
      },
      writable: true,
      configurable: true,
    })

    viewport.startWatching()
    expect(mockTerminal.fitAddon.fit).not.toHaveBeenCalled()

    viewport.stopWatching()

    vi.advanceTimersByTime(200)
    expect(mockTerminal.fitAddon.fit).not.toHaveBeenCalled()

    vi.useRealTimers()
  })

  it('flags adjustResize when innerHeight shrinks, clears on stop', () => {
    const terminal = ref(null)
    const containerRef = ref<HTMLElement | null>(container)
    const viewport = useTerminalViewport(terminal, containerRef)

    Object.defineProperty(window, 'visualViewport', {
      value: {
        height: 500,
        offsetTop: 0,
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
      },
      writable: true,
      configurable: true,
    })
    Object.defineProperty(window, 'innerHeight', {
      value: 500,
      writable: true,
      configurable: true,
    })

    viewport.startWatching()
    expect(mockShared.isAdjustResize.value).toBe(true)

    viewport.stopWatching()
    expect(mockShared.isAdjustResize.value).toBe(false)
  })

  it('does not flag adjustResize when innerHeight stays same (PWA/iOS)', () => {
    const terminal = ref(null)
    const containerRef = ref<HTMLElement | null>(container)
    const viewport = useTerminalViewport(terminal, containerRef)

    Object.defineProperty(window, 'visualViewport', {
      value: {
        height: 550,
        offsetTop: 0,
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
      },
      writable: true,
      configurable: true,
    })
    Object.defineProperty(window, 'innerHeight', {
      value: 800,
      writable: true,
      configurable: true,
    })

    viewport.startWatching()
    expect(mockShared.isAdjustResize.value).toBe(false)
    // keyboardHeight from visualViewport: 800 - 550 - 0 = 250
    expect(viewport.keyboardHeight.value).toBe(250)

    viewport.stopWatching()
  })
})