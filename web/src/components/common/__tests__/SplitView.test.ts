import { describe, expect, it, vi, afterEach } from 'vitest'
import { mount, VueWrapper } from '@vue/test-utils'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { defineComponent } from 'vue'
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

  it('collapsed=true (enabled): hides left pane and divider, right pane remains', () => {
    wrapper = mountSplit({ enabled: true, ratio: 0.4, collapsed: true })
    expect(wrapper.find('.split-view__divider').exists()).toBe(false)
    const left = wrapper.find('.split-view__left')
    expect(left.classes()).toContain('split-view__left--collapsed')
    // Left slot content is still mounted (slot hidden via wrapper), right visible
    expect(wrapper.find('.pane-right').exists()).toBe(true)
  })

  it('collapsed=true (disabled): ignored — normal split behavior', () => {
    wrapper = mountSplit({ enabled: false, collapsed: true })
    expect(wrapper.find('.split-view__divider').exists()).toBe(false)
    const left = wrapper.find('.split-view__left')
    expect(left.classes()).not.toContain('split-view__left--collapsed')
  })

  it('rightCollapsed=true (enabled): hides right pane and divider, left pane remains', () => {
    wrapper = mountSplit({ enabled: true, ratio: 0.4, rightCollapsed: true })
    expect(wrapper.find('.split-view__divider').exists()).toBe(false)
    const right = wrapper.find('.split-view__right')
    expect(right.classes()).toContain('split-view__right--collapsed')
    expect(wrapper.find('.pane-left').exists()).toBe(true)
  })

  it('rightCollapsed=true (enabled): left pane fills the full width (flex-grow, no % width)', () => {
    // Regression: a fixed percentage width would leave the right side blank —
    // the left pane must switch to flex-grow when the right pane is hidden.
    wrapper = mountSplit({ enabled: true, ratio: 0.4, rightCollapsed: true })
    const left = wrapper.find('.split-view__left')
    expect(left.attributes('style')).toContain('flex: 1 1 auto')
    expect(left.attributes('style')).not.toContain('width: 40%')
  })

  it('rightCollapsed=false (enabled): left pane keeps the percentage width', () => {
    wrapper = mountSplit({ enabled: true, ratio: 0.4 })
    const left = wrapper.find('.split-view__left')
    expect(left.attributes('style')).toContain('width: 40%')
  })

  it('rightCollapsed=true (disabled): ignored — normal split behavior', () => {
    wrapper = mountSplit({ enabled: false, rightCollapsed: true })
    const right = wrapper.find('.split-view__right')
    expect(right.classes()).not.toContain('split-view__right--collapsed')
  })

  it('rightCollapsed defaults to false (right pane visible)', () => {
    wrapper = mountSplit({ enabled: true, ratio: 0.5 })
    expect(wrapper.find('.split-view__right').classes()).not.toContain('split-view__right--collapsed')
    expect(wrapper.find('.split-view__divider').exists()).toBe(true)
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

describe('SplitView — vertical orientation', () => {
  function mountVertical(props = {}) {
    return mount(SplitView, {
      props: { enabled: true, orientation: 'vertical', ratio: 0.5, ...props },
      slots: {
        top: '<div class="pane-top">T</div>',
        bottom: '<div class="pane-bottom">B</div>',
      },
      attachTo: document.body,
    })
  }

  it('renders top/bottom slots and a vertical divider', () => {
    wrapper = mountVertical()
    expect(wrapper.find('.pane-top').text()).toBe('T')
    expect(wrapper.find('.pane-bottom').text()).toBe('B')
    expect(wrapper.find('.split-view').classes()).toContain('split-view--vertical')
    expect(wrapper.find('.split-view__divider--vertical').exists()).toBe(true)
  })

  it('first pane height follows the ratio (not width)', () => {
    wrapper = mountVertical({ ratio: 0.3 })
    const first = wrapper.find('.split-view__left')
    expect(first.attributes('style')).toContain('height: 30%')
    expect(first.attributes('style')).not.toContain('width: 30%')
  })

  it('divider reports aria-orientation=horizontal for a stacked split', () => {
    wrapper = mountVertical()
    const divider = wrapper.find('.split-view__divider')
    expect(divider.attributes('aria-orientation')).toBe('horizontal')
  })

  it('drags on clientY and clamps to min heights', async () => {
    wrapper = mountVertical({ ratio: 0.5, minLeft: 160, minRight: 160 })
    const divider = wrapper.find('.split-view__divider').element as HTMLElement
    divider.setPointerCapture = vi.fn()
    divider.releasePointerCapture = vi.fn()

    Object.defineProperty(wrapper.find('.split-view').element, 'getBoundingClientRect', {
      configurable: true,
      value: () => ({ left: 0, width: 800, top: 100, bottom: 700, height: 600, right: 800, x: 0, y: 100, toJSON() {} }),
    })

    divider.dispatchEvent(new PointerEvent('pointerdown', { pointerId: 1, button: 0, bubbles: true, clientY: 300 }))
    // Drag far up: clamped to the 160px min top pane => 160/600.
    window.dispatchEvent(new PointerEvent('pointermove', { pointerId: 1, bubbles: true, clientY: 0 }))
    window.dispatchEvent(new PointerEvent('pointerup', { pointerId: 1, bubbles: true }))

    const emitted = wrapper.emitted('update:ratio') as Array<Array<number>>
    expect(emitted).toBeTruthy()
    expect(emitted[emitted.length - 1][0]).toBeCloseTo(160 / 600, 3)
  })

  it('rightCollapsed (bottom hidden) makes the top pane fill via flex-grow', () => {
    wrapper = mountVertical({ ratio: 0.4, rightCollapsed: true })
    const first = wrapper.find('.split-view__left')
    expect(first.attributes('style')).toContain('flex: 1 1 auto')
    expect(first.attributes('style')).not.toContain('height: 40%')
    expect(wrapper.find('.split-view__divider').exists()).toBe(false)
  })

  it('collapsed hides the top pane', () => {
    wrapper = mountVertical({ collapsed: true })
    expect(wrapper.find('.split-view__left').classes()).toContain('split-view__left--collapsed')
    expect(wrapper.find('.split-view__divider').exists()).toBe(false)
  })
})

describe('SplitView — nested splits (file manager inside App split)', () => {
  it('scopes pane styling to its own children so a parent split cannot size a nested one', () => {
    // Regression: the file manager's vertical split is nested inside App's
    // horizontal split. With descendant selectors, the outer
    // `.split-view--horizontal ... .split-view__left { max-width: calc(100% - 320px) }`
    // also matched the INNER vertical pane, capping its width (696px pane
    // rendered at 375px). Every pane rule must use the child combinator.
    // jsdom does not load SFC <style>, so assert against the source.
    const src = readFileSync(
      resolve(process.cwd(), 'src/components/common/SplitView.vue'),
      'utf8',
    )
    const style = src.slice(src.indexOf('<style'))
    // Only SIZING rules are dangerous when unscoped: a leaked width/max-width
    // from the outer split would cap the inner pane. The base
    // `.split-view__left, .split-view__right { position:absolute; inset:0 }`
    // rule intentionally has no combinator (it is overridden for active splits
    // and sets no size), so it is excluded.
    const SIZING = /^(width|height|flex|flex-basis|flex-grow|flex-shrink|min-width|max-width|min-height|max-height)\s*:/
    const offenders: string[] = []
    let current: string | null = null
    for (const raw of style.split('\n')) {
      const line = raw.trim()
      if (line.endsWith('{')) {
        current = (line.includes('.split-view__left') || line.includes('.split-view__right')) && !line.includes('>')
          ? line
          : null
        continue
      }
      if (current && SIZING.test(line)) offenders.push(`${current} -> ${line}`)
      if (line === '}') current = null
    }
    // Any pane selector lacking `>` that sets a size is a descendant selector
    // that can leak onto a nested split's panes.
    expect(offenders).toEqual([])
  })

  it('renders two nested splits whose panes are each scoped to their own split', () => {
    const Nested = defineComponent({
      components: { SplitView },
      template: `
        <SplitView :enabled="true" orientation="horizontal" :ratio="0.5">
          <template #left>
            <SplitView :enabled="true" orientation="vertical" :ratio="0.5">
              <template #top><div class="inner-top">T</div></template>
              <template #bottom><div class="inner-bottom">B</div></template>
            </SplitView>
          </template>
          <template #right><div class="outer-right">R</div></template>
        </SplitView>
      `,
    })
    wrapper = mount(Nested, { attachTo: document.body })

    const outer = wrapper.find('.split-view--horizontal')
    const inner = wrapper.find('.split-view--vertical')
    expect(outer.exists()).toBe(true)
    expect(inner.exists()).toBe(true)
    // Each split owns exactly its own two panes — the inner split's panes must
    // be children of the inner split, not matched by the outer split's rules.
    expect(outer.element.querySelectorAll(':scope > .split-view__left').length).toBe(1)
    expect(inner.element.querySelectorAll(':scope > .split-view__left').length).toBe(1)
    expect(inner.element.querySelectorAll(':scope > .split-view__right').length).toBe(1)
    // The inner split lives inside the outer's left pane.
    expect(outer.element.querySelector(':scope > .split-view__left .split-view--vertical')).toBeTruthy()
    expect(wrapper.find('.inner-top').text()).toBe('T')
    expect(wrapper.find('.outer-right').text()).toBe('R')
  })
})

describe('SplitView — min sizes drive both JS clamp and CSS floors', () => {
  it('exposes minLeft/minRight as CSS vars on the root', () => {
    wrapper = mountSplit({ enabled: true, ratio: 0.5, minLeft: 120, minRight: 140 })
    const root = wrapper.find('.split-view').element as HTMLElement
    // JS clampRatio() and the CSS min/max floors must read the same numbers,
    // otherwise a small mobile minimum would be silently overridden by CSS.
    expect(root.style.getPropertyValue('--split-min-first')).toBe('120px')
    expect(root.style.getPropertyValue('--split-min-second')).toBe('140px')
  })

  it('pane sizing rules consume the vars (not hard-coded pixel values)', () => {
    const src = readFileSync(
      resolve(process.cwd(), 'src/components/common/SplitView.vue'),
      'utf8',
    )
    const style = src.slice(src.indexOf('<style'))
    // Every min/max sizing rule on a pane must reference the vars.
    expect(style).toMatch(/min-height:\s*var\(--split-min-first/)
    expect(style).toMatch(/min-height:\s*var\(--split-min-second/)
    expect(style).toMatch(/min-width:\s*var\(--split-min-first/)
    expect(style).toMatch(/min-width:\s*var\(--split-min-second/)
  })
})
