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
    setWidths(scrollWidth: number, clientWidth: number) {
      Object.defineProperty(el, 'scrollWidth', { value: scrollWidth, configurable: true })
      Object.defineProperty(el, 'clientWidth', { value: clientWidth, configurable: true })
    },
  }
}

afterEach(() => {
  vi.useRealTimers()
  vi.clearAllMocks()
})

describe('useActionBarLabels', () => {
  it('shows the labels when the row can hold them', async () => {
    const { api, setWidths } = mountBar()
    setWidths(200, 300)
    api.measure()
    await nextTick()
    expect(api.showLabels.value).toBe(true)
  })

  it('drops the labels when the row overflows', async () => {
    const { api, setWidths } = mountBar()
    setWidths(320, 300)
    api.measure()
    await nextTick()
    expect(api.showLabels.value).toBe(false)
  })

  it('tolerates sub-pixel rounding (2px) instead of hiding the labels', async () => {
    const { api, setWidths } = mountBar()
    // A row that fits EXACTLY can report one pixel of overflow.
    setWidths(301, 300)
    api.measure()
    await nextTick()
    expect(api.showLabels.value).toBe(true)
  })

  it('assumes there is room before the row is laid out', async () => {
    // A hidden pane (or jsdom) measures 0 wide; guessing "no room" would show a
    // flash of icon-only buttons the moment the pane appears.
    const { api, setWidths } = mountBar()
    setWidths(0, 0)
    api.measure()
    await nextTick()
    expect(api.showLabels.value).toBe(true)
  })

  it('forces the labels on for the measurement itself', () => {
    // The whole trick: measure with `.measure-labels` applied, otherwise the
    // label-less layout always looks like it fits.
    const { api, el } = mountBar()
    let hadMeasureClass = false
    Object.defineProperty(el, 'scrollWidth', {
      get: () => {
        hadMeasureClass = el.classList.contains('measure-labels')
        return 100
      },
      configurable: true,
    })
    Object.defineProperty(el, 'clientWidth', { value: 300, configurable: true })
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
