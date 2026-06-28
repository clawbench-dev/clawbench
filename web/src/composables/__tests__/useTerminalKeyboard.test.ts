import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'

// We need to test the module-level singleton behavior.
// Since fullScreenHeight is captured at module load time as window.innerHeight,
// and the module uses a module-level ref, we need to be careful about isolation.

describe('useTerminalKeyboard', () => {
  // Reset the module between test groups to avoid shared state
  beforeEach(() => {
    vi.resetModules()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('exposes keyboardHeight as a reactive ref starting at 0', async () => {
    const { useTerminalKeyboard } = await import('@/composables/useTerminalKeyboard')
    const { keyboardHeight } = useTerminalKeyboard()

    expect(keyboardHeight.value).toBe(0)
  })

  it('setKeyboardHeight updates the reactive keyboardHeight', async () => {
    const { useTerminalKeyboard } = await import('@/composables/useTerminalKeyboard')
    const { keyboardHeight, setKeyboardHeight } = useTerminalKeyboard()

    setKeyboardHeight(300)
    expect(keyboardHeight.value).toBe(300)

    // Reset for other tests
    setKeyboardHeight(0)
  })

  it('shares keyboardHeight across multiple useTerminalKeyboard calls', async () => {
    const { useTerminalKeyboard } = await import('@/composables/useTerminalKeyboard')
    const instance1 = useTerminalKeyboard()
    const instance2 = useTerminalKeyboard()

    instance1.setKeyboardHeight(250)
    expect(instance2.keyboardHeight.value).toBe(250)

    // Cleanup
    instance1.setKeyboardHeight(0)
  })

  it('captures fullScreenHeight from window.innerHeight at module load time', async () => {
    const { useTerminalKeyboard } = await import('@/composables/useTerminalKeyboard')
    const { fullScreenHeight } = useTerminalKeyboard()

    // fullScreenHeight should be a number equal to window.innerHeight
    // at the time the module was loaded
    expect(typeof fullScreenHeight).toBe('number')
    expect(fullScreenHeight).toBeGreaterThan(0)
  })

  it('setKeyboardHeight to 0 resets the value', async () => {
    const { useTerminalKeyboard } = await import('@/composables/useTerminalKeyboard')
    const { keyboardHeight, setKeyboardHeight } = useTerminalKeyboard()

    setKeyboardHeight(400)
    expect(keyboardHeight.value).toBe(400)

    setKeyboardHeight(0)
    expect(keyboardHeight.value).toBe(0)
  })

  it('setKeyboardHeight with negative value still updates', async () => {
    const { useTerminalKeyboard } = await import('@/composables/useTerminalKeyboard')
    const { keyboardHeight, setKeyboardHeight } = useTerminalKeyboard()

    setKeyboardHeight(-50)
    expect(keyboardHeight.value).toBe(-50)

    // Cleanup
    setKeyboardHeight(0)
  })

  it('exposes isAdjustResize as a reactive ref starting at false', async () => {
    const { useTerminalKeyboard } = await import('@/composables/useTerminalKeyboard')
    const { isAdjustResize } = useTerminalKeyboard()

    expect(isAdjustResize.value).toBe(false)
  })

  it('setAdjustResize updates the reactive isAdjustResize', async () => {
    const { useTerminalKeyboard } = await import('@/composables/useTerminalKeyboard')
    const { isAdjustResize, setAdjustResize } = useTerminalKeyboard()

    setAdjustResize(true)
    expect(isAdjustResize.value).toBe(true)

    setAdjustResize(false)
    expect(isAdjustResize.value).toBe(false)
  })

  it('shares isAdjustResize across multiple useTerminalKeyboard calls', async () => {
    const { useTerminalKeyboard } = await import('@/composables/useTerminalKeyboard')
    const instance1 = useTerminalKeyboard()
    const instance2 = useTerminalKeyboard()

    instance1.setAdjustResize(true)
    expect(instance2.isAdjustResize.value).toBe(true)

    // Cleanup
    instance1.setAdjustResize(false)
  })
})
