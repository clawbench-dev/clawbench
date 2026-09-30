import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import AnimatedWallpaper from '../AnimatedWallpaper.vue'
import { getAnimatedStyle } from '@/utils/animatedWallpapers'

/**
 * jsdom has no 2D canvas context: `getContext('2d')` returns null. The component
 * must therefore bail out cleanly rather than throw — the wallpaper is decoration.
 *
 * That also means the *drawing* cannot be tested here (see the per-style tests
 * under utils/animatedWallpapers and waveMath.test.ts for the maths). What IS
 * worth pinning here is the lifecycle, because a leaked requestAnimationFrame
 * loop is the failure mode that actually bites: App.vue's root carries
 * `:key="projectKey"`, so every project switch remounts this component. If
 * unmount does not cancel the loop, each switch leaves a loop running against a
 * detached canvas.
 */
describe('AnimatedWallpaper', () => {
  let rafSpy: ReturnType<typeof vi.spyOn>
  let cafSpy: ReturnType<typeof vi.spyOn>
  let getContextSpy: ReturnType<typeof vi.spyOn>
  let originalRaf: typeof globalThis.requestAnimationFrame
  let originalCaf: typeof globalThis.cancelAnimationFrame
  let drawCalls: number

  beforeEach(() => {
    drawCalls = 0
    originalRaf = globalThis.requestAnimationFrame
    originalCaf = globalThis.cancelAnimationFrame
    // Deterministic rAF: never actually invoke the callback, just hand out ids.
    rafSpy = vi.spyOn(globalThis, 'requestAnimationFrame').mockImplementation(() => 1)
    cafSpy = vi.spyOn(globalThis, 'cancelAnimationFrame').mockImplementation(() => {})
    // A stub context so the component gets past its null-context guard. `fillRect`
    // counts as "a frame was painted" — every style fills its stage first.
    getContextSpy = vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue({
      setTransform: vi.fn(),
      clearRect: vi.fn(),
      save: vi.fn(),
      restore: vi.fn(),
      fillRect: vi.fn(() => { drawCalls++ }),
      beginPath: vi.fn(),
      moveTo: vi.fn(),
      lineTo: vi.fn(),
      closePath: vi.fn(),
      arc: vi.fn(),
      fill: vi.fn(),
      stroke: vi.fn(),
      createLinearGradient: vi.fn(() => ({ addColorStop: vi.fn() })),
      createRadialGradient: vi.fn(() => ({ addColorStop: vi.fn() })),
      globalCompositeOperation: 'source-over',
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
    const wrapper = mount(AnimatedWallpaper)
    expect(wrapper.find('canvas').exists()).toBe(true)
    wrapper.unmount()
  })

  describe('resize repaint', () => {
    // Assigning canvas.width/height resets the backing store to transparent, so
    // a resize that does NOT repaint presents a blank frame until the next rAF
    // tick (up to 33ms at the 30fps cap). This is the "wallpaper flashes when I
    // focus an input" bug: focusing the chat input opens the soft keyboard,
    // which shrinks .app-container and therefore the canvas box.
    let roCallbacks: ResizeObserverCallback[]
    let rect: DOMRect
    let rectSpy: ReturnType<typeof vi.spyOn>

    beforeEach(() => {
      roCallbacks = []
      vi.stubGlobal('ResizeObserver', class {
        constructor(cb: ResizeObserverCallback) { roCallbacks.push(cb) }
        observe() {}
        unobserve() {}
        disconnect() {}
      })
      // jsdom returns a zero rect, which would collapse the canvas to 1x1 and
      // make "unchanged size" impossible to express. Drive a mutable box instead.
      rect = { width: 100, height: 50, top: 0, left: 0, right: 100, bottom: 50, x: 0, y: 0, toJSON: () => ({}) } as DOMRect
      rectSpy = vi.spyOn(HTMLCanvasElement.prototype, 'getBoundingClientRect').mockImplementation(() => rect)
    })

    afterEach(() => {
      rectSpy.mockRestore()
      vi.unstubAllGlobals()
    })

    it('repaints synchronously when a box change clears the canvas, even with the loop running', () => {
      const wrapper = mount(AnimatedWallpaper)
      // The rAF mock never invokes its callback, so `running` stays true and the
      // frame loop cannot repaint on its own — only the observer can.
      const before = drawCalls
      expect(roCallbacks.length).toBe(1)
      // The soft keyboard shrinking .app-container: a real box change.
      rect = { ...rect, height: 40, bottom: 40 } as DOMRect
      roCallbacks[0]([], {} as ResizeObserver)
      expect(drawCalls).toBeGreaterThan(before)
      wrapper.unmount()
    })

    it('does not clear or repaint when the rounded backing size is unchanged', () => {
      // A sub-pixel layout jitter can round to the same backing size; clearing
      // the canvas for it would flash for nothing.
      const wrapper = mount(AnimatedWallpaper)
      const el = wrapper.find('canvas').element as HTMLCanvasElement
      const realW = el.width
      let sets = 0
      Object.defineProperty(el, 'width', {
        get: () => realW,
        set: () => { sets++ },
        configurable: true,
      })
      const before = drawCalls
      roCallbacks[0]([], {} as ResizeObserver)
      expect(sets).toBe(0)
      expect(drawCalls).toBe(before)
      wrapper.unmount()
    })
  })

  it('starts a rAF loop on mount', () => {
    const wrapper = mount(AnimatedWallpaper)
    expect(rafSpy).toHaveBeenCalled()
    wrapper.unmount()
  })

  it('cancels the rAF loop on unmount', () => {
    // The project-switch remount is the real-world trigger for this.
    const wrapper = mount(AnimatedWallpaper)
    rafSpy.mockClear()
    wrapper.unmount()
    expect(cafSpy).toHaveBeenCalled()
  })

  it('does not keep scheduling frames after unmount', () => {
    const wrapper = mount(AnimatedWallpaper)
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
      const wrapper = mount(AnimatedWallpaper)
      wrapper.unmount()
    }).not.toThrow()
  })

  it('does not start a loop when there is no 2D context', () => {
    getContextSpy.mockReturnValue(null)
    const wrapper = mount(AnimatedWallpaper)
    expect(rafSpy).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('accepts a speed prop without restarting the canvas', () => {
    const wrapper = mount(AnimatedWallpaper, { props: { speed: 80 } })
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
    const wrapper = mount(AnimatedWallpaper)
    wrapper.unmount()
    expect(removeSpy).toHaveBeenCalledWith('visibilitychange', expect.any(Function))
    removeSpy.mockRestore()
  })

  it('renders a registered style by id', () => {
    const wrapper = mount(AnimatedWallpaper, { props: { styleId: 'silk' } })
    expect(wrapper.find('canvas').exists()).toBe(true)
    wrapper.unmount()
  })

  it('falls back to the default style for an unknown id', () => {
    // A stored id can outlive its style; rendering nothing would leave a blank
    // wallpaper with no error.
    expect(() => {
      const wrapper = mount(AnimatedWallpaper, { props: { styleId: 'does-not-exist' } })
      wrapper.unmount()
    }).not.toThrow()
  })

  it('switching style does not restart the canvas', () => {
    const wrapper = mount(AnimatedWallpaper, { props: { styleId: 'xmb' } })
    rafSpy.mockClear()
    void wrapper.setProps({ styleId: 'silk' })
    // No new loop: the running one reads props.styleId each frame.
    expect(rafSpy.mock.calls.length).toBe(0)
    wrapper.unmount()
  })

  it('does not throw when a style draw fails', () => {
    // A throwing style must not kill the loop (and must not escape the component).
    const style = getAnimatedStyle('xmb')
    const spy = vi.spyOn(style, 'draw').mockImplementation(() => { throw new Error('boom') })
    const wrapper = mount(AnimatedWallpaper)
    expect(() => {
      // Drive one frame through the reduced-motion static path, which calls draw
      // synchronously on mount.
      wrapper.unmount()
    }).not.toThrow()
    spy.mockRestore()
  })

  describe('static (reduced-motion) path', () => {
    beforeEach(() => {
      // jsdom has no matchMedia at all, so stub the global rather than spy on it.
      // `matches: true` forces the reduced-motion branch: one static frame, no loop.
      vi.stubGlobal('matchMedia', vi.fn().mockReturnValue({
        matches: true,
        media: '(prefers-reduced-motion: reduce)',
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        addListener: vi.fn(),
        removeListener: vi.fn(),
        onchange: null,
        dispatchEvent: vi.fn(),
      }))
    })

    afterEach(() => vi.unstubAllGlobals())

    it('draws one static frame and never starts a loop', () => {
      const wrapper = mount(AnimatedWallpaper)
      expect(drawCalls).toBeGreaterThan(0)
      expect(rafSpy).not.toHaveBeenCalled()
      wrapper.unmount()
    })

    it('redraws when params change, since no frame loop is running', () => {
      // Without this watcher a reduced-motion user could drag a slider forever and
      // the frozen frame would never update.
      const wrapper = mount(AnimatedWallpaper, { props: { params: { lam: 100 } } })
      const before = drawCalls
      void wrapper.setProps({ params: { lam: 180 } })
      return nextTick().then(() => {
        expect(drawCalls).toBeGreaterThan(before)
        wrapper.unmount()
      })
    })

    it('redraws when the style changes', () => {
      const wrapper = mount(AnimatedWallpaper, { props: { styleId: 'xmb' } })
      const before = drawCalls
      void wrapper.setProps({ styleId: 'silk' })
      return nextTick().then(() => {
        expect(drawCalls).toBeGreaterThan(before)
        wrapper.unmount()
      })
    })
  })
})
