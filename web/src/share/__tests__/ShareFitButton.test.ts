import { describe, expect, it, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import i18n from '@/i18n'
import ShareFitButton from '@/share/ShareFitButton.vue'
import { SHARE_FIT_KEY } from '@/share/shareScale'

/**
 * The "fit to screen" switch on the share topbar.
 *
 * The behaviour that matters is a two-way toggle: the first click enlarges the
 * page, the second restores it, and the button SAYS which state it is in (both
 * by its pressed attribute and by its tooltip). A one-way "fit" that could not
 * be undone would be worse than no control at all — the reader would have no way
 * back to the size the page was designed at.
 */

const ROOT_ATTR = '--ui-scale'

afterEach(() => {
  document.documentElement.style.removeProperty(ROOT_ATTR)
  document.documentElement.style.removeProperty('--input-line-height')
  localStorage.removeItem(SHARE_FIT_KEY)
})

function stubScreen(shortEdge: number): void {
  Object.defineProperty(window.screen, 'width', { value: shortEdge, configurable: true })
  Object.defineProperty(window.screen, 'height', { value: shortEdge, configurable: true })
  Object.defineProperty(window, 'devicePixelRatio', { value: 1, configurable: true })
}

function mountButton() {
  return mount(ShareFitButton, { global: { plugins: [i18n] } })
}

describe('ShareFitButton', () => {
  it('starts unpressed when nothing is stored', () => {
    stubScreen(1440)
    const w = mountButton()
    const btn = w.find('[data-action="share-fit"]')
    expect(btn.exists()).toBe(true)
    expect(btn.attributes('aria-pressed')).toBe('false')
    // Off must not touch the document: the page opens at its design size.
    expect(document.documentElement.style.getPropertyValue(ROOT_ATTR)).toBe('')
  })

  it('enlarges the page on the first click and restores it on the second', async () => {
    stubScreen(1440) // → 1.33
    const w = mountButton()
    const btn = w.find('[data-action="share-fit"]')

    await btn.trigger('click')
    await nextTick()
    expect(Number(document.documentElement.style.getPropertyValue(ROOT_ATTR))).toBeGreaterThan(1)
    expect(btn.attributes('aria-pressed')).toBe('true')
    expect(localStorage.getItem(SHARE_FIT_KEY)).toBe('1')

    await btn.trigger('click')
    await nextTick()
    expect(document.documentElement.style.getPropertyValue(ROOT_ATTR)).toBe('1')
    expect(btn.attributes('aria-pressed')).toBe('false')
    expect(localStorage.getItem(SHARE_FIT_KEY)).toBeNull()
  })

  it('names the RESULT in its tooltip, not the current state', async () => {
    // A tooltip that says "fit to screen" while already fitted tells the reader
    // nothing about what clicking will do.
    stubScreen(1440)
    const w = mountButton()
    const btn = w.find('[data-action="share-fit"]')

    const offTitle = btn.attributes('title')
    await btn.trigger('click')
    await nextTick()
    const onTitle = btn.attributes('title')

    expect(offTitle).toBeTruthy()
    expect(onTitle).toBeTruthy()
    expect(onTitle).not.toBe(offTitle)
  })

  it('reflects a previously stored choice on first render', () => {
    // The entry point applies the scale before mount; the button must agree
    // with it instead of showing "off" on a page that is already enlarged.
    stubScreen(1440)
    localStorage.setItem(SHARE_FIT_KEY, '1')
    const w = mountButton()
    expect(w.find('[data-action="share-fit"]').attributes('aria-pressed')).toBe('true')
  })

  it('uses a single icon and no text label', () => {
    // The requirement was "one icon is enough": a labelled button would crowd
    // the topbar on a phone, where the fit switch matters most.
    stubScreen(1440)
    const w = mountButton()
    const btn = w.find('[data-action="share-fit"]')
    expect(btn.find('svg').exists()).toBe(true)
    expect(btn.text()).toBe('')
  })
})
