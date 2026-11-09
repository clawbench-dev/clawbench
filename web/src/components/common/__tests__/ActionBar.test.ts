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
  // Tests here stub getComputedStyle / ResizeObserver / requestAnimationFrame.
  // Without this, one test's fake silently decides the next test's layout.
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
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
    // A spy (not a hand-rolled defineProperty): it has to be undone after this
    // test, or every later bar in the file reads `padding: 20px` and folds a row
    // that fits — a failure that looks like a product bug.
    vi.spyOn(window, 'getComputedStyle').mockReturnValue({
      paddingLeft: '20px',
      paddingRight: '20px',
      columnGap: '0px',
      fontSize: '15px',
    } as unknown as CSSStyleDeclaration)
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

  /**
   * The width arithmetic is not allowed to be the last word.
   *
   * With `align="end"` the row is justified to flex-end, so overflow spills out
   * of the START edge, where `overflow: hidden` clips the first button — and the
   * element still reports `scrollWidth === clientWidth`, so the browser claims
   * nothing is wrong. A row measured while the bar was a few pixels wider than
   * it paints therefore loses the first icon (seen in the file viewer: the
   * refresh glyph arrived cut in half).
   *
   * jsdom cannot lay out flex, so the fake below answers with the PHYSICAL
   * truth: buttons are right-aligned, which means their `left` goes negative
   * once the content is wider than the bar. `clientWidth` is deliberately left
   * at the stale, wider value — that is exactly the defect.
   */
  it('keeps folding while the first button still sticks out of the bar', async () => {
    const count = 3
    const perIcon = 200
    const paintedWidth = 500 // what the browser actually shows
    const staleClientWidth = 620 // what measure() believes

    const barRef = ref<{ measure: () => void } | null>(null)
    const Comp = defineComponent({
      render() {
        return h(
          ActionBar,
          { ref: barRef, mode: 'menu', alwaysMore: true },
          {
            default: () => Array.from({ length: count }, (_, i) =>
              h('button', { class: 'btn', title: `Action ${i + 1}` }, [h('span', { class: 'action-label' }, `Action ${i + 1}`)]),
            ),
          },
        )
      },
    })
    const wrapper = mount(Comp, { global: { plugins: [i18n] } })
    const bar = wrapper.element as HTMLElement
    Object.defineProperty(bar, 'clientWidth', { configurable: true, value: staleClientWidth })
    // The bar's own box, as the browser would report it: 500px wide at x=0.
    bar.getBoundingClientRect = () =>
      ({ left: 0, right: paintedWidth, top: 0, bottom: 0, width: paintedWidth, height: 0, x: 0, y: 0, toJSON: () => ({}) }) as DOMRect

    const buttons = Array.from(bar.children).filter(
      (el): el is HTMLElement => el instanceof HTMLElement && el.tagName === 'BUTTON' && !el.hasAttribute('data-ab-more'),
    )
    buttons.forEach((b, i) => {
      b.getBoundingClientRect = () => {
        // Right-aligned row: the visible ones are pushed to the trailing edge,
        // so a row that does not fit starts at a NEGATIVE x.
        const visible = buttons.filter(el => el.style.display !== 'none').length
        const left = Math.min(0, paintedWidth - visible * perIcon) + i * perIcon
        return { left, width: perIcon, right: left + perIcon, top: 0, bottom: 0, height: 0, x: left, y: 0, toJSON: () => ({}) } as DOMRect
      }
    })

    barRef.value?.measure()
    await nextTick()

    const shown = buttons.filter(el => el.style.display !== 'none').length
    // The arithmetic believed 620px and kept all three; the ground truth is a
    // 500px bar, where 2 × 200 fit and 3 do not — so one button must have been
    // folded away even though clientWidth said there was room.
    expect(shown, 'the first button must not stick out of the bar').toBe(2)
  })
})

describe('ActionBar — the bar shrinking by a hair', () => {
  /**
   * A fold decision goes stale when the bar's width creeps down in steps the
   * observer ignores. The width really does creep: a webfont swapping in or
   * fractional layout moves it a pixel at a time, and the old `< 2px` dead-band
   * swallowed every step — the row then overflowed and (with `align="end"`) the
   * first button was clipped instead of folded.
   */
  it('re-measures when the bar shrinks by a single pixel', async () => {
    const frames: Array<FrameRequestCallback> = []
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
      frames.push(cb)
      // 0 keeps the component's `if (frame) return` dedupe honest while these
      // frames are drained by hand (a real id would stay latched for ever).
      return 0
    })
    vi.stubGlobal('cancelAnimationFrame', () => {})
    const observed: Array<{ callback: ResizeObserverCallback }> = []
    class FakeResizeObserver {
      constructor(public callback: ResizeObserverCallback) {
        observed.push(this)
      }
      observe() {}
      unobserve() {}
      disconnect() {}
    }
    vi.stubGlobal('ResizeObserver', FakeResizeObserver)

    try {
      // 5 × 100 = 500 fills the 500px bar exactly; one pixel narrower and a
      // button has to go. The old 2px dead-band swallowed that pixel.
      const { bar, measure } = mountBar({ count: 5, sizes: { icon: 500, labelled: 700, available: 500 } })
      await nextTick()
      // Deterministic starting point: fold decided at width 500, nothing hidden.
      // (mountBar installs the fake clientWidth AFTER mounting, so the mount's
      // own measure ran on an un-laid-out bar.)
      frames.length = 0
      measure()
      await nextTick()
      const hiddenBefore = Array.from(bar.children).filter(
        el => el instanceof HTMLElement && el.tagName === 'BUTTON' && !el.hasAttribute('data-ab-more') && el.style.display === 'none',
      )
      expect(hiddenBefore.length, 'the full row fits to begin with').toBe(0)

      // Teach the component the current width (mountBar installs the fake
      // clientWidth after mounting, so the first observer report is what
      // records it), then drop the frame that report queued.
      observed[0].callback([] as unknown as ResizeObserverEntry[], observed[0] as unknown as ResizeObserver)
      frames.length = 0

      // The bar shrinks by ONE pixel: below the old 2px dead-band.
      Object.defineProperty(bar, 'clientWidth', { configurable: true, value: 499 })
      observed[0].callback([] as unknown as ResizeObserverEntry[], observed[0] as unknown as ResizeObserver)

      const queued = frames.splice(0)
      expect(queued.length, 'a 1px shrink must schedule a re-measure').toBeGreaterThan(0)
      queued.forEach(cb => cb(0))
      await nextTick()

      const hiddenAfter = Array.from(bar.children).filter(
        el => el instanceof HTMLElement && el.tagName === 'BUTTON' && !el.hasAttribute('data-ab-more') && el.style.display === 'none',
      )
      expect(hiddenAfter.length, 'a 1px shrink must re-run the fold').toBeGreaterThan(0)
    } finally {
      vi.unstubAllGlobals()
    }
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

describe('ActionBar — wrapped buttons', () => {
  it('still reports overflow when the buttons sit in a wrapper', async () => {
    // The code preview groups its tools in a wrapper (it needs the group to
    // stop pointerdown from starting a card drag). No direct button means the
    // fold maths has nothing to do, but scroll mode must still know the row
    // overflows — otherwise the fades and drag never turn on and the row just
    // clips its last buttons.
    const wrapper = mount(ActionBar, {
      props: { mode: 'scroll' },
      global: { plugins: [i18n] },
      slots: { default: () => h('div', { class: 'wrap' }, [h('button', 'A'), h('button', 'B')]) },
    })
    const bar = wrapper.element as HTMLElement
    Object.defineProperty(bar, 'clientWidth', { configurable: true, value: 100 })
    Object.defineProperty(bar, 'scrollWidth', { configurable: true, value: 300 })
    ;(wrapper.vm as unknown as { measure: () => void }).measure()
    await nextTick()
    expect(bar.classList.contains('ab--overflowing')).toBe(true)
  })
})

describe('ActionBar — row gap', () => {
  const mountWith = (props: Record<string, unknown>) =>
    mount(ActionBar, {
      props,
      global: { plugins: [i18n] },
      slots: { default: () => h('button', 'A') },
    })

  it('separates the buttons by default', () => {
    // Regression: the gap was left to each host's CSS and the chat row ended up
    // at 0 — as soon as one button's background lit up, the two tints touched
    // and the pair read as a single control.
    const wrapper = mountWith({})
    expect((wrapper.element as HTMLElement).style.gap).toBe('calc(8 * var(--dp))')
  })

  it('lets a keyboard-like strip ask for zero', () => {
    const wrapper = mountWith({ gap: 0 })
    expect((wrapper.element as HTMLElement).style.gap).toBe('calc(0 * var(--dp))')
  })
})
