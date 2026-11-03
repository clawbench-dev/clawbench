import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import enMessages from '@/i18n/locales/en'
import CaptureSourceGrid from '../CaptureSourceGrid.vue'
import type { CaptureSource } from '@/composables/useLiveShare'

/**
 * One list, two hosts (创建会议 and 会议中切换共享源).
 *
 * The contract worth pinning is the picture: the two call sites drifted apart
 * once — the create page rendered a still frame per source, the swap list only
 * rendered names — so a name-only regression here breaks both screens at once
 * instead of one of them silently.
 */

vi.mock('lucide-vue-next', async (importOriginal) => {
  const actual = await importOriginal<typeof import('lucide-vue-next')>()
  const stub = (name: string) => ({ name, props: { size: Number }, render: () => null })
  // Spread the real module: a narrow mock turns any icon this shared grid adds
  // into `undefined` and the render throws, which reads as a broken component.
  return { ...actual, Monitor: stub('Monitor'), MonitorOff: stub('MonitorOff'), Check: stub('Check'), Plus: stub('Plus') }
})

function mountGrid(props: Record<string, unknown>) {
  const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: enMessages } })
  return mount(CaptureSourceGrid, {
    props: props as never,
    global: { plugins: [i18n] },
  })
}

const sources: CaptureSource[] = [
  { id: 'screen:0:0', name: 'Entire Screen', kind: 'screen', thumbnail: 'data:image/png;base64,AAA' },
  { id: 'window:1:0', name: 'Terminal', kind: 'window', thumbnail: '' },
]

describe('CaptureSourceGrid', () => {
  it('shows the still frame when the host supplied one, an icon when it did not', () => {
    const wrapper = mountGrid({ sources, selectedId: '' })
    const tiles = wrapper.findAll('.capture-source')

    expect(tiles).toHaveLength(2)
    expect(tiles[0].find('.capture-source__thumb img').attributes('src')).toBe('data:image/png;base64,AAA')
    // Wayland placeholders and icon-less windows come through with an empty
    // thumbnail; the tile keeps its 16:9 shape instead of collapsing.
    expect(tiles[1].find('.capture-source__thumb img').exists()).toBe(false)
    expect(tiles[1].find('.capture-source__thumb').exists()).toBe(true)
  })

  it('labels each tile with its kind so a screen cannot be mistaken for a window', () => {
    const wrapper = mountGrid({ sources, selectedId: '' })
    const tiles = wrapper.findAll('.capture-source')

    // The label sits ON the frame: a third line of text under every tile pushed
    // the rest of the create form off a phone.
    expect(tiles[0].find('.capture-source__kind').text()).toBe('Screen')
    expect(tiles[1].find('.capture-source__kind').text()).toBe('Window')
    expect(tiles[1].find('.capture-source__name').text()).toBe('Terminal')
  })

  it('marks exactly the selected tile', () => {
    const wrapper = mountGrid({ sources, selectedId: 'window:1:0' })
    const tiles = wrapper.findAll('.capture-source')

    expect(tiles[0].classes()).not.toContain('capture-source--on')
    expect(tiles[1].classes()).toContain('capture-source--on')
  })

  it('reports the chosen id and ignores clicks while disabled', async () => {
    const wrapper = mountGrid({ sources, selectedId: '' })
    await wrapper.findAll('.capture-source')[0].trigger('click')
    expect(wrapper.emitted('select')).toEqual([['screen:0:0']])

    const busy = mountGrid({ sources, selectedId: '', disabled: true })
    await busy.findAll('.capture-source')[1].trigger('click')
    expect(busy.emitted('select')).toBeUndefined()
  })

  it('offers "share nothing" as a frame of its own when the host asks for it', async () => {
    const wrapper = mountGrid({ sources, selectedId: '', allowOff: true })
    const tiles = wrapper.findAll('.capture-source')

    expect(tiles).toHaveLength(3)
    expect(tiles[0].attributes('data-source')).toBe('__off__')
    // Nothing selected means exactly that: the empty frame is the choice.
    expect(tiles[0].classes()).toContain('capture-source--on')

    await tiles[0].trigger('click')
    expect(wrapper.emitted('select')).toEqual([['']])

    // Mid-meeting the tile is not offered: stopping the picture is the screen
    // toggle's job there, not a source choice.
    expect(mountGrid({ sources, selectedId: '' }).find('[data-source="__off__"]').exists()).toBe(false)
  })

  it('shows an "ask for the list" frame that reports the click, not a selection', async () => {
    const wrapper = mountGrid({ sources: [], selectedId: '', allowOff: true, pickLabel: 'Choose what to share' })
    const pick = wrapper.find('[data-source="__pick__"]')

    expect(pick.exists()).toBe(true)
    expect(pick.text()).toContain('Choose what to share')
    await pick.trigger('click')
    expect(wrapper.emitted('pick')).toHaveLength(1)
    expect(wrapper.emitted('select')).toBeUndefined()

    // Busy: the frame stays (the layout must not jump) but cannot be clicked
    // twice.
    const busy = mountGrid({ sources: [], selectedId: '', pickLabel: 'Listing…', pickBusy: true })
    expect(busy.find('[data-source="__pick__"]').attributes('disabled')).toBeDefined()
    // No label, no frame.
    expect(mountGrid({ sources: [], selectedId: '' }).find('[data-source="__pick__"]').exists()).toBe(false)
  })
})
