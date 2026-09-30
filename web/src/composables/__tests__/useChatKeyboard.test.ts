import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'

/**
 * useChatKeyboard exists for iOS WKWebView, where the soft keyboard shrinks only
 * the visual viewport (innerHeight stays put). It reports:
 *     height = innerHeight - visualViewport.height - visualViewport.offsetTop
 *
 * That expression is NOT keyboard-specific. On desktop it is normally 0, but a
 * classic horizontal scrollbar makes it ~15px and a pinch-zoom makes it far
 * larger (measured 160px at scale 1.25). App.vue applies the value as
 * `.chat-keyboard-open { bottom: <n>px }` on .app-container, which resizes (and
 * clears) the animated wallpaper canvas — the "dynamic wallpaper flashes when
 * the chat input is focused" bug. Only the chat input calls this composable,
 * which is why the file-manager search input did not flash.
 *
 * The value must therefore be gated by a minimum-height threshold, exactly as
 * useSoftKeyboard already does (KEYBOARD_MIN_HEIGHT = 120).
 */
const originalVV = Object.getOwnPropertyDescriptor(window, 'visualViewport')

function stubViewport(delta: number) {
  const vv = {
    width: 1280,
    height: window.innerHeight - delta,
    offsetTop: 0,
    offsetLeft: 0,
    scale: 1,
    pageTop: 0,
    pageLeft: 0,
    addEventListener() {},
    removeEventListener() {},
    dispatchEvent() { return true },
  }
  Object.defineProperty(window, 'visualViewport', { configurable: true, value: vv })
}

describe('useChatKeyboard', () => {
  beforeEach(() => {
    vi.resetModules()
  })

  afterEach(() => {
    if (originalVV) Object.defineProperty(window, 'visualViewport', originalVV)
    else Reflect.deleteProperty(window, 'visualViewport')
  })

  it('does not treat a small viewport difference (scrollbar / zoom artifact) as a keyboard', async () => {
    // 15px is exactly what a classic horizontal scrollbar produces on desktop.
    stubViewport(15)
    const { useChatKeyboard } = await import('../useChatKeyboard')
    const { chatKeyboardHeight, activate } = useChatKeyboard()
    activate()
    expect(chatKeyboardHeight.value).toBe(0)
  })

  it('reports a real soft-keyboard height', async () => {
    stubViewport(300)
    const { useChatKeyboard } = await import('../useChatKeyboard')
    const { chatKeyboardHeight, activate } = useChatKeyboard()
    activate()
    expect(chatKeyboardHeight.value).toBe(300)
  })
})
