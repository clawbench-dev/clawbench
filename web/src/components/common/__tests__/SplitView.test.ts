import { describe, expect, it, vi, afterEach } from 'vitest'
import { mount, VueWrapper } from '@vue/test-utils'
import { nextTick } from 'vue'
import SplitView from '@/components/common/SplitView.vue'

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

function mountSplit(props = {}) {
  return mount(SplitView, {
    props,
    slots: {
      left: '<div class="pane-left">L</div>',
      right: '<div class="pane-right">R</div>',
    },
    attachTo: document.body,
  })
}

let wrapper: VueWrapper | null = null
afterEach(() => {
  wrapper?.unmount()
  wrapper = null
  vi.restoreAllMocks()
})

describe('SplitView', () => {
  it('enabled=false: no divider, panes render inline', () => {
    wrapper = mountSplit({ enabled: false })
    expect(wrapper.find('.split-view__divider').exists()).toBe(false)
    expect(wrapper.find('.pane-left').text()).toBe('L')
    expect(wrapper.find('.pane-right').text()).toBe('R')
  })

  it('enabled=true: renders divider and left width follows ratio', async () => {
    wrapper = mountSplit({ enabled: true, ratio: 0.4 })
    expect(wrapper.find('.split-view__divider').exists()).toBe(true)
    const left = wrapper.find('.split-view__left')
    await nextTick()
    expect(left.attributes('style')).toContain('width: 40%')
  })

  it('emits update:ratio on divider drag, clamped to min widths', async () => {
    wrapper = mountSplit({ enabled: true, ratio: 0.5 })
    const divider = wrapper.find('.split-view__divider').element as HTMLElement
    divider.setPointerCapture = vi.fn()
    divider.releasePointerCapture = vi.fn()

    Object.defineProperty(wrapper.find('.split-view').element, 'getBoundingClientRect', {
      configurable: true,
      value: () => ({ left: 0, width: 1000, top: 0, bottom: 0, height: 600, right: 1000, x: 0, y: 0, toJSON() {} }),
    })

    divider.dispatchEvent(new PointerEvent('pointerdown', { pointerId: 1, button: 0, bubbles: true, clientX: 300 }))
    window.dispatchEvent(new PointerEvent('pointermove', { pointerId: 1, bubbles: true, clientX: 100 }))
    window.dispatchEvent(new PointerEvent('pointerup', { pointerId: 1, bubbles: true }))

    const emitted = wrapper.emitted('update:ratio') as Array<Array<number>>
    expect(emitted).toBeTruthy()
    // 100px / 1000 = 0.1, clamped up to 320/1000 = 0.32
    expect(emitted[emitted.length - 1][0]).toBeCloseTo(0.32)
  })
})
