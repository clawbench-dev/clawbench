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
    // nothing about what clicking will do. Asserted against the actual strings:
    // "the two titles differ" would still pass if they were swapped.
    stubScreen(1440)
    const w = mountButton()
    const btn = w.find('[data-action="share-fit"]')

    expect(btn.attributes('title')).toBe(i18n.global.t('share.fitOn'))
    await btn.trigger('click')
    await nextTick()
    expect(btn.attributes('title')).toBe(i18n.global.t('share.fitOff'))
  })

  it('keeps a stable accessible name and reports state separately', async () => {
    // A toggle whose accessible name changes with its state reads as "恢复原始
    // 大小, 未按下" — the name and the pressed flag contradict each other. The
    // name says what the control IS; aria-pressed says whether it is on.
    stubScreen(1440)
    const w = mountButton()
    const btn = w.find('[data-action="share-fit"]')

    const name = btn.attributes('aria-label')
    expect(name).toBe(i18n.global.t('share.fitLabel'))
    await btn.trigger('click')
    await nextTick()
    expect(btn.attributes('aria-label')).toBe(name)
  })

  it('reflects a previously stored choice on first render', () => {
    // The entry point applies the scale before mount; the button must agree
    // with it instead of showing "off" on a page that is already enlarged.
    stubScreen(1440)
    localStorage.setItem(SHARE_FIT_KEY, '1')
    const w = mountButton()
    expect(w.find('[data-action="share-fit"]').attributes('aria-pressed')).toBe('true')
  })

  it('is absent when the screen already fits', () => {
    // 1080p maps to exactly 100%, so the switch could not change anything. A
    // dead control reads as broken, and on a phone it costs the file name room.
    stubScreen(1080)
    const w = mountButton()
    expect(w.find('[data-action="share-fit"]').exists()).toBe(false)
  })

  it('follows the same choice made in another tab', async () => {
    // `storage` fires only in the OTHER tabs, which is exactly the case that
    // needs syncing: otherwise a second tab keeps showing "off" (and the design
    // size) after the reader turned fitting on in the first one.
    stubScreen(1440)
    const w = mountButton()
    const btn = w.find('[data-action="share-fit"]')
    expect(btn.attributes('aria-pressed')).toBe('false')

    localStorage.setItem(SHARE_FIT_KEY, '1')
    window.dispatchEvent(new StorageEvent('storage', { key: SHARE_FIT_KEY, newValue: '1' }))
    await nextTick()

    expect(btn.attributes('aria-pressed')).toBe('true')
    expect(Number(document.documentElement.style.getPropertyValue(ROOT_ATTR))).toBeGreaterThan(1)
  })

  it('uses a single icon and no text label', () => {
    // The requirement was "one icon is enough": a labelled button would crowd
    // the topbar on a phone.
    stubScreen(1440)
    const w = mountButton()
    const btn = w.find('[data-action="share-fit"]')
    expect(btn.find('svg').exists()).toBe(true)
    expect(btn.text()).toBe('')
  })
})
