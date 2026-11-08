import { describe, expect, it, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent, h, nextTick } from 'vue'
import { useDesignWidthAtMost } from '@/share/useDesignWidth'
import { applyUIScale } from '@/utils/uiScale'

/**
 * The width test the share pages use for their "drop the optional bits" rules.
 *
 * A media query compares against the WINDOW's CSS width, which interface
 * scaling never changes — so at 1.33× a 640px window still claims 640px of
 * room while only ~480 design px are actually available. These tests pin the
 * two things that make the composable right: it divides by the applied scale,
 * and it re-evaluates on BOTH signals (window resize and the scale event,
 * because scaling does not fire `resize`).
 */

function mountProbe(maxDesignPx: number) {
  let matches: ReturnType<typeof useDesignWidthAtMost> | null = null
  const Probe = defineComponent({
    setup() {
      matches = useDesignWidthAtMost(maxDesignPx)
      return () => h('div')
    },
  })
  const wrapper = mount(Probe)
  return { wrapper, state: () => matches!.value }
}

function setViewport(width: number): void {
  Object.defineProperty(window, 'innerWidth', { value: width, configurable: true, writable: true })
}

afterEach(() => {
  document.documentElement.style.removeProperty('--ui-scale')
  document.documentElement.style.removeProperty('--input-line-height')
  setViewport(1024)
})

describe('useDesignWidthAtMost', () => {
  it('measures the window in design px, not CSS px', async () => {
    setViewport(640)
    const { state } = mountProbe(640)
    // At 100% the window is exactly at the threshold.
    expect(state()).toBe(true)

    // The same window at 150% has only ~427 design px of room, so a rule that
    // hides content below 640 design px must now fire. This is the whole point:
    // a media query would still report 640 and keep the content.
    applyUIScale(1.5)
    await nextTick()
    expect(state()).toBe(true)

    setViewport(800)
    applyUIScale(1)
    await nextTick()
    expect(state()).toBe(false)

    applyUIScale(1.5) // 800 / 1.5 = 533 design px < 640
    await nextTick()
    expect(state()).toBe(true)
  })

  it('re-evaluates on the scale event, which is not a resize', async () => {
    setViewport(800)
    const { state } = mountProbe(640)
    expect(state()).toBe(false)

    // `applyUIScale` dispatches UI_SCALE_EVENT and nothing else — no resize,
    // because the viewport genuinely did not change. Listening only to `resize`
    // would leave this false until the reader dragged the window.
    applyUIScale(1.5)
    await nextTick()
    expect(state()).toBe(true)
  })

  it('re-evaluates on a window resize', async () => {
    setViewport(900)
    const { state } = mountProbe(640)
    expect(state()).toBe(false)

    setViewport(500)
    window.dispatchEvent(new Event('resize'))
    await nextTick()
    expect(state()).toBe(true)
  })

  it('is a plain boolean at 100% for the boundary itself', async () => {
    // `<=`, not `<`: the dock's 520dp rule has always fired AT 520, and moving
    // the boundary by one pixel would silently change which phones get labels.
    setViewport(520)
    const { state } = mountProbe(520)
    expect(state()).toBe(true)

    setViewport(521)
    window.dispatchEvent(new Event('resize'))
    await nextTick()
    expect(state()).toBe(false)
  })

  it('stops listening once the component is gone', async () => {
    setViewport(900)
    const { wrapper, state } = mountProbe(640)
    expect(state()).toBe(false)

    wrapper.unmount()
    setViewport(400)
    window.dispatchEvent(new Event('resize'))
    await nextTick()
    // Unmounted: the value can no longer change (and no listener is leaked).
    expect(state()).toBe(false)
  })
})
