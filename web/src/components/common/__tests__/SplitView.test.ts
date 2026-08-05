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

// jsdom does not load SFC scoped `<style>` blocks, so getComputedStyle() returns
// the browser default ('block') for the disabled-mode wrappers. Inject the
// relevant rule so the disabled (non-active) wrappers resolve to display:contents.
const splitCss = document.createElement('style')
splitCss.textContent = `
.split-view:not(.split-view--active) .split-view__left,
.split-view:not(.split-view--active) .split-view__right { display: contents; }
`
document.head.appendChild(splitCss)

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

  it('enabled=false: wrappers are display:contents (no absolute overlay blocking pointer events)', () => {
    // Regression: if the disabled-mode wrappers stayed position:absolute, the
    // later one would overlay the visible pane and swallow all touch/scroll.
    wrapper = mountSplit({ enabled: false })
    const left = wrapper.find('.split-view__left').element as HTMLElement
    const right = wrapper.find('.split-view__right').element as HTMLElement
    expect(getComputedStyle(left).display).toBe('contents')
    expect(getComputedStyle(right).display).toBe('contents')
  })

  it('enabled=true: wrappers are positioned flex panes (not display:contents)', async () => {
    wrapper = mountSplit({ enabled: true, ratio: 0.5 })
    const left = wrapper.find('.split-view__left').element as HTMLElement
    const right = wrapper.find('.split-view__right').element as HTMLElement
    expect(getComputedStyle(left).display).not.toBe('contents')
    expect(getComputedStyle(right).display).not.toBe('contents')
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

  it('gutterSize prop drives the divider width via --split-gutter CSS var', () => {
    wrapper = mountSplit({ enabled: true, ratio: 0.5, gutterSize: 12 })
    const root = wrapper.find('.split-view').element as HTMLElement
    expect(root.style.getPropertyValue('--split-gutter')).toBe('12px')
  })

  it('aria-valuenow/min/max are on the same percentage scale', async () => {
    const rect = { left: 0, width: 1000, top: 0, bottom: 0, height: 600, right: 1000, x: 0, y: 0, toJSON() {} }
    vi.spyOn(Element.prototype, 'getBoundingClientRect').mockReturnValue(rect as DOMRect)
    wrapper = mountSplit({ enabled: true, ratio: 0.5 })
    await nextTick()
    const divider = wrapper.find('.split-view__divider').element as HTMLElement
    expect(divider.getAttribute('aria-valuenow')).toBe('50')
    expect(divider.getAttribute('aria-valuemin')).toBe('32')
    expect(divider.getAttribute('aria-valuemax')).toBe('68')
  })
})
