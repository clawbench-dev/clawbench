import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useAndroidBackPress, BACK_PRESS_EVENT } from '../useAndroidBackPress'

// Replicates how MainActivity#onBackPressed reads the flag: it evaluates a JS
// snippet that dispatches the event and returns window.__clawbenchBackHandled
// **synchronously**. An async listener would set the flag too late.
function dispatchBackPressLikeNative(): boolean | undefined {
  window.__clawbenchBackHandled = false
  window.dispatchEvent(new Event(BACK_PRESS_EVENT))
  return window.__clawbenchBackHandled
}

describe('useAndroidBackPress', () => {
  let dispose: (() => void) | undefined

  beforeEach(() => {
    window.__clawbenchBackHandled = undefined
  })

  afterEach(() => {
    dispose?.()
    dispose = undefined
    window.__clawbenchBackHandled = undefined
  })

  it('writes the handled flag synchronously even though the navigation is async', () => {
    const navigate = vi.fn(async () => true)
    dispose = useAndroidBackPress({
      canHandle: () => true,
      navigate,
      requestExitConfirm: () => false,
      showExitHint: () => {},
    })

    // No await: the native side reads the return value in the same tick.
    const handled = dispatchBackPressLikeNative()
    expect(handled).toBe(true)
    expect(navigate).toHaveBeenCalledTimes(1)
  })

  it('first press with nothing to handle: prevents exit and shows the hint', () => {
    const showExitHint = vi.fn()
    dispose = useAndroidBackPress({
      canHandle: () => false,
      navigate: vi.fn(),
      requestExitConfirm: () => false,
      showExitHint,
    })

    const handled = dispatchBackPressLikeNative()
    expect(handled).toBe(true)
    expect(showExitHint).toHaveBeenCalledTimes(1)
  })

  it('second press within the timeout: allows the native exit', () => {
    dispose = useAndroidBackPress({
      canHandle: () => false,
      navigate: vi.fn(),
      requestExitConfirm: () => true,
      showExitHint: () => {},
    })

    const handled = dispatchBackPressLikeNative()
    expect(handled).toBe(false)
  })

  it('navigate() never runs when canHandle() is false', () => {
    const navigate = vi.fn()
    dispose = useAndroidBackPress({
      canHandle: () => false,
      navigate,
      requestExitConfirm: () => true,
      showExitHint: () => {},
    })

    dispatchBackPressLikeNative()
    expect(navigate).not.toHaveBeenCalled()
  })

  it('dispose removes the listener', () => {
    const disposeFn = useAndroidBackPress({
      canHandle: () => true,
      navigate: vi.fn(),
      requestExitConfirm: () => false,
      showExitHint: () => {},
    })
    disposeFn()

    const handled = dispatchBackPressLikeNative()
    // The listener is gone, so the flag stays at its initial value.
    expect(handled).toBe(false)
  })

  describe('reason', () => {
    function dispatchWithDetail(detail: unknown): void {
      window.__clawbenchBackHandled = false
      window.dispatchEvent(new CustomEvent(BACK_PRESS_EVENT, { detail }))
    }

    it('forwards the reason carried by the event', () => {
      const canHandle = vi.fn(() => true)
      const navigate = vi.fn(async () => true)
      dispose = useAndroidBackPress({
        canHandle,
        navigate,
        requestExitConfirm: () => false,
        showExitHint: () => {},
      })

      dispatchWithDetail({ reason: 'edge-swipe' })
      expect(canHandle).toHaveBeenCalledWith('edge-swipe')
      expect(navigate).toHaveBeenCalledWith('edge-swipe')
    })

    it('defaults to android when the event carries no detail (native back)', () => {
      const canHandle = vi.fn(() => true)
      const navigate = vi.fn(async () => true)
      dispose = useAndroidBackPress({
        canHandle,
        navigate,
        requestExitConfirm: () => false,
        showExitHint: () => {},
      })

      dispatchBackPressLikeNative()
      expect(canHandle).toHaveBeenCalledWith('android')
      expect(navigate).toHaveBeenCalledWith('android')
    })

    it('falls back to android for an unknown reason', () => {
      const canHandle = vi.fn(() => true)
      const navigate = vi.fn(async () => true)
      dispose = useAndroidBackPress({
        canHandle,
        navigate,
        requestExitConfirm: () => false,
        showExitHint: () => {},
      })

      dispatchWithDetail({ reason: 'not-a-reason' })
      expect(canHandle).toHaveBeenCalledWith('android')
    })
  })
})
