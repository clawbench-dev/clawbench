import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { ref, nextTick, effectScope, type Ref } from 'vue'
import { useTerminalKeyboardDetect } from '@/composables/useTerminalKeyboardDetect'

const mockShared = vi.hoisted(() => {
  // eslint-disable-next-line @typescript-eslint/no-var-requires
  const { ref } = require('vue') as typeof import('vue')
  return { keyboardHeight: ref(0), isAdjustResize: ref(false) }
})

vi.mock('@/composables/useTerminalKeyboard', () => ({
  useTerminalKeyboard: () => ({
    keyboardHeight: mockShared.keyboardHeight,
    setKeyboardHeight: (h: number) => { mockShared.keyboardHeight.value = h },
    isAdjustResize: mockShared.isAdjustResize,
    setAdjustResize: (v: boolean) => { mockShared.isAdjustResize.value = v },
    fullScreenHeight: 800,
  }),
}))

const mockIsPC = ref(false)
vi.mock('@/composables/usePlatformDetect', () => ({
  usePlatformDetect: () => ({ isPC: mockIsPC }),
}))

let originalVisualViewport: VisualViewport | null
let originalInnerHeight: number
let originalMaxTouchPoints: number | undefined
let vvHeight = 800
let vvOffsetTop = 0
let vvResizeHandler: (() => void) | null = null

function setVisualViewport() {
  Object.defineProperty(window, 'visualViewport', {
    value: {
      get height() { return vvHeight },
      get offsetTop() { return vvOffsetTop },
      addEventListener: (_type: string, cb: () => void) => { vvResizeHandler = cb },
      removeEventListener: vi.fn(),
    },
    writable: true,
    configurable: true,
  })
}

function setInnerHeight(h: number) {
  Object.defineProperty(window, 'innerHeight', {
    value: h,
    writable: true,
    configurable: true,
  })
}

/** Soft keyboard opens: shrink visualViewport, keep innerHeight (iOS/PWA). */
function openKeyboardNoAdjustResize() {
  vvHeight = 600
  vvResizeHandler?.()
}

beforeEach(() => {
  mockShared.keyboardHeight.value = 0
  mockShared.isAdjustResize.value = false
  mockIsPC.value = false
  vvHeight = 800
  vvOffsetTop = 0
  vvResizeHandler = null
  originalInnerHeight = window.innerHeight
  originalVisualViewport = window.visualViewport
  originalMaxTouchPoints = (navigator as Navigator & { maxTouchPoints?: number }).maxTouchPoints
  setInnerHeight(800)
  setVisualViewport()
})

afterEach(() => {
  if (typeof originalMaxTouchPoints !== 'undefined') {
    Object.defineProperty(navigator, 'maxTouchPoints', {
      value: originalMaxTouchPoints,
      writable: true,
      configurable: true,
    })
  }
  setInnerHeight(originalInnerHeight)
  if (originalVisualViewport) {
    Object.defineProperty(window, 'visualViewport', {
      value: originalVisualViewport,
      writable: true,
      configurable: true,
    })
  }
})

describe('useTerminalKeyboardDetect', () => {
  it('starts detecting as soon as the terminal tab is active and hides the Dock on keyboard open', () => {
    const terminalActive = ref(true)
    const scope = effectScope()
    scope.run(() => useTerminalKeyboardDetect(terminalActive))
    try {
      // Immediate start with no keyboard → 0
      expect(mockShared.keyboardHeight.value).toBe(0)

      openKeyboardNoAdjustResize()
      // vvKeyboard = 800 - 600 - 0 = 200
      expect(mockShared.keyboardHeight.value).toBe(200)
      expect(mockShared.isAdjustResize.value).toBe(false)
    } finally {
      scope.stop()
    }
  })

  it('stops and resets the height to 0 when the terminal tab is deactivated', async () => {
    const terminalActive = ref(true)
    const scope = effectScope()
    scope.run(() => useTerminalKeyboardDetect(terminalActive))
    try {
      openKeyboardNoAdjustResize()
      expect(mockShared.keyboardHeight.value).toBe(200)

      terminalActive.value = false
      await nextTick()
      expect(mockShared.keyboardHeight.value).toBe(0)
      expect(mockShared.isAdjustResize.value).toBe(false)
    } finally {
      scope.stop()
    }
  })

  it('re-activating with the keyboard still open keeps detection working (no stale reset)', async () => {
    const terminalActive = ref(false)
    const scope = effectScope()
    scope.run(() => useTerminalKeyboardDetect(terminalActive))
    try {
      terminalActive.value = true
      await nextTick()
      openKeyboardNoAdjustResize()
      expect(mockShared.keyboardHeight.value).toBe(200)

      // Leave and re-enter the terminal tab while the keyboard is still open.
      terminalActive.value = false
      await nextTick()
      terminalActive.value = true
      await nextTick()
      expect(mockShared.keyboardHeight.value).toBe(200)
    } finally {
      scope.stop()
    }
  })

  it('detects keyboard from Android adjustResize (innerHeight shrinks)', () => {
    const terminalActive = ref(true)
    const scope = effectScope()
    scope.run(() => useTerminalKeyboardDetect(terminalActive))
    try {
      // Android adjustResize: innerHeight shrinks, vv follows it.
      vvHeight = 500
      setInnerHeight(500)
      vvResizeHandler?.()

      // vvKeyboard = 500 - 500 = 0; resizeKeyboard = 800 - 500 = 300
      expect(mockShared.keyboardHeight.value).toBe(300)
      expect(mockShared.isAdjustResize.value).toBe(true)
    } finally {
      scope.stop()
    }
  })

  it('uses the larger of vvKeyboard and resizeKeyboard', () => {
    const terminalActive = ref(true)
    const scope = effectScope()
    scope.run(() => useTerminalKeyboardDetect(terminalActive))
    try {
      // innerHeight shrunk more than visualViewport suggests
      vvHeight = 700
      setInnerHeight(600)
      vvResizeHandler?.()

      // vvKeyboard = 600 - 700 = -100; resizeKeyboard = 800 - 600 = 200
      expect(mockShared.keyboardHeight.value).toBe(200)
    } finally {
      scope.stop()
    }
  })

  it('clamps keyboard height to 0 minimum', () => {
    const terminalActive = ref(true)
    const scope = effectScope()
    scope.run(() => useTerminalKeyboardDetect(terminalActive))
    try {
      vvHeight = 800
      setInnerHeight(800)
      vvResizeHandler?.()
      expect(mockShared.keyboardHeight.value).toBe(0)
    } finally {
      scope.stop()
    }
  })

  it('accounts for visualViewport offsetTop (URL bar)', () => {
    const terminalActive = ref(true)
    const scope = effectScope()
    scope.run(() => useTerminalKeyboardDetect(terminalActive))
    try {
      vvOffsetTop = 50
      vvHeight = 700
      setInnerHeight(800)
      vvResizeHandler?.()

      // vvKeyboard = 800 - 700 - 50 = 50
      expect(mockShared.keyboardHeight.value).toBe(50)
    } finally {
      scope.stop()
    }
  })

  it('keeps keyboard height at 0 on a non-touch PC even when innerHeight shrinks (window resize)', () => {
    mockIsPC.value = true
    Object.defineProperty(navigator, 'maxTouchPoints', { value: 0, configurable: true, writable: true })
    const terminalActive = ref(true)
    const scope = effectScope()
    scope.run(() => useTerminalKeyboardDetect(terminalActive))
    try {
      vvHeight = 500
      setInnerHeight(500) // shrunk — would otherwise read as a keyboard
      vvResizeHandler?.()

      expect(mockShared.keyboardHeight.value).toBe(0)
      expect(mockShared.isAdjustResize.value).toBe(false)
    } finally {
      scope.stop()
    }
  })

  it('detects the keyboard on a touch device even when its UA is misdetected as a PC', () => {
    // The classic phone-in-WebView case: UA says desktop but the device has a
    // touchscreen and a soft keyboard. Must NOT be bypassed.
    mockIsPC.value = true
    Object.defineProperty(navigator, 'maxTouchPoints', { value: 5, configurable: true, writable: true })
    const terminalActive = ref(true)
    const scope = effectScope()
    scope.run(() => useTerminalKeyboardDetect(terminalActive))
    try {
      openKeyboardNoAdjustResize()
      expect(mockShared.keyboardHeight.value).toBe(200)
    } finally {
      scope.stop()
    }
  })

  it('poll fallback detects the keyboard even when no resize event fires', () => {
    vi.useFakeTimers()
    const terminalActive = ref(true)
    const scope = effectScope()
    scope.run(() => useTerminalKeyboardDetect(terminalActive))
    try {
      // Keyboard opens but the WebView never dispatches a resize event.
      vvHeight = 600
      setInnerHeight(800)
      expect(mockShared.keyboardHeight.value).toBe(0)

      vi.advanceTimersByTime(200)
      expect(mockShared.keyboardHeight.value).toBe(200)

      // Keyboard closes silently too.
      vvHeight = 800
      vi.advanceTimersByTime(200)
      expect(mockShared.keyboardHeight.value).toBe(0)
    } finally {
      scope.stop()
      vi.useRealTimers()
    }
  })
})