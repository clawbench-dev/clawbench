import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { ref, nextTick } from 'vue'
import { useTerminalViewport } from '@/composables/useTerminalViewport'

// Shared refs for the mocked module — exposed so tests can assert on the
// module-level singleton state that App.vue reads (not just the local ref).
const mockShared = vi.hoisted(() => {
  // eslint-disable-next-line @typescript-eslint/no-var-requires
  const { ref } = require('vue') as typeof import('vue')
  return { keyboardHeight: ref(0), isAdjustResize: ref(false) }
})

// Mock useTerminalKeyboard to avoid module-level side effects
vi.mock('@/composables/useTerminalKeyboard', () => {
  return {
    useTerminalKeyboard: () => ({
      keyboardHeight: mockShared.keyboardHeight,
      setKeyboardHeight: (h: number) => { mockShared.keyboardHeight.value = h },
      isAdjustResize: mockShared.isAdjustResize,
      setAdjustResize: (v: boolean) => { mockShared.isAdjustResize.value = v },
      fullScreenHeight: 800,
    }),
  }
})

// Mock platform detection: default to non-PC (mobile) to keep existing
// keyboard-detection tests valid; flip to true to test the PC path.
const mockIsPC = ref(false)
vi.mock('@/composables/usePlatformDetect', () => {
  return {
    usePlatformDetect: () => ({ isPC: mockIsPC }),
  }
})

// Mock ResizeObserver (not available in jsdom by default)
class MockResizeObserver {
  private callback: ResizeObserverCallback
  constructor(callback: ResizeObserverCallback) {
    this.callback = callback
  }
  observe() {}
  unobserve() {}
  disconnect() {}
}

vi.stubGlobal('ResizeObserver', MockResizeObserver)

function createMockTerminal() {
  return {
    fitAddon: {
      fit: vi.fn(),
    },
  }
}

describe('useTerminalViewport', () => {
  let container: HTMLElement
  let mockResizeObserver: ResizeObserver | null
  let originalVisualViewport: VisualViewport | null
  let originalInnerHeight: number

  beforeEach(() => {
    container = document.createElement('div')
    document.body.appendChild(container)

    // Reset mock state
    mockShared.isAdjustResize.value = false
    mockShared.keyboardHeight.value = 0
    mockIsPC.value = false

    // Save originals
    originalInnerHeight = window.innerHeight
    originalVisualViewport = window.visualViewport
  })

  afterEach(() => {
    document.body.removeChild(container)
    vi.restoreAllMocks()

    // Restore originals
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

  it('calculates viewport height from visualViewport when available', () => {
    const terminal = ref(null)
    const containerRef = ref<HTMLElement | null>(container)
    const viewport = useTerminalViewport(terminal, containerRef)

    // Mock visualViewport
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
    // But also compared with fullScreenHeight(800) - innerHeight(800) = 0
    // So max(200, 0, 0) = 200
    expect(viewport.keyboardHeight.value).toBe(200)

    viewport.stopWatching()
  })

  it('keeps detecting an open keyboard across deactivate/reactivate cycles', () => {
    const terminal = ref(null)
    const containerRef = ref<HTMLElement | null>(container)
    const viewport = useTerminalViewport(terminal, containerRef)

    // Keyboard open (non-adjustResize: innerHeight unchanged, vv shrunk)
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
    expect(mockShared.keyboardHeight.value).toBe(200)

    // Leaving the terminal tab must not clobber keyboard detection: if the
    // keyboard is still open on reactivation, the dock must hide again.
    viewport.stopWatching()
    viewport.startWatching()
    expect(mockShared.keyboardHeight.value).toBe(200)

    viewport.stopWatching()
  })

  it('clears the shared keyboard height when the active container becomes null', () => {
    let resizeHandler: (() => void) | null = null
    const terminal = ref(null)
    const containerRef = ref<HTMLElement | null>(container)
    const viewport = useTerminalViewport(terminal, containerRef)

    // Keyboard open — shared singleton must reflect it (App.vue reads this)
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
    // following keyboard-close resize must clear the stale shared height so the
    // Dock is not left hidden forever.
    containerRef.value = null
    resizeHandler!()
    expect(mockShared.keyboardHeight.value).toBe(0)
  })

  it('detects keyboard from Android adjustResize (innerHeight shrinks)', () => {
    const terminal = ref(null)
    const containerRef = ref<HTMLElement | null>(container)
    const viewport = useTerminalViewport(terminal, containerRef)

    // Mock visualViewport as if keyboard is open on Android
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
    // vvKeyboard = innerHeight(500) - vv.height(500) - offsetTop(0) = 0
    // resizeKeyboard = fullScreenHeight(800) - innerHeight(500) = 300
    // max(0, 300, 0) = 300
    expect(viewport.keyboardHeight.value).toBe(300)

    viewport.stopWatching()
  })

  it('uses container clientHeight when visualViewport is not available', () => {
    const terminal = ref(null)
    const containerRef = ref<HTMLElement | null>(container)
    const viewport = useTerminalViewport(terminal, containerRef)

    // Remove visualViewport
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

    // Should not throw
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

    // Scenario: visualViewport reports keyboard, but innerHeight shrink is larger
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

    // vvKeyboard = 600 - 700 - 0 = -100 → Math.max with 0 later
    // resizeKeyboard = 800 - 600 = 200
    // max(-100, 200, 0) = 200
    expect(viewport.keyboardHeight.value).toBe(200)

    viewport.stopWatching()
  })

  it('clamps keyboard height to 0 minimum', () => {
    const terminal = ref(null)
    const containerRef = ref<HTMLElement | null>(container)
    const viewport = useTerminalViewport(terminal, containerRef)

    // No keyboard visible
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

    // vvKeyboard = 800 - 800 - 0 = 0
    // resizeKeyboard = 800 - 800 = 0
    // max(0, 0, 0) = 0
    expect(viewport.keyboardHeight.value).toBe(0)

    viewport.stopWatching()
  })

  it('accounts for visualViewport offsetTop in keyboard height calculation', () => {
    const terminal = ref(null)
    const containerRef = ref<HTMLElement | null>(container)
    const viewport = useTerminalViewport(terminal, containerRef)

    // URL bar takes 50px at top
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
    // resizeKeyboard = 800 - 800 = 0
    // max(50, 0, 0) = 50
    expect(viewport.keyboardHeight.value).toBe(50)

    viewport.stopWatching()
  })

  it('debounces fit() calls during viewport updates', () => {
    vi.useFakeTimers()
    const mockTerminal = createMockTerminal()
    const terminal = ref(mockTerminal)
    const containerRef = ref<HTMLElement | null>(container)
    const viewport = useTerminalViewport(terminal, containerRef)

    // Mock visualViewport so startWatching works
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

    // fit() should not be called immediately (debounced)
    expect(mockTerminal.fitAddon.fit).not.toHaveBeenCalled()

    // After debounce (100ms), fit() should be called
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
    Object.defineProperty(window, 'innerHeight', {
      value: 800,
      writable: true,
      configurable: true,
    })

    viewport.startWatching()
    expect(mockTerminal.fitAddon.fit).not.toHaveBeenCalled()

    // Stop watching before debounce fires
    viewport.stopWatching()

    // Advance past debounce time — fit() should NOT be called
    vi.advanceTimersByTime(200)
    expect(mockTerminal.fitAddon.fit).not.toHaveBeenCalled()

    vi.useRealTimers()
  })

  it('detects adjustResize when innerHeight shrinks (Android native)', () => {
    const terminal = ref(null)
    const containerRef = ref<HTMLElement | null>(container)
    const viewport = useTerminalViewport(terminal, containerRef)

    // Android adjustResize: innerHeight shrinks when keyboard opens
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
      value: 500, // shrunk from 800 → adjustResize
      writable: true,
      configurable: true,
    })

    viewport.startWatching()
    expect(mockShared.isAdjustResize.value).toBe(true)

    viewport.stopWatching()
    // Reset on stop
    expect(mockShared.isAdjustResize.value).toBe(false)
  })

  it('keeps keyboard height at 0 on PC even when innerHeight shrinks (window resize)', () => {
    mockIsPC.value = true
    const terminal = ref(null)
    const containerRef = ref<HTMLElement | null>(container)
    const viewport = useTerminalViewport(terminal, containerRef)

    // On PC, shrinking the window lowers innerHeight exactly like an Android
    // adjustResize keyboard would — but it is a user window resize, not a soft
    // keyboard. It must not set keyboardHeight > 0 (which would hide the bottom
    // dock via anyKeyboardActive) nor flag adjustResize.
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
      value: 500, // shrunk from 800 → would otherwise read as a keyboard
      writable: true,
      configurable: true,
    })

    viewport.startWatching()

    expect(viewport.viewportHeight.value).toBe(500)
    expect(viewport.keyboardHeight.value).toBe(0)
    expect(mockShared.isAdjustResize.value).toBe(false)

    viewport.stopWatching()
  })

  it('does not detect adjustResize when innerHeight stays same (PWA/iOS)', () => {
    const terminal = ref(null)
    const containerRef = ref<HTMLElement | null>(container)
    const viewport = useTerminalViewport(terminal, containerRef)

    // PWA standalone / iOS: innerHeight stays 800, visualViewport shrinks
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
      value: 800, // unchanged — NOT adjustResize
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
