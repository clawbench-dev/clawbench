/**
 * HintTooltip tests.
 *
 * The tooltip shows a popup with `content` on desktop hover (gated by
 * `(hover: hover)` media query), positioned near the cursor, with a hover
 * delay. Touch devices never see it. When `content` is cleared the popup
 * disappears.
 */
import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import { nextTick, h, type Component } from 'vue'
import HintTooltip from '@/components/common/HintTooltip.vue'

function installHoverMatchMedia(hover: boolean) {
  const orig = window.matchMedia
  window.matchMedia = vi.fn((query: string) => ({
    matches: query.includes('hover: hover') ? hover : false,
    media: query,
    onchange: null,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    dispatchEvent: vi.fn(),
  })) as unknown as typeof window.matchMedia
  return () => { window.matchMedia = orig }
}

/** The teleported popup renders in <body>. */
function tipEl(): HTMLElement | null {
  return document.body.querySelector('.hint-tooltip')
}

describe('HintTooltip', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    // Fake timers stall requestAnimationFrame — fire it immediately so
    // schedulePlace() placements run within the same tick.
    vi.spyOn(window, 'requestAnimationFrame').mockImplementation((cb: FrameRequestCallback) => {
      cb(0)
      return 1
    })
  })
  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
    document.body.innerHTML = ''
  })

  /** Mount inside a host row (the component binds to its parent element). */
  function mountInRow(content: string, delay = 400): VueWrapper {
    const Host: Component = {
      components: { HintTooltip },
      props: ['content', 'delay'],
      setup(props) {
        return () =>
          h('div', { class: 'menu-row' }, [
            h('span', 'some text'),
            h(HintTooltip, { content: props.content, delay: props.delay }),
          ])
      },
    }
    return mount(Host, { props: { content, delay }, attachTo: document.body })
  }

  it('does not render the popup without content', async () => {
    const restore = installHoverMatchMedia(true)
    mountInRow('')
    await nextTick()
    expect(tipEl()).toBeNull()
    restore()
  })

  it('shows the popup on desktop hover after the delay', async () => {
    const restore = installHoverMatchMedia(true)
    const wrapper = mountInRow('/home/user/projects/clawbench', 400)

    const row = wrapper.find('.menu-row')
    expect(row.exists()).toBe(true)
    await row.trigger('mouseover')
    await row.trigger('mousemove', { clientX: 50, clientY: 30 })

    // Not yet visible during the hover delay window
    expect(tipEl()).toBeNull()

    vi.advanceTimersByTime(400)
    await nextTick()

    const tip = tipEl()
    expect(tip).not.toBeNull()
    expect(tip!.textContent).toBe('/home/user/projects/clawbench')
    expect(tip!.getAttribute('role')).toBe('tooltip')

    // Positioned near the cursor (mousemove position + offset)
    expect(tip!.style.left).toMatch(/^\d+(\.\d+)?px$/)
    expect(tip!.style.top).toMatch(/^\d+(\.\d+)?px$/)

    restore()
  })

  it('does not restart the delay when moving between row children', async () => {
    const restore = installHoverMatchMedia(true)
    const wrapper = mountInRow('/a/b/c', 400)

    const row = wrapper.find('.menu-row')
    await row.trigger('mouseover') // enter the row → delay starts
    vi.advanceTimersByTime(100)

    // Move between children (mouseover again) — must not reset the 400ms clock
    await row.trigger('mouseover')
    vi.advanceTimersByTime(300)
    await nextTick()
    expect(tipEl()).not.toBeNull()

    restore()
  })

  it('hides the popup when the pointer leaves the row', async () => {
    const restore = installHoverMatchMedia(true)
    const wrapper = mountInRow('/a/b/c', 0)

    const row = wrapper.find('.menu-row')
    await row.trigger('mouseover')
    vi.advanceTimersByTime(0)
    await nextTick()
    expect(tipEl()).not.toBeNull()

    // Leave into an unrelated element → popup hides
    const outside = document.createElement('div')
    document.body.appendChild(outside)
    await row.trigger('mouseout', { relatedTarget: outside })
    await nextTick()
    expect(tipEl()).toBeNull()
    outside.remove()

    restore()
  })

  it('stays visible when moving between the rows own children', async () => {
    const restore = installHoverMatchMedia(true)
    const wrapper = mountInRow('/a/b/c', 0)

    const row = wrapper.find('.menu-row')
    const span = wrapper.find('.menu-row span')
    await row.trigger('mouseover')
    vi.advanceTimersByTime(0)
    await nextTick()
    expect(tipEl()).not.toBeNull()

    // mouseout to a sibling inside the same row → still inside → stays
    await span.trigger('mouseover')
    await row.trigger('mouseout', { relatedTarget: span.element })
    await nextTick()
    expect(tipEl()).not.toBeNull()

    restore()
  })

  it('never shows on touch devices', async () => {
    const restore = installHoverMatchMedia(false)
    const wrapper = mountInRow('/a/b/c', 0)

    const row = wrapper.find('.menu-row')
    await row.trigger('mouseover')
    vi.advanceTimersByTime(500)
    await nextTick()
    expect(tipEl()).toBeNull()

    restore()
  })

  it('clears the popup when content becomes empty', async () => {
    const restore = installHoverMatchMedia(true)
    const wrapper = mountInRow('/a/b/c', 0)

    const row = wrapper.find('.menu-row')
    await row.trigger('mouseover')
    vi.advanceTimersByTime(0)
    await nextTick()
    expect(tipEl()).not.toBeNull()

    await wrapper.setProps({ content: '' })
    await nextTick()
    expect(tipEl()).toBeNull()

    restore()
  })
})
