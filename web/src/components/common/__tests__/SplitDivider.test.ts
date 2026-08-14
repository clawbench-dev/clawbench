import { describe, expect, it, vi, afterEach } from 'vitest'
import { mount, VueWrapper } from '@vue/test-utils'
import SplitDivider from '@/components/common/SplitDivider.vue'

let wrapper: VueWrapper | null = null

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
  document.body.classList.remove('split-view-dragging')
  vi.restoreAllMocks()
})

function mountDivider(props = {}) {
  wrapper = mount(SplitDivider, { props })
  return wrapper
}

describe('SplitDivider', () => {
  it('renders the divider with gutter line and separator semantics', () => {
    mountDivider()
    const el = wrapper!.find('.split-view__divider').element as HTMLElement
    expect(el.getAttribute('role')).toBe('separator')
    expect(el.getAttribute('aria-orientation')).toBe('vertical')
    expect(wrapper!.find('.split-view__gutter-line').exists()).toBe(true)
  })

  it('applies aria-value attributes when provided', () => {
    mountDivider({ ariaValueNow: 50, ariaValueMin: 32, ariaValueMax: 68 })
    const el = wrapper!.find('.split-view__divider').element as HTMLElement
    expect(el.getAttribute('aria-valuenow')).toBe('50')
    expect(el.getAttribute('aria-valuemin')).toBe('32')
    expect(el.getAttribute('aria-valuemax')).toBe('68')
  })

  it('emits dragmove(clientX) and adds body dragging class while dragging', () => {
    mountDivider()
    const div = wrapper!.find('.split-view__divider').element as HTMLElement
    div.setPointerCapture = vi.fn()
    div.releasePointerCapture = vi.fn()

    div.dispatchEvent(new PointerEvent('pointerdown', { pointerId: 1, button: 0, bubbles: true, clientX: 300 }))
    expect(document.body.classList.contains('split-view-dragging')).toBe(true)

    window.dispatchEvent(new PointerEvent('pointermove', { pointerId: 1, bubbles: true, clientX: 120 }))
    const emitted = wrapper!.emitted('dragmove') as Array<Array<number>>
    expect(emitted).toBeTruthy()
    expect(emitted[emitted.length - 1][0]).toBe(120)

    window.dispatchEvent(new PointerEvent('pointerup', { pointerId: 1, bubbles: true }))
    expect(document.body.classList.contains('split-view-dragging')).toBe(false)
  })

  it('does not drag on non-primary button', () => {
    mountDivider()
    const div = wrapper!.find('.split-view__divider').element as HTMLElement
    div.setPointerCapture = vi.fn()

    div.dispatchEvent(new PointerEvent('pointerdown', { pointerId: 1, button: 2, bubbles: true, clientX: 300 }))
    window.dispatchEvent(new PointerEvent('pointermove', { pointerId: 1, bubbles: true, clientX: 120 }))
    expect(wrapper!.emitted('dragmove')).toBeUndefined()
    expect(document.body.classList.contains('split-view-dragging')).toBe(false)
  })

  it('cleans up body class on unmount during drag', () => {
    mountDivider()
    const div = wrapper!.find('.split-view__divider').element as HTMLElement
    div.setPointerCapture = vi.fn()
    div.dispatchEvent(new PointerEvent('pointerdown', { pointerId: 1, button: 0, bubbles: true, clientX: 300 }))
    expect(document.body.classList.contains('split-view-dragging')).toBe(true)
    wrapper!.unmount()
    wrapper = null
    expect(document.body.classList.contains('split-view-dragging')).toBe(false)
  })
})
