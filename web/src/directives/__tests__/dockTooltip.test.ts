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
