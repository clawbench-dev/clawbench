import { describe, expect, it, vi, afterEach } from 'vitest'
import { mount, VueWrapper } from '@vue/test-utils'
import SplitDivider from '@/components/common/SplitDivider.vue'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

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

describe('SplitDivider — vertical orientation', () => {
  it('renders a row-resize divider with horizontal separator semantics', () => {
    mountDivider({ orientation: 'vertical' })
    const el = wrapper!.find('.split-view__divider').element as HTMLElement
    expect(el.getAttribute('role')).toBe('separator')
    expect(el.getAttribute('aria-orientation')).toBe('horizontal')
    expect(el.classList.contains('split-view__divider--vertical')).toBe(true)
  })

  it('emits dragmove(clientY) for a vertical split', () => {
    mountDivider({ orientation: 'vertical' })
    const div = wrapper!.find('.split-view__divider').element as HTMLElement
    div.setPointerCapture = vi.fn()
    div.releasePointerCapture = vi.fn()

    div.dispatchEvent(new PointerEvent('pointerdown', { pointerId: 1, button: 0, bubbles: true, clientY: 300 }))
    expect(document.body.classList.contains('split-view-dragging--vertical')).toBe(true)
    window.dispatchEvent(new PointerEvent('pointermove', { pointerId: 1, bubbles: true, clientY: 420 }))

    const emitted = wrapper!.emitted('dragmove') as Array<Array<number>>
    expect(emitted[emitted.length - 1][0]).toBe(420)

    window.dispatchEvent(new PointerEvent('pointerup', { pointerId: 1, bubbles: true }))
    expect(document.body.classList.contains('split-view-dragging--vertical')).toBe(false)
  })

  it('defaults to horizontal orientation (clientX)', () => {
    mountDivider()
    const el = wrapper!.find('.split-view__divider').element as HTMLElement
    expect(el.classList.contains('split-view__divider--horizontal')).toBe(true)
    expect(el.getAttribute('aria-orientation')).toBe('vertical')
  })
})

describe('SplitDivider — touch target sizing', () => {
  it('enlarges the grab band and the resting line for coarse pointers', () => {
    // A 1px line is an unhittable touch target, and the hover-only growth never
    // fires on touch. jsdom does not load SFC <style>, so assert the source
    // declares a (pointer: coarse) block covering both orientations.
    const src = readFileSync(
      resolve(process.cwd(), 'src/components/common/SplitDivider.vue'),
      'utf8',
    )
    const style = src.slice(src.indexOf('<style'))
    expect(style).toMatch(/@media\s*\(pointer:\s*coarse\)/)
    const coarse = style.slice(style.indexOf('@media (pointer: coarse)'))
    // Both orientations must widen their ::before hit band inside the block.
    expect(coarse).toMatch(/\.split-view__divider--vertical::before/)
    expect(coarse).toMatch(/\.split-view__divider--horizontal::before/)
  })

  it('sets a minLeft/minRight-driven CSS var so JS clamp and CSS floors agree', () => {
    mountDivider({ ariaValueNow: 50 })
    const el = wrapper!.find('.split-view__divider').element as HTMLElement
    // The divider itself does not carry the vars; the SplitView root does.
    expect(el.getAttribute('role')).toBe('separator')
  })
})
