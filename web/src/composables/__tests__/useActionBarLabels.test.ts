import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent, nextTick, ref } from 'vue'
import { useActionBarLabels } from '@/composables/useActionBarLabels'

const mockObserve = vi.fn()
const mockDisconnect = vi.fn()
vi.stubGlobal(
  'ResizeObserver',
  class {
    observe = mockObserve
    disconnect = mockDisconnect
  },
)

function mountBar(opts: Parameters<typeof useActionBarLabels>[1] = {}) {
  let api: ReturnType<typeof useActionBarLabels> | null = null
  const Comp = defineComponent({
    template: '<div ref="barRef" class="bar"><span class="action-label">Reply</span></div>',
    setup() {
      const barRef = ref<HTMLElement | null>(null)
      api = useActionBarLabels(() => barRef.value, opts)
      return { barRef, ...api }
    },
  })
  const wrapper = mount(Comp)
  const el = wrapper.element as HTMLElement
  return {
    wrapper,
    el,
    api: api as unknown as ReturnType<typeof useActionBarLabels>,
    /**
     * jsdom has no layout, so both the available width and the width the row
     * needs are supplied here. `needed` is reported through the child's rect —
     * that is what the composable reads (scrollWidth is clamped to clientWidth
     * and cannot express spare room).
     */
    setSizes(needed: number, available: number) {
      Object.defineProperty(el, 'clientWidth', { value: available, configurable: true })
      const child = el.firstElementChild as HTMLElement
      child.getBoundingClientRect = () =>
        ({ left: 0, right: needed, width: needed, top: 0, bottom: 0, height: 0, x: 0, y: 0, toJSON: () => ({}) }) as DOMRect
    },
  }
}

afterEach(() => {
  vi.useRealTimers()
  vi.clearAllMocks()
})

describe('useActionBarLabels', () => {
  it('shows the labels when the row can hold them', async () => {
    const { api, setSizes } = mountBar()
    setSizes(200, 300)
    api.measure()
    await nextTick()
    expect(api.showLabels.value).toBe(true)
  })

  it('drops the labels when the row overflows', async () => {
    const { api, setSizes } = mountBar()
    setSizes(320, 300)
    api.measure()
    await nextTick()
    expect(api.showLabels.value).toBe(false)
  })

  it('tolerates sub-pixel rounding (2px) instead of hiding the labels', async () => {
    const { api, setSizes } = mountBar()
    // Labels are already on; a row that fits EXACTLY can report one pixel of
    // overflow, and that alone must not hide them.
    setSizes(200, 300)
    api.measure()
    setSizes(301, 300)
    api.measure()
    await nextTick()
    expect(api.showLabels.value).toBe(true)
  })

  it('needs real spare room before turning the labels back on', async () => {
    // The dead zone: without it a bar dragged across the boundary flips at
    // almost every pixel (on → 2px narrower → off → 2px wider → on).
    const { api, setSizes } = mountBar()
    setSizes(320, 300)
    api.measure()
    await nextTick()
    expect(api.showLabels.value).toBe(false)

    setSizes(290, 300) // 10px spare: still not enough
    api.measure()
    await nextTick()
    expect(api.showLabels.value).toBe(false)

    setSizes(280, 300) // 20px spare: on
    api.measure()
    await nextTick()
    expect(api.showLabels.value).toBe(true)

    setSizes(299, 300) // 1px spare: stays on (only overflow turns them off)
    api.measure()
    await nextTick()
    expect(api.showLabels.value).toBe(true)
  })

  it('assumes there is room before the row is laid out', async () => {
    // A hidden pane (or jsdom) measures 0 wide; guessing "no room" would show a
    // flash of icon-only buttons the moment the pane appears.
    const { api, setSizes } = mountBar()
    setSizes(0, 0)
    api.measure()
    await nextTick()
    expect(api.showLabels.value).toBe(true)
  })

  it('forces the labels on for the measurement itself', () => {
    // The whole trick: measure with `.measure-labels` applied, otherwise the
    // label-less layout always looks like it fits.
    const { api, el, setSizes } = mountBar()
    setSizes(100, 300)
    let hadMeasureClass = false
    const child = el.firstElementChild as HTMLElement
    child.getBoundingClientRect = () => {
      hadMeasureClass = el.classList.contains('measure-labels')
      return { left: 0, right: 100, width: 100, top: 0, bottom: 0, height: 0, x: 0, y: 0, toJSON: () => ({}) } as DOMRect
    }
    api.measure()
    expect(hadMeasureClass).toBe(true)
    // ...and it is removed again, so the temporary state never paints.
    expect(el.classList.contains('measure-labels')).toBe(false)
  })

  it('runs the caller’s follow-up after every measurement', () => {
    const onMeasured = vi.fn()
    const { api } = mountBar({ onMeasured })
    // Mount already measured once; count the explicit ones from here.
    onMeasured.mockClear()
    api.measure()
    expect(onMeasured).toHaveBeenCalledTimes(1)
  })

  it('debounces size changes instead of measuring on every frame', () => {
    vi.useFakeTimers()
    const onMeasured = vi.fn()
    const { api } = mountBar({ onMeasured, debounceMs: 50 })
    onMeasured.mockClear()

    api.scheduleMeasure()
    api.scheduleMeasure()
    api.scheduleMeasure()
    expect(onMeasured).not.toHaveBeenCalled()

    vi.advanceTimersByTime(49)
    expect(onMeasured).not.toHaveBeenCalled()
    vi.advanceTimersByTime(1)
    expect(onMeasured).toHaveBeenCalledTimes(1)
  })

  it('measures once on mount and observes the bar', () => {
    const onMeasured = vi.fn()
    mountBar({ onMeasured })
    expect(onMeasured).toHaveBeenCalledTimes(1)
    expect(mockObserve).toHaveBeenCalledTimes(1)
  })

  it('stops observing when the bar unmounts', () => {
    const { wrapper } = mountBar()
    wrapper.unmount()
    expect(mockDisconnect).toHaveBeenCalledTimes(1)
  })
})
