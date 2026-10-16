import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { useVisualViewportInset } from '../useVisualViewportInset'

/**
 * The selection toolbar is `position: fixed`, so its offsets are measured from
 * the LAYOUT viewport. On a mobile BROWSER the soft keyboard shrinks only the
 * VISUAL viewport, leaving the layout bottom (and therefore the bar) behind the
 * keyboard — this composable measures how much is hidden so the bar can be
 * pulled back into view.
 *
 * jsdom has no `visualViewport`, so these tests install a stub. The stub is a
 * SINGLE mutable object, mirroring the real API (one visualViewport whose
 * properties change over time) — replacing the object would orphan the
 * listeners the composable registered on it, which is not something a browser
 * ever does.
 */
describe('useVisualViewportInset', () => {
  const { bottomInset, topInset, viewportVersion, install, uninstall } = useVisualViewportInset()

  let listeners: Record<string, Array<() => void>>
  let vvState: { innerWidth: number; innerHeight: number; height: number; offsetTop: number; scale: number }

  function installStubViewport() {
    listeners = { resize: [], scroll: [] }
    vvState = { innerWidth: 400, innerHeight: 800, height: 800, offsetTop: 0, scale: 1 }
    const vv = {
      get height() { return vvState.height },
      get offsetTop() { return vvState.offsetTop },
      get scale() { return vvState.scale },
      get width() { return vvState.innerWidth },
      addEventListener: (t: string, cb: () => void) => { (listeners[t] ||= []).push(cb) },
      removeEventListener: (t: string, cb: () => void) => {
        listeners[t] = (listeners[t] || []).filter((f) => f !== cb)
      },
    }
    Object.defineProperty(window, 'visualViewport', { value: vv, configurable: true })
    Object.defineProperty(window, 'innerHeight', {
      get: () => vvState.innerHeight,
      configurable: true,
    })
    Object.defineProperty(window, 'innerWidth', {
      get: () => vvState.innerWidth,
      configurable: true,
    })
  }

  /** Simulate a viewport change: update the metrics, then dispatch (or not). */
  function setViewport(next: Partial<typeof vvState>, fire?: 'resize' | 'scroll') {
    Object.assign(vvState, next)
    if (fire) for (const cb of listeners[fire] || []) cb()
  }

  beforeEach(() => {
    vi.useFakeTimers()
    installStubViewport()
  })

  afterEach(() => {
    uninstall()
    vi.useRealTimers()
    // @ts-expect-error — remove the stub so other suites see the jsdom default
    delete window.visualViewport
  })

  it('reports zero when the visual viewport fills the layout viewport', () => {
    install()
    expect(bottomInset.value).toBe(0)
    expect(topInset.value).toBe(0)
  })

  it('reports the hidden bottom when the keyboard shrinks only the visual viewport', () => {
    install()
    setViewport({ height: 500 }, 'resize')
    expect(bottomInset.value).toBe(300)
  })

  /**
   * Android uses adjustResize, which shrinks the LAYOUT viewport too. In that
   * mode `position: fixed` is already above the keyboard, so the inset must stay
   * 0 — reporting a value there would double-compensate and lift the bar off the
   * keyboard for no reason.
   */
  it('reports zero under adjustResize (both viewports shrink together)', () => {
    install()
    setViewport({ innerHeight: 500, height: 500 }, 'resize')
    expect(bottomInset.value).toBe(0)
  })

  /**
   * The raw difference is NOT keyboard-specific: a classic desktop horizontal
   * scrollbar makes it ~15px. Treating that as a keyboard would move the toolbar
   * away from its selection on every desktop machine.
   */
  it('ignores sub-threshold differences (desktop scrollbar / browser chrome)', () => {
    install()
    setViewport({ height: 785 }, 'resize')
    expect(bottomInset.value).toBe(0)
  })

  it('tracks the top offset when the page scrolls to reveal the field', () => {
    install()
    setViewport({ height: 500, offsetTop: 120 }, 'scroll')
    expect(topInset.value).toBe(120)
    // innerHeight - (offsetTop + height) = 800 - 620 = 180
    expect(bottomInset.value).toBe(180)
  })

  /**
   * Android WebViews do not reliably dispatch resize/scroll when the keyboard
   * opens, so the poll is the only signal that reaches detection there. Without
   * it the bar would never re-position on the native app.
   */
  it('detects the keyboard through the poll when no event fires', () => {
    install()
    expect(bottomInset.value).toBe(0)

    // Metrics change but NO event is dispatched — only time passes.
    setViewport({ height: 500 })
    vi.advanceTimersByTime(300)
    expect(bottomInset.value).toBe(300)
  })

  it('clears the values on uninstall', () => {
    install()
    setViewport({ height: 500 }, 'resize')
    expect(bottomInset.value).toBe(300)

    uninstall()
    expect(bottomInset.value).toBe(0)
    expect(topInset.value).toBe(0)
  })

  it('is idempotent on install (no duplicate listeners)', () => {
    install()
    install()
    // Asserting the LISTENER COUNT, not the resulting value: a second install
    // that re-registered would just run measure() twice to the same number, so a
    // value assertion would pass with the guard deleted.
    expect(listeners.resize).toHaveLength(1)
    expect(listeners.scroll).toHaveLength(1)
  })

  it('removes its listeners on uninstall', () => {
    install()
    expect(listeners.resize).toHaveLength(1)
    uninstall()
    expect(listeners.resize).toHaveLength(0)
    expect(listeners.scroll).toHaveLength(0)
  })

  /**
   * Pinch-zoom shrinks `visualViewport.height` (height = innerHeight / scale),
   * which is indistinguishable from a keyboard by the height math alone. On iOS
   * — which ignores `user-scalable=no` — zooming in to read small text and then
   * selecting would otherwise yank the bar into the top half of the screen.
   */
  it('ignores a pinch-zoomed visual viewport', () => {
    install()
    // scale 2 → height halves, exactly like a keyboard would.
    setViewport({ height: 400, scale: 2 }, 'resize')
    expect(bottomInset.value).toBe(0)
  })

  it('degrades to zero without a visualViewport (old engines)', () => {
    // @ts-expect-error — simulate an engine with no visualViewport
    delete window.visualViewport
    install()
    expect(bottomInset.value).toBe(0)
    expect(topInset.value).toBe(0)
  })

  /**
   * Android's adjustResize shrinks the LAYOUT viewport, so the inset correctly
   * stays 0 — but `getZoomedViewport()` (which the toolbar positions against)
   * reads `window.innerHeight` NON-reactively. Without a version counter that
   * changes when the layout height changes, a `bottom`-anchored bar would keep an
   * offset measured against the old, taller viewport and drift off-screen.
   */
  describe('viewportVersion', () => {
    /**
     * The decisive Android case, isolated: the LAYOUT height changes while every
     * visual metric stays put, so the inset does not move and only the layout
     * signature can detect it. (Changing both together, as an earlier version of
     * this test did, passes even when the layout term is dropped from the
     * signature — a tautology.)
     */
    it('changes when only the layout height moves (Android adjustResize)', () => {
      install()
      const before = viewportVersion.value

      // vv.height is deliberately NOT changed: this isolates the layout term.
      setViewport({ innerHeight: 500 }, 'resize')

      expect(viewportVersion.value).toBeGreaterThan(before)
    })

    it('changes when the layout viewport shrinks together with the visual one', () => {
      install()
      const before = viewportVersion.value

      setViewport({ innerHeight: 500, height: 500 }, 'resize')

      expect(bottomInset.value).toBe(0)
      expect(viewportVersion.value).toBeGreaterThan(before)
    })

    it('changes when only the layout width moves (rotation / window resize)', () => {
      install()
      const before = viewportVersion.value

      setViewport({ innerWidth: 500 }, 'resize')

      expect(viewportVersion.value).toBeGreaterThan(before)
    })

    it('changes for the visual-viewport keyboard case too', () => {
      install()
      const before = viewportVersion.value
      setViewport({ height: 500 }, 'resize')
      expect(viewportVersion.value).toBeGreaterThan(before)
    })

    /**
     * `measure()` runs on a 300ms poll. Bumping unconditionally would re-render
     * every consumer three times a second forever.
     */
    it('does not change when nothing moved', () => {
      install()
      const before = viewportVersion.value
      vi.advanceTimersByTime(1200) // four poll ticks, no metric change
      expect(viewportVersion.value).toBe(before)
    })

    it('changes on a pinch-zoom even though the inset is suppressed', () => {
      install()
      const before = viewportVersion.value
      setViewport({ height: 400, scale: 2 }, 'resize')
      expect(bottomInset.value).toBe(0)
      expect(viewportVersion.value).toBeGreaterThan(before)
    })
  })
})
