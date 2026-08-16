import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { ref } from 'vue'
import { useTerminalViewport } from '@/composables/useTerminalViewport'

// Soft-keyboard detection no longer lives here (it moved to
// useTerminalKeyboardDetect), so these tests cover only sizing/fit.

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

  it('initializes with zero viewport height', () => {
    const terminal = ref(null)
    const containerRef = ref<HTMLElement | null>(container)
    const viewport = useTerminalViewport(terminal, containerRef)

    expect(viewport.viewportHeight.value).toBe(0)
  })

  it('calculates viewport height from visualViewport when available', () => {
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

    viewport.startWatching()

    expect(viewport.viewportHeight.value).toBe(600)

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

    viewport.stopWatching()
  })

  it('does nothing on updateViewport when containerRef is null', () => {
    const terminal = ref(null)
    const containerRef = ref<HTMLElement | null>(null)
    const viewport = useTerminalViewport(terminal, containerRef)

    // Should not throw
    viewport.startWatching()

    expect(viewport.viewportHeight.value).toBe(0)

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

    viewport.startWatching()
    expect(mockTerminal.fitAddon.fit).not.toHaveBeenCalled()

    // Stop watching before debounce fires
    viewport.stopWatching()

    // Advance past debounce time — fit() should NOT be called
    vi.advanceTimersByTime(200)
    expect(mockTerminal.fitAddon.fit).not.toHaveBeenCalled()

    vi.useRealTimers()
  })
})