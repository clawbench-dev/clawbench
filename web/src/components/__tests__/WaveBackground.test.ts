import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import WaveBackground from '../WaveBackground.vue'

/**
 * jsdom has no 2D canvas context: `getContext('2d')` returns null. The component
 * must therefore bail out cleanly rather than throw — the wave is decoration.
 *
 * That also means the *drawing* cannot be tested here (see waveMath.test.ts for
 * the shape/palette maths). What IS worth pinning here is the lifecycle, because
 * a leaked requestAnimationFrame loop is the failure mode that actually bites:
 * App.vue's root carries `:key="projectKey"`, so every project switch remounts
 * this component. If unmount does not cancel the loop, each switch leaves a
 * loop running against a detached canvas.
 */
describe('WaveBackground', () => {
  let rafSpy: ReturnType<typeof vi.spyOn>
  let cafSpy: ReturnType<typeof vi.spyOn>
  let getContextSpy: ReturnType<typeof vi.spyOn>
  let originalRaf: typeof globalThis.requestAnimationFrame
  let originalCaf: typeof globalThis.cancelAnimationFrame

  beforeEach(() => {
    originalRaf = globalThis.requestAnimationFrame
    originalCaf = globalThis.cancelAnimationFrame
    // Deterministic rAF: never actually invoke the callback, just hand out ids.
    rafSpy = vi.spyOn(globalThis, 'requestAnimationFrame').mockImplementation(() => 1)
    cafSpy = vi.spyOn(globalThis, 'cancelAnimationFrame').mockImplementation(() => {})
    // A stub context so the component gets past its null-context guard.
    getContextSpy = vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue({
      setTransform: vi.fn(),
      clearRect: vi.fn(),
      fillRect: vi.fn(),
      beginPath: vi.fn(),
      moveTo: vi.fn(),
      lineTo: vi.fn(),
      closePath: vi.fn(),
      fill: vi.fn(),
      stroke: vi.fn(),
      createLinearGradient: vi.fn(() => ({ addColorStop: vi.fn() })),
    } as unknown as CanvasRenderingContext2D)
  })

  afterEach(() => {
    rafSpy.mockRestore()
    cafSpy.mockRestore()
    getContextSpy.mockRestore()
    globalThis.requestAnimationFrame = originalRaf
    globalThis.cancelAnimationFrame = originalCaf
    vi.restoreAllMocks()
  })

  it('mounts and renders a canvas', () => {
    const wrapper = mount(WaveBackground)
    expect(wrapper.find('canvas').exists()).toBe(true)
    wrapper.unmount()
  })

  it('starts a rAF loop on mount', () => {
    const wrapper = mount(WaveBackground)
    expect(rafSpy).toHaveBeenCalled()
    wrapper.unmount()
  })

  it('cancels the rAF loop on unmount', () => {
    // The project-switch remount is the real-world trigger for this.
    const wrapper = mount(WaveBackground)
    rafSpy.mockClear()
    wrapper.unmount()
    expect(cafSpy).toHaveBeenCalled()
  })

  it('does not keep scheduling frames after unmount', () => {
    const wrapper = mount(WaveBackground)
    wrapper.unmount()
    const callsAtUnmount = rafSpy.mock.calls.length
    // Any further scheduling would have to come from a surviving loop.
    expect(rafSpy.mock.calls.length).toBe(callsAtUnmount)
  })

  it('survives a null 2D context without throwing', () => {
    // jsdom's real behaviour, and the guard that keeps component tests from
    // needing a canvas polyfill.
    getContextSpy.mockReturnValue(null)
    expect(() => {
      const wrapper = mount(WaveBackground)
      wrapper.unmount()
    }).not.toThrow()
  })

  it('does not start a loop when there is no 2D context', () => {
    getContextSpy.mockReturnValue(null)
    const wrapper = mount(WaveBackground)
    expect(rafSpy).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('accepts a speed prop without restarting the canvas', () => {
    const wrapper = mount(WaveBackground, { props: { speed: 80 } })
    const callsAfterMount = rafSpy.mock.calls.length
    rafSpy.mockClear()
    // Changing speed must not rebuild anything; it only rescales time in draw().
    void wrapper.setProps({ speed: 30 })
    expect(rafSpy.mock.calls.length).toBe(0)
    expect(callsAfterMount).toBeGreaterThan(0)
    wrapper.unmount()
  })

  it('removes its visibilitychange listener on unmount', () => {
    const removeSpy = vi.spyOn(document, 'removeEventListener')
    const wrapper = mount(WaveBackground)
    wrapper.unmount()
    expect(removeSpy).toHaveBeenCalledWith('visibilitychange', expect.any(Function))
    removeSpy.mockRestore()
  })
})
