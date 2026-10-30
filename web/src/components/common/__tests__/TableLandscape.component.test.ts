import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (k: string) => k, locale: { value: 'en' } }),
}))

// Plain `{ value }` object (not a real ref): the component only reads
// `.value`, and hoisting keeps it out of the mock factory's TDZ.
const { touchPrimary } = vi.hoisted(() => ({ touchPrimary: { value: false } }))
vi.mock('@/composables/usePlatformDetect.ts', () => ({
  usePlatformDetect: () => ({ isTouchPrimary: touchPrimary }),
}))

import TableLandscape from '@/components/common/TableLandscape.vue'
import { closeTableLandscape, openTableLandscape, tableLandscapeState } from '@/composables/useTableLandscape.ts'

function buildSourceBlock(): HTMLElement {
  const wrapper = document.createElement('div')
  wrapper.className = 'table-block-wrapper word-wrap'
  const holder = document.createElement('div')
  holder.className = 'table-wrap'
  holder.innerHTML = '<table><tbody><tr><td>only cell</td></tr></tbody></table>'
  wrapper.appendChild(holder)
  document.body.appendChild(wrapper)
  return wrapper
}

function setViewport(width: number, height: number): void {
  Object.defineProperty(window, 'innerWidth', { configurable: true, value: width })
  Object.defineProperty(window, 'innerHeight', { configurable: true, value: height })
}

async function mountOverlay() {
  const wrapper = mount(TableLandscape, { attachTo: document.body })
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  touchPrimary.value = false
  setViewport(1024, 768)
})

afterEach(() => {
  closeTableLandscape()
  document.body.innerHTML = ''
  document.body.style.overflow = ''
})

describe('TableLandscape', () => {
  it('renders nothing until a table is opened', async () => {
    const wrapper = await mountOverlay()
    expect(document.querySelector('.table-landscape')).toBeNull()
    wrapper.unmount()
  })

  it('renders the cloned table, locks body scroll and focuses the close button', async () => {
    const wrapper = await mountOverlay()
    openTableLandscape(buildSourceBlock())
    await flushPromises()
    await nextTick()

    const overlay = document.querySelector<HTMLElement>('.table-landscape')
    expect(overlay).not.toBeNull()
    expect(overlay!.textContent).toContain('only cell')
    expect(overlay!.getAttribute('aria-modal')).toBe('true')
    expect(document.body.style.overflow).toBe('hidden')
    expect(document.activeElement).toBe(document.querySelector('.table-landscape-close'))

    wrapper.unmount()
  })

  it('closes on Escape and releases the scroll lock', async () => {
    const wrapper = await mountOverlay()
    openTableLandscape(buildSourceBlock())
    await flushPromises()

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushPromises()

    expect(tableLandscapeState.value).toBeNull()
    expect(document.querySelector('.table-landscape')).toBeNull()
    expect(document.body.style.overflow).toBe('')

    wrapper.unmount()
  })

  it('closes from the close button and returns focus to the trigger', async () => {
    const wrapper = await mountOverlay()
    const trigger = document.createElement('button')
    trigger.className = 'table-block-landscape-btn'
    document.body.appendChild(trigger)
    openTableLandscape(buildSourceBlock(), trigger)
    await flushPromises()
    await nextTick()

    document.querySelector<HTMLElement>('.table-landscape-close')!.click()
    await flushPromises()

    expect(tableLandscapeState.value).toBeNull()
    expect(document.activeElement).toBe(trigger)

    wrapper.unmount()
  })

  it('closes when the empty area around the table is tapped', async () => {
    const wrapper = await mountOverlay()
    openTableLandscape(buildSourceBlock())
    await flushPromises()

    const scroll = document.querySelector<HTMLElement>('.table-landscape-scroll')!
    scroll.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await flushPromises()

    expect(tableLandscapeState.value).toBeNull()
    wrapper.unmount()
  })

  it('traps Tab inside the overlay', async () => {
    const wrapper = await mountOverlay()
    openTableLandscape(buildSourceBlock())
    await flushPromises()
    await nextTick()

    const event = new KeyboardEvent('keydown', { key: 'Tab', cancelable: true })
    document.dispatchEvent(event)
    await nextTick()

    // Only one focusable element exists, so Tab must be prevented rather than
    // allowed to move focus into the page behind the overlay.
    expect(event.defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(document.querySelector('.table-landscape-close'))

    wrapper.unmount()
  })

  it('rotates on a touch device held in portrait', async () => {
    touchPrimary.value = true
    setViewport(390, 844)
    const wrapper = await mountOverlay()
    openTableLandscape(buildSourceBlock())
    await flushPromises()

    expect(document.querySelector('.table-landscape')!.classList.contains('is-rotated')).toBe(true)
    wrapper.unmount()
  })

  it('keeps the table upright in a narrow desktop window', async () => {
    setViewport(400, 900)
    const wrapper = await mountOverlay()
    openTableLandscape(buildSourceBlock())
    await flushPromises()

    expect(document.querySelector('.table-landscape')!.classList.contains('is-rotated')).toBe(false)
    wrapper.unmount()
  })

  it('closes on unmount so the layer cannot survive a route change', async () => {
    const wrapper = await mountOverlay()
    openTableLandscape(buildSourceBlock())
    await flushPromises()

    wrapper.unmount()
    await flushPromises()

    expect(tableLandscapeState.value).toBeNull()
    expect(document.querySelector('.table-landscape')).toBeNull()
    expect(document.body.style.overflow).toBe('')
  })
})
