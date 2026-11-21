import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  // Keep the real module (createI18n etc.) so shared shells that import the
  // i18n singleton still load; only t() is stubbed.
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

// A fake kit whose Avatar renders a seed-tagged SVG, so tests can assert that a
// seed change re-renders every tile.
const { mockLoadAvatarKit, mockStyles, mockFileToAvatarDataUri, MockAvatarImageError } = vi.hoisted(() => {
  const mockStyles = { bottts: { $id: 'bottts' }, identicon: { $id: 'identicon' }, shapes: { $id: 'shapes' } }
  class MockAvatarImageError extends Error {
    code: string
    constructor(code: string) {
      super(code)
      this.name = 'AvatarImageError'
      this.code = code
    }
  }
  return {
    mockStyles,
    MockAvatarImageError,
    mockFileToAvatarDataUri: vi.fn(),
    mockLoadAvatarKit: vi.fn().mockResolvedValue({
      Avatar: class {
        constructor(public style: unknown, public options: { seed: string; backgroundColor?: string[] }) {}
        toString() {
          return `<svg data-seed="${this.options.seed}" data-bg="${this.options.backgroundColor?.[0] ?? ''}"></svg>`
        }
      },
      styles: mockStyles,
    }),
  }
})

vi.mock('@/utils/lazyAvatar', () => ({
  loadAvatarKit: mockLoadAvatarKit,
}))

vi.mock('@/utils/avatarPolicy', () => ({
  AVATAR_STYLES: ['bottts', 'identicon', 'shapes'],
  AVATAR_STYLE_GROUPS: [{ id: 'people', styles: ['bottts', 'identicon', 'shapes'] }],
  avatarStyleLabelKey: (style: string) => `settings.items.avatarStyles.${style}`,
  avatarBackgroundFor: () => '#E3ECF7',
}))

vi.mock('@/utils/avatarImage', () => ({
  fileToAvatarDataUri: mockFileToAvatarDataUri,
  AvatarImageError: MockAvatarImageError,
}))

vi.mock('@/composables/useBackHandler', () => ({
  registerBackHandler: () => () => {},
  PRIORITY_OVERLAY: 100,
}))

import AgentAvatarPicker from '@/components/settings/AgentAvatarPicker.vue'
import { avatarBackgroundFor } from '@/utils/avatarPolicy'

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
    mockFileToAvatarDataUri.mockReset()
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

  it('emits saved with the selected style SVG and its provenance', async () => {
    const wrapper = mountPicker()
    await flushPromises()

    await wrapper.find('.fbtn-primary').trigger('click')
    expect(wrapper.emitted('saved')).toBeTruthy()
    const spec = wrapper.emitted('saved')![0][0] as { avatar: string; style: string; seed: string }
    expect(spec.avatar).toContain('<svg')
    expect(spec.style).toBe('bottts')
    expect(spec.seed).toBe('CodeBuddy')
  })

  it('emits an empty spec when cleared', async () => {
    const wrapper = mountPicker()
    await flushPromises()

    await wrapper.find('.avatar-picker__clear').trigger('click')
    expect(wrapper.emitted('saved')![0][0]).toEqual({ avatar: '', style: '', seed: '' })
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

  it('bakes a soft background into every tile', async () => {
    const wrapper = mountPicker()
    await flushPromises()

    const bg = avatarBackgroundFor('CodeBuddy')
    const srcs = wrapper.findAll('.avatar-picker__tile-img').map(i => decodeURIComponent(i.attributes('src') || ''))
    for (const src of srcs) expect(src).toContain(`data-bg="${bg}"`)
  })

  it('uploads an image and saves it as a data URI', async () => {
    mockFileToAvatarDataUri.mockResolvedValue('data:image/webp;base64,UklGRg==')
    const wrapper = mountPicker()
    await flushPromises()

    const file = new File(['x'], 'avatar.png', { type: 'image/png' })
    const input = wrapper.find('input[type="file"]')
    Object.defineProperty(input.element, 'files', { value: [file], configurable: true })
    await input.trigger('change')
    await flushPromises()

    expect(mockFileToAvatarDataUri).toHaveBeenCalledWith(file, expect.any(String))
    const uploadedTile = wrapper.find('.avatar-picker__uploaded .avatar-picker__tile.is-selected')
    expect(uploadedTile.exists()).toBe(true)
    expect(uploadedTile.find('img').attributes('src')).toBe('data:image/webp;base64,UklGRg==')

    await wrapper.find('.fbtn-primary').trigger('click')
    expect(wrapper.emitted('saved')![0][0]).toEqual({
      avatar: 'data:image/webp;base64,UklGRg==',
      style: '',
      seed: '',
    })
  })

  it('shows a localized error when the upload is rejected', async () => {
    mockFileToAvatarDataUri.mockRejectedValue(new MockAvatarImageError('too-large'))
    const wrapper = mountPicker()
    await flushPromises()

    const input = wrapper.find('input[type="file"]')
    Object.defineProperty(input.element, 'files', {
      value: [new File(['x'], 'avatar.png', { type: 'image/png' })],
      configurable: true,
    })
    await input.trigger('change')
    await flushPromises()

    expect(wrapper.find('.avatar-picker__error-text').text()).toBe('settings.items.agentAvatarUploadTooLarge')
  })

  it('blocks save when the current avatar cannot be matched to a tile', async () => {
    const wrapper = mountPicker({ currentAvatar: '<svg>custom</svg>' })
    await flushPromises()

    expect(wrapper.find('.avatar-picker__current').exists()).toBe(true)
    expect(wrapper.find('.fbtn-primary').attributes('disabled')).toBeDefined()
  })

  it('re-enables save after an explicit shuffle', async () => {
    const wrapper = mountPicker({ currentAvatar: '<svg>custom</svg>' })
    await flushPromises()

    await wrapper.find('.avatar-picker__shuffle').trigger('click')
    expect(wrapper.find('.fbtn-primary').attributes('disabled')).toBeUndefined()
  })

  it('selects the matching tile when the current avatar uses the default seed', async () => {
    const svg = `<svg data-seed="CodeBuddy" data-bg="${avatarBackgroundFor('CodeBuddy')}"></svg>`
    const wrapper = mountPicker({ currentAvatar: svg })
    await flushPromises()

    expect(wrapper.find('.avatar-picker__current').exists()).toBe(false)
    expect(wrapper.find('.avatar-picker__tile.is-selected').attributes('data-style')).toBe('bottts')
    expect(wrapper.find('.fbtn-primary').attributes('disabled')).toBeUndefined()
  })

  it('reopens on the stored style and seed without matching the image', async () => {
    const wrapper = mountPicker({
      currentAvatar: '<svg>legacy</svg>',
      currentStyle: 'shapes',
      currentSeed: 'Custom Seed',
    })
    await flushPromises()

    expect(wrapper.find('.avatar-picker__current').exists()).toBe(false)
    expect(wrapper.find('.avatar-picker__tile.is-selected').attributes('data-style')).toBe('shapes')
    expect((wrapper.find('.avatar-picker__input').element as HTMLInputElement).value).toBe('Custom Seed')
    expect(wrapper.find('.fbtn-primary').attributes('disabled')).toBeUndefined()
  })

  it('falls back when provenance has a style but no seed', async () => {
    const wrapper = mountPicker({
      currentAvatar: '<svg>legacy</svg>',
      currentStyle: 'shapes',
      currentSeed: '',
    })
    await flushPromises()

    expect(wrapper.find('.avatar-picker__current').exists()).toBe(true)
    expect(wrapper.find('.fbtn-primary').attributes('disabled')).toBeDefined()
  })

  it('requires a new choice when the stored style is no longer in the catalog', async () => {
    const wrapper = mountPicker({
      currentAvatar: '<svg>legacy</svg>',
      currentStyle: 'removed-style',
      currentSeed: 'Custom Seed',
    })
    await flushPromises()

    // The old image is still shown, but the picker cannot render that style
    // any more, so Save stays disabled until the user picks an available one.
    expect(wrapper.find('.avatar-picker__current').exists()).toBe(true)
    expect(wrapper.find('.fbtn-primary').attributes('disabled')).toBeDefined()
  })

  it('treats an uploaded current avatar as the selected upload', async () => {
    const wrapper = mountPicker({ currentAvatar: 'data:image/webp;base64,UklGRg==' })
    await flushPromises()

    expect(wrapper.find('.avatar-picker__uploaded .avatar-picker__tile.is-selected').exists()).toBe(true)
    expect(wrapper.find('.fbtn-primary').attributes('disabled')).toBeUndefined()
  })
})
