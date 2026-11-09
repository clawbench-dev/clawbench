import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { DockTipDirective } from '../dockTooltip'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * The chip lives in <body> (teleported by the directive) and is shared by every
 * dock button, so tests drive the directive directly rather than mounting Vue —
 * the directive hooks are the whole surface.
 */

function makeTouch(type: string, x: number, y: number): Event {
  const ev = new Event(type, { bubbles: true, cancelable: true })
  Object.defineProperty(ev, 'touches', { value: [{ clientX: x, clientY: y }] })
  return ev
}

function chipEl(): HTMLElement | null {
  return document.body.querySelector('.dock-tip')
}

function chipText(): string | null {
  return chipEl()?.textContent ?? null
}

function chipVisible(): boolean {
  return !!chipEl()?.classList.contains('is-visible')
}

describe('DockTipDirective', () => {
  let els: HTMLElement[] = []

  function mountTip(text = 'File manager'): HTMLElement {
    const el = document.createElement('button')
    document.body.appendChild(el)
    DockTipDirective.mounted(el, { value: text } as never)
    els.push(el)
    return el
  }

  beforeEach(() => {
    // Fake only the timer APIs the directive uses; faking the whole scheduler
    // stalls Vue/queueMicrotask and leaves fork workers unable to exit.
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    vi.stubGlobal('matchMedia', (query: string) => ({
      matches: true,
      media: query,
      onchange: null,
      addEventListener: () => {},
      removeEventListener: () => {},
      addListener: () => {},
      removeListener: () => {},
      dispatchEvent: () => false,
    }))
  })

  afterEach(() => {
    for (const el of els) DockTipDirective.unmounted(el)
    els = []
    vi.clearAllTimers()
    vi.useRealTimers()
    vi.unstubAllGlobals()
    document.body.innerHTML = ''
  })

  it('shows the label only after the hover delay, then hides on leave', () => {
    const el = mountTip('File manager')

    el.dispatchEvent(new MouseEvent('mouseenter'))
    vi.advanceTimersByTime(299)
    expect(chipVisible(), 'must not flash on a pass-by').toBe(false)

    vi.advanceTimersByTime(1)
    expect(chipVisible()).toBe(true)
    expect(chipText()).toBe('File manager')

    el.dispatchEvent(new MouseEvent('mouseleave'))
    expect(chipVisible()).toBe(false)
  })

  it('cancels a pending hover when the pointer leaves early', () => {
    const el = mountTip()

    el.dispatchEvent(new MouseEvent('mouseenter'))
    el.dispatchEvent(new MouseEvent('mouseleave'))
    vi.advanceTimersByTime(500)

    expect(chipVisible()).toBe(false)
  })

  it('shows immediately on keyboard focus, without waiting for hover', () => {
    const el = mountTip('Terminal')

    el.dispatchEvent(new FocusEvent('focus'))
    expect(chipVisible()).toBe(true)
    expect(chipText()).toBe('Terminal')

    el.dispatchEvent(new FocusEvent('blur'))
    expect(chipVisible()).toBe(false)
  })

  it('ignores focus on touch-only hosts (a tap must not pop a chip)', () => {
    vi.stubGlobal('matchMedia', (query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addEventListener: () => {},
      removeEventListener: () => {},
      addListener: () => {},
      removeListener: () => {},
      dispatchEvent: () => false,
    }))
    const el = mountTip()

    el.dispatchEvent(new FocusEvent('focus'))
    expect(chipVisible()).toBe(false)
  })

  it('reveals the label after a 450ms hold and swallows the release click', () => {
    const el = mountTip('Settings')

    el.dispatchEvent(makeTouch('touchstart', 10, 10))
    vi.advanceTimersByTime(449)
    expect(chipVisible()).toBe(false)

    vi.advanceTimersByTime(1)
    expect(chipVisible()).toBe(true)
    expect(chipText()).toBe('Settings')

    const end = makeTouch('touchend', 10, 10)
    el.dispatchEvent(end)
    // preventDefault on touchend is what stops the synthetic click — the dock's
    // click switches/collapses panes, which a discovery gesture must not do.
    expect(end.defaultPrevented).toBe(true)
    expect(chipVisible()).toBe(false)
  })

  it('treats a short tap as a normal tap (no chip, click still fires)', () => {
    const el = mountTip()

    el.dispatchEvent(makeTouch('touchstart', 10, 10))
    vi.advanceTimersByTime(200)
    const end = makeTouch('touchend', 10, 10)
    el.dispatchEvent(end)
    vi.advanceTimersByTime(500)

    expect(chipVisible()).toBe(false)
    expect(end.defaultPrevented).toBe(false)
  })

  it('cancels the hold when the finger moves (dock scrolling)', () => {
    const el = mountTip()

    el.dispatchEvent(makeTouch('touchstart', 10, 10))
    el.dispatchEvent(makeTouch('touchmove', 10, 40))
    vi.advanceTimersByTime(500)

    expect(chipVisible()).toBe(false)
  })

  it('positions the chip to the right of the button, vertically centred', () => {
    document.documentElement.style.fontSize = '15px' // 1 design px = 1 CSS px
    const el = mountTip()
    el.getBoundingClientRect = () => ({
      x: 7, y: 100, left: 7, top: 100, right: 55, bottom: 134, width: 48, height: 34,
      toJSON: () => ({}),
    }) as DOMRect

    el.dispatchEvent(new FocusEvent('focus'))

    // 8 design px gap, no chip height in jsdom → centred on the button's middle.
    expect(chipEl()!.style.left).toBe('63px')
    expect(chipEl()!.style.top).toBe('117px')
    document.documentElement.style.fontSize = ''
  })

  it('hides on click (the tab switch would otherwise leave a floating chip)', () => {
    const el = mountTip()

    el.dispatchEvent(new FocusEvent('focus'))
    expect(chipVisible()).toBe(true)

    el.dispatchEvent(new MouseEvent('click'))
    expect(chipVisible()).toBe(false)
  })

  it('follows a label change while the chip is on screen', () => {
    const el = mountTip('GitHub Integration')
    el.dispatchEvent(new FocusEvent('focus'))

    DockTipDirective.updated!(el, { value: 'GitLab Integration' } as never)

    expect(chipText()).toBe('GitLab Integration')
  })

  it('does not let a sibling button rewrite the visible chip', () => {
    const shown = mountTip('GitHub Integration')
    const sibling = mountTip('Settings')
    shown.dispatchEvent(new FocusEvent('focus'))

    // `updated` fires for every button in the v-for on a language switch.
    DockTipDirective.updated!(sibling, { value: '设置' } as never)

    expect(chipText()).toBe('GitHub Integration')
  })

  it('renders nothing for an empty label', () => {
    const el = mountTip('')

    el.dispatchEvent(new FocusEvent('focus'))
    expect(chipEl()).toBeNull()
  })

  it('stops responding after unmount', () => {
    const el = mountTip()
    DockTipDirective.unmounted(el)
    els = []

    el.dispatchEvent(new FocusEvent('focus'))
    vi.advanceTimersByTime(500)

    expect(chipVisible()).toBe(false)
  })
})

/**
 * App.vue has no component test, so the wiring that makes the directive matter
 * is pinned as source text. Without the registration the directive silently
 * does nothing (Vue only warns), and without the stylesheet the chip is an
 * unstyled body-level div.
 */
describe('wide dock tooltip wiring', () => {
  const app = readWebFile('src/App.vue')
  const main = readWebFile('src/main.ts')

  it('labels both wide-dock button loops with the shared lookup', () => {
    const labels = app.match(/v-dock-tip="dockTabTitle\(tab\)"/g) ?? []
    expect(labels.length).toBe(2)
    // Accessible name has to survive dropping the native title attribute.
    expect((app.match(/:aria-label="dockTabTitle\(tab\)"/g) ?? []).length).toBe(2)
  })

  it('does not keep a native title on the wide dock (it would double up)', () => {
    const wideDock = app.slice(app.indexOf('class="wide-dock"'), app.indexOf('class="content-area"'))
    expect(wideDock).not.toContain(':title=')
  })

  it('registers the directive and loads its global stylesheet', () => {
    expect(main).toContain("app.directive('dock-tip', DockTipDirective)")
    expect(main).toContain("import '@/assets/dock-tooltip.css'")
  })
})

describe('DockTipDirective — touch and chip edge cases', () => {
  let els: HTMLElement[] = []

  function mountTip(text = 'File manager'): HTMLElement {
    const el = document.createElement('button')
    document.body.appendChild(el)
    DockTipDirective.mounted(el, { value: text } as never)
    els.push(el)
    return el
  }

  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    vi.stubGlobal('matchMedia', (query: string) => ({
      matches: true,
      media: query,
      onchange: null,
      addEventListener: () => {},
      removeEventListener: () => {},
      addListener: () => {},
      removeListener: () => {},
      dispatchEvent: () => false,
    }))
  })

  afterEach(() => {
    for (const el of els) DockTipDirective.unmounted(el)
    els = []
    vi.clearAllTimers()
    vi.useRealTimers()
    vi.unstubAllGlobals()
    document.body.innerHTML = ''
  })

  it('touchcancel drops a pending hold and hides the chip', () => {
    const el = mountTip('Settings')

    el.dispatchEvent(makeTouch('touchstart', 10, 10))
    vi.advanceTimersByTime(500)
    expect(chipVisible()).toBe(true)

    el.dispatchEvent(makeTouch('touchcancel', 10, 10))
    expect(chipVisible()).toBe(false)

    // The cancelled gesture must not leave a live press timer behind: a later
    // release is just a tap, not a swallowed click.
    const end = makeTouch('touchend', 10, 10)
    el.dispatchEvent(end)
    expect(end.defaultPrevented).toBe(false)
  })

  it('ignores touchmove without a pending hold (a scroll before the press)', () => {
    const el = mountTip()

    el.dispatchEvent(makeTouch('touchmove', 10, 90))
    vi.advanceTimersByTime(500)
    expect(chipVisible()).toBe(false)
  })

  it('swallows the contextmenu a hold synthesises, but not a real right-click', () => {
    const el = mountTip()

    const idle = new Event('contextmenu', { bubbles: true, cancelable: true })
    el.dispatchEvent(idle)
    expect(idle.defaultPrevented, 'a right-click without a hold keeps the native menu').toBe(false)

    el.dispatchEvent(makeTouch('touchstart', 10, 10))
    vi.advanceTimersByTime(500)
    const synthesised = new Event('contextmenu', { bubbles: true, cancelable: true })
    el.dispatchEvent(synthesised)
    expect(synthesised.defaultPrevented, 'the chip is the only product of the gesture').toBe(true)
  })

  it('ignores updated on an element the directive never mounted', () => {
    const stranger = document.createElement('button')
    document.body.appendChild(stranger)
    // Must not throw: `updated` fires for every v-for button, including ones
    // that were not mounted by this directive (a sibling component's button).
    expect(() => DockTipDirective.updated!(stranger, { value: 'x' } as never)).not.toThrow()
  })

  it('reuses the chip instead of stacking a second one', () => {
    const first = mountTip('File manager')
    const second = mountTip('Settings')

    first.dispatchEvent(new FocusEvent('focus'))
    const chip = chipEl()
    second.dispatchEvent(new FocusEvent('focus'))

    expect(chipEl()).toBe(chip)
    expect(document.body.querySelectorAll('.dock-tip')).toHaveLength(1)
  })

  it('mirrors the chip to the inside when the dock is against the right edge', () => {
    const el = mountTip()
    el.getBoundingClientRect = () => ({
      x: 300, y: 100, left: 300, top: 100, right: 340, bottom: 134, width: 40, height: 34,
      toJSON: () => ({}),
    }) as DOMRect
    const originalWidth = window.innerWidth
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 350 })

    el.dispatchEvent(new FocusEvent('focus'))

    // The chip has no width in jsdom, so the flip branch is what decides the
    // side: the left edge minus the gap, never the right edge plus it.
    expect(chipEl()!.style.left).toBe('292px')
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: originalWidth })
  })

  it('ignores touch frames that carry no touch point', () => {
    const el = mountTip()
    const noPoint = (type: string) => {
      const ev = new Event(type, { bubbles: true, cancelable: true })
      Object.defineProperty(ev, 'touches', { value: [] })
      return ev
    }

    // Some browsers emit a frame with an empty list (the finger left between
    // events). Reading touches[0] blindly would throw inside the listener.
    expect(() => el.dispatchEvent(noPoint('touchstart'))).not.toThrow()
    vi.advanceTimersByTime(500)
    expect(chipVisible()).toBe(false)

    // A hold in progress must survive a point-less move frame: the timer is
    // still running, so the label appears on schedule.
    el.dispatchEvent(makeTouch('touchstart', 10, 10))
    expect(() => el.dispatchEvent(noPoint('touchmove'))).not.toThrow()
    vi.advanceTimersByTime(500)
    expect(chipVisible()).toBe(true)
  })

  it('does not open a chip on hover for touch-only hosts', () => {
    vi.stubGlobal('matchMedia', (query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addEventListener: () => {},
      removeEventListener: () => {},
      addListener: () => {},
      removeListener: () => {},
      dispatchEvent: () => false,
    }))
    const el = mountTip()

    el.dispatchEvent(new MouseEvent('mouseenter'))
    vi.advanceTimersByTime(500)
    expect(chipVisible(), 'a touch host has no hover').toBe(false)
  })
})
