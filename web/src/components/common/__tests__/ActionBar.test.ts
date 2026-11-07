import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { defineComponent, h, nextTick, ref } from 'vue'
import ActionBar from '@/components/common/ActionBar.vue'

const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: { nav: { more: 'More' } } } })

/**
 * ActionBar is the single control behind every icon row: it decides the labels,
 * the folding (menu mode) and the drag/fade (scroll mode). jsdom has no layout,
 * so the widths are supplied by hand — the point of these tests is the
 * DECISION, which is exactly the part that used to be duplicated per bar.
 */

interface Sizes {
  /** Width the buttons need with labels OFF. */
  icon: number
  /** Width the buttons need with labels ON. */
  labelled: number
  /** Room the bar has. */
  available: number
}

function mountBar(opts: { mode?: 'menu' | 'scroll'; sizes?: Sizes; count?: number; alwaysMore?: boolean } = {}) {
  const count = opts.count ?? 3
  const sizes: Sizes = opts.sizes ?? { icon: 100, labelled: 200, available: 500 }

  const barRef = ref<{ measure: () => void } | null>(null)

  const Comp = defineComponent({
    render() {
      return h(
        ActionBar,
        { ref: barRef, mode: opts.mode ?? 'menu', alwaysMore: opts.alwaysMore },
        {
          default: () => Array.from({ length: count }, (_, i) =>
            h('button', { class: 'btn', title: `Action ${i + 1}` }, [
              h('span', { class: 'icon' }),
              h('span', { class: 'action-label' }, `Action ${i + 1}`),
            ]),
          ),
          'menu-extra': () => h('button', { class: 'dropdown-item' }, 'Delete'),
        },
      )
    },
  })

  const wrapper = mount(Comp, { global: { plugins: [i18n] } })
  const bar = wrapper.element as HTMLElement

  Object.defineProperty(bar, 'clientWidth', { configurable: true, value: sizes.available })
  // jsdom reports 0 for scrollWidth. The component asks the browser how wide the
  // row becomes WITH labels, so the fake has to answer the same question: the
  // labelled total while `.ab--labels` is on, the icon total otherwise.
  Object.defineProperty(bar, 'scrollWidth', {
    configurable: true,
    get: () => (bar.classList.contains('ab--labels') ? sizes.labelled : sizes.icon),
  })
  const buttons = Array.from(bar.children).filter(
    (el): el is HTMLElement => el instanceof HTMLElement && el.tagName === 'BUTTON' && !el.hasAttribute('data-ab-more'),
  )
  const perIcon = sizes.icon / Math.max(1, buttons.length)
  const perLabel = sizes.labelled / Math.max(1, buttons.length)
  buttons.forEach((b, i) => {
    // Evaluated per call: the width depends on the label state, which the bar
    // flips while measuring.
    b.getBoundingClientRect = () => {
      // Only `.ab--labels` widens a button, exactly like the real stylesheets:
      // `.ab-measuring` merely disables transitions. A fake that also reacted to
      // `.ab-measuring` hid a real defect — the measurement saw label widths that
      // the real page would never apply, so it always concluded "fits".
      const withLabels = bar.classList.contains('ab--labels')
      const width = withLabels ? perLabel : perIcon
      return { left: i * width, right: (i + 1) * width, width, top: 0, bottom: 0, height: 0, x: 0, y: 0, toJSON: () => ({}) } as DOMRect
    }
  })

  return { wrapper, bar, measure: () => barRef.value?.measure() }
}

afterEach(() => {
  document.body.innerHTML = ''
})

describe('ActionBar — labels', () => {
  it('shows the labels when the row can hold them', async () => {
    const { bar, measure } = mountBar({ sizes: { icon: 90, labelled: 200, available: 500 } })
    measure()
    await nextTick()
    expect(bar.classList.contains('ab--labels')).toBe(true)
  })

  it('never leaves the labels on in a row that overflows', async () => {
    // Regression: the measurement used to force only `.ab-measuring`, which
    // does NOT widen the buttons. It therefore measured icon widths, concluded
    // "the labels fit", turned them on — and the row then stuck out of the bar.
    const cases: Sizes[] = [
      { icon: 90, labelled: 700, available: 500 },
      { icon: 90, labelled: 505, available: 500 },
      { icon: 90, labelled: 1200, available: 500 },
    ]
    for (const sizes of cases) {
      const { bar, measure } = mountBar({ sizes })
      measure()
      await nextTick()
      if (!bar.classList.contains('ab--labels')) continue
      const kids = Array.from(bar.children).filter(
        (el): el is HTMLElement => el instanceof HTMLElement && el.tagName === 'BUTTON' && !el.hasAttribute('data-ab-more'),
      )
      const total = kids.reduce((sum, k) => sum + k.getBoundingClientRect().width, 0)
      expect(total, 'labels are on but the row is wider than the bar').toBeLessThanOrEqual(bar.clientWidth)
    }
  })

  it('drops the labels when they do not fit, keeping the icons', async () => {
    const { bar, measure } = mountBar({ sizes: { icon: 90, labelled: 600, available: 500 } })
    measure()
    await nextTick()
    expect(bar.classList.contains('ab--labels')).toBe(false)
  })

  it('needs real spare room before turning them back on (no boundary flicker)', async () => {
    // Without the dead zone a bar dragged across the boundary toggles at almost
    // every pixel: on → 2px narrower → off → 2px wider → on.
    const { bar, measure } = mountBar({ sizes: { icon: 90, labelled: 600, available: 500 } })
    measure()
    await nextTick()
    expect(bar.classList.contains('ab--labels')).toBe(false)

    // Start from the OFF state, then check that 1px of slack is not enough to
    // turn them back on. (A bar that is already ON only turns off on overflow.)
    const tight = mountBar({ sizes: { icon: 90, labelled: 499, available: 500 } })
    Object.defineProperty(tight.bar, 'clientWidth', { configurable: true, value: 200 })
    tight.measure()
    await nextTick()
    expect(tight.bar.classList.contains('ab--labels')).toBe(false)
    Object.defineProperty(tight.bar, 'clientWidth', { configurable: true, value: 500 })
    tight.measure()
    await nextTick()
    expect(tight.bar.classList.contains('ab--labels')).toBe(false)

    // Real slack is.
    const roomy = mountBar({ sizes: { icon: 90, labelled: 470, available: 500 } })
    roomy.measure()
    await nextTick()
    expect(roomy.bar.classList.contains('ab--labels')).toBe(true)
  })

  it('subtracts the bar’s own padding from the room it has', async () => {
    // Padding is not room the buttons can use; counting it lets a labelled row
    // overflow by exactly that much.
    const { bar, measure } = mountBar({ sizes: { icon: 90, labelled: 480, available: 500 } })
    // jsdom does not compute `padding: 20px` from inline styles for
    // getComputedStyle in every case; set the resolved values directly.
    bar.style.padding = '0 20px'
    Object.defineProperty(window, 'getComputedStyle', {
      configurable: true,
      value: () => ({ paddingLeft: '20px', paddingRight: '20px', columnGap: '0px', fontSize: '15px' }),
    })
    measure()
    await nextTick()
    expect(bar.classList.contains('ab--labels')).toBe(false)
  })
})

describe('ActionBar — folding (menu mode)', () => {
  it('folds the tail into the more button when the icons alone overflow', async () => {
    // 5 buttons × 60 = 300 > 200 available.
    const { bar, wrapper, measure } = mountBar({
      count: 5,
      sizes: { icon: 300, labelled: 400, available: 200 },
    })
    measure()
    await nextTick()
    expect(wrapper.find('[data-ab-more]').exists()).toBe(true)
    const hidden = Array.from(bar.children).filter(
      el => el instanceof HTMLElement && el.tagName === 'BUTTON' && !el.hasAttribute('data-ab-more') && el.style.display === 'none',
    )
    expect(hidden.length).toBeGreaterThan(0)
  })

  it('never shows labels once buttons had to be folded away', async () => {
    // Degradation order is labels → icons → more menu. A row that already hid
    // buttons is short of room by definition, so labelling the survivors would
    // claim space that is actually occupied by the hidden ones.
    const { bar, measure } = mountBar({ count: 5, sizes: { icon: 300, labelled: 400, available: 200 } })
    measure()
    await nextTick()
    expect(bar.classList.contains('ab--labels')).toBe(false)
  })

  it('keeps every button inline when nothing overflows', async () => {
    const { bar, wrapper, measure } = mountBar({ count: 3, sizes: { icon: 90, labelled: 200, available: 500 } })
    measure()
    await nextTick()
    expect(wrapper.find('[data-ab-more]').exists()).toBe(false)
    const hidden = Array.from(bar.children).filter(el => (el as HTMLElement).style.display === 'none')
    expect(hidden.length).toBe(0)
  })

  it('offers the folded buttons (and the host’s own entries) in the menu', async () => {
    const { wrapper, measure } = mountBar({
      count: 5,
      sizes: { icon: 300, labelled: 400, available: 200 },
      alwaysMore: true,
    })
    measure()
    await nextTick()
    await wrapper.find('[data-ab-more]').trigger('click')
    await flushPromises()
    await nextTick()
    await nextTick()

    // PopupMenu teleports to <body>, so the items live outside the wrapper.
    const texts = Array.from(document.body.querySelectorAll('.ab-menu-item, .dropdown-item')).map(
      el => el.textContent ?? '',
    )
    expect(texts.some(t => t.includes('Action 5'))).toBe(true)
    expect(texts.some(t => t.includes('Delete'))).toBe(true)
  })

  it('shows the more button even with nothing folded when the host owns menu entries', async () => {
    const { wrapper, measure } = mountBar({ count: 2, sizes: { icon: 60, labelled: 120, available: 500 }, alwaysMore: true })
    measure()
    await nextTick()
    expect(wrapper.find('[data-ab-more]').exists()).toBe(true)
  })
})

describe('ActionBar — scrolling (scroll mode)', () => {
  it('never folds a button away', async () => {
    const { bar, wrapper, measure } = mountBar({ mode: 'scroll', count: 5, sizes: { icon: 400, labelled: 600, available: 200 } })
    measure()
    await nextTick()
    expect(wrapper.find('[data-ab-more]').exists()).toBe(false)
    expect(bar.classList.contains('ab--scroll')).toBe(true)
  })

  it('reports overflow only when the row really scrolls', async () => {
    const { bar, measure } = mountBar({ mode: 'scroll', count: 3, sizes: { icon: 90, labelled: 200, available: 500 } })
    measure()
    await nextTick()
    expect(bar.classList.contains('ab--overflowing')).toBe(false)

    Object.defineProperty(bar, 'scrollWidth', { configurable: true, value: 900 })
    measure()
    await nextTick()
    expect(bar.classList.contains('ab--overflowing')).toBe(true)
  })

  it('marks the side that still has content behind it', async () => {
    const { bar, measure } = mountBar({ mode: 'scroll', count: 3, sizes: { icon: 90, labelled: 200, available: 500 } })
    Object.defineProperty(bar, 'scrollWidth', { configurable: true, value: 900 })
    measure()
    await nextTick()
    // At the start: content to the right only.
    expect(bar.dataset.fadeLeft).toBe('0')
    expect(bar.dataset.fadeRight).toBe('1')

    Object.defineProperty(bar, 'scrollLeft', { configurable: true, value: 400 })
    bar.dispatchEvent(new Event('scroll'))
    await nextTick()
    expect(bar.dataset.fadeLeft).toBe('1')
    expect(bar.dataset.fadeRight).toBe('0')
  })
})
