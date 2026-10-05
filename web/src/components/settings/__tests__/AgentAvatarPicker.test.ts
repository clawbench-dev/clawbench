import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key,
  }),
}))

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

// A fake kit whose Avatar renders a seed-tagged SVG, so tests can assert that a
// seed change re-renders every tile.
const { mockLoadAvatarKit, mockStyles } = vi.hoisted(() => {
  const mockStyles = { bottts: { $id: 'bottts' }, identicon: { $id: 'identicon' }, shapes: { $id: 'shapes' } }
  return {
    mockStyles,
    mockLoadAvatarKit: vi.fn().mockResolvedValue({
      Avatar: class {
        constructor(public style: unknown, public options: { seed: string }) {}
        toString() { return `<svg data-seed="${this.options.seed}"></svg>` }
      },
      styles: mockStyles,
    }),
  }
})

vi.mock('@/utils/lazyAvatar', () => ({
  AVATAR_STYLES: ['bottts', 'identicon', 'shapes'],
  loadAvatarKit: mockLoadAvatarKit,
}))

vi.mock('@/composables/useBackHandler', () => ({
  registerBackHandler: () => () => {},
  PRIORITY_OVERLAY: 100,
}))

import AgentAvatarPicker from '@/components/settings/AgentAvatarPicker.vue'

let wrappers: VueWrapper[] = []

function mountPicker(props: Record<string, unknown> = {}) {
  const w = mount(AgentAvatarPicker, {
    props: { open: true, agentName: 'CodeBuddy', ...props },
    // ModalDialog Teleports to <body>; stubbing Teleport keeps the DOM inside
    // the wrapper so find() works.
    global: { stubs: { Teleport: true } },
  })
  wrappers.push(w)
  return w
}

describe('AgentAvatarPicker', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  afterEach(() => {
    for (const w of wrappers) w.unmount()
    wrappers = []
  })

  it('loads the kit and renders one tile per style', async () => {
    const wrapper = mountPicker()
    await flushPromises()

    expect(mockLoadAvatarKit).toHaveBeenCalled()
    const tiles = wrapper.findAll('.avatar-picker__tile')
    expect(tiles.length).toBe(3)
    expect(tiles.map(t => t.attributes('data-style'))).toEqual(['bottts', 'identicon', 'shapes'])
    const img = wrapper.find('.avatar-picker__tile-img')
    expect(img.attributes('src')).toContain('data:image/svg+xml;charset=utf-8,')
  })

  it('marks the default style as selected', async () => {
    const wrapper = mountPicker()
    await flushPromises()
    const selected = wrapper.find('.avatar-picker__tile.is-selected')
    expect(selected.attributes('data-style')).toBe('bottts')
  })

  it('selects a different style on tile click', async () => {
    const wrapper = mountPicker()
    await flushPromises()

    await wrapper.find('.avatar-picker__tile[data-style="shapes"]').trigger('click')
    expect(wrapper.find('.avatar-picker__tile.is-selected').attributes('data-style')).toBe('shapes')
  })

  it('re-renders every tile when the seed changes', async () => {
    const wrapper = mountPicker()
    await flushPromises()

    const before = wrapper.findAll('.avatar-picker__tile-img').map(i => i.attributes('src'))
    await wrapper.find('.avatar-picker__input').setValue('NewSeed')
    await flushPromises()
    const after = wrapper.findAll('.avatar-picker__tile-img').map(i => i.attributes('src'))

    // Every tile changed (not just the selected one). The fake Avatar embeds the
    // seed verbatim, so we can also assert the new seed reached each tile.
    expect(after).not.toEqual(before)
    for (const src of after) {
      expect(decodeURIComponent(src!)).toContain('NewSeed')
    }
  })

  it('emits saved with the selected style SVG', async () => {
    const wrapper = mountPicker()
    await flushPromises()

    await wrapper.find('.fbtn-primary').trigger('click')
    expect(wrapper.emitted('saved')).toBeTruthy()
    expect(wrapper.emitted('saved')![0][0]).toContain('<svg')
  })

  it('emits saved with an empty string when cleared', async () => {
    const wrapper = mountPicker()
    await flushPromises()

    await wrapper.find('.avatar-picker__clear').trigger('click')
    expect(wrapper.emitted('saved')![0][0]).toBe('')
  })

  it('shows a retry affordance when the kit fails to load', async () => {
    mockLoadAvatarKit.mockRejectedValueOnce(new Error('network'))
    const wrapper = mountPicker()
    await flushPromises()

    expect(wrapper.find('.avatar-picker__error').exists()).toBe(true)
  })

  it('does not load the kit while closed', async () => {
    mountPicker({ open: false })
    await flushPromises()
    expect(mockLoadAvatarKit).not.toHaveBeenCalled()
  })
})
