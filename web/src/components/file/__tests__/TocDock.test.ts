import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent, nextTick } from 'vue'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import TocDock from '@/components/file/TocDock.vue'

// ── Mocks ──

// Mutable width sink that stands in for the preference module
const { setWidth, tocDockWidth, toggleSide } = vi.hoisted(() => {
  const tocDockWidth = { value: 260 }
  return {
    tocDockWidth,
    setWidth: vi.fn((w: number) => { tocDockWidth.value = w }),
    toggleSide: vi.fn(),
  }
})

vi.mock('@/composables/useTocDockPreference', () => ({
  useTocDockPreference: () => ({
    tocDockWidth,
    setWidth,
    toggleSide,
  }),
}))

vi.mock('@/components/TocPanel.vue', () => ({
  default: defineComponent({
    name: 'TocPanel',
    props: { file: Object, pdfOutline: Array },
    emits: ['jump', 'jumpPage'],
    template: '<div class="toc-panel-stub"><button class="toc-jump-stub" @click="$emit(\'jump\', 12)">jump</button></div>',
  }),
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (k: string) => k }),
}))

function mountDock(props: Record<string, any> = {}) {
  return mount(TocDock, {
    props: {
      open: true,
      file: { name: 'readme.md', path: '/readme.md' },
      pdfOutline: [],
      ...props,
    },
    attachTo: document.body,
  })
}

describe('TocDock', () => {
  beforeEach(() => {
    setWidth.mockClear()
    toggleSide.mockClear()
    tocDockWidth.value = 260
  })

  it('renders the dock container with the inline TocPanel when open', () => {
    const wrapper = mountDock()
    expect(wrapper.find('.toc-dock').exists()).toBe(true)
    expect(wrapper.find('.toc-panel-stub').exists()).toBe(true)
  })

  it('does NOT render the BottomSheet-backed TocDrawer (inline + popup bug)', () => {
    const wrapper = mountDock()
    // Regression: the dock must use the pure-content TocPanel, never the
    // BottomSheet-backed TocDrawer — otherwise the bottom-sheet popup would
    // appear alongside the inline dock.
    expect(wrapper.findComponent({ name: 'TocDrawer' }).exists()).toBe(false)
    expect(wrapper.find('.bottom-sheet').exists()).toBe(false)
    expect(wrapper.find('.toc-panel-stub').exists()).toBe(true)
  })

  it('passes the file and pdfOutline to TocPanel', () => {
    const wrapper = mountDock({ pdfOutline: [{ id: 'p1', text: 'Page 1', level: 1, line: 1 }] })
    const panel = wrapper.findComponent({ name: 'TocPanel' })
    expect(panel.props('file')).toMatchObject({ name: 'readme.md' })
    expect(panel.props('pdfOutline')).toEqual([{ id: 'p1', text: 'Page 1', level: 1, line: 1 }])
  })

  it('forwards jump/jumpPage events from TocPanel without closing (docked stays open)', () => {
    const wrapper = mountDock()
    const panel = wrapper.findComponent({ name: 'TocPanel' })
    panel.vm.$emit('jump', 12)
    panel.vm.$emit('jumpPage', 3)
    expect(wrapper.emitted('jump')).toEqual([[12, undefined]])
    expect(wrapper.emitted('jumpPage')).toEqual([[3]])
    // Docked mode keeps the panel open after jumping to an item.
    expect(wrapper.emitted('close')).toBeFalsy()
  })

  it('emits close when the dock close button is clicked', async () => {
    const wrapper = mountDock()
    await wrapper.find('.toc-dock-close').trigger('click')
    expect(wrapper.emitted('close')).toBeTruthy()
  })

  it('toggles the dock side when the side-toggle button is clicked', async () => {
    const wrapper = mountDock({ side: 'left' })
    const btn = wrapper.find('.toc-dock-side-toggle')
    expect(btn.exists()).toBe(true)
    await btn.trigger('click')
    expect(toggleSide).toHaveBeenCalledTimes(1)
  })

  it('applies the dock width from preference', () => {
    tocDockWidth.value = 320
    const wrapper = mountDock()
    const dock = wrapper.find('.toc-dock')
    expect(dock.attributes('style')).toContain('width: 320px')
  })

  it('constrains the dock to the min/max width range via CSS', () => {
    const wrapper = mountDock()
    const dock = wrapper.find('.toc-dock')
    expect(dock.attributes('style')).toContain('width: 260px')
    // CSS bounds act as the visual clamp; the numeric clamp lives in the
    // preference module (covered by useTocDockPreference tests).
    expect(getComputedStyle(dock.element).maxWidth).toBe('400px')
    expect(getComputedStyle(dock.element).minWidth).toBe('200px')
  })

  it('drags RIGHT to narrow the dock (divider follows pointer on left edge)', async () => {
    const wrapper = mountDock()
    const divider = wrapper.find('.toc-dock-divider')
    pressDivider(divider, 300)

    // Press at clientX=300, drag right to 340 (+40): the dock's left edge
    // follows the pointer rightward, narrowing the dock 260 → 220.
    window.dispatchEvent(new PointerEvent('pointermove', { pointerId: 1, bubbles: true, clientX: 340 }))
    await nextTick()

    window.dispatchEvent(new PointerEvent('pointerup', { pointerId: 1, bubbles: true }))
    await nextTick()

    expect(setWidth).toHaveBeenCalled()
    const calledWidth = setWidth.mock.calls[0][0]
    expect(calledWidth).toBe(220)
  })

  it('drags LEFT to widen the dock', async () => {
    const wrapper = mountDock()
    const divider = wrapper.find('.toc-dock-divider')
    pressDivider(divider, 400)

    // Press at clientX=400, drag left to 350 (−50): the dock's left edge moves
    // left, widening the dock 260 → 310.
    window.dispatchEvent(new PointerEvent('pointermove', { pointerId: 1, bubbles: true, clientX: 350 }))
    await nextTick()

    window.dispatchEvent(new PointerEvent('pointerup', { pointerId: 1, bubbles: true }))
    await nextTick()

    expect(setWidth).toHaveBeenCalled()
    expect(setWidth.mock.calls[0][0]).toBe(310)
  })

  it('defaults to the right side with the right-edge divider styling', () => {
    const wrapper = mountDock()
    const dock = wrapper.find('.toc-dock')
    expect(dock.classes()).toContain('toc-dock--right')
  })

  it('adds the left-side class when side="left"', () => {
    const wrapper = mountDock({ side: 'left' })
    const dock = wrapper.find('.toc-dock')
    expect(dock.classes()).toContain('toc-dock--left')
    expect(dock.classes()).not.toContain('toc-dock--right')
  })

  it('drags LEFT to narrow the left-side dock (divider follows pointer on right edge)', async () => {
    const wrapper = mountDock({ side: 'left' })
    const divider = wrapper.find('.toc-dock-divider')
    pressDivider(divider, 300)

    // Press at clientX=300, drag left to 260 (−40): the dock's RIGHT edge
    // follows the pointer leftward, narrowing the dock 260 → 220.
    window.dispatchEvent(new PointerEvent('pointermove', { pointerId: 1, bubbles: true, clientX: 260 }))
    await nextTick()

    window.dispatchEvent(new PointerEvent('pointerup', { pointerId: 1, bubbles: true }))
    await nextTick()

    expect(setWidth).toHaveBeenCalled()
    const calledWidth = setWidth.mock.calls[0][0]
    expect(calledWidth).toBe(220)
  })

  it('drags RIGHT to widen the left-side dock', async () => {
    const wrapper = mountDock({ side: 'left' })
    const divider = wrapper.find('.toc-dock-divider')
    pressDivider(divider, 200)

    // Press at clientX=200, drag right to 250 (+50): the dock's right edge moves
    // right, widening the dock 260 → 310.
    window.dispatchEvent(new PointerEvent('pointermove', { pointerId: 1, bubbles: true, clientX: 250 }))
    await nextTick()

    window.dispatchEvent(new PointerEvent('pointerup', { pointerId: 1, bubbles: true }))
    await nextTick()

    expect(setWidth).toHaveBeenCalled()
    expect(setWidth.mock.calls[0][0]).toBe(310)
  })

  it('ignores pointermove while not dragging', async () => {
    const wrapper = mountDock()
    window.dispatchEvent(new PointerEvent('pointermove', { pointerId: 1, bubbles: true, clientX: 340 }))
    await nextTick()
    expect(setWidth).not.toHaveBeenCalled()
  })

  it('cleans up drag listeners on unmount while dragging', async () => {
    const wrapper = mountDock()
    const divider = wrapper.find('.toc-dock-divider')
    pressDivider(divider, 300)
    await nextTick()
    expect(document.body.classList.contains('toc-dock-resizing')).toBe(true)

    // Unmount mid-drag: should clean up the body class and listeners.
    wrapper.unmount()
    expect(document.body.classList.contains('toc-dock-resizing')).toBe(false)
  })
})

describe('TocDock — drag highlight survives touch', () => {
  // Regression: the expanded highlight used to be driven by `:active` alone.
  // On touch, `setPointerCapture` makes the browser drop `:active`, so a fast
  // swipe showed no highlight while a held press did. The component now tracks
  // the pointer session in a `dragging` ref and styles it too.
  it('marks the divider as dragging for the whole pointer session', async () => {
    const wrapper = mountDock()
    const divider = wrapper.find('.toc-dock-divider')
    const el = divider.element as HTMLElement

    expect(el.classList.contains('resize-divider--expanded')).toBe(false)

    pressDivider(divider, 300)
    await nextTick()
    expect(el.classList.contains('resize-divider--expanded')).toBe(true)

    // Still marked mid-drag — the part `:active` failed to guarantee.
    window.dispatchEvent(new PointerEvent('pointermove', { pointerId: 1, bubbles: true, clientX: 340 }))
    await nextTick()
    expect(el.classList.contains('resize-divider--expanded')).toBe(true)

    window.dispatchEvent(new PointerEvent('pointerup', { pointerId: 1, bubbles: true }))
    await nextTick()
    expect(el.classList.contains('resize-divider--expanded')).toBe(false)
  })

  it('clears the dragging mark on pointercancel', async () => {
    const wrapper = mountDock()
    const divider = wrapper.find('.toc-dock-divider')
    const el = divider.element as HTMLElement
    pressDivider(divider, 300)
    await nextTick()
    expect(el.classList.contains('resize-divider--expanded')).toBe(true)
    window.dispatchEvent(new PointerEvent('pointercancel', { pointerId: 1, bubbles: true }))
    await nextTick()
    expect(el.classList.contains('resize-divider--expanded')).toBe(false)
  })

  it('styles the expanded host band for :active, --expanded and hover', () => {
    // The host geometry is TocDock's own (the line rules are shared — see
    // resizeDivider.css.test.ts). Styling only `:active` is the bug this guards
    // against: touch loses `:active` mid-swipe.
    const src = readSource('file/TocDock.vue')
    const style = src.slice(src.indexOf('<style'))
    expect(style).toMatch(/\.toc-dock-divider:active,\s*\.toc-dock-divider\.resize-divider--expanded/)
    // Left-docked shift must apply while dragging, or the highlight sits off-centre.
    expect(style).toMatch(/\.toc-dock--left \.toc-dock-divider\.resize-divider--expanded/)
  })

  it('consumes the shared line element instead of its own', () => {
    // The inner line moved to assets/resize-divider.css so both dividers share
    // one definition. A leftover local class would silently reintroduce the
    // duplicated (and drift-prone) copy.
    const wrapper = mountDock()
    const el = wrapper.find('.toc-dock-divider').element as HTMLElement
    expect(wrapper.find('.resize-divider__line').exists()).toBe(true)
    expect(wrapper.find('.toc-dock-divider__line').exists()).toBe(false)
    expect(el.classList.contains('resize-divider')).toBe(true)
  })
})

/** Dispatch pointerdown on the divider with pointer-capture mocked (jsdom lacks it). */
function pressDivider(divider: { element: Element | null }, clientX: number) {
  const el = divider.element as HTMLElement
  el.setPointerCapture = vi.fn()
  el.releasePointerCapture = vi.fn()
  el.dispatchEvent(new PointerEvent('pointerdown', { pointerId: 1, button: 0, bubbles: true, clientX }))
}

/** Read an SFC source; cwd differs between a bare vitest run and vitest-run.sh. */
function readSource(relPath: string): string {
  for (const base of [process.cwd(), resolve(process.cwd(), 'web')]) {
    try {
      return readFileSync(resolve(base, 'src/components/' + relPath), 'utf8')
    } catch {
      // try the next candidate
    }
  }
  throw new Error(relPath + ' not found from cwd: ' + process.cwd())
}
